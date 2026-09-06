"""Configuration: a single JSON file under ~/.config/chatgpt-dictate/.

The reverse-engineered endpoint details (path / form field / response key) live here
as plain settings so they can be corrected without touching code -- see README for the
DevTools procedure to confirm them against the current chatgpt.com build.
"""
from __future__ import annotations

import json
from pathlib import Path

from ._platform import IS_WIN

# Windows needs an Alt-free default. RegisterHotKey only swallows the *final*
# key, so with an Alt-based chord the Alt keydown/keyup still reach the
# foreground app -- which reads a bare Alt tap as "open the menu bar" and takes
# the caret out of the text field (Chrome jumps to its ⋮ menu). The transcript
# then has nowhere to land. macOS Option has no such behaviour.
DEFAULT_HOTKEY = "ctrl+shift+d" if IS_WIN else "option+d"

CONFIG_DIR = Path.home() / ".config" / "chatgpt-dictate"
CONFIG_PATH = CONFIG_DIR / "config.json"
PROFILE_DIR = CONFIG_DIR / "webprofile"
LOG_PATH = CONFIG_DIR / "dictate.log"

DEFAULTS: dict = {
    # --- interaction ---
    # macOS: "option+d".  Windows: "ctrl+shift+d" -- avoid Alt there, see above.
    "hotkey": DEFAULT_HOTKEY,
    "mode": "toggle",              # "toggle" (tap on / tap off) or "ptt" (hold)
    "max_seconds": 120,            # hard cap on a single recording
    "trailing_space": True,        # append a space after inserted text

    # --- silence auto-stop (toggle mode only) ---
    # End the recording automatically after a pause, so you don't press the hotkey
    # twice. Detection runs in-page via the Web Audio API — no permissions needed.
    "silence_auto_stop": True,
    "silence_ms": 1800,            # stop after this much continuous silence
    "silence_min_ms": 700,         # ignore silence in the first moment of recording
    "silence_threshold": 0.02,     # RMS level (0..1) below which audio counts as "silent"

    # --- floating HUD overlay ---
    "show_hud": True,              # translucent on-screen recording/transcribing indicator
    "hud_corner": "bottom",        # "bottom" | "bottom-right" | "top-right"

    # --- text insertion ---
    # "auto"      : type the text if synthetic input is available, else clipboard.
    #               (Windows/Linux type directly; macOS types only if Accessibility
    #               is granted, otherwise copies and you press the paste key.)
    # "clipboard" : always just copy; you press the paste key yourself.
    # "keystroke" : synthesise Unicode keystrokes.
    # "paste"     : clipboard + synthesised paste keystroke.
    "insert": "auto",
    "paste_threshold": 280,        # keystroke mode auto-switches to paste above this many chars
    "keystroke_chunk": 18,         # UTF-16 units per synthesised event
    "keystroke_delay_ms": 4,       # pause between chunks

    # --- reverse-engineered transcription endpoint (VERIFY via DevTools, see README) ---
    "chatgpt_origin": "https://chatgpt.com",
    "transcribe_path": "/backend-api/transcribe",
    "audio_field": "file",         # multipart field name for the audio blob
    "audio_filename": "audio.webm",
    "response_key": "text",        # JSON key holding the transcript
    "extra_form_fields": {},       # e.g. {"model": "whisper-1"} if the endpoint wants it

    # --- misc ---
    "show_engine_window": False,   # debug: show the normally-hidden browser engine
    "sound_cues": True,           # start/stop/error cues (macOS + Linux; Windows is
                                  # always silent — see sound.py — and uses the HUD)
}


def load() -> dict:
    cfg = dict(DEFAULTS)
    try:
        with open(CONFIG_PATH, encoding="utf-8") as f:
            user = json.load(f)
        if isinstance(user, dict):
            cfg.update(user)
    except FileNotFoundError:
        pass
    except Exception as e:  # malformed file -> fall back to defaults, keep running
        print(f"[config] {CONFIG_PATH} unreadable ({e}); using defaults")
    return cfg


def save(cfg: dict) -> None:
    CONFIG_DIR.mkdir(parents=True, exist_ok=True)
    tmp = CONFIG_PATH.with_suffix(".json.tmp")
    with open(tmp, "w", encoding="utf-8") as f:
        json.dump(cfg, f, indent=2)
    tmp.replace(CONFIG_PATH)


def ensure_exists() -> None:
    CONFIG_DIR.mkdir(parents=True, exist_ok=True)
    if not CONFIG_PATH.exists():
        save(DEFAULTS)
