package pkg

import (
	"context"
	"encoding/json"
	"time"
)

type PermissionMode string

const (
	PermissionDefault PermissionMode = ""
	PermissionNever   PermissionMode = "never"
	PermissionSkip    PermissionMode = "skip"
)

type SandboxMode string

const (
	SandboxDefault          SandboxMode = ""
	SandboxWorkspaceWrite   SandboxMode = "workspace-write"
	SandboxDangerFullAccess SandboxMode = "danger-full-access"
)

type SessionInput struct {
	Runtime    RuntimeKind       `json:"runtime"`
	Prompt     string            `json:"prompt"`
	WorkingDir string            `json:"working_dir,omitempty"`
	Model      string            `json:"model,omitempty"`
	SessionID  string            `json:"session_id,omitempty"`
	Env        []string          `json:"env,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	Permission PermissionMode    `json:"permission,omitempty"`
	Sandbox    SandboxMode       `json:"sandbox,omitempty"`
}

type Session interface {
	Send(ctx context.Context, prompt string) error
	Events() <-chan Event
	Cancel(ctx context.Context) error
}

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
	ActivityThinking  ActivityType = "thinking"
	ActivityReasoning ActivityType = "reasoning"
	ActivityToolCall  ActivityType = "tool_call"
	ActivityCommand   ActivityType = "command"
)

type Activity struct {
	Type       ActivityType `json:"type"`
	Parameters []string     `json:"parameters,omitempty"`
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
