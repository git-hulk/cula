package opencode

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
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
	return aime.RuntimeOpenCode
}

func (r *Runtime) Detect(ctx context.Context) (aime.RuntimeInfo, error) {
	binary := iruntime.BinaryPath(r.cfg, "opencode")
	info := iruntime.LookupRuntime(binary, "opencode", aime.RuntimeOpenCode, "OpenCode")
	if !info.Installed {
		return info, nil
	}
	if out, err := exec.CommandContext(ctx, binary, "--version").Output(); err == nil {
		re := regexp.MustCompile(`(\d+\.\d+\.\d+)`)
		if m := re.FindStringSubmatch(string(out)); len(m) > 1 {
			info.Version = m[1]
		}
	}
	home, _ := os.UserHomeDir()
	authPath := filepath.Join(home, ".local", "share", "opencode", "auth.json")
	if data, err := os.ReadFile(authPath); err == nil && len(data) > 2 {
		info.AuthStatus = aime.AuthLoggedIn
	} else {
		info.AuthStatus = aime.AuthLoggedOut
	}
	if out, err := exec.CommandContext(ctx, binary, "models").Output(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			id := strings.TrimSpace(line)
			if id != "" && strings.Contains(id, "/") {
				info.Models = append(info.Models, aime.Model{ID: id, Name: id})
			}
		}
	}
	return info, nil
}

func (r *Runtime) SpawnSession(ctx context.Context, input aime.SessionInput) (aime.Session, error) {
	return newSession(ctx, r, input)
}
