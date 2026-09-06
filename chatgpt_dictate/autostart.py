"""Platform dispatcher for start-at-login.

  supported()         can this platform manage autostart from inside the app?
  is_enabled()        is the login entry currently registered?
  enable()            register it (idempotent)
  disable()           remove it (idempotent)
  refresh()           rewrite the entry if it has gone stale
  command()           the command this install would register
  recorded_command()  what is registered right now, or None

macOS keeps its LaunchAgent route (packaging/make_app.sh --launch-agent), so it
reports unsupported here and the tray item stays hidden.
"""
from __future__ import annotations

from ._platform import IS_WIN

if IS_WIN:
    from .autostart_win import (
        command,
        disable,
        enable,
        is_enabled,
        recorded_command,
        refresh,
        supported,
    )
else:  # pragma: no cover - macOS/Linux manage autostart outside the app
    def supported() -> bool:
        return False

    def is_enabled() -> bool:
        return False

    def enable() -> bool:
        return False

    def disable() -> bool:
        return False

    def refresh() -> None:
        pass

    def command() -> str:
        return ""

    def recorded_command() -> str | None:
        return None

__all__ = [
    "supported", "is_enabled", "enable", "disable",
    "refresh", "command", "recorded_command",
]
