"""Tiny platform helpers so the rest of the package stays OS-agnostic."""
from __future__ import annotations

import sys

IS_MAC = sys.platform == "darwin"
IS_WIN = sys.platform.startswith("win")
IS_LINUX = sys.platform.startswith("linux")

# label for the manual-paste key in user-facing messages
PASTE_HINT = "⌘V" if IS_MAC else "Ctrl+V"
