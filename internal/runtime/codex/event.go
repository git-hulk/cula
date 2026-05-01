package codex

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
	switch ev.Method {
	case "turn/completed":
		if ev.Params.Turn == nil || ev.Params.Turn.Status == "" {
			return cula.Event{}, false
		}
		return cula.Event{Type: cula.EventDone}, true
	case "item/started":
		if ev.Params.Item != nil {
			return p.itemEvent(ev.Params.Item, false)
		}
	case "item/completed":
		if ev.Params.Item != nil {
			return p.completedItemEvent(ev.Params.Item)
		}
	}
	return cula.Event{}, false
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
