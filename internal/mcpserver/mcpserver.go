// Package mcpserver embeds a Model Context Protocol server in CH-UI, exposing
// governed, read-only ClickHouse access to MCP clients (Claude Code, claude.ai,
// Cursor, ...) over the streamable HTTP transport at /mcp.
//
// Design rules:
//   - Auth is mandatory. Every request carries a chm_ bearer key; there is no
//     sessionless/unauthenticated mode.
//   - Queries execute through the existing tunnel Gateway with the key's own
//     ClickHouse credentials. The ClickHouse user's grants are the real
//     permission boundary; server-forced settings (readonly=2, row/time caps)
//     and a read-only statement gate sit on top.
//   - Every executed query is recorded in query history (source=mcp) and the
//     audit log, so DBAs can see exactly what an AI did.
package mcpserver

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/governance"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
	"github.com/caioricciuti/ch-ui/internal/version"
)

// Deps carries the shared server dependencies the MCP tools execute against.
type Deps struct {
	DB         *database.DB
	Gateway    *tunnel.Gateway
	Config     *config.Config
	Guardrails *governance.GuardrailService // nil when governance is unavailable
}

// authedKey is a validated MCP key plus its decrypted ClickHouse password,
// resolved once per request by the auth middleware.
type authedKey struct {
	key        *database.MCPKey
	chPassword string
}

type ctxKeyType struct{}

var ctxKey ctxKeyType

// GenerateKey returns a new plaintext MCP key, its SHA-256 hex hash for
// storage, and the display prefix. The plaintext is shown exactly once.
func GenerateKey() (plaintext, hash, prefix string) {
	b := make([]byte, 16)
	rand.Read(b)
	plaintext = "chm_" + hex.EncodeToString(b)
	return plaintext, HashKey(plaintext), plaintext[:12] + "..."
}

// HashKey returns the SHA-256 hex digest used to store and look up keys.
func HashKey(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

var keyFormatRe = regexp.MustCompile(`^chm_[a-f0-9]{32}$`)

// Handler returns the authenticated /mcp http.Handler.
func Handler(deps Deps) http.Handler {
	streamable := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		ak, ok := r.Context().Value(ctxKey).(*authedKey)
		if !ok {
			// The auth middleware rejects unauthenticated requests before the
			// streamable handler runs; this is unreachable, but never serve an
			// unauthenticated server if it somehow isn't.
			return nil
		}
		return buildServer(deps, ak)
	}, &mcp.StreamableHTTPOptions{Stateless: true})

	return authMiddleware(deps, streamable)
}

// authMiddleware validates the Bearer chm_ key, loads its connection, decrypts
// the ClickHouse password, and stashes the result in the request context.
func authMiddleware(deps Deps, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		prm := BaseURL(r, deps.Config) + "/.well-known/oauth-protected-resource/mcp"
		token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer"))
		if token == "" || !keyFormatRe.MatchString(token) {
			unauthorized(w, prm, "missing or malformed token; pass 'Authorization: Bearer chm_...' or sign in with OAuth")
			return
		}

		k, err := deps.DB.GetMCPKeyByHash(HashKey(token))
		if err != nil || k == nil {
			unauthorized(w, prm, "unknown or revoked token")
			return
		}
		// Constant-time confirmation of the full hash (lookup already matched;
		// this guards against store lookups ever becoming prefix-based).
		if subtle.ConstantTimeCompare([]byte(k.KeyHash), []byte(HashKey(token))) != 1 {
			unauthorized(w, prm, "unknown or revoked token")
			return
		}
		if k.Expired(time.Now()) {
			if k.Kind == "oauth" {
				unauthorized(w, prm, "access token expired; use the refresh token or sign in again")
			} else {
				unauthorized(w, prm, "MCP key expired; rotate it in Admin → MCP Server")
			}
			return
		}
		password, err := crypto.Decrypt(k.CHPasswordEnc, deps.Config.AppSecretKey)
		if err != nil {
			http.Error(w, `{"error":"failed to decrypt key credentials"}`, http.StatusInternalServerError)
			return
		}

		if !limiter.allow(k.ID) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", strconv.Itoa(int(rateWindow.Seconds())))
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":"rate limit exceeded for this MCP key"}`))
			return
		}
		touchKey(deps, k.ID)

		ctx := context.WithValue(r.Context(), ctxKey, &authedKey{key: k, chPassword: password})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ---- per-key rate limit and last-used throttle ----

const (
	// rateLimit is the number of MCP HTTP requests one key may make per
	// rateWindow. Each tool call is one request; an agent loop that trips this
	// is misbehaving, not working.
	rateLimit  = 120
	rateWindow = time.Minute

	// touchInterval bounds how often last_used_at is written per key; a
	// SQLite write per request is wasted I/O.
	touchInterval = time.Minute
)

// keyLimiter is a fixed-window counter per key. In-memory on purpose: the
// budget is per process and resets on restart, which is fine for abuse
// control (the ClickHouse settings are the real resource guard).
type keyLimiter struct {
	mu      sync.Mutex
	windows map[string]*rateEntry
}

type rateEntry struct {
	start time.Time
	count int
}

var limiter = &keyLimiter{windows: make(map[string]*rateEntry)}

func (l *keyLimiter) allow(keyID string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.windows[keyID]
	if !ok || now.Sub(e.start) >= rateWindow {
		l.windows[keyID] = &rateEntry{start: now, count: 1}
		if len(l.windows) > 4096 { // keys revoked long ago; drop stale windows
			for id, w := range l.windows {
				if now.Sub(w.start) >= rateWindow {
					delete(l.windows, id)
				}
			}
		}
		return true
	}
	e.count++
	return e.count <= rateLimit
}

var lastTouch sync.Map // key ID -> time.Time

func touchKey(deps Deps, keyID string) {
	now := time.Now()
	if v, ok := lastTouch.Load(keyID); ok && now.Sub(v.(time.Time)) < touchInterval {
		return
	}
	lastTouch.Store(keyID, now)
	go deps.DB.TouchMCPKey(keyID)
}

// unauthorized answers 401 with the RFC 9728 pointer OAuth-capable clients
// use to discover the authorization server.
func unauthorized(w http.ResponseWriter, resourceMetadata, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("WWW-Authenticate", `Bearer realm="ch-ui-mcp", resource_metadata="`+resourceMetadata+`", scope="read"`)
	w.WriteHeader(http.StatusUnauthorized)
	w.Write([]byte(`{"error":"` + msg + `"}`))
}

// BaseURL returns the public origin CH-UI is reached at, for OAuth metadata
// and redirects. Reverse-proxy headers win, then the request itself, then
// the configured app_url.
func BaseURL(r *http.Request, cfg *config.Config) string {
	if r != nil && r.Host != "" {
		scheme := "http"
		if r.TLS != nil || strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https") {
			scheme = "https"
		}
		host := r.Host
		if fh := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); fh != "" {
			host = fh
		}
		return scheme + "://" + host
	}
	if cfg != nil && strings.TrimSpace(cfg.AppURL) != "" {
		if u, err := url.Parse(cfg.AppURL); err == nil && u.Host != "" {
			return u.Scheme + "://" + u.Host
		}
	}
	return "http://localhost"
}

// serverInstructions is sent to the client at initialize/discover time. It is
// the one place to tell the model how to work with this server well; keep it
// short, clients prepend it to the system prompt.
const serverInstructions = `CH-UI exposes one ClickHouse connection. Work schema-first: search_catalog finds tables, columns, saved queries and dashboards by name or comment; then describe_table (sorting key, sample rows, size) before writing SQL, and use the sorting key in WHERE clauses to avoid full scans. Prefer run_saved_query when a verified saved query answers the question. Call estimate_query before querying a large table and pass max_bytes to run_select when the scan is big. run_select is read-only, capped at 100 rows by default (max_rows up to 2000) and 60 seconds; aggregate or add LIMIT rather than paging through raw rows. Use explain_query before running an expensive query on a large table. ClickHouse SQL specifics: use toDate/toStartOfHour for time bucketing, uniq()/uniqExact() for distinct counts, and backticks for identifiers; there is no implicit type coercion between String and numbers. Tools with readOnlyHint=false only create drafts in CH-UI (saved queries, dashboards, models, pipelines); nothing they create runs against ClickHouse until a human reviews it in the UI.`

// auditToolCalls writes one audit row per tools/call so every tool, not just
// run_select, is attributable to a key. run_select keeps its own richer
// mcp.query.execute row (with the SQL in query history) and is skipped here.
func auditToolCalls(deps Deps, ak *authedKey) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			res, err := next(ctx, method, req)
			if method != "tools/call" {
				return res, err
			}
			name := ""
			switch p := req.GetParams().(type) {
			case *mcp.CallToolParamsRaw:
				name = p.Name
			case *mcp.CallToolParams:
				name = p.Name
			}
			if name == "" || name == "run_select" {
				return res, err
			}
			status := "ok"
			if err != nil {
				status = "error"
			} else if r, ok := res.(*mcp.CallToolResult); ok && r.IsError {
				status = "error"
			}
			audit(deps, ak, "mcp.tool.call", "mcp key: "+ak.key.Name+", tool: "+name+", status: "+status)
			return res, err
		}
	}
}

// buildServer assembles the per-request MCP server bound to the authenticated
// key. Free tools are always registered; Pro tools only with an active
// license.
func buildServer(deps Deps, ak *authedKey) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    "ch-ui",
		Title:   "CH-UI — self-hosted ClickHouse console",
		Version: version.Version,
	}, &mcp.ServerOptions{Instructions: serverInstructions})

	srv.AddReceivingMiddleware(auditToolCalls(deps, ak))
	registerFreeTools(srv, deps, ak)
	registerCatalogTools(srv, deps, ak)
	registerListTools(srv, deps, ak)
	if ak.key.Scopes == "read_write" {
		registerWriteTools(srv, deps, ak)
	}
	if deps.Config != nil && deps.Config.IsPro() {
		registerProTools(srv, deps, ak)
	}
	return srv
}
