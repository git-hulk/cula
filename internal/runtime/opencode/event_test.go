package opencode

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

func TestParseEventStructuredActivityAndSession(t *testing.T) {
	parser := eventParser{}
	raw := rawJSON(t, `{"sessionID":"open-session","type":"part","part":{"type":"reasoning","text":"**Inspecting repo**\n\nLooking around"}}`)
	if got := parser.captureSession(raw); got != "open-session" {
		t.Fatalf("session id = %q, want open-session", got)
	}
	events := ParseEvent(raw)
	if len(events) != 1 || events[0].Activity == nil || events[0].Activity.Type != aime.ActivityReasoning {
		t.Fatalf("reasoning events = %#v", events)
	}
	if got := events[0].Activity.Parameters; len(got) != 1 || got[0] != "Inspecting repo" {
		t.Fatalf("reasoning params = %#v", got)
	}

	tool := rawJSON(t, `{"type":"part","part":{"type":"tool","tool":"bash","callID":"call-1","input":{"command":"ls"},"state":{"status":"completed","output":"done"}}}`)
	events = ParseEvent(tool)
	if len(events) != 3 {
		t.Fatalf("tool events len = %d, want 3: %#v", len(events), events)
	}
	if events[0].Activity == nil || events[0].Activity.Type != aime.ActivityToolCall {
		t.Fatalf("activity = %#v", events[0].Activity)
	}
	if got := events[0].Activity.Parameters; len(got) != 2 || got[0] != "bash" || got[1] != "ls" {
		t.Fatalf("tool params = %#v", got)
	}
	if events[1].ToolCall == nil || events[1].ToolCall.ID != "call-1" {
		t.Fatalf("tool call = %#v", events[1].ToolCall)
	}
	if events[2].ToolResult == nil || events[2].ToolResult.Content != "done" {
		t.Fatalf("tool result = %#v", events[2].ToolResult)
	}
}
