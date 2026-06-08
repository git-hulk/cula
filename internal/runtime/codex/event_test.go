package codex

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

func TestParseEventStructuredActivity(t *testing.T) {
	started := rawJSON(t, `{"method":"item/started","params":{"item":{"id":"call-1","type":"functionCall","name":"apply_patch","arguments":"{\"path\":\"types.go\"}"}}}`)
	ev, ok := ParseEvent(started)
	if !ok || ev.Type != cula.EventToolCall || ev.ToolCall == nil || ev.ToolCall.Name != "apply_patch" {
		t.Fatalf("tool event = %#v, %v", ev, ok)
	}

	commandDone := rawJSON(t, `{"method":"item/completed","params":{"item":{"type":"commandExecution","command":"go test ./...","exitCode":0}}}`)
	ev, ok = ParseEvent(commandDone)
	if !ok || ev.Activity == nil || ev.Activity.Type != cula.ActivityCommand {
		t.Fatalf("command event = %#v, %v", ev, ok)
	}
	if got := ev.Activity.Parameters; len(got) != 2 || got[0] != "go test ./..." || got[1] != "0" {
		t.Fatalf("command params = %#v", got)
	}

	done := rawJSON(t, `{"method":"turn/completed","params":{"turn":{"status":"completed"}}}`)
	ev, ok = ParseEvent(done)
	if !ok || ev.Type != cula.EventDone {
		t.Fatalf("done event = %#v, %v", ev, ok)
	}
}

func TestParseEventAgentMessagePhase(t *testing.T) {
	commentary := rawJSON(t, `{"method":"item/completed","params":{"item":{"id":"m1","type":"agentMessage","text":"I'm going to map the repo first.","phase":"commentary"}}}`)
	ev, ok := ParseEvent(commentary)
	if !ok || ev.Type != cula.EventActivity || ev.Activity == nil || ev.Activity.Type != cula.ActivityNarration {
		t.Fatalf("commentary event = %#v, %v", ev, ok)
	}
	if got := ev.Activity.Parameters; len(got) != 1 || got[0] != "I'm going to map the repo first." {
		t.Fatalf("commentary params = %#v", got)
	}

	final := rawJSON(t, `{"method":"item/completed","params":{"item":{"id":"m2","type":"agentMessage","text":"All done.","phase":"final_answer"}}}`)
	ev, ok = ParseEvent(final)
	if !ok || ev.Type != cula.EventText || ev.Text != "All done." {
		t.Fatalf("final_answer event = %#v, %v", ev, ok)
	}

	legacy := rawJSON(t, `{"method":"item/completed","params":{"item":{"id":"m3","type":"agentMessage","text":"Legacy reply."}}}`)
	ev, ok = ParseEvent(legacy)
	if !ok || ev.Type != cula.EventText || ev.Text != "Legacy reply." {
		t.Fatalf("legacy agentMessage event = %#v, %v", ev, ok)
	}
}

func TestParseEventAgentMessageDeltaIgnored(t *testing.T) {
	delta := rawJSON(t, `{"method":"item/agentMessage/delta","params":{"delta":"I","itemId":"msg_1","threadId":"t","turnId":"u"}}`)
	if ev, ok := ParseEvent(delta); ok {
		t.Fatalf("expected delta to be dropped, got %#v", ev)
	}
}

func TestParseEventTokenUsage(t *testing.T) {
	usage := rawJSON(t, `{"method":"thread/tokenUsage/updated","params":{"threadId":"t","turnId":"u","tokenUsage":{"total":{"totalTokens":229934,"inputTokens":226261,"cachedInputTokens":164096,"outputTokens":3673,"reasoningOutputTokens":1687},"last":{"totalTokens":35775,"inputTokens":35219,"cachedInputTokens":18304,"outputTokens":556,"reasoningOutputTokens":516},"modelContextWindow":258400}}}`)
	ev, ok := ParseEvent(usage)
	if !ok || ev.Type != cula.EventActivity || ev.Activity == nil || ev.Activity.Type != cula.ActivityNarration {
		t.Fatalf("tokenUsage event = %#v, %v", ev, ok)
	}
	want := "tokens 229934/258400 · in 226261 · cached 164096 · out 3673 · reasoning 1687"
	if got := ev.Activity.Parameters; len(got) != 1 || got[0] != want {
		t.Fatalf("tokenUsage params = %#v, want [%q]", got, want)
	}

	missing := rawJSON(t, `{"method":"thread/tokenUsage/updated","params":{"threadId":"t","turnId":"u"}}`)
	ev, ok = ParseEvent(missing)
	if !ok || ev.Type != cula.EventRaw {
		t.Fatalf("tokenUsage without body falls through to EventRaw, got %#v, ok=%v", ev, ok)
	}
}

func TestParseEventUnknownMethod(t *testing.T) {
	// Unknown method with a real body — surface as EventRaw so callers
	// can still inspect the payload.
	withBody := rawJSON(t, `{"method":"something/new","params":{"hello":"world"}}`)
	ev, ok := ParseEvent(withBody)
	if !ok || ev.Type != cula.EventRaw {
		t.Fatalf("unknown method with body should be EventRaw, got %#v, ok=%v", ev, ok)
	}

	// Empty body shapes are dropped to keep the stream tidy.
	for _, raw := range []string{
		`{"method":"something/new","params":{}}`,
		`{"method":"something/new","params":null}`,
		`{"method":"something/new","params":[]}`,
		`{"method":"something/new"}`,
	} {
		if ev, ok := ParseEvent(rawJSON(t, raw)); ok {
			t.Fatalf("expected drop for empty body %s, got %#v", raw, ev)
		}
	}
}

// TestParseSnapshotSummaryRepo replays a real "Read and summary this
// repository" trace captured from the codex app-server JSON-RPC stream. The
// parser must (a) classify every method it recognises, (b) drop only the
// streaming item/agentMessage/delta fragments (their consolidated text comes
// back on item/completed), and (c) surface every other unrecognised method
// as EventRaw rather than silently swallowing it. Run
// scripts/capture-events.sh to regenerate the snapshot when the upstream
// schema changes.
func TestParseSnapshotSummaryRepo(t *testing.T) {
	lines := readSnapshot(t, "summary_repo.jsonl")

	counts := map[cula.EventType]int{}
	activityCounts := map[cula.ActivityType]int{}
	var dropped int
	var sawDone bool

	for i, line := range lines {
		raw := json.RawMessage(line)
		if err := json.Unmarshal(line, new(any)); err != nil {
			t.Fatalf("line %d invalid JSON: %v", i+1, err)
		}

		ev, ok := ParseEvent(raw)
		if !ok {
			dropped++
			continue
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
		case cula.EventActivity:
			if ev.Activity == nil || ev.Activity.Type == "" {
				t.Fatalf("line %d: EventActivity missing fields: %#v\nraw=%s", i+1, ev.Activity, line)
			}
			activityCounts[ev.Activity.Type]++
		case cula.EventToolResult:
			if ev.ToolResult == nil || ev.ToolResult.Content == "" {
				t.Fatalf("line %d: EventToolResult missing fields: %#v\nraw=%s", i+1, ev.ToolResult, line)
			}
		case cula.EventDone:
			sawDone = true
		}
	}

	if !sawDone {
		t.Errorf("snapshot does not contain a terminal EventDone (turn/completed)")
	}

	expected := []cula.EventType{
		cula.EventText,
		cula.EventActivity,
		cula.EventDone,
		cula.EventRaw,
	}
	for _, typ := range expected {
		if counts[typ] == 0 {
			t.Errorf("expected at least one %s event, got counts=%v", typ, counts)
		}
	}

	expectedActivities := []cula.ActivityType{
		cula.ActivityThinking,
		cula.ActivityCommand,
		cula.ActivityNarration,
	}
	for _, a := range expectedActivities {
		if activityCounts[a] == 0 {
			t.Errorf("expected at least one %s activity, got counts=%v", a, activityCounts)
		}
	}

	total := 0
	for _, c := range counts {
		total += c
	}
	if total+dropped != len(lines) {
		t.Fatalf("event count mismatch: parsed %d events + %d drops from %d lines (counts=%v)", total, dropped, len(lines), counts)
	}
	if dropped == 0 {
		t.Errorf("expected some events to be dropped (item/agentMessage/delta), got zero")
	}
	t.Logf("codex snapshot: %d lines, %d dropped, counts=%v, activities=%v", len(lines), dropped, counts, activityCounts)
}
