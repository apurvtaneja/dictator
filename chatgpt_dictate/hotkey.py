"""Platform dispatcher for the global hotkey listener.

Exposes HotkeyListener / HotkeyError / parse_hotkey with a uniform interface:
  signals pressed, released; bool attr supports_release
  register(spec), unregister(), dispose(); attr `normalised`
"""
from __future__ import annotations

from ._platform import IS_MAC, IS_WIN

if IS_MAC:
    from .hotkey_mac import HotkeyListener, HotkeyError, parse_hotkey
elif IS_WIN:
    from .hotkey_win import HotkeyListener, HotkeyError, parse_hotkey
else:  # pragma: no cover - unsupported platform
    from PySide6.QtCore import QObject, Signal

    class HotkeyError(RuntimeError):
        pass

    class HotkeyListener(QObject):
        pressed = Signal()
        released = Signal()
        supports_release = False

        def __init__(self, parent=None):
            super().__init__(parent)
            self.normalised = ""

        def register(self, spec: str) -> None:
            raise HotkeyError("global hotkeys are not supported on this platform")

        def unregister(self) -> None:
            pass

        def dispose(self) -> None:
            pass

    def parse_hotkey(spec: str):
        raise HotkeyError("global hotkeys are not supported on this platform")

__all__ = ["HotkeyListener", "HotkeyError", "parse_hotkey"]
