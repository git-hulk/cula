package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"

	cula "github.com/git-hulk/cula/pkg"
)

type Config struct {
	DefaultWorkingDir string
	Registry          *cula.Registry
}

// spinnerThinking pulses a dot from small to large and back so the thinking
// indicator feels like a single icon breathing rather than a generic spinner.
var spinnerThinking = spinner.Spinner{
	Frames: []string{"⋅", "·", "•", "●", "•", "·"},
	FPS:    time.Second / 8,
}

var thinkingVerbs = []string{
	"Thinking", "Pondering", "Meandering", "Reasoning",
	"Reflecting", "Mulling", "Cogitating", "Ruminating",
	"Deliberating", "Considering",
}

type phase int

const (
	phaseLoading phase = iota
	phaseSelectRuntime
	phaseSelectModel
	phaseConfigure
	phaseChat
)

type blockKind int

const (
	blkUser blockKind = iota
	blkText
	blkToolCall
	blkCommand
	blkActivity
	blkNarration
	blkError
	blkInfo
)

type block struct {
	kind   blockKind
	title  string
	body   string
	active bool
	// timestamp marks when the block first appeared in the transcript. Only
	// rendered for user/assistant turns so the chat reads as a conversation
	// log without cluttering tool-call rows.
	timestamp time.Time
	// tool-call specific
	toolID     string
	toolName   string
	toolInput  map[string]any
	toolResult string
}

type eventMsg cula.Event
type eventsClosedMsg struct{}
type sessionReadyMsg struct {
	session cula.Session
}
type sessionErrMsg struct{ err error }
type runtimesDetectedMsg struct {
	infos []cula.RuntimeInfo
}

type Model struct {
	cfg Config

	phase phase

	// selection state
	runtimeInfos []cula.RuntimeInfo
	runtimeIdx   int

	selectedRuntime cula.RuntimeKind
	modelOpts       []cula.Model
	modelIdx        int
	selectedModel   string

	// configure phase
	workingDirInput textinput.Model
	sessionIDInput  textinput.Model
	configFocus     int

	// chat state
	workingDir string
	session    cula.Session
	eventsCh   <-chan cula.Event
	sessionID  string

	blocks   []block
	activity *block // transient activity slot — replaced by each new non-update activity, cleared by permanent events
	input    textarea.Model
	viewport viewport.Model
	spinner  spinner.Model

	busy          bool
	thinkingStart time.Time
	followOutput  bool
	state         cula.State
	width         int
	height        int
	err           string

	// markdown renderer for assistant text — recreated when bubble width
	// changes so word-wrap stays accurate.
	mdRenderer *glamour.TermRenderer
	mdWidth    int
}

func New(cfg Config) *Model {
	ta := textarea.New()
	ta.Placeholder = "Type a message and press Enter to send (Ctrl+J or Alt+Enter for newline)…"
	ta.Prompt = "│ "
	ta.CharLimit = 0
	ta.SetHeight(3)
	ta.ShowLineNumbers = false
	// Plain Enter is reserved for sending. Terminals do not reliably expose a
	// distinct Shift+Enter keypress, so use portable newline bindings instead.
	ta.KeyMap.InsertNewline = key.NewBinding(
		key.WithKeys("ctrl+j", "alt+enter"),
		key.WithHelp("ctrl+j/alt+enter", "insert newline"),
	)
	ta.Focus()

	sp := spinner.New()
	sp.Spinner = spinnerThinking
	sp.Style = styleSpinner

	vp := viewport.New(0, 0)
	vp.SetContent("")

	wdInput := textinput.New()
	wdInput.Placeholder = cfg.DefaultWorkingDir
	wdInput.SetValue(cfg.DefaultWorkingDir)
	wdInput.Prompt = "  "
	wdInput.CharLimit = 4096
	wdInput.Width = 60

	sidInput := textinput.New()
	sidInput.Placeholder = "leave blank to start a new session"
	sidInput.Prompt = "  "
	sidInput.CharLimit = 256
	sidInput.Width = 60

	return &Model{
		cfg:             cfg,
		phase:           phaseLoading,
		input:           ta,
		spinner:         sp,
		viewport:        vp,
		workingDirInput: wdInput,
		sessionIDInput:  sidInput,
		followOutput:    true,
	}
}

// SessionID returns the current session id, if any.
func (m *Model) SessionID() string {
	return m.sessionID
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, m.spinner.Tick, m.detectRuntimes())
}

func (m *Model) detectRuntimes() tea.Cmd {
	return func() tea.Msg {
		infos := m.cfg.Registry.DetectAll(context.Background())
		// stable order: claude-code, codex, opencode, copilot
		order := map[cula.RuntimeKind]int{
			cula.RuntimeClaudeCode: 0,
			cula.RuntimeCodex:      1,
			cula.RuntimeOpenCode:   2,
			cula.RuntimeCopilot:    3,
		}
		sort.Slice(infos, func(i, j int) bool {
			return order[infos[i].Kind] < order[infos[j].Kind]
		})
		return runtimesDetectedMsg{infos: infos}
	}
}

func (m *Model) spawnSession() tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		sess, err := m.cfg.Registry.SpawnSession(ctx, cula.SessionInput{
			Runtime:    m.selectedRuntime,
			Model:      m.selectedModel,
			SessionID:  m.sessionID,
			WorkingDir: m.workingDir,
		})
		if err != nil {
			return sessionErrMsg{err: err}
		}
		return sessionReadyMsg{session: sess}
	}
}

func waitForEvent(ch <-chan cula.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return eventsClosedMsg{}
		}
		return eventMsg(ev)
	}
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.layout()
		m.refreshTranscript()
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		// keep spinner ticking while loading or while busy
		if m.phase == phaseLoading || m.busy {
			if m.phase == phaseChat {
				m.refreshTranscript()
			}
			return m, cmd
		}
		return m, nil

	case runtimesDetectedMsg:
		m.runtimeInfos = msg.infos
		m.runtimeIdx = m.firstSelectableRuntime()
		m.phase = phaseSelectRuntime
		return m, nil
	}

	switch m.phase {
	case phaseSelectRuntime:
		return m.updateRuntimeSelect(msg)
	case phaseSelectModel:
		return m.updateModelSelect(msg)
	case phaseConfigure:
		return m.updateConfigure(msg)
	case phaseChat:
		return m.updateChat(msg)
	}
	return m, nil
}

func (m *Model) firstSelectableRuntime() int {
	for i, info := range m.runtimeInfos {
		if info.Installed {
			return i
		}
	}
	return 0
}

func (m *Model) updateRuntimeSelect(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.Type {
	case tea.KeyCtrlC, tea.KeyEsc:
		return m, tea.Quit
	case tea.KeyUp:
		m.moveRuntime(-1)
	case tea.KeyDown:
		m.moveRuntime(+1)
	case tea.KeyEnter:
		if len(m.runtimeInfos) == 0 {
			return m, nil
		}
		info := m.runtimeInfos[m.runtimeIdx]
		if !info.Installed {
			return m, nil
		}
		m.selectedRuntime = info.Kind
		m.modelOpts = info.Models
		m.modelIdx = 0
		m.phase = phaseSelectModel
	}
	switch key.String() {
	case "k":
		m.moveRuntime(-1)
	case "j":
		m.moveRuntime(+1)
	}
	return m, nil
}

func (m *Model) moveRuntime(delta int) {
	n := len(m.runtimeInfos)
	if n == 0 {
		return
	}
	idx := m.runtimeIdx
	for range n {
		idx = (idx + delta + n) % n
		if m.runtimeInfos[idx].Installed {
			m.runtimeIdx = idx
			return
		}
	}
}

func (m *Model) updateModelSelect(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	total := len(m.modelOpts) + 1 // +1 for the "default" entry at index 0
	switch key.Type {
	case tea.KeyCtrlC, tea.KeyEsc:
		// go back to runtime selection
		m.phase = phaseSelectRuntime
		return m, nil
	case tea.KeyUp:
		m.modelIdx = (m.modelIdx - 1 + total) % total
	case tea.KeyDown:
		m.modelIdx = (m.modelIdx + 1) % total
	case tea.KeyEnter:
		if m.modelIdx == 0 {
			m.selectedModel = ""
		} else {
			m.selectedModel = m.modelOpts[m.modelIdx-1].ID
		}
		m.phase = phaseConfigure
		m.configFocus = 0
		m.workingDirInput.Focus()
		m.sessionIDInput.Blur()
		return m, textinput.Blink
	}
	switch key.String() {
	case "k":
		m.modelIdx = (m.modelIdx - 1 + total) % total
	case "j":
		m.modelIdx = (m.modelIdx + 1) % total
	}
	return m, nil
}

func (m *Model) updateConfigure(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit
		case tea.KeyEsc:
			m.phase = phaseSelectModel
			m.workingDirInput.Blur()
			m.sessionIDInput.Blur()
			return m, nil
		case tea.KeyTab, tea.KeyShiftTab, tea.KeyDown, tea.KeyUp:
			m.toggleConfigFocus()
			return m, textinput.Blink
		case tea.KeyEnter:
			m.startChat()
			return m, m.spawnSession()
		}
	}

	var cmd tea.Cmd
	if m.configFocus == 0 {
		m.workingDirInput, cmd = m.workingDirInput.Update(msg)
	} else {
		m.sessionIDInput, cmd = m.sessionIDInput.Update(msg)
	}
	return m, cmd
}

func (m *Model) toggleConfigFocus() {
	if m.configFocus == 0 {
		m.configFocus = 1
		m.workingDirInput.Blur()
		m.sessionIDInput.Focus()
	} else {
		m.configFocus = 0
		m.sessionIDInput.Blur()
		m.workingDirInput.Focus()
	}
}

func (m *Model) startChat() {
	m.workingDir = strings.TrimSpace(m.workingDirInput.Value())
	if m.workingDir == "" {
		m.workingDir = m.cfg.DefaultWorkingDir
	}
	m.sessionID = strings.TrimSpace(m.sessionIDInput.Value())
	m.workingDirInput.Blur()
	m.sessionIDInput.Blur()
	m.phase = phaseChat
	m.followOutput = true
	m.layout()
	body := fmt.Sprintf("runtime=%s · model=%s · working directory=%s", m.selectedRuntime, valueOr(m.selectedModel, "default"), m.workingDir)
	if m.sessionID != "" {
		body += " · resume=" + m.sessionID
	}
	m.appendBlock(block{kind: blkInfo, title: "session", body: body})
	m.refreshTranscript()
}

func (m *Model) updateChat(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case sessionErrMsg:
		m.err = msg.err.Error()
		m.appendBlock(block{kind: blkError, title: "session error", body: msg.err.Error()})
		m.refreshTranscript()
		return m, nil

	case sessionReadyMsg:
		m.session = msg.session
		m.eventsCh = msg.session.Events()
		return m, waitForEvent(m.eventsCh)

	case eventMsg:
		m.handleEvent(cula.Event(msg))
		m.refreshTranscript()
		if m.eventsCh != nil {
			cmds = append(cmds, waitForEvent(m.eventsCh))
		}

	case eventsClosedMsg:
		m.busy = false
		m.thinkingStart = time.Time{}
		m.activity = nil
		m.dropTurnActivity()
		m.input.Focus()
		m.refreshTranscript()
		return m, nil

	case tea.KeyMsg:
		if m.handleHistoryKey(msg) {
			m.refreshTranscript()
			return m, nil
		}
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			if m.session != nil {
				_ = m.session.Cancel(context.Background())
			}
			return m, tea.Quit
		case tea.KeyEnter:
			if msg.Alt {
				break
			}
			if m.busy || m.session == nil {
				return m, nil
			}
			prompt := strings.TrimSpace(m.input.Value())
			if prompt == "" {
				return m, nil
			}
			m.input.Reset()
			m.input.Blur()
			m.activity = nil
			m.appendBlock(block{kind: blkUser, body: prompt, timestamp: time.Now()})
			m.busy = true
			m.thinkingStart = time.Now()
			m.refreshTranscript()
			cmds = append(cmds, m.sendPrompt(prompt), m.spinner.Tick)
			return m, tea.Batch(cmds...)
		case tea.KeyCtrlL:
			m.blocks = nil
			m.activity = nil
			m.followOutput = true
			m.refreshTranscript()
			return m, nil
		}
	}

	var cmd tea.Cmd
	// While the assistant is replying, drop key events targeted at the
	// textarea so the user can't type the next prompt until the turn
	// completes. Non-key messages (window resize, blink, etc.) still flow
	// through so the input stays correctly sized.
	if _, isKey := msg.(tea.KeyMsg); !isKey || !m.busy {
		m.input, cmd = m.input.Update(msg)
		cmds = append(cmds, cmd)
	}

	beforeOffset := m.viewport.YOffset
	m.viewport, cmd = m.viewport.Update(msg)
	if m.viewport.YOffset != beforeOffset {
		m.followOutput = m.viewport.AtBottom()
	}
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m *Model) handleHistoryKey(msg tea.KeyMsg) bool {
	switch msg.Type {
	case tea.KeyPgUp:
		m.viewport.PageUp()
		m.followOutput = false
		return true
	case tea.KeyPgDown:
		m.viewport.PageDown()
		m.followOutput = m.viewport.AtBottom()
		return true
	case tea.KeyHome:
		m.viewport.GotoTop()
		m.followOutput = false
		return true
	case tea.KeyEnd:
		m.viewport.GotoBottom()
		m.followOutput = true
		return true
	}

	switch msg.String() {
	case "ctrl+u":
		m.viewport.HalfPageUp()
		m.followOutput = false
		return true
	case "ctrl+d":
		m.viewport.HalfPageDown()
		m.followOutput = m.viewport.AtBottom()
		return true
	}

	return false
}

func (m *Model) sendPrompt(prompt string) tea.Cmd {
	return func() tea.Msg {
		if err := m.session.Send(context.Background(), prompt); err != nil {
			return sessionErrMsg{err: err}
		}
		return nil
	}
}

func (m *Model) handleEvent(ev cula.Event) {
	if ev.SessionID != "" {
		m.sessionID = ev.SessionID
	}

	switch ev.Type {
	case cula.EventState:
		m.state = ev.State
		switch ev.State {
		case cula.StateRunning:
		case cula.StateCompleted, cula.StateFailed, cula.StateCanceled:
			m.busy = false
			m.thinkingStart = time.Time{}
			m.activity = nil
			m.dropTurnActivity()
			m.input.Focus()
		}
	case cula.EventDone:
		m.busy = false
		m.thinkingStart = time.Time{}
		m.activity = nil
		m.dropTurnActivity()
		m.input.Focus()
	case cula.EventText:
		text := strings.TrimSpace(ev.Text)
		if text == "" {
			return
		}
		m.activity = nil
		m.dropThinkingBlocks()
		// Merge with the most recent assistant text block in this turn so
		// interleaved tool calls (edits/writes go to the permanent transcript)
		// don't split a single reply into multiple bubbles.
		for i := len(m.blocks) - 1; i >= 0; i-- {
			if m.blocks[i].kind == blkUser {
				break
			}
			if m.blocks[i].kind == blkText {
				m.blocks[i].body += "\n\n" + text
				return
			}
		}
		m.appendBlock(block{kind: blkText, body: text, timestamp: time.Now()})
	case cula.EventActivity:
		if ev.Activity == nil {
			return
		}
		switch ev.Activity.Type {
		case cula.ActivityThinking:
			// Coalesce: at most one "Thinking…" row per turn, and never one
			// after assistant text has already landed in the turn. Repeated
			// reasoning bursts would otherwise stack into a wall of bullets,
			// or — once we've dropped the live row to make room for text —
			// reappear below the reply with a second activity banner.
			if m.shouldSuppressThinking() {
				return
			}
			m.activity = nil
			m.appendBlock(block{kind: blkActivity, title: thinkingTitle})
		case cula.ActivityCommand:
			m.activity = &block{kind: blkCommand, title: "command", body: strings.Join(ev.Activity.Parameters, "  •  "), active: true}
		case cula.ActivityToolCall:
			m.activity = &block{kind: blkToolCall, title: "tool", body: strings.Join(ev.Activity.Parameters, " "), active: true}
		case cula.ActivityNarration:
			body := strings.TrimSpace(strings.Join(ev.Activity.Parameters, "\n"))
			if body == "" {
				return
			}
			m.activity = nil
			m.appendBlock(block{kind: blkNarration, body: body})
		}
	case cula.EventToolCall:
		if ev.ToolCall == nil {
			return
		}
		m.activity = nil
		m.appendBlock(block{
			kind:      blkToolCall,
			title:     ev.ToolCall.Name,
			toolID:    ev.ToolCall.ID,
			toolName:  ev.ToolCall.Name,
			toolInput: ev.ToolCall.Input,
			active:    true,
		})
	case cula.EventToolResult:
		if ev.ToolResult == nil {
			return
		}
		for i := len(m.blocks) - 1; i >= 0; i-- {
			b := &m.blocks[i]
			if b.kind == blkToolCall && b.toolID == ev.ToolResult.ToolCallID {
				b.toolResult = ev.ToolResult.Content
				b.active = false
				return
			}
		}
		if m.activity != nil && m.activity.kind == blkToolCall && m.activity.toolID == ev.ToolResult.ToolCallID {
			m.activity.toolResult = ev.ToolResult.Content
			m.activity.active = false
		}
	case cula.EventError:
		m.activity = nil
		m.appendBlock(block{kind: blkError, title: "error", body: ev.Error})
	case cula.EventStderr:
		m.appendBlock(block{kind: blkInfo, title: "stderr", body: ev.Error})
	}
}

func (m *Model) appendBlock(b block) {
	m.blocks = append(m.blocks, b)
}

// dropThinkingBlocks strips Thinking… activity rows from the transcript once a
// turn has completed. They're useful as a live indicator while the assistant
// is mid-reply, but afterwards they only add noise above the actual answer.
func (m *Model) dropThinkingBlocks() {
	out := m.blocks[:0]
	for _, b := range m.blocks {
		if b.kind == blkActivity && b.title == thinkingTitle {
			continue
		}
		out = append(out, b)
	}
	m.blocks = out
}

// dropTurnActivity strips every activity-kind row (Thinking, tool calls,
// commands) from the transcript at turn end. While the assistant is working
// these rows are a useful live progress indicator, but once the final answer
// has landed they're just noise above the reply.
func (m *Model) dropTurnActivity() {
	out := m.blocks[:0]
	for _, b := range m.blocks {
		if isActivityBlock(b.kind) {
			continue
		}
		out = append(out, b)
	}
	m.blocks = out
}

const thinkingTitle = "Thinking…"

// shouldSuppressThinking reports whether a new "Thinking…" row should be
// dropped on arrival. We suppress when this turn already has a Thinking… row
// (coalesce repeated bursts) or any assistant text (codex emits reasoning
// items between agentMessage chunks; a second Thinking… below the reply just
// duplicates the busy footer).
func (m *Model) shouldSuppressThinking() bool {
	for i := len(m.blocks) - 1; i >= 0; i-- {
		b := m.blocks[i]
		if b.kind == blkUser {
			return false
		}
		if b.kind == blkText {
			return true
		}
		if b.kind == blkActivity && b.title == thinkingTitle {
			return true
		}
	}
	return false
}

func (m *Model) layout() {
	if m.width == 0 || m.height == 0 {
		return
	}
	inputHeight := 5
	vpHeight := max(m.height-inputHeight-2, 3)
	m.viewport.Width = m.width
	m.viewport.Height = vpHeight
	m.input.SetWidth(m.width - 2)
}

func (m *Model) View() string {
	switch m.phase {
	case phaseLoading:
		return m.renderLoading()
	case phaseSelectRuntime:
		return m.renderRuntimeSelect()
	case phaseSelectModel:
		return m.renderModelSelect()
	case phaseConfigure:
		return m.renderConfigure()
	case phaseChat:
		return m.renderChat()
	}
	return ""
}

func (m *Model) renderConfigure() string {
	var b strings.Builder
	b.WriteString(renderWordmark())
	b.WriteString("  ")
	summary := "runtime: " + string(m.selectedRuntime)
	if m.selectedModel != "" {
		summary += " · model: " + m.selectedModel
	}
	b.WriteString(styleFaint.Render(summary + " · session settings"))
	b.WriteString("\n\n")

	wdLabel := "  working dir"
	sidLabel := "  session id"
	if m.configFocus == 0 {
		wdLabel = styleAssistantHeader.Render("▸ working dir")
	} else {
		sidLabel = styleAssistantHeader.Render("▸ session id")
	}

	b.WriteString(wdLabel + " " + styleFaint.Render("(optional, defaults to current dir)") + "\n")
	b.WriteString(m.workingDirInput.View() + "\n\n")
	b.WriteString(sidLabel + " " + styleFaint.Render("(optional, leave blank for a new session)") + "\n")
	b.WriteString(m.sessionIDInput.View() + "\n\n")
	b.WriteString(styleHint.Render("Tab switch field · Enter start · Esc back · Ctrl+C quit"))
	return b.String()
}

func (m *Model) renderLoading() string {
	return renderWordmark() + "\n" +
		styleFaint.Render("  "+greeting()+" — an agent runtime sandbox") + "\n\n" +
		m.spinner.View() + " " + styleFaint.Render("scanning for installed runtimes…")
}

func (m *Model) renderRuntimeSelect() string {
	var b strings.Builder
	b.WriteString(renderWordmark())
	b.WriteString("  ")
	b.WriteString(styleFaint.Render("pick a runtime"))
	b.WriteString("\n\n")

	for i, info := range m.runtimeInfos {
		b.WriteString(m.renderRuntimeOption(info, i == m.runtimeIdx))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(styleHint.Render("↑/↓ navigate · Enter select · Esc quit"))
	return b.String()
}

func (m *Model) renderRuntimeOption(info cula.RuntimeInfo, selected bool) string {
	cursor := "  "
	if selected {
		cursor = styleAssistantHeader.Render(glyphCursor + " ")
	}

	name := string(info.Kind)
	authBadge := renderAuthBadge(info)

	if !info.Installed {
		return cursor + styleFaint.Render(name+"  not installed")
	}

	version := ""
	if info.Version != "" {
		version = styleFaint.Render(" v" + info.Version)
	}

	nameStyle := lipgloss.NewStyle()
	if selected {
		nameStyle = nameStyle.Bold(true)
	}
	return cursor + nameStyle.Render(name) + version + "  " + authBadge
}

func renderAuthBadge(info cula.RuntimeInfo) string {
	switch info.AuthStatus {
	case cula.AuthLoggedIn:
		return lipgloss.NewStyle().Foreground(colOk).Render(glyphDotActive + " logged in")
	case cula.AuthLoggedOut:
		return lipgloss.NewStyle().Foreground(colWarn).Render(glyphDotActive + " logged out")
	case cula.AuthNotInstalled:
		return styleFaint.Render(glyphDotActive + " not installed")
	}
	return styleFaint.Render(glyphDotActive + " unknown")
}

func (m *Model) renderModelSelect() string {
	var b strings.Builder
	b.WriteString(renderWordmark())
	b.WriteString("  ")
	b.WriteString(styleFaint.Render("runtime: ") + string(m.selectedRuntime) + styleFaint.Render(" · pick a model"))
	b.WriteString("\n\n")

	options := []string{"(default)"}
	for _, mo := range m.modelOpts {
		label := mo.ID
		if mo.Name != "" && mo.Name != mo.ID {
			label = mo.Name + "  " + styleFaint.Render(mo.ID)
		}
		options = append(options, label)
	}

	for i, opt := range options {
		cursor := "  "
		style := lipgloss.NewStyle()
		if i == m.modelIdx {
			cursor = styleAssistantHeader.Render(glyphCursor + " ")
			style = lipgloss.NewStyle().Bold(true)
		}
		if i == 0 {
			b.WriteString(cursor + style.Render(opt) + " " + styleFaint.Render("— let the runtime pick"))
		} else {
			b.WriteString(cursor + style.Render(opt))
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(styleHint.Render("↑/↓ navigate · Enter select · Esc back"))
	return b.String()
}

func (m *Model) renderChat() string {
	body := m.viewport.View()
	footer := m.renderFooter()
	return lipgloss.JoinVertical(lipgloss.Left, body, footer)
}

func (m *Model) renderFooter() string {
	rule := ""
	if m.width > 0 {
		rule = styleDivider.Render(strings.Repeat(glyphRule, m.width))
	}
	status := styleFaint.Render(m.historyStatus())
	hintStyled := styleHint.Render("PgUp/PgDn scroll · Ctrl+U/Ctrl+D half-page · Home top · End latest · Enter send · Ctrl+J/Alt+Enter newline · Ctrl+L clear · Esc/Ctrl+C quit")
	return lipgloss.JoinVertical(lipgloss.Left, rule, status, m.input.View(), hintStyled)
}

// renderThinkingHint renders the live thinking status line shown directly under
// the Thinking… activity row. The assistant banner is already rendered above
// the activity section, so this line focuses on progress only.
func (m *Model) renderThinkingHint() string {
	icon := m.spinner.View()
	if m.thinkingStart.IsZero() {
		return styleHint.Render(fmt.Sprintf("%s %s… (input locked until reply)", icon, thinkingVerbs[0]))
	}
	elapsed := time.Since(m.thinkingStart)
	secs := int(elapsed.Seconds())
	verb := thinkingVerbs[(secs/5)%len(thinkingVerbs)]
	return styleHint.Render(fmt.Sprintf("%s %s… (%ds · input locked until reply)\n", icon, verb, secs))
}

func (m *Model) refreshTranscript() {
	if m.phase != phaseChat || m.viewport.Height == 0 {
		return
	}
	oldOffset := m.viewport.YOffset
	follow := m.followOutput || m.viewport.AtBottom()
	var sb strings.Builder
	if len(m.blocks) == 0 && m.activity == nil {
		sb.WriteString(m.renderWelcome())
		m.viewport.SetContent(sb.String())
		m.viewport.GotoTop()
		return
	}
	// Activity-kind blocks (thinking, tool calls, commands) cluster under a
	// single assistant banner so a long stretch of operations reads as one
	// turn rather than a wall of separator-divided rows. Assistant text
	// bubbles are transparent to the grouping — only a fresh user prompt
	// closes the banner, so tool calls split by mid-turn prose stay visually
	// under the same speaker.
	visible := m.visibleBlocks()
	inActivityGroup := false
	for _, b := range visible {
		current := isActivityBlock(b.kind)
		if current && !inActivityGroup {
			sb.WriteString(m.renderActivityBanner())
			inActivityGroup = true
		}
		if b.kind == blkUser {
			inActivityGroup = false
		}
		sb.WriteString(m.renderBlock(b))
		sb.WriteString("\n")
	}
	if m.activity != nil {
		if !inActivityGroup {
			sb.WriteString(m.renderActivityBanner())
		}
		sb.WriteString(m.renderBlock(*m.activity))
		sb.WriteString("\n")
	}
	m.viewport.SetContent(sb.String())
	if follow {
		m.viewport.GotoBottom()
		m.followOutput = true
		return
	}
	m.viewport.SetYOffset(oldOffset)
	m.followOutput = m.viewport.AtBottom()
}

func isActivityBlock(k blockKind) bool {
	return k == blkActivity || k == blkToolCall || k == blkCommand || k == blkNarration
}

// visibleBlocks returns the transcript with older tool calls of the same name
// dropped within each turn. A turn is everything between user prompts; within
// it, only the latest tool call of each toolName survives so a long run of
// Read/Grep operations collapses to one row each.
func (m *Model) visibleBlocks() []block {
	if len(m.blocks) == 0 {
		return nil
	}
	out := make([]block, 0, len(m.blocks))
	turnStart := 0
	flush := func(end int) {
		latest := make(map[string]int)
		for k := turnStart; k < end; k++ {
			if m.blocks[k].kind == blkToolCall {
				latest[m.blocks[k].toolName] = k
			}
		}
		for k := turnStart; k < end; k++ {
			b := m.blocks[k]
			if b.kind == blkToolCall && latest[b.toolName] != k {
				continue
			}
			out = append(out, b)
		}
	}
	for i, b := range m.blocks {
		if b.kind == blkUser {
			flush(i)
			out = append(out, b)
			turnStart = i + 1
		}
	}
	flush(len(m.blocks))
	return out
}

func (m *Model) renderActivityBanner() string {
	style := lipgloss.NewStyle().Foreground(colAssistant).Bold(true)
	return style.Render("▎ "+m.assistantDisplayName()) + "\n"
}

func (m *Model) renderWelcome() string {
	title := styleWelcomeTitle.Render(glyphMark + " ready when you are")
	body := styleFaint.Render("type a prompt below and press Enter") + "\n" +
		styleFaint.Render("Ctrl+L clears the transcript · Esc cancels")
	card := styleWelcomeCard.Render(title + "\n\n" + body)
	if m.width > 8 {
		card = lipgloss.PlaceHorizontal(m.width-2, lipgloss.Center, card)
	}
	return "\n" + card + "\n"
}

func (m *Model) historyStatus() string {
	if len(m.viewport.View()) == 0 {
		return "history: empty"
	}
	if m.viewport.AtBottom() {
		return "history: live tail"
	}
	return fmt.Sprintf("history: %.0f%%", m.viewport.ScrollPercent()*100)
}

func (m *Model) renderBlock(b block) string {
	switch b.kind {
	case blkUser:
		header := styleUserHeader.Render("▎ you") + styleUserMeta.Render(formatStamp(b.timestamp))
		return m.renderBubble(styleUserBubble, header, styleUserBody.Render("\n"+b.body))
	case blkText:
		header := styleAssistantHeader.Render("▎ "+m.assistantDisplayName()) + styleAssistantMeta.Render(formatStamp(b.timestamp))
		body := m.renderMarkdown(b.body)
		return m.renderBubble(styleAssistantBubble, header, "\n"+body)
	case blkCommand:
		return "  " + styleTool.Render(activityBullet(b.active && m.busy)+" $ ") + styleToolBody.Render(b.body)
	case blkActivity:
		if b.title == thinkingTitle && m.busy {
			return "  " + m.renderThinkingHint()
		}
		return "  " + styleFaint.Render(activityBullet(false)+" "+b.title)
	case blkNarration:
		// Indent each line under the activity banner so a multi-paragraph
		// commentary chunk reads as one quoted aside instead of bleeding
		// out to the bubble's edge.
		lines := strings.Split(b.body, "\n")
		for i, ln := range lines {
			lines[i] = "  " + styleFaint.Render(glyphArrow+" ") + styleNarration.Render(ln)
		}
		return strings.Join(lines, "\n")
	case blkToolCall:
		bullet := activityBullet(b.active && m.busy)
		style := toolHeaderStyle(b.toolName)
		head := "  " + style.Render(bullet+" "+b.toolName)
		if args := formatToolArgs(b.toolName, b.toolInput); args != "" {
			head += styleToolBody.Render("(" + truncate(args, 120) + ")")
		}
		if summary := summarizeToolResult(b); summary != "" {
			head += "\n     " + styleFaint.Render("⎿  "+summary)
		}
		return head
	case blkError:
		return styleError.Render("✗ "+valueOr(b.title, "error")+": ") + b.body
	case blkInfo:
		switch b.title {
		case "session":
			return styleSessionInfo.Render("• session ") + styleFaint.Render(b.body)
		case "stderr":
			return styleStderrLabel.Render("• stderr ") + styleFaint.Render(b.body)
		}
		return styleFaint.Render("• " + b.title + ": " + b.body)
	}
	return b.body
}

// activityBullet returns the leading glyph for an activity row: a sparkle
// while the row is the current in-flight action, a filled bullet once it has
// settled. Mirrors the Claude Code transcript style.
func activityBullet(active bool) string {
	if active {
		return "✳"
	}
	return "⏺"
}

// formatToolArgs returns the inside of the `Tool(...)` parens for a tool
// call header. Promotes the primary param value when there is one (file_path,
// command, etc.); otherwise joins the remaining inputs as `k=v, k=v`.
func formatToolArgs(name string, input map[string]any) string {
	if len(input) == 0 {
		return ""
	}
	if key := primaryInputKey(name); key != "" {
		if v, ok := input[key]; ok {
			return stringifyDetail(v)
		}
	}
	keys := make([]string, 0, len(input))
	for k := range input {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+stringifyArg(input[k]))
	}
	return strings.Join(parts, ", ")
}

// summarizeToolResult returns the `⎿` continuation text for a finished tool
// call: the first non-empty line of the tool result, truncated. Returns "" for
// in-flight calls or empty results so we don't render a dangling continuation.
func summarizeToolResult(b block) string {
	if b.active {
		return ""
	}
	for ln := range strings.SplitSeq(b.toolResult, "\n") {
		ln = strings.TrimSpace(ln)
		if ln != "" {
			return truncate(ln, 120)
		}
	}
	return ""
}

func primaryInputKey(name string) string {
	switch strings.ToLower(name) {
	case "bash", "shell", "exec", "run":
		return "command"
	case "read", "view", "open", "cat", "file_read",
		"edit", "write", "multiedit", "multi_edit",
		"notebookedit", "notebook_edit", "apply_patch":
		return "file_path"
	case "ls", "list", "tree":
		return "path"
	case "grep", "search", "ripgrep", "rg", "find":
		return "pattern"
	case "websearch", "web_search":
		return "query"
	case "fetch", "webfetch", "web_fetch", "curl", "http":
		return "url"
	case "task", "agent", "subagent":
		return "description"
	}
	return ""
}

func stringifyDetail(v any) string {
	switch t := v.(type) {
	case string:
		return truncate(t, 200)
	case nil:
		return "null"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return truncate(string(b), 200)
}

func stringifyArg(v any) string {
	switch t := v.(type) {
	case string:
		return truncate(t, 60)
	case nil:
		return "null"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return truncate(string(b), 60)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 1 {
		return "…"
	}
	return s[:max-1] + "…"
}

func valueOr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func (m *Model) assistantDisplayName() string {
	runtimeName := string(m.selectedRuntime)
	var modelName string
	for _, info := range m.runtimeInfos {
		if info.Kind != m.selectedRuntime {
			continue
		}
		if info.Name != "" {
			runtimeName = info.Name
		}
		for _, mdl := range info.Models {
			if mdl.ID == m.selectedModel {
				modelName = mdl.Name
				break
			}
		}
		break
	}
	if modelName == "" {
		modelName = m.selectedModel
	}
	modelName = shortModelName(runtimeName, modelName)
	if modelName == "" {
		return runtimeName
	}
	return runtimeName + "(" + modelName + ")"
}

// shortModelName drops a leading runtime brand from the model label so the
// header reads "Claude Code(Opus 4.7)" instead of "Claude Code(Claude Opus 4.7)".
func shortModelName(runtime, model string) string {
	for _, prefix := range brandPrefixes(runtime) {
		if rest, ok := strings.CutPrefix(model, prefix); ok {
			return rest
		}
	}
	return model
}

func brandPrefixes(runtime string) []string {
	first := strings.SplitN(runtime, " ", 2)[0]
	if first == "" {
		return nil
	}
	return []string{first + " "}
}

// renderWordmark prints the cula brand mark for full-page screens.
func renderWordmark() string {
	return styleBrandMark.Render(glyphMark+" cula") + styleFaint.Render(" "+glyphArrow)
}

// greeting returns a soft, time-aware salutation for the loading screen.
func greeting() string {
	h := time.Now().Hour()
	switch {
	case h < 5:
		return "still up?"
	case h < 12:
		return "good morning"
	case h < 17:
		return "good afternoon"
	case h < 22:
		return "good evening"
	default:
		return "burning the midnight oil"
	}
}

// formatStamp renders a block's local-time stamp as " · 15:04" — a leading
// dot separator keeps it visually attached to the speaker label. Returns ""
// for the zero time so legacy/system blocks stay untimestamped.
func formatStamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return "  " + t.Format("15:04")
}

// renderMarkdown renders assistant text through glamour so headings, lists,
// code blocks, and inline emphasis get proper terminal styling. Falls back to
// the raw body if the renderer can't be built or the input fails to render —
// we never want a markdown error to swallow the reply.
func (m *Model) renderMarkdown(body string) string {
	body = strings.TrimRight(body, "\n")
	width := max(m.viewport.Width-4, 20)
	if m.mdRenderer == nil || m.mdWidth != width {
		r, err := glamour.NewTermRenderer(
			glamour.WithStyles(assistantBubbleMarkdownStyle()),
			glamour.WithWordWrap(width),
		)
		if err != nil {
			return styleAssistantBody.Render(body)
		}
		m.mdRenderer = r
		m.mdWidth = width
	}
	out, err := m.mdRenderer.Render(body)
	if err != nil {
		return styleAssistantBody.Render(body)
	}
	return strings.Trim(out, "\n")
}

// assistantBubbleMarkdownStyle returns a glamour style derived from the dark
// theme but stripped of internal margins and embedded background colors so the
// assistant bubble paints as a single unbroken surface. Without this, glamour's
// per-element backgrounds (H1, inline code, code blocks) and document margin
// punch holes in the bubble's bg fill.
func assistantBubbleMarkdownStyle() ansi.StyleConfig {
	cfg := styles.DarkStyleConfig
	zero := uint(0)
	cfg.Document.Margin = &zero
	cfg.Document.BlockPrefix = ""
	cfg.Document.BlockSuffix = ""
	cfg.H1.BackgroundColor = nil
	cfg.Code.BackgroundColor = nil
	cfg.CodeBlock.Margin = &zero
	if cfg.CodeBlock.Chroma != nil {
		cfg.CodeBlock.Chroma.Error.BackgroundColor = nil
	}
	return cfg
}

// renderBubble lays out a user/assistant turn as a width-filling bubble so
// the background tint actually paints the row, not just the glyphs. Width is
// taken from the viewport with a small right margin so consecutive bubbles of
// the two speakers visually separate without a hard divider.
func (m *Model) renderBubble(container lipgloss.Style, header, body string) string {
	w := max(m.viewport.Width, 20)
	return container.Width(w).Render(header + "\n" + body)
}

// toolHeaderStyle picks a header style for a tool block based on whether the
// tool reads or mutates state. Read-style lookups recede in cyan so a long
// stretch of them doesn't drown out the actual change in amber.
func toolHeaderStyle(name string) lipgloss.Style {
	switch strings.ToLower(name) {
	case "read", "view", "open", "cat", "file_read",
		"grep", "search", "find", "ripgrep", "rg",
		"ls", "list", "tree",
		"fetch", "webfetch", "web_fetch", "curl", "http",
		"websearch", "web_search":
		return styleToolPassive
	}
	return styleTool
}
