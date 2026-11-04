package tuie2e

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Gaurav-Gosain/tuitest"
)

// History must be real scrollback, not just output still on the live screen.
// Read labels before picking and assert both the cursor cell and OSC 52 text,
// so a render-only jump or a wrong absolute-row conversion cannot pass.
func TestCopyModeSeekScrollback(t *testing.T) {
	for _, daemon := range []bool{false, true} {
		t.Run(fmt.Sprintf("daemon=%t", daemon), func(t *testing.T) {
			term, out := startWhitespaceClient(t, daemon)
			cmd := seekCmd + `; i=0; while [ "$i" -lt 150 ]; do printf 'filler %s\n' "$i"; i=$((i+1)); done; printf 'HISTORY%s\n' '-DONE'`
			runInShell(t, term, cmd, "HISTORY-DONE", shellTimeout)
			if lastRowWith(term.Screen(), seekLines[0]) >= 0 {
				t.Fatal("history fixture is still on the live screen")
			}
			if err := term.SendKeys(tuitest.Ctrl('b'), "["); err != nil {
				t.Fatal(err)
			}
			waitCopyCursor(t, term, "history copy mode")
			sendEach(t, term, "g", "g")
			if err := term.WaitFor(func(s tuitest.Screen) bool {
				return lastRowWith(s, seekLines[0]) >= 0
			}, uiTimeout); err != nil {
				t.Fatalf("gg did not show history: %v\n%s", err, term.Snapshot())
			}
			first := seekRows(t, term)
			line := term.Screen().Line(first)
			left := len([]rune(line[:strings.Index(line, seekLines[0])]))
			row, col := waitCopyCursor(t, term, "gg")
			pick := seekCell{first + 1, left + 4}
			seekPick(t, term, "s", "Q", first, "Q", seekCell{row, col}, pick)
			seekCursorAt(t, term, pick, "history s Q")
			seekRestored(t, term, first, "history pick")
			from := len(clipboardWrites(out))
			sendEach(t, term, "v", "e", "y")
			if got := rawWrite(t, term, out, from, "history yank"); got != "Qane" {
				t.Fatalf("history yank = %q, want Qane", got)
			}
			copiesFrame(t, term, "seek-history-yank")
		})
	}
}

// A linecopy pick on any row of a wrapped logical line must include every
// wrapped row, including when the last row is not the labelled/picked row.
// Assert exact clipboard bytes for a capital pick and for a two-pick range.
func TestCopyModeLinecopyLogicalLine(t *testing.T) {
	term, out := startSeekCopyClient(t)
	long := "WRAPPED-" + strings.Repeat("abcdef0123456789", 6) + "-TAIL"
	cmd := fmt.Sprintf("clear; printf '%%s\\n' 'BEGIN-LINE' '%s' 'AFTER-LINE' WRAP\"\"-DONE", long)
	runInShell(t, term, cmd, "WRAP-DONE", shellTimeout)
	time.Sleep(300 * time.Millisecond)
	first := lastRowWith(term.Screen(), "BEGIN-LINE")
	wrapped := lastRowWith(term.Screen(), "WRAPPED-")
	if first < 0 || wrapped <= first {
		t.Fatalf("wrapped fixture missing\n%s", term.Snapshot())
	}
	line := term.Screen().Line(first)
	left := len([]rune(line[:strings.Index(line, "BEGIN-LINE")]))
	for _, single := range []bool{true, false} {
		if err := term.SendKeys(tuitest.Ctrl('b'), "W"); err != nil {
			t.Fatal(err)
		}
		waitCopyCursor(t, term, "wrapped linecopy")
		cells := []seekCell{{first, left}, {wrapped, left}}
		labels := seekLabelsAt(t, term, cells, seekCell{-1, -1})
		from := len(clipboardWrites(out))
		want := long
		if single {
			sendEach(t, term, strings.ToUpper(labels[cells[1]]))
		} else {
			sendEach(t, term, labels[cells[0]])
			labels = seekLabelsAt(t, term, cells, seekCell{-1, -1})
			sendEach(t, term, labels[cells[1]])
			want = "BEGIN-LINE\n" + long
		}
		if got := rawWrite(t, term, out, from, "logical linecopy"); got != want {
			t.Fatalf("single=%t: copied %q, want %q", single, got, want)
		}
		copiesFrame(t, term, "linecopy-logical-line")
	}
}

// Seek in copy mode (s and S): the easymotion jump of tmux-copy-toolkit, native
// to copy mode. s asks for one character and S for two, every match on the
// screen gets a label, and typing a label moves the real copy cursor onto its
// match, so a v selection, a yank or any other copy-mode key carries on from
// there.
//
// How these could pass wrongly, written down first:
//   - The labels could be drawn and the cursor not moved. Every pick is read
//     back off the screen as the position of the one magenta copy cursor cell,
//     which the label cells never use, and a v selection is read off the wire
//     as the exact text between two picks.
//   - A label could be matched to the wrong cell by the test itself. The
//     labels are read from the cells the matches were on before the key, and
//     the cursor must land on the cell the picked label sat on.
//   - The cursor could land where it already was. The first pick is on a row
//     other than the prompt row the cursor starts on, and every later pick
//     starts from the previous one.
//   - Case could be ignored or forced. A capital Q must label only the capital
//     Qs and a lowercase q both cases (smartcase, as copytk's default), so the
//     lowercase q in the first line is labelled by one and not the other.
//   - S could take one character, or any two. The line "Qxy" shares its first
//     character with the match "Qoo" and must stay unlabelled.
//   - Esc could leave the labels up or the cursor moved. It must restore the
//     text under every label and leave the cursor where it was.
//   - The text under a label could stay changed. After every pick or cancel the
//     rows are compared with the printed ones.

// seekLines are the lines the cases print. Q is a letter the prompt, the dock
// and the shell do not use, and the matches are searched on these rows only.
var seekLines = []string{
	"Qa lower qb",
	"mid Qane here",
	"end QQ last",
	"zig Qoo Qxy",
}

// seekAlphabet is the default hints alphabet, which seek labels are made of.
const seekAlphabet = "asdfghjkl"

const seekCmd = `clear; printf '%s\n' 'Qa lower qb' 'mid Qane here' 'end QQ last' 'zig Qoo Qxy' "DONE""-MARK"`

type seekCell struct{ row, col int }

// seekRows is the screen row of the first printed line, found from the marker
// line below the last one.
func seekRows(t *testing.T, term *tuitest.Terminal) int {
	t.Helper()
	first := lastRowWith(term.Screen(), seekLines[0])
	if first < 0 {
		t.Fatalf("the printed lines are not on the screen\n%s", term.Snapshot())
	}
	for i, want := range seekLines {
		if got := term.Screen().Line(first + i); !strings.Contains(got, want) {
			t.Fatalf("row %d holds %q, want %q", first+i, got, want)
		}
	}
	return first
}

// seekMatches are the cells of the printed rows holding any of chars.
func seekMatches(t *testing.T, term *tuitest.Terminal, first int, chars string) []seekCell {
	t.Helper()
	var out []seekCell
	for i, want := range seekLines {
		line := []rune(term.Screen().Line(first + i))
		off := strings.Index(string(line), want)
		if off < 0 {
			t.Fatalf("row %d lost %q", first+i, want)
		}
		base := len([]rune(string(line)[:off]))
		for c, r := range []rune(want) {
			if strings.ContainsRune(chars, r) {
				out = append(out, seekCell{first + i, base + c})
			}
		}
	}
	return out
}

// seekLabelAt waits until every cell in cells shows a label letter and returns
// the letters, in the order of cells. skip is a cell that is allowed to show
// its own character, the one the copy cursor is on.
func seekLabelsAt(t *testing.T, term *tuitest.Terminal, cells []seekCell, skip seekCell) map[seekCell]string {
	t.Helper()
	got := map[seekCell]string{}
	err := term.WaitFor(func(s tuitest.Screen) bool {
		for _, c := range cells {
			if c == skip {
				continue
			}
			content := s.Cell(c.col, c.row).Content
			if content == "" || !strings.Contains(seekAlphabet, content) {
				return false
			}
			got[c] = content
		}
		return true
	}, uiTimeout)
	if err != nil {
		t.Fatalf("the matches %v did not show labels from %q\n%s", cells, seekAlphabet, term.Snapshot())
	}
	seen := map[string]bool{}
	for _, l := range got {
		if seen[l] {
			t.Fatalf("the label %q is on two matches\n%s", l, term.Snapshot())
		}
		seen[l] = true
	}
	return got
}

// seekRestored waits until the printed rows hold their own text again.
func seekRestored(t *testing.T, term *tuitest.Terminal, first int, what string) {
	t.Helper()
	if err := term.WaitFor(func(s tuitest.Screen) bool {
		for i, want := range seekLines {
			if !strings.Contains(s.Line(first+i), want) {
				return false
			}
		}
		return true
	}, uiTimeout); err != nil {
		t.Fatalf("%s: the printed rows did not come back\n%s", what, term.Snapshot())
	}
}

// seekCursorAt waits for the copy cursor to sit on cell.
func seekCursorAt(t *testing.T, term *tuitest.Terminal, cell seekCell, what string) {
	t.Helper()
	var r, c int
	if err := term.WaitFor(func(s tuitest.Screen) bool {
		var ok bool
		r, c, ok = copyCursorCell(s)
		return ok && r == cell.row && c == cell.col
	}, uiTimeout); err != nil {
		t.Fatalf("%s: the copy cursor is on row %d column %d, want row %d column %d\n%s",
			what, r, c, cell.row, cell.col, term.Snapshot())
	}
}

// seekPick types chars after key (s or S), waits for the labels on want, and
// types the label of pick.
func seekPick(t *testing.T, term *tuitest.Terminal, key, chars string, first int, matched string, cursor, pick seekCell) {
	t.Helper()
	cells := seekMatches(t, term, first, matched)
	sendEach(t, term, key)
	sendEach(t, term, chars)
	labels := seekLabelsAt(t, term, cells, cursor)
	label, ok := labels[pick]
	if !ok {
		t.Fatalf("no label on %v: %v", pick, labels)
	}
	copiesFrame(t, term, "seek-labels")
	sendEach(t, term, label)
}

func TestCopyModeSeek(t *testing.T) {
	for _, daemon := range []bool{false, true} {
		name := "standalone"
		if daemon {
			name = "daemon"
		}
		t.Run(name, func(t *testing.T) {
			term, out := startWhitespaceClient(t, daemon)
			runInShell(t, term, seekCmd, "DONE-MARK", shellTimeout)
			time.Sleep(300 * time.Millisecond)
			first := seekRows(t, term)
			// at is a cell of a printed line: the column counts from the
			// line's own first character, which the pane's left edge offsets.
			left := len([]rune(term.Screen().Line(first)[:strings.Index(term.Screen().Line(first), seekLines[0])]))
			at := func(line, col int) seekCell { return seekCell{first + line, left + col} }

			if err := term.SendKeys(tuitest.Ctrl('b'), "["); err != nil {
				t.Fatalf("send prefix+[: %v", err)
			}
			row, col := waitCopyCursor(t, term, "prefix+[")
			prompt := seekCell{row, col}

			// A capital Q labels the capital Qs only. The pick is on a row other
			// than the prompt's, so the cursor really moves.
			seekPick(t, term, "s", "Q", first, "Q", prompt, at(1, 4))
			seekCursorAt(t, term, at(1, 4), "s Q")
			seekRestored(t, term, first, "after s Q")

			// A lowercase q labels both cases. The cursor starts on the Q of
			// line 1, which is not labelled.
			seekPick(t, term, "s", "q", first, "Qq", at(1, 4), at(0, 9))
			seekCursorAt(t, term, at(0, 9), "s q")
			seekRestored(t, term, first, "after s q")

			// Esc gives up: the text comes back and the cursor stays.
			cells := seekMatches(t, term, first, "Q")
			sendEach(t, term, "s", "Q")
			seekLabelsAt(t, term, cells, seekCell{-1, -1})
			sendEach(t, term, tuitest.Esc)
			seekRestored(t, term, first, "after esc")
			seekCursorAt(t, term, at(0, 9), "esc")

			// S takes two characters: only Qoo is labelled, not Qxy or Qane.
			cells = seekMatches(t, term, first, "Q")
			sendEach(t, term, "S", "Q", "o")
			if err := term.WaitFor(func(s tuitest.Screen) bool {
				c := s.Cell(at(3, 4).col, at(3, 4).row).Content
				return c != "" && strings.Contains(seekAlphabet, c)
			}, uiTimeout); err != nil {
				t.Fatalf("S Q o did not label Qoo\n%s", term.Snapshot())
			}
			for _, c := range cells {
				if c == at(3, 4) {
					continue
				}
				if got := term.Screen().Cell(c.col, c.row).Content; got != "Q" {
					t.Fatalf("S Q o labelled %v, which is not a Qo match: %q\n%s", c, got, term.Snapshot())
				}
			}
			label := term.Screen().Cell(at(3, 4).col, at(3, 4).row).Content
			sendEach(t, term, label)
			seekCursorAt(t, term, at(3, 4), "S Q o")
			seekRestored(t, term, first, "after S Q o")

			// A seek moves the cursor like any motion, so a selection carries
			// on from it: s to the first Q, v, s to the second Q of QQ, y.
			seekPick(t, term, "s", "Q", first, "Q", at(3, 4), at(0, 0))
			seekCursorAt(t, term, at(0, 0), "s Q to line 0")
			sendEach(t, term, "v")
			seekPick(t, term, "s", "Q", first, "Q", at(0, 0), at(2, 5))
			seekCursorAt(t, term, at(2, 5), "v s Q")
			from := len(clipboardWrites(out))
			sendEach(t, term, "y")
			want := "Qa lower qb\nmid Qane here\nend QQ"
			if got := rawWrite(t, term, out, from, "v s Q y"); got != want {
				t.Fatalf("v s Q y copied %q, want %q", got, want)
			}
			copiesFrame(t, term, "seek-yank")
			alive(t, term, "after seek in copy mode")
		})
	}
}

// seekCopyConfig binds the two copy actions to copytk's own keys under the
// leader: prefix S is easycopy and prefix W is linecopy.
const seekCopyConfig = copyCursorConfig + "\n[keybindings.prefix_mode]\ncopy_mode_easycopy = [\"S\"]\ncopy_mode_linecopy = [\"W\"]\n"

// startSeekCopyClient is startWhitespaceClient with the two actions bound.
func startSeekCopyClient(t *testing.T) (*tuitest.Terminal, *lockedBuffer) {
	t.Helper()
	base := t.TempDir()
	writeConfig(t, base, seekCopyConfig)
	out := &lockedBuffer{}
	term := startIn(t, base, startOpts{out: out, env: copyColorOpts.env})
	waitBoot(t, term)
	newWindow(t, term)
	enterTerminalMode(t, term)
	return term, out
}

// Easycopy and linecopy (copytk's prefix S and prefix W): from outside copy
// mode, two labelled picks copy a range and close copy mode.
//
// How these could pass wrongly, written down first:
//   - The copy could be right and copy mode still open, or the keys could not
//     have entered copy mode at all. The copy cursor must be on screen after
//     the first pick and gone after the second.
//   - The second pick could offer matches before the first. Easycopy's second
//     round must leave the Q before the start unlabelled, and linecopy's must
//     leave the lines above the first pick unlabelled.
//   - The end could be copied exclusive. The easycopy range ends on the
//     character picked, so the clipboard must hold it.
//   - A capital label could be read as a plain one. It must copy one line,
//     where a lowercase one asks for a second pick.
func TestCopyModeEasycopyLinecopy(t *testing.T) {
	term, out := startSeekCopyClient(t)
	runInShell(t, term, seekCmd, "DONE-MARK", shellTimeout)
	time.Sleep(300 * time.Millisecond)
	first := seekRows(t, term)
	left := len([]rune(term.Screen().Line(first)[:strings.Index(term.Screen().Line(first), seekLines[0])]))
	at := func(line, col int) seekCell { return seekCell{first + line, left + col} }
	none := seekCell{-1, -1}

	// Easycopy from terminal mode: Q of line 0 to the second Q of QQ.
	if err := term.SendKeys(tuitest.Ctrl('b'), "S"); err != nil {
		t.Fatalf("send prefix+S: %v", err)
	}
	waitCopyCursor(t, term, "prefix+S")
	cells := seekMatches(t, term, first, "Q")
	sendEach(t, term, "Q")
	start := seekLabelsAt(t, term, cells, none)
	sendEach(t, term, start[at(0, 0)])
	if err := term.WaitFor(func(s tuitest.Screen) bool {
		r, c, ok := copyCursorCell(s)
		return ok && r == at(0, 0).row && c == at(0, 0).col
	}, uiTimeout); err != nil {
		t.Fatalf("the first pick did not start the copy on the first Q\n%s", term.Snapshot())
	}
	// The second round asks for a character again, and labels only the Qs
	// after the start.
	sendEach(t, term, "Q")
	after := 0
	var ends []seekCell
	for _, c := range cells {
		if c.row > at(0, 0).row || (c.row == at(0, 0).row && c.col > at(0, 0).col) {
			ends = append(ends, c)
			after++
		}
	}
	end := seekLabelsAt(t, term, ends, none)
	if got := term.Screen().Cell(at(0, 0).col, at(0, 0).row).Content; got != "Q" {
		t.Fatalf("the start of the copy shows %q, want its own Q, not a label", got)
	}
	copiesFrame(t, term, "easycopy-labels")
	from := len(clipboardWrites(out))
	sendEach(t, term, end[at(2, 5)])
	if got, want := rawWrite(t, term, out, from, "prefix S Q a Q b"), "Qa lower qb\nmid Qane here\nend QQ"; got != want {
		t.Fatalf("easycopy copied %q, want %q", got, want)
	}
	if err := term.WaitFor(func(s tuitest.Screen) bool {
		_, _, ok := copyCursorCell(s)
		return !ok
	}, uiTimeout); err != nil {
		t.Fatalf("copy mode is still open after the easycopy\n%s", term.Snapshot())
	}
	seekRestored(t, term, first, "after easycopy")

	// Linecopy: lines 1 to 3, then one line with a capital label.
	lineCells := []seekCell{at(0, 0), at(1, 0), at(2, 0), at(3, 0)}
	if err := term.SendKeys(tuitest.Ctrl('b'), "W"); err != nil {
		t.Fatalf("send prefix+W: %v", err)
	}
	waitCopyCursor(t, term, "prefix+W")
	labels := seekLabelsAt(t, term, lineCells, none)
	sendEach(t, term, labels[at(1, 0)])
	// Only the lines from the first pick down are offered again.
	second := seekLabelsAt(t, term, lineCells[1:], none)
	if got := term.Screen().Cell(at(0, 0).col, at(0, 0).row).Content; got != "Q" {
		t.Fatalf("a line above the first pick shows %q, want its own text", got)
	}
	from = len(clipboardWrites(out))
	sendEach(t, term, second[at(3, 0)])
	if got, want := rawWrite(t, term, out, from, "prefix W a b"), "mid Qane here\nend QQ last\nzig Qoo Qxy"; got != want {
		t.Fatalf("linecopy copied %q, want %q", got, want)
	}
	if err := term.WaitFor(func(s tuitest.Screen) bool {
		_, _, ok := copyCursorCell(s)
		return !ok
	}, uiTimeout); err != nil {
		t.Fatalf("copy mode is still open after the linecopy\n%s", term.Snapshot())
	}

	if err := term.SendKeys(tuitest.Ctrl('b'), "W"); err != nil {
		t.Fatalf("send prefix+W: %v", err)
	}
	waitCopyCursor(t, term, "prefix+W")
	labels = seekLabelsAt(t, term, lineCells, none)
	from = len(clipboardWrites(out))
	sendEach(t, term, strings.ToUpper(labels[at(2, 0)]))
	if got, want := rawWrite(t, term, out, from, "prefix W A"), "end QQ last"; got != want {
		t.Fatalf("a capital label copied %q, want %q", got, want)
	}
	alive(t, term, "after easycopy and linecopy")
}
