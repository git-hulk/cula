package opencode

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

func TestParseEventStructuredActivityAndSession(t *testing.T) {
	parser := eventParser{}
	raw := rawJSON(t, `{"sessionID":"open-session","type":"part","part":{"type":"reasoning","text":"**Inspecting repo**\n\nLooking around"}}`)
	if got := parser.captureSession(raw); got != "open-session" {
		t.Fatalf("session id = %q, want open-session", got)
	}
	ev, ok := ParseEvent(raw)
	if !ok || ev.Activity == nil || ev.Activity.Type != cula.ActivityThinking {
		t.Fatalf("reasoning event = %#v, %v", ev, ok)
	}
	if got := ev.Activity.Parameters; len(got) != 0 {
		t.Fatalf("reasoning params = %#v, want none", got)
	}

	tool := rawJSON(t, `{"type":"part","part":{"type":"tool","tool":"bash","callID":"call-1","input":{"command":"ls"},"state":{"status":"completed","output":"done"}}}`)
	ev, ok = ParseEvent(tool)
	if !ok || ev.Type != cula.EventToolResult || ev.ToolResult == nil || ev.ToolResult.Content != "done" {
		t.Fatalf("tool result event = %#v, %v", ev, ok)
	}
}

// TestParseSnapshotSummaryRepo replays a real "Read and summary this
// repository" trace captured from `opencode run --format json` and asserts
// the parser classifies every line without dropping any. step_start frames
// and step_finish reasons the parser does not yet recognise (e.g.
// "tool-calls") intentionally fall through to EventRaw — the contract is
// that no line is silently swallowed. Run scripts/capture-events.sh to
// regenerate the snapshot when the upstream stream schema changes.
func TestParseSnapshotSummaryRepo(t *testing.T) {
	lines := readSnapshot(t, "summary_repo.jsonl")
	parser := eventParser{}

	counts := map[cula.EventType]int{}
	var sessionID string
	var sawDone bool

	for i, line := range lines {
		raw := json.RawMessage(line)
		if err := json.Unmarshal(line, new(any)); err != nil {
			t.Fatalf("line %d invalid JSON: %v", i+1, err)
		}

		if id := parser.captureSession(raw); id != "" {
			sessionID = id
		}

		ev, ok := ParseEvent(raw)
		if !ok {
			ev = cula.Event{Type: cula.EventRaw}
		}
		ev.Raw = raw

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
			if ev.ToolCall == nil || ev.ToolCall.Name == "" {
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
		t.Errorf("snapshot does not contain a terminal EventDone (step_finish stop)")
	}

	expected := []cula.EventType{
		cula.EventText,
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
	t.Logf("opencode snapshot: %d lines, counts=%v, session=%s", len(lines), counts, sessionID)
}
