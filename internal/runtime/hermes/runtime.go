package hermes

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

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
	info := cula.RuntimeInfo{
		Kind:       cula.RuntimeHermes,
		Name:       "Hermes Agent",
		AuthStatus: cula.AuthNotInstalled,
	}
	if strings.TrimSpace(apiKey()) == "" {
		return info, nil
	}

	models, status := r.detectAPI(ctx)
	info.AuthStatus = status
	if status == cula.AuthLoggedIn {
		info.Installed = true
		info.Models = models
	}
	return info, nil
}

func (r *Runtime) detectAPI(ctx context.Context) ([]cula.Model, cula.AuthStatus) {
	client := &http.Client{Timeout: 2 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBaseURL()+"/v1/models", nil)
	if err != nil {
		return nil, cula.AuthUnknown
	}
	addAuth(req, apiKey())
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

func apiKey() string {
	return os.Getenv("HERMES_API_KEY")
}

func apiBaseURL() string {
	if base := strings.TrimSpace(os.Getenv("HERMES_API_BASE_URL")); base != "" {
		return strings.TrimRight(base, "/")
	}
	return defaultBaseURL
}

func addAuth(req *http.Request, key string) {
	if strings.TrimSpace(key) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(key))
	}
}
