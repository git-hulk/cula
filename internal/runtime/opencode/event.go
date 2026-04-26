package opencode

import (
	"encoding/json"
	"strings"

	iruntime "github.com/git-hulk/aime/internal/runtime"
	aime "github.com/git-hulk/aime/pkg"
)

type eventParser struct{}

func ParseEvent(raw json.RawMessage) []aime.Event {
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

func (p eventParser) parse(raw json.RawMessage) []aime.Event {
	var ev event
	if json.Unmarshal(raw, &ev) != nil {
		return nil
	}
	if ev.Type == "step_finish" {
		return p.stepFinishEvents(ev)
	}
	if ev.Part == nil {
		return nil
	}
	switch ev.Part.Type {
	case "text":
		if ev.Part.Text != "" {
			return []aime.Event{{Type: aime.EventText, Text: ev.Part.Text}}
		}
	case "reasoning":
		params := []string{}
		if summary := reasoningSummary(ev.Part.Text); summary != "" {
			params = append(params, summary)
		}
		return []aime.Event{{Type: aime.EventActivity, Activity: &aime.Activity{Type: aime.ActivityReasoning, Parameters: params}}}
	case "tool":
		return p.toolEvents(ev.Part)
	}
	return nil
}

func (eventParser) stepFinishEvents(ev event) []aime.Event {
	if ev.Part == nil {
		return []aime.Event{{Type: aime.EventDone}}
	}
	switch ev.Part.Reason {
	case "", "stop", "length", "error":
		return []aime.Event{{Type: aime.EventDone}}
	default:
		return nil
	}
}

func (eventParser) toolEvents(part *part) []aime.Event {
	toolName := part.Tool
	if toolName == "" && part.State != nil {
		toolName = part.State.Title
	}
	input := part.Input
	if input == nil && part.State != nil {
		input = part.State.Input
	}
	params := []string{toolName}
	if summary := iruntime.StringMapSummary(input); summary != "" {
		params = append(params, summary)
	}
	out := []aime.Event{
		{Type: aime.EventActivity, Activity: &aime.Activity{Type: aime.ActivityToolCall, Parameters: params}},
		{Type: aime.EventToolCall, ToolCall: &aime.ToolCall{ID: part.CallID, Name: toolName, Input: input}},
	}
	if part.State != nil && (part.State.Status == "completed" || part.State.Status == "error") {
		content := part.State.Output
		if content == "" {
			content = part.State.Error
		}
		if content != "" {
			out = append(out, aime.Event{Type: aime.EventToolResult, ToolResult: &aime.ToolResult{ToolCallID: part.CallID, Content: content}})
		}
	}
	return out
}

func reasoningSummary(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if i := strings.IndexAny(text, "\n\r"); i >= 0 {
		text = text[:i]
	}
	text = strings.TrimSpace(strings.Trim(strings.TrimSpace(text), "*_"))
	return iruntime.Truncate(text, 80)
}
