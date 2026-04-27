package tui

import "testing"

func TestRefreshTranscriptFollowsTailWhenAttached(t *testing.T) {
	m := New(Config{})
	m.phase = phaseChat
	m.width = 80
	m.height = 12
	m.layout()

	for range 20 {
		m.appendBlock(block{kind: blkInfo, title: "info", body: "line"})
	}
	m.refreshTranscript()
	if !m.viewport.AtBottom() {
		t.Fatal("expected initial transcript to follow the tail")
	}

	m.appendBlock(block{kind: blkInfo, title: "info", body: "new line"})
	m.refreshTranscript()
	if !m.viewport.AtBottom() {
		t.Fatal("expected appended content to keep following the tail")
	}
}

func TestRefreshTranscriptPreservesHistoryScroll(t *testing.T) {
	m := New(Config{})
	m.phase = phaseChat
	m.width = 80
	m.height = 12
	m.layout()

	for range 24 {
		m.appendBlock(block{kind: blkInfo, title: "info", body: "line"})
	}
	m.refreshTranscript()
	m.viewport.LineUp(3)
	m.followOutput = false
	offset := m.viewport.YOffset
	if m.viewport.AtBottom() {
		t.Fatal("expected viewport to be detached from the tail after scrolling up")
	}

	m.appendBlock(block{kind: blkInfo, title: "info", body: "new line"})
	m.refreshTranscript()
	if got := m.viewport.YOffset; got != offset {
		t.Fatalf("viewport offset = %d, want %d", got, offset)
	}
	if m.viewport.AtBottom() {
		t.Fatal("expected viewport to stay in history instead of snapping back to the tail")
	}
}
