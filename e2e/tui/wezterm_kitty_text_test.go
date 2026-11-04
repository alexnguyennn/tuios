package tuie2e

import (
	"strings"
	"testing"
	"time"

	"github.com/Gaurav-Gosain/tuitest"
)

// WezTerm's keys under the kitty protocol's associated-text flag, which tuios
// asks the host for while it reads keys itself (flags 29). WezTerm attaches the
// key's control character as text, which the protocol forbids: kitty and
// Ghostty send CSI 27u for Esc. These are byte-for-byte what WezTerm sends,
// captured from real key presses.
const (
	weztermEsc   = "\x1b[27;1;27u"
	weztermEnter = "\x1b[13;1;13u"
	// weztermW is a plain w, which carries its own character as text.
	weztermW = "\x1b[119;1;119u"
)

// TestWeztermEscClosesThePalette. With WezTerm under flags 29, Esc read as
// "\x1b" and Enter as "\r", so the palette neither closed nor ran what it
// found.
//
// Negative control: on a binary without dropSpuriousKeyText (local/release
// before the fix) the palette stays open on weztermEsc.
func TestWeztermEscClosesThePalette(t *testing.T) {
	term, _ := attachClientBase(t)
	dir := artifactDir(t)

	if err := term.SendKeys(legacyCtrlP); err != nil {
		t.Fatalf("open palette: %v", err)
	}
	waitPaletteOpen(t, term, "before wezterm esc")
	if err := term.SendKeys(tuitest.Key(weztermEsc)); err != nil {
		t.Fatalf("send wezterm esc: %v", err)
	}
	waitPaletteClosed(t, term, "on wezterm esc")
	saveArtifact(t, term, dir, "closed-on-esc")
	alive(t, term, "after wezterm esc in the palette")
}

// TestWeztermEnterRunsThePaletteRow is the Enter half: Enter in WezTerm's
// form runs the row the query found, and the text of a plain key in the same
// form still reaches the query.
//
// Negative control: on a binary without dropSpuriousKeyText the palette stays
// open on weztermEnter and no window is made.
func TestWeztermEnterRunsThePaletteRow(t *testing.T) {
	term, base := attachClientBase(t)
	dir := artifactDir(t)

	if err := term.SendKeys(legacyCtrlP); err != nil {
		t.Fatalf("open palette: %v", err)
	}
	waitPaletteOpen(t, term, "before wezterm enter")
	if err := term.Type("#new_windo"); err != nil {
		t.Fatalf("type the token query: %v", err)
	}
	if err := term.SendKeys(tuitest.Key(weztermW)); err != nil {
		t.Fatalf("send wezterm w: %v", err)
	}
	if err := term.WaitForText("#new_window", uiTimeout); err != nil {
		t.Fatalf("the wezterm w did not reach the query: %v\n%s", err, term.Snapshot())
	}
	saveArtifact(t, term, dir, "1-query")
	if err := term.SendKeys(tuitest.Key(weztermEnter)); err != nil {
		t.Fatalf("send wezterm enter: %v", err)
	}
	waitPaletteClosed(t, term, "on wezterm enter")
	waitWindowCount(t, term, 2, "wezterm enter on the new_window row")
	waitSessionWindows(t, base, "e2e-ctrlp", 2, "wezterm enter on the new_window row")
	saveArtifact(t, term, dir, "2-ran-on-enter")
	alive(t, term, "after wezterm enter in the palette")
}

// TestWeztermEscLeavesCopyMode is copy mode's side of the same report: Esc in
// WezTerm's form has to leave copy mode, and the cursor with it.
//
// Negative control: on a binary without dropSpuriousKeyText the copy cursor
// stays on screen.
func TestWeztermEscLeavesCopyMode(t *testing.T) {
	base := t.TempDir()
	writeConfig(t, base, copyCursorConfig)
	term := startIn(t, base, copyColorOpts)
	waitBoot(t, term)
	newWindow(t, term)
	enterTerminalMode(t, term)
	runInShell(t, term, "echo copy-$((40+2))", "copy-42", shellTimeout)
	time.Sleep(300 * time.Millisecond)

	if err := term.SendKeys(tuitest.Ctrl('b'), "["); err != nil {
		t.Fatalf("send prefix+[: %v", err)
	}
	waitCopyCursor(t, term, "prefix+[")
	if err := term.SendKeys(tuitest.Key(weztermEsc)); err != nil {
		t.Fatalf("send wezterm esc: %v", err)
	}
	if err := term.WaitFor(func(s tuitest.Screen) bool {
		_, _, ok := copyCursorCell(s)
		return !ok
	}, uiTimeout); err != nil {
		t.Fatalf("wezterm esc did not leave copy mode: %v\n%s", err, term.Snapshot())
	}
	// The pane has the keyboard again: its shell answers, and nothing of the
	// escape was typed into it.
	runInShell(t, term, "echo back-$((40+3))", "back-43", shellTimeout)
	if strings.Contains(term.Screen().Text(), "[27;1;27u") {
		t.Fatalf("the escape leaked into the pane:\n%s", term.Snapshot())
	}
}
