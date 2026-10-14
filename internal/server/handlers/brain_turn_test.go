// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package handlers

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

// claudeTextThenTool is one Anthropic reply with some text and then a tool call.
func claudeTextThenTool(text, id, name, args string) []map[string]any {
	return []map[string]any{
		claudeStart(10, 0, 0),
		{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "text", "text": ""}},
		{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": text}},
		{"type": "content_block_stop", "index": 0},
		{"type": "content_block_start", "index": 1, "content_block": map[string]any{"type": "tool_use", "id": id, "name": name, "input": map[string]any{}}},
		{"type": "content_block_delta", "index": 1, "delta": map[string]any{"type": "input_json_delta", "partial_json": args}},
		{"type": "content_block_stop", "index": 1},
		{"type": "message_delta", "delta": map[string]any{"stop_reason": "tool_use"}, "usage": map[string]any{"output_tokens": 5}},
		{"type": "message_stop"},
	}
}

func lastAssistantContent(t *testing.T, rg *brainRig) string {
	t.Helper()
	msgs, err := rg.db.GetBrainMessages(rg.chatID)
	if err != nil || len(msgs) == 0 {
		t.Fatalf("messages %v %v", msgs, err)
	}
	return msgs[len(msgs)-1].Content
}

func TestBrainIterationSeparator(t *testing.T) {
	cases := map[string]string{"": "", "a": "\n\n", "a\n": "\n", "a\n\n": ""}
	for in, want := range cases {
		if got := iterationSeparator(in); got != want {
			t.Errorf("iterationSeparator(%q) = %q, want %q", in, got, want)
		}
	}
}

// Text from two agent steps is split by a paragraph break, in the stream
// and in the stored message alike.
func TestBrainAgentSeparatesStepText(t *testing.T) {
	rg, _ := newClaudeRig(t,
		claudeTextThenTool("Looking.", "toolu_1", "list_dashboards", `{}`),
		claudeText("Found none.", "end_turn", 10, 5),
	)
	_, text := rg.runTurn(t, "dashboards?")
	if text != "Looking.\n\nFound none." {
		t.Fatalf("streamed %q", text)
	}
	if got := lastAssistantContent(t, rg); got != text {
		t.Errorf("stored %q, streamed %q", got, text)
	}
}

// The daily budget stops a tool loop that runs past it, with a note.
func TestBrainAgentStopsWhenBudgetRunsOutMidTurn(t *testing.T) {
	rg, fc := newClaudeRig(t,
		claudeToolUse("toolu_1", "list_dashboards", `{}`),
		claudeText("never sent", "end_turn", 1, 1),
	)
	if err := rg.db.SetSetting(settingBrainDailyBudget, "500"); err != nil {
		t.Fatal(err)
	}
	done, text := rg.runTurn(t, "dashboards?")
	if done == nil || !strings.Contains(text, "Stopped. You have used the daily Brain budget of 500 tokens") {
		t.Fatalf("done %v text %q", done, text)
	}
	if fc.request(1) != nil {
		t.Error("no model call after the budget ran out")
	}
	if b, _ := done["budget"].(map[string]any); b == nil || b["exceeded"] != true || b["period"] != "day" {
		t.Errorf("done carries the budget state: %v", done["budget"])
	}
}

// Another running turn of the same person counts between steps: its usage
// is not in the ledger yet, but the reservation map has it.
func TestBrainAgentCountsParallelTurns(t *testing.T) {
	rg, fc := newClaudeRig(t,
		claudeToolUse("toolu_1", "list_dashboards", `{}`),
		claudeText("never sent", "end_turn", 1, 1),
	)
	if err := rg.db.SetSetting(settingBrainDailyBudget, "5000"); err != nil {
		t.Fatal(err)
	}
	other := rg.h.live.start(identityOf(rg.session).subject)
	defer rg.h.live.end(other)
	// Under the budget at the start; this turn's first step (2,000 cache
	// writes and some tokens) takes the sum past it.
	rg.h.live.set(other, database.BrainSpend{Tokens: 4000})
	_, text := rg.runTurn(t, "dashboards?")
	if !strings.Contains(text, "Stopped. You have used") {
		t.Fatalf("text %q", text)
	}
	if fc.request(1) != nil {
		t.Error("no second step once the running turns used the budget")
	}
	if got := rg.h.live.others(identityOf(rg.session).subject, other); got.Tokens != 0 {
		t.Errorf("the finished turn left a reservation: %+v", got)
	}
}

// The plain streaming path (no tools) is checked before it starts, with
// the person's running turns counted.
func TestBrainCommunityPathBudgetCheck(t *testing.T) {
	rg := newBrainRig(t, "analyst", []string{"hi"}, nil)
	rg.h.Config.SetProAccessForTest(func() config.ProAccess { return config.ProNone })
	if err := rg.db.SetSetting(settingBrainDailyBudget, "100"); err != nil {
		t.Fatal(err)
	}
	other := rg.h.live.start(identityOf(rg.session).subject)
	rg.h.live.set(other, database.BrainSpend{Tokens: 100})
	resp, err := http.Post(rg.app.URL+"/chats/"+rg.chatID+"/messages/stream", "application/json", strings.NewReader(`{"content":"hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if rg.llm.request(0) != nil {
		t.Error("no provider call when running turns used the budget")
	}
	rg.h.live.end(other)
	rg.stream(t, context.Background(), "hi", func(map[string]any) bool { return true })
	if rg.llm.request(0) == nil {
		t.Error("the turn runs once the other turn ended")
	}
}

// Hitting the step cap ends the turn with a note instead of silently.
func TestBrainAgentToolLimitNote(t *testing.T) {
	replies := make([][]map[string]any, brainMaxIterations)
	for i := range replies {
		replies[i] = claudeToolUse("toolu_1", "list_dashboards", `{}`)
	}
	rg, fc := newClaudeRig(t, replies...)
	_, text := rg.runTurn(t, "loop")
	if !strings.Contains(text, "Stopped after 20 steps") {
		t.Fatalf("text %q", text)
	}
	if fc.request(brainMaxIterations) != nil {
		t.Error("the loop ran past the cap")
	}
}

// A rename and archive made while a turn runs survive the turn's end.
func TestBrainTurnKeepsChatChangesMadeMeanwhile(t *testing.T) {
	rg := newBrainRig(t, "analyst", []string{saveQueryStep, "saved"}, nil)
	rg.stream(t, context.Background(), "save a query", func(ev map[string]any) bool {
		if ev["type"] == "tool_call_pending_approval" {
			if err := rg.db.UpdateBrainChat(rg.chatID, "Renamed", "", "", true, "db", "", ""); err != nil {
				t.Error(err)
			}
			id, _ := ev["approvalId"].(string)
			go func() {
				resp, err := http.Post(rg.app.URL+"/approvals/"+id+"/approve", "application/json", nil)
				if err == nil {
					resp.Body.Close()
				}
			}()
		}
		return true
	})
	chat, err := rg.db.GetBrainChatByIDForUser(rg.chatID, middleware.Actor(rg.session))
	if err != nil || chat == nil {
		t.Fatal(err)
	}
	if chat.Title != "Renamed" || !chat.Archived || chat.ContextDatabase == nil || *chat.ContextDatabase != "db" {
		t.Errorf("chat changes were reverted: %+v", chat)
	}
	if chat.ModelID == nil || *chat.ModelID == "" {
		t.Error("the turn should still record its model")
	}
}

// A new chat still gets a title from its first prompt.
func TestBrainTurnTitlesNewChat(t *testing.T) {
	rg := newBrainRig(t, "analyst", []string{"hi"}, nil)
	rg.stream(t, context.Background(), "count the orders", func(map[string]any) bool { return true })
	chat, _ := rg.db.GetBrainChatByIDForUser(rg.chatID, middleware.Actor(rg.session))
	if chat == nil || chat.Title != "count the orders" {
		t.Fatalf("chat %+v", chat)
	}
}

// When an approve lands just before the timeout, the stored approval wins:
// the call runs and the row is not overwritten with timeout.
func TestBrainApprovalTimeoutLosesToStoredApprove(t *testing.T) {
	rg := newBrainRig(t, "analyst", nil, nil)
	prev := brainApprovalTimeout
	brainApprovalTimeout = 20 * time.Millisecond
	t.Cleanup(func() { brainApprovalTimeout = prev })
	actor := middleware.Actor(rg.session)

	for _, tc := range []struct {
		name       string
		approve    bool
		wantRun    bool
		wantStatus string
	}{
		{"approved first", true, true, "approved"},
		{"nobody decided", false, false, "timeout"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := "appr-" + strings.ReplaceAll(tc.name, " ", "-")
			if err := rg.db.CreateBrainApproval(id, rg.chatID, "m", "c", "create_saved_query", "{}", actor); err != nil {
				t.Fatal(err)
			}
			ch := rg.h.registerApproval(id)
			if tc.approve {
				// The route stored the decision; its signal had not landed.
				if ok, err := rg.db.DecideBrainApprovalAs(id, "approved", actor); err != nil || !ok {
					t.Fatal(ok, err)
				}
			}
			decision, abandoned := rg.h.awaitApproval(context.Background(), id, ch)
			if abandoned || decision.Approved != tc.wantRun {
				t.Fatalf("decision %+v abandoned %v", decision, abandoned)
			}
			a, _ := rg.db.GetBrainApprovalByID(id)
			if a == nil || a.Status != tc.wantStatus {
				t.Fatalf("stored %+v", a)
			}
			if ok, _ := rg.db.DecideBrainApprovalAs(id, "approved", actor); ok {
				t.Error("a settled approval must not be decided again")
			}
		})
	}
}
