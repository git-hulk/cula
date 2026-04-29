package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/git-hulk/cula/internal/runtime/claudecode"
	"github.com/git-hulk/cula/internal/runtime/codex"
	"github.com/git-hulk/cula/internal/runtime/copilot"
	"github.com/git-hulk/cula/internal/runtime/opencode"
	"github.com/git-hulk/cula/internal/tui"
	cula "github.com/git-hulk/cula/pkg"
)

func main() {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "getwd: %v\n", err)
		os.Exit(1)
	}

	registry := cula.NewRegistry(
		claudecode.New(cula.Config{}),
		codex.New(cula.Config{}),
		opencode.New(cula.Config{}),
		copilot.New(cula.Config{}),
	)

	model := tui.New(tui.Config{
		DefaultWorkingDir: cwd,
		Registry:          registry,
	})

	prog := tea.NewProgram(model, tea.WithAltScreen())
	final, err := prog.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "tui error: %v\n", err)
		os.Exit(1)
	}
	if m, ok := final.(*tui.Model); ok {
		if sid := m.SessionID(); sid != "" {
			fmt.Printf("session: %s\n", sid)
		}
	}
}
