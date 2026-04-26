package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sync"
	"sync/atomic"
	"syscall"

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
	return aime.RuntimeCodex
}

func (r *Runtime) Detect(ctx context.Context) (aime.RuntimeInfo, error) {
	binary := iruntime.BinaryPath(r.cfg, "codex")
	info := iruntime.LookupRuntime(binary, "codex", aime.RuntimeCodex, "Codex CLI")
	if !info.Installed {
		return info, nil
	}
	if out, err := iruntime.RunOutput(ctx, binary, "--version"); err == nil {
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
			info.AuthStatus = aime.AuthLoggedIn
		} else {
			info.AuthStatus = aime.AuthLoggedOut
		}
	} else {
		info.AuthStatus = aime.AuthLoggedOut
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
				info.Models = append(info.Models, aime.Model{ID: m.Slug, Name: m.DisplayName})
			}
		}
	}
	return info, nil
}

func (r *Runtime) Start(ctx context.Context, input aime.SessionInput) (aime.Session, error) {
	input = iruntime.NormalizeInput(input)
	runCtx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(runCtx, iruntime.BinaryPath(r.cfg, "codex"), "app-server", "--listen", "stdio://")
	cmd.Dir = input.WorkingDir
	cmd.Env = iruntime.CommandEnv(input, r.cfg)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}

	s := &session{
		input:     input,
		cmd:       cmd,
		stdin:     stdin,
		events:    make(chan aime.Event, 1024),
		ctx:       runCtx,
		cancelCtx: cancel,
		promptCh:  make(chan string, 16),
		doneCh:    make(chan struct{}),
		sessionID: input.SessionID,
		state:     aime.StateRunning,
	}
	iruntime.Emit(s.events, s.doneCh, aime.Event{Type: aime.EventState, Runtime: aime.RuntimeCodex, SessionID: input.SessionID, State: aime.StateRunning})

	scanner := iruntime.ScannerFor(stdout)
	if err := s.initialize(scanner); err != nil {
		_ = cmd.Process.Kill()
		cancel()
		return nil, err
	}

	s.wg.Add(2)
	go func() {
		defer s.wg.Done()
		s.readStdout(scanner)
	}()
	go func() {
		defer s.wg.Done()
		s.readStderr(stderr)
	}()
	go s.wait()
	go s.consumePrompts()

	if input.Prompt != "" {
		if err := s.Send(ctx, input.Prompt); err != nil {
			_ = s.Cancel(ctx)
			return nil, err
		}
	}
	return s, nil
}

type session struct {
	input aime.SessionInput

	cmd   *exec.Cmd
	stdin io.WriteCloser

	events chan aime.Event

	ctx       context.Context
	cancelCtx context.CancelFunc
	promptCh  chan string
	doneCh    chan struct{}
	doneOnce  sync.Once
	wg        sync.WaitGroup
	nextID    atomic.Int64
	threadID  string
	mu        sync.Mutex
	sessionID string
	state     aime.State
}

var _ aime.Session = (*session)(nil)

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

func (s *session) Events() <-chan aime.Event {
	return s.events
}

func (s *session) Cancel(ctx context.Context) error {
	s.doneOnce.Do(func() {
		s.mu.Lock()
		s.state = aime.StateCanceled
		s.mu.Unlock()
		iruntime.Emit(s.events, nil, aime.Event{Type: aime.EventState, Runtime: aime.RuntimeCodex, SessionID: s.currentSessionID(), State: aime.StateCanceled})
		close(s.doneCh)
		s.cancelCtx()
		if s.cmd != nil && s.cmd.Process != nil {
			_ = s.cmd.Process.Signal(syscall.SIGTERM)
		}
	})
	return nil
}

func (s *session) nextRequestID() int64 {
	return s.nextID.Add(1)
}

func (s *session) currentSessionID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessionID
}

func (s *session) initialize(scanner *bufio.Scanner) error {
	initID := s.nextRequestID()
	if err := iruntime.WriteJSONLine(s.stdin, map[string]any{
		"jsonrpc": "2.0",
		"id":      initID,
		"method":  "initialize",
		"params": map[string]any{
			"clientInfo": map[string]string{"name": "aime", "version": "0.1.0"},
		},
	}); err != nil {
		return fmt.Errorf("send initialize: %w", err)
	}
	if _, err := s.readJSONRPCResponse(scanner, initID, false); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	if err := iruntime.WriteJSONLine(s.stdin, map[string]any{
		"jsonrpc": "2.0",
		"method":  "initialized",
	}); err != nil {
		return fmt.Errorf("send initialized: %w", err)
	}

	threadID := s.nextRequestID()
	params := map[string]any{
		"cwd":            s.input.WorkingDir,
		"approvalPolicy": string(s.input.Permission),
		"sandbox":        string(s.input.Sandbox),
	}
	if params["approvalPolicy"] == "" {
		params["approvalPolicy"] = "never"
	}
	if params["sandbox"] == "" {
		params["sandbox"] = "danger-full-access"
	}
	if s.input.Model != "" {
		params["model"] = s.input.Model
	}
	if err := iruntime.WriteJSONLine(s.stdin, map[string]any{
		"jsonrpc": "2.0",
		"id":      threadID,
		"method":  "thread/start",
		"params":  params,
	}); err != nil {
		return fmt.Errorf("send thread/start: %w", err)
	}
	result, err := s.readJSONRPCResponse(scanner, threadID, true)
	if err != nil {
		return fmt.Errorf("thread/start: %w", err)
	}
	var resp struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return fmt.Errorf("parse thread/start: %w", err)
	}
	if resp.Thread.ID == "" {
		return fmt.Errorf("thread/start returned empty thread id")
	}
	s.threadID = resp.Thread.ID
	s.mu.Lock()
	s.sessionID = resp.Thread.ID
	s.mu.Unlock()
	return nil
}

func (s *session) consumePrompts() {
	for {
		select {
		case <-s.doneCh:
			return
		case prompt := <-s.promptCh:
			turnID := s.nextRequestID()
			req := map[string]any{
				"jsonrpc": "2.0",
				"id":      turnID,
				"method":  "turn/start",
				"params": map[string]any{
					"threadId": s.threadID,
					"input": []map[string]string{
						{"type": "text", "text": prompt},
					},
				},
			}
			if err := iruntime.WriteJSONLine(s.stdin, req); err != nil {
				iruntime.Emit(s.events, s.doneCh, aime.Event{
					Type:      aime.EventError,
					Runtime:   aime.RuntimeCodex,
					SessionID: s.currentSessionID(),
					Error:     fmt.Sprintf("send turn/start: %v", err),
				})
			}
		}
	}
}

func (s *session) readStdout(scanner *bufio.Scanner) {
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		var raw json.RawMessage
		if err := json.Unmarshal(line, &raw); err != nil {
			iruntime.Emit(s.events, s.doneCh, aime.Event{Type: aime.EventError, Runtime: aime.RuntimeCodex, SessionID: s.currentSessionID(), Error: fmt.Sprintf("decode stdout: %v", err)})
			continue
		}
		for _, ev := range iruntime.DecorateEvents(ParseEvent(raw), aime.RuntimeCodex, s.currentSessionID(), raw) {
			iruntime.Emit(s.events, s.doneCh, ev)
		}
	}
	if err := scanner.Err(); err != nil {
		iruntime.Emit(s.events, s.doneCh, aime.Event{Type: aime.EventError, Runtime: aime.RuntimeCodex, SessionID: s.currentSessionID(), Error: fmt.Sprintf("read stdout: %v", err)})
	}
}

func (s *session) readStderr(r io.Reader) {
	scanner := iruntime.ScannerFor(r)
	for scanner.Scan() {
		iruntime.Emit(s.events, s.doneCh, aime.Event{
			Type:      aime.EventStderr,
			Runtime:   aime.RuntimeCodex,
			SessionID: s.currentSessionID(),
			Error:     scanner.Text(),
		})
	}
	if err := scanner.Err(); err != nil {
		iruntime.Emit(s.events, s.doneCh, aime.Event{
			Type:      aime.EventError,
			Runtime:   aime.RuntimeCodex,
			SessionID: s.currentSessionID(),
			Error:     fmt.Sprintf("read stderr: %v", err),
		})
	}
}

func (s *session) readJSONRPCResponse(scanner *bufio.Scanner, requestID int64, forward bool) (json.RawMessage, error) {
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		var msg struct {
			ID     *json.RawMessage `json:"id"`
			Result json.RawMessage  `json:"result"`
			Error  *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
			Method string `json:"method"`
		}
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}
		if msg.ID != nil {
			var id int64
			if json.Unmarshal(*msg.ID, &id) == nil && id == requestID {
				if msg.Error != nil {
					return nil, fmt.Errorf("JSON-RPC error %d: %s", msg.Error.Code, msg.Error.Message)
				}
				return msg.Result, nil
			}
		}
		if forward {
			var raw json.RawMessage
			if json.Unmarshal(line, &raw) == nil {
				for _, ev := range iruntime.DecorateEvents(ParseEvent(raw), aime.RuntimeCodex, s.currentSessionID(), raw) {
					iruntime.Emit(s.events, s.doneCh, ev)
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("stdout closed before response %d", requestID)
}

func (s *session) wait() {
	err := s.cmd.Wait()
	s.wg.Wait()
	code := iruntime.ExitCode(err)
	s.mu.Lock()
	canceled := s.state == aime.StateCanceled
	s.mu.Unlock()
	if !canceled {
		state := aime.StateCompleted
		if code != 0 {
			state = aime.StateFailed
		}
		s.mu.Lock()
		s.state = state
		s.mu.Unlock()
		iruntime.Emit(s.events, nil, aime.Event{
			Type:      aime.EventState,
			Runtime:   aime.RuntimeCodex,
			SessionID: s.currentSessionID(),
			State:     state,
			ExitCode:  &code,
		})
	}
	s.doneOnce.Do(func() {
		close(s.doneCh)
	})
	close(s.events)
}
