package mcpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const newProtocol = "2026-07-28"

// callToolVersioned posts one tools/call with the Mcp-Protocol-Version header
// set. For protocol 2026-07-28 the per-request _meta carries the version and
// client capabilities (capsJSON); extraParams is spliced into params as-is
// (e.g. `"inputResponses":{...}`).
func callToolVersioned(t *testing.T, h http.Handler, key, version, capsJSON, name, argsJSON, extraParams string) string {
	t.Helper()
	params := `"name":"` + name + `","arguments":` + argsJSON
	if version >= newProtocol {
		params += `,"_meta":{"io.modelcontextprotocol/protocolVersion":"` + version + `",` +
			`"io.modelcontextprotocol/clientInfo":{"name":"test","version":"0.0.0"},` +
			`"io.modelcontextprotocol/clientCapabilities":` + capsJSON + `}`
	}
	if extraParams != "" {
		params += "," + extraParams
	}
	body := `{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{` + params + `}}`

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Mcp-Protocol-Version", version)
	if version >= newProtocol {
		req.Header.Set("Mcp-Method", "tools/call")
		req.Header.Set("Mcp-Name", name)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s transport: want 200, got %d: %s", name, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

type rpcToolResult struct {
	Result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		IsError       bool                       `json:"isError"`
		ResultType    string                     `json:"resultType"`
		InputRequests map[string]json.RawMessage `json:"inputRequests"`
	} `json:"result"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// decodeToolResult parses a JSON or single-event SSE response body.
func decodeToolResult(t *testing.T, body string) rpcToolResult {
	t.Helper()
	payload := body
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "data:") {
			payload = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		}
	}
	var r rpcToolResult
	if err := json.Unmarshal([]byte(payload), &r); err != nil {
		t.Fatalf("decode response: %v\n%s", err, body)
	}
	if r.Error != nil {
		t.Fatalf("rpc error: %s", r.Error.Message)
	}
	return r
}

func savedQueryCount(t *testing.T, deps Deps) int {
	t.Helper()
	qs, err := deps.DB.GetSavedQueries()
	if err != nil {
		t.Fatalf("list saved queries: %v", err)
	}
	return len(qs)
}

const saveArgs = `{"name":"weekly revenue","sql":"SELECT sum(amount) FROM orders"}`

func TestConfirmWriteOldProtocolWritesImmediately(t *testing.T) {
	deps, _, writeKey := writeDeps(t)
	h := Handler(deps)

	r := decodeToolResult(t, callToolVersioned(t, h, writeKey, "2025-06-18", "", "save_query", saveArgs, ""))
	if r.Result.IsError || r.Result.InputRequests != nil {
		t.Fatalf("old protocol should write without a prompt: %+v", r.Result)
	}
	if n := savedQueryCount(t, deps); n != 1 {
		t.Fatalf("want 1 saved query, got %d", n)
	}
}

func TestConfirmWriteNewProtocolWithoutElicitationWritesImmediately(t *testing.T) {
	deps, _, writeKey := writeDeps(t)
	h := Handler(deps)

	r := decodeToolResult(t, callToolVersioned(t, h, writeKey, newProtocol, `{}`, "save_query", saveArgs, ""))
	if r.Result.IsError || r.Result.InputRequests != nil {
		t.Fatalf("client without elicitation should write without a prompt: %+v", r.Result)
	}
	if n := savedQueryCount(t, deps); n != 1 {
		t.Fatalf("want 1 saved query, got %d", n)
	}
}

func TestConfirmWriteNewProtocolWithElicitation(t *testing.T) {
	deps, _, writeKey := writeDeps(t)
	h := Handler(deps)
	caps := `{"elicitation":{"form":{}}}`

	// First call: an input request, nothing written.
	r := decodeToolResult(t, callToolVersioned(t, h, writeKey, newProtocol, caps, "save_query", saveArgs, ""))
	if r.Result.ResultType != "input_required" {
		t.Errorf("want resultType input_required, got %q", r.Result.ResultType)
	}
	raw, ok := r.Result.InputRequests[confirmInputID]
	if !ok {
		t.Fatalf("want input request %q, got %+v", confirmInputID, r.Result)
	}
	var ir struct {
		Method string `json:"method"`
		Params struct {
			Mode            string `json:"mode"`
			Message         string `json:"message"`
			RequestedSchema struct {
				Properties map[string]struct {
					Type string `json:"type"`
				} `json:"properties"`
			} `json:"requestedSchema"`
		} `json:"params"`
	}
	if err := json.Unmarshal(raw, &ir); err != nil {
		t.Fatalf("decode input request: %v", err)
	}
	if ir.Method != "elicitation/create" || ir.Params.Mode != "form" {
		t.Errorf("want form elicitation, got method %q mode %q", ir.Method, ir.Params.Mode)
	}
	if ir.Params.Message != `Save query "weekly revenue" to CH-UI?` {
		t.Errorf("unexpected prompt: %q", ir.Params.Message)
	}
	if ir.Params.RequestedSchema.Properties["confirm"].Type != "boolean" {
		t.Errorf("schema should have a boolean confirm: %s", raw)
	}
	if n := savedQueryCount(t, deps); n != 0 {
		t.Fatalf("first round must not write, got %d saved queries", n)
	}

	// Retry with decline: nothing written, plain (non-error) declined result.
	r = decodeToolResult(t, callToolVersioned(t, h, writeKey, newProtocol, caps, "save_query", saveArgs,
		`"inputResponses":{"confirm_write":{"action":"decline"}}`))
	if r.Result.IsError || r.Result.InputRequests != nil {
		t.Fatalf("decline should be a normal result: %+v", r.Result)
	}
	if len(r.Result.Content) == 0 || !strings.Contains(r.Result.Content[0].Text, "declined") {
		t.Errorf("want declined message, got %+v", r.Result.Content)
	}
	if n := savedQueryCount(t, deps); n != 0 {
		t.Fatalf("decline must not write, got %d saved queries", n)
	}

	// Accept with confirm=false counts as a decline.
	r = decodeToolResult(t, callToolVersioned(t, h, writeKey, newProtocol, caps, "save_query", saveArgs,
		`"inputResponses":{"confirm_write":{"action":"accept","content":{"confirm":false}}}`))
	if len(r.Result.Content) == 0 || !strings.Contains(r.Result.Content[0].Text, "declined") {
		t.Errorf("confirm=false: want declined message, got %+v", r.Result.Content)
	}
	if n := savedQueryCount(t, deps); n != 0 {
		t.Fatalf("confirm=false must not write, got %d saved queries", n)
	}

	// Retry with accept + confirm=true: written.
	r = decodeToolResult(t, callToolVersioned(t, h, writeKey, newProtocol, caps, "save_query", saveArgs,
		`"inputResponses":{"confirm_write":{"action":"accept","content":{"confirm":true}}}`))
	if r.Result.IsError || r.Result.InputRequests != nil {
		t.Fatalf("accept should write: %+v", r.Result)
	}
	if n := savedQueryCount(t, deps); n != 1 {
		t.Fatalf("want 1 saved query after accept, got %d", n)
	}
}

func TestConfirmWriteAllWriteToolsPrompt(t *testing.T) {
	deps, _, writeKey := writeDeps(t)
	h := Handler(deps)
	caps := `{"elicitation":{}}` // empty object means form support

	calls := map[string]string{
		"save_query":       saveArgs,
		"create_dashboard": `{"name":"Signups","panels":[{"name":"Total","sql":"SELECT 1"}]}`,
		"create_model":     `{"name":"daily_rollup","sql":"SELECT 1"}`,
		"create_pipeline":  `{"name":"ingest","source_type":"source_webhook","target_database":"default","target_table":"events"}`,
	}
	for tool, args := range calls {
		r := decodeToolResult(t, callToolVersioned(t, h, writeKey, newProtocol, caps, tool, args, ""))
		if _, ok := r.Result.InputRequests[confirmInputID]; !ok {
			t.Errorf("%s: want a confirmation input request, got %+v", tool, r.Result)
		}
	}
	if n := savedQueryCount(t, deps); n != 0 {
		t.Errorf("no saved query should exist, got %d", n)
	}
	if ds, _ := deps.DB.GetDashboards(""); len(ds) != 0 {
		t.Errorf("no dashboard should exist, got %d", len(ds))
	}
	keys, err := deps.DB.ListMCPKeys()
	if err != nil || len(keys) == 0 {
		t.Fatalf("list keys: %v", err)
	}
	if ms, _ := deps.DB.GetModelsByConnection(keys[0].ConnectionID); len(ms) != 0 {
		t.Errorf("no model should exist, got %d", len(ms))
	}
	if ps, _ := deps.DB.GetPipelines(); len(ps) != 0 {
		t.Errorf("no pipeline should exist, got %d", len(ps))
	}

	// Invalid arguments are rejected before any prompt.
	r := decodeToolResult(t, callToolVersioned(t, h, writeKey, newProtocol, caps, "save_query", `{"name":"","sql":""}`, ""))
	if !r.Result.IsError || r.Result.InputRequests != nil {
		t.Errorf("invalid args should error without prompting: %+v", r.Result)
	}
}
