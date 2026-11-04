package app

import tea "charm.land/bubbletea/v2"

// kittyLegacyFormKey reports whether the kitty keyboard protocol sends code in
// its legacy form (CSI <n> ; <mods> ~, or CSI 1 ; <mods> <letter>) rather than
// as CSI u. These are the arrows, the navigation block and F1 to F12.
func kittyLegacyFormKey(code rune) bool {
	switch code {
	case tea.KeyUp, tea.KeyDown, tea.KeyLeft, tea.KeyRight, tea.KeyBegin,
		tea.KeyHome, tea.KeyEnd, tea.KeyInsert, tea.KeyDelete,
		tea.KeyPgUp, tea.KeyPgDown:
		return true
	}
	return code >= tea.KeyF1 && code <= tea.KeyF12
}

// fixKittyLegacyMods puts Super back on a key the host sent in the kitty
// protocol's legacy form.
//
// Under the kitty protocol the modifier parameter of every key uses the kitty
// bits: 8 is Super and 32 is Meta. Bubble Tea's decoder reads the legacy forms
// with the xterm bits instead, where 8 is Meta, so Cmd+F12 arrived as
// meta+f12 and a super+f12 binding could never match. CSI u keys are decoded
// with the kitty bits already and are left alone, as is every key from a host
// that has not granted the protocol, where 8 really is xterm's Meta.
func fixKittyLegacyMods(k tea.Key, hostKitty bool) tea.Key {
	if !hostKitty || k.Mod&(tea.ModMeta|tea.ModSuper) == 0 || !kittyLegacyFormKey(k.Code) {
		return k
	}
	meta, super := k.Mod&tea.ModMeta != 0, k.Mod&tea.ModSuper != 0
	k.Mod &^= tea.ModMeta | tea.ModSuper
	if meta {
		k.Mod |= tea.ModSuper
	}
	if super {
		k.Mod |= tea.ModMeta
	}
	return k
}

// FixHostKey applies the repairs in fixHostKey to a key event from the host
// terminal. Every other message is returned unchanged. Keys that tuios makes
// itself (send-keys, tapes) do not come through here, since they carry the
// modifiers they were given.
//
// This is the one place a key from the host enters the input path, so every
// reader after it (bindings, overlays, copy mode, text fields and the encoders
// that write the key to a pane) sees the key as fixed here.
func (m *OS) FixHostKey(msg tea.Msg) tea.Msg {
	switch k := msg.(type) {
	case tea.KeyPressMsg:
		return tea.KeyPressMsg(fixHostKey(tea.Key(k), m.KeyboardEnhancementsEnabled))
	case tea.KeyReleaseMsg:
		return tea.KeyReleaseMsg(fixHostKey(tea.Key(k), m.KeyboardEnhancementsEnabled))
	}
	return msg
}

// fixHostKey is every repair a key from the host gets, in order. The text-only
// key goes before the chord text: it turns key 0's ctrl+space back into the
// text it carries, which must then not be read as text on a Ctrl chord.
func fixHostKey(k tea.Key, hostKitty bool) tea.Key {
	return dropSpuriousKeyText(fixKittyTextOnlyKey(fixKittyLegacyMods(k, hostKitty)))
}

// dropSpuriousKeyText removes associated text the host attached to a key that
// typed nothing.
//
// Bubble Tea spells a key by its text whenever it has one, so a key that
// carries text no binding expects is misread everywhere tuios compares
// String(). Two kinds reach tuios under the kitty protocol's associated-text
// flag, which it asks for while it reads keys itself:
//
//   - A control character. WezTerm sends Esc as CSI 27;1;27u, Enter as
//     CSI 13;1;13u, Tab as CSI 9;1;9u and Backspace as CSI 127::8;1;8u. The
//     protocol says associated text must not contain control codes, and the
//     decoder keeps it all the same, so those keys read as "\x1b", "\r", "\t"
//     and "\b": the palette, copy mode and every menu never saw Esc or Enter.
//     kitty and Ghostty send no text for them. The text goes whatever was held.
//   - The key's own character on a chord. WezTerm sends Ctrl+C as CSI 99;5;99u,
//     and the decoder's attempt to drop text on a chord runs before it reads
//     the text, so Ctrl+C read as "c". kitty and Ghostty send none here
//     either. The text goes only when it adds nothing to the key: one ASCII
//     character equal to the key's code (or, under Ctrl, the capital of a
//     letter key, which the legacy encoder reads from the key and not from
//     the text). Anything else on a chord is kept, because it is typing:
//     a character macOS composed for Option (£ on Option+3), AltGr on Windows
//     (Ctrl+Alt with €), a non-Latin key, and Alt+Shift+x, whose capital a
//     pane without the kitty protocol can only get from the text.
//
// Shift and the lock keys are not chords: shifted "A" keeps its text. Neither
// is text with no modifier, so a plain "q" or an IME commit is left alone.
func dropSpuriousKeyText(k tea.Key) tea.Key {
	if k.Text == "" {
		return k
	}
	for _, r := range k.Text {
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			k.Text = ""
			return k
		}
	}
	chord := k.Mod &^ (tea.ModShift | tea.ModCapsLock | tea.ModNumLock | tea.ModScrollLock)
	if chord == 0 || len(k.Text) != 1 {
		return k
	}
	r := rune(k.Text[0])
	switch {
	case r == k.Code:
		k.Text = ""
	case chord&tea.ModCtrl != 0 && r >= 'A' && r <= 'Z' && r+('a'-'A') == k.Code:
		k.Text = ""
	}
	return k
}

// fixKittyTextOnlyKey turns a kitty text event with no key back into the text
// it carries.
//
// With report-all-keys and associated text on, the kitty protocol sends text
// that no key produced (an input method commit, composed text) as key number
// 0: CSI 0 ; mods ; codepoints u. The decoder looks 0 up in its legacy table,
// where it is NUL, so the text arrived as ctrl+space and a pane got a NUL or
// CSI 32;5u instead of the characters typed. tuios asks for that mode while it
// reads keys itself and while a hold key or a pane needs every key, so this is
// how IME text reaches it then.
//
// A real Ctrl+Space carries no text, or a space, so any other text on a
// ctrl+space can only have come from key 0.
func fixKittyTextOnlyKey(k tea.Key) tea.Key {
	if k.Code != tea.KeySpace || k.Mod&tea.ModCtrl == 0 || k.Text == "" || k.Text == " " {
		return k
	}
	runes := []rune(k.Text)
	if len(runes) == 1 {
		k.Code = runes[0]
	} else {
		k.Code = tea.KeyExtended
	}
	k.Mod &^= tea.ModCtrl
	return k
}
