"""Platform dispatcher for native window behaviour.

  set_accessory_policy()   make the process a background agent (mac; no-op elsewhere)
  make_overlay(widget)     make a window a non-focus-stealing floating overlay
  activate_app()           bring the process forward (for the login window)
"""
from __future__ import annotations

from ._platform import IS_MAC, IS_WIN

if IS_MAC:
    from .macwin import activate_app, make_overlay, set_accessory_policy
elif IS_WIN:
    from .winwin import activate_app, make_overlay, set_accessory_policy
else:  # pragma: no cover
    def set_accessory_policy() -> bool:
        return False

    def make_overlay(widget) -> bool:
        return False

    def activate_app() -> bool:
        return False

__all__ = ["set_accessory_policy", "make_overlay", "activate_app"]
