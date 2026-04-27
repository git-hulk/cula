package opencode

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
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
	return cula.RuntimeOpenCode
}

func (r *Runtime) Detect(ctx context.Context) (cula.RuntimeInfo, error) {
	binary := iruntime.BinaryPath(r.cfg, "opencode")
	info := iruntime.LookupRuntime(binary, "opencode", cula.RuntimeOpenCode, "OpenCode")
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
		info.AuthStatus = cula.AuthLoggedIn
	} else {
		info.AuthStatus = cula.AuthLoggedOut
	}
	if out, err := exec.CommandContext(ctx, binary, "models").Output(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			id := strings.TrimSpace(line)
			if id != "" && strings.Contains(id, "/") {
				info.Models = append(info.Models, cula.Model{ID: id, Name: id})
			}
		}
	}
	return info, nil
}

func (r *Runtime) SpawnSession(ctx context.Context, input cula.SessionInput) (cula.Session, error) {
	return newSession(ctx, r, input)
}
