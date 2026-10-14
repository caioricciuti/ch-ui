package database

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/crypto"
)

const (
	rotOldSecret   = "rotation-old-secret"
	rotNewSecret   = "rotation-new-secret"
	rotOtherSecret = "rotation-unknown-secret"
)

func encWith(t *testing.T, secret, plain string) string {
	t.Helper()
	enc, err := crypto.Encrypt(plain, secret)
	if err != nil {
		t.Fatal(err)
	}
	return enc
}

// storedSecret reads one stored secret back as ciphertext.
type storedSecret struct {
	kind  string
	plain string
	read  func(t *testing.T, db *DB) string
}

func queryString(t *testing.T, db *DB, q string, args ...interface{}) string {
	t.Helper()
	var s string
	if err := db.conn.QueryRow(q, args...).Scan(&s); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return s
}

func jsonField(t *testing.T, raw, field string) string {
	t.Helper()
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	s, _ := m[field].(string)
	return s
}

// seedEverySecretKind stores one secret of every kind encrypted with secret
// and returns how to read each back.
func seedEverySecretKind(t *testing.T, db *DB, secret string) []storedSecret {
	t.Helper()
	enc := func(p string) string { return encWith(t, secret, p) }
	exec := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := db.conn.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}

	exec(`INSERT INTO connections (id, name, tunnel_token, sso_ch_user, sso_ch_password_encrypted) VALUES ('c1', 'c1', 'tok-c1', 'sso', ?)`, enc("sso-sa-pw"))
	exec(`INSERT INTO sessions (id, connection_id, clickhouse_user, encrypted_password, token, expires_at) VALUES ('s1', 'c1', 'u', ?, 'stok', '2099-01-01T00:00:00Z')`, enc("session-pw"))
	exec(`INSERT INTO oauth_codes (code_hash, client_id, client_name, redirect_uri, scope, code_challenge, connection_id, ch_user, ch_password_enc, subject, expires_at) VALUES ('h1', 'cl', 'cl', 'http://x', 'read', 'cc', 'c1', 'u', ?, 'sub', '2099-01-01T00:00:00Z')`, enc("oauth-pw"))
	exec(`INSERT INTO mcp_keys (id, name, key_hash, key_prefix, connection_id, ch_user, ch_password_enc) VALUES ('k1', 'k', 'kh', 'kp', 'c1', 'u', ?)`, enc("mcp-pw"))
	exec(`INSERT INTO dashboards (id, name) VALUES ('d1', 'd')`)
	exec(`INSERT INTO dashboard_shares (id, dashboard_id, token, connection_id, clickhouse_user, encrypted_password) VALUES ('sh1', 'd1', 'shtok', 'c1', 'u', ?)`, enc("share-pw"))
	exec(`INSERT INTO background_credentials (connection_id, worker, mode, username, encrypted_password, updated_at) VALUES ('c1', 'alerts', 'service_account', 'svc', ?, '2026-01-01T00:00:00Z')`, enc("bg-pw"))
	exec(`INSERT INTO brain_providers (id, name, kind, encrypted_api_key) VALUES ('p1', 'p', 'openai', ?)`, enc("brain-key"))
	exec(`INSERT INTO alert_channels (id, name, channel_type, config_encrypted) VALUES ('ch1', 'ch', 'webhook', ?)`, enc(`{"url":"https://hook"}`))
	exec(`INSERT INTO pipelines (id, name, connection_id) VALUES ('pl1', 'pl', 'c1')`)
	nodeCfg, _ := json.Marshal(map[string]interface{}{
		"password": pipelineSecretPrefix + enc("pipeline-pw"),
		"host":     "kafka:9092",
		"batch":    json.Number("12345678901234567890"),
		"api_key":  "",
	})
	exec(`INSERT INTO pipeline_nodes (id, pipeline_id, node_type, label, config_encrypted) VALUES ('n1', 'pl1', 'source_kafka', 'n', ?)`, string(nodeCfg))
	ssoCfg, _ := json.Marshal(SSOConfig{Enabled: true, IssuerURL: "https://idp", ClientID: "cid", ClientSecretEnc: enc("sso-client-secret")})
	if err := db.SetSetting(SettingSSOConfig, string(ssoCfg)); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSetting("github.c1.encrypted_pat", enc("gh-pat")); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSetting("github.c1.webhook_secret", enc("gh-hook")); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSetting("github.c1.repo", "owner/repo"); err != nil {
		t.Fatal(err)
	}

	col := func(q string) func(*testing.T, *DB) string {
		return func(t *testing.T, db *DB) string { return queryString(t, db, q) }
	}
	return []storedSecret{
		{"session password", "session-pw", col(`SELECT encrypted_password FROM sessions WHERE id = 's1'`)},
		{"oauth code password", "oauth-pw", col(`SELECT ch_password_enc FROM oauth_codes WHERE code_hash = 'h1'`)},
		{"mcp key password", "mcp-pw", col(`SELECT ch_password_enc FROM mcp_keys WHERE id = 'k1'`)},
		{"dashboard share password", "share-pw", col(`SELECT encrypted_password FROM dashboard_shares WHERE id = 'sh1'`)},
		{"background account password", "bg-pw", col(`SELECT encrypted_password FROM background_credentials WHERE connection_id = 'c1'`)},
		{"connection sso service account password", "sso-sa-pw", col(`SELECT sso_ch_password_encrypted FROM connections WHERE id = 'c1'`)},
		{"brain provider api key", "brain-key", col(`SELECT encrypted_api_key FROM brain_providers WHERE id = 'p1'`)},
		{"alert channel config", `{"url":"https://hook"}`, col(`SELECT config_encrypted FROM alert_channels WHERE id = 'ch1'`)},
		{"pipeline node secret", "pipeline-pw", func(t *testing.T, db *DB) string {
			v := jsonField(t, queryString(t, db, `SELECT config_encrypted FROM pipeline_nodes WHERE id = 'n1'`), "password")
			if !strings.HasPrefix(v, pipelineSecretPrefix) {
				t.Fatalf("pipeline secret lost its prefix: %q", v)
			}
			return strings.TrimPrefix(v, pipelineSecretPrefix)
		}},
		{"sso client secret", "sso-client-secret", func(t *testing.T, db *DB) string {
			return jsonField(t, queryString(t, db, `SELECT value FROM settings WHERE key = ?`, SettingSSOConfig), "client_secret_enc")
		}},
		{"github token", "gh-pat", col(`SELECT value FROM settings WHERE key = 'github.c1.encrypted_pat'`)},
		{"github webhook secret", "gh-hook", col(`SELECT value FROM settings WHERE key = 'github.c1.webhook_secret'`)},
	}
}

func TestRotateSecretsConvertsEveryKind(t *testing.T) {
	db := openTestDB(t)
	secrets := seedEverySecretKind(t, db, rotOldSecret)

	report, err := db.RotateSecrets(rotNewSecret, rotOldSecret)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Unreadable) != 0 {
		t.Fatalf("unexpected unreadable secrets: %+v", report.Unreadable)
	}

	newKey, err := crypto.NewKey(rotNewSecret)
	if err != nil {
		t.Fatal(err)
	}
	oldKey, err := crypto.NewKey(rotOldSecret)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range secrets {
		if report.Rotated[s.kind] != 1 {
			t.Errorf("%s: rotated %d, want 1", s.kind, report.Rotated[s.kind])
		}
		enc := s.read(t, db)
		if got, err := newKey.Decrypt(enc); err != nil || got != s.plain {
			t.Errorf("%s: new key decrypt = %q, %v", s.kind, got, err)
		}
		if _, err := oldKey.Decrypt(enc); err == nil {
			t.Errorf("%s: still readable with the old key", s.kind)
		}
	}
	if len(report.Rotated) != len(secrets) {
		t.Errorf("rotated kinds %v, want %d kinds", report.Rotated, len(secrets))
	}

	// Non-secret fields survive untouched.
	node := queryString(t, db, `SELECT config_encrypted FROM pipeline_nodes WHERE id = 'n1'`)
	if !strings.Contains(node, `"batch":12345678901234567890`) || !strings.Contains(node, `"host":"kafka:9092"`) {
		t.Errorf("pipeline config fields changed: %s", node)
	}
	if got := queryString(t, db, `SELECT value FROM settings WHERE key = 'github.c1.repo'`); got != "owner/repo" {
		t.Errorf("github repo setting changed: %q", got)
	}
	if sso, err := db.GetSSOConfig(); err != nil || sso.IssuerURL != "https://idp" || sso.ClientID != "cid" || !sso.Enabled {
		t.Errorf("sso config fields changed: %+v, %v", sso, err)
	}

	// A second run is a no-op.
	before := make([]string, len(secrets))
	for i, s := range secrets {
		before[i] = s.read(t, db)
	}
	again, err := db.RotateSecrets(rotNewSecret, rotOldSecret)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Rotated) != 0 || len(again.Unreadable) != 0 {
		t.Fatalf("second run rotated %v, unreadable %v", again.Rotated, again.Unreadable)
	}
	for i, s := range secrets {
		if again.Current[s.kind] != 1 {
			t.Errorf("%s: second run current %d, want 1", s.kind, again.Current[s.kind])
		}
		if s.read(t, db) != before[i] {
			t.Errorf("%s: second run rewrote the value", s.kind)
		}
	}
}

func TestRotateSecretsReportsUnknownKey(t *testing.T) {
	db := openTestDB(t)
	unknown := encWith(t, rotOtherSecret, "mystery")
	if _, err := db.conn.Exec(`INSERT INTO alert_channels (id, name, channel_type, config_encrypted) VALUES ('ch-x', 'x', 'webhook', ?)`, unknown); err != nil {
		t.Fatal(err)
	}
	if _, err := db.conn.Exec(`INSERT INTO brain_providers (id, name, kind, encrypted_api_key) VALUES ('p-old', 'p', 'openai', ?)`, encWith(t, rotOldSecret, "k")); err != nil {
		t.Fatal(err)
	}

	report, err := db.RotateSecrets(rotNewSecret, rotOldSecret)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Unreadable) != 1 || report.Unreadable[0] != (UnreadableSecret{Kind: "alert channel config", ID: "ch-x"}) {
		t.Fatalf("unreadable = %+v", report.Unreadable)
	}
	if report.Rotated["brain provider api key"] != 1 {
		t.Fatalf("readable secret next to an unknown one not rotated: %v", report.Rotated)
	}
	if got := queryString(t, db, `SELECT config_encrypted FROM alert_channels WHERE id = 'ch-x'`); got != unknown {
		t.Fatal("unreadable secret was modified")
	}
}

func TestRotateSecretsRequiresBothKeys(t *testing.T) {
	db := openTestDB(t)
	if _, err := db.RotateSecrets(rotNewSecret, ""); err == nil {
		t.Fatal("missing previous key accepted")
	}
}

// Every column whose name says it holds an encrypted value must be in
// encryptedColumns, or a key rotation silently leaves it behind. Columns that
// are encrypted in a different shape (pipeline_nodes.config_encrypted holds
// JSON with enc:v1: fields) are handled separately and listed here.
func TestEncryptedColumnsListIsComplete(t *testing.T) {
	db := openTestDB(t)
	handledElsewhere := map[string]bool{"pipeline_nodes.config_encrypted": true}
	listed := map[string]bool{}
	for _, c := range encryptedColumns {
		listed[c.table+"."+c.column] = true
	}
	rows, err := db.conn.Query(`SELECT m.name, p.name FROM sqlite_master m, pragma_table_info(m.name) p
		WHERE m.type = 'table' AND (p.name LIKE '%encrypt%' OR p.name LIKE '%\_enc' ESCAPE '\')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var table, column string
		if err := rows.Scan(&table, &column); err != nil {
			t.Fatal(err)
		}
		key := table + "." + column
		if !listed[key] && !handledElsewhere[key] {
			t.Errorf("%s looks encrypted but is not in encryptedColumns; APP_SECRET_KEY rotation would skip it", key)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}
