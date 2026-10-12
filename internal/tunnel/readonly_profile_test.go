package tunnel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// fakeReadonlyAgent answers like ClickHouse for a user whose profile is
// readonly=1: any setting other than readonly and bind parameters is refused,
// naming the setting. It records the settings of every query it receives.
func fakeReadonlyAgent(t *testing.T, ws *websocket.Conn, seen chan<- map[string]string) {
	for {
		var msg GatewayMessage
		if err := ws.ReadJSON(&msg); err != nil {
			return
		}
		if msg.Type != "query" {
			continue
		}
		seen <- msg.Settings
		refused := ""
		for k := range msg.Settings {
			if k != "readonly" && !strings.HasPrefix(k, "param_") {
				refused = k
				break
			}
		}
		reply := AgentMessage{Type: "query_result", ID: msg.ID, Data: json.RawMessage(`[{"value":1}]`)}
		if refused != "" {
			reply = AgentMessage{Type: "query_error", ID: msg.ID, Error: fmt.Sprintf("Code: 164. DB::Exception: Cannot modify '%s' setting in readonly mode. (READONLY)", refused)}
		}
		if err := ws.WriteJSON(reply); err != nil {
			return
		}
	}
}

func TestReadonlyProfileDropsRefusedSettings(t *testing.T) {
	g, connectionID := newTestGateway(t)
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
	go fakeReadonlyAgent(t, ws, seen)

	settings := map[string]string{"readonly": "1", "max_execution_time": "15", "log_comment": "x", "param_database": "db"}
	result, err := g.ExecuteQueryWithSettingsCtx(context.Background(), connectionID, "SELECT 1", "ro", "", settings, 5*time.Second)
	if err != nil {
		t.Fatalf("query must succeed once refused settings are dropped: %v", err)
	}
	if result == nil {
		t.Fatal("nil result")
	}
	if len(settings) != 4 {
		t.Fatal("the caller's settings map must not be modified")
	}
	close(seen)
	var last map[string]string
	attempts := 0
	for s := range seen {
		last = s
		attempts++
	}
	if attempts != 3 {
		t.Fatalf("want 3 attempts (two refused settings), got %d", attempts)
	}
	if last["readonly"] != "1" || last["param_database"] != "db" || len(last) != 2 {
		t.Fatalf("final attempt must keep readonly and bind parameters only, got %v", last)
	}
}

func TestRefusedSetting(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{errors.New("Code: 164. DB::Exception: Cannot modify 'max_execution_time' setting in readonly mode. (READONLY)"), "max_execution_time"},
		{errors.New("Code: 164. DB::Exception: default: Cannot execute query in readonly mode. (READONLY)"), ""},
		{errors.New("query timeout"), ""},
		{nil, ""},
	}
	for _, test := range tests {
		if got := refusedSetting(test.err); got != test.want {
			t.Fatalf("refusedSetting(%v) = %q, want %q", test.err, got, test.want)
		}
	}
}
