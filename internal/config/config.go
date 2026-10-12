// Package config loads and saves ~/.config/dictator/config.json.
//
// The file is shared with the earlier Python version, so the same key names are
// used and keys this version doesn't know are preserved on save. Saves are
// per-key (Update): the running Config may carry one-off CLI overrides that must
// never be written back.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

type Config struct {
	Hotkey        string `json:"hotkey"`
	Mode          string `json:"mode"` // "toggle" | "ptt"
	MaxSeconds    int    `json:"max_seconds"`
	TrailingSpace bool   `json:"trailing_space"`

	SilenceAutoStop  bool    `json:"silence_auto_stop"`
	SilenceMs        int     `json:"silence_ms"`
	SilenceMinMs     int     `json:"silence_min_ms"`
	SilenceThreshold float64 `json:"silence_threshold"`

	ShowHUD   bool   `json:"show_hud"`
	HUDCorner string `json:"hud_corner"` // "bottom" | "bottom-right" | "top-right"
	SoundCues bool   `json:"sound_cues"`

	Insert         string `json:"insert"` // "auto" | "clipboard" | "keystroke" | "paste"
	PasteThreshold int    `json:"paste_threshold"`
	Live           bool   `json:"live"`          // type phrase by phrase while speaking (cleanup off only)
	LivePauseMs    int    `json:"live_pause_ms"` // pause that ends a phrase in live mode

	STTModel            string `json:"stt_model"`
	Language            string `json:"language"` // "" = auto-detect; "hinglish" = Hindi in Roman script
	Cleanup             bool   `json:"cleanup"`
	CleanupModel        string `json:"cleanup_model"`
	CleanupInstructions string `json:"cleanup_instructions"`
}

func Defaults() Config {
	return Config{
		Hotkey:           "option+d",
		Mode:             "toggle",
		MaxSeconds:       120,
		TrailingSpace:    true,
		SilenceAutoStop:  true,
		SilenceMs:        1800,
		SilenceMinMs:     700,
		SilenceThreshold: 0.02,
		ShowHUD:          true,
		HUDCorner:        "bottom",
		SoundCues:        true,
		Insert:           "auto",
		PasteThreshold:   280,
		Live:             false,
		LivePauseMs:      700,
		STTModel:         "openai/gpt-4o-mini-transcribe",
		Cleanup:          true,
		CleanupModel:     "deepseek/deepseek-v4.1-flash",
	}
}

// Hinglish is the Language value for Hindi written in Roman script.
const Hinglish = "hinglish"

// STTLanguage is the language hint for speech-to-text. Hinglish leaves it to
// auto-detect, which copes best with mixed Hindi and English; cleanup then
// fixes the script.
func (c Config) STTLanguage() string {
	if c.Language == Hinglish {
		return ""
	}
	return c.Language
}

// LiveTyping reports whether to type phrase by phrase. Cleanup turns it off:
// cleaned one phrase at a time, a paragraph loses its thread across pauses.
func (c Config) LiveTyping() bool { return c.Live && !c.Cleanup }

var (
	Dir     = filepath.Join(home(), ".config", "dictator")
	Path    = filepath.Join(Dir, "config.json")
	LogPath = filepath.Join(Dir, "dictate.log")
	PIDPath = filepath.Join(Dir, "dictator.pid")
	// LockPath is flock()ed by the running instance; the kernel drops the lock
	// if the process dies, so there is no stale-lock handling to get wrong.
	LockPath = filepath.Join(Dir, "dictator.lock")

	legacyDir = filepath.Join(home(), ".config", "chatgpt-dictate")
)

func home() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return h
}

// UsePath points the config file elsewhere (--config). Log, lock and pid stay.
func UsePath(p string) {
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	Path = p
}

// Load returns defaults overlaid with the file's values. A missing file is not
// an error; a malformed one returns defaults plus the error so it can be logged.
func Load() (Config, error) {
	cfg := Defaults()
	data, err := os.ReadFile(Path)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Defaults(), fmt.Errorf("%s unreadable (%v); using defaults", Path, err)
	}
	return cfg, nil
}

// Update persists only the given keys (JSON names) from cfg on top of what is
// on disk, keeping every other key -- including ones this version ignores.
func Update(cfg Config, keys ...string) error {
	raw := map[string]json.RawMessage{}
	if data, err := os.ReadFile(Path); err == nil {
		_ = json.Unmarshal(data, &raw) // malformed file: start over
	}
	cur, err := asMap(cfg)
	if err != nil {
		return err
	}
	for _, k := range keys {
		v, ok := cur[k]
		if !ok {
			return fmt.Errorf("config: unknown key %q", k)
		}
		raw[k] = v
	}
	return writeAtomic(raw)
}

// EnsureExists migrates the pre-rename directory, removes leftovers of the
// Python/Chromium version, and writes defaults on first run.
func EnsureExists() error {
	if _, err := os.Stat(Dir); errors.Is(err, fs.ErrNotExist) {
		if _, err := os.Stat(legacyDir); err == nil {
			_ = os.Rename(legacyDir, Dir)
		}
	}
	if err := os.MkdirAll(Dir, 0o700); err != nil {
		return err
	}
	// The old QtWebEngine profile (a ChatGPT browser session) is no longer used.
	_ = os.RemoveAll(filepath.Join(Dir, "webprofile"))
	_ = os.Remove(filepath.Join(Dir, "dictate.lock"))
	if _, err := os.Stat(Path); errors.Is(err, fs.ErrNotExist) {
		m, err := asMap(Defaults())
		if err != nil {
			return err
		}
		return writeAtomic(m)
	}
	return nil
}

func asMap(cfg Config) (map[string]json.RawMessage, error) {
	data, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	m := map[string]json.RawMessage{}
	return m, json.Unmarshal(data, &m)
}

func writeAtomic(m map[string]json.RawMessage) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(Path), 0o700); err != nil {
		return err
	}
	tmp := Path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, Path)
}
