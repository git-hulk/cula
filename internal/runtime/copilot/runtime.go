package copilot

import (
	"context"
	"encoding/json"
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
	return cula.RuntimeCopilot
}

func (r *Runtime) Detect(ctx context.Context) (cula.RuntimeInfo, error) {
	binary := iruntime.BinaryPath(r.cfg, "copilot")
	info := iruntime.LookupRuntime(binary, "copilot", cula.RuntimeCopilot, "GitHub Copilot CLI")
	if !info.Installed {
		return info, nil
	}
	if out, err := exec.CommandContext(ctx, binary, "--version").Output(); err == nil {
		re := regexp.MustCompile(`(\d+\.\d+\.\d+)`)
		if m := re.FindStringSubmatch(string(out)); len(m) > 1 {
			info.Version = m[1]
		}
	}
	info.AuthStatus = detectAuth()
	info.Models = []cula.Model{
		{ID: "claude-sonnet-4.5", Name: "Claude Sonnet 4.5"},
		{ID: "claude-sonnet-4", Name: "Claude Sonnet 4"},
		{ID: "claude-opus-4.6", Name: "Claude Opus 4.6"},
		{ID: "gpt-5", Name: "GPT-5"},
		{ID: "gpt-5.4", Name: "GPT-5.4"},
	}
	return info, nil
}

func (r *Runtime) SpawnSession(ctx context.Context, input cula.SessionInput) (cula.Session, error) {
	return newSession(ctx, r, input)
}

func detectAuth() cula.AuthStatus {
	for _, name := range []string{"COPILOT_GITHUB_TOKEN", "GITHUB_TOKEN", "GH_TOKEN"} {
		if strings.TrimSpace(os.Getenv(name)) != "" {
			return cula.AuthLoggedIn
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return cula.AuthLoggedOut
	}
	candidates := []string{
		filepath.Join(home, ".config", "github-copilot", "apps.json"),
		filepath.Join(home, ".config", "github-copilot", "hosts.json"),
	}
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var apps map[string]struct {
			OAuthToken string `json:"oauth_token"`
		}
		if json.Unmarshal(data, &apps) == nil {
			for _, app := range apps {
				if strings.TrimSpace(app.OAuthToken) != "" {
					return cula.AuthLoggedIn
				}
			}
		}
	}
	return cula.AuthLoggedOut
}
