package tui

import (
	"github.com/charmbracelet/lipgloss"
)

// brand palette
//
// Neutrals carry a subtle violet undertone so they read as cohesive with the
// cula brand mark — pure greys feel sterile next to colored content. Hues are
// kept few on purpose: violet (brand), sky (user/info), emerald (assistant),
// amber (mutation/attention), red (error). Anything more would tip into
// rainbow vomit; the palette earns its color by giving each hue a job.
var (
	colBrand     = lipgloss.Color("#34d399") // violet — cula
	colBrandSoft = lipgloss.Color("#34d399") // dimmed violet for subtle borders
	colInk       = lipgloss.Color("#e5e3ee") // violet-tinted ink
	colInkSoft   = lipgloss.Color("#d3d0e0")
	colMuted     = lipgloss.Color("#a4a0b3")
	colFaint     = lipgloss.Color("#736f82")
	colDivider   = lipgloss.Color("#3a3450") // a touch lifted for separator visibility
	colUser      = lipgloss.Color("#60a5fa") // sky
	colAssistant = lipgloss.Color("#34d399") // emerald
	colAccent    = lipgloss.Color("#f59e0b") // amber — mutations / attention
	colInfo      = lipgloss.Color("#38bdf8") // cyan-sky — passive / informational
	colOk        = lipgloss.Color("#22c55e")
	colWarn      = lipgloss.Color("#f59e0b")
	colError     = lipgloss.Color("#ef4444")

	// colUserBubbleBg is a soft, lifted gray for the user message bubble —
	// slightly cooler than the terminal background so the prompt reads as one
	// grouped surface without competing with text colors.
	colUserBubbleBg = lipgloss.Color("#2a2733")
)

// glyphs — kept narrow so widths stay predictable across terminals.
const (
	glyphMark      = "◆"
	glyphCursor    = "▸"
	glyphRule      = "─"
	glyphDotActive = "●"
	glyphArrow     = "›"
)

var (
	styleBrandMark = lipgloss.NewStyle().
			Foreground(colBrand).
			Bold(true)

	styleSpinner = lipgloss.NewStyle().Foreground(colAccent)

	styleUserHeader = lipgloss.NewStyle().
			Foreground(colUser).
			Background(colUserBubbleBg).
			Bold(true)

	styleUserBody = lipgloss.NewStyle().
			Foreground(colInk).
			Background(colUserBubbleBg)

	styleUserBubble = lipgloss.NewStyle().
			Background(colUserBubbleBg).
			Padding(1, 1, 1, 1)

	styleUserMeta = lipgloss.NewStyle().
			Foreground(colMuted).
			Background(colUserBubbleBg)

	styleAssistantHeader = lipgloss.NewStyle().
				Foreground(colAssistant).
				Bold(true)

	styleAssistantBody = lipgloss.NewStyle().
				Foreground(colInkSoft)

	styleAssistantBubble = lipgloss.NewStyle().
				Padding(1, 1)

	styleAssistantMeta = lipgloss.NewStyle().
				Foreground(colMuted)

	// styleTool is for state-mutating tool calls (edit/write/exec/command) —
	// amber + bold so the eye lands on real changes when scanning.
	styleTool = lipgloss.NewStyle().
			Foreground(colAccent).
			Bold(true)

	// styleToolPassive is for read-only lookups (read/search/list/fetch) —
	// they recede in cool cyan so a long stretch of reads doesn't compete
	// with the mutation that actually mattered.
	styleToolPassive = lipgloss.NewStyle().
				Foreground(colInfo)

	styleToolBody = lipgloss.NewStyle().Foreground(colInk)

	styleError = lipgloss.NewStyle().
			Foreground(colError).
			Bold(true)

	styleFaint = lipgloss.NewStyle().Foreground(colFaint)

	// styleNarration renders codex-style commentary preamble — the "I'll do
	// X next" narration the agent emits between actions. Faint italic so it
	// reads as in-progress thinking rather than the terminal answer.
	styleNarration = lipgloss.NewStyle().
			Foreground(colMuted).
			Italic(true)

	styleHint = lipgloss.NewStyle().
			Foreground(colFaint).
			Italic(true).
			MarginTop(1)

	styleDivider = lipgloss.NewStyle().Foreground(colDivider)

	styleWelcomeCard = lipgloss.NewStyle().
				Foreground(colMuted).
				BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(colBrandSoft).
				Padding(1, 3).
				MarginTop(1)

	styleWelcomeTitle = lipgloss.NewStyle().
				Foreground(colBrand).
				Bold(true)

	// styleSessionInfo tints the session metadata block with a soft brand
	// violet so the "session" line reads as on-brand framing, not noise.
	styleSessionInfo = lipgloss.NewStyle().Foreground(colBrand)

	// styleStderrLabel marks stderr lines in cyan so they're distinguishable
	// from session info at a glance — different signal, different color.
	styleStderrLabel = lipgloss.NewStyle().Foreground(colInfo)
)
