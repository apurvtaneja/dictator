"""Text insertion on Windows via SendInput (ctypes).

Windows does not gate synthetic input behind a permission, so `accessibility_ok()`
is always True and the transcript can be typed straight into the focused field --
no Ctrl+V step needed. A clipboard path is still provided for `insert: "clipboard"`.
"""
from __future__ import annotations

import ctypes
import time
from ctypes import wintypes

_user32 = ctypes.windll.user32

_INPUT_KEYBOARD = 1
_KEYEVENTF_KEYUP = 0x0002
_KEYEVENTF_UNICODE = 0x0004
_VK_CONTROL = 0x11
_VK_V = 0x56

_ULONG_PTR = wintypes.WPARAM  # pointer-sized unsigned


class _KEYBDINPUT(ctypes.Structure):
    _fields_ = [
        ("wVk", wintypes.WORD),
        ("wScan", wintypes.WORD),
        ("dwFlags", wintypes.DWORD),
        ("time", wintypes.DWORD),
        ("dwExtraInfo", _ULONG_PTR),
    ]


class _INPUTUNION(ctypes.Union):
    # sized to the largest INPUT member (MOUSEINPUT) so sizeof(INPUT) is correct
    _fields_ = [("ki", _KEYBDINPUT), ("_pad", ctypes.c_ubyte * 32)]


class _INPUT(ctypes.Structure):
    _fields_ = [("type", wintypes.DWORD), ("u", _INPUTUNION)]


def _send(*inputs: _INPUT) -> None:
    n = len(inputs)
    arr = (_INPUT * n)(*inputs)
    _user32.SendInput(n, arr, ctypes.sizeof(_INPUT))


def _key_unicode(code_unit: int, key_up: bool) -> _INPUT:
    flags = _KEYEVENTF_UNICODE | (_KEYEVENTF_KEYUP if key_up else 0)
    ki = _KEYBDINPUT(0, code_unit, flags, 0, 0)
    return _INPUT(_INPUT_KEYBOARD, _INPUTUNION(ki=ki))


def _key_vk(vk: int, key_up: bool) -> _INPUT:
    flags = _KEYEVENTF_KEYUP if key_up else 0
    ki = _KEYBDINPUT(vk, 0, flags, 0, 0)
    return _INPUT(_INPUT_KEYBOARD, _INPUTUNION(ki=ki))


def accessibility_ok() -> bool:
    return True  # no permission gate for SendInput on Windows


def type_via_keystroke(text: str, chunk: int = 18, delay_ms: int = 4) -> None:
    units = text.encode("utf-16-le")
    events = []
    for i in range(0, len(units), 2):
        cu = units[i] | (units[i + 1] << 8)
        events.append(_key_unicode(cu, False))
        events.append(_key_unicode(cu, True))
    # send in small batches so a long transcript stays responsive
    batch = max(1, chunk) * 2
    for i in range(0, len(events), batch):
        _send(*events[i:i + batch])
        if delay_ms:
            time.sleep(delay_ms / 1000.0)


def copy_to_clipboard(text: str) -> None:
    try:
        from PySide6.QtWidgets import QApplication
        cb = QApplication.clipboard()
        if cb is not None:
            cb.setText(text)
    except Exception:
        pass


def press_paste() -> None:
    _send(
        _key_vk(_VK_CONTROL, False),
        _key_vk(_VK_V, False),
        _key_vk(_VK_V, True),
        _key_vk(_VK_CONTROL, True),
    )


def type_via_paste(text: str, restore_after_ms: int = 200) -> None:
    from PySide6.QtWidgets import QApplication
    from PySide6.QtCore import QTimer

    cb = QApplication.clipboard()
    previous = cb.text()
    cb.setText(text)
    QTimer.singleShot(30, press_paste)
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


def open_accessibility_settings() -> None:
    pass  # not applicable on Windows
