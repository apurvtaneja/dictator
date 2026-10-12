// Package hotkey parses hotkey specs like "option+d" or "ctrl+shift+f6" into a
// macOS virtual key code and a Carbon modifier mask.
package hotkey

import (
	"fmt"
	"strings"
)

// Carbon modifier bits (Events.h).
const (
	cmdKey     = 1 << 8
	shiftKey   = 1 << 9
	optionKey  = 1 << 11
	controlKey = 1 << 12
)

var modTokens = map[string]uint32{
	"cmd": cmdKey, "command": cmdKey, "⌘": cmdKey,
	"ctrl": controlKey, "control": controlKey, "⌃": controlKey,
	"alt": optionKey, "opt": optionKey, "option": optionKey, "⌥": optionKey,
	"shift": shiftKey, "⇧": shiftKey,
}

// ANSI virtual key codes.
var keyCodes = map[string]uint32{
	"a": 0x00, "s": 0x01, "d": 0x02, "f": 0x03, "h": 0x04, "g": 0x05, "z": 0x06,
	"x": 0x07, "c": 0x08, "v": 0x09, "b": 0x0B, "q": 0x0C, "w": 0x0D, "e": 0x0E,
	"r": 0x0F, "y": 0x10, "t": 0x11, "1": 0x12, "2": 0x13, "3": 0x14, "4": 0x15,
	"6": 0x16, "5": 0x17, "=": 0x18, "9": 0x19, "7": 0x1A, "-": 0x1B, "8": 0x1C,
	"0": 0x1D, "]": 0x1E, "o": 0x1F, "u": 0x20, "[": 0x21, "i": 0x22, "p": 0x23,
	"l": 0x25, "j": 0x26, "'": 0x27, "k": 0x28, ";": 0x29, "\\": 0x2A, ",": 0x2B,
	"/": 0x2C, "n": 0x2D, "m": 0x2E, ".": 0x2F, "`": 0x32,
	"space": 0x31, "return": 0x24, "enter": 0x24, "tab": 0x30, "escape": 0x35, "esc": 0x35,
	"f1": 0x7A, "f2": 0x78, "f3": 0x63, "f4": 0x76, "f5": 0x60, "f6": 0x61,
	"f7": 0x62, "f8": 0x64, "f9": 0x65, "f10": 0x6D, "f11": 0x67, "f12": 0x6F,
	"f13": 0x69, "f14": 0x6B, "f15": 0x71, "f16": 0x6A, "f17": 0x40, "f18": 0x4F,
	"f19": 0x50, "f20": 0x5A,
}

// Spec is a parsed hotkey.
type Spec struct {
	KeyCode    uint32
	Modifiers  uint32
	Normalised string
}

// Parse accepts "+"- or "-"-separated tokens, case-insensitive.
func Parse(s string) (Spec, error) {
	// "-" is both a separator and a key; protect a trailing "-" key ("cmd+-").
	raw := strings.ToLower(strings.TrimSpace(s))
	var tokens []string
	for _, t := range strings.FieldsFunc(raw, func(r rune) bool { return r == '+' }) {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if t != "-" && strings.Contains(t, "-") {
			for _, p := range strings.Split(t, "-") {
				if p = strings.TrimSpace(p); p != "" {
					tokens = append(tokens, p)
				}
			}
			continue
		}
		tokens = append(tokens, t)
	}
	if len(tokens) == 0 {
		return Spec{}, fmt.Errorf("empty hotkey")
	}
	var spec Spec
	key := ""
	for _, t := range tokens {
		if m, ok := modTokens[t]; ok {
			spec.Modifiers |= m
			continue
		}
		code, ok := keyCodes[t]
		if !ok {
			return Spec{}, fmt.Errorf("unknown key %q in %q", t, s)
		}
		if key != "" {
			return Spec{}, fmt.Errorf("hotkey %q has more than one non-modifier key", s)
		}
		key, spec.KeyCode = t, code
	}
	if key == "" {
		return Spec{}, fmt.Errorf("hotkey %q has no non-modifier key", s)
	}
	spec.Normalised = strings.Join(tokens, "+")
	return spec, nil
}

// canonical names for keys that have aliases in keyCodes
var preferred = map[string]bool{"return": true, "escape": true}

// Names maps each supported key code to the name Parse accepts for it. The
// Settings window uses it to turn a pressed key into a spec.
func Names() map[uint32]string {
	out := make(map[uint32]string, len(keyCodes))
	for name, code := range keyCodes {
		if cur, ok := out[code]; !ok || preferred[name] && !preferred[cur] {
			out[code] = name
		}
	}
	return out
}
