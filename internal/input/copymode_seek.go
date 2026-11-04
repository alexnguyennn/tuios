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
		targets := seekTargets(cm, window, []rune(cm.SeekQuery), fx.alphabet)
		if len(targets) == 0 {
			fx.ShowNotification(fmt.Sprintf("No match for %q", cm.SeekQuery), "info", 0)
			cm.CancelSeek()
			return
		}
		cm.SeekTargets = targets
		fx.ShowNotification("", "info", 0)
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
	// meaning (type the match); a jump has no second action to give it.
	r = unicode.ToLower(r)
	if !strings.ContainsRune(hints.NormalizeAlphabet(fx.alphabet), r) {
		return
	}
	typed := cm.SeekTyped + string(r)
	var hit *terminal.SeekTarget
	prefix := false
	for i := range cm.SeekTargets {
		switch label := cm.SeekTargets[i].Label; {
		case label == typed:
			hit = &cm.SeekTargets[i]
		case strings.HasPrefix(label, typed):
			prefix = true
		}
	}
	if hit == nil && !prefix {
		// A letter that starts no label is a slip, and the seek goes on.
		return
	}
	if hit == nil {
		cm.SeekTyped = typed
		return
	}
	target := *hit
	cm.CancelSeek()
	seekMoveTo(cm, window, target.Line, target.X)
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

// seekTargets finds every match of query on the screen the copy cursor is
// looking at and gives each a label. The match under the cursor is left out,
// because jumping to where the cursor is already tells nothing.
//
// A label is drawn over the match and the cells after it, one cell a letter.
// It must be whole on the screen and on single-width cells, and two labels
// never overlap: a match whose label would not fit is dropped, and the rest
// are labelled again, because fewer matches never need longer labels. Each
// round drops at least one match, so the loop ends.
func seekTargets(cm *terminal.CopyMode, window *terminal.Window, query []rune, alphabet string) []terminal.SeekTarget {
	if len(query) == 0 || window.Terminal == nil {
		return nil
	}
	alphabet = hints.NormalizeAlphabet(alphabet)
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
		for x := range cols {
			if row[x].w == 0 || !seekEqual(row[x].r, query[0], exact) {
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
