package hermes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	cula "github.com/git-hulk/cula/pkg"
)

func TestAPIConfigFallsBackToHermesGatewayEnv(t *testing.T) {
	if got := apiKey(cula.Config{Env: []string{"API_SERVER_KEY=gateway-secret"}}, cula.SessionInput{}); got != "gateway-secret" {
		t.Fatalf("apiKey fallback = %q", got)
	}
	if got := apiBaseURL(cula.Config{Env: []string{"API_SERVER_HOST=0.0.0.0", "API_SERVER_PORT=9999"}}, cula.SessionInput{}); got != "http://0.0.0.0:9999" {
		t.Fatalf("apiBaseURL host/port = %q", got)
	}
}

func TestDetectUsesAPIModelsWhenReachable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("missing bearer auth: %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[{"id":"hermes-agent","owned_by":"hermes"},{"id":"openai/gpt-5.1"}]}`))
	}))
	defer server.Close()

	rt := New(cula.Config{BinaryPath: "/definitely/not/hermes", Env: []string{"HERMES_API_BASE_URL=" + server.URL, "HERMES_API_KEY=secret"}})
	info, err := rt.Detect(context.Background())
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if info.Kind != cula.RuntimeHermes || info.Name != "Hermes Agent" {
		t.Fatalf("unexpected info identity: %#v", info)
	}
	if !info.Installed || info.AuthStatus != cula.AuthLoggedIn {
		t.Fatalf("expected reachable API to mark installed/logged in: %#v", info)
	}
	if len(info.Models) != 2 || info.Models[0].ID != "hermes-agent" || info.Models[1].ID != "openai/gpt-5.1" {
		t.Fatalf("models = %#v", info.Models)
	}
}
