"""Global hotkey via Carbon RegisterEventHotKey, reached through ctypes.

Why Carbon and not a CGEventTap: RegisterEventHotKey delivers a system-wide hotkey
*without* needing Accessibility or Input-Monitoring permission, and the event is
consumed so the key never reaches the focused app (e.g. Option+D won't type the
character it normally produces). It works inside a Qt app because Qt pumps the
CFRunLoop this handler is installed on.

Exposes HotkeyListener(QObject) with `pressed` / `released` signals.
"""
from __future__ import annotations

import ctypes
import ctypes.util
import struct

from PySide6.QtCore import QObject, Qt, QMetaObject, Signal, Slot

# --- Carbon / HIToolbox bindings -------------------------------------------------

_carbon_path = ctypes.util.find_library("Carbon")
if not _carbon_path:  # pragma: no cover - macOS always ships Carbon
    raise RuntimeError("Carbon framework not found")
_carbon = ctypes.CDLL(_carbon_path)


def _four_char_code(s: str) -> int:
    return struct.unpack(">I", s.encode("mac-roman"))[0]


kEventClassKeyboard = _four_char_code("keyb")
kEventHotKeyPressed = 5
kEventHotKeyReleased = 6
_HOTKEY_SIGNATURE = _four_char_code("dcdt")  # "dictate"

# Carbon modifier masks (NOT the CGEvent flag masks)
cmdKey = 0x0100
shiftKey = 0x0200
optionKey = 0x0800
controlKey = 0x1000

_MOD_TOKENS = {
    "cmd": cmdKey, "command": cmdKey, "⌘": cmdKey,
    "ctrl": controlKey, "control": controlKey, "⌃": controlKey,
    "alt": optionKey, "opt": optionKey, "option": optionKey, "⌥": optionKey,
    "shift": shiftKey, "⇧": shiftKey,
}

# ANSI virtual key codes
_KEYCODES = {
    "a": 0x00, "s": 0x01, "d": 0x02, "f": 0x03, "h": 0x04, "g": 0x05, "z": 0x06,
    "x": 0x07, "c": 0x08, "v": 0x09, "b": 0x0B, "q": 0x0C, "w": 0x0D, "e": 0x0E,
    "r": 0x0F, "y": 0x10, "t": 0x11, "1": 0x12, "2": 0x13, "3": 0x14, "4": 0x15,
    "6": 0x16, "5": 0x17, "=": 0x18, "9": 0x19, "7": 0x1A, "-": 0x1B, "8": 0x1C,
    "0": 0x1D, "]": 0x1E, "o": 0x1F, "u": 0x20, "[": 0x21, "i": 0x22, "p": 0x23,
    "l": 0x25, "j": 0x26, "'": 0x27, "k": 0x28, ";": 0x29, "\\": 0x2A, ",": 0x2B,
    "/": 0x2C, "n": 0x2D, "m": 0x2E, ".": 0x2F, "`": 0x32,
    "space": 0x31, "return": 0x24, "enter": 0x24, "tab": 0x30, "escape": 0x35, "esc": 0x35,
    "f1": 0x7A, "f2": 0x78, "f3": 0x63, "f4": 0x76, "f5": 0x60, "f6": 0x61,
    "f7": 0x62, "f8": 0x64, "f9": 0x65, "f10": 0x6D, "f11": 0x67, "f12": 0x6F,
}


class _EventTypeSpec(ctypes.Structure):
    _fields_ = [("eventClass", ctypes.c_uint32), ("eventKind", ctypes.c_uint32)]


class _EventHotKeyID(ctypes.Structure):
    _fields_ = [("signature", ctypes.c_uint32), ("id", ctypes.c_uint32)]


_HandlerProc = ctypes.CFUNCTYPE(
    ctypes.c_int32, ctypes.c_void_p, ctypes.c_void_p, ctypes.c_void_p
)

_carbon.GetApplicationEventTarget.restype = ctypes.c_void_p
_carbon.GetEventKind.argtypes = [ctypes.c_void_p]
_carbon.GetEventKind.restype = ctypes.c_uint32
_carbon.InstallEventHandler.argtypes = [
    ctypes.c_void_p, _HandlerProc, ctypes.c_uint32,
    ctypes.POINTER(_EventTypeSpec), ctypes.c_void_p, ctypes.POINTER(ctypes.c_void_p),
]
_carbon.InstallEventHandler.restype = ctypes.c_int32
_carbon.RemoveEventHandler.argtypes = [ctypes.c_void_p]
_carbon.RemoveEventHandler.restype = ctypes.c_int32
_carbon.RegisterEventHotKey.argtypes = [
    ctypes.c_uint32, ctypes.c_uint32, _EventHotKeyID,
    ctypes.c_void_p, ctypes.c_uint32, ctypes.POINTER(ctypes.c_void_p),
]
_carbon.RegisterEventHotKey.restype = ctypes.c_int32
_carbon.UnregisterEventHotKey.argtypes = [ctypes.c_void_p]
_carbon.UnregisterEventHotKey.restype = ctypes.c_int32


class HotkeyError(RuntimeError):
    pass


def parse_hotkey(spec: str) -> tuple[int, int, str]:
    """'option+d' -> (keycode, carbon_modifier_mask, normalised_string)."""
    tokens = [t.strip().lower() for t in spec.replace("-", "+").split("+") if t.strip()]
    if not tokens:
        raise HotkeyError(f"empty hotkey spec: {spec!r}")
    mods = 0
    key = None
    for tok in tokens:
        if tok in _MOD_TOKENS:
            mods |= _MOD_TOKENS[tok]
        elif tok in _KEYCODES:
            key = tok
        else:
            raise HotkeyError(f"unknown hotkey token {tok!r} in {spec!r}")
    if key is None:
        raise HotkeyError(f"hotkey {spec!r} has no non-modifier key")
    return _KEYCODES[key], mods, "+".join(tokens)


class HotkeyListener(QObject):
    pressed = Signal()
    released = Signal()
    supports_release = True  # Carbon delivers both key-down and key-up

    def __init__(self, parent: QObject | None = None) -> None:
        super().__init__(parent)
        self._handler_ref = ctypes.c_void_p()
        self._hotkey_ref = ctypes.c_void_p()
        self._installed = False
        self._registered = False
        # keep the ctypes callback alive for the process lifetime
        self._cb = _HandlerProc(self._on_carbon_event)
        self.normalised = ""

    # -- public API --------------------------------------------------------------
    def register(self, spec: str) -> None:
        keycode, mods, normalised = parse_hotkey(spec)
        self.unregister()
        target = _carbon.GetApplicationEventTarget()

        if not self._installed:
            types = (_EventTypeSpec * 2)(
                _EventTypeSpec(kEventClassKeyboard, kEventHotKeyPressed),
                _EventTypeSpec(kEventClassKeyboard, kEventHotKeyReleased),
            )
            status = _carbon.InstallEventHandler(
                target, self._cb, 2, types, None, ctypes.byref(self._handler_ref)
            )
            if status != 0:
                raise HotkeyError(f"InstallEventHandler failed (OSStatus {status})")
            self._installed = True

        hk_id = _EventHotKeyID(_HOTKEY_SIGNATURE, 1)
        status = _carbon.RegisterEventHotKey(
            keycode, mods, hk_id, target, 0, ctypes.byref(self._hotkey_ref)
        )
        if status != 0:
            raise HotkeyError(
                f"RegisterEventHotKey failed for {normalised!r} (OSStatus {status}); "
                "another app may already own this combination"
            )
        self._registered = True
        self.normalised = normalised

    def unregister(self) -> None:
        if self._registered and self._hotkey_ref:
            _carbon.UnregisterEventHotKey(self._hotkey_ref)
            self._hotkey_ref = ctypes.c_void_p()
        self._registered = False

    def dispose(self) -> None:
        self.unregister()
        if self._installed and self._handler_ref:
            _carbon.RemoveEventHandler(self._handler_ref)
            self._handler_ref = ctypes.c_void_p()
        self._installed = False

    # -- carbon callback -------------------------------------------------------
    def _on_carbon_event(self, _next_handler, event, _user_data) -> int:
        try:
            kind = _carbon.GetEventKind(event)
            if kind == kEventHotKeyPressed:
                QMetaObject.invokeMethod(self, "_emit_pressed", Qt.QueuedConnection)
            elif kind == kEventHotKeyReleased:
                QMetaObject.invokeMethod(self, "_emit_released", Qt.QueuedConnection)
        except Exception:  # never let an exception unwind into Carbon
            pass
        return 0  # noErr - consume the event

    @Slot()
    def _emit_pressed(self) -> None:
        self.pressed.emit()

    @Slot()
    def _emit_released(self) -> None:
        self.released.emit()
