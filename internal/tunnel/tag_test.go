package tunnel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestQueryTagLogComment(t *testing.T) {
	cases := []struct {
		tag  QueryTag
		want string
	}{
		{QueryTag{}, ""},
		{QueryTag{Surface: "editor"}, `{"chui":"editor"}`},
		{QueryTag{Surface: "editor", Person: "caio@example.com"}, `{"chui":"editor","person":"caio@example.com"}`},
		{QueryTag{Surface: "dashboard", Person: "caio@example.com"}.WithObject("dash", "42"), `{"chui":"dashboard","person":"caio@example.com","object":"dash:42"}`},
		{QueryTag{Surface: "schedule"}.WithObject("schedule", ""), `{"chui":"schedule"}`},
	}
	for _, c := range cases {
		if got := c.tag.LogComment(); got != c.want {
			t.Errorf("%+v: got %s want %s", c.tag, got, c.want)
		}
	}
	if !strings.HasPrefix(QueryTag{Surface: "brain", Person: "x"}.LogComment(), LogCommentPrefix("brain")) {
		t.Errorf("LogCommentPrefix must match the rendered comment: %q", LogCommentPrefix("brain"))
	}
	long := strings.Repeat("a", 300)
	var decoded map[string]string
	if err := json.Unmarshal([]byte(QueryTag{Surface: "editor", Person: long}.LogComment()), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded["person"]) != maxTagField {
		t.Errorf("person must be clipped to %d bytes, got %d", maxTagField, len(decoded["person"]))
	}
}

func TestTagSettings(t *testing.T) {
	ctx := WithTag(context.Background(), QueryTag{Surface: "editor", Person: "p"})
	if got := TagSettings(context.Background(), nil); got != nil {
		t.Errorf("no tag: settings must pass through, got %v", got)
	}
	got := TagSettings(ctx, nil)
	if got["log_comment"] != `{"chui":"editor","person":"p"}` {
		t.Errorf("tag not applied: %v", got)
	}
	own := map[string]string{"log_comment": "ch-ui:cost-center", "readonly": "1"}
	if got := TagSettings(ctx, own); got["log_comment"] != "ch-ui:cost-center" {
		t.Errorf("an explicit log_comment must win: %v", got)
	}
	in := map[string]string{"readonly": "1"}
	out := TagSettings(ctx, in)
	if _, leaked := in["log_comment"]; leaked {
		t.Error("the caller's map must not be modified")
	}
	if out["readonly"] != "1" || out["log_comment"] == "" {
		t.Errorf("merged settings wrong: %v", out)
	}
	refined := WithObjectTag(ctx, "dash", "42")
	if tag, _ := TagFrom(refined); tag.Object != "dash:42" || tag.Person != "p" {
		t.Errorf("WithObjectTag must keep the person and add the object: %+v", tag)
	}
	if WithObjectTag(context.Background(), "dash", "42") != context.Background() {
		t.Error("WithObjectTag without a tag must return ctx unchanged")
	}
}

// fakeRecordingAgent answers every query with one row and reports the settings it saw.
func fakeRecordingAgent(ws *websocket.Conn, seen chan<- map[string]string) {
	for {
		var msg GatewayMessage
		if err := ws.ReadJSON(&msg); err != nil {
			return
		}
		if msg.Type != "query" {
			continue
		}
		seen <- msg.Settings
		if err := ws.WriteJSON(AgentMessage{Type: "query_result", ID: msg.ID, Data: json.RawMessage(`[{"value":1}]`)}); err != nil {
			return
		}
	}
}

func connectFakeAgent(t *testing.T, g *Gateway, agent func(*websocket.Conn, chan<- map[string]string)) <-chan map[string]string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(g.HandleWebSocket))
	t.Cleanup(server.Close)
	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	if err := ws.WriteJSON(AgentMessage{Type: "auth", Token: "test-only"}); err != nil {
		t.Fatal(err)
	}
	var ack GatewayMessage
	if err := ws.ReadJSON(&ack); err != nil || ack.Type != "auth_ok" {
		t.Fatalf("auth: %+v, %v", ack, err)
	}
	seen := make(chan map[string]string, 16)
	go agent(ws, seen)
	return seen
}

func TestGatewayAppliesQueryTag(t *testing.T) {
	g, connectionID := newTestGateway(t)
	seen := connectFakeAgent(t, g, fakeRecordingAgent)
	ctx := WithTag(context.Background(), QueryTag{Surface: "editor", Person: "caio@example.com"})

	if _, err := g.ExecuteQueryWithSettingsCtx(ctx, connectionID, "SELECT 1", "u", "", map[string]string{"max_execution_time": "5"}, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	got := <-seen
	if got["log_comment"] != `{"chui":"editor","person":"caio@example.com"}` || got["max_execution_time"] != "5" {
		t.Errorf("settings query: %v", got)
	}

	if _, err := g.ExecuteQueryWithFormatCtx(ctx, connectionID, "SELECT 1", "u", "", "JSONCompact", 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if got := <-seen; got["log_comment"] == "" {
		t.Errorf("format query must carry the tag: %v", got)
	}

	if _, err := g.ExecuteQuery(connectionID, "SELECT 1", "u", "", 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if got := <-seen; got["log_comment"] != "" {
		t.Errorf("a query without a tagged context must not be tagged: %v", got)
	}
}

func TestReadonlyProfileDropsQueryTag(t *testing.T) {
	g, connectionID := newTestGateway(t)
	seen := connectFakeAgent(t, g, func(ws *websocket.Conn, seen chan<- map[string]string) { fakeReadonlyAgent(t, ws, seen) })
	ctx := WithTag(context.Background(), QueryTag{Surface: "editor", Person: "ro@example.com"})
	if _, err := g.ExecuteQueryWithSettingsCtx(ctx, connectionID, "SELECT 1", "ro", "", map[string]string{"readonly": "1"}, 5*time.Second); err != nil {
		t.Fatalf("readonly profile must still run the query without the tag: %v", err)
	}
	first := <-seen
	if first["log_comment"] == "" {
		t.Fatalf("first attempt must carry the tag: %v", first)
	}
	second := <-seen
	if _, still := second["log_comment"]; still || second["readonly"] != "1" {
		t.Errorf("retry must drop the tag and keep readonly: %v", second)
	}
}
