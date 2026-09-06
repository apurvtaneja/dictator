"""First-run / re-login window: a visible chatgpt.com browser.

Shares the persistent QWebEngineProfile with the background engine, so once you
finish logging in here (SSO, 2FA, whatever), the session cookie lives in the
profile and the engine can use it. The window closes itself once
/api/auth/session reports an accessToken.
"""
from __future__ import annotations

from PySide6.QtCore import QTimer, QUrl, Signal
from PySide6.QtWebEngineCore import QWebEnginePage, QWebEngineProfile
from PySide6.QtWebEngineWidgets import QWebEngineView

from .webpage import AUTH_KICK_JS, AUTH_READ_JS, QuietPage


def _on_chatgpt(host: str) -> bool:
    return host == "chatgpt.com" or host.endswith(".chatgpt.com") or host == "chat.openai.com"


class AuthWindow(QWebEngineView):
    logged_in = Signal()

    def __init__(self, profile: QWebEngineProfile, cfg: dict) -> None:
        super().__init__()
        self._cfg = cfg
        self.setPage(QuietPage(profile, self))
        self.page().featurePermissionRequested.connect(self._on_feature)
        self.setWindowTitle("Sign in to ChatGPT — Dictate")
        self.resize(920, 760)

        self._timer = QTimer(self)
        self._timer.setInterval(1500)
        self._timer.timeout.connect(self._check)

    def start(self) -> None:
        self.load(QUrl(self._cfg["chatgpt_origin"] + "/"))
        self.show()
        self.raise_()
        self.activateWindow()
        # the process runs as a background agent, so foreground it for the login UI
        try:
            from .nativewin import activate_app
            activate_app()
        except Exception:
            pass
        self._timer.start()

    def _on_feature(self, url, feature) -> None:
        if feature == QWebEnginePage.Feature.MediaAudioCapture:
            self.page().setFeaturePermission(
                url, feature, QWebEnginePage.PermissionPolicy.PermissionGrantedByUser
            )

    def _check(self) -> None:
        # Only probe while the webview is actually on chatgpt.com. During an OAuth
        # redirect (accounts.google.com, an SSO IdP, ...) a relative
        # /api/auth/session fetch would hit that origin and be CORS-blocked.
        if not _on_chatgpt(self.url().host()):
            return
        # read the result of the previous tick's kick, then kick again
        self.page().runJavaScript(AUTH_READ_JS, self._on_check)
        self.page().runJavaScript(AUTH_KICK_JS)

    def _on_check(self, ok) -> None:
        if ok in (True, "true"):
            self._timer.stop()
            print("[auth] signed in — session token present")
            self.logged_in.emit()
            self.close()

    def closeEvent(self, event) -> None:  # noqa: N802 (Qt override)
        self._timer.stop()
        super().closeEvent(event)
