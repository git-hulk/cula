package hermes

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cula "github.com/git-hulk/cula/pkg"
)

func TestSessionStartsRunAndStreamsEvents(t *testing.T) {
	var sawAuth bool
	var sawSessionID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer secret" {
			sawAuth = true
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/runs":
			var body struct {
				Input     string `json:"input"`
				SessionID string `json:"session_id"`
				Model     string `json:"model"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body.Input != "hello" || body.Model != "hermes/test" {
				t.Fatalf("run body = %#v", body)
			}
			sawSessionID = body.SessionID
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"run_id":"run_123","status":"started"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/runs/run_123/events":
			w.Header().Set("Content-Type", "text/event-stream")
			flusher, _ := w.(http.Flusher)
			for _, ev := range []string{
				`{"event":"message.delta","run_id":"run_123","delta":"hi"}`,
				`{"event":"run.completed","run_id":"run_123","output":"done"}`,
			} {
				fmt.Fprintf(w, "data: %s\n\n", ev)
				if flusher != nil {
					flusher.Flush()
				}
			}
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	rt := New(cula.Config{Env: []string{"HERMES_API_BASE_URL=" + server.URL, "HERMES_API_KEY=secret"}})
	sess, err := rt.SpawnSession(context.Background(), cula.SessionInput{
		Prompt:    "hello",
		SessionID: "session-1",
		Model:     "hermes/test",
	})
	if err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}

	var types []cula.EventType
	var texts []string
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev, ok := <-sess.Events():
			if !ok {
				goto done
			}
			types = append(types, ev.Type)
			if ev.Type == cula.EventText {
				texts = append(texts, ev.Text)
			}
			if ev.Type == cula.EventDone {
				goto done
			}
		case <-deadline:
			t.Fatalf("timed out waiting for done; types=%v texts=%v", types, texts)
		}
	}
done:
	if strings.Join(texts, "|") != "hi" {
		t.Fatalf("texts = %v", texts)
	}
	if !sawAuth {
		t.Fatalf("API key was not sent as bearer auth")
	}
	if sawSessionID != "session-1" {
		t.Fatalf("session_id = %q, want session-1", sawSessionID)
	}
	if types[len(types)-1] != cula.EventDone {
		t.Fatalf("last event = %s, want done (all=%v)", types[len(types)-1], types)
	}
}

func TestSessionDoesNotDuplicateCompletedOutputAfterDeltas(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/runs":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"run_id":"run_dup","status":"started"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/runs/run_dup/events":
			w.Header().Set("Content-Type", "text/event-stream")
			for _, ev := range []string{
				`{"event":"message.delta","run_id":"run_dup","delta":"P"}`,
				`{"event":"message.delta","run_id":"run_dup","delta":"ONG"}`,
				`{"event":"run.completed","run_id":"run_dup","output":"PONG"}`,
			} {
				fmt.Fprintf(w, "data: %s\n\n", ev)
			}
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	rt := New(cula.Config{Env: []string{"HERMES_API_BASE_URL=" + server.URL}})
	sess, err := rt.SpawnSession(context.Background(), cula.SessionInput{Prompt: "hello"})
	if err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}

	var texts []string
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-sess.Events():
			if ev.Type == cula.EventText {
				texts = append(texts, ev.Text)
			}
			if ev.Type == cula.EventDone {
				if got := strings.Join(texts, ""); got != "PONG" {
					t.Fatalf("text stream = %q, want PONG without duplicated completed output; chunks=%v", got, texts)
				}
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for done; texts=%v", texts)
		}
	}
}

func TestReadSSESkipsCommentsAndBlankLines(t *testing.T) {
	scanner := bufio.NewScanner(strings.NewReader(": keepalive\n\ndata: {\"event\":\"message.delta\",\"delta\":\"x\"}\n\n"))
	lines := readSSEData(scanner)
	if len(lines) != 1 || string(lines[0]) != `{"event":"message.delta","delta":"x"}` {
		t.Fatalf("readSSEData = %q", lines)
	}
}
