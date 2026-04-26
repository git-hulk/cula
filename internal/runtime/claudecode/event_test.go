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
	events := ParseEvent(text)
	if len(events) != 1 || events[0].Type != aime.EventText || events[0].Text != "hello" {
		t.Fatalf("text events = %#v", events)
	}

	tool := rawJSON(t, `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tool-1","name":"Bash","input":{"command":"go test ./..."}}]}}`)
	events = ParseEvent(tool)
	if len(events) != 2 {
		t.Fatalf("tool events len = %d, want 2: %#v", len(events), events)
	}
	if events[0].Activity == nil || events[0].Activity.Type != aime.ActivityToolCall {
		t.Fatalf("activity = %#v", events[0].Activity)
	}
	if got := events[0].Activity.Parameters; len(got) != 2 || got[0] != "Bash" || got[1] != "go test ./..." {
		t.Fatalf("activity params = %#v", got)
	}
	if events[1].ToolCall == nil || events[1].ToolCall.ID != "tool-1" {
		t.Fatalf("tool call = %#v", events[1].ToolCall)
	}

	result := rawJSON(t, `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tool-1","content":"ok"}]}}`)
	events = ParseEvent(result)
	if len(events) != 1 || events[0].ToolResult == nil || events[0].ToolResult.Content != "ok" {
		t.Fatalf("tool result events = %#v", events)
	}
}
