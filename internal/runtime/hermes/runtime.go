package hermes

import (
	"context"
	"encoding/json"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"time"

	iruntime "github.com/git-hulk/cula/internal/runtime"
	cula "github.com/git-hulk/cula/pkg"
)

const (
	defaultBaseURL = "http://127.0.0.1:8642"
)

type Runtime struct {
	cfg cula.Config
}

func New(cfg cula.Config) *Runtime {
	return &Runtime{cfg: cfg}
}

func (r *Runtime) Kind() cula.RuntimeKind {
	return cula.RuntimeHermes
}

func (r *Runtime) Detect(ctx context.Context) (cula.RuntimeInfo, error) {
	binary := iruntime.BinaryPath(r.cfg, "hermes")
	info := iruntime.LookupRuntime(binary, "hermes", cula.RuntimeHermes, "Hermes Agent")
	if info.Installed {
		if out, err := exec.CommandContext(ctx, binary, "--version").Output(); err == nil {
			re := regexp.MustCompile(`(\d+\.\d+\.\d+)`)
			if m := re.FindStringSubmatch(string(out)); len(m) > 1 {
				info.Version = m[1]
			} else {
				info.Version = strings.TrimSpace(string(out))
			}
		}
	}

	models, status := r.detectAPI(ctx)
	if status != cula.AuthUnknown {
		info.Installed = true
		info.AuthStatus = status
		info.Models = models
		if info.BinaryPath == "" {
			info.Name = "Hermes Agent"
		}
		return info, nil
	}
	if info.Installed && info.AuthStatus == cula.AuthUnknown {
		info.AuthStatus = cula.AuthUnknown
	}
	return info, nil
}

func (r *Runtime) detectAPI(ctx context.Context) ([]cula.Model, cula.AuthStatus) {
	client := &http.Client{Timeout: 2 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBaseURL(r.cfg, cula.SessionInput{})+"/v1/models", nil)
	if err != nil {
		return nil, cula.AuthUnknown
	}
	addAuth(req, apiKey(r.cfg, cula.SessionInput{}))
	resp, err := client.Do(req)
	if err != nil {
		return nil, cula.AuthUnknown
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, cula.AuthLoggedOut
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, cula.AuthUnknown
	}
	var payload struct {
		Data []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if json.NewDecoder(resp.Body).Decode(&payload) != nil {
		return nil, cula.AuthLoggedIn
	}
	models := make([]cula.Model, 0, len(payload.Data))
	for _, m := range payload.Data {
		name := m.Name
		if name == "" {
			name = m.ID
		}
		if m.ID != "" {
			models = append(models, cula.Model{ID: m.ID, Name: name})
		}
	}
	return models, cula.AuthLoggedIn
}

func (r *Runtime) SpawnSession(ctx context.Context, input cula.SessionInput) (cula.Session, error) {
	return newSession(ctx, r, input)
}

func envValue(cfgEnv, inputEnv []string, key, fallback string) string {
	prefix := key + "="
	for i := len(inputEnv) - 1; i >= 0; i-- {
		if strings.HasPrefix(inputEnv[i], prefix) {
			return strings.TrimPrefix(inputEnv[i], prefix)
		}
	}
	for i := len(cfgEnv) - 1; i >= 0; i-- {
		if strings.HasPrefix(cfgEnv[i], prefix) {
			return strings.TrimPrefix(cfgEnv[i], prefix)
		}
	}
	return fallback
}

func apiKey(cfg cula.Config, input cula.SessionInput) string {
	if key := envValue(cfg.Env, input.Env, "HERMES_API_KEY", ""); key != "" {
		return key
	}
	return envValue(cfg.Env, input.Env, "API_SERVER_KEY", "")
}

func apiBaseURL(cfg cula.Config, input cula.SessionInput) string {
	if base := envValue(cfg.Env, input.Env, "HERMES_API_BASE_URL", ""); base != "" {
		return strings.TrimRight(base, "/")
	}
	if base := envValue(cfg.Env, input.Env, "API_SERVER_BASE_URL", ""); base != "" {
		return strings.TrimRight(base, "/")
	}
	host := envValue(cfg.Env, input.Env, "API_SERVER_HOST", "")
	port := envValue(cfg.Env, input.Env, "API_SERVER_PORT", "")
	if host != "" || port != "" {
		if host == "" {
			host = "127.0.0.1"
		}
		if port == "" {
			port = "8642"
		}
		return "http://" + host + ":" + port
	}
	return defaultBaseURL
}

func addAuth(req *http.Request, key string) {
	if strings.TrimSpace(key) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(key))
	}
}
