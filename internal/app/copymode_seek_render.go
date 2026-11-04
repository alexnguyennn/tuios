package app

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/Gaurav-Gosain/tuios/internal/overlay"
	"github.com/Gaurav-Gosain/tuios/internal/terminal"
)

// A seek label (s and S in copy mode, see internal/input/copymode_seek.go) is
// drawn by the pane's own cell loop, the way a search match is, not by a pass
// over the canvas as hints mode is. The labels are part of copy mode, so they
// live in the pane's cached layer and cost one nil check per frame while no
// seek is open.

// seekMark is one cell of a seek label: the letter and the style it is drawn
// in.
type seekMark struct {
	r     rune
	style lipgloss.Style
}

// seekMarks maps a viewport cell (y*maxX+x) to the label letter drawn on it,
// or returns nil when the pane has no seek with targets open. A label whose
// first letters do not start with what has been typed is not drawn, and the
// letters that have been typed are drawn as a search match, so the next
// letter to press is the loud one.
func (m *OS) seekMarks(window *terminal.Window, marks *markStyles, maxX, maxY, scrollbackLen int) map[int]seekMark {
	cm := window.CopyMode
	if !window.CopyModeVisible() || len(cm.SeekTargets) == 0 {
		return nil
	}
	top := scrollbackLen - window.ScrollbackOffset
	typed := strings.ToLower(cm.SeekTyped)
	out := make(map[int]seekMark, len(cm.SeekTargets)*2)
	for _, t := range cm.SeekTargets {
		if !strings.HasPrefix(t.Label, typed) {
			continue
		}
		y := t.Line - top
		if y < 0 || y >= maxY {
			continue
		}
		for i, r := range t.Label {
			x := t.Start + i
			if x < 0 || x >= maxX {
				continue
			}
			style := marks.match
			if i < len(cm.SeekTyped) {
				style = marks.search
			}
			out[y*maxX+x] = seekMark{r: r, style: style}
		}
	}
	return out
}

// seekLegend is the dock's legend while a seek is open: what to type next,
// and the way out.
func seekLegend(cm *terminal.CopyMode) []overlay.Hint {
	next := overlay.Hint{Key: "char", Label: "to find", Priority: overlay.HintEssential}
	if len(cm.SeekTargets) > 0 {
		next = overlay.Hint{Key: "label", Label: "jump", Priority: overlay.HintEssential}
	}
	return []overlay.Hint{
		next,
		{Key: "esc", Label: "cancel", Priority: overlay.HintEssential},
		{Key: "ctrl+c", Label: "quit", Priority: overlay.HintOptional},
	}
}
