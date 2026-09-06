# Windows Autostart Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a "Start at login" toggle to the Windows tray menu that registers the app under the per-user `Run` key, so it never has to be started by hand again.

**Architecture:** Follows the repo's existing platform-dispatcher pattern (`hotkey.py` → `hotkey_win.py`). A new `autostart.py` dispatcher reports unsupported on every platform except Windows, so the tray item is never added on macOS. `autostart_win.py` writes one string value to `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` with stdlib `winreg`. Because `Run` entries get no working directory, the recorded command bootstraps `sys.path` itself rather than relying on `-m chatgpt_dictate`.

**Tech Stack:** Python 3.9+ stdlib (`winreg`, `pathlib`), PySide6 (`QAction` only).

**Spec:** `docs/superpowers/specs/2026-09-06-windows-autostart-design.md`

## Global Constraints

- **No new dependencies.** Stdlib + PySide6 only. No `pywin32`, no COM, no Windows Script Host.
- **macOS behaviour must not change.** Nothing outside a Windows-only branch may be altered. macOS keeps its LaunchAgent route (`packaging/make_app.sh --launch-agent`).
- **Never raise into the UI.** Every registry call is wrapped. `enable()`/`disable()` return `bool`; `is_enabled()` degrades to `False`; `refresh()` is best-effort and silent.
- **No file logging, no error toasts, no Task Scheduler.** Explicitly cut during design.
- Registry value name is exactly `ChatGPT Dictate`.
- The `-c` argument must never contain a double quote — it would terminate the outer command-line quoting.
- The repo has **no test framework**. Verification steps use runnable `python -c` assertions. Do not add pytest.

---

### Task 1: Autostart module + dispatcher

**Files:**
- Create: `chatgpt_dictate/autostart_win.py`
- Create: `chatgpt_dictate/autostart.py`

**Interfaces:**
- Consumes: `chatgpt_dictate._platform.IS_WIN` (existing).
- Produces, used by Tasks 2 and 3:
  - `supported() -> bool`
  - `is_enabled() -> bool`
  - `enable() -> bool`
  - `disable() -> bool`
  - `refresh() -> None`
  - `command() -> str` — the command this install *would* register (`""` when unsupported)
  - `recorded_command() -> str | None` — what is actually in the registry now, `None` if absent

- [ ] **Step 1: Write the failing check**

Create `C:\Users\<you>\AppData\Local\Temp\check_autostart.py`:

```python
import sys
sys.path.insert(0, r"C:\projects\dictator")
from chatgpt_dictate import autostart

assert autostart.supported() is True, "should be supported on Windows"

cmd = autostart.command()
print("command:", cmd)

# The -c payload must never contain a double quote.
payload = cmd.split(" -c ", 1)[1]
assert payload.startswith('"') and payload.endswith('"'), "payload must be double-quoted"
assert '"' not in payload[1:-1], "inner payload must contain no double quotes"

# It must actually run and import the package.
assert "sys.path.insert" in cmd
assert "from chatgpt_dictate.__main__ import main" in cmd

# Enable / detect / disable round trip.
assert autostart.enable() is True
assert autostart.is_enabled() is True
assert autostart.recorded_command() == cmd
assert autostart.disable() is True
assert autostart.is_enabled() is False
assert autostart.recorded_command() is None
assert autostart.disable() is True, "disable must be idempotent"

print("PASS")
```

- [ ] **Step 2: Run it to verify it fails**

Run: `python C:\Users\<you>\AppData\Local\Temp\check_autostart.py`
Expected: FAIL with `ModuleNotFoundError: No module named 'chatgpt_dictate.autostart'`

- [ ] **Step 3: Create `chatgpt_dictate/autostart_win.py`**

```python
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
```

Note: `FileNotFoundError` is a subclass of `OSError`, so it must be caught first in `disable()`.

- [ ] **Step 4: Create `chatgpt_dictate/autostart.py`**

```python
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
```

- [ ] **Step 5: Run the check to verify it passes**

Run: `python C:\Users\<you>\AppData\Local\Temp\check_autostart.py`
Expected: `PASS`, and no leftover value (the script disables at the end).

- [ ] **Step 6: Verify the recorded command actually launches the app**

Run this to prove the bootstrap payload is executable — it should print the diagnostics block and exit 0:

```powershell
python -c "import sys; sys.path.insert(0, 'C:\\projects\\dictator'); from chatgpt_dictate.__main__ import main; sys.exit(main())" --check
```

Expected: the same output as `run.bat --check`.

- [ ] **Step 7: Verify macOS is unaffected**

Run: `python -c "import ast,sys; ast.parse(open(r'C:\projects\dictator\chatgpt_dictate\autostart.py').read()); print('parses OK')"`

Confirm by reading `autostart.py` that every macOS path returns the inert stub values, and that no existing file was touched in this task.

- [ ] **Step 8: Commit**

```bash
git add chatgpt_dictate/autostart.py chatgpt_dictate/autostart_win.py
git commit -m "Add Windows autostart module behind a platform dispatcher"
```

---

### Task 2: Tray toggle and startup self-heal

**Files:**
- Modify: `chatgpt_dictate/__main__.py` (import line 15; `main()` around line 145; menu around line 203; new helper beside `_set_mode` near line 280)

**Interfaces:**
- Consumes from Task 1: `autostart.supported()`, `autostart.is_enabled()`, `autostart.enable()`, `autostart.disable()`, `autostart.refresh()`
- Produces: `_toggle_autostart(action, checked: bool) -> None`

- [ ] **Step 1: Add the import**

Change line 15 from:

```python
from . import config, textinsert
```

to:

```python
from . import autostart, config, textinsert
```

- [ ] **Step 2: Self-heal a stale entry at startup**

In `main()`, immediately after `cfg = config.load()`, add:

```python
    # Repair a login entry left pointing at an old repo path or interpreter.
    autostart.refresh()
```

- [ ] **Step 3: Add the tray menu item**

In `main()`, after the `mode_menu` block and *before* `act_config` is created, add:

```python
    if autostart.supported():
        act_autostart = QAction("Start at login", menu, checkable=True)
        act_autostart.setChecked(autostart.is_enabled())
        act_autostart.triggered.connect(
            lambda checked: _toggle_autostart(act_autostart, checked)
        )
        menu.addAction(act_autostart)
```

- [ ] **Step 4: Add the toggle helper**

Beside `_set_mode` near line 280, add:

```python
def _toggle_autostart(action, checked: bool) -> None:
    if checked:
        autostart.enable()
    else:
        autostart.disable()
    # Reflect what actually happened -- a refused registry write must not leave
    # the menu claiming a state that isn't true.
    action.setChecked(autostart.is_enabled())
```

- [ ] **Step 5: Verify the module still loads and macOS stays clean**

Run: `python -m chatgpt_dictate --check`
Expected: the diagnostics block prints, exit code 0.

Run: `python -c "import sys; sys.path.insert(0, r'C:\projects\dictator'); import chatgpt_dictate.__main__ as m; print('imports OK'); print('helper:', callable(m._toggle_autostart))"`
Expected: `imports OK` then `helper: True`

- [ ] **Step 6: Verify the toggle end to end in the real tray**

Run: `run.bat`

1. Open the tray menu. Confirm **Start at login** appears between *Mode* and *Open config file*, unticked.
2. Tick it. Confirm it stays ticked.
3. In another terminal: `reg query "HKCU\Software\Microsoft\Windows\CurrentVersion\Run" /v "ChatGPT Dictate"` — the value must exist and name a `pythonw.exe`.
4. Open Task Manager ▸ Startup. Confirm the entry is listed.
5. Untick it. Re-run the `reg query` — expected: `ERROR: The system was unable to find the specified registry key or value.`
6. Tick it again and leave it on for the reboot test in Task 3.

- [ ] **Step 7: Commit**

```bash
git add chatgpt_dictate/__main__.py
git commit -m "Add Start at login tray toggle with startup self-heal"
```

---

### Task 3: Diagnostics line and documentation

**Files:**
- Modify: `chatgpt_dictate/__main__.py` (`run_check()`, around lines 60-69)
- Modify: `README.md` (dispatcher table line 62; Windows setup lines 126-127; file map lines 258-262)

**Interfaces:**
- Consumes from Task 1: `autostart.supported()`, `autostart.is_enabled()`, `autostart.command()`, `autostart.recorded_command()`

- [ ] **Step 1: Add the autostart line to `run_check()`**

In `run_check()`, immediately after the `hotkey` / `mode` prints and before the `transcribe target` print, add:

```python
    if autostart.supported():
        recorded = autostart.recorded_command()
        print("  start at login   :", "enabled" if recorded else "disabled")
        print("  autostart command:", recorded or autostart.command())
        if recorded and recorded != autostart.command():
            print("                     (stale — repaired next time the app starts)")
```

- [ ] **Step 2: Verify the diagnostics output**

Run: `run.bat --check`
Expected: a `start at login : enabled` line (the toggle was left on at the end of Task 2) and an `autostart command:` line naming `pythonw.exe`.

- [ ] **Step 3: Update the dispatcher table in README.md**

Replace line 62:

```
| Sound cues | `sound.py` | `afplay` | none (silent; HUD only) |
```

with:

```
| Sound cues | `sound.py` | `afplay` | none (silent; HUD only) |
| Start at login | `autostart.py` | LaunchAgent (`make_app.sh`) | `autostart_win.py` (HKCU Run key) |
```

- [ ] **Step 4: Replace the manual autostart instructions**

Delete this bullet (lines 126-127):

```
- **Autostart / no console:** put a shortcut to `pythonw -m chatgpt_dictate` (run from the
  repo folder) in `shell:startup`. `pythonw` runs it without a console window.
```

Then, after the bullet list that ends the *1. Run it* section and before the `---` divider, add:

```markdown
### 2. Start it automatically at login

Tray menu ▸ **Start at login**. One click and it comes back after every reboot,
with no console window — `pythonw` runs it silently.

It writes a single per-user registry value, so there's no admin prompt and
nothing to install:

```
HKCU\Software\Microsoft\Windows\CurrentVersion\Run  ▸  "ChatGPT Dictate"
```

To turn it off, untick the same menu item — or use **Task Manager ▸ Startup**,
where Windows lists it beside your other login apps.

To see what's registered: `run.bat --check` prints the entry and the exact
command recorded.

If you move the repo folder or upgrade Python, the recorded command is repaired
automatically the next time you start the app by hand.
```

- [ ] **Step 5: Update the file map**

Replace line 261:

```
  sound.py              start/stop/error cues (afplay | winsound)
```

with:

```
  sound.py              start/stop/error cues (afplay | silent on Windows)
  autostart.py          dispatcher ─► autostart_win.py (HKCU Run key); macOS uses a LaunchAgent
```

- [ ] **Step 6: Verify the README renders and is accurate**

Read back the changed sections. Confirm: no remaining reference to `shell:startup`, no remaining claim that Windows uses `winsound`, and the section numbering under *Setup — Windows* runs 1 then 2.

Run: `python -c "import re,io; t=open(r'C:\projects\dictator\README.md',encoding='utf-8').read(); assert 'shell:startup' not in t, 'stale shell:startup reference'; assert 'winsound' not in t, 'stale winsound reference'; print('README clean')"`
Expected: `README clean`

- [ ] **Step 7: Reboot test**

Sign out of Windows and back in (or reboot).

Expected: the tray icon appears on its own with **no console window**, and `run.bat --check` still reports `start at login : enabled`.

- [ ] **Step 8: Commit**

```bash
git add chatgpt_dictate/__main__.py README.md
git commit -m "Document Windows start-at-login and report it in --check"
```

---

## Self-Review

**Spec coverage.** Every spec section maps to a task: dispatcher + implementation → Task 1; tray wiring and `refresh()` at startup → Task 2; `run_check()` line, README, and the manual reboot test → Task 3. The spec's single-instance note needs no code, and its "Non-goals" are restated as Global Constraints.

**Deviation from the spec, deliberate.** The spec listed a five-function interface; this plan adds `command()` and `recorded_command()`. The spec's own Testing section requires `--check` to print "the exact command recorded", which is unreachable without them. Both are trivial reads.

**Placeholder scan.** No TBD/TODO. Every code step carries the actual code; every verification step carries the actual command and its expected output.

**Type consistency.** `command() -> str` and `recorded_command() -> str | None` are defined identically in `autostart_win.py`, in both dispatcher branches, and at every call site in Tasks 2 and 3. `enable()`/`disable()` return `bool` throughout; `refresh()` returns `None` throughout. The registry value name `ChatGPT Dictate` is identical in the module, the plan's `reg query` commands, and the README.
