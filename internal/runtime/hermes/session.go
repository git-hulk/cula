package hermes

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	iruntime "github.com/git-hulk/cula/internal/runtime"
	cula "github.com/git-hulk/cula/pkg"
)

var _ cula.Session = (*session)(nil)

type session struct {
	runtime     *Runtime
	input       cula.SessionInput
	client      *http.Client
	baseURL     string
	apiKey      string
	events      chan cula.Event
	promptCh    chan string
	doneCh      chan struct{}
	doneOnce    sync.Once
	ctx         context.Context
	cancelCtx   context.CancelFunc
	mu          sync.Mutex
	sessionID   string
	activeRun   string
	emittedText bool
}

func newSession(ctx context.Context, rt *Runtime, input cula.SessionInput) (cula.Session, error) {
	input = iruntime.NormalizeInput(input)
	runCtx, cancel := context.WithCancel(context.Background())
	s := &session{
		runtime:   rt,
		input:     input,
		client:    &http.Client{Timeout: 0},
		baseURL:   apiBaseURL(),
		apiKey:    apiKey(),
		events:    make(chan cula.Event, 1024),
		promptCh:  make(chan string, 16),
		doneCh:    make(chan struct{}),
		ctx:       runCtx,
		cancelCtx: cancel,
		sessionID: input.SessionID,
	}
	iruntime.Emit(s.events, s.doneCh, cula.Event{Type: cula.EventState, Runtime: cula.RuntimeHermes, SessionID: input.SessionID, State: cula.StateRunning})
	go s.consumePrompts()
	if input.Prompt != "" {
		if err := s.Send(ctx, input.Prompt); err != nil {
			_ = s.Cancel(ctx)
			return nil, err
		}
	}
	return s, nil
}

func (s *session) Send(ctx context.Context, prompt string) error {
	if prompt == "" {
		return nil
	}
	select {
	case s.promptCh <- prompt:
		return nil
	case <-s.doneCh:
		return iruntime.ErrSessionClosed
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *session) Events() <-chan cula.Event { return s.events }

func (s *session) Cancel(ctx context.Context) error {
	s.doneOnce.Do(func() {
		s.cancelCtx()
		s.mu.Lock()
		runID := s.activeRun
		s.mu.Unlock()
		if runID != "" {
			_ = s.stopRun(ctx, runID)
		}
		iruntime.Emit(s.events, nil, cula.Event{Type: cula.EventState, Runtime: cula.RuntimeHermes, SessionID: s.sessionID, State: cula.StateCanceled})
		close(s.doneCh)
		close(s.events)
	})
	return nil
}

func (s *session) consumePrompts() {
	for {
		select {
		case prompt := <-s.promptCh:
			if err := s.runPrompt(prompt); err != nil {
				s.emitError(err.Error())
				iruntime.Emit(s.events, s.doneCh, cula.Event{Type: cula.EventState, Runtime: cula.RuntimeHermes, SessionID: s.sessionID, State: cula.StateFailed})
			}
		case <-s.doneCh:
			return
		}
	}
}

func (s *session) runPrompt(prompt string) error {
	runID, err := s.startRun(prompt)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.activeRun = runID
	s.emittedText = false
	if s.sessionID == "" {
		s.sessionID = runID
	}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if s.activeRun == runID {
			s.activeRun = ""
		}
		s.mu.Unlock()
	}()
	return s.streamRun(runID)
}

func (s *session) startRun(prompt string) (string, error) {
	body := map[string]any{"input": prompt}
	if s.sessionID != "" {
		body["session_id"] = s.sessionID
	}
	if s.input.Model != "" {
		body["model"] = s.input.Model
	}
	data, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(s.ctx, http.MethodPost, s.baseURL+"/v1/runs", bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	addAuth(req, s.apiKey)
	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("start hermes run: status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var out struct {
		RunID  string `json:"run_id"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.RunID == "" {
		return "", fmt.Errorf("start hermes run: empty run_id")
	}
	return out.RunID, nil
}

func (s *session) streamRun(runID string) error {
	req, err := http.NewRequestWithContext(s.ctx, http.MethodGet, s.baseURL+"/v1/runs/"+runID+"/events", nil)
	if err != nil {
		return err
	}
	addAuth(req, s.apiKey)
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("stream hermes run: status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	for scanner.Scan() {
		if line, ok := parseSSEDataLine(scanner.Text()); ok {
			s.handleRaw(line)
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

func (s *session) handleRaw(line []byte) {
	var raw json.RawMessage
	if err := json.Unmarshal(line, &raw); err != nil {
		s.emitError(fmt.Sprintf("decode SSE event: %v", err))
		return
	}
	var parsed event
	_ = json.Unmarshal(raw, &parsed)
	if id := captureSession(raw); id != "" {
		s.sessionID = id
	}
	ev, ok := ParseEvent(raw)
	if !ok {
		ev = cula.Event{Type: cula.EventRaw}
	}
	emit := true
	if parsed.Event == "message.delta" && ev.Type == cula.EventText {
		s.mu.Lock()
		s.emittedText = true
		s.mu.Unlock()
	}
	if parsed.Event == "run.completed" && ev.Type == cula.EventText {
		s.mu.Lock()
		emit = !s.emittedText
		s.mu.Unlock()
	}
	if emit {
		s.emitEvent(raw, ev)
	}
	if parsed.Event == "run.completed" {
		iruntime.Emit(s.events, s.doneCh, cula.Event{Type: cula.EventDone, Runtime: cula.RuntimeHermes, SessionID: s.sessionID})
	} else if parsed.Event == "run.failed" {
		iruntime.Emit(s.events, s.doneCh, cula.Event{Type: cula.EventState, Runtime: cula.RuntimeHermes, SessionID: s.sessionID, State: cula.StateFailed})
	}
}

func readSSEData(scanner *bufio.Scanner) [][]byte {
	var lines [][]byte
	for scanner.Scan() {
		if line, ok := parseSSEDataLine(scanner.Text()); ok {
			lines = append(lines, line)
		}
	}
	return lines
}

func parseSSEDataLine(line string) ([]byte, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, ":") || !strings.HasPrefix(line, "data:") {
		return nil, false
	}
	data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	if data == "" || data == "[DONE]" {
		return nil, false
	}
	return []byte(data), true
}

func (s *session) stopRun(ctx context.Context, runID string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	stopCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(stopCtx, http.MethodPost, s.baseURL+"/v1/runs/"+runID+"/stop", nil)
	if err != nil {
		return err
	}
	addAuth(req, s.apiKey)
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (s *session) emitEvent(raw json.RawMessage, ev cula.Event) {
	ev.Runtime = cula.RuntimeHermes
	ev.SessionID = s.sessionID
	if s.input.IncludeRaw {
		ev.Raw = raw
	}
	iruntime.Emit(s.events, s.doneCh, ev)
}

func (s *session) emitError(message string) {
	iruntime.Emit(s.events, s.doneCh, cula.Event{Type: cula.EventError, Runtime: cula.RuntimeHermes, SessionID: s.sessionID, Error: message})
}
