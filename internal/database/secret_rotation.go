package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/caioricciuti/ch-ui/internal/crypto"
)

// encryptedColumns lists every column that holds one value encrypted with
// the app secret key (crypto.Encrypt). idExpr names the row in reports; it
// never selects the secret itself. A new encrypted column must be added here
// or APP_SECRET_KEY rotation leaves it behind.
var encryptedColumns = []struct {
	kind, table, idExpr, column string
}{
	{"session password", "sessions", "id", "encrypted_password"},
	{"oauth code password", "oauth_codes", "code_hash", "ch_password_enc"},
	{"mcp key password", "mcp_keys", "id", "ch_password_enc"},
	{"dashboard share password", "dashboard_shares", "id", "encrypted_password"},
	{"background account password", "background_credentials", "connection_id || '/' || worker", "encrypted_password"},
	{"connection sso service account password", "connections", "id", "sso_ch_password_encrypted"},
	{"brain provider api key", "brain_providers", "id", "encrypted_api_key"},
	{"alert channel config", "alert_channels", "id", "config_encrypted"},
}

// pipelineSecretPrefix marks an encrypted value inside a pipeline node
// config. It must match encPrefix in internal/pipelines/secrets.go, which
// imports this package and so cannot be imported from here.
const pipelineSecretPrefix = "enc:v1:"

// UnreadableSecret names a stored secret that decrypts with neither the
// current nor the previous key. It never carries the value.
type UnreadableSecret struct {
	Kind string
	ID   string
}

// SecretRotationReport summarizes a RotateSecrets run. Counts are keyed by
// secret kind.
type SecretRotationReport struct {
	Rotated    map[string]int // re-encrypted from the previous key
	Current    map[string]int // already under the current key, left as is
	Unreadable []UnreadableSecret
}

// Kinds returns every kind that appears in the report, sorted.
func (r *SecretRotationReport) Kinds() []string {
	seen := map[string]bool{}
	for k := range r.Rotated {
		seen[k] = true
	}
	for k := range r.Current {
		seen[k] = true
	}
	for _, u := range r.Unreadable {
		seen[u.Kind] = true
	}
	kinds := make([]string, 0, len(seen))
	for k := range seen {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	return kinds
}

type secretRotator struct {
	cur, prev *crypto.Key
	report    *SecretRotationReport
}

// rotate returns value re-encrypted with the current key and true when it
// was written with the previous key. Values under the current key, empty
// values and values neither key opens come back unchanged.
func (r *secretRotator) rotate(kind, id, value string) (string, bool, error) {
	if value == "" {
		return value, false, nil
	}
	if _, err := r.cur.Decrypt(value); err == nil {
		r.report.Current[kind]++
		return value, false, nil
	}
	plain, err := r.prev.Decrypt(value)
	if err != nil {
		r.report.Unreadable = append(r.report.Unreadable, UnreadableSecret{Kind: kind, ID: id})
		return value, false, nil
	}
	enc, err := r.cur.Encrypt(plain)
	if err != nil {
		return "", false, fmt.Errorf("re-encrypt %s %s: %w", kind, id, err)
	}
	r.report.Rotated[kind]++
	return enc, true, nil
}

// rotateJSONFields re-encrypts string fields of a JSON object that pick
// selects. pick returns the ciphertext and how to rebuild the field from new
// ciphertext. Other fields keep their exact bytes.
func (r *secretRotator) rotateJSONFields(kind, id, raw string, pick func(field, value string) (string, func(string) string, bool)) (string, bool, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		return raw, false, nil
	}
	changed := false
	for name, rawValue := range fields {
		var s string
		if json.Unmarshal(rawValue, &s) != nil {
			continue
		}
		enc, rebuild, ok := pick(name, s)
		if !ok {
			continue
		}
		next, rotated, err := r.rotate(kind, id+"."+name, enc)
		if err != nil {
			return "", false, err
		}
		if !rotated {
			continue
		}
		b, err := json.Marshal(rebuild(next))
		if err != nil {
			return "", false, err
		}
		fields[name] = b
		changed = true
	}
	if !changed {
		return raw, false, nil
	}
	out, err := json.Marshal(fields)
	if err != nil {
		return "", false, err
	}
	return string(out), true, nil
}

// RotateSecrets re-encrypts every stored secret written with previousSecret
// so it is readable with currentSecret alone. It runs in one transaction:
// either every rotatable secret is rewritten or none is. Secrets already
// under the current key are skipped, so a second run changes nothing.
// Secrets neither key opens are left untouched and listed in the report.
func (db *DB) RotateSecrets(currentSecret, previousSecret string) (*SecretRotationReport, error) {
	if currentSecret == "" || previousSecret == "" {
		return nil, fmt.Errorf("both the current and the previous secret are required")
	}
	cur, err := crypto.NewKey(currentSecret)
	if err != nil {
		return nil, err
	}
	prev, err := crypto.NewKey(previousSecret)
	if err != nil {
		return nil, err
	}
	r := &secretRotator{cur: cur, prev: prev, report: &SecretRotationReport{
		Rotated: map[string]int{},
		Current: map[string]int{},
	}}

	tx, err := db.conn.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin rotation: %w", err)
	}
	defer tx.Rollback()

	for _, c := range encryptedColumns {
		if err := r.rotateColumn(tx, c.kind, c.table, c.idExpr, c.column); err != nil {
			return nil, err
		}
	}
	if err := r.rotatePipelineNodes(tx); err != nil {
		return nil, err
	}
	if err := r.rotateSettings(tx); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit rotation: %w", err)
	}
	return r.report, nil
}

type rotationRow struct {
	rowid int64
	id    string
	value string
}

// readRotationRows loads every row before any update, so no cursor is open
// on the table while it is being written.
func readRotationRows(tx *sql.Tx, query string) ([]rotationRow, error) {
	rows, err := tx.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []rotationRow
	for rows.Next() {
		var row rotationRow
		var id, value sql.NullString
		if err := rows.Scan(&row.rowid, &id, &value); err != nil {
			return nil, err
		}
		row.id, row.value = id.String, value.String
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *secretRotator) rotateColumn(tx *sql.Tx, kind, table, idExpr, column string) error {
	rows, err := readRotationRows(tx, fmt.Sprintf(
		"SELECT rowid, %s, %s FROM %s WHERE %s IS NOT NULL AND %s != ''",
		idExpr, column, table, column, column))
	if err != nil {
		return fmt.Errorf("read %s: %w", kind, err)
	}
	update := fmt.Sprintf("UPDATE %s SET %s = ? WHERE rowid = ?", table, column)
	for _, row := range rows {
		next, rotated, err := r.rotate(kind, row.id, row.value)
		if err != nil {
			return err
		}
		if !rotated {
			continue
		}
		if _, err := tx.Exec(update, next, row.rowid); err != nil {
			return fmt.Errorf("update %s %s: %w", kind, row.id, err)
		}
	}
	return nil
}

// rotatePipelineNodes handles pipeline node configs: plain JSON whose secret
// fields carry pipelineSecretPrefix plus ciphertext.
func (r *secretRotator) rotatePipelineNodes(tx *sql.Tx) error {
	const kind = "pipeline node secret"
	rows, err := readRotationRows(tx, "SELECT rowid, id, config_encrypted FROM pipeline_nodes WHERE config_encrypted LIKE '%"+pipelineSecretPrefix+"%'")
	if err != nil {
		return fmt.Errorf("read %s: %w", kind, err)
	}
	pick := func(_, value string) (string, func(string) string, bool) {
		if !strings.HasPrefix(value, pipelineSecretPrefix) {
			return "", nil, false
		}
		return strings.TrimPrefix(value, pipelineSecretPrefix), func(enc string) string { return pipelineSecretPrefix + enc }, true
	}
	for _, row := range rows {
		next, changed, err := r.rotateJSONFields(kind, row.id, row.value, pick)
		if err != nil {
			return err
		}
		if !changed {
			continue
		}
		if _, err := tx.Exec("UPDATE pipeline_nodes SET config_encrypted = ? WHERE rowid = ?", next, row.rowid); err != nil {
			return fmt.Errorf("update %s %s: %w", kind, row.id, err)
		}
	}
	return nil
}

// rotateSettings handles the settings rows that hold secrets: the SSO client
// secret inside the sso_config JSON and the GitHub sync token and webhook
// secret per connection (github.<connection>.encrypted_pat and
// .webhook_secret, see github_sync.go).
func (r *secretRotator) rotateSettings(tx *sql.Tx) error {
	rows, err := readRotationRows(tx, "SELECT rowid, key, value FROM settings WHERE key = '"+SettingSSOConfig+"' OR key LIKE 'github.%'")
	if err != nil {
		return fmt.Errorf("read settings: %w", err)
	}
	for _, row := range rows {
		var next string
		var changed bool
		switch {
		case row.id == SettingSSOConfig:
			next, changed, err = r.rotateJSONFields("sso client secret", row.id, row.value, func(field, value string) (string, func(string) string, bool) {
				if field != "client_secret_enc" {
					return "", nil, false
				}
				return value, func(enc string) string { return enc }, true
			})
		case strings.HasSuffix(row.id, ".encrypted_pat"):
			next, changed, err = r.rotate("github token", row.id, row.value)
		case strings.HasSuffix(row.id, ".webhook_secret"):
			next, changed, err = r.rotate("github webhook secret", row.id, row.value)
		default:
			continue
		}
		if err != nil {
			return err
		}
		if !changed {
			continue
		}
		if _, err := tx.Exec("UPDATE settings SET value = ? WHERE rowid = ?", next, row.rowid); err != nil {
			return fmt.Errorf("update setting %s: %w", row.id, err)
		}
	}
	return nil
}
