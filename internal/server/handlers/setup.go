package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/embedded"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

// First-run setup lets whoever can read the server log point CH-UI at
// ClickHouse from the login page, for installs where the embedded connection
// cannot reach ClickHouse and so nobody can log in to fix it. It is gated by a
// one-time code printed to the log at startup and closes for good on the
// first admin login (password or SSO), or at startup when an admin already
// exists. It never edits the embedded connection: that one is rewritten from
// config on every start (embedded.ensureEmbeddedConnection).
const (
	settingSetupClosedAt     = "setup_closed_at"
	settingSetupConnectionID = "setup_connection_id"
	// The embedded connection's URL when setup first saved a connection, i.e.
	// the URL that could not be reached. While the embedded connection still
	// has it, the login picker hides the embedded connection.
	settingSetupEmbeddedURL = "setup_embedded_url"

	setupCodeTTL          = time.Hour
	setupMaxBadCodes      = 10
	setupMaxAttemptsPerIP = 5
	setupCodeLength       = 12
	setupMaxNameLength    = 100

	// setupActor is the audit username for everything done through setup.
	setupActor = "setup"
)

// crockfordAlphabet is Crockford base32: no I, L, O or U.
const crockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// SetupHandler serves POST /api/auth/setup and owns the in-memory setup code.
// The code itself is never stored; only its SHA-256 is kept, in memory.
type SetupHandler struct {
	DB          *database.DB
	RateLimiter *middleware.RateLimiter
	Agents      *embedded.Manager
	// Now is the clock; nil means time.Now. Tests inject their own.
	Now func() time.Time

	mu        sync.Mutex
	codeHash  []byte // nil when no code was issued, or it was burned
	expiresAt time.Time
	badCodes  int

	// saveMu serialises the create-or-update path so two correct requests
	// cannot create two connections.
	saveMu sync.Mutex
}

func (s *SetupHandler) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Start runs once at server startup. When an admin already exists it closes
// setup for good; when setup is open it issues a code and logs it once.
func (s *SetupHandler) Start() {
	closedAt, err := s.DB.GetSetting(settingSetupClosedAt)
	if err != nil {
		slog.Error("First-run setup disabled: could not read setup state", "error", err)
		return
	}
	if closedAt != "" {
		return
	}
	admins, err := s.DB.CountUsersWithRole("admin")
	if err != nil {
		slog.Error("First-run setup disabled: could not count admins", "error", err)
		return
	}
	if admins > 0 {
		closeSetup(s.DB, "an admin already exists at startup", setupActor, "")
		return
	}

	code, err := s.issueCode()
	if err != nil {
		slog.Error("First-run setup disabled: could not generate a setup code", "error", err)
		return
	}
	slog.Warn("First-run setup is open. Enter this setup code on the login page to point CH-UI at ClickHouse. It expires after 1 hour or when the first admin signs in.",
		"setup_code", code)
}

// issueCode generates a new code, keeps its hash and returns the display form.
func (s *SetupHandler) issueCode() (string, error) {
	code, err := generateSetupCode()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(normalizeSetupCode(code)))
	s.mu.Lock()
	s.codeHash = sum[:]
	s.expiresAt = s.now().Add(setupCodeTTL)
	s.badCodes = 0
	s.mu.Unlock()
	return code, nil
}

// generateSetupCode returns 12 Crockford base32 characters as XXXX-XXXX-XXXX.
// 256 is a multiple of 32, so masking each byte has no modulo bias.
func generateSetupCode() (string, error) {
	buf := make([]byte, setupCodeLength)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	var b strings.Builder
	for i, c := range buf {
		if i > 0 && i%4 == 0 {
			b.WriteByte('-')
		}
		b.WriteByte(crockfordAlphabet[c&31])
	}
	return b.String(), nil
}

// normalizeSetupCode upper-cases, drops dashes and spaces, and maps the
// Crockford look-alikes (O to 0, I and L to 1).
func normalizeSetupCode(raw string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(raw) {
		switch r {
		case '-', ' ', '\t':
			continue
		case 'O':
			r = '0'
		case 'I', 'L':
			r = '1'
		}
		b.WriteRune(r)
	}
	return b.String()
}

// codeLive reports whether a code was issued, is not burned and not expired.
func (s *SetupHandler) codeLive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.codeHash != nil && s.now().Before(s.expiresAt)
}

// discardCode forgets the code, for when setup closes.
func (s *SetupHandler) discardCode() {
	s.mu.Lock()
	s.codeHash = nil
	s.mu.Unlock()
}

// checkCode compares a submitted code in constant time. On a mismatch it
// counts the bad code and burns the code after setupMaxBadCodes; burned is
// true when this call burned it.
func (s *SetupHandler) checkCode(raw string) (ok, burned bool) {
	sum := sha256.Sum256([]byte(normalizeSetupCode(raw)))
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.codeHash == nil {
		return false, false
	}
	if subtle.ConstantTimeCompare(sum[:], s.codeHash) == 1 {
		return true, false
	}
	s.badCodes++
	if s.badCodes >= setupMaxBadCodes {
		s.codeHash = nil
		return false, true
	}
	return false, false
}

// Open is what GET /api/auth/config reports as setup_open: setup not closed,
// no admin yet, and a live code.
func (s *SetupHandler) Open() bool {
	if s == nil || !s.codeLive() {
		return false
	}
	open, err := setupOpen(s.DB)
	return err == nil && open
}

// setupOpen reports whether setup is still open in the database: the sticky
// closed flag is empty and no explicit CH-UI admin exists.
func setupOpen(db *database.DB) (bool, error) {
	closedAt, err := db.GetSetting(settingSetupClosedAt)
	if err != nil {
		return false, err
	}
	if closedAt != "" {
		return false, nil
	}
	admins, err := db.CountUsersWithRole("admin")
	if err != nil {
		return false, err
	}
	return admins == 0, nil
}

// closeSetup writes the sticky closed flag if it is still empty and audits
// setup.closed. It returns true when this call closed setup.
func closeSetup(db *database.DB, reason, actor, clientIP string) bool {
	closedAt, err := db.GetSetting(settingSetupClosedAt)
	if err != nil {
		slog.Warn("Could not read setup state", "error", err)
		return false
	}
	if closedAt != "" {
		return false
	}
	if err := db.SetSetting(settingSetupClosedAt, time.Now().UTC().Format(time.RFC3339)); err != nil {
		slog.Warn("Could not close first-run setup", "error", err)
		return false
	}
	_ = db.CreateAuditLog(database.AuditLogParams{
		Action:    "setup.closed",
		Username:  strPtr(actor),
		Details:   strPtr(fmt.Sprintf("First-run setup closed: %s", reason)),
		IPAddress: strPtr(clientIP),
	})
	slog.Info("First-run setup closed", "reason", reason)
	return true
}

// closeSetupOnAdminLogin closes setup after a successful admin login. Other
// roles leave it alone. setup may be nil.
func closeSetupOnAdminLogin(db *database.DB, setup *SetupHandler, role, actor, clientIP string) {
	if role != "admin" {
		return
	}
	closeSetup(db, "first admin signed in", actor, clientIP)
	if setup != nil {
		setup.discardCode()
	}
}

type setupRequest struct {
	Code          string `json:"code"`
	Name          string `json:"name"`
	ClickHouseURL string `json:"clickhouse_url"`
}

// Setup saves the first-run direct connection.
//
// POST /api/auth/setup  {"code": "...", "name": "...", "clickhouse_url": "..."}
func (s *SetupHandler) Setup(w http.ResponseWriter, r *http.Request) {
	open, err := setupOpen(s.DB)
	if err != nil {
		slog.Error("Failed to read setup state", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to read setup state")
		return
	}
	if !open {
		writeError(w, http.StatusNotFound, "Setup is closed")
		return
	}
	if !s.codeLive() {
		writeError(w, http.StatusGone, "Setup code expired. Restart CH-UI to get a new one.")
		return
	}

	clientIP := getClientIP(r)
	ipKey := "setup:ip:" + clientIP
	if s.RateLimiter != nil {
		res := s.RateLimiter.CheckAuthRateLimit(ipKey, "ip", setupMaxAttemptsPerIP, RateLimitWindow)
		if !res.Allowed {
			retrySeconds := int(res.RetryAfter.Seconds())
			slog.Warn("Setup rate limited", "ip", clientIP, "retryAfter", retrySeconds)
			writeJSON(w, http.StatusTooManyRequests, map[string]interface{}{
				"success":    false,
				"error":      "Too many setup attempts from this IP",
				"retryAfter": retrySeconds,
			})
			return
		}
	}

	var req setupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if strings.TrimSpace(req.Code) == "" {
		writeError(w, http.StatusBadRequest, "Setup code is required")
		return
	}

	ok, burned := s.checkCode(req.Code)
	if !ok {
		if s.RateLimiter != nil {
			s.RateLimiter.RecordAttempt(ipKey, "ip")
		}
		_ = s.DB.CreateAuditLog(database.AuditLogParams{
			Action:    "setup.code_rejected",
			Username:  strPtr(setupActor),
			Details:   strPtr("Invalid setup code"),
			IPAddress: strPtr(clientIP),
		})
		if burned {
			_ = s.DB.CreateAuditLog(database.AuditLogParams{
				Action:    "setup.code_burned",
				Username:  strPtr(setupActor),
				Details:   strPtr(fmt.Sprintf("Setup code discarded after %d invalid attempts", setupMaxBadCodes)),
				IPAddress: strPtr(clientIP),
			})
			slog.Warn("Setup code discarded after too many invalid attempts; restart CH-UI to get a new setup code")
		}
		writeError(w, http.StatusUnauthorized, "Invalid setup code")
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "Connection name is required")
		return
	}
	if utf8.RuneCountInString(name) > setupMaxNameLength {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Connection name must be at most %d characters", setupMaxNameLength))
		return
	}
	chURL, err := validateSetupClickHouseURL(req.ClickHouseURL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	conn, err := s.saveConnection(name, chURL)
	if err != nil {
		slog.Error("Failed to save setup connection", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to save connection")
		return
	}

	if s.RateLimiter != nil {
		s.RateLimiter.ResetLimit(ipKey)
	}
	_ = s.DB.CreateAuditLog(database.AuditLogParams{
		Action:       "setup.connection_saved",
		Username:     strPtr(setupActor),
		ConnectionID: strPtr(conn.ID),
		Details:      strPtr(fmt.Sprintf("First-run setup saved connection %q to %s", conn.Name, conn.ClickHouseURL)),
		IPAddress:    strPtr(clientIP),
	})
	slog.Info("First-run setup saved connection", "id", conn.ID, "name", conn.Name, "clickhouse_url", conn.ClickHouseURL)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"connection": map[string]interface{}{
			"id":   conn.ID,
			"name": conn.Name,
		},
	})
}

// saveConnection creates the setup connection the first time and updates the
// same one afterwards. If it was deleted (or is somehow not a plain direct
// connection) a new one is created. The connector is started or restarted.
func (s *SetupHandler) saveConnection(name, chURL string) (*database.Connection, error) {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()

	var conn *database.Connection
	id, err := s.DB.GetSetting(settingSetupConnectionID)
	if err != nil {
		return nil, err
	}
	if id != "" {
		existing, err := s.DB.GetConnectionByID(id)
		if err != nil {
			return nil, err
		}
		if existing != nil && !existing.IsEmbedded && existing.Type == database.ConnectionTypeDirect {
			conn = existing
		}
	}

	if conn != nil {
		if _, err := renameConnection(s.DB, conn, name); err != nil {
			return nil, fmt.Errorf("rename setup connection: %w", err)
		}
		if _, err := setConnectionURL(s.DB, conn, chURL); err != nil {
			return nil, fmt.Errorf("update setup connection url: %w", err)
		}
	} else {
		newID, _, err := createConnectionRecord(s.DB, name, database.ConnectionTypeDirect, chURL)
		if err != nil {
			return nil, fmt.Errorf("create setup connection: %w", err)
		}
		if err := s.DB.SetSetting(settingSetupConnectionID, newID); err != nil {
			return nil, fmt.Errorf("store setup connection id: %w", err)
		}
		if embedded, err := s.DB.GetEmbeddedConnection(); err == nil && embedded != nil {
			if err := s.DB.SetSetting(settingSetupEmbeddedURL, embedded.ClickHouseURL); err != nil {
				return nil, fmt.Errorf("store embedded url: %w", err)
			}
		}
		conn, err = s.DB.GetConnectionByID(newID)
		if err != nil {
			return nil, err
		}
		if conn == nil {
			return nil, fmt.Errorf("setup connection %s missing after create", newID)
		}
	}

	// Always (re)start: a retry with the same URL should reconnect.
	startDirectConnector(s.Agents, *conn)
	return conn, nil
}

// hiddenLoginConnectionID returns the id of the embedded connection when the
// login picker should hide it: a setup connection exists and the embedded
// connection still points at the URL it had when setup replaced it. Changing
// clickhouse_url in config brings it back. Admin always lists it.
func hiddenLoginConnectionID(db *database.DB) string {
	setupID, err := db.GetSetting(settingSetupConnectionID)
	if err != nil || setupID == "" {
		return ""
	}
	brokenURL, err := db.GetSetting(settingSetupEmbeddedURL)
	if err != nil || brokenURL == "" {
		return ""
	}
	if setupConn, err := db.GetConnectionByID(setupID); err != nil || setupConn == nil {
		return ""
	}
	embedded, err := db.GetEmbeddedConnection()
	if err != nil || embedded == nil || embedded.ClickHouseURL != brokenURL {
		return ""
	}
	return embedded.ID
}

// validateSetupClickHouseURL is the stricter URL check for the
// unauthenticated setup endpoint: on top of validateClickHouseURL it rejects
// credentials, query strings, fragments, link-local and cloud metadata
// targets, and numeric hosts that are not standard IP literals. Loopback and
// private ranges are allowed, since that is where ClickHouse usually lives.
func validateSetupClickHouseURL(raw string) (string, error) {
	validated, err := validateClickHouseURL(raw)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(validated)
	if err != nil {
		return "", fmt.Errorf("clickhouse_url must be a valid http:// or https:// URL")
	}
	if u.User != nil {
		return "", fmt.Errorf("clickhouse_url must not contain a username or password")
	}
	if u.RawQuery != "" || u.ForceQuery {
		return "", fmt.Errorf("clickhouse_url must not contain a query string")
	}
	if u.Fragment != "" || strings.Contains(validated, "#") {
		return "", fmt.Errorf("clickhouse_url must not contain a fragment")
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "" {
		return "", fmt.Errorf("clickhouse_url must include a host")
	}
	if host == "metadata.google.internal" {
		return "", fmt.Errorf("clickhouse_url must not point at a cloud metadata service")
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		addr = addr.Unmap()
		if addr.IsLinkLocalUnicast() || addr == netip.MustParseAddr("fd00:ec2::254") {
			return "", fmt.Errorf("clickhouse_url must not point at a link-local or cloud metadata address")
		}
		return validated, nil
	}
	// A host whose last label is numeric is not a DNS name. Some resolvers
	// read forms like 2852039166 or 0xA9FEA9FE as IPv4, which would dodge
	// the check above.
	labels := strings.Split(host, ".")
	if last := labels[len(labels)-1]; last != "" && (isAllDigits(last) || strings.HasPrefix(last, "0x")) {
		return "", fmt.Errorf("clickhouse_url host must be a hostname or a standard IP address")
	}
	return validated, nil
}

func isAllDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}
