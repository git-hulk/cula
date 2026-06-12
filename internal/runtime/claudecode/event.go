package claudecode

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

// ParseTokenUsage extracts a token_usage activity from a claude-code stream
// line when one is present. It returns false for lines that don't carry usage
// — callers should ignore the negative return rather than treat it as an
// error. We only surface usage from the terminal `result` frame, which
// consolidates per-step usage into a modelUsage map alongside the cost; the
// per-assistant-message usage fields are intentionally skipped to avoid
// flooding the transcript with one row per tool call.
func ParseTokenUsage(raw json.RawMessage) (cula.Event, bool) {
	var ev struct {
		Type       string          `json:"type"`
		ModelUsage json.RawMessage `json:"modelUsage"`
	}
	if json.Unmarshal(raw, &ev) != nil || ev.Type != "result" || len(ev.ModelUsage) == 0 {
		return cula.Event{}, false
	}
	var usage map[string]modelUsageEntry
	if json.Unmarshal(ev.ModelUsage, &usage) != nil || len(usage) == 0 {
		return cula.Event{}, false
	}
	return cula.Event{Type: cula.EventActivity, Activity: &cula.Activity{
		Type:       cula.ActivityTokenUsage,
		Parameters: []string{formatModelUsage(usage)},
		Data:       ev.ModelUsage,
	}}, true
}

type modelUsageEntry struct {
	CacheCreationInputTokens int     `json:"cacheCreationInputTokens"`
	CacheReadInputTokens     int     `json:"cacheReadInputTokens"`
	ContextWindow            int     `json:"contextWindow"`
	CostUSD                  float64 `json:"costUSD"`
	InputTokens              int     `json:"inputTokens"`
	MaxOutputTokens          int     `json:"maxOutputTokens"`
	OutputTokens             int     `json:"outputTokens"`
}

func formatModelUsage(usage map[string]modelUsageEntry) string {
	// Pick a stable model entry so the formatted line is deterministic for
	// tests; if more than one model contributed we fall back to summing.
	if len(usage) == 1 {
		for model, u := range usage {
			return formatUsageLine(model, u)
		}
	}
	var summed modelUsageEntry
	models := make([]string, 0, len(usage))
	for model, u := range usage {
		models = append(models, model)
		summed.CacheCreationInputTokens += u.CacheCreationInputTokens
		summed.CacheReadInputTokens += u.CacheReadInputTokens
		summed.InputTokens += u.InputTokens
		summed.OutputTokens += u.OutputTokens
		summed.CostUSD += u.CostUSD
	}
	return formatUsageLine(strings.Join(models, "+"), summed)
}

func formatUsageLine(model string, u modelUsageEntry) string {
	parts := []string{fmt.Sprintf("in %d", u.InputTokens), fmt.Sprintf("out %d", u.OutputTokens)}
	if u.CacheReadInputTokens > 0 {
		parts = append(parts, fmt.Sprintf("cache_read %d", u.CacheReadInputTokens))
	}
	if u.CacheCreationInputTokens > 0 {
		parts = append(parts, fmt.Sprintf("cache_write %d", u.CacheCreationInputTokens))
	}
	if u.ContextWindow > 0 {
		parts = append(parts, fmt.Sprintf("ctx %d", u.ContextWindow))
	}
	if u.CostUSD > 0 {
		parts = append(parts, fmt.Sprintf("$%.4f", u.CostUSD))
	}
	if model != "" {
		return model + " · " + strings.Join(parts, " · ")
	}
	return strings.Join(parts, " · ")
}

func contentBlocks(content iruntime.RawValue) []block {
	if raw := content.Raw(); len(raw) == 0 || raw[0] != '[' {
		return nil
	}
	var blocks []block
	_ = json.Unmarshal(content.Raw(), &blocks)
	return blocks
}
