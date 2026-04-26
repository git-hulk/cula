package claudecode

import (
	"encoding/json"
	"testing"

	aime "github.com/git-hulk/aime/pkg"
)

func rawJSON(t *testing.T, s string) json.RawMessage {
	t.Helper()
	var raw json.RawMessage
	if err := json.Unmarshal([]byte(s), &raw); err != nil {
		t.Fatalf("invalid test JSON: %v", err)
	}
	return raw
}

func TestParseEventTextToolResultAndSession(t *testing.T) {
	parser := eventParser{}
	init := rawJSON(t, `{"type":"system","subtype":"init","session_id":"claude-session"}`)
	if got := parser.captureSession(init); got != "claude-session" {
		t.Fatalf("session id = %q, want claude-session", got)
	}

	text := rawJSON(t, `{"type":"assistant","message":{"content":[{"type":"text","text":"hello"}]}}`)
	ev, ok := ParseEvent(text)
	if !ok || ev.Type != aime.EventText || ev.Text != "hello" {
		t.Fatalf("text event = %#v, %v", ev, ok)
	}

	tool := rawJSON(t, `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tool-1","name":"Bash","input":{"command":"go test ./..."}}]}}`)
	ev, ok = ParseEvent(tool)
	if !ok || ev.Type != aime.EventToolCall || ev.ToolCall == nil || ev.ToolCall.ID != "tool-1" {
		t.Fatalf("tool event = %#v, %v", ev, ok)
	}

	result := rawJSON(t, `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tool-1","content":"ok"}]}}`)
	ev, ok = ParseEvent(result)
	if !ok || ev.Type != aime.EventToolResult || ev.ToolResult == nil || ev.ToolResult.Content != "ok" {
		t.Fatalf("tool result event = %#v, %v", ev, ok)
	}
}
