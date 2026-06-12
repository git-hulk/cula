package pkg

import (
	"encoding/json"
	"time"
)

type EventType string

const (
	EventRaw        EventType = "raw"
	EventText       EventType = "text"
	EventActivity   EventType = "activity"
	EventToolCall   EventType = "tool_call"
	EventToolResult EventType = "tool_result"
	EventState      EventType = "state"
	EventStderr     EventType = "stderr"
	EventError      EventType = "error"
	EventDone       EventType = "done"
)

type ActivityType string

const (
	ActivityThinking   ActivityType = "thinking"
	ActivityToolCall   ActivityType = "tool_call"
	ActivityCommand    ActivityType = "command"
	ActivityNarration  ActivityType = "narration"
	ActivityTokenUsage ActivityType = "token_usage"
)

// Activity describes an in-flight signal from a runtime that isn't itself
// assistant text or a tool call — reasoning, a shell command, narration, or a
// token-usage update. Parameters carries free-form display strings (used by
// thinking/command/tool_call/narration). Data carries a runtime-specific JSON
// payload — currently only ActivityTokenUsage uses it, with the per-runtime
// usage shape preserved verbatim so callers that care can introspect it.
type Activity struct {
	Type       ActivityType    `json:"type"`
	Parameters []string        `json:"parameters,omitempty"`
	Data       json.RawMessage `json:"data,omitempty"`
}

type ToolCall struct {
	ID    string         `json:"id,omitempty"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input,omitempty"`
}

type ToolResult struct {
	ToolCallID string `json:"tool_call_id"`
	Content    string `json:"content"`
}

type State string

const (
	StateRunning   State = "running"
	StateCompleted State = "completed"
	StateFailed    State = "failed"
	StateCanceled  State = "canceled"
)

type Event struct {
	Type       EventType       `json:"type"`
	Runtime    RuntimeKind     `json:"runtime"`
	SessionID  string          `json:"session_id,omitempty"`
	Text       string          `json:"text,omitempty"`
	Activity   *Activity       `json:"activity,omitempty"`
	ToolCall   *ToolCall       `json:"tool_call,omitempty"`
	ToolResult *ToolResult     `json:"tool_result,omitempty"`
	State      State           `json:"state,omitempty"`
	ExitCode   *int            `json:"exit_code,omitempty"`
	Error      string          `json:"error,omitempty"`
	Raw        json.RawMessage `json:"raw,omitempty"`
	Timestamp  time.Time       `json:"timestamp"`
}
