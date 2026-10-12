package audio

import (
	"os"
	"path/filepath"
	"testing"
)

// Uses a real AAC file when DICTATOR_TEST_M4A points at one (macOS-only codec path).
func TestToWAV(t *testing.T) {
	in := os.Getenv("DICTATOR_TEST_M4A")
	if in == "" {
		t.Skip("set DICTATOR_TEST_M4A to an .m4a file")
	}
	out := filepath.Join(t.TempDir(), "x.wav")
	if err := ToWAV(in, out); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(out)
	if err != nil || st.Size() < 44 {
		t.Fatalf("no WAV written: %v", err)
	}
	t.Logf("WAV %d bytes", st.Size())
}
