package codex

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"

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
	return cula.RuntimeCodex
}

func (r *Runtime) Detect(ctx context.Context) (cula.RuntimeInfo, error) {
	binary := iruntime.BinaryPath(r.cfg, "codex")
	info := iruntime.LookupRuntime(binary, "codex", cula.RuntimeCodex, "Codex CLI")
	if !info.Installed {
		return info, nil
	}
	if out, err := exec.CommandContext(ctx, binary, "--version").Output(); err == nil {
		re := regexp.MustCompile(`codex-cli\s+(\S+)`)
		if m := re.FindStringSubmatch(string(out)); len(m) > 1 {
			info.Version = m[1]
		}
	}
	home, _ := os.UserHomeDir()
	if data, err := os.ReadFile(filepath.Join(home, ".codex", "auth.json")); err == nil {
		var auth struct {
			Tokens struct {
				AccessToken string `json:"access_token"`
			} `json:"tokens"`
		}
		if json.Unmarshal(data, &auth) == nil && auth.Tokens.AccessToken != "" {
			info.AuthStatus = cula.AuthLoggedIn
		} else {
			info.AuthStatus = cula.AuthLoggedOut
		}
	} else {
		info.AuthStatus = cula.AuthLoggedOut
	}
	if data, err := os.ReadFile(filepath.Join(home, ".codex", "models_cache.json")); err == nil {
		var cache struct {
			Models []struct {
				Slug        string `json:"slug"`
				DisplayName string `json:"display_name"`
			} `json:"models"`
		}
		if json.Unmarshal(data, &cache) == nil {
			for _, m := range cache.Models {
				info.Models = append(info.Models, cula.Model{ID: m.Slug, Name: m.DisplayName})
			}
		}
	}
	return info, nil
}

func (r *Runtime) SpawnSession(ctx context.Context, input cula.SessionInput) (cula.Session, error) {
	return newSession(ctx, r, input)
}
