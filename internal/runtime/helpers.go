package runtime

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	aime "github.com/git-hulk/aime/pkg"
)

var ErrSessionClosed = errors.New("session is closed")

func NormalizeInput(input aime.SessionInput) aime.SessionInput {
	if input.WorkingDir == "" {
		if wd, err := os.Getwd(); err == nil {
			input.WorkingDir = wd
		}
	}
	return input
}

func BinaryPath(cfg aime.Config, fallback string) string {
	if cfg.BinaryPath != "" {
		return cfg.BinaryPath
	}
	return fallback
}

func CommandEnv(input aime.SessionInput, cfg aime.Config) []string {
	env := os.Environ()
	env = append(env, cfg.Env...)
	env = append(env, input.Env...)
	return env
}

func RunOutput(ctx context.Context, binary string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, binary, args...)
	return cmd.Output()
}

func ScannerFor(r io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	return scanner
}

func Emit(events chan<- aime.Event, done <-chan struct{}, ev aime.Event) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now()
	}
	if done != nil {
		select {
		case <-done:
			return false
		default:
		}
	}
	if done == nil {
		events <- ev
		return true
	}
	select {
	case events <- ev:
		return true
	case <-done:
		return false
	}
}

func DecorateEvents(events []aime.Event, kind aime.RuntimeKind, sessionID string, raw json.RawMessage) []aime.Event {
	out := make([]aime.Event, 0, len(events)+1)
	out = append(out, aime.Event{
		Type:      aime.EventRaw,
		Runtime:   kind,
		SessionID: sessionID,
		Raw:       raw,
	})
	for _, ev := range events {
		if ev.Runtime == "" {
			ev.Runtime = kind
		}
		if ev.SessionID == "" {
			ev.SessionID = sessionID
		}
		if len(ev.Raw) == 0 {
			ev.Raw = raw
		}
		out = append(out, ev)
	}
	return out
}

func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return 1
}

func LookupRuntime(binary, name string, kind aime.RuntimeKind, displayName string) aime.RuntimeInfo {
	info := aime.RuntimeInfo{
		Kind:       kind,
		Name:       displayName,
		AuthStatus: aime.AuthNotInstalled,
	}
	if binary == "" {
		binary = name
	}
	if path, err := exec.LookPath(binary); err == nil {
		info.Installed = true
		info.BinaryPath = path
		info.AuthStatus = aime.AuthUnknown
	} else if filepath.IsAbs(binary) {
		if st, statErr := os.Stat(binary); statErr == nil && !st.IsDir() {
			info.Installed = true
			info.BinaryPath = binary
			info.AuthStatus = aime.AuthUnknown
		}
	}
	return info
}

func Truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}

func StringMapSummary(input map[string]any) string {
	if len(input) == 0 {
		return ""
	}
	for _, key := range []string{"command", "cmd", "path", "file_path", "pattern", "query", "url"} {
		if v, ok := input[key]; ok {
			if s := fmt.Sprint(v); strings.TrimSpace(s) != "" {
				return Truncate(s, 80)
			}
		}
	}
	data, err := json.Marshal(input)
	if err != nil {
		return ""
	}
	return Truncate(string(data), 80)
}

func ParseArguments(args string) map[string]any {
	args = strings.TrimSpace(args)
	if args == "" {
		return nil
	}
	var input map[string]any
	if json.Unmarshal([]byte(args), &input) == nil {
		return input
	}
	return map[string]any{"arguments": args}
}

func WriteJSONLine(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = w.Write(data)
	return err
}
