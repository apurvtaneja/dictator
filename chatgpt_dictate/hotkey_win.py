"""Global hotkey on Windows via user32 RegisterHotKey (ctypes).

RegisterHotKey posts WM_HOTKEY to this thread's message queue; a Qt native event
filter picks it up from the running event loop. No special permission is needed.

Limitation vs macOS: RegisterHotKey reports only the key-*down*, so there is no
key-release event. `supports_release` is therefore False, and push-to-talk falls
back to toggle behaviour (the app handles that). Toggle mode + silence auto-stop
work fully.

Interface matches hotkey_mac.HotkeyListener: signals pressed/released, methods
register(spec) / unregister() / dispose(), attribute `normalised`.
"""
from __future__ import annotations

import ctypes
from ctypes import wintypes

from PySide6.QtCore import QAbstractNativeEventFilter, QCoreApplication, QObject, Signal

_user32 = ctypes.windll.user32

MOD_ALT = 0x0001
MOD_CONTROL = 0x0002
MOD_SHIFT = 0x0004
MOD_WIN = 0x0008
MOD_NOREPEAT = 0x4000
WM_HOTKEY = 0x0312
_HOTKEY_ID = 0xD1C7

# On Windows: option/alt -> Alt, cmd/win/meta -> Windows key.
_MOD_TOKENS = {
    "alt": MOD_ALT, "opt": MOD_ALT, "option": MOD_ALT, "⌥": MOD_ALT,
    "ctrl": MOD_CONTROL, "control": MOD_CONTROL, "⌃": MOD_CONTROL,
    "shift": MOD_SHIFT, "⇧": MOD_SHIFT,
    "cmd": MOD_WIN, "command": MOD_WIN, "win": MOD_WIN, "meta": MOD_WIN,
    "super": MOD_WIN, "⌘": MOD_WIN,
}

# Virtual-key codes. Letters/digits are their ASCII uppercase codes.
_VK = {
    "space": 0x20, "return": 0x0D, "enter": 0x0D, "tab": 0x09, "escape": 0x1B, "esc": 0x1B,
    "f1": 0x70, "f2": 0x71, "f3": 0x72, "f4": 0x73, "f5": 0x74, "f6": 0x75,
    "f7": 0x76, "f8": 0x77, "f9": 0x78, "f10": 0x79, "f11": 0x7A, "f12": 0x7B,
    "-": 0xBD, "=": 0xBB, "[": 0xDB, "]": 0xDD, ";": 0xBA, "'": 0xDE,
    ",": 0xBC, ".": 0xBE, "/": 0xBF, "`": 0xC0, "\\": 0xDC,
}


class HotkeyError(RuntimeError):
    pass


class MSG(ctypes.Structure):
    _fields_ = [
        ("hwnd", wintypes.HWND),
        ("message", wintypes.UINT),
        ("wParam", wintypes.WPARAM),
        ("lParam", wintypes.LPARAM),
        ("time", wintypes.DWORD),
        ("pt_x", wintypes.LONG),
        ("pt_y", wintypes.LONG),
    ]


def parse_hotkey(spec: str) -> tuple[int, int, str]:
    """'option+d' -> (vk, win_modifier_mask, normalised)."""
    tokens = [t.strip().lower() for t in spec.replace("-", "+").split("+") if t.strip()]
    if not tokens:
        raise HotkeyError(f"empty hotkey spec: {spec!r}")
    mods = 0
    vk = None
    for tok in tokens:
        if tok in _MOD_TOKENS:
            mods |= _MOD_TOKENS[tok]
        elif len(tok) == 1 and (tok.isalnum()):
            vk = ord(tok.upper())
        elif tok in _VK:
            vk = _VK[tok]
        else:
            raise HotkeyError(f"unknown hotkey token {tok!r} in {spec!r}")
    if vk is None:
        raise HotkeyError(f"hotkey {spec!r} has no non-modifier key")
    return vk, mods, "+".join(tokens)


def _msg_address(message) -> int | None:
    """Get the native MSG* address from whatever wrapper PySide6 handed us."""
    try:
        return int(message)
    except (TypeError, ValueError):
        pass
    try:
        import shiboken6
        return int(shiboken6.Shiboken.getCppPointer(message)[0])
    except Exception:
        return None


class _HotkeyFilter(QAbstractNativeEventFilter):
    def __init__(self, on_hotkey) -> None:
        super().__init__()
        self._on_hotkey = on_hotkey

    def nativeEventFilter(self, event_type, message):
        try:
            if event_type == b"windows_generic_MSG":
                addr = _msg_address(message)
                if addr is not None:
                    msg = MSG.from_address(addr)
                    if msg.message == WM_HOTKEY and msg.wParam == _HOTKEY_ID:
                        self._on_hotkey()
        except Exception:
            pass
        return False, 0


class HotkeyListener(QObject):
    pressed = Signal()
    released = Signal()
    supports_release = False  # RegisterHotKey gives key-down only

    def __init__(self, parent: QObject | None = None) -> None:
        super().__init__(parent)
        self._registered = False
        self._filter = _HotkeyFilter(self.pressed.emit)
        QCoreApplication.instance().installNativeEventFilter(self._filter)
        self.normalised = ""

    def register(self, spec: str) -> None:
        vk, mods, normalised = parse_hotkey(spec)
        self.unregister()
        ok = _user32.RegisterHotKey(None, _HOTKEY_ID, mods | MOD_NOREPEAT, vk)
        if not ok:
            raise HotkeyError(
                f"RegisterHotKey failed for {normalised!r}; another app may own this combination"
            )
        self._registered = True
        self.normalised = normalised

    def unregister(self) -> None:
        if self._registered:
            _user32.UnregisterHotKey(None, _HOTKEY_ID)
            self._registered = False

    def dispose(self) -> None:
        self.unregister()
        try:
            QCoreApplication.instance().removeNativeEventFilter(self._filter)
        except Exception:
            pass
