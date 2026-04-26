package claudecode

import (
	"encoding/json"
	"strings"

	iruntime "github.com/git-hulk/aime/internal/runtime"
	aime "github.com/git-hulk/aime/pkg"
)

type eventParser struct{}

func ParseEvent(raw json.RawMessage) (aime.Event, bool) {
	return eventParser{}.parse(raw)
}

type event struct {
	Type      string            `json:"type"`
	SubType   string            `json:"subtype"`
	Role      string            `json:"role"`
	SessionID string            `json:"session_id"`
	Message   *message          `json:"message"`
	Content   iruntime.RawValue `json:"content"`
}

type message struct {
	Content []block `json:"content"`
}

type block struct {
	Type      string            `json:"type"`
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Text      string            `json:"text"`
	Input     map[string]any    `json:"input"`
	ToolUseID string            `json:"tool_use_id"`
	Content   iruntime.RawValue `json:"content"`
}

func (eventParser) captureSession(raw json.RawMessage) string {
	var ev struct {
		Type      string `json:"type"`
		SubType   string `json:"subtype"`
		SessionID string `json:"session_id"`
	}
	if json.Unmarshal(raw, &ev) == nil && ev.SessionID != "" &&
		(ev.Type == "init" || (ev.Type == "system" && ev.SubType == "init")) {
		return ev.SessionID
	}
	return ""
}

func (p eventParser) parse(raw json.RawMessage) (aime.Event, bool) {
	var ev event
	if json.Unmarshal(raw, &ev) != nil {
		return aime.Event{}, false
	}
	switch ev.Type {
	case "result":
		return aime.Event{Type: aime.EventDone}, true
	case "assistant":
		if out, ok := p.assistantEvent(ev); ok {
			return out, true
		}
	case "user":
		if out, ok := p.toolResultEvent(ev); ok {
			return out, true
		}
	}
	if ev.Role == "assistant" {
		if text, ok := ev.Content.AsString(); ok && strings.TrimSpace(text) != "" {
			return aime.Event{Type: aime.EventText, Text: text}, true
		}
	}
	for _, b := range contentBlocks(ev.Content) {
		if out, ok := toolResultBlockEvent(b); ok {
			return out, true
		}
	}
	return aime.Event{}, false
}

func (eventParser) assistantEvent(ev event) (aime.Event, bool) {
	if ev.Message == nil {
		return aime.Event{}, false
	}
	for _, b := range ev.Message.Content {
		switch b.Type {
		case "thinking":
			return aime.Event{Type: aime.EventActivity, Activity: &aime.Activity{Type: aime.ActivityThinking}}, true
		case "tool_use":
			return aime.Event{Type: aime.EventToolCall, ToolCall: &aime.ToolCall{ID: b.ID, Name: b.Name, Input: b.Input}}, true
		}
	}
	for _, b := range ev.Message.Content {
		if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
			return aime.Event{Type: aime.EventText, Text: b.Text}, true
		}
	}
	return aime.Event{}, false
}

func (eventParser) toolResultEvent(ev event) (aime.Event, bool) {
	if ev.Message == nil {
		return aime.Event{}, false
	}
	for _, b := range ev.Message.Content {
		if out, ok := toolResultBlockEvent(b); ok {
			return out, true
		}
	}
	return aime.Event{}, false
}

func toolResultBlockEvent(b block) (aime.Event, bool) {
	if b.Type != "tool_result" || b.ToolUseID == "" {
		return aime.Event{}, false
	}
	text := b.Content.AsText()
	if text == "" {
		return aime.Event{}, false
	}
	return aime.Event{Type: aime.EventToolResult, ToolResult: &aime.ToolResult{ToolCallID: b.ToolUseID, Content: text}}, true
}

func contentBlocks(content iruntime.RawValue) []block {
	if raw := content.Raw(); len(raw) == 0 || raw[0] != '[' {
		return nil
	}
	var blocks []block
	_ = json.Unmarshal(content.Raw(), &blocks)
	return blocks
}
