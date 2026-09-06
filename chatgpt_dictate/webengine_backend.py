"""The transcription backend: a hidden QtWebEngine page logged into chatgpt.com.

It loads chatgpt.com once (reusing the persistent profile written by the login
window), injects page/inject.js, and drives it. All the messy parts -- cookies,
Cloudflare, TLS/proxy trust, User-Agent, the microphone capture itself -- are
handled by Chromium because the network call happens inside the page.

Contract (so a different backend could be swapped in later):
  signals: transcribed(str), failed(str), state_changed(str), auth_changed(bool)
  methods: start_recording(), stop_recording(), reload(), refresh_auth()
"""
from __future__ import annotations

import json
import os
from pathlib import Path

_DEBUG = bool(os.environ.get("CHATGPT_DICTATE_DEBUG"))


def _dbg(*a):
    if _DEBUG:
        print("[backend]", *a, flush=True)

from PySide6.QtCore import QObject, Qt, QTimer, QUrl, Signal
from PySide6.QtWebEngineCore import (
    QWebEnginePage,
    QWebEngineProfile,
    QWebEngineScript,
)
from PySide6.QtWebEngineWidgets import QWebEngineView

from .webpage import AUTH_KICK_JS, AUTH_READ_JS, QuietPage

_INJECT_JS = (Path(__file__).parent / "page" / "inject.js").read_text(encoding="utf-8")

_POLL_MS = 150
_TRANSCRIBE_TIMEOUT_MS = 60_000

_MIC_TYPES = {
    QWebEnginePage.Feature.MediaAudioCapture,
    QWebEnginePage.Feature.MediaAudioVideoCapture,
}


class ChatGPTWebBackend(QObject):
    transcribed = Signal(str)
    failed = Signal(str)
    state_changed = Signal(str)   # loading | ready | load-failed | recording | transcribing | idle
    auth_changed = Signal(bool)
    auto_stopped = Signal()       # the page's silence detector ended the recording on its own

    def __init__(self, profile: QWebEngineProfile, cfg: dict, parent: QObject | None = None) -> None:
        super().__init__(parent)
        self._cfg = cfg
        self._ready = False
        self._authed = False
        self._phase = "idle"
        self._elapsed = 0

        self._page = QuietPage(profile, self)
        self._view = QWebEngineView()
        self._view.setPage(self._page)

        # Re-inject the driver into every document automatically, so an SPA
        # navigation or reload can never leave window.__dictate undefined.
        script = QWebEngineScript()
        script.setName("chatgpt-dictate-inject")
        script.setInjectionPoint(QWebEngineScript.InjectionPoint.DocumentReady)
        script.setWorldId(QWebEngineScript.ScriptWorldId.MainWorld)
        script.setRunsOnSubFrames(False)
        script.setSourceCode(_INJECT_JS)
        self._page.scripts().insert(script)

        self._page.featurePermissionRequested.connect(self._on_feature_permission)
        if hasattr(self._page, "permissionRequested"):
            self._page.permissionRequested.connect(self._on_permission)
        self._page.loadFinished.connect(self._on_load_finished)

        if cfg.get("show_engine_window"):
            self._view.resize(640, 480)
            self._view.setWindowTitle("ChatGPT Dictate — engine")
            self._view.show()
        else:
            self._view.setWindowFlags(
                Qt.Tool | Qt.FramelessWindowHint | Qt.WindowStaysOnBottomHint
            )
            self._view.setAttribute(Qt.WA_ShowWithoutActivating, True)
            self._view.resize(1, 1)
            self._view.move(0, 0)
            self._view.show()

        self._poll = QTimer(self)
        self._poll.setInterval(_POLL_MS)
        self._poll.timeout.connect(self._tick)

        self.reload()

    # -- lifecycle -----------------------------------------------------------
    def reload(self) -> None:
        self._ready = False
        self.state_changed.emit("loading")
        self._page.load(QUrl(self._cfg["chatgpt_origin"] + "/"))

    def refresh_auth(self, _tries: int = 6) -> None:
        self._page.runJavaScript(AUTH_KICK_JS)
        QTimer.singleShot(
            700,
            lambda: self._page.runJavaScript(
                AUTH_READ_JS, lambda ok: self._on_auth_probe(ok, _tries)
            ),
        )

    def _on_auth_probe(self, ok, tries: int) -> None:
        authed = ok in (True, "true")
        if authed != self._authed:
            self._authed = authed
            self.auth_changed.emit(authed)
        elif authed:
            self.auth_changed.emit(True)
        elif tries > 1:
            QTimer.singleShot(1200, lambda: self.refresh_auth(tries - 1))

    @property
    def ready(self) -> bool:
        return self._ready

    @property
    def authed(self) -> bool:
        return self._authed

    def dispose(self) -> None:
        self._poll.stop()
        try:
            self._view.close()
            self._view.deleteLater()
            self._page.deleteLater()
        except Exception:
            pass

    # -- page load ---------------------------------------------------------
    def _js_cfg(self) -> str:
        # silence auto-stop only makes sense in toggle mode (in push-to-talk the
        # key release ends the recording).
        silence = bool(self._cfg.get("silence_auto_stop", True)) and self._cfg.get("mode") == "toggle"
        return json.dumps({
            "origin": self._cfg["chatgpt_origin"],
            "path": self._cfg["transcribe_path"],
            "field": self._cfg["audio_field"],
            "filename": self._cfg["audio_filename"],
            "responseKey": self._cfg["response_key"],
            "extraFields": self._cfg.get("extra_form_fields", {}),
            "maxSeconds": self._cfg.get("max_seconds", 120),
            "silenceStop": silence,
            "silenceMs": int(self._cfg.get("silence_ms", 1800)),
            "silenceMinMs": int(self._cfg.get("silence_min_ms", 700)),
            "silenceThreshold": float(self._cfg.get("silence_threshold", 0.02)),
        })

    def reconfigure(self) -> None:
        """Re-push config to the page (e.g. after the mode changed)."""
        if self._ready:
            self._page.runJavaScript(
                f"window.__dictate && window.__dictate.configure({self._js_cfg()})"
            )

    def _on_load_finished(self, ok: bool) -> None:
        if not ok:
            self.state_changed.emit("load-failed")
            return

        def after_host(host):
            host = (host or "").strip('"')
            on_chatgpt = (
                host == "chatgpt.com" or host.endswith(".chatgpt.com") or host == "chat.openai.com"
            )
            if not on_chatgpt:
                self._ready = False
                self.failed.emit("reauth")
                self.state_changed.emit("load-failed")
                return
            # the persistent script has already run at DocumentReady; just configure
            self._page.runJavaScript(
                f"window.__dictate && window.__dictate.configure({self._js_cfg()})"
            )
            self._ready = True
            self.state_changed.emit("ready")
            self.refresh_auth()

        self._page.runJavaScript("location.hostname", after_host)

    def _on_feature_permission(self, url, feature) -> None:
        if feature in _MIC_TYPES:
            self._page.setFeaturePermission(
                url, feature, QWebEnginePage.PermissionPolicy.PermissionGrantedByUser
            )

    def _on_permission(self, permission) -> None:
        # Qt 6.8+ QWebEnginePermission API
        try:
            ptype = permission.permissionType()
            name = str(ptype)
            if "MediaAudio" in name:
                permission.grant()
        except Exception:
            pass

    # -- recording -------------------------------------------------------
    def start_recording(self) -> None:
        if not self._ready:
            self.failed.emit("engine-not-ready")
            return
        self._phase = "recording"
        self._elapsed = 0
        self.state_changed.emit("recording")
        self._page.runJavaScript("window.__dictate && window.__dictate.start()")
        if not self._poll.isActive():
            self._poll.start()

    def stop_recording(self) -> None:
        self._phase = "transcribing"
        self._elapsed = 0
        self.state_changed.emit("transcribing")
        self._page.runJavaScript("window.__dictate && window.__dictate.stop()")
        if not self._poll.isActive():
            self._poll.start()

    def _tick(self) -> None:
        self._elapsed += _POLL_MS
        if self._phase == "transcribing" and self._elapsed >= _TRANSCRIBE_TIMEOUT_MS:
            self._poll.stop()
            self._phase = "idle"
            self.failed.emit("timeout")
            self.state_changed.emit("idle")
            return
        if self._phase == "idle":
            self._poll.stop()
            return
        # One round trip per tick: phase + one-shot events + final result.
        # runJavaScript in this build mangles bare objects, so serialise to JSON.
        self._page.runJavaScript(
            "JSON.stringify(window.__dictate ? window.__dictate.poll() : null)",
            self._on_poll,
        )

    def _on_poll(self, raw) -> None:
        if not raw or raw == "null":
            return
        try:
            data = json.loads(raw)
        except (TypeError, ValueError):
            _dbg("poll parse fail", repr(raw)[:200])
            return
        if not isinstance(data, dict):
            return

        for ev in data.get("events") or []:
            if ev == "autostopped" and self._phase == "recording":
                _dbg("auto-stopped by silence")
                self._phase = "transcribing"
                self._elapsed = 0
                self.auto_stopped.emit()

        res = data.get("result")
        if isinstance(res, dict):
            _dbg("poll result ->", repr(res)[:200])
            self._poll.stop()
            self._phase = "idle"
            self.state_changed.emit("idle")
            if res.get("ok"):
                self.transcribed.emit(str(res.get("text", "")))
            else:
                self.failed.emit(str(res.get("error", "unknown")))

    # -- diagnostics ---------------------------------------------------
    def probe_state(self, cb) -> None:
        self._page.runJavaScript(
            "JSON.stringify(window.__dictate ? window.__dictate.state() : {missing:true})", cb
        )
