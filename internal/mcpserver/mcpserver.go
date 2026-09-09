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
	"regexp"
	"strings"

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
		token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer"))
		if token == "" || !keyFormatRe.MatchString(token) {
			unauthorized(w, "missing or malformed MCP key; pass 'Authorization: Bearer chm_...'")
			return
		}

		k, err := deps.DB.GetMCPKeyByHash(HashKey(token))
		if err != nil || k == nil {
			unauthorized(w, "unknown or revoked MCP key")
			return
		}
		// Constant-time confirmation of the full hash (lookup already matched;
		// this guards against store lookups ever becoming prefix-based).
		if subtle.ConstantTimeCompare([]byte(k.KeyHash), []byte(HashKey(token))) != 1 {
			unauthorized(w, "unknown or revoked MCP key")
			return
		}

		password, err := crypto.Decrypt(k.CHPasswordEnc, deps.Config.AppSecretKey)
		if err != nil {
			http.Error(w, `{"error":"failed to decrypt key credentials"}`, http.StatusInternalServerError)
			return
		}

		go deps.DB.TouchMCPKey(k.ID)

		ctx := context.WithValue(r.Context(), ctxKey, &authedKey{key: k, chPassword: password})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func unauthorized(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("WWW-Authenticate", `Bearer realm="ch-ui-mcp"`)
	w.WriteHeader(http.StatusUnauthorized)
	w.Write([]byte(`{"error":"` + msg + `"}`))
}

// serverInstructions is sent to the client at initialize/discover time. It is
// the one place to tell the model how to work with this server well; keep it
// short, clients prepend it to the system prompt.
const serverInstructions = `CH-UI exposes one ClickHouse connection. Work schema-first: call list_databases, then list_tables, then describe_table before writing SQL; describe_table returns the sorting key, use it in WHERE clauses to avoid full scans. run_select is read-only, capped at 100 rows by default (max_rows up to 2000) and 60 seconds; aggregate or add LIMIT rather than paging through raw rows. Use explain_query before running an expensive query on a large table. ClickHouse SQL specifics: use toDate/toStartOfHour for time bucketing, uniq()/uniqExact() for distinct counts, and backticks for identifiers; there is no implicit type coercion between String and numbers. Tools with readOnlyHint=false only create drafts in CH-UI (saved queries, dashboards, models, pipelines); nothing they create runs against ClickHouse until a human reviews it in the UI.`

// buildServer assembles the per-request MCP server bound to the authenticated
// key. Free tools are always registered; Pro tools only with an active
// license.
func buildServer(deps Deps, ak *authedKey) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    "ch-ui",
		Title:   "CH-UI — self-hosted ClickHouse console",
		Version: version.Version,
	}, &mcp.ServerOptions{Instructions: serverInstructions})

	registerFreeTools(srv, deps, ak)
	registerListTools(srv, deps, ak)
	if ak.key.Scopes == "read_write" {
		registerWriteTools(srv, deps, ak)
	}
	if deps.Config != nil && deps.Config.IsPro() {
		registerProTools(srv, deps, ak)
	}
	return srv
}
