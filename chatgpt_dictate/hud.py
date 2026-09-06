"""A small floating on-screen indicator: Recording -> Transcribing -> Copied.

This exists because the menu-bar dot is unreliable feedback on a notched Mac (the
icon can be hidden behind the notch). The HUD is a frameless, always-on-top,
click-through, non-activating window, so it never steals keyboard focus -- critical,
because the whole point is that you press Cmd-V into some *other* app right after.

No permissions required: it's a plain translucent Qt window.
"""
from __future__ import annotations

from PySide6.QtCore import Qt, QTimer
from PySide6.QtGui import QColor, QFont, QFontMetrics, QPainter, QPainterPath
from PySide6.QtWidgets import QApplication, QWidget

_ACCENTS = {
    "recording": QColor("#ff453a"),
    "transcribing": QColor("#0a84ff"),
    "copied": QColor("#30d158"),
    "typed": QColor("#30d158"),
    "error": QColor("#ff9f0a"),
}
_DEFAULT_TEXT = {
    "recording": "Recording…",
    "transcribing": "Transcribing…",
    "copied": "Copied — press ⌘V",
    "typed": "Inserted",
    "error": "Error",
}
# how long the terminal states stay before fading out
_AUTOHIDE_MS = {"copied": 3200, "typed": 1600, "error": 3600}


class HUD(QWidget):
    def __init__(self, corner: str = "bottom") -> None:
        super().__init__(None)
        self._corner = corner
        self._state = "idle"
        self._text = ""
        self._accent = QColor("#8e8e93")
        self._phase = 0.0  # animation phase

        self.setWindowFlags(
            Qt.FramelessWindowHint
            | Qt.Tool
            | Qt.WindowStaysOnTopHint
            | Qt.WindowDoesNotAcceptFocus
            | Qt.WindowTransparentForInput  # click-through: never intercepts the mouse
        )
        self.setAttribute(Qt.WA_TranslucentBackground, True)
        self.setAttribute(Qt.WA_ShowWithoutActivating, True)  # never take focus
        self.setFixedHeight(44)
        self.setFixedWidth(230)

        self._font = QFont()
        self._font.setPointSize(13)
        self._font.setWeight(QFont.Medium)

        self._anim = QTimer(self)
        self._anim.setInterval(1000 // 30)
        self._anim.timeout.connect(self._on_anim)

        self._hide_timer = QTimer(self)
        self._hide_timer.setSingleShot(True)
        self._hide_timer.timeout.connect(self.fade_out)

        self._fade = QTimer(self)
        self._fade.setInterval(1000 // 30)
        self._fade.timeout.connect(self._on_fade)

        self._native_done = False
        # Create the native NSWindow now and set its Space behaviour up front, so
        # the very first show() already joins the active Space (no switch).
        try:
            self.winId()
            self._apply_native_overlay()
        except Exception:
            pass

    def _apply_native_overlay(self) -> None:
        # Make the window join the *active* Space instead of switching to ours.
        # Re-applied on each fresh show in case the native window was recreated.
        try:
            from .nativewin import make_overlay
            self._native_done = make_overlay(self) or self._native_done
        except Exception:
            pass

    # -- public API ---------------------------------------------------------
    def set_state(self, state: str, message: str = "") -> None:
        if state in ("hide", "idle", "ready", "loading", "load-failed"):
            self.fade_out()
            return
        if state not in _ACCENTS:
            return
        self._state = state
        self._text = message or _DEFAULT_TEXT.get(state, "")
        self._accent = _ACCENTS[state]
        self._phase = 0.0
        self._hide_timer.stop()
        self._fade.stop()
        self.setWindowOpacity(1.0)
        self._size_to_text()
        self._place()
        if not self.isVisible():
            self.show()
            self._apply_native_overlay()
        # NB: no raise_()/activateWindow() — the window floats via its status level,
        # and re-ordering it would steal keyboard focus from the user's text field.
        if state in ("recording", "transcribing"):
            self._anim.start()
        else:
            self._anim.stop()
        delay = _AUTOHIDE_MS.get(state)
        if delay:
            self._hide_timer.start(delay)
        self.update()

    def fade_out(self) -> None:
        self._anim.stop()
        self._hide_timer.stop()
        if self.isVisible():
            self._fade.start()
        else:
            self.hide()

    # -- animation ----------------------------------------------------------
    def _on_anim(self) -> None:
        self._phase = (self._phase + 0.06) % 1.0
        self.update()

    def _on_fade(self) -> None:
        o = self.windowOpacity() - 0.12
        if o <= 0.0:
            self._fade.stop()
            self.setWindowOpacity(1.0)
            self.hide()
        else:
            self.setWindowOpacity(o)

    # -- geometry -----------------------------------------------------------
    def _size_to_text(self) -> None:
        fm = QFontMetrics(self._font)
        w = 44 + fm.horizontalAdvance(self._text) + 22
        self.setFixedWidth(max(180, min(w, 420)))

    def _place(self) -> None:
        screen = QApplication.primaryScreen()
        if screen is None:
            return
        geo = screen.availableGeometry()
        w, h = self.width(), self.height()
        margin = 28
        if self._corner == "bottom-right":
            x = geo.right() - w - margin
            y = geo.bottom() - h - margin
        elif self._corner == "top-right":
            x = geo.right() - w - margin
            y = geo.top() + margin
        else:  # bottom center
            x = geo.left() + (geo.width() - w) // 2
            y = geo.bottom() - h - margin
        self.move(int(x), int(y))

    # -- painting -----------------------------------------------------------
    def paintEvent(self, _event) -> None:  # noqa: N802 (Qt override)
        p = QPainter(self)
        p.setRenderHint(QPainter.Antialiasing)
        r = self.rect().adjusted(1, 1, -1, -1)

        path = QPainterPath()
        path.addRoundedRect(r, 12, 12)
        p.fillPath(path, QColor(28, 28, 30, 235))
        p.setPen(QColor(255, 255, 255, 28))
        p.drawPath(path)

        # status glyph on the left
        cx, cy = 24, self.height() / 2
        if self._state == "recording":
            # pulsing filled dot
            import math
            scale = 0.72 + 0.28 * (0.5 + 0.5 * math.sin(self._phase * 2 * math.pi))
            rad = 7 * scale
            p.setBrush(self._accent)
            p.setPen(Qt.NoPen)
            p.drawEllipse(int(cx - rad), int(cy - rad), int(rad * 2), int(rad * 2))
        elif self._state == "transcribing":
            # three marching dots
            import math
            for i in range(3):
                t = (self._phase * 2 + i * 0.18) % 1.0
                a = 0.35 + 0.65 * (0.5 + 0.5 * math.sin(t * 2 * math.pi))
                c = QColor(self._accent)
                c.setAlphaF(a)
                p.setBrush(c)
                p.setPen(Qt.NoPen)
                p.drawEllipse(int(cx - 9 + i * 8), int(cy - 2.5), 5, 5)
        else:
            # static check / dot
            p.setBrush(self._accent)
            p.setPen(Qt.NoPen)
            p.drawEllipse(int(cx - 6), int(cy - 6), 12, 12)

        p.setFont(self._font)
        p.setPen(QColor(245, 245, 247))
        text_rect = self.rect().adjusted(44, 0, -12, 0)
        p.drawText(text_rect, Qt.AlignVCenter | Qt.AlignLeft, self._text)
        p.end()
