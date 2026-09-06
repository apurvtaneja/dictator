"""Shared QWebEnginePage bits: console-noise suppression and the auth probe.

Why the two-step auth probe: QWebEnginePage.runJavaScript() hands the callback the
value of the *last statement*, and does NOT await a Promise. So we can't get the
result of `fetch('/api/auth/session')` back directly. Instead one call kicks the
fetch and parks the boolean on `window.__dictateAuth`; a later call reads that
plain value.
"""
from __future__ import annotations

from PySide6.QtWebEngineCore import QWebEnginePage

_CONSOLE_NOISE = (
    "ResizeObserver loop",
    "[GSI_LOGGER]",
    "WebGPU Context Provider",
    "Failed to fetch",
    "FedCM",
    "Access to fetch at 'https://accounts.google",
)

# fire-and-park: sets window.__dictateAuth to true/false when the fetch settles
AUTH_KICK_JS = (
    "(function(){"
    "fetch('/api/auth/session',{credentials:'include'})"
    ".then(function(r){return r.json();})"
    ".then(function(j){window.__dictateAuth=!!(j&&j.accessToken);"
    "window.__dictateEmail=(j&&j.user&&j.user.email)||null;})"
    ".catch(function(){window.__dictateAuth=false;});"
    "})();"
)
# read as a JSON string ("true"/"false"): runJavaScript in some builds mangles
# bare booleans/objects, but JSON.stringify round-trips reliably.
AUTH_READ_JS = "JSON.stringify(window.__dictateAuth === true)"


class QuietPage(QWebEnginePage):
    """QWebEnginePage that drops known-harmless console spam from chatgpt.com."""

    def javaScriptConsoleMessage(self, level, message, line, source):  # noqa: N802
        for noise in _CONSOLE_NOISE:
            if noise in message:
                return
        super().javaScriptConsoleMessage(level, message, line, source)
