package claudecode

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
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

func readSnapshot(t *testing.T, name string) [][]byte {
	t.Helper()
	path := filepath.Join("testdata", name)
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open snapshot %s: %v", path, err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	var lines [][]byte
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		lines = append(lines, append([]byte(nil), line...))
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read snapshot %s: %v", path, err)
	}
	if len(lines) == 0 {
		t.Fatalf("snapshot %s has no events", path)
	}
	return lines
}

func TestParseEventTextToolResultAndSession(t *testing.T) {
	parser := eventParser{}
	init := rawJSON(t, `{"type":"system","subtype":"init","session_id":"claude-session"}`)
	if got := parser.captureSession(init); got != "claude-session" {
		t.Fatalf("session id = %q, want claude-session", got)
	}

	text := rawJSON(t, `{"type":"assistant","message":{"content":[{"type":"text","text":"hello"}]}}`)
	ev, ok := ParseEvent(text)
	if !ok || ev.Type != cula.EventText || ev.Text != "hello" {
		t.Fatalf("text event = %#v, %v", ev, ok)
	}

	tool := rawJSON(t, `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tool-1","name":"Bash","input":{"command":"go test ./..."}}]}}`)
	ev, ok = ParseEvent(tool)
	if !ok || ev.Type != cula.EventToolCall || ev.ToolCall == nil || ev.ToolCall.ID != "tool-1" {
		t.Fatalf("tool event = %#v, %v", ev, ok)
	}

	result := rawJSON(t, `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tool-1","content":"ok"}]}}`)
	ev, ok = ParseEvent(result)
	if !ok || ev.Type != cula.EventToolResult || ev.ToolResult == nil || ev.ToolResult.Content != "ok" {
		t.Fatalf("tool result event = %#v, %v", ev, ok)
	}
}

// TestParseSnapshotSummaryRepo replays a real "Read and summary this
// repository" trace captured from the claude CLI and asserts the parser
// classifies every line without dropping any. Run scripts/capture-events.sh
// to regenerate the snapshot when the upstream stream-json schema changes.
func TestParseSnapshotSummaryRepo(t *testing.T) {
	lines := readSnapshot(t, "summary_repo.jsonl")
	parser := eventParser{}

	counts := map[cula.EventType]int{}
	var sessionID string
	var sawDone bool

	for i, line := range lines {
		raw := json.RawMessage(line)
		// Round-trip JSON validation — the runtime would error on any
		// undecodable line, so we hold the snapshot to the same standard.
		if err := json.Unmarshal(line, new(any)); err != nil {
			t.Fatalf("line %d invalid JSON: %v", i+1, err)
		}

		if id := parser.captureSession(raw); id != "" {
			sessionID = id
		}

		ev, ok := ParseEvent(raw)
		if !ok {
			// Mirror session.readStdout: every unrecognised line is still
			// surfaced as EventRaw so it isn't lost downstream.
			ev = cula.Event{Type: cula.EventRaw}
		}
		ev.Raw = raw

		// Each snapshot line must produce exactly one event. The Raw payload
		// must be preserved verbatim so consumers can inspect the original.
		if !bytes.Equal(ev.Raw, line) {
			t.Fatalf("line %d: Raw payload was rewritten", i+1)
		}
		if ev.Type == "" {
			t.Fatalf("line %d: emitted event has empty type", i+1)
		}

		counts[ev.Type]++

		switch ev.Type {
		case cula.EventText:
			if ev.Text == "" {
				t.Fatalf("line %d: EventText with empty Text\nraw=%s", i+1, line)
			}
		case cula.EventToolCall:
			if ev.ToolCall == nil || ev.ToolCall.Name == "" || ev.ToolCall.ID == "" {
				t.Fatalf("line %d: EventToolCall missing fields: %#v\nraw=%s", i+1, ev.ToolCall, line)
			}
		case cula.EventToolResult:
			if ev.ToolResult == nil || ev.ToolResult.ToolCallID == "" || ev.ToolResult.Content == "" {
				t.Fatalf("line %d: EventToolResult missing fields: %#v\nraw=%s", i+1, ev.ToolResult, line)
			}
		case cula.EventActivity:
			if ev.Activity == nil || ev.Activity.Type == "" {
				t.Fatalf("line %d: EventActivity missing fields: %#v\nraw=%s", i+1, ev.Activity, line)
			}
		case cula.EventDone:
			sawDone = true
		}
	}

	if sessionID == "" {
		t.Errorf("captureSession never extracted a session id from the snapshot")
	}
	if !sawDone {
		t.Errorf("snapshot does not contain a terminal EventDone")
	}

	// Categories the snapshot is known to exercise. Failing here means the
	// parser regressed — re-capture and review before adjusting these.
	expected := []cula.EventType{
		cula.EventText,
		cula.EventToolCall,
		cula.EventToolResult,
		cula.EventActivity,
		cula.EventDone,
	}
	for _, typ := range expected {
		if counts[typ] == 0 {
			t.Errorf("expected at least one %s event, got counts=%v", typ, counts)
		}
	}

	total := 0
	for _, c := range counts {
		total += c
	}
	if total != len(lines) {
		t.Fatalf("event count mismatch: parsed %d events from %d lines (counts=%v)", total, len(lines), counts)
	}
	t.Logf("claude-code snapshot: %d lines, counts=%v, session=%s", len(lines), counts, sessionID)
}
