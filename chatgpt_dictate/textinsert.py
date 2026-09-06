"""Platform dispatcher for inserting the transcript at the cursor.

Uniform API:
  accessibility_ok() -> bool         can we synthesise keystrokes right now?
  type_text(text, cfg) -> None       type the text (keystroke or paste per cfg)
  copy_to_clipboard(text) -> None    put text on the clipboard
  open_accessibility_settings()      open the OS permission pane (mac only)
"""
from __future__ import annotations

from ._platform import IS_MAC, IS_WIN

if IS_MAC:
    from .inject_mac import (
        accessibility_ok,
        copy_to_clipboard,
        open_accessibility_settings,
        type_text,
    )
elif IS_WIN:
    from .inject_win import (
        accessibility_ok,
        copy_to_clipboard,
        open_accessibility_settings,
        type_text,
    )
else:  # pragma: no cover - clipboard-only fallback
    def accessibility_ok() -> bool:
        return False

    def type_text(text: str, cfg: dict) -> None:
        copy_to_clipboard(text)

    def copy_to_clipboard(text: str) -> None:
        try:
            from PySide6.QtWidgets import QApplication
            cb = QApplication.clipboard()
            if cb is not None:
                cb.setText(text)
        except Exception:
            pass

    def open_accessibility_settings() -> None:
        pass

__all__ = ["accessibility_ok", "type_text", "copy_to_clipboard", "open_accessibility_settings"]
