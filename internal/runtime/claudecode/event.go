package claudecode

import (
	"encoding/json"
	"strings"

	iruntime "github.com/git-hulk/cula/internal/runtime"
	cula "github.com/git-hulk/cula/pkg"
)

type eventParser struct{}

func ParseEvent(raw json.RawMessage) (cula.Event, bool) {
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

func (p eventParser) parse(raw json.RawMessage) (cula.Event, bool) {
	var ev event
	if json.Unmarshal(raw, &ev) != nil {
		return cula.Event{}, false
	}
	switch ev.Type {
	case "result":
		return cula.Event{Type: cula.EventDone}, true
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
			return cula.Event{Type: cula.EventText, Text: text}, true
		}
	}
	for _, b := range contentBlocks(ev.Content) {
		if out, ok := toolResultBlockEvent(b); ok {
			return out, true
		}
	}
	return cula.Event{}, false
}

func (eventParser) assistantEvent(ev event) (cula.Event, bool) {
	if ev.Message == nil {
		return cula.Event{}, false
	}
	for _, b := range ev.Message.Content {
		switch b.Type {
		case "thinking":
			return cula.Event{Type: cula.EventActivity, Activity: &cula.Activity{Type: cula.ActivityThinking}}, true
		case "tool_use":
			return cula.Event{Type: cula.EventToolCall, ToolCall: &cula.ToolCall{ID: b.ID, Name: b.Name, Input: b.Input}}, true
		}
	}
	for _, b := range ev.Message.Content {
		if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
			return cula.Event{Type: cula.EventText, Text: b.Text}, true
		}
	}
	return cula.Event{}, false
}

func (eventParser) toolResultEvent(ev event) (cula.Event, bool) {
	if ev.Message == nil {
		return cula.Event{}, false
	}
	for _, b := range ev.Message.Content {
		if out, ok := toolResultBlockEvent(b); ok {
			return out, true
		}
	}
	return cula.Event{}, false
}

func toolResultBlockEvent(b block) (cula.Event, bool) {
	if b.Type != "tool_result" || b.ToolUseID == "" {
		return cula.Event{}, false
	}
	text := b.Content.AsText()
	if text == "" {
		return cula.Event{}, false
	}
	return cula.Event{Type: cula.EventToolResult, ToolResult: &cula.ToolResult{ToolCallID: b.ToolUseID, Content: text}}, true
}

func contentBlocks(content iruntime.RawValue) []block {
	if raw := content.Raw(); len(raw) == 0 || raw[0] != '[' {
		return nil
	}
	var blocks []block
	_ = json.Unmarshal(content.Raw(), &blocks)
	return blocks
}
