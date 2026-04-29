package copilot

import (
	"encoding/json"
	"strings"

	cula "github.com/git-hulk/cula/pkg"
)

// Copilot CLI emits JSONL events when invoked with `--output-format json`.
// Every line carries a top-level `type` discriminator and an optional `data`
// payload. We only translate the user-visible turn events; lifecycle frames
// (mcp status, tools_updated, message deltas, …) fall through to EventRaw so
// the contract that no line is silently dropped is preserved.
type event struct {
	Type      string          `json:"type"`
	SessionID string          `json:"sessionId"`
	ExitCode  *int            `json:"exitCode"`
	Ephemeral bool            `json:"ephemeral"`
	Data      json.RawMessage `json:"data"`
}

type assistantMessageData struct {
	Content      string        `json:"content"`
	ToolRequests []toolRequest `json:"toolRequests"`
}

type toolRequest struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type toolExecutionStartData struct {
	ToolCallID string         `json:"toolCallId"`
	ToolName   string         `json:"toolName"`
	Arguments  map[string]any `json:"arguments"`
}

type toolExecutionCompleteData struct {
	ToolCallID string `json:"toolCallId"`
	Success    bool   `json:"success"`
	Result     struct {
		Content string `json:"content"`
	} `json:"result"`
	Error string `json:"error"`
}

type assistantReasoningData struct {
	Content string `json:"content"`
}

// ParseEvent converts a Copilot CLI JSONL frame into a cula.Event. It returns
// false when the frame is structurally invalid or carries no user-visible
// payload — callers should fall back to EventRaw in that case.
func ParseEvent(raw json.RawMessage) (cula.Event, bool) {
	var ev event
	if json.Unmarshal(raw, &ev) != nil {
		return cula.Event{}, false
	}
	switch ev.Type {
	case "assistant.message":
		return assistantMessageEvent(ev.Data)
	case "tool.execution_start":
		return toolStartEvent(ev.Data)
	case "tool.execution_complete":
		return toolCompleteEvent(ev.Data)
	case "assistant.reasoning":
		return reasoningEvent(ev.Data)
	case "result":
		return resultEvent(ev)
	}
	return cula.Event{}, false
}

// captureSession returns the sessionId from a `result` frame, which is the
// only frame that carries it as a top-level field.
func captureSession(raw json.RawMessage) string {
	var ev event
	if json.Unmarshal(raw, &ev) != nil {
		return ""
	}
	return ev.SessionID
}

func assistantMessageEvent(data json.RawMessage) (cula.Event, bool) {
	var msg assistantMessageData
	if json.Unmarshal(data, &msg) != nil {
		return cula.Event{}, false
	}
	if text := strings.TrimSpace(msg.Content); text != "" {
		return cula.Event{Type: cula.EventText, Text: text}, true
	}
	return cula.Event{}, false
}

func toolStartEvent(data json.RawMessage) (cula.Event, bool) {
	var d toolExecutionStartData
	if json.Unmarshal(data, &d) != nil || d.ToolName == "" {
		return cula.Event{}, false
	}
	return cula.Event{
		Type: cula.EventToolCall,
		ToolCall: &cula.ToolCall{
			ID:    d.ToolCallID,
			Name:  d.ToolName,
			Input: d.Arguments,
		},
	}, true
}

func toolCompleteEvent(data json.RawMessage) (cula.Event, bool) {
	var d toolExecutionCompleteData
	if json.Unmarshal(data, &d) != nil || d.ToolCallID == "" {
		return cula.Event{}, false
	}
	content := d.Result.Content
	if content == "" {
		content = d.Error
	}
	if content == "" {
		return cula.Event{}, false
	}
	return cula.Event{
		Type: cula.EventToolResult,
		ToolResult: &cula.ToolResult{
			ToolCallID: d.ToolCallID,
			Content:    content,
		},
	}, true
}

func reasoningEvent(data json.RawMessage) (cula.Event, bool) {
	var d assistantReasoningData
	if json.Unmarshal(data, &d) != nil {
		return cula.Event{}, false
	}
	activity := &cula.Activity{Type: cula.ActivityThinking}
	if summary := strings.TrimSpace(d.Content); summary != "" {
		activity.Parameters = []string{summary}
	}
	return cula.Event{Type: cula.EventActivity, Activity: activity}, true
}

func resultEvent(ev event) (cula.Event, bool) {
	out := cula.Event{Type: cula.EventDone}
	if ev.ExitCode != nil && *ev.ExitCode != 0 {
		out.Type = cula.EventError
		out.ExitCode = ev.ExitCode
	}
	return out, true
}
