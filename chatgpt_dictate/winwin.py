"""Windows window tweaks for the HUD overlay (ctypes / user32).

Windows has no Spaces to switch, so most of the macOS work isn't needed. The one
useful bit is making the HUD a no-activate tool window so it never steals keyboard
focus from the field the user is about to paste into.
"""
from __future__ import annotations

import ctypes

_user32 = ctypes.windll.user32

_GWL_EXSTYLE = -20
_WS_EX_TOPMOST = 0x00000008
_WS_EX_TOOLWINDOW = 0x00000080
_WS_EX_NOACTIVATE = 0x08000000
_WS_EX_TRANSPARENT = 0x00000020

try:
    _GetWindowLong = _user32.GetWindowLongPtrW
    _SetWindowLong = _user32.SetWindowLongPtrW
    _GetWindowLong.restype = ctypes.c_ssize_t
    _SetWindowLong.restype = ctypes.c_ssize_t
    _GetWindowLong.argtypes = [ctypes.c_void_p, ctypes.c_int]
    _SetWindowLong.argtypes = [ctypes.c_void_p, ctypes.c_int, ctypes.c_ssize_t]
except AttributeError:  # 32-bit Python
    _GetWindowLong = _user32.GetWindowLongW
    _SetWindowLong = _user32.SetWindowLongW
    _GetWindowLong.restype = ctypes.c_long
    _SetWindowLong.restype = ctypes.c_long
    _GetWindowLong.argtypes = [ctypes.c_void_p, ctypes.c_int]
    _SetWindowLong.argtypes = [ctypes.c_void_p, ctypes.c_int, ctypes.c_long]


def set_accessory_policy() -> bool:
    return True  # nothing to do on Windows; Tool windows already stay off the taskbar


def activate_app() -> bool:
    return False  # Qt's activateWindow() is enough for the login window on Windows


def make_overlay(widget) -> bool:
    """Mark the HUD's HWND as a no-activate, topmost tool window (click-through)."""
    try:
        hwnd = ctypes.c_void_p(int(widget.winId()))
    except Exception:
        return False
    if not hwnd:
        return False
    try:
        ex = _GetWindowLong(hwnd, _GWL_EXSTYLE)
        ex |= _WS_EX_TOPMOST | _WS_EX_TOOLWINDOW | _WS_EX_NOACTIVATE | _WS_EX_TRANSPARENT
        _SetWindowLong(hwnd, _GWL_EXSTYLE, ex)
        return True
    except Exception:
        return False
