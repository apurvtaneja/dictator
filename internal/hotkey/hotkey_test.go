package hotkey

import "testing"

func TestParse(t *testing.T) {
	cases := map[string]Spec{
		"option+d":      {0x02, optionKey, "option+d"},
		"Ctrl+Shift+F6": {0x61, controlKey | shiftKey, "ctrl+shift+f6"},
		"cmd-space":     {0x31, cmdKey, "cmd+space"},
		"cmd+-":         {0x1B, cmdKey, "cmd+-"},
	}
	for in, want := range cases {
		got, err := Parse(in)
		if err != nil || got != want {
			t.Errorf("Parse(%q) = %+v, %v; want %+v", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "option", "option+zz", "a+b"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%q) should fail", bad)
		}
	}
}

func TestNamesRoundTrip(t *testing.T) {
	names := Names()
	if names[0x24] != "return" || names[0x35] != "escape" {
		t.Errorf("aliases: got %q, %q", names[0x24], names[0x35])
	}
	for code, name := range names {
		got, err := Parse("cmd+" + name)
		if err != nil || got.KeyCode != code {
			t.Errorf("Parse(cmd+%s) = %+v, %v; want code %#x", name, got, err, code)
		}
	}
}
