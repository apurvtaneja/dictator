"""Type text at the current cursor location via synthesised CoreGraphics events.

Two strategies:
  * "keystroke" - CGEventKeyboardSetUnicodeString: posts the literal Unicode, no
    keycode mapping, no clipboard side effects. Good for short/medium text.
  * "paste"     - put text on the clipboard, synthesise Cmd-V, restore the clipboard.
    Faster for long text; briefly clobbers the pasteboard.

Either strategy needs the host process to hold the Accessibility permission
(System Settings > Privacy & Security > Accessibility).
"""
from __future__ import annotations

import ctypes
import ctypes.util
import time

_cg = ctypes.CDLL(ctypes.util.find_library("CoreGraphics"))
_cf = ctypes.CDLL(ctypes.util.find_library("CoreFoundation"))
_appservices = ctypes.CDLL(ctypes.util.find_library("ApplicationServices"))

# CGEvent* signatures
_cg.CGEventCreateKeyboardEvent.argtypes = [ctypes.c_void_p, ctypes.c_uint16, ctypes.c_bool]
_cg.CGEventCreateKeyboardEvent.restype = ctypes.c_void_p
_cg.CGEventKeyboardSetUnicodeString.argtypes = [
    ctypes.c_void_p, ctypes.c_ulong, ctypes.POINTER(ctypes.c_uint16)
]
_cg.CGEventSetFlags.argtypes = [ctypes.c_void_p, ctypes.c_uint64]
_cg.CGEventPost.argtypes = [ctypes.c_uint32, ctypes.c_void_p]
_cf.CFRelease.argtypes = [ctypes.c_void_p]

_appservices.AXIsProcessTrusted.restype = ctypes.c_bool

_kCGHIDEventTap = 0
_kCGEventFlagMaskCommand = 1 << 20
_VK_V = 0x09


def accessibility_ok() -> bool:
    """True if this process may post synthetic input events."""
    try:
        return bool(_appservices.AXIsProcessTrusted())
    except Exception:
        return False


def _post_unicode(text: str) -> None:
    buf = text.encode("utf-16-le")
    n = len(buf) // 2
    if n == 0:
        return
    arr = (ctypes.c_uint16 * n).from_buffer_copy(buf)
    for key_down in (True, False):
        ev = _cg.CGEventCreateKeyboardEvent(None, 0, key_down)
        if not ev:
            return
        _cg.CGEventKeyboardSetUnicodeString(ev, n, arr)
        _cg.CGEventPost(_kCGHIDEventTap, ev)
        _cf.CFRelease(ev)


def type_via_keystroke(text: str, chunk: int = 18, delay_ms: int = 4) -> None:
    chunk = max(1, chunk)
    for i in range(0, len(text), chunk):
        _post_unicode(text[i:i + chunk])
        if delay_ms:
            time.sleep(delay_ms / 1000.0)


def press_cmd_v() -> None:
    for key_down in (True, False):
        ev = _cg.CGEventCreateKeyboardEvent(None, _VK_V, key_down)
        if not ev:
            return
        _cg.CGEventSetFlags(ev, _kCGEventFlagMaskCommand)
        _cg.CGEventPost(_kCGHIDEventTap, ev)
        _cf.CFRelease(ev)


def type_via_paste(text: str, restore_after_ms: int = 200) -> None:
    """Clipboard + Cmd-V, then restore the previous clipboard text.

    Uses Qt's clipboard so it works without extra deps. Only plain text in the
    previous clipboard is preserved.
    """
    from PySide6.QtWidgets import QApplication
    from PySide6.QtCore import QTimer

    cb = QApplication.clipboard()
    previous = cb.text()
    cb.setText(text)
    # let the pasteboard settle before pasting
    QTimer.singleShot(30, press_cmd_v)
    QTimer.singleShot(30 + max(restore_after_ms, 50), lambda: cb.setText(previous))


def type_text(text: str, cfg: dict) -> None:
    if not text:
        return
    mode = cfg.get("insert", "auto")
    threshold = int(cfg.get("paste_threshold", 280))
    if mode == "paste" or (mode in ("auto", "keystroke") and len(text) > threshold):
        type_via_paste(text)
    else:
        type_via_keystroke(
            text,
            chunk=int(cfg.get("keystroke_chunk", 18)),
            delay_ms=int(cfg.get("keystroke_delay_ms", 4)),
        )


def copy_to_clipboard(text: str) -> None:
    """Put text on the clipboard. Needs no special permission."""
    try:
        from PySide6.QtWidgets import QApplication
        cb = QApplication.clipboard()
        if cb is not None:
            cb.setText(text)
            return
    except Exception:
        pass
    # fallback if Qt clipboard is somehow unavailable
    import subprocess
    p = subprocess.Popen(["/usr/bin/pbcopy"], stdin=subprocess.PIPE)
    p.communicate(text.encode("utf-8"))


def open_accessibility_settings() -> None:
    import subprocess
    subprocess.Popen([
        "open",
        "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility",
    ])
