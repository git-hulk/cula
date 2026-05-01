package main

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	cula "github.com/git-hulk/cula/pkg"
)

func TestTokenPromptCapturesAllFieldsAfterTabbing(t *testing.T) {
	m := newTokenPromptModel(config{})
	if len(m.fields) != 2 {
		t.Fatalf("fields = %d, want 2", len(m.fields))
	}

	m.fields[0].input.SetValue("xoxb-test")
	m.moveFocus(tea.KeyDown)
	m.fields[1].input.SetValue("xapp-test")

	if !m.captureAllTokens() {
		t.Fatalf("captureAllTokens failed: %s", m.err)
	}
	if m.cfg.botToken != "xoxb-test" {
		t.Fatalf("bot token = %q", m.cfg.botToken)
	}
	if m.cfg.appToken != "xapp-test" {
		t.Fatalf("app token = %q", m.cfg.appToken)
	}
}

func TestSelectedRuntimeIndexPrefersInstalledRuntime(t *testing.T) {
	infos := []cula.RuntimeInfo{
		{Kind: cula.RuntimeClaudeCode, Installed: true},
		{Kind: cula.RuntimeCodex, Installed: false},
		{Kind: cula.RuntimeOpenCode, Installed: true},
	}

	if got := selectedRuntimeIndex(infos, cula.RuntimeOpenCode); got != 2 {
		t.Fatalf("selectedRuntimeIndex preferred = %d, want 2", got)
	}
	if got := selectedRuntimeIndex(infos, cula.RuntimeCodex); got != 0 {
		t.Fatalf("selectedRuntimeIndex fallback = %d, want 0", got)
	}
}

func TestModelChoicesKeepCustomPreferredModel(t *testing.T) {
	choices := modelChoices([]cula.Model{{ID: "gpt-5"}, {ID: "gpt-5.1"}}, "custom-model")

	if len(choices) != 4 {
		t.Fatalf("choices = %d, want 4", len(choices))
	}
	if choices[0].id != "" {
		t.Fatalf("default choice id = %q, want empty", choices[0].id)
	}
	if choices[1].id != "custom-model" {
		t.Fatalf("preferred choice id = %q", choices[1].id)
	}
	if got := selectedModelIndex(choices, "custom-model"); got != 1 {
		t.Fatalf("selectedModelIndex = %d, want 1", got)
	}
}

func TestModelChoicesSelectDetectedPreferredModel(t *testing.T) {
	choices := modelChoices([]cula.Model{{ID: "gpt-5"}, {ID: "gpt-5.1"}}, "gpt-5.1")

	if len(choices) != 3 {
		t.Fatalf("choices = %d, want 3", len(choices))
	}
	if choices[2].id != "gpt-5.1" {
		t.Fatalf("detected preferred choice id = %q", choices[2].id)
	}
	if got := selectedModelIndex(choices, "gpt-5.1"); got != 2 {
		t.Fatalf("selectedModelIndex = %d, want 2", got)
	}
}

func TestWorkdirCompletionsIncludeDirectoriesOnly(t *testing.T) {
	root := t.TempDir()
	alpha := filepath.Join(root, "alpha")
	if err := os.Mkdir(alpha, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "alpine.txt"), []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := workdirCompletions(filepath.Join(root, "al"))
	want := withTrailingPathSeparator(alpha)
	if len(got) != 1 || got[0] != want {
		t.Fatalf("workdirCompletions = %#v, want %#v", got, []string{want})
	}
}

func TestWorkdirCompletionsListChildrenAfterSeparator(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "child")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}

	got := workdirCompletions(withTrailingPathSeparator(root))
	want := withTrailingPathSeparator(child)
	if len(got) != 1 || got[0] != want {
		t.Fatalf("workdirCompletions = %#v, want %#v", got, []string{want})
	}
}

func TestWorkdirCompletionsPreserveRelativePrefix(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := os.Mkdir("alpha", 0o755); err != nil {
		t.Fatal(err)
	}

	got := workdirCompletions("." + string(os.PathSeparator) + "al")
	want := "." + string(os.PathSeparator) + "alpha" + string(os.PathSeparator)
	if len(got) != 1 || got[0] != want {
		t.Fatalf("workdirCompletions = %#v, want %#v", got, []string{want})
	}
}

func TestResolveWorkdirPathExpandsHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got, err := resolveWorkdirPath("~/project")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, "project")
	if got != want {
		t.Fatalf("resolveWorkdirPath = %q, want %q", got, want)
	}
}
