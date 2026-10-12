package apikey

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSetGetModeAndRemove(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "")
	Path = filepath.Join(t.TempDir(), "sub", "openrouter.key")

	if Get() != "" || Source() != "" {
		t.Fatal("expected no key")
	}
	if err := Set("  sk-or-v1-abc  "); err != nil {
		t.Fatal(err)
	}
	if Get() != "sk-or-v1-abc" || Source() != "file" {
		t.Fatalf("got %q from %q", Get(), Source())
	}
	st, _ := os.Stat(Path)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v, want 0600", st.Mode().Perm())
	}

	// a change made by another process (e.g. --set-key) is picked up
	time.Sleep(10 * time.Millisecond)
	os.WriteFile(Path, []byte("sk-or-v1-new\n"), 0o600)
	os.Chtimes(Path, time.Now().Add(time.Second), time.Now().Add(time.Second))
	if Get() != "sk-or-v1-new" {
		t.Fatalf("stale key %q", Get())
	}

	if err := Set(""); err != nil || Get() != "" {
		t.Fatalf("remove: %v %q", err, Get())
	}
}

func TestEnvOverrides(t *testing.T) {
	Path = filepath.Join(t.TempDir(), "openrouter.key")
	t.Setenv("OPENROUTER_API_KEY", "sk-env")
	if Get() != "sk-env" || Source() != "env" {
		t.Fatal("env should win")
	}
}
