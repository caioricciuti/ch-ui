package mcpserver

import (
	"context"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/caioricciuti/ch-ui/internal/database"
)

type queryHistoryArgs struct {
	Range  string `json:"range,omitempty" jsonschema:"how far back to look: 1h, 6h, 24h, 7d (default), or 30d"`
	Status string `json:"status,omitempty" jsonschema:"ok or error; omit for both"`
	Limit  int    `json:"limit,omitempty" jsonschema:"maximum queries to return, newest first (default 50, max 500)"`
}

// historyStatuses maps the tool's status argument to the stored value.
var historyStatuses = map[string]string{"": "", "ok": "success", "success": "success", "error": "error"}

// registerHistoryTools exposes the MCP client's own query history. Available
// to every key.
func registerHistoryTools(srv *mcp.Server, deps Deps, ak *authedKey) {
	title, ann := readOnlyTool("Query history")
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "query_history",
		Title:       title,
		Annotations: ann,
		Description: "The queries this MCP client already ran on this connection, newest first, with status, error, elapsed time and rows returned. Never includes queries run in the CH-UI editor or by other keys or people.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args queryHistoryArgs) (*mcp.CallToolResult, any, error) {
		rng := args.Range
		window, ok := opsRanges[rng]
		if !ok {
			rng, window = "7d", opsRanges["7d"]
		}
		status, ok := historyStatuses[strings.ToLower(strings.TrimSpace(args.Status))]
		if !ok {
			return errResult("status must be ok or error"), nil, nil
		}
		f := database.MCPHistoryFilter{
			ConnectionID: ak.key.ConnectionID,
			Since:        time.Now().UTC().Add(-window).Format("2006-01-02 15:04:05"),
			Status:       status,
			Limit:        clampPageSize(args.Limit),
		}
		// An OAuth token reads its person's MCP history: a refresh mints a
		// new key row, and the history must survive it. An API key reads
		// only its own rows; its ClickHouse user may be shared with people
		// in the editor and with other keys. No allow-list filter: every row
		// is a query this client ran, already checked against the
		// allow-list when it ran.
		if s := strings.TrimSpace(ak.key.Subject); s != "" {
			f.Actor = s
		} else {
			f.KeyID = ak.key.ID
		}
		entries, err := deps.DB.ListMCPQueryHistory(f)
		if err != nil {
			return errResult("failed to read query history: %v", err), nil, nil
		}
		items := make([]map[string]any, 0, len(entries))
		for _, e := range entries {
			item := map[string]any{
				"query": e.QueryText, "status": e.Status, "elapsed_ms": e.ElapsedMS,
				"rows_returned": e.RowsReturned, "created_at": e.CreatedAt,
			}
			if e.ErrorMessage != nil {
				item["error"] = *e.ErrorMessage
			}
			items = append(items, item)
		}
		return jsonResult(map[string]any{"range": rng, "queries": items}), nil, nil
	})
}
