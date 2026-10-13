package handlers

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	braincore "github.com/caioricciuti/ch-ui/internal/brain"
	"github.com/caioricciuti/ch-ui/internal/brain/tools"
	"github.com/caioricciuti/ch-ui/internal/database"
)

const (
	// brainHistoryTokenBudget bounds the earlier chat turns sent with each
	// request, well under the context window of any model Brain targets.
	brainHistoryTokenBudget = 24000
	// brainMessageMaxChars bounds one earlier message; longer ones keep
	// their head and tail.
	brainMessageMaxChars = 16000
	// brainTurnMaxChars bounds one agent turn (history plus this turn's tool
	// calls and results, about 64k tokens). Past it, the oldest tool outputs
	// of the turn are replaced with a short note.
	brainTurnMaxChars = 256000
)

// estimateTokens is a cheap token estimate: about four characters a token,
// plus a little per-message overhead.
func estimateTokens(s string) int {
	return utf8.RuneCountInString(s)/4 + 4
}

type historyMessage struct {
	Role    string
	Content string
}

// buildBrainHistory turns the stored chat into the messages sent to the
// model: user and assistant turns only, each assistant turn followed by a
// compact summary of its tool results (ids, names, row counts) so follow-ups
// can refer to what was created, newest first until the token budget runs
// out. The last message (the prompt being answered) is always kept. It
// returns the messages in chat order and how many earlier ones were left out.
func buildBrainHistory(history []database.BrainMessage, toolCalls []database.BrainToolCall, budget int) ([]historyMessage, int) {
	summaries := toolSummariesByMessage(toolCalls)

	var all []historyMessage
	for _, msg := range history {
		role := strings.TrimSpace(strings.ToLower(msg.Role))
		if role != "user" && role != "assistant" {
			continue
		}
		if msg.Status == "error" {
			continue
		}
		content := strings.TrimSpace(msg.Content)
		if role == "assistant" {
			if s := summaries[msg.ID]; s != "" {
				if content != "" {
					content += "\n\n"
				}
				content += s
			}
		}
		if content == "" {
			continue
		}
		all = append(all, historyMessage{Role: role, Content: clipMiddle(content, brainMessageMaxChars)})
	}

	if len(all) == 0 {
		return nil, 0
	}
	used := 0
	start := len(all)
	for i := len(all) - 1; i >= 0; i-- {
		cost := estimateTokens(all[i].Content)
		if i < len(all)-1 && used+cost > budget {
			break
		}
		used += cost
		start = i
	}
	kept := all[start:]
	// Keep the conversation starting on a user turn.
	for len(kept) > 1 && kept[0].Role != "user" {
		kept = kept[1:]
		start++
	}
	return kept, start
}

// historyOmittedNote tells the model earlier turns were left out.
func historyOmittedNote(omitted int) string {
	if omitted <= 0 {
		return ""
	}
	return fmt.Sprintf("\n\nNote: %d earlier message(s) in this chat were left out to fit the context window. If the user refers to something you cannot see, ask them to restate it.", omitted)
}

// clipMiddle keeps the head and tail of s when it is longer than max runes.
func clipMiddle(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	half := max / 2
	return string(r[:half]) + "\n[... earlier content trimmed ...]\n" + string(r[len(r)-half:])
}

// toolSummariesByMessage renders, per assistant message, one line per tool
// call with the identifiers a follow-up needs.
func toolSummariesByMessage(calls []database.BrainToolCall) map[string]string {
	lines := map[string][]string{}
	for _, tc := range calls {
		if line := summarizeToolCall(tc); line != "" {
			lines[tc.MessageID] = append(lines[tc.MessageID], line)
		}
	}
	out := make(map[string]string, len(lines))
	for id, ls := range lines {
		out[id] = tools.WrapToolOutput("summary", "Tool calls in this turn:\n"+strings.Join(ls, "\n"))
	}
	return out
}

// summaryKeys are the result fields worth carrying into later turns.
var summaryKeys = map[string]bool{
	"id": true, "name": true, "title": true, "row_count": true, "rows": true,
	"status": true, "started": true, "deleted": true, "declined": true, "cron": true,
	"database": true, "table": true, "error": true,
}

func summarizeToolCall(tc database.BrainToolCall) string {
	var parts []string
	var out map[string]any
	if json.Unmarshal([]byte(tc.OutputJSON), &out) == nil {
		keys := make([]string, 0, len(out))
		for k := range out {
			if summaryKeys[k] || strings.HasSuffix(k, "_id") {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		for _, k := range keys {
			var v string
			switch x := out[k].(type) {
			case string:
				v = x
			case float64, bool:
				v = fmt.Sprint(x)
			default:
				continue
			}
			if len(v) > 120 {
				v = v[:120] + "..."
			}
			parts = append(parts, k+"="+v)
		}
		if _, ok := out["row_count"]; !ok {
			if data, ok := out["data"].([]any); ok {
				parts = append(parts, fmt.Sprintf("row_count=%d", len(data)))
			}
		}
	}
	status := tc.Status
	if status == "" {
		status = "success"
	}
	return fmt.Sprintf("- %s (%s) %s", tc.ToolName, status, strings.Join(parts, " "))
}

// toChatMessages converts history for the tool-calling path.
func toChatMessages(h []historyMessage) []braincore.ChatMessage {
	out := make([]braincore.ChatMessage, 0, len(h))
	for _, m := range h {
		out = append(out, braincore.ChatMessage{Role: m.Role, Content: m.Content})
	}
	return out
}

// toMessages converts history for the plain streaming path.
func toMessages(h []historyMessage) []braincore.Message {
	out := make([]braincore.Message, 0, len(h))
	for _, m := range h {
		out = append(out, braincore.Message{Role: m.Role, Content: m.Content})
	}
	return out
}

// compactTurn keeps one agent turn under maxChars by replacing the oldest
// tool outputs with a short note. Tool messages stay in place because each
// tool call needs its answer; only their content shrinks. The newest tool
// output is never elided.
func compactTurn(msgs []braincore.ChatMessage, maxChars int) {
	total := 0
	lastTool := -1
	for i, m := range msgs {
		total += len(m.Content)
		for _, tc := range m.ToolCalls {
			total += len(tc.Function.Arguments)
		}
		if m.Role == "tool" {
			lastTool = i
		}
	}
	for i := range msgs {
		if total <= maxChars {
			return
		}
		if msgs[i].Role != "tool" || i == lastTool {
			continue
		}
		note := tools.WrapToolOutput(msgs[i].Name, "[output elided to save context; call the tool again if you still need it]")
		if len(note) >= len(msgs[i].Content) {
			continue
		}
		total -= len(msgs[i].Content) - len(note)
		msgs[i].Content = note
	}
}
