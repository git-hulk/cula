package claudecode

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

func (p eventParser) parse(raw json.RawMessage) []aime.Event {
	var ev event
	if json.Unmarshal(raw, &ev) != nil {
		return nil
	}
	var out []aime.Event
	switch ev.Type {
	case "result":
		return []aime.Event{{Type: aime.EventDone}}
	case "assistant":
		out = append(out, p.assistantEvents(ev)...)
	case "user":
		out = append(out, p.toolResultEvents(ev)...)
	}
	if ev.Role == "assistant" {
		if text, ok := ev.Content.AsString(); ok && strings.TrimSpace(text) != "" {
			out = append(out, aime.Event{Type: aime.EventText, Text: text})
		}
	}
	for _, b := range contentBlocks(ev.Content) {
		out = append(out, toolResultBlockEvent(b)...)
	}
	return out
}

func (eventParser) assistantEvents(ev event) []aime.Event {
	if ev.Message == nil {
		return nil
	}
	var out []aime.Event
	hasToolUse := false
	for _, b := range ev.Message.Content {
		switch b.Type {
		case "thinking":
			out = append(out, aime.Event{Type: aime.EventActivity, Activity: &aime.Activity{Type: aime.ActivityThinking}})
		case "tool_use":
			hasToolUse = true
			params := []string{b.Name}
			if summary := iruntime.StringMapSummary(b.Input); summary != "" {
				params = append(params, summary)
			}
			out = append(out,
				aime.Event{Type: aime.EventActivity, Activity: &aime.Activity{Type: aime.ActivityToolCall, Parameters: params}},
				aime.Event{Type: aime.EventToolCall, ToolCall: &aime.ToolCall{ID: b.ID, Name: b.Name, Input: b.Input}},
			)
		}
	}
	if hasToolUse {
		return out
	}
	for _, b := range ev.Message.Content {
		if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
			out = append(out, aime.Event{Type: aime.EventText, Text: b.Text})
		}
	}
	return out
}

func (eventParser) toolResultEvents(ev event) []aime.Event {
	if ev.Message == nil {
		return nil
	}
	var out []aime.Event
	for _, b := range ev.Message.Content {
		out = append(out, toolResultBlockEvent(b)...)
	}
	return out
}

func toolResultBlockEvent(b block) []aime.Event {
	if b.Type != "tool_result" || b.ToolUseID == "" {
		return nil
	}
	text := b.Content.AsText()
	if text == "" {
		return nil
	}
	return []aime.Event{{Type: aime.EventToolResult, ToolResult: &aime.ToolResult{ToolCallID: b.ToolUseID, Content: text}}}
}

func contentBlocks(content iruntime.RawValue) []block {
	if raw := content.Raw(); len(raw) == 0 || raw[0] != '[' {
		return nil
	}
	var blocks []block
	_ = json.Unmarshal(content.Raw(), &blocks)
	return blocks
}
