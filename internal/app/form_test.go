package app

import (
	"encoding/json"
	"testing"
)

// The Settings window has sent booleans both as true/false and as 1/0.
func TestSettingsFormAcceptsBoolsAndNumbers(t *testing.T) {
	for _, in := range []string{
		`{"show_hud":true,"cleanup":false,"sound_cues":true,"start_at_login":false,"trailing_space":true,"silence_auto_stop":false}`,
		`{"show_hud":1,"cleanup":0,"sound_cues":1,"start_at_login":0,"trailing_space":1,"silence_auto_stop":0}`,
	} {
		var f settingsForm
		if err := json.Unmarshal([]byte(in), &f); err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if !f.ShowHUD || f.Cleanup || !f.SoundCues || f.StartAtLogin || !f.TrailingSpace || f.SilenceAutoStop {
			t.Fatalf("%s decoded wrong: %+v", in, f)
		}
	}
	var f settingsForm
	if err := json.Unmarshal([]byte(`{"show_hud":"yes"}`), &f); err == nil {
		t.Fatal("garbage should be rejected")
	}
}
