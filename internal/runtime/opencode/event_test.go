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
	ev, ok := ParseEvent(raw)
	if !ok || ev.Activity == nil || ev.Activity.Type != aime.ActivityReasoning {
		t.Fatalf("reasoning event = %#v, %v", ev, ok)
	}
	if got := ev.Activity.Parameters; len(got) != 1 || got[0] != "Inspecting repo" {
		t.Fatalf("reasoning params = %#v", got)
	}

	tool := rawJSON(t, `{"type":"part","part":{"type":"tool","tool":"bash","callID":"call-1","input":{"command":"ls"},"state":{"status":"completed","output":"done"}}}`)
	ev, ok = ParseEvent(tool)
	if !ok || ev.Type != aime.EventToolResult || ev.ToolResult == nil || ev.ToolResult.Content != "done" {
		t.Fatalf("tool result event = %#v, %v", ev, ok)
	}
}
