package input

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/Gaurav-Gosain/tuios/internal/app"
	"github.com/Gaurav-Gosain/tuios/internal/hints"
	"github.com/Gaurav-Gosain/tuios/internal/terminal"
	uv "github.com/charmbracelet/ultraviolet"
)

// Seek in copy mode: s asks for one character and S for two, labels every
// match on the screen, and typing a label moves the real copy cursor onto its
// match. It is the easymotion jump of tmux-copy-toolkit, and the copy cursor
// it moves is the one every other copy-mode key works from, so a v selection
// or a yank carries on from where the label put it.
//
// The labels are made by the same engine hints mode uses (internal/hints), in
// the alphabet [hints] alphabet names, nearest match first: a match on the
// cursor's row beats one a row away, whatever the columns.
//
// The matching follows copytk's default: a lowercase character matches both
// cases, and a character typed with a capital matches only that capital.

// seekPrompt is the dock message that says a seek is waiting for characters.
func seekPrompt(chars int) string {
	if chars == 1 {
		return "s: type a character"
	}
	return "S: type two characters"
}

// beginSeek starts a seek for chars characters.
func beginSeek(cm *terminal.CopyMode, chars int, fx *copyModeEffects) {
	cm.CancelSeek()
	cm.SeekChars = chars
	fx.ShowNotification(seekPrompt(chars), "info", 0)
	fx.InvalidateCache()
}

// seekRune is the one printable character a key typed, or false.
func seekRune(msg tea.KeyPressMsg) (rune, bool) {
	key := msg.Key()
	if key.Mod.Contains(tea.ModCtrl) || key.Mod.Contains(tea.ModAlt) || key.Mod.Contains(tea.ModSuper) {
		return 0, false
	}
	r, size := utf8.DecodeRuneInString(key.Text)
	if key.Text == "" || size != len(key.Text) || r == utf8.RuneError || !unicode.IsPrint(r) {
		return 0, false
	}
	return r, true
}

// handleSeekKey takes a key while a seek is under way. It is called from the
// dispatcher, so the seek has every key: a key it does not use is dropped,
// never passed on to the copy-mode motions. ctrl+c is the dispatcher's, and
// leaves copy mode from here too.
//
//   - esc gives up, and the cursor stays where it was
//   - before the targets are drawn, a printable key is a character to find
//   - after, a letter of the alphabet is a letter of a label, and backspace
//     takes a letter back
func handleSeekKey(msg tea.KeyPressMsg, cm *terminal.CopyMode, window *terminal.Window, fx *copyModeEffects) {
	fx.InvalidateCache()
	if msg.String() == "esc" {
		cm.CancelSeek()
		fx.ShowNotification("", "info", 0)
		return
	}
	if len(cm.SeekTargets) == 0 {
		r, ok := seekRune(msg)
		if !ok {
			return
		}
		cm.SeekQuery += string(r)
		if utf8.RuneCountInString(cm.SeekQuery) < cm.SeekChars {
			return
		}
		showSeekTargets(cm, window, fx)
		return
	}
	if msg.String() == "backspace" {
		if cm.SeekTyped != "" {
			cm.SeekTyped = cm.SeekTyped[:len(cm.SeekTyped)-1]
		}
		return
	}
	r, ok := seekRune(msg)
	if !ok {
		return
	}
	// Shift on a label letter is the same letter. Hints mode gives it a
	// meaning (type the match); a jump has no second action to give it. A
	// linecopy reads it as one line.
	if !strings.ContainsRune(hints.NormalizeAlphabet(fx.alphabet), unicode.ToLower(r)) {
		return
	}
	typed := cm.SeekTyped + string(r)
	match := strings.ToLower(typed)
	var hit *terminal.SeekTarget
	prefix := false
	for i := range cm.SeekTargets {
		switch label := cm.SeekTargets[i].Label; {
		case label == match:
			hit = &cm.SeekTargets[i]
		case strings.HasPrefix(label, match):
			prefix = true
		}
	}
	if hit == nil && !prefix {
		// A letter that starts no label is a slip, and the seek goes on.
		return
	}
	cm.SeekTyped = typed
	if hit == nil {
		return
	}
	pickSeekTarget(cm, window, *hit, fx)
}

// showSeekTargets labels what the typed characters (or, for a linecopy, every
// line) match, or says nothing matched and ends the seek.
func showSeekTargets(cm *terminal.CopyMode, window *terminal.Window, fx *copyModeEffects) {
	req := seekRequest{query: []rune(cm.SeekQuery), alphabet: fx.alphabet}
	switch cm.SeekPurpose {
	case terminal.SeekLineStart, terminal.SeekLineEnd:
		req.lines = true
	}
	switch cm.SeekPurpose {
	case terminal.SeekCopyEnd, terminal.SeekLineEnd:
		// The second pick comes after the first. A line may be the same line.
		anchor := cm.SeekAnchor
		req.after = &anchor
	}
	targets := seekTargets(cm, window, req)
	if len(targets) == 0 {
		if req.lines {
			fx.ShowNotification("No line to pick", "info", 0)
		} else {
			fx.ShowNotification(fmt.Sprintf("No match for %q", cm.SeekQuery), "info", 0)
		}
		cm.CancelSeek()
		return
	}
	cm.SeekTargets = targets
	cm.SeekTyped = ""
	fx.ShowNotification("", "info", 0)
}

// pickSeekTarget does what a completed label asked for. A plain seek moves the
// cursor. An easycopy or a linecopy takes two picks: the first starts a
// selection and asks for the second, and the second copies it.
func pickSeekTarget(cm *terminal.CopyMode, window *terminal.Window, t terminal.SeekTarget, fx *copyModeEffects) {
	purpose, upper := cm.SeekPurpose, strings.ContainsFunc(cm.SeekTyped, unicode.IsUpper)
	switch purpose {
	case terminal.SeekMove:
		cm.CancelSeek()
		seekMoveTo(cm, window, t.Line, t.X)
	case terminal.SeekCopyStart:
		cm.CancelSeek()
		seekMoveTo(cm, window, t.Line, t.X)
		enterVisualChar(cm, window)
		continueSeek(cm, terminal.SeekCopyEnd, 1, terminal.Position{X: t.X, Y: t.Line}, fx)
	case terminal.SeekLineStart:
		cm.CancelSeek()
		start := t.Line
		for start > 0 && selectionRowWraps(window, start-1) {
			start--
		}
		seekMoveTo(cm, window, start, 0)
		enterVisualLine(cm, window)
		if upper {
			seekLineEnd(cm, window, t.Line)
			seekYank(cm, window, fx)
			return
		}
		continueSeek(cm, terminal.SeekLineEnd, 1, terminal.Position{X: 0, Y: start}, fx)
		showSeekTargets(cm, window, fx)
	case terminal.SeekCopyEnd, terminal.SeekLineEnd:
		cm.CancelSeek()
		if purpose == terminal.SeekLineEnd {
			seekLineEnd(cm, window, t.Line)
		} else {
			seekMoveTo(cm, window, t.Line, t.X)
		}
		seekYank(cm, window, fx)
	}
}

// seekLineEnd extends linecopy to the hard line break, not the picked screen
// row. Use the same soft-wrap metadata as extraction, across history/live
// boundaries too; cap at the buffer end when the final row is still wrapped.
func seekLineEnd(cm *terminal.CopyMode, window *terminal.Window, line int) {
	last := window.ScrollbackLen() + window.Terminal.Height() - 1
	for line < last && selectionRowWraps(window, line) {
		line++
	}
	seekMoveTo(cm, window, line, 0)
}

// continueSeek starts the second pick of an easycopy or a linecopy.
func continueSeek(cm *terminal.CopyMode, purpose terminal.SeekPurpose, chars int, anchor terminal.Position, fx *copyModeEffects) {
	cm.SeekChars = chars
	cm.SeekPurpose = purpose
	cm.SeekAnchor = anchor
	if purpose == terminal.SeekCopyEnd {
		fx.ShowNotification("Easycopy: type the character the copy ends on", "info", 0)
	}
}

// seekYank copies the selection and leaves copy mode, as y does and then q.
func seekYank(cm *terminal.CopyMode, window *terminal.Window, fx *copyModeEffects) {
	text := extractVisualText(cm, window)
	fx.Flash(cm.VisualStart, cm.VisualEnd)
	cm.State = terminal.CopyModeNormal
	fx.ShowNotification(fmt.Sprintf("Yanked %d chars", len(text)), "success", 0)
	fx.InvalidateCache()
	fx.SetClipboard(text)
	fx.ExitCopyMode()
}

// seekMoveTo puts the copy cursor on absolute line absY, column x, scrolling
// the view the least that shows it, and in a visual selection moves the free
// end of the selection with it.
func seekMoveTo(cm *terminal.CopyMode, window *terminal.Window, absY, x int) {
	showAbsoluteRow(cm, window, absY)
	cm.CursorX = x
	if cm.State.InVisual() {
		updateVisualEnd(cm, window)
	}
}

// seekCell is one cell of a row as seek reads it.
type seekCell struct {
	r rune
	w int
}

// seekRow reads one absolute line into cells: the first rune of each cell and
// its width. A continuation cell of a wide glyph has width 0, a cell with
// nothing in it is a space, and a line shorter than cols is padded with spaces
// of width 1, as the pane draws it.
func seekRow(window *terminal.Window, absY, cols int) []seekCell {
	row := make([]seekCell, cols)
	scrollbackLen := window.ScrollbackLen()
	for x := range cols {
		c := getCellAtAbs(window, scrollbackLen, absY, x)
		switch {
		case c == nil || (c.Content == "" && c.Width != 0):
			row[x] = seekCell{' ', 1}
		case c.Width == 0:
			row[x] = seekCell{0, 0}
		default:
			r, _ := utf8.DecodeRuneInString(c.Content)
			row[x] = seekCell{r, c.Width}
		}
	}
	return row
}

// seekEqual compares one cell's rune with a typed one. exact is set when the
// query has a capital, and then case counts.
func seekEqual(cell, typed rune, exact bool) bool {
	if exact {
		return cell == typed
	}
	return unicode.ToLower(cell) == unicode.ToLower(typed)
}

// seekRequest is what a seek looks for.
type seekRequest struct {
	// query is the characters to find, one or two.
	query []rune
	// lines asks for the start of every line instead of a match.
	lines bool
	// after, when set, leaves out every match before this position. A line
	// pick leaves out the lines above it and keeps the line itself.
	after    *terminal.Position
	alphabet string
}

// pos reports whether a match at line absY, column x is allowed by after.
func (r seekRequest) pos(absY, x int) bool {
	if r.after == nil {
		return true
	}
	if r.lines {
		return absY >= r.after.Y
	}
	return absY > r.after.Y || (absY == r.after.Y && x > r.after.X)
}

// seekBlank reports whether a row holds nothing but spaces.
func seekBlank(row []seekCell) bool {
	for _, c := range row {
		if c.w != 0 && c.r != ' ' {
			return false
		}
	}
	return true
}

// seekTargets finds every match of query on the screen the copy cursor is
// looking at and gives each a label. The match under the cursor is left out,
// because jumping to where the cursor is already tells nothing.
//
// A label is drawn over the match and the cells after it, one cell a letter.
// It must be whole on the screen and on single-width cells, and two labels
// never overlap: a match whose label would not fit is dropped, and the rest
// are labelled again, because fewer matches never need longer labels. Each
// round drops at least one match, so the loop ends.
func seekTargets(cm *terminal.CopyMode, window *terminal.Window, req seekRequest) []terminal.SeekTarget {
	query := req.query
	if (len(query) == 0 && !req.lines) || window.Terminal == nil {
		return nil
	}
	alphabet := hints.NormalizeAlphabet(req.alphabet)
	scrollbackLen := window.ScrollbackLen()
	total := scrollbackLen + window.Terminal.Height()
	top := scrollbackLen - cm.ScrollOffset
	cols := window.LastContentCol() + 1
	exact := slices.ContainsFunc(query, unicode.IsUpper)
	cursorLine := getAbsoluteY(cm, window)

	type candidate struct {
		line, x  int
		distance int
		cells    []seekCell
	}
	var cands []candidate
	for vy := 0; vy <= window.LastContentRow(); vy++ {
		absY := top + vy
		if absY < 0 || absY >= total {
			continue
		}
		row := seekRow(window, absY, cols)
		if req.lines {
			// A line is picked by its first column, and a line with nothing
			// on it is not worth a label.
			if !seekBlank(row) && req.pos(absY, 0) {
				cands = append(cands, candidate{
					line: absY, x: 0, cells: row,
					distance: abs(absY-cursorLine) * cols,
				})
			}
			continue
		}
		for x := range cols {
			if row[x].w == 0 || !seekEqual(row[x].r, query[0], exact) {
				continue
			}
			if !req.pos(absY, x) {
				continue
			}
			if len(query) > 1 {
				next := x + row[x].w
				if next >= cols || row[next].w == 0 || !seekEqual(row[next].r, query[1], exact) {
					continue
				}
			}
			if absY == cursorLine && x == cm.CursorX {
				continue
			}
			// A row away is further than any column on the cursor's row,
			// as copytk sorts them.
			cands = append(cands, candidate{
				line: absY, x: x, cells: row,
				distance: abs(absY-cursorLine)*cols + abs(x-cm.CursorX),
			})
		}
	}
	slices.SortStableFunc(cands, func(a, b candidate) int { return a.distance - b.distance })

	var out []terminal.SeekTarget
	for len(cands) > 0 {
		targets := make([]hints.Target, len(cands))
		for i, c := range cands {
			targets[i] = hints.Target{
				Text:     string(query),
				Key:      fmt.Sprintf("%d:%d", c.line, c.x),
				Distance: c.distance,
			}
		}
		labels := hints.Assign(targets, alphabet)
		occupied := map[int][]bool{}
		out = out[:0]
		kept := cands[:0:0]
		for i, c := range cands {
			label := labels[i]
			start := min(c.x, cols-len(label))
			if start < 0 || !seekFits(c.cells, start, len(label)) {
				continue
			}
			used := occupied[c.line]
			if used == nil {
				used = make([]bool, cols)
				occupied[c.line] = used
			}
			if slices.Contains(used[start:start+len(label)], true) {
				continue
			}
			for j := range len(label) {
				used[start+j] = true
			}
			kept = append(kept, c)
			out = append(out, terminal.SeekTarget{Line: c.line, X: c.x, Start: start, Label: label})
		}
		if len(kept) == len(cands) {
			return out
		}
		cands = kept
	}
	return nil
}

// seekFits reports whether n cells from start are all single-width, so a
// label drawn over them leaves no half of a wide glyph behind.
func seekFits(cells []seekCell, start, n int) bool {
	if start+n > len(cells) {
		return false
	}
	for _, c := range cells[start : start+n] {
		if c.w != 1 {
			return false
		}
	}
	return true
}

// getCellAtAbs is the cell at column x of absolute line absY, from the
// scrollback or the live screen, or nil past the end of a line.
func getCellAtAbs(window *terminal.Window, scrollbackLen, absY, x int) *uv.Cell {
	if absY < scrollbackLen {
		line := window.ScrollbackLine(absY)
		if x < len(line) {
			return &line[x]
		}
		return nil
	}
	return window.Terminal.CellAt(x, absY-scrollbackLen)
}

// seekAlphabet is the [hints] alphabet in force, which seek labels share.
func seekAlphabet(o *app.OS) string {
	if o == nil || o.UserConfig == nil {
		return ""
	}
	return o.UserConfig.Hints.LabelAlphabet()
}

// handleCopyModeEasycopy is copy_mode_easycopy: from anywhere, enter copy mode
// and pick the two ends of a copy by the characters they start with. It is
// copytk's easycopy: the first pick starts a selection, the second ends it
// and copies, the character it ends on included, and copy mode closes.
func handleCopyModeEasycopy(_ tea.KeyPressMsg, o *app.OS) (*app.OS, tea.Cmd) {
	return startSeekAction(o, terminal.SeekCopyStart, 1)
}

// handleCopyModeLinecopy is copy_mode_linecopy: enter copy mode and pick the
// first and the last line of a copy by their labels, or one line with a label
// typed in capitals. It is copytk's linecopy.
func handleCopyModeLinecopy(_ tea.KeyPressMsg, o *app.OS) (*app.OS, tea.Cmd) {
	return startSeekAction(o, terminal.SeekLineStart, 0)
}

// startSeekAction enters copy mode when the pane is not in it and starts a
// two-pick copy. chars is how many characters the first pick asks for; a
// linecopy asks for none and labels the lines at once.
func startSeekAction(o *app.OS, purpose terminal.SeekPurpose, chars int) (*app.OS, tea.Cmd) {
	fw := o.GetFocusedWindow()
	if fw == nil || fw.Terminal == nil || o.MultiCopy.Has(fw.ID) {
		return o, nil
	}
	if !fw.CopyModeVisible() {
		o.EnterCopyModeFocused()
		fw = o.GetFocusedWindow()
		if fw == nil || !fw.CopyModeVisible() {
			return o, nil
		}
	}
	cm := fw.CopyMode
	fx := &copyModeEffects{alphabet: seekAlphabet(o)}
	// The labels read the cell buffer, so they are made under the same shared
	// lock every copy-mode key holds.
	func() {
		fw.RLockIO()
		defer fw.RUnlockIO()
		cm.CancelSeek()
		cm.State = terminal.CopyModeNormal
		cm.SeekPurpose = purpose
		if chars == 0 {
			cm.SeekChars = 1
			showSeekTargets(cm, fw, fx)
			return
		}
		cm.SeekChars = chars
		fx.ShowNotification("Easycopy: type the character the copy starts on", "info", 0)
		fx.InvalidateCache()
	}()
	return fx.apply(o, fw)
}
