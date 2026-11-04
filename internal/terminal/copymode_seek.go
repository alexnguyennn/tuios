package terminal

// SeekTarget is one labelled match of a copy-mode seek (s and S): a cell on
// the screen whose label, typed, moves the copy cursor onto it.
type SeekTarget struct {
	// Line is the absolute line of the match, scrollback first. It is
	// absolute, like a search match, so the target keeps its row when the
	// pane prints more output and the view stays anchored.
	Line int
	// X is the column of the match, which is where the copy cursor lands.
	X int
	// Start is the column of the label's first letter. It is X unless the
	// label would run off the right edge, and then it moves left until it fits.
	Start int
	// Label is the letters to type, never the start of another target's.
	Label string
}

// SeekPurpose is what picking a label does.
type SeekPurpose int

const (
	// SeekMove moves the copy cursor onto the match. It is s and S.
	SeekMove SeekPurpose = iota
	// SeekCopyStart picks the first end of an easycopy: the cursor moves
	// there and a character selection starts. The next pick ends it.
	SeekCopyStart
	// SeekCopyEnd picks the last end of an easycopy and copies the
	// selection, the end character included.
	SeekCopyEnd
	// SeekLineStart picks the first line of a linecopy. A capital letter
	// copies that one line.
	SeekLineStart
	// SeekLineEnd picks the last line of a linecopy and copies the lines.
	SeekLineEnd
)

// SeekActive reports whether a seek is waiting for its characters or for a
// label. While it is, copy mode takes every key as part of the seek.
func (cm *CopyMode) SeekActive() bool {
	return cm != nil && cm.SeekChars > 0
}

// CancelSeek ends a seek without moving the cursor.
func (cm *CopyMode) CancelSeek() {
	if cm == nil {
		return
	}
	cm.SeekChars = 0
	cm.SeekQuery = ""
	cm.SeekTargets = nil
	cm.SeekTyped = ""
	cm.SeekPurpose = SeekMove
	cm.SeekAnchor = Position{}
	cm.SeekUpper = false
}
