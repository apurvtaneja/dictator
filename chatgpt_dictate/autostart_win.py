"""Start-at-login on Windows via the per-user Run key (stdlib winreg).

Explorer launches every value under
HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Run at logon -- with no
working directory, which is why the recorded command bootstraps sys.path itself
instead of relying on `-m chatgpt_dictate` resolving from the current folder.

Per-user, so no admin rights are needed, and the entry shows up in
Task Manager > Startup where it can also be switched off.
"""
from __future__ import annotations

import sys
import winreg
from pathlib import Path

_RUN_KEY = r"Software\Microsoft\Windows\CurrentVersion\Run"
_VALUE_NAME = "ChatGPT Dictate"


def _pythonw() -> str:
    """The console-less interpreter beside the current one; falls back to it."""
    exe = Path(sys.executable)
    quiet = exe.with_name("pythonw.exe")
    return str(quiet if quiet.exists() else exe)


def _as_literal(path: str) -> str:
    """Embed a path as a single-quoted Python literal.

    The whole -c argument is wrapped in double quotes on the command line, so the
    literal must never contain one -- hence single quotes plus escaping.
    """
    return path.replace("\\", "\\\\").replace("'", "\\'")


def command() -> str:
    repo = str(Path(__file__).resolve().parent.parent)
    code = (
        "import sys; "
        f"sys.path.insert(0, '{_as_literal(repo)}'); "
        "from chatgpt_dictate.__main__ import main; "
        "sys.exit(main())"
    )
    return f'"{_pythonw()}" -c "{code}"'


def supported() -> bool:
    return True


def recorded_command() -> str | None:
    try:
        with winreg.OpenKey(winreg.HKEY_CURRENT_USER, _RUN_KEY) as key:
            value, _ = winreg.QueryValueEx(key, _VALUE_NAME)
        return str(value)
    except OSError:
        return None


def is_enabled() -> bool:
    return recorded_command() is not None


def enable() -> bool:
    try:
        with winreg.CreateKeyEx(
            winreg.HKEY_CURRENT_USER, _RUN_KEY, 0, winreg.KEY_SET_VALUE
        ) as key:
            winreg.SetValueEx(key, _VALUE_NAME, 0, winreg.REG_SZ, command())
        return True
    except OSError:
        return False


def disable() -> bool:
    try:
        with winreg.OpenKey(
            winreg.HKEY_CURRENT_USER, _RUN_KEY, 0, winreg.KEY_SET_VALUE
        ) as key:
            winreg.DeleteValue(key, _VALUE_NAME)
        return True
    except FileNotFoundError:
        return True  # already absent -- disabling is idempotent
    except OSError:
        return False


def refresh() -> None:
    """Repair a stale entry (repo moved, Python upgraded). Silent by design."""
    current = recorded_command()
    if current is not None and current != command():
        enable()
