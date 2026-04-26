package pkg

import (
	"context"
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
