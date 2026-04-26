package codex

import (
	"encoding/json"
	"fmt"
	"strings"

	iruntime "github.com/git-hulk/aime/internal/runtime"
	aime "github.com/git-hulk/aime/pkg"
)

type eventParser struct{}

func ParseEvent(raw json.RawMessage) (aime.Event, bool) {
	return eventParser{}.parse(raw)
}

type event struct {
	Method string `json:"method"`
	Params params `json:"params"`
}

type params struct {
	Item *item `json:"item"`
	Turn *turn `json:"turn"`
}

type turn struct {
	Status string `json:"status"`
}

type item struct {
	ID        string           `json:"id"`
	Type      string           `json:"type"`
	Name      string           `json:"name"`
	Text      string           `json:"text"`
	Command   string           `json:"command"`
	ExitCode  *float64         `json:"exitCode"`
	Arguments string           `json:"arguments"`
	Summary   []reasoningBlock `json:"summary"`
	Input     map[string]any   `json:"input"`
	Metadata  map[string]any   `json:"metadata"`
}

type reasoningBlock struct {
	Text string `json:"text"`
}

func (p eventParser) parse(raw json.RawMessage) (aime.Event, bool) {
	var ev event
	if json.Unmarshal(raw, &ev) != nil || ev.Method == "" {
		return aime.Event{}, false
	}
	switch ev.Method {
	case "turn/completed":
		if ev.Params.Turn == nil || ev.Params.Turn.Status == "" {
			return aime.Event{}, false
		}
		return aime.Event{Type: aime.EventDone}, true
	case "item/started":
		if ev.Params.Item != nil {
			return p.itemEvent(ev.Params.Item, false)
		}
	case "item/completed":
		if ev.Params.Item != nil {
			return p.completedItemEvent(ev.Params.Item)
		}
	}
	return aime.Event{}, false
}

func (p eventParser) completedItemEvent(item *item) (aime.Event, bool) {
	if item.Type == "agentMessage" && strings.TrimSpace(item.Text) != "" {
		return aime.Event{Type: aime.EventText, Text: item.Text}, true
	}
	return p.itemEvent(item, true)
}

func (eventParser) itemEvent(item *item, completed bool) (aime.Event, bool) {
	switch item.Type {
	case "reasoning":
		if !completed {
			return aime.Event{Type: aime.EventActivity, Activity: &aime.Activity{Type: aime.ActivityThinking}}, true
		}
		for _, summary := range item.Summary {
			if text := reasoningSummary(summary.Text); text != "" {
				return aime.Event{Type: aime.EventActivity, Activity: &aime.Activity{Type: aime.ActivityReasoning, Parameters: []string{text}}}, true
			}
		}
	case "functionCall", "tool_call":
		input := item.Input
		if input == nil {
			input = iruntime.ParseArguments(item.Arguments)
		}
		if !completed {
			return aime.Event{Type: aime.EventToolCall, ToolCall: &aime.ToolCall{ID: item.ID, Name: item.Name, Input: input}}, true
		}
		return aime.Event{}, false
	case "commandExecution":
		params := []string{}
		if item.Command != "" {
			params = append(params, iruntime.Truncate(item.Command, 160))
		}
		if completed && item.ExitCode != nil {
			params = append(params, fmt.Sprintf("%d", int(*item.ExitCode)))
		}
		return aime.Event{Type: aime.EventActivity, Activity: &aime.Activity{Type: aime.ActivityCommand, Parameters: params}}, true
	}
	return aime.Event{}, false
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
