package opencode

import (
	"encoding/json"
	"fmt"
	"strings"

	iruntime "github.com/git-hulk/cula/internal/runtime"
	cula "github.com/git-hulk/cula/pkg"
)

type eventParser struct{}

func ParseEvent(raw json.RawMessage) (cula.Event, bool) {
	return eventParser{}.parse(raw)
}

type event struct {
	Type      string `json:"type"`
	SessionID string `json:"sessionID"`
	Part      *part  `json:"part"`
}

type part struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Text     string         `json:"text"`
	Reason   string         `json:"reason"`
	Tool     string         `json:"tool"`
	CallID   string         `json:"callID"`
	Input    map[string]any `json:"input"`
	State    *state         `json:"state"`
	Metadata map[string]any `json:"metadata"`
}

type state struct {
	Status string         `json:"status"`
	Title  string         `json:"title"`
	Input  map[string]any `json:"input"`
	Output string         `json:"output"`
	Error  string         `json:"error"`
}

func (eventParser) captureSession(raw json.RawMessage) string {
	var ev struct {
		SessionID string `json:"sessionID"`
	}
	if json.Unmarshal(raw, &ev) == nil {
		return ev.SessionID
	}
	return ""
}

func (p eventParser) parse(raw json.RawMessage) (cula.Event, bool) {
	var ev event
	if json.Unmarshal(raw, &ev) != nil {
		return cula.Event{}, false
	}
	if ev.Type == "step_finish" {
		return p.stepFinishEvent(ev)
	}
	if ev.Part == nil {
		return cula.Event{}, false
	}
	switch ev.Part.Type {
	case "text":
		if ev.Part.Text != "" {
			return cula.Event{Type: cula.EventText, Text: ev.Part.Text}, true
		}
	case "reasoning":
		activity := &cula.Activity{Type: cula.ActivityThinking}
		if summary := reasoningSummary(ev.Part.Text); summary != "" {
			activity.Parameters = []string{summary}
		}
		return cula.Event{Type: cula.EventActivity, Activity: activity}, true
	case "tool":
		return p.toolEvent(ev.Part)
	}
	return cula.Event{}, false
}

func (eventParser) stepFinishEvent(ev event) (cula.Event, bool) {
	if ev.Part == nil {
		return cula.Event{Type: cula.EventDone}, true
	}
	switch ev.Part.Reason {
	case "", "stop", "length", "error":
		return cula.Event{Type: cula.EventDone}, true
	default:
		return cula.Event{}, false
	}
}

// reasoningSummary returns the bolded heading on the first line of an
// opencode reasoning part's text (e.g. "Inspecting the repo" from
// "**Inspecting the repo**\n\n..."), or "" if none is usable.
func reasoningSummary(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if i := strings.IndexAny(text, "\n\r"); i >= 0 {
		text = text[:i]
	}
	text = strings.TrimSpace(strings.Trim(strings.TrimSpace(text), "*_"))
	if text == "" {
		return ""
	}
	return iruntime.Truncate(text, 80)
}

// ParseTokenUsage extracts a token_usage activity from an opencode step_finish
// frame when one carries a tokens payload. It returns false for frames that
// don't carry tokens — callers should ignore the negative return rather than
// treat it as an error. Both mid-turn (reason="tool-calls") and terminal
// (reason="stop") step_finish frames emit a tokens snapshot, so surfacing
// every one gives users a running view of context use across the turn.
func ParseTokenUsage(raw json.RawMessage) (cula.Event, bool) {
	var ev struct {
		Type string `json:"type"`
		Part *struct {
			Tokens json.RawMessage `json:"tokens"`
		} `json:"part"`
	}
	if json.Unmarshal(raw, &ev) != nil || ev.Type != "step_finish" || ev.Part == nil || len(ev.Part.Tokens) == 0 {
		return cula.Event{}, false
	}
	var tokens tokenStats
	if json.Unmarshal(ev.Part.Tokens, &tokens) != nil {
		return cula.Event{}, false
	}
	return cula.Event{Type: cula.EventActivity, Activity: &cula.Activity{
		Type:       cula.ActivityTokenUsage,
		Parameters: []string{formatTokens(tokens)},
		Data:       ev.Part.Tokens,
	}}, true
}

type tokenStats struct {
	Input     int `json:"input"`
	Output    int `json:"output"`
	Reasoning int `json:"reasoning"`
	Total     int `json:"total"`
	Cache     struct {
		Read  int `json:"read"`
		Write int `json:"write"`
	} `json:"cache"`
}

func formatTokens(t tokenStats) string {
	parts := []string{fmt.Sprintf("tokens %d", t.Total), fmt.Sprintf("in %d", t.Input), fmt.Sprintf("out %d", t.Output)}
	if t.Reasoning > 0 {
		parts = append(parts, fmt.Sprintf("reasoning %d", t.Reasoning))
	}
	if t.Cache.Read > 0 || t.Cache.Write > 0 {
		parts = append(parts, fmt.Sprintf("cache r=%d w=%d", t.Cache.Read, t.Cache.Write))
	}
	return strings.Join(parts, " · ")
}

func (eventParser) toolEvent(part *part) (cula.Event, bool) {
	toolName := part.Tool
	if toolName == "" && part.State != nil {
		toolName = part.State.Title
	}
	input := part.Input
	if input == nil && part.State != nil {
		input = part.State.Input
	}
	if part.State != nil && (part.State.Status == "completed" || part.State.Status == "error") {
		content := part.State.Output
		if content == "" {
			content = part.State.Error
		}
		if content != "" {
			return cula.Event{Type: cula.EventToolResult, ToolResult: &cula.ToolResult{ToolCallID: part.CallID, Content: content}}, true
		}
	}
	return cula.Event{Type: cula.EventToolCall, ToolCall: &cula.ToolCall{ID: part.CallID, Name: toolName, Input: input}}, true
}
