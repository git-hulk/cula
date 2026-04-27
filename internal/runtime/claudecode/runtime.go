package claudecode

import (
	"context"
	"encoding/json"
	"os/exec"
	"regexp"
	"strings"

	iruntime "github.com/git-hulk/cula/internal/runtime"
	cula "github.com/git-hulk/cula/pkg"
)

type Runtime struct {
	cfg cula.Config
}

func New(cfg cula.Config) *Runtime {
	return &Runtime{cfg: cfg}
}

func (r *Runtime) Kind() cula.RuntimeKind {
	return cula.RuntimeClaudeCode
}

func (r *Runtime) Detect(ctx context.Context) (cula.RuntimeInfo, error) {
	binary := iruntime.BinaryPath(r.cfg, "claude")
	info := iruntime.LookupRuntime(binary, "claude", cula.RuntimeClaudeCode, "Claude Code")
	if !info.Installed {
		return info, nil
	}
	if out, err := exec.CommandContext(ctx, binary, "--version").Output(); err == nil {
		re := regexp.MustCompile(`(\d+\.\d+\.\d+)`)
		if m := re.FindStringSubmatch(string(out)); len(m) > 1 {
			info.Version = m[1]
		}
	}
	if out, err := exec.CommandContext(ctx, binary, "auth", "status").Output(); err == nil {
		var status struct {
			LoggedIn bool `json:"loggedIn"`
		}
		if json.Unmarshal([]byte(strings.TrimSpace(string(out))), &status) == nil && status.LoggedIn {
			info.AuthStatus = cula.AuthLoggedIn
		} else {
			info.AuthStatus = cula.AuthLoggedOut
		}
	} else {
		info.AuthStatus = cula.AuthLoggedOut
	}
	info.Models = []cula.Model{
		{ID: "claude-opus-4-7", Name: "Claude Opus 4.7"},
		{ID: "claude-opus-4-6", Name: "Claude Opus 4.6"},
		{ID: "claude-sonnet-4-6", Name: "Claude Sonnet 4.6"},
		{ID: "claude-haiku-4-5", Name: "Claude Haiku 4.5"},
	}
	return info, nil
}

func (r *Runtime) SpawnSession(ctx context.Context, input cula.SessionInput) (cula.Session, error) {
	return newSession(ctx, r, input)
}
