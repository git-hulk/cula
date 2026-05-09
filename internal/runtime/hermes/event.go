package hermes

import (
	"encoding/json"
	"fmt"
	"strings"

	cula "github.com/git-hulk/cula/pkg"
)

type event struct {
	Event   string         `json:"event"`
	RunID   string         `json:"run_id"`
	Delta   string         `json:"delta"`
	Text    string         `json:"text"`
	Output  string         `json:"output"`
	Error   string         `json:"error"`
	Tool    string         `json:"tool"`
	Input   map[string]any `json:"input"`
	Result  any            `json:"result"`
	Content string         `json:"content"`
}

func ParseEvent(raw json.RawMessage) (cula.Event, bool) {
	var ev event
	if json.Unmarshal(raw, &ev) != nil || ev.Event == "" {
		return cula.Event{}, false
	}

	switch ev.Event {
	case "message.delta":
		if ev.Delta == "" {
			return cula.Event{}, false
		}
		return cula.Event{Type: cula.EventText, Text: ev.Delta}, true
	case "tool.started":
		name := strings.TrimSpace(ev.Tool)
		if name == "" {
			name = "tool"
		}
		return cula.Event{Type: cula.EventToolCall, ToolCall: &cula.ToolCall{Name: name, Input: ev.Input}}, true
	case "tool.completed":
		content := ev.Output
		if content == "" {
			content = ev.Content
		}
		if content == "" && ev.Result != nil {
			content = fmt.Sprint(ev.Result)
		}
		return cula.Event{Type: cula.EventToolResult, ToolResult: &cula.ToolResult{Content: content}}, true
	case "reasoning.available":
		params := []string{}
		if strings.TrimSpace(ev.Text) != "" {
			params = append(params, ev.Text)
		}
		return cula.Event{Type: cula.EventActivity, Activity: &cula.Activity{Type: cula.ActivityThinking, Parameters: params}}, true
	case "approval.request":
		return cula.Event{Type: cula.EventActivity, Activity: &cula.Activity{Type: cula.ActivityNarration, Parameters: []string{"waiting for approval"}}}, true
	case "run.completed":
		if ev.Output != "" {
			return cula.Event{Type: cula.EventText, Text: ev.Output}, true
		}
		return cula.Event{Type: cula.EventDone}, true
	case "run.failed":
		return cula.Event{Type: cula.EventError, Error: ev.Error}, true
	case "run.cancelled":
		return cula.Event{Type: cula.EventState, State: cula.StateCanceled}, true
	}
	return cula.Event{}, false
}

func captureSession(raw json.RawMessage) string {
	var ev event
	if json.Unmarshal(raw, &ev) != nil {
		return ""
	}
	return ev.RunID
}
