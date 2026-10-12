package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func useTemp(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	Dir = dir
	Path = filepath.Join(dir, "config.json")
}

func TestLoadDefaultsWhenMissing(t *testing.T) {
	useTemp(t)
	cfg, err := Load()
	if err != nil || cfg.Hotkey != "option+d" || cfg.STTModel == "" || !cfg.Cleanup {
		t.Fatalf("defaults not applied: %+v %v", cfg, err)
	}
}

func TestUpdateKeepsUnknownKeysAndOnlyWritesNamedKeys(t *testing.T) {
	useTemp(t)
	os.WriteFile(Path, []byte(`{"hotkey":"f6","transcribe_path":"/legacy","mode":"ptt"}`), 0o600)
	cfg, err := Load()
	if err != nil || cfg.Hotkey != "f6" || cfg.Mode != "ptt" {
		t.Fatalf("load: %+v %v", cfg, err)
	}
	cfg.Mode = "toggle"
	cfg.ShowHUD = false // a CLI override: must not be persisted
	if err := Update(cfg, "mode"); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	data, _ := os.ReadFile(Path)
	json.Unmarshal(data, &m)
	if m["mode"] != "toggle" || m["transcribe_path"] != "/legacy" || m["hotkey"] != "f6" {
		t.Fatalf("unexpected file: %s", data)
	}
	if _, ok := m["show_hud"]; ok {
		t.Fatalf("override leaked into file: %s", data)
	}
}

func TestUpdateRejectsUnknownKey(t *testing.T) {
	useTemp(t)
	if err := Update(Defaults(), "nope"); err == nil {
		t.Fatal("expected error")
	}
}

func TestMalformedFileFallsBackToDefaults(t *testing.T) {
	useTemp(t)
	os.WriteFile(Path, []byte(`{not json`), 0o600)
	cfg, err := Load()
	if err == nil || cfg.Hotkey != "option+d" {
		t.Fatalf("want defaults + error, got %+v %v", cfg, err)
	}
}
