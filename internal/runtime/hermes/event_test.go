package hermes

import (
	"encoding/json"
	"testing"

	cula "github.com/git-hulk/cula/pkg"
)

func rawJSON(t *testing.T, s string) json.RawMessage {
	t.Helper()
	var raw json.RawMessage
	if err := json.Unmarshal([]byte(s), &raw); err != nil {
		t.Fatalf("invalid test JSON: %v", err)
	}
	return raw
}

func TestParseEventCoreShapes(t *testing.T) {
	text := rawJSON(t, `{"event":"message.delta","run_id":"run_1","delta":"hello"}`)
	ev, ok := ParseEvent(text)
	if !ok || ev.Type != cula.EventText || ev.Text != "hello" {
		t.Fatalf("message.delta = %#v, %v", ev, ok)
	}

	toolStart := rawJSON(t, `{"event":"tool.started","run_id":"run_1","tool":"terminal","input":{"command":"pwd"}}`)
	ev, ok = ParseEvent(toolStart)
	if !ok || ev.Type != cula.EventToolCall || ev.ToolCall == nil || ev.ToolCall.Name != "terminal" {
		t.Fatalf("tool.started = %#v, %v", ev, ok)
	}
	if got, _ := ev.ToolCall.Input["command"].(string); got != "pwd" {
		t.Fatalf("tool input command = %q, want pwd", got)
	}

	toolDone := rawJSON(t, `{"event":"tool.completed","run_id":"run_1","tool":"terminal","output":"/repo\n"}`)
	ev, ok = ParseEvent(toolDone)
	if !ok || ev.Type != cula.EventToolResult || ev.ToolResult == nil || ev.ToolResult.Content != "/repo\n" {
		t.Fatalf("tool.completed = %#v, %v", ev, ok)
	}

	reasoning := rawJSON(t, `{"event":"reasoning.available","run_id":"run_1","text":"thinking"}`)
	ev, ok = ParseEvent(reasoning)
	if !ok || ev.Type != cula.EventActivity || ev.Activity == nil || ev.Activity.Type != cula.ActivityThinking {
		t.Fatalf("reasoning.available = %#v, %v", ev, ok)
	}

	completed := rawJSON(t, `{"event":"run.completed","run_id":"run_1","output":"final answer"}`)
	ev, ok = ParseEvent(completed)
	if !ok || ev.Type != cula.EventText || ev.Text != "final answer" {
		t.Fatalf("run.completed = %#v, %v", ev, ok)
	}

	failed := rawJSON(t, `{"event":"run.failed","run_id":"run_1","error":"boom"}`)
	ev, ok = ParseEvent(failed)
	if !ok || ev.Type != cula.EventError || ev.Error != "boom" {
		t.Fatalf("run.failed = %#v, %v", ev, ok)
	}
}

func TestCaptureSession(t *testing.T) {
	raw := rawJSON(t, `{"event":"run.completed","run_id":"run_abc","output":"done"}`)
	if got := captureSession(raw); got != "run_abc" {
		t.Fatalf("captureSession = %q, want run_abc", got)
	}
}
