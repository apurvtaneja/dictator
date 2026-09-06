"""Reach a Qt window's underlying Cocoa NSWindow (via the Obj-C runtime through
ctypes, no pyobjc) to fix two macOS-specific behaviours for the HUD overlay:

  * Showing the window must NOT switch the user to the Space (virtual desktop)
    where this process's windows live. Setting CanJoinAllSpaces makes the window
    appear on whatever Space is currently active instead of dragging focus.
  * The overlay should float above other apps, including a fullscreen app, without
    activating our process (FullScreenAuxiliary + a high window level).
"""
from __future__ import annotations

import ctypes
import ctypes.util

_objc = ctypes.CDLL(ctypes.util.find_library("objc"))
_objc.sel_registerName.restype = ctypes.c_void_p
_objc.sel_registerName.argtypes = [ctypes.c_char_p]
_objc.objc_getClass.restype = ctypes.c_void_p
_objc.objc_getClass.argtypes = [ctypes.c_char_p]

# NSApplicationActivationPolicy
_NS_APP_POLICY_ACCESSORY = 1  # background agent: no Dock icon, never steals the
                              # active Space when its windows show (like LSUIElement)

# NSWindowCollectionBehavior bits
_CAN_JOIN_ALL_SPACES = 1 << 0
_STATIONARY = 1 << 4
_FULLSCREEN_AUXILIARY = 1 << 8

# NSWindow levels
_NS_STATUS_WINDOW_LEVEL = 25

# NSWindowStyleMask
_NS_NONACTIVATING_PANEL = 1 << 7  # a panel that never becomes key -> never steals focus


def _sel(name: str):
    return _objc.sel_registerName(name.encode())


def _send(receiver, selector, arg=None, argtype=None, restype=ctypes.c_void_p):
    fn = _objc.objc_msgSend
    fn.restype = restype
    if arg is None:
        fn.argtypes = [ctypes.c_void_p, ctypes.c_void_p]
        return fn(receiver, selector)
    fn.argtypes = [ctypes.c_void_p, ctypes.c_void_p, argtype]
    return fn(receiver, selector, arg)


def set_accessory_policy() -> bool:
    """Turn this process into a background agent (like a menu-bar app).

    This is the real cure for 'showing a window switches my Space': a Regular app
    activates and macOS follows it to its home Space; an Accessory app never does.
    Call once, after the QApplication (NSApp) exists. No Dock icon after this.
    """
    try:
        cls = _objc.objc_getClass(b"NSApplication")
        if not cls:
            return False
        nsapp = _send(ctypes.c_void_p(cls), _sel("sharedApplication"))
        if not nsapp:
            return False
        _send(ctypes.c_void_p(nsapp), _sel("setActivationPolicy:"),
              _NS_APP_POLICY_ACCESSORY, argtype=ctypes.c_long)
        return True
    except Exception:
        return False


def activate_app() -> bool:
    """Bring this (accessory) process to the front — used when the login window
    opens, since a background agent won't foreground itself otherwise."""
    try:
        cls = _objc.objc_getClass(b"NSApplication")
        nsapp = _send(ctypes.c_void_p(cls), _sel("sharedApplication"))
        _send(ctypes.c_void_p(nsapp), _sel("activateIgnoringOtherApps:"),
              True, argtype=ctypes.c_bool)
        return True
    except Exception:
        return False


def make_overlay(widget) -> bool:
    """Make an already-native Qt widget behave as an all-Spaces floating overlay.

    Returns True on success. Safe to call repeatedly; a no-op if the native
    window can't be reached.
    """
    try:
        view = int(widget.winId())  # NSView* on macOS
    except Exception:
        return False
    if not view:
        return False
    try:
        window = _send(ctypes.c_void_p(view), _sel("window"))
        if not window:
            return False
        window = ctypes.c_void_p(window)
        behavior = _CAN_JOIN_ALL_SPACES | _STATIONARY | _FULLSCREEN_AUXILIARY
        _send(window, _sel("setCollectionBehavior:"), behavior, argtype=ctypes.c_ulong)
        _send(window, _sel("setLevel:"), _NS_STATUS_WINDOW_LEVEL, argtype=ctypes.c_long)
        # Never take keyboard focus from the app the user is typing into: Qt::Tool
        # maps to an NSPanel, and the non-activating style makes it un-keyable.
        _objc.objc_msgSend.restype = ctypes.c_ulong
        _objc.objc_msgSend.argtypes = [ctypes.c_void_p, ctypes.c_void_p]
        cur = _objc.objc_msgSend(window, _sel("styleMask"))
        _send(window, _sel("setStyleMask:"), cur | _NS_NONACTIVATING_PANEL, argtype=ctypes.c_ulong)
        _send(window, _sel("setHidesOnDeactivate:"), False, argtype=ctypes.c_bool)
        return True
    except Exception:
        return False
