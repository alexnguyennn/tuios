package input

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Gaurav-Gosain/tuios/internal/app"
	uv "github.com/charmbracelet/ultraviolet"
)

// hostKey decodes seq as the host terminal's bytes and runs it through the
// repairs every host key gets before the input path reads it.
func hostKey(t *testing.T, seq string) tea.Msg {
	t.Helper()
	var d uv.EventDecoder
	n, ev := d.Decode([]byte(seq))
	if n != len(seq) {
		t.Fatalf("%q: decoded %d of %d bytes", seq, n, len(seq))
	}
	o := &app.OS{KeyboardEnhancementsEnabled: true}
	switch e := ev.(type) {
	case uv.KeyPressEvent:
		return o.FixHostKey(tea.KeyPressMsg(e))
	case uv.KeyReleaseEvent:
		return o.FixHostKey(tea.KeyReleaseMsg(e))
	}
	t.Fatalf("%q decoded to %T, want a key event", seq, ev)
	return nil
}

// TestHostKeyTextFromEveryTerminal is the WezTerm Esc/Enter report. Under the
// kitty protocol's associated-text flag (tuios asks for flags 29 while it
// reads keys itself) WezTerm attaches the key's control character as text:
// Esc is CSI 27;1;27u. Bubble Tea spells a key by its text, so Esc read as
// "\x1b" and the palette, copy mode and every menu never saw it. Each row is
// the bytes a real terminal sends for one key under the flags named, and
// asserts how tuios reads the key and what a pane is sent for it.
//
// kitty is what a pane that pushed CSI >31u gets, legacy what a pane with no
// flags gets. A WezTerm row must give the same bytes as the spec-correct form
// of the same key: dropping the text loses nothing a pane needs.
//
// Negative control: return k unchanged from dropSpuriousKeyText and every
// WezTerm row reads as its control character or as the bare letter.
func TestHostKeyTextFromEveryTerminal(t *testing.T) {
	cases := []struct {
		name    string
		seq     string
		key     string // msg.String() as tuios's overlays compare it
		command string // commandKey, which copy mode and the fixed panels read
		text    string // msg.Text left for a field being typed into
		kitty   string
		legacy  string
	}{
		// WezTerm, flags 29: associated text on every key, control codes included.
		{"wezterm esc, 29", "\x1b[27;1;27u", "esc", "esc", "", "\x1b[27u", "\x1b"},
		{"wezterm enter, 29", "\x1b[13;1;13u", "enter", "enter", "", "\x1b[13u", "\r"},
		{"wezterm tab, 29", "\x1b[9;1;9u", "tab", "tab", "", "\x1b[9u", "\t"},
		{"wezterm shift+tab, 29", "\x1b[9;2;9u", "shift+tab", "shift+tab", "", "\x1b[9;2u", "\x1b[Z"},
		{"wezterm backspace, 29", "\x1b[127::8;1;8u", "backspace", "backspace", "", "\x1b[127u", "\x7f"},
		{"wezterm ctrl+c, 29", "\x1b[99;5;99u", "ctrl+c", "ctrl+c", "", "\x1b[99;5u", "\x03"},
		{"wezterm ctrl+shift+a, 29", "\x1b[97:65;6;65u", "ctrl+shift+a", "ctrl+shift+a", "", "\x1b[97:65;6u", "\x01"},
		{"wezterm ctrl+alt+c, 29", "\x1b[99;7;99u", "ctrl+alt+c", "ctrl+alt+c", "", "\x1b[99;7u", "\x03"},
		{"wezterm alt+x, 29", "\x1b[120;3;120u", "alt+x", "alt+x", "", "\x1b[120;3u", "\x1bx"},
		{"wezterm super+a, 29", "\x1b[97;9;97u", "super+a", "super+a", "", "\x1b[97;9u", "a"},
		{"wezterm q, 29", "\x1b[113;1;113u", "q", "q", "q", "\x1b[113;1;113u", "q"},
		{"wezterm shifted a, 29", "\x1b[97:65;2;65u", "A", "A", "A", "\x1b[97:65;2;65u", "A"},
		// Alt+Shift keeps its capital: a legacy pane can only get it from the text.
		{"wezterm alt+shift+x, 29", "\x1b[120:88;4;88u", "X", "alt+shift+x", "X", "\x1b[120:88;4;88u", "\x1bX"},

		// kitty and Ghostty, spec-correct. Nothing changes.
		{"kitty esc", "\x1b[27u", "esc", "esc", "", "\x1b[27u", "\x1b"},
		{"kitty esc, explicit mods (flags 5)", "\x1b[27;1u", "esc", "esc", "", "\x1b[27u", "\x1b"},
		{"kitty enter, 29", "\x1b[13u", "enter", "enter", "", "\x1b[13u", "\r"},
		{"kitty a, 29", "\x1b[97;;97u", "a", "a", "a", "\x1b[97;1;97u", "a"},
		{"kitty shifted a, 29", "\x1b[97:65;2;65u", "A", "A", "A", "\x1b[97:65;2;65u", "A"},
		{"kitty ctrl+c, 5 and 29", "\x1b[99;5u", "ctrl+c", "ctrl+c", "", "\x1b[99;5u", "\x03"},

		// Legacy, and flags 5 for the keys that keep their legacy form.
		{"legacy esc", "\x1b", "esc", "esc", "", "\x1b[27u", "\x1b"},
		{"legacy enter, also flags 5", "\r", "enter", "enter", "", "\x1b[13u", "\r"},
		{"legacy ctrl+c", "\x03", "ctrl+c", "ctrl+c", "", "\x1b[99;5u", "\x03"},
		{"legacy q, also flags 5", "q", "q", "q", "q", "\x1b[113;1;113u", "q"},

		// A Ukrainian layout: ш on the I key. The character is typed, the
		// fixed panels read the I key, and Ctrl sends the I key's control code.
		{"ukrainian sha, 29", "\x1b[1096::105;;1096u", "ш", "i", "ш", "\x1b[1096::105;1;1096u", "ш"},
		{"wezterm ukrainian ctrl+sha, 29", "\x1b[1096::105;5;1096u", "ш", "ctrl+i", "ш", "\x1b[1096::105;5;1096u", "\t"},

		// A character macOS composed for Option keeps its text: it is typing.
		{"option+3 composes pound, 29", "\x1b[163::51;3;163u", "£", "alt+3", "£", "\x1b[163::51;3;163u", "£"},

		// Text no key produced (an input method commit) is left alone.
		{"ime commit, 29", "\x1b[0;;20320:22909u", "你好", "你好", "你好", "你好", "你好"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			msg, ok := hostKey(t, c.seq).(tea.KeyPressMsg)
			if !ok {
				t.Fatalf("%q is not a key press", c.seq)
			}
			read := readKey(msg)
			if got := read.String(); got != c.key {
				t.Errorf("%q reads as %q, want %q", c.seq, got, c.key)
			}
			if got := commandKey(read); got != c.command {
				t.Errorf("%q: command key %q, want %q", c.seq, got, c.command)
			}
			if msg.Text != c.text {
				t.Errorf("%q: text %q, want %q", c.seq, msg.Text, c.text)
			}
			if got := forwardNative(t, pushAll, msg); got != c.kitty {
				t.Errorf("%q: kitty pane got %q, want %q", c.seq, got, c.kitty)
			}
			if got := forwardDaemon(t, pushAll, msg); got != c.kitty {
				t.Errorf("%q: daemon kitty pane got %q, want %q", c.seq, got, c.kitty)
			}
			if got := forwardNative(t, "", msg); got != c.legacy {
				t.Errorf("%q: legacy pane got %q, want %q", c.seq, got, c.legacy)
			}
			if got := forwardDaemon(t, "", msg); got != c.legacy {
				t.Errorf("%q: daemon legacy pane got %q, want %q", c.seq, got, c.legacy)
			}
		})
	}
}

// TestHostKeyReleaseTextFromEveryTerminal is the release side: a terminal
// reporting event types sends :3 for a release, and WezTerm attaches the
// same control text to it.
//
// Negative control: return k unchanged from dropSpuriousKeyText and the
// WezTerm release reads as "\x1b".
func TestHostKeyReleaseTextFromEveryTerminal(t *testing.T) {
	cases := []struct {
		name, seq, key string
	}{
		{"wezterm esc release", "\x1b[27;1:3;27u", "esc"},
		{"wezterm enter release", "\x1b[13;1:3;13u", "enter"},
		{"wezterm ctrl+c release", "\x1b[99;5:3;99u", "ctrl+c"},
		{"kitty esc release", "\x1b[27;1:3u", "esc"},
		{"kitty q release", "\x1b[113;1:3u", "q"},
	}
	for _, c := range cases {
		rel, ok := hostKey(t, c.seq).(tea.KeyReleaseMsg)
		if !ok {
			t.Fatalf("%s: %q is not a key release", c.name, c.seq)
		}
		if got := rel.String(); got != c.key {
			t.Errorf("%s: %q reads as %q, want %q", c.name, c.seq, got, c.key)
		}
	}
}
