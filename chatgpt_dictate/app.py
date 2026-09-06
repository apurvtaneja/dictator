"""Orchestrator: wires the hotkey to the engine to text insertion, and holds the
recording state machine (idle -> recording -> transcribing -> typing -> idle).
"""
from __future__ import annotations

from PySide6.QtCore import QObject, QTimer, Signal
from PySide6.QtWebEngineCore import QWebEngineProfile

from . import config, sound, textinsert
from ._platform import PASTE_HINT
from .auth_window import AuthWindow
from .hotkey import HotkeyListener, HotkeyError
from .webengine_backend import ChatGPTWebBackend


class DictateApp(QObject):
    # forwarded for the tray icon
    status = Signal(str)      # human-readable status line
    engine_state = Signal(str)
    notice = Signal(str, str)  # (title, message)
    hud_state = Signal(str, str)  # (state, message) for the floating HUD overlay

    def __init__(self, cfg: dict, parent: QObject | None = None) -> None:
        super().__init__(parent)
        self.cfg = cfg
        self._recording = False
        self._auth: AuthWindow | None = None
        self._auth_prompted = False
        self._finalizing_login = False

        # one persistent profile shared by the engine and the login window
        self.profile = QWebEngineProfile("chatgpt-dictate", self)
        self.profile.setPersistentStoragePath(str(config.PROFILE_DIR))
        self.profile.setCachePath(str(config.PROFILE_DIR / "cache"))
        self.profile.setPersistentCookiesPolicy(
            QWebEngineProfile.PersistentCookiesPolicy.ForcePersistentCookies
        )

        self.backend = ChatGPTWebBackend(self.profile, cfg, self)
        self.backend.transcribed.connect(self._on_transcribed)
        self.backend.failed.connect(self._on_failed)
        self.backend.state_changed.connect(self._on_engine_state)
        self.backend.auth_changed.connect(self._on_auth_changed)
        self.backend.auto_stopped.connect(self._on_auto_stopped)

        self._max_timer = QTimer(self)
        self._max_timer.setSingleShot(True)
        self._max_timer.timeout.connect(self._on_max_timeout)

        self.hotkey = HotkeyListener(self)
        self.hotkey.pressed.connect(self._on_hotkey_press)
        self.hotkey.released.connect(self._on_hotkey_release)
        try:
            self.hotkey.register(cfg["hotkey"])
            self.status.emit(f"Ready — {self.hotkey.normalised}")
        except HotkeyError as e:
            self.status.emit(f"Hotkey error: {e}")
            self.notice.emit("Hotkey not registered", str(e))

    # -- hotkey -> state machine -------------------------------------------
    def _ptt_active(self) -> bool:
        # push-to-talk needs a key-release event; where the platform can't deliver
        # one (Windows RegisterHotKey), fall back to toggle behaviour.
        return self.cfg.get("mode") == "ptt" and self.hotkey.supports_release

    def _on_hotkey_press(self) -> None:
        if self._ptt_active():
            self._begin()
        else:  # toggle (or ptt degraded to toggle)
            self._end() if self._recording else self._begin()

    def _on_hotkey_release(self) -> None:
        if self._ptt_active() and self._recording:
            self._end()

    def _begin(self) -> None:
        if self._recording:
            return
        if not self.backend.ready:
            self.notice.emit("Not ready", "Engine still loading — try again in a moment.")
            return
        if not self.backend.authed:
            if self._finalizing_login:
                self.notice.emit("Finishing sign-in", "Give it a few seconds, then press again.")
            else:
                self.notice.emit("Sign in first", "Opening the ChatGPT sign-in window.")
                self._maybe_open_auth()
            return
        self._recording = True
        self._cue("start")
        self.status.emit("Recording…")
        self.hud_state.emit("recording", "")
        self.backend.start_recording()
        self._max_timer.start(int(self.cfg.get("max_seconds", 120)) * 1000)

    def _end(self) -> None:
        if not self._recording:
            return
        self._recording = False
        self._max_timer.stop()
        self._cue("stop")
        self.status.emit("Transcribing…")
        self.hud_state.emit("transcribing", "")
        self.backend.stop_recording()

    def _on_auto_stopped(self) -> None:
        # the in-page silence detector ended the recording; the page is already
        # transcribing, so just reflect that here (don't call stop_recording again).
        if not self._recording:
            return
        self._recording = False
        self._max_timer.stop()
        self._cue("stop")
        self.status.emit("Transcribing…")
        self.hud_state.emit("transcribing", "")

    def _on_max_timeout(self) -> None:
        if self._recording:
            self.notice.emit("Max length reached", "Stopping and transcribing.")
            self._end()

    # -- engine results --------------------------------------------------
    def _on_transcribed(self, text: str) -> None:
        text = (text or "").strip()
        if not text:
            self.status.emit("No speech detected")
            self.notice.emit("Nothing transcribed", "No speech detected.")
            self.hud_state.emit("error", "No speech detected")
            return
        if self.cfg.get("trailing_space", True):
            text += " "

        # insert modes: "clipboard" always copies; "auto"/"keystroke"/"paste" type
        # the text when synthetic input is available (always on Windows; needs
        # Accessibility on macOS), else fall back to clipboard.
        insert = self.cfg.get("insert", "auto")
        wants_typing = insert != "clipboard"
        can_type = wants_typing and textinsert.accessibility_ok()
        n = len(text.strip())

        if can_type:
            textinsert.type_text(text, self.cfg)
            self.status.emit(f"Inserted {n} chars — {self.hotkey.normalised}")
            self.hud_state.emit("typed", f"Inserted {n} chars")
            return

        # clipboard fallback
        textinsert.copy_to_clipboard(text)
        self._cue("stop")
        self.hud_state.emit("copied", f"Copied — press {PASTE_HINT}")
        if wants_typing:
            self.notice.emit(f"Copied — press {PASTE_HINT}",
                             f"{n} chars on the clipboard (auto-typing unavailable).")
            self.status.emit(f"Copied — press {PASTE_HINT}  (auto-typing needs Accessibility)")
        else:
            self.notice.emit(f"Copied — press {PASTE_HINT}", f"{n} chars on the clipboard.")
            self.status.emit(f"Copied {n} chars — press {PASTE_HINT} to paste")

    def _on_failed(self, kind: str) -> None:
        self._recording = False
        self._max_timer.stop()
        self._cue("error")
        if kind == "reauth":
            self.status.emit("Sign-in required")
            self.hud_state.emit("error", "Sign-in required")
            self._maybe_open_auth()
            return
        friendly = {
            "no-mic": "No microphone found.",
            "no-mic-permission": "Microphone permission was denied for the engine.",
            "timeout": "Transcription timed out.",
            "empty-audio": "No audio was captured.",
            "engine-not-ready": "Engine is still loading.",
        }.get(kind, f"Dictation failed: {kind}")
        self.status.emit(friendly)
        self.notice.emit("Dictation failed", friendly)
        self.hud_state.emit("error", friendly)

    def _on_engine_state(self, state: str) -> None:
        self.engine_state.emit(state)
        if state == "ready":
            if not self.backend.authed:
                self.status.emit("Signed out — dictate or use the menu to sign in")
            else:
                self.status.emit(f"Ready — {self.hotkey.normalised}")
        elif state == "loading":
            self.status.emit("Engine loading…")
        elif state == "load-failed":
            self.status.emit("Engine failed to load chatgpt.com")

    def _on_auth_changed(self, authed: bool) -> None:
        if authed:
            self._auth_prompted = False
            self._finalizing_login = False
            self.status.emit(f"Ready — {self.hotkey.normalised}")
        else:
            self.status.emit("Signed out — opening sign-in window")
            if not self._auth_prompted:
                self._auth_prompted = True
                self._maybe_open_auth()

    # -- auth ----------------------------------------------------------
    def _maybe_open_auth(self, force: bool = False) -> None:
        if self.backend.authed and not force:
            self.status.emit(f"Ready — {self.hotkey.normalised}")
            return
        if self._auth is not None and self._auth.isVisible():
            self._auth.raise_()
            self._auth.activateWindow()
            return
        self._auth = AuthWindow(self.profile, self.cfg)
        self._auth.logged_in.connect(self._on_logged_in)
        self._auth.start()
        self.status.emit("Waiting for sign-in…")

    def _on_logged_in(self) -> None:
        self.status.emit("Signed in — reloading engine")
        self._auth_prompted = False
        self._finalizing_login = True
        QTimer.singleShot(20000, lambda: setattr(self, "_finalizing_login", False))
        self.backend.reload()

    def refresh_auth(self) -> None:
        self.backend.refresh_auth()

    # -- helpers -----------------------------------------------------
    def _cue(self, kind: str) -> None:
        if self.cfg.get("sound_cues", True):
            sound.play(kind)

    def set_mode(self, mode: str) -> None:
        self.cfg["mode"] = mode
        config.save(self.cfg)
        self.backend.reconfigure()  # silence auto-stop is toggle-mode only
        self.status.emit(f"Mode: {mode} — {self.hotkey.normalised}")

    def force_relogin(self) -> None:
        self._maybe_open_auth(force=True)

    def dispose(self) -> None:
        self.hotkey.dispose()
        self.backend.dispose()
