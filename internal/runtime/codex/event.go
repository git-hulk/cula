package codex

import (
	"bytes"
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
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type params struct {
	Item       *item       `json:"item"`
	Turn       *turn       `json:"turn"`
	TokenUsage *tokenUsage `json:"tokenUsage"`
}

type turn struct {
	Status string `json:"status"`
}

type tokenUsage struct {
	Total              tokenStats `json:"total"`
	Last               tokenStats `json:"last"`
	ModelContextWindow int        `json:"modelContextWindow"`
}

type tokenStats struct {
	TotalTokens           int `json:"totalTokens"`
	InputTokens           int `json:"inputTokens"`
	CachedInputTokens     int `json:"cachedInputTokens"`
	OutputTokens          int `json:"outputTokens"`
	ReasoningOutputTokens int `json:"reasoningOutputTokens"`
}

type item struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	Name      string         `json:"name"`
	Text      string         `json:"text"`
	Phase     string         `json:"phase"`
	Command   string         `json:"command"`
	ExitCode  *float64       `json:"exitCode"`
	Arguments string         `json:"arguments"`
	Input     map[string]any `json:"input"`
	Metadata  map[string]any `json:"metadata"`
}

func (p eventParser) parse(raw json.RawMessage) (cula.Event, bool) {
	var ev event
	if json.Unmarshal(raw, &ev) != nil || ev.Method == "" {
		return cula.Event{}, false
	}
	if ev.Method == "item/agentMessage/delta" {
		// Codex streams the in-progress assistant reply token-by-token via
		// this method and then republishes the consolidated text once on
		// item/completed (phase=final_answer or commentary). We surface only
		// the completed payload so downstream consumers (TUI, Slack) don't
		// have to coalesce partial fragments, so ignore the streaming deltas.
		return cula.Event{}, false
	}
	var pp params
	_ = json.Unmarshal(ev.Params, &pp)
	switch ev.Method {
	case "turn/completed":
		if pp.Turn != nil && pp.Turn.Status != "" {
			return cula.Event{Type: cula.EventDone}, true
		}
	case "item/started":
		if pp.Item != nil {
			if out, ok := p.itemEvent(pp.Item, false); ok {
				return out, true
			}
		}
	case "item/completed":
		if pp.Item != nil {
			if out, ok := p.completedItemEvent(pp.Item); ok {
				return out, true
			}
		}
	case "thread/tokenUsage/updated":
		if pp.TokenUsage != nil {
			return cula.Event{Type: cula.EventActivity, Activity: &cula.Activity{
				Type:       cula.ActivityNarration,
				Parameters: []string{formatTokenUsage(pp.TokenUsage)},
			}}, true
		}
	}
	// Fall-through: an unknown method, or a known method whose body
	// didn't match our schema. Surface it as EventRaw so callers can
	// still inspect the payload — but drop entries with no body to keep
	// the stream tidy.
	if isEmptyBody(ev.Params) {
		return cula.Event{}, false
	}
	return cula.Event{Type: cula.EventRaw}, true
}

func isEmptyBody(p json.RawMessage) bool {
	trimmed := bytes.TrimSpace(p)
	switch string(trimmed) {
	case "", "null", "{}", "[]":
		return true
	}
	return false
}

func formatTokenUsage(u *tokenUsage) string {
	parts := []string{fmt.Sprintf("tokens %d", u.Total.TotalTokens)}
	if u.ModelContextWindow > 0 {
		parts[0] = fmt.Sprintf("tokens %d/%d", u.Total.TotalTokens, u.ModelContextWindow)
	}
	parts = append(parts, fmt.Sprintf("in %d", u.Total.InputTokens))
	if u.Total.CachedInputTokens > 0 {
		parts = append(parts, fmt.Sprintf("cached %d", u.Total.CachedInputTokens))
	}
	parts = append(parts, fmt.Sprintf("out %d", u.Total.OutputTokens))
	if u.Total.ReasoningOutputTokens > 0 {
		parts = append(parts, fmt.Sprintf("reasoning %d", u.Total.ReasoningOutputTokens))
	}
	return strings.Join(parts, " · ")
}

func (p eventParser) completedItemEvent(item *item) (cula.Event, bool) {
	if item.Type == "agentMessage" && strings.TrimSpace(item.Text) != "" {
		// Codex tags mid-turn preamble/progress narration as "commentary"
		// and the terminal reply as "final_answer". Surface commentary as
		// an activity narration row so it groups under the assistant
		// banner and gets cleared at turn end alongside other progress
		// indicators, instead of stacking inside the final answer bubble.
		if item.Phase == "commentary" {
			return cula.Event{Type: cula.EventActivity, Activity: &cula.Activity{Type: cula.ActivityNarration, Parameters: []string{item.Text}}}, true
		}
		return cula.Event{Type: cula.EventText, Text: item.Text}, true
	}
	return p.itemEvent(item, true)
}

func (eventParser) itemEvent(item *item, completed bool) (cula.Event, bool) {
	switch item.Type {
	case "reasoning":
		if completed {
			return cula.Event{}, false
		}
		return cula.Event{Type: cula.EventActivity, Activity: &cula.Activity{Type: cula.ActivityThinking}}, true
	case "functionCall", "tool_call":
		input := item.Input
		if input == nil {
			input = iruntime.ParseArguments(item.Arguments)
		}
		if !completed {
			return cula.Event{Type: cula.EventToolCall, ToolCall: &cula.ToolCall{ID: item.ID, Name: item.Name, Input: input}}, true
		}
		return cula.Event{}, false
	case "commandExecution":
		params := []string{}
		if item.Command != "" {
			params = append(params, iruntime.Truncate(item.Command, 160))
		}
		if completed && item.ExitCode != nil {
			params = append(params, fmt.Sprintf("%d", int(*item.ExitCode)))
		}
		return cula.Event{Type: cula.EventActivity, Activity: &cula.Activity{Type: cula.ActivityCommand, Parameters: params}}, true
	}
	return cula.Event{}, false
}
