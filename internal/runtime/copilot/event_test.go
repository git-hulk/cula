package copilot

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

func TestParseEventCoreShapes(t *testing.T) {
	msg := rawJSON(t, `{"type":"assistant.message","data":{"messageId":"m1","content":"hello","toolRequests":[]}}`)
	ev, ok := ParseEvent(msg)
	if !ok || ev.Type != cula.EventText || ev.Text != "hello" {
		t.Fatalf("assistant.message = %#v, %v", ev, ok)
	}

	start := rawJSON(t, `{"type":"tool.execution_start","data":{"toolCallId":"call-1","toolName":"bash","arguments":{"command":"ls"}}}`)
	ev, ok = ParseEvent(start)
	if !ok || ev.Type != cula.EventToolCall || ev.ToolCall == nil || ev.ToolCall.Name != "bash" || ev.ToolCall.ID != "call-1" {
		t.Fatalf("tool.execution_start = %#v, %v", ev, ok)
	}
	if got, _ := ev.ToolCall.Input["command"].(string); got != "ls" {
		t.Fatalf("tool input command = %q, want ls", got)
	}

	complete := rawJSON(t, `{"type":"tool.execution_complete","data":{"toolCallId":"call-1","success":true,"result":{"content":"hello\n"}}}`)
	ev, ok = ParseEvent(complete)
	if !ok || ev.Type != cula.EventToolResult || ev.ToolResult == nil || ev.ToolResult.ToolCallID != "call-1" || ev.ToolResult.Content != "hello\n" {
		t.Fatalf("tool.execution_complete = %#v, %v", ev, ok)
	}

	res := rawJSON(t, `{"type":"result","sessionId":"s-1","exitCode":0}`)
	if id := captureSession(res); id != "s-1" {
		t.Fatalf("captureSession = %q, want s-1", id)
	}
	ev, ok = ParseEvent(res)
	if !ok || ev.Type != cula.EventDone {
		t.Fatalf("result(success) = %#v, %v", ev, ok)
	}

	failed := rawJSON(t, `{"type":"result","sessionId":"s-1","exitCode":2}`)
	ev, ok = ParseEvent(failed)
	if !ok || ev.Type != cula.EventError || ev.ExitCode == nil || *ev.ExitCode != 2 {
		t.Fatalf("result(failure) = %#v, %v", ev, ok)
	}
}

// TestParseSnapshotEchoHello replays a real Copilot CLI trace captured from
// `copilot -p ... --output-format json --stream off`. Lifecycle frames such
// as session.* and assistant.turn_* intentionally fall through to EventRaw —
// the contract is that no line is silently dropped, and the user-visible
// frames (text, tool calls/results, result) decode into structured events.
func TestParseSnapshotEchoHello(t *testing.T) {
	lines := readSnapshot(t, "echo_hello.jsonl")

	counts := map[cula.EventType]int{}
	var sessionID string
	var sawDone bool

	for i, line := range lines {
		raw := json.RawMessage(line)
		if err := json.Unmarshal(line, new(any)); err != nil {
			t.Fatalf("line %d invalid JSON: %v", i+1, err)
		}

		if id := captureSession(raw); id != "" {
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
		case cula.EventDone:
			sawDone = true
		}
	}

	if sessionID == "" {
		t.Errorf("captureSession never extracted a session id from the snapshot")
	}
	if !sawDone {
		t.Errorf("snapshot does not contain a terminal EventDone (result frame)")
	}

	expected := []cula.EventType{
		cula.EventText,
		cula.EventToolCall,
		cula.EventToolResult,
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
	t.Logf("copilot snapshot: %d lines, counts=%v, session=%s", len(lines), counts, sessionID)
}
