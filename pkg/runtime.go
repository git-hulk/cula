package pkg

import "context"

type RuntimeKind string

const (
	RuntimeClaudeCode RuntimeKind = "claude-code"
	RuntimeCodex      RuntimeKind = "codex"
	RuntimeOpenCode   RuntimeKind = "opencode"
	RuntimeCopilot    RuntimeKind = "copilot"
	RuntimeHermes     RuntimeKind = "hermes"
)

const (
	AuthLoggedIn     AuthStatus = "logged_in"
	AuthLoggedOut    AuthStatus = "logged_out"
	AuthNotInstalled AuthStatus = "not_installed"
	AuthUnknown      AuthStatus = "unknown"
)

type Runtime interface {
	Kind() RuntimeKind
	Detect(ctx context.Context) (RuntimeInfo, error)
	SpawnSession(ctx context.Context, input SessionInput) (Session, error)
}

type RuntimeInfo struct {
	Kind       RuntimeKind `json:"kind"`
	Name       string      `json:"name"`
	Installed  bool        `json:"installed"`
	Version    string      `json:"version,omitempty"`
	BinaryPath string      `json:"binary_path,omitempty"`
	AuthStatus AuthStatus  `json:"auth_status"`
	Models     []Model     `json:"models,omitempty"`
}

type AuthStatus string

type Model struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type RuntimeOption func(*Config)

type Config struct {
	BinaryPath string
	Env        []string
}

func WithBinaryPath(path string) RuntimeOption {
	return func(c *Config) {
		c.BinaryPath = path
	}
}

func WithEnv(env []string) RuntimeOption {
	return func(c *Config) {
		c.Env = append([]string(nil), env...)
	}
}
