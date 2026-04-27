package opencode

import (
	"encoding/json"

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
		return cula.Event{Type: cula.EventActivity, Activity: &cula.Activity{Type: cula.ActivityThinking}}, true
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

