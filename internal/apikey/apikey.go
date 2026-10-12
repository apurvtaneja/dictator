// Package apikey stores the OpenRouter API key in ~/.config/dictator/openrouter.key,
// readable only by the user (0600) -- the same model as the gh, aws and openai CLIs.
//
// Not the Keychain: an unsigned or ad-hoc-signed app gets a new code identity on
// every build, and the Keychain then asks for the login password again each time.
//
// OPENROUTER_API_KEY in the environment overrides the file.
package apikey

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/apurvtaneja/dictator/internal/config"
)

// Path is the key file (a var so tests can point it elsewhere).
var Path = filepath.Join(config.Dir, "openrouter.key")

var (
	mu      sync.Mutex
	cached  string
	modTime time.Time
)

// Get returns the key, or "" if none. The file is re-read only when it changes,
// so a key set with --set-key while the app runs is picked up.
func Get() string {
	if k := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY")); k != "" {
		return k
	}
	mu.Lock()
	defer mu.Unlock()
	st, err := os.Stat(Path)
	if err != nil {
		cached, modTime = "", time.Time{}
		return ""
	}
	if !st.ModTime().Equal(modTime) {
		data, err := os.ReadFile(Path)
		if err != nil {
			return cached
		}
		cached, modTime = strings.TrimSpace(string(data)), st.ModTime()
	}
	return cached
}

// Source says where the key comes from: "env", "file" or "" (none).
func Source() string {
	switch {
	case strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY")) != "":
		return "env"
	case Get() != "":
		return "file"
	}
	return ""
}

// Set stores the key; an empty key deletes the file.
func Set(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		if err := os.Remove(Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(Path), 0o700); err != nil {
		return err
	}
	tmp := Path + ".tmp"
	if err := os.WriteFile(tmp, []byte(key+"\n"), 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil { // WriteFile keeps an existing file's mode
		return err
	}
	return os.Rename(tmp, Path)
}

// Masked renders a key for display: "sk-or-v1-…c3f9".
func Masked(key string) string {
	if len(key) <= 12 {
		return strings.Repeat("•", len(key))
	}
	return key[:9] + "…" + key[len(key)-4:]
}
