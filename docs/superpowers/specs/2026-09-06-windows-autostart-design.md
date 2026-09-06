# Windows autostart — design

**Date:** 2026-09-06
**Status:** approved, ready for implementation planning

## Problem

The tool has to be started by hand on Windows every session. macOS already has
scripted start-at-login (`packaging/make_app.sh --launch-agent` writes a
LaunchAgent). Windows has nothing equivalent — `README.md:126` only tells the
user to hand-build a shortcut in `shell:startup`.

## Goal

A single, reversible switch that makes the app start at login on Windows with no
console window, managed from inside the app.

## Non-goals

Explicitly out of scope, decided during design:

- **File logging.** `config.LOG_PATH` is defined and the tray has an "Open log
  file" item, but nothing in the codebase writes to it. Running console-less
  means `print()` output is discarded. Accepted: to diagnose a login-time
  failure, launch manually from a terminal.
- Startup error notifications.
- Task Scheduler (delayed start, restart-on-crash).
- macOS autostart — already covered by `make_app.sh`.

## Approach

Write a per-user value under
`HKCU\Software\Microsoft\Windows\CurrentVersion\Run` using stdlib `winreg`.

Chosen over the two alternatives:

| Approach | Why not |
|---|---|
| Startup-folder `.lnk` | Creating a real shortcut with no dependencies needs ~100 lines of ctypes COM (`IShellLinkW` + `IPersistFile`), or Windows Script Host, which Microsoft is removing from Windows 11. A `.cmd` instead flashes a console at every login. |
| Task Scheduler (`schtasks.exe`) | Requires generating task XML and spawning a process; hardest to inspect or undo; heavy for a personal tray tool. |

`Run` wins on: pure stdlib, one piece of state, no admin prompt, and it appears
in **Task Manager ▸ Startup**, giving a second familiar off-switch. Verified on
the target machine that ~20 other apps (Steam, Discord, Docker, Spotify)
register the same way, and that `pythonw.exe` exists alongside `python.exe`
(system Python 3.11, no virtualenv).

If a login-time network race with the chatgpt.com engine load turns out to be a
real problem, Task Scheduler can be swapped in behind the same tray toggle.

## Components

Follows the existing platform-dispatcher pattern (`hotkey.py` → `hotkey_win.py`,
`nativewin.py` → `winwin.py`).

### `chatgpt_dictate/autostart.py` (dispatcher)

On non-Windows, stubs report unsupported so the tray item never appears.

```
supported()  -> bool   can this platform manage autostart?
is_enabled() -> bool   is the Run entry present?
enable()     -> bool   write the entry unconditionally (idempotent)
disable()    -> bool   remove it
refresh()    -> None   rewrite only if already enabled and the command is stale
```

`enable()` always writes. `refresh()` is a no-op unless the entry already exists
and its recorded command differs from the freshly built one.

### `chatgpt_dictate/autostart_win.py` (implementation)

Registry value named `ChatGPT Dictate`, set to:

```
"<pythonw.exe>" -c "import sys; sys.path.insert(0, '<repo>'); from chatgpt_dictate.__main__ import main; sys.exit(main())"
```

- `pythonw.exe` resolves as the sibling of `sys.executable`; falls back to
  `sys.executable` when absent.
- Repo root is `Path(__file__).resolve().parent.parent`.
- `Run` entries get no working directory, which is why the command needs the
  `sys.path` bootstrap rather than plain `-m chatgpt_dictate`.
- The repo path is embedded as a **single-quoted** Python literal with
  backslashes and single quotes escaped. No double quote may appear inside the
  `-c` argument, or the outer command-line quoting breaks. Explorer launches
  `Run` entries via `CreateProcess`, not a shell, so there are no other
  metacharacters to escape.

`is_enabled()` reports presence only, so the checkbox reflects reality even when
the recorded command is stale. `refresh()` compares the stored value against the
freshly built command and rewrites on mismatch.

## Wiring

In `__main__.py`:

- One checkable `QAction`, "Start at login", added between the Mode submenu and
  "Open config file" (around `__main__.py:203`), shown only when
  `autostart.supported()`.
- Initial checked state from `is_enabled()`.
- On toggle: call `enable()`/`disable()`, then re-read `is_enabled()` and set the
  checkbox from that result — a failed write visibly snaps back rather than
  showing a state that isn't true.
- Call `autostart.refresh()` once during startup, after config load.

## Data flow

Enable → `winreg` writes the `Run` value → at next login Explorer runs
`pythonw -c <bootstrap>` → `main()` → tray icon appears, no console window.

## Error handling

No registry call may raise into the UI. Every call is wrapped; `enable()` and
`disable()` report success as a bool, while `is_enabled()` degrades to False and
`refresh()` is best-effort and silent.

| Failure | Behaviour |
|---|---|
| Registry write denied (policy) | `enable()` returns False; checkbox reverts |
| `pythonw.exe` missing | Falls back to `python.exe`; works, but shows a console |
| Repo moved / Python upgraded | Entry is stale until the next manual launch, where `refresh()` repairs it |

### Single-instance interaction

The existing `QLockFile` guard (`__main__.py:150`) already covers the new
overlap: if the app autostarts at login and the user later runs `run.bat`, the
second copy prints "already running" and exits 1. No change required.

`--check`, `--mic-test` and `--hud-test` do not touch autostart state.

## Testing

The repo has no test framework, so verification is manual plus one automated
surface.

**Automated:** extend `run_check()` so `run.bat --check` prints whether
autostart is registered and the exact command recorded. This reuses the existing
diagnostics channel and partly offsets the decision to skip file logging.

**Manual:**

1. Toggle "Start at login" on.
2. Confirm the value exists (`reg query "HKCU\Software\Microsoft\Windows\CurrentVersion\Run" /v "ChatGPT Dictate"`) and appears in Task Manager ▸ Startup.
3. Sign out and back in; confirm the tray icon returns with **no console window**.
4. Toggle off; confirm the value is gone and the app does not start at next login.

## Size

Two new files; roughly 20 lines of edits to `__main__.py`; a README update to
replace the manual `shell:startup` instructions at `README.md:126`.
