package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/caioricciuti/ch-ui/internal/database"
)

// Sign-in with a ClickHouse URL (config allow_login_url, off by default) lets
// the login page take a URL instead of a saved connection, like v1 did. The
// server makes the connection, so the URL goes through the same checks as
// first-run setup, and the number of connections the login page may create
// is capped. Connections are reused by URL, so repeat logins do not pile up.
const (
	// settingLoginURLConnectionIDs is a JSON array of connection ids created
	// from the login page. Only ids that still exist count toward the cap.
	settingLoginURLConnectionIDs = "login_url_connection_ids"

	// loginURLMaxConnections caps the connections the login page may create.
	loginURLMaxConnections = 20

	errLoginURLDisabled = "Signing in with a ClickHouse URL is disabled on this server"
	errLoginURLAndConn  = "Send either a connection or a ClickHouse URL, not both"
	errLoginURLCapped   = "Too many connections were created from the login page. Ask an admin to remove unused ones in Admin > Connections."
)

// errLoginURLCapReached is returned by loginURLConnection when no saved
// connection matches and the cap is reached.
var errLoginURLCapReached = errors.New("login url connection cap reached")

// loginURLAllowed reports whether sign-in with a ClickHouse URL is enabled.
func (h *AuthHandler) loginURLAllowed() bool {
	return h.Config != nil && h.Config.AllowLoginURL
}

// clickHouseURLKey normalizes a ClickHouse URL for matching: scheme and host
// lower-cased, trailing slashes dropped from the path. Anything that does not
// parse is compared as trimmed text.
func clickHouseURLKey(raw string) string {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return strings.TrimRight(raw, "/")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	return u.String()
}

// loginURLConnectionName is the display name for a connection created from
// the login page: host[:port] of the URL.
func loginURLConnectionName(chURL string) string {
	if u, err := url.Parse(chURL); err == nil && u.Host != "" {
		return strings.ToLower(u.Host)
	}
	return chURL
}

// loginURLConnection returns the saved non-embedded direct connection whose
// URL matches chURL, or creates one. chURL must already have passed
// validateSetupClickHouseURL. created is true when this call made it. A new
// connection is recorded in settingLoginURLConnectionIDs and audited as
// connection.created_from_login. When none matches and the cap is reached it
// returns errLoginURLCapReached.
func (h *AuthHandler) loginURLConnection(chURL, username, clientIP string) (conn *database.Connection, created bool, err error) {
	h.loginURLMu.Lock()
	defer h.loginURLMu.Unlock()

	connections, err := h.DB.GetConnections()
	if err != nil {
		return nil, false, err
	}
	key := clickHouseURLKey(chURL)
	existing := make(map[string]bool, len(connections))
	for i := range connections {
		c := &connections[i]
		existing[c.ID] = true
		if !c.IsEmbedded && c.Type == database.ConnectionTypeDirect && clickHouseURLKey(c.ClickHouseURL) == key {
			return c, false, nil
		}
	}

	ids, err := loginURLConnectionIDs(h.DB)
	if err != nil {
		return nil, false, err
	}
	live := make([]string, 0, len(ids))
	for _, id := range ids {
		if existing[id] {
			live = append(live, id)
		}
	}
	if len(live) >= loginURLMaxConnections {
		return nil, false, errLoginURLCapReached
	}

	chURL = key // store the normalized form
	name := loginURLConnectionName(chURL)
	id, _, err := createConnectionRecord(h.DB, name, database.ConnectionTypeDirect, chURL)
	if err != nil {
		return nil, false, fmt.Errorf("create login url connection: %w", err)
	}
	// Deleted ids are dropped here so the list does not grow forever.
	raw, err := json.Marshal(append(live, id))
	if err != nil {
		return nil, false, err
	}
	if err := h.DB.SetSetting(settingLoginURLConnectionIDs, string(raw)); err != nil {
		return nil, false, fmt.Errorf("store login url connection id: %w", err)
	}
	conn, err = h.DB.GetConnectionByID(id)
	if err != nil {
		return nil, false, err
	}
	if conn == nil {
		return nil, false, fmt.Errorf("login url connection %s missing after create", id)
	}

	_ = h.DB.CreateAuditLog(database.AuditLogParams{
		Action:       "connection.created_from_login",
		Username:     strPtr(username),
		ConnectionID: strPtr(conn.ID),
		Details:      strPtr(fmt.Sprintf("Created direct connection %q to %s from the login page", conn.Name, conn.ClickHouseURL)),
		IPAddress:    strPtr(clientIP),
	})
	slog.Info("Created connection from the login page", "id", conn.ID, "clickhouse_url", conn.ClickHouseURL, "ip", clientIP)

	startDirectConnector(h.Agents, *conn)
	return conn, true, nil
}

// loginURLConnectionIDs reads settingLoginURLConnectionIDs. An unreadable
// value is treated as empty, with a warning, rather than blocking logins.
func loginURLConnectionIDs(db *database.DB) ([]string, error) {
	raw, err := db.GetSetting(settingLoginURLConnectionIDs)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var ids []string
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		slog.Warn("Ignoring unreadable login URL connection list", "key", settingLoginURLConnectionIDs, "error", err)
		return nil, nil
	}
	return ids, nil
}
