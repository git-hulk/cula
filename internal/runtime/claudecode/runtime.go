package claudecode

import (
	"context"
	"encoding/json"
	"os/exec"
	"regexp"
	"strings"

	iruntime "github.com/git-hulk/aime/internal/runtime"
	aime "github.com/git-hulk/aime/pkg"
)

type Runtime struct {
	cfg aime.Config
}

func New(cfg aime.Config) *Runtime {
	return &Runtime{cfg: cfg}
}

func (r *Runtime) Kind() aime.RuntimeKind {
	return aime.RuntimeClaudeCode
}

func (r *Runtime) Detect(ctx context.Context) (aime.RuntimeInfo, error) {
	binary := iruntime.BinaryPath(r.cfg, "claude")
	info := iruntime.LookupRuntime(binary, "claude", aime.RuntimeClaudeCode, "Claude Code")
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
			info.AuthStatus = aime.AuthLoggedIn
		} else {
			info.AuthStatus = aime.AuthLoggedOut
		}
	} else {
		info.AuthStatus = aime.AuthLoggedOut
	}
	info.Models = []aime.Model{
		{ID: "claude-opus-4-7", Name: "Claude Opus 4.7"},
		{ID: "claude-opus-4-6", Name: "Claude Opus 4.6"},
		{ID: "claude-sonnet-4-6", Name: "Claude Sonnet 4.6"},
		{ID: "claude-haiku-4-5", Name: "Claude Haiku 4.5"},
	}
	return info, nil
}

func (r *Runtime) SpawnSession(ctx context.Context, input aime.SessionInput) (aime.Session, error) {
	return newSession(ctx, r, input)
}
