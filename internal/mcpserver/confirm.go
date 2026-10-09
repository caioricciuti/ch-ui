package mcpserver

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Confirm-before-write for the MCP write tools.
//
// Clients on protocol 2026-07-28 or later that declare form elicitation get
// a yes/no prompt before anything is written. The prompt travels as a
// multi-round-trip input request: the first call returns InputRequests and
// writes nothing, the client asks the user and retries the same call with
// the answer in InputResponses.
//
// Older clients keep the previous behaviour (the write runs without a
// prompt). This is deliberate: the server is stateless, and for older
// protocols the SDK would try to fulfil input requests itself with a
// server-to-client elicitation/create request, which a stateless server
// cannot send. confirmWrite therefore never returns input requests unless
// the SDK will hand them to the client as an input_required result.
//
// No RequestState is used. The client retries with the original arguments,
// so the handler re-validates them and the answer applies to exactly what is
// written. A client could fake an "accept" answer, but it already holds the
// key and could write without asking anyone; the prompt protects the user
// from the model, not the server from the client.

// mrtrMinProtocol is the first protocol version where the SDK returns input
// requests to the client instead of fulfilling them server-side.
const mrtrMinProtocol = "2026-07-28"

// confirmInputID keys the confirmation in InputRequests / InputResponses.
const confirmInputID = "confirm_write"

// confirmWrite decides whether a write tool may proceed. It returns nil when
// the write should run, or a result the handler must return as-is: either
// the input request asking the user, or the "declined" result.
func confirmWrite(deps Deps, ak *authedKey, req *mcp.CallToolRequest, message string) *mcp.CallToolResult {
	if !clientCanConfirm(req) {
		return nil
	}

	var resp mcp.InputResponse
	if req.Params != nil {
		resp = req.Params.InputResponses[confirmInputID]
	}
	if resp == nil {
		return &mcp.CallToolResult{InputRequests: mcp.InputRequestMap{
			confirmInputID: &mcp.ElicitParams{
				Mode:    "form",
				Message: message,
				RequestedSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"confirm": map[string]any{
							"type":        "boolean",
							"title":       "Confirm",
							"description": "Write this to CH-UI",
							"default":     false,
						},
					},
					"required": []string{"confirm"},
				},
			},
		}}
	}

	if er, ok := resp.(*mcp.ElicitResult); ok && er.Action == "accept" {
		if confirmed, _ := er.Content["confirm"].(bool); confirmed {
			return nil
		}
	}

	tool := ""
	if req.Params != nil {
		tool = req.Params.Name
	}
	audit(deps, ak, "mcp.write.declined", "mcp key: "+ak.key.Name+", tool: "+tool+", prompt: "+message)
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{
		Text: "The user declined this change. Nothing was written to CH-UI.",
	}}}
}

// clientCanConfirm reports whether the caller gets input requests back as an
// input_required result (protocol 2026-07-28+) and declared form
// elicitation. It is stricter than the SDK's own check: a session without
// InitializeParams counts as old, so we never return input requests the SDK
// would try to fulfil with a server-to-client request.
func clientCanConfirm(req *mcp.CallToolRequest) bool {
	if req == nil || req.Session == nil {
		return false
	}
	ip := req.Session.InitializeParams()
	if ip == nil || ip.ProtocolVersion < mrtrMinProtocol {
		return false
	}
	caps := req.ClientCapabilities()
	if caps == nil || caps.Elicitation == nil {
		return false
	}
	// An empty elicitation object means form support.
	return caps.Elicitation.Form != nil || caps.Elicitation.URL == nil
}
