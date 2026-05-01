package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	cula "github.com/git-hulk/cula/pkg"
)

var (
	wizardColBrand   = lipgloss.AdaptiveColor{Light: "#047857", Dark: "#34d399"}
	wizardColInk     = lipgloss.AdaptiveColor{Light: "#2e2b38", Dark: "#d3d0e0"}
	wizardColMuted   = lipgloss.AdaptiveColor{Light: "#5f5a6e", Dark: "#a4a0b3"}
	wizardColFaint   = lipgloss.AdaptiveColor{Light: "#7e778c", Dark: "#736f82"}
	wizardColInfo    = lipgloss.AdaptiveColor{Light: "#0369a1", Dark: "#38bdf8"}
	wizardColOk      = lipgloss.AdaptiveColor{Light: "#15803d", Dark: "#22c55e"}
	wizardColWarn    = lipgloss.AdaptiveColor{Light: "#b45309", Dark: "#f59e0b"}
	wizardColError   = lipgloss.AdaptiveColor{Light: "#be123c", Dark: "#ef4444"}
	wizardColDivider = lipgloss.AdaptiveColor{Light: "#d8d3e2", Dark: "#3a3450"}

	wizardTitleStyle = lipgloss.NewStyle().
				Foreground(wizardColBrand).
				Bold(true)
	wizardCopyStyle     = lipgloss.NewStyle().Foreground(wizardColInk)
	wizardMutedStyle    = lipgloss.NewStyle().Foreground(wizardColMuted)
	wizardFaintStyle    = lipgloss.NewStyle().Foreground(wizardColFaint)
	wizardHintStyle     = lipgloss.NewStyle().Foreground(wizardColFaint).Italic(true)
	wizardSelectedStyle = lipgloss.NewStyle().Foreground(wizardColBrand).Bold(true)
	wizardInfoStyle     = lipgloss.NewStyle().Foreground(wizardColInfo)
	wizardOkStyle       = lipgloss.NewStyle().Foreground(wizardColOk)
	wizardWarnStyle     = lipgloss.NewStyle().Foreground(wizardColWarn)
	wizardErrorStyle    = lipgloss.NewStyle().Foreground(wizardColError).Bold(true)
	wizardDividerStyle  = lipgloss.NewStyle().Foreground(wizardColDivider)
)

type tokenField struct {
	name  string
	input textinput.Model
}

type tokenPromptModel struct {
	cfg      config
	fields   []tokenField
	focus    int
	err      string
	done     bool
	canceled bool
}

func newTokenPromptModel(cfg config) *tokenPromptModel {
	m := &tokenPromptModel{cfg: cfg}
	if cfg.botToken == "" {
		m.fields = append(m.fields, tokenField{name: "Slack bot token", input: tokenInput("xoxb-...")})
	}
	if cfg.appToken == "" {
		m.fields = append(m.fields, tokenField{name: "Slack app token", input: tokenInput("xapp-...")})
	}
	if len(m.fields) > 0 {
		m.fields[0].input.Focus()
	}
	return m
}

func tokenInput(placeholder string) textinput.Model {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.Prompt = "  "
	ti.CharLimit = 4096
	ti.Width = 72
	ti.EchoMode = textinput.EchoPassword
	styleTextInput(&ti)
	return ti
}

func styleTextInput(ti *textinput.Model) {
	ti.PromptStyle = wizardSelectedStyle
	ti.TextStyle = wizardCopyStyle
	ti.PlaceholderStyle = wizardFaintStyle
	ti.Cursor.Style = wizardSelectedStyle
}

func (m *tokenPromptModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m *tokenPromptModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			m.canceled = true
			return m, tea.Quit
		case tea.KeyTab, tea.KeyShiftTab, tea.KeyUp, tea.KeyDown:
			m.moveFocus(key.Type)
			return m, textinput.Blink
		case tea.KeyEnter:
			if !m.captureCurrentToken() {
				return m, nil
			}
			if m.focus < len(m.fields)-1 {
				m.moveFocus(tea.KeyDown)
				return m, textinput.Blink
			}
			if !m.captureAllTokens() {
				return m, textinput.Blink
			}
			m.done = true
			return m, tea.Quit
		}
	}

	var cmd tea.Cmd
	m.fields[m.focus].input, cmd = m.fields[m.focus].input.Update(msg)
	return m, cmd
}

func (m *tokenPromptModel) moveFocus(key tea.KeyType) {
	if len(m.fields) == 0 {
		return
	}
	m.fields[m.focus].input.Blur()
	switch key {
	case tea.KeyShiftTab, tea.KeyUp:
		m.focus = (m.focus - 1 + len(m.fields)) % len(m.fields)
	default:
		m.focus = (m.focus + 1) % len(m.fields)
	}
	m.fields[m.focus].input.Focus()
	m.err = ""
}

func (m *tokenPromptModel) captureCurrentToken() bool {
	field := &m.fields[m.focus]
	value := strings.TrimSpace(field.input.Value())
	if value == "" {
		m.err = field.name + " is required"
		return false
	}
	switch field.name {
	case "Slack bot token":
		m.cfg.botToken = value
	case "Slack app token":
		m.cfg.appToken = value
	}
	m.err = ""
	return true
}

func (m *tokenPromptModel) captureAllTokens() bool {
	for i := range m.fields {
		m.focus = i
		if !m.captureCurrentToken() {
			for j := range m.fields {
				m.fields[j].input.Blur()
			}
			m.fields[i].input.Focus()
			return false
		}
	}
	return true
}

func (m *tokenPromptModel) View() string {
	var b strings.Builder
	b.WriteString(wizardHeader("Cula Slack setup", "Provide missing Slack tokens before choosing a runtime."))
	for i, field := range m.fields {
		label := "  " + field.name
		if i == m.focus {
			label = wizardSelectedStyle.Render("> " + field.name)
		} else {
			label = wizardMutedStyle.Render(label)
		}
		b.WriteString(label + "\n")
		b.WriteString(field.input.View() + "\n\n")
	}
	if m.err != "" {
		b.WriteString(wizardErrorStyle.Render("Error: "+m.err) + "\n\n")
	}
	b.WriteString(wizardHintStyle.Render("Tab switch field | Enter continue | Esc quit"))
	return b.String()
}

type setupPhase int

const (
	setupSelectRuntime setupPhase = iota
	setupSelectModel
	setupWorkingDir
)

const maxWorkdirCompletions = 100

type modelChoice struct {
	id          string
	label       string
	description string
}

type runtimeSetupModel struct {
	cfg      config
	infos    []cula.RuntimeInfo
	phase    setupPhase
	err      string
	done     bool
	canceled bool

	runtimeIdx int
	models     []modelChoice
	modelIdx   int
	workdir    textinput.Model
}

func newRuntimeSetupModel(cfg config, infos []cula.RuntimeInfo) *runtimeSetupModel {
	infos = sortedRuntimeInfos(infos)
	ti := textinput.New()
	ti.Placeholder = cfg.workingDir
	ti.SetValue(cfg.workingDir)
	ti.Prompt = "  "
	ti.CharLimit = 4096
	ti.Width = 72
	ti.ShowSuggestions = true
	ti.CompletionStyle = wizardHintStyle
	styleTextInput(&ti)

	m := &runtimeSetupModel{
		cfg:        cfg,
		infos:      infos,
		runtimeIdx: selectedRuntimeIndex(infos, cfg.runtime),
		workdir:    ti,
	}
	m.refreshWorkdirCompletions()
	return m
}

func (m *runtimeSetupModel) Init() tea.Cmd {
	return nil
}

func (m *runtimeSetupModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.Type {
		case tea.KeyCtrlC:
			m.canceled = true
			return m, tea.Quit
		case tea.KeyEsc:
			return m.handleEsc()
		}
		switch m.phase {
		case setupSelectRuntime:
			return m.updateRuntimeKey(key)
		case setupSelectModel:
			return m.updateModelKey(key)
		case setupWorkingDir:
			return m.updateWorkdirKey(key, msg)
		}
	}
	if m.phase == setupWorkingDir {
		var cmd tea.Cmd
		m.workdir, cmd = m.workdir.Update(msg)
		m.refreshWorkdirCompletions()
		return m, cmd
	}
	return m, nil
}

func (m *runtimeSetupModel) handleEsc() (tea.Model, tea.Cmd) {
	switch m.phase {
	case setupSelectRuntime:
		m.canceled = true
		return m, tea.Quit
	case setupSelectModel:
		m.phase = setupSelectRuntime
	case setupWorkingDir:
		m.workdir.Blur()
		m.phase = setupSelectModel
	}
	m.err = ""
	return m, nil
}

func (m *runtimeSetupModel) updateRuntimeKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.Type {
	case tea.KeyUp:
		m.moveRuntime(-1)
	case tea.KeyDown:
		m.moveRuntime(+1)
	case tea.KeyEnter:
		if len(m.infos) == 0 {
			return m, nil
		}
		info := m.infos[m.runtimeIdx]
		if !info.Installed {
			return m, nil
		}
		m.cfg.runtime = info.Kind
		m.models = modelChoices(info.Models, m.cfg.model)
		m.modelIdx = selectedModelIndex(m.models, m.cfg.model)
		m.phase = setupSelectModel
	}
	switch key.String() {
	case "k":
		m.moveRuntime(-1)
	case "j":
		m.moveRuntime(+1)
	}
	return m, nil
}

func (m *runtimeSetupModel) updateModelKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if len(m.models) == 0 {
		m.models = []modelChoice{{label: "(default)", description: "let the runtime pick"}}
	}
	switch key.Type {
	case tea.KeyUp:
		m.modelIdx = (m.modelIdx - 1 + len(m.models)) % len(m.models)
	case tea.KeyDown:
		m.modelIdx = (m.modelIdx + 1) % len(m.models)
	case tea.KeyEnter:
		m.cfg.model = m.models[m.modelIdx].id
		m.phase = setupWorkingDir
		m.workdir.Focus()
		return m, textinput.Blink
	}
	switch key.String() {
	case "k":
		m.modelIdx = (m.modelIdx - 1 + len(m.models)) % len(m.models)
	case "j":
		m.modelIdx = (m.modelIdx + 1) % len(m.models)
	}
	return m, nil
}

func (m *runtimeSetupModel) updateWorkdirKey(key tea.KeyMsg, msg tea.Msg) (tea.Model, tea.Cmd) {
	switch key.Type {
	case tea.KeyEnter:
		dir := strings.TrimSpace(m.workdir.Value())
		if dir == "" {
			dir = m.cfg.workingDir
		}
		resolved, err := resolveWorkdirPath(dir)
		if err != nil {
			m.err = fmt.Sprintf("working directory: %v", err)
			return m, nil
		}
		info, err := os.Stat(resolved)
		if err != nil {
			m.err = fmt.Sprintf("working directory: %v", err)
			return m, nil
		}
		if !info.IsDir() {
			m.err = "working directory is not a directory"
			return m, nil
		}
		m.cfg.workingDir = resolved
		m.done = true
		return m, tea.Quit
	}
	var cmd tea.Cmd
	m.workdir, cmd = m.workdir.Update(msg)
	m.refreshWorkdirCompletions()
	return m, cmd
}

func (m *runtimeSetupModel) refreshWorkdirCompletions() {
	m.workdir.SetSuggestions(workdirCompletions(m.workdir.Value()))
}

func (m *runtimeSetupModel) moveRuntime(delta int) {
	n := len(m.infos)
	if n == 0 {
		return
	}
	idx := m.runtimeIdx
	for range n {
		idx = (idx + delta + n) % n
		if m.infos[idx].Installed {
			m.runtimeIdx = idx
			return
		}
	}
}

func (m *runtimeSetupModel) View() string {
	var b strings.Builder
	switch m.phase {
	case setupSelectRuntime:
		b.WriteString(wizardHeader("Cula Slack setup", "Choose the local runtime for Slack requests."))
		for i, info := range m.infos {
			b.WriteString(runtimeLine(info, i == m.runtimeIdx) + "\n")
		}
		b.WriteString("\n")
		b.WriteString(wizardHintStyle.Render("Up/Down navigate | Enter select | Esc quit"))
	case setupSelectModel:
		b.WriteString(wizardHeader("Cula Slack setup", "Choose the model."))
		b.WriteString(wizardMetaLine("Runtime", string(m.cfg.runtime)) + "\n\n")
		for i, model := range m.models {
			b.WriteString(modelLine(model, i == m.modelIdx) + "\n")
		}
		b.WriteString("\n")
		b.WriteString(wizardHintStyle.Render("Up/Down navigate | Enter select | Esc back"))
	case setupWorkingDir:
		b.WriteString(wizardHeader("Cula Slack setup", "Choose the working directory."))
		b.WriteString(wizardMetaLine("Runtime", string(m.cfg.runtime)))
		b.WriteString(wizardDividerStyle.Render(" | "))
		b.WriteString(wizardMetaLine("Model", valueOr(m.cfg.model, "default")) + "\n\n")
		b.WriteString(wizardSelectedStyle.Render("> working directory") + "\n")
		b.WriteString(m.workdir.View() + "\n\n")
		if m.err != "" {
			b.WriteString(wizardErrorStyle.Render("Error: "+m.err) + "\n\n")
		}
		b.WriteString(wizardHintStyle.Render("Tab complete | Up/Down suggestions | Enter start bot | Esc back"))
	}
	return b.String()
}

func wizardHeader(title, subtitle string) string {
	return wizardTitleStyle.Render(title) + "\n" +
		wizardMutedStyle.Render(subtitle) + "\n\n"
}

func wizardMetaLine(label, value string) string {
	return wizardFaintStyle.Render(label+": ") + wizardInfoStyle.Render(value)
}

func modelLine(model modelChoice, selected bool) string {
	label := model.label
	if selected {
		label = wizardSelectedStyle.Render("> " + label)
	} else {
		label = wizardCopyStyle.Render("  " + label)
	}
	if model.description != "" {
		label += wizardFaintStyle.Render(" - " + model.description)
	}
	return label
}

func configureInteractive(ctx context.Context, cfg config, registry *cula.Registry) (config, error) {
	if cfg.botToken == "" || cfg.appToken == "" {
		model := newTokenPromptModel(cfg)
		final, err := tea.NewProgram(model, tea.WithAltScreen()).Run()
		if err != nil {
			return config{}, fmt.Errorf("token prompt: %w", err)
		}
		result, ok := final.(*tokenPromptModel)
		if !ok {
			return config{}, fmt.Errorf("token prompt returned %T", final)
		}
		if result.canceled {
			return config{}, fmt.Errorf("setup canceled")
		}
		if !result.done {
			return config{}, fmt.Errorf("missing Slack token")
		}
		cfg = result.cfg
	}

	infos := registry.DetectAll(ctx)
	infos = sortedRuntimeInfos(infos)
	if selectedRuntimeIndex(infos, cfg.runtime) < 0 {
		return config{}, fmt.Errorf("no installed runtime found")
	}

	model := newRuntimeSetupModel(cfg, infos)
	final, err := tea.NewProgram(model, tea.WithAltScreen()).Run()
	if err != nil {
		return config{}, fmt.Errorf("runtime setup: %w", err)
	}
	result, ok := final.(*runtimeSetupModel)
	if !ok {
		return config{}, fmt.Errorf("runtime setup returned %T", final)
	}
	if result.canceled {
		return config{}, fmt.Errorf("setup canceled")
	}
	if !result.done {
		return config{}, fmt.Errorf("runtime setup incomplete")
	}
	return result.cfg, nil
}

func sortedRuntimeInfos(infos []cula.RuntimeInfo) []cula.RuntimeInfo {
	out := append([]cula.RuntimeInfo(nil), infos...)
	order := map[cula.RuntimeKind]int{
		cula.RuntimeClaudeCode: 0,
		cula.RuntimeCodex:      1,
		cula.RuntimeOpenCode:   2,
		cula.RuntimeCopilot:    3,
	}
	sort.Slice(out, func(i, j int) bool {
		oi, ok := order[out[i].Kind]
		if !ok {
			oi = len(order)
		}
		oj, ok := order[out[j].Kind]
		if !ok {
			oj = len(order)
		}
		if oi == oj {
			return out[i].Kind < out[j].Kind
		}
		return oi < oj
	})
	return out
}

func selectedRuntimeIndex(infos []cula.RuntimeInfo, preferred cula.RuntimeKind) int {
	firstInstalled := -1
	for i, info := range infos {
		if info.Installed && firstInstalled == -1 {
			firstInstalled = i
		}
		if info.Installed && info.Kind == preferred {
			return i
		}
	}
	return firstInstalled
}

func modelChoices(models []cula.Model, preferred string) []modelChoice {
	choices := []modelChoice{{label: "(default)", description: "let the runtime pick"}}
	seen := make(map[string]bool, len(models)+1)
	preferredDetected := false
	for _, model := range models {
		if model.ID == preferred {
			preferredDetected = true
			break
		}
	}
	if preferred != "" && !preferredDetected {
		choices = append(choices, modelChoice{id: preferred, label: preferred, description: "from CULA_MODEL"})
		seen[preferred] = true
	}
	for _, model := range models {
		if model.ID == "" || seen[model.ID] {
			continue
		}
		label := model.ID
		if model.Name != "" && model.Name != model.ID {
			label = model.Name + " (" + model.ID + ")"
		}
		choices = append(choices, modelChoice{id: model.ID, label: label})
		seen[model.ID] = true
	}
	return choices
}

func selectedModelIndex(models []modelChoice, preferred string) int {
	if preferred == "" {
		return 0
	}
	for i, model := range models {
		if model.id == preferred {
			return i
		}
	}
	return 0
}

func resolveWorkdirPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", nil
	}
	return expandHomePath(path)
}

func workdirCompletions(input string) []string {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil
	}
	if input == "~" {
		if _, err := os.UserHomeDir(); err != nil {
			return nil
		}
		return []string{"~" + string(os.PathSeparator)}
	}

	expanded, err := expandHomePath(input)
	if err != nil {
		return nil
	}
	lookupDir, namePrefix := splitCompletionPath(expanded)
	displayPrefix := completionDisplayPrefix(input)

	entries, err := os.ReadDir(lookupDir)
	if err != nil {
		return nil
	}
	lowerPrefix := strings.ToLower(namePrefix)
	matches := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(strings.ToLower(name), lowerPrefix) {
			continue
		}
		if !completionEntryIsDir(lookupDir, entry) {
			continue
		}
		matches = append(matches, displayPrefix+name+string(os.PathSeparator))
	}
	sort.Strings(matches)
	if len(matches) > maxWorkdirCompletions {
		matches = matches[:maxWorkdirCompletions]
	}
	return matches
}

func expandHomePath(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if path == "~" {
		return home, nil
	}
	return filepath.Join(home, path[2:]), nil
}

func splitCompletionPath(path string) (dir, prefix string) {
	if hasTrailingPathSeparator(path) {
		return trimTrailingPathSeparators(path), ""
	}
	return filepath.Dir(path), filepath.Base(path)
}

func completionDisplayPrefix(path string) string {
	if hasTrailingPathSeparator(path) {
		return path
	}
	base := filepath.Base(path)
	if base == "." || base == string(os.PathSeparator) {
		return path
	}
	return path[:len(path)-len(base)]
}

func completionEntryIsDir(parent string, entry os.DirEntry) bool {
	if entry.IsDir() {
		return true
	}
	if entry.Type()&os.ModeSymlink == 0 {
		return false
	}
	info, err := os.Stat(filepath.Join(parent, entry.Name()))
	return err == nil && info.IsDir()
}

func hasTrailingPathSeparator(path string) bool {
	return path != "" && os.IsPathSeparator(path[len(path)-1])
}

func trimTrailingPathSeparators(path string) string {
	for len(path) > 1 && os.IsPathSeparator(path[len(path)-1]) {
		path = path[:len(path)-1]
	}
	return path
}

func withTrailingPathSeparator(path string) string {
	if hasTrailingPathSeparator(path) {
		return path
	}
	return path + string(os.PathSeparator)
}

func runtimeLine(info cula.RuntimeInfo, selected bool) string {
	name := string(info.Kind)
	prefix := "  "
	nameStyle := wizardCopyStyle
	if selected {
		prefix = wizardSelectedStyle.Render("> ")
		nameStyle = wizardSelectedStyle
	}
	if !info.Installed {
		return prefix + wizardFaintStyle.Render(name+" - not installed")
	}
	parts := []string{prefix + nameStyle.Render(name)}
	if info.Version != "" {
		parts = append(parts, wizardFaintStyle.Render("v"+info.Version))
	}
	parts = append(parts, renderAuthStatus(info.AuthStatus))
	if len(info.Models) > 0 {
		parts = append(parts, wizardMutedStyle.Render(fmt.Sprintf("%d models", len(info.Models))))
	}
	return strings.Join(parts, " - ")
}

func renderAuthStatus(status cula.AuthStatus) string {
	switch status {
	case cula.AuthLoggedIn:
		return wizardOkStyle.Render("logged in")
	case cula.AuthLoggedOut:
		return wizardWarnStyle.Render("logged out")
	case cula.AuthNotInstalled:
		return wizardFaintStyle.Render("not installed")
	default:
		return wizardFaintStyle.Render("unknown")
	}
}
