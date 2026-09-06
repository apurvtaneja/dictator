# ChatGPT Dictate (macOS + Windows)

Press a hotkey anywhere, speak, and the transcription lands in whatever field has
focus. Transcription reuses your logged-in **chatgpt.com** web session — no API key.
Runs on stock **Python + PySide6** (Qt 6, includes QtWebEngine) — no `pip install`,
nothing for a network-level TLS proxy to block, since the network call goes out
through the embedded browser's own trusted stack.

Default hotkey **Option+D** (Alt+D on Windows), mode **toggle** (tap to start, tap to
stop), with **silence auto-stop** so a single press is usually enough.

**macOS** vs **Windows**:
- *macOS:* typing the transcript needs the Accessibility permission (admin-gated). On a
  managed Mac without it, the tool copies the text and you press **⌘V** — one keystroke.
- *Windows:* no permission gate — the transcript is **typed straight into the field**,
  no paste step. (Push-to-talk isn't available on Windows — a global hotkey there reports
  only key-down — so it runs as toggle; silence auto-stop covers the gap.)

---

## Screenshots

The floating HUD signals the current state with a small overlay on screen:

| Recording | Transcribing | Copied | No speech |
|---|---|---|---|
| ![Recording](docs/screenshots/hud-recording.png) | ![Transcribing](docs/screenshots/hud-transcribing.png) | ![Copied](docs/screenshots/hud-copied.png) | ![No speech detected](docs/screenshots/hud-nospeech.png) |

---

## How it works

```
hotkey (Option+D / Alt+D)     global hotkey via ctypes — Carbon RegisterEventHotKey
   │  (platform module)        on macOS, user32 RegisterHotKey on Windows.
   ▼
app.py  ── state machine ──►  webengine_backend.py
                              A hidden QtWebEngine page kept loaded on chatgpt.com.
                              An injected script (page/inject.js) does:
                                • navigator.mediaDevices.getUserMedia (mic)
                                • MediaRecorder -> audio/webm;opus blob
                                • silence detection (Web Audio) for auto-stop
                                • fetch('/backend-api/transcribe', Bearer <token>)
                              Running the request *inside the page* means Chromium
                              handles cookies, the Cloudflare clearance, the
                              network-proxy TLS chain, and the User-Agent.
   │  transcript text
   ▼
text insertion                macOS: CoreGraphics Unicode events (needs Accessibility),
   (platform module)          else clipboard + ⌘V. Windows: SendInput types directly.
```

### Platform layout

Everything OS-specific sits behind small dispatchers, so the core is shared:

| Concern | Dispatcher | macOS impl | Windows impl |
|---|---|---|---|
| Global hotkey | `hotkey.py` | `hotkey_mac.py` (Carbon) | `hotkey_win.py` (RegisterHotKey) |
| Text insertion | `textinsert.py` | `inject_mac.py` (CoreGraphics) | `inject_win.py` (SendInput) |
| Window/overlay | `nativewin.py` | `macwin.py` (NSWindow) | `winwin.py` (user32) |
| Sound cues | `sound.py` | `afplay` | none (silent; HUD only) |
| Start at login | `autostart.py` | LaunchAgent (`make_app.sh`) | `autostart_win.py` (HKCU Run key) |

Shared everywhere: `webengine_backend.py`, `page/inject.js`, `auth_window.py`, `hud.py`,
`app.py`, `config.py`. `auth_window.py` is a normal visible chatgpt.com browser used only
for first-time sign-in; it shares the persistent profile with the hidden engine.

---

## Setup — macOS

### 1. Run it

```sh
cd ~/chatgpt-dictate
./run.sh          # or: /usr/bin/python3 -m chatgpt_dictate
```

A grey dot appears in the menu bar. First run: the engine loads chatgpt.com; if
you're not signed in, press the hotkey once (or menu ▸ *Sign in / re-login…*) to
open the login window. Finish signing in there and it closes itself.
Diagnostics without the UI: `./run.sh --check`.

### 2. Grant permissions (System Settings ▸ Privacy & Security)

| Permission | Why | How |
|---|---|---|
| **Microphone** | the engine records your voice | prompted automatically on first recording |
| **Accessibility** | to *type* the transcript (optional) | add manually; without it the tool copies + you press ⌘V |

If you run from a terminal, the permission attaches to the *terminal app*. For a
stable identity, use the app wrapper below and grant permissions to that instead.

### 3. Optional: menu-bar app wrapper + autostart

```sh
sh packaging/make_app.sh                 # builds ~/Applications/ChatGPT Dictate.app
sh packaging/make_app.sh --launch-agent  # also writes a LaunchAgent for login start
```

This only writes a `.plist`, a shell launcher, and a generated icon — it installs
nothing. Then grant Microphone (+ Accessibility if you want auto-typing) to
*ChatGPT Dictate.app*, and add it under System Settings ▸ General ▸ Login Items.

---

## Setup — Windows

**Prerequisite:** Python 3.9+ with **PySide6** installed (`python -c "import PySide6"`
must succeed). If it's missing and you can't `pip install PySide6`, this won't run —
that's the one hard requirement.

### 1. Run it

```bat
cd chatgpt-dictate
run.bat            REM or: python -m chatgpt_dictate
```

`run.bat` keeps the console open so you can watch status lines and quit with
Ctrl+C. To start it **detached** instead — no console window, and the terminal
is free immediately:

```bat
run-bg.bat
```

That one runs under `pythonw`, the console-less interpreter, so quit it from the
tray menu rather than with Ctrl+C. The trade-off is silence: with no console
there is no output, so if the tray icon never appears, run `run.bat` to see why.

A round icon appears in the system tray (click the **^** to show hidden icons). First
run: press the hotkey once (or tray menu ▸ *Sign in / re-login…*) to open the login
window; sign in and it closes itself. Diagnostics: `run.bat --check`.

- **Microphone:** Windows prompts once (Settings ▸ Privacy ▸ Microphone — allow desktop apps).
- **Typing:** works with no permission — the transcript is typed straight in (no paste step).
- Default hotkey is **Alt+D** (the config's `option` maps to Alt; `cmd` maps to the Win key).
- Push-to-talk isn't supported on Windows (global hotkeys report key-down only); it runs as
  toggle. Silence auto-stop makes single-press dictation seamless.

### 2. Start it automatically at login

Tray menu ▸ **Start at login**. One click and it comes back after every reboot,
with no console window — it's launched with `pythonw`, which runs silently.

It writes a single per-user registry value, so there's no admin prompt and
nothing to install:

```
HKCU\Software\Microsoft\Windows\CurrentVersion\Run  ▸  "ChatGPT Dictate"
```

To turn it off, untick the same menu item — or use **Task Manager ▸ Startup**,
where Windows lists it beside your other login apps.

To see what's registered, run `run.bat --check`; it prints whether autostart is
enabled and the exact command recorded.

If you move the repo folder or upgrade Python, the recorded command is repaired
automatically the next time you start the app by hand.

---

## Configuration

`~/.config/chatgpt-dictate/config.json` (created on first run; menu ▸ *Open config file*).

| Key | Default | Notes |
|---|---|---|
| `hotkey` | `"option+d"` | e.g. `"cmd+shift+d"`, `"ctrl+option+d"`, `"f6"` |
| `mode` | `"toggle"` | `"toggle"` or `"ptt"` (hold). Also switchable from the menu. |
| `max_seconds` | `120` | hard cap per recording |
| `insert` | `"clipboard"` | `"clipboard"` (copy → you press ⌘V, no permission), `"keystroke"` or `"paste"` (both need Accessibility; auto-fall back to clipboard if not granted) |
| `paste_threshold` | `280` | keystroke mode auto-uses paste above this length |
| `trailing_space` | `true` | append a space after inserted text |
| `silence_auto_stop` | `true` | **toggle mode:** end recording automatically after a pause, so you only press the hotkey once. Detected in-page (Web Audio), no permission. |
| `silence_ms` | `1800` | how much continuous silence ends the recording |
| `silence_min_ms` | `700` | ignore silence in the first moment (avoids instant stop) |
| `silence_threshold` | `0.02` | RMS level (0..1) below which audio counts as silent — raise it in a noisy room, lower it if it stops too early |
| `show_hud` | `true` | floating on-screen overlay: Recording → Transcribing → Copied ✓ |
| `hud_corner` | `"bottom"` | `"bottom"`, `"bottom-right"`, or `"top-right"` |
| `transcribe_path` | `"/backend-api/transcribe"` | **verify — see below** |
| `audio_field` | `"file"` | multipart field name for the blob |
| `response_key` | `"text"` | JSON key holding the transcript |
| `extra_form_fields` | `{}` | e.g. `{"model": "whisper-1"}` if required |
| `show_engine_window` | `false` | set `true` to watch the hidden browser while debugging |
| `sound_cues` | `true` | start/stop/error sounds (macOS + Linux; Windows is always silent) |

Restart the tool after editing.

---

## Reverse-engineering the endpoint — verify against the live site

`page/inject.js` ships with the historically-correct call, but OpenAI changes these
paths. Confirm the current values and put any differences in `config.json`
(`transcribe_path` / `audio_field` / `response_key` / `extra_form_fields`).

1. Open **chatgpt.com** in Chrome, signed in. DevTools ▸ **Network**, filter **Fetch/XHR**.
2. Click the real **microphone button**, say one sentence, stop.
3. Inspect the request that carries your audio:
   - **Request URL** — the path after the origin. Historically `/backend-api/transcribe`;
     may be `/backend-api/audio/transcriptions` or a `voice`/`realtime` path.
   - **Payload** — under *Form Data* / *Request Payload*, the **field name** of the
     binary part (`file` vs `audio`) and any sibling fields (e.g. `model`).
   - **Request Headers** — confirm `authorization: Bearer …`. Note any of
     `openai-sentinel-token`, `oai-device-id`, `oai-language`, or an
     `openai-…-chat-requirements` header.
   - **Response** — the JSON shape, e.g. `{ "text": "…" }` vs `{ "transcription": "…" }`.
4. Update `config.json` accordingly. If the response key isn't `text`, set `response_key`.

### If an anti-bot token is required

If step 3 shows an `openai-sentinel-token` / chat-requirements header on the
transcribe call, the plain `fetch` in `inject.js` will get a 403 (surfaced as
`reauth` / `http-403`). Because `inject.js` runs in the real page context you can
call the site's own token generator before the fetch — inspect the site JS for the
function that mints `openai-sentinel-token` and add a call in `_finish()` in
`page/inject.js`. Historically the transcribe (Whisper) endpoint did **not** need
it; `/backend-api/conversation` is the one that does.

### Get `show_engine_window: true` for debugging

Set it in config, restart, and you'll see the engine's chatgpt.com page. Open its
DevTools via `QTWEBENGINE_REMOTE_DEBUGGING`:

```sh
QTWEBENGINE_REMOTE_DEBUGGING=9223 /usr/bin/python3 -m chatgpt_dictate
# then open http://localhost:9223 in a browser
```

---

## Proxy / MITM notes

Nothing to configure. QtWebEngine's Chromium and `curl` both use the macOS system
trust store, which already contains any network-proxy (TLS inspection) root CA. The
reverse-engineered call goes out *through* the embedded browser, so it inherits that
trust automatically.

Only relevant if you later add a direct-Python HTTPS backend (e.g. an OpenAI
API-key fallback): build a CA bundle and point Python at it —

```sh
cat "$(/usr/bin/python3 -m certifi)" \
    <(security find-certificate -a -p -c "<Your-Proxy-CA>" /Library/Keychains/System.keychain) \
    > ~/.config/chatgpt-dictate/corp-ca.pem
export SSL_CERT_FILE=~/.config/chatgpt-dictate/corp-ca.pem
```

---

## Troubleshooting

| Symptom | Fix |
|---|---|
| Menu says "Blocked — needs Accessibility permission" | Add the running app (terminal or *ChatGPT Dictate.app*) under Privacy & Security ▸ Accessibility, toggle it on. |
| Hotkey does nothing when run from terminal | Some terminals restrict global hotkeys; build and run `ChatGPT Dictate.app`. |
| "Signed out" persists after logging in | Menu ▸ *Sign in / re-login…*, complete login; the window auto-closes when `/api/auth/session` returns a token. |
| `reauth` / `http-403` on every dictation | The endpoint likely now wants an anti-bot token — see *If an anti-bot token is required* above; or your session expired (re-login). |
| `http-404` / empty transcript | Endpoint path or field name changed — redo the *Reverse-engineering* steps and update `config.json`. |
| Recording never stops / no result | Check `~/.config/chatgpt-dictate/dictate.log`; set `show_engine_window: true` and watch the page. |
| First launch: "cannot be opened" | Right-click the `.app` ▸ Open, once. It's your own unsigned bundle. |

---

## Legal / ToS

`/backend-api/transcribe` is an **undocumented** endpoint. Automating it with your
session cookie may not be permitted by OpenAI's Terms of Service and can break
without notice. This is a personal-use interoperability tool; you're responsible for
how you use it. The backend is isolated (`webengine_backend.py` + the three signals
it emits) so an official `POST /v1/audio/transcriptions` API-key implementation can
replace it without touching the hotkey or text-insertion code.

## Files

```
chatgpt_dictate/
  __main__.py           QApplication + tray icon + wiring
  app.py                recording state machine, auth handling  (OS-agnostic)
  config.py             config.json under ~/.config/chatgpt-dictate/
  webengine_backend.py  hidden chatgpt.com engine: mic capture + transcription
  page/inject.js        in-page script: record, silence-detect, call the endpoint
  auth_window.py        visible chatgpt.com login window
  hud.py                floating on-screen Recording/Transcribing/Copied overlay
  _platform.py          IS_MAC / IS_WIN / paste-key hint
  hotkey.py             dispatcher ─► hotkey_mac.py (Carbon) | hotkey_win.py (RegisterHotKey)
  textinsert.py         dispatcher ─► inject_mac.py (CoreGraphics) | inject_win.py (SendInput)
  nativewin.py          dispatcher ─► macwin.py (NSWindow) | winwin.py (user32)
  sound.py              start/stop/error cues (afplay | silent on Windows)
  autostart.py          dispatcher ─► autostart_win.py (HKCU Run key); macOS uses a LaunchAgent
run.sh / run.bat        launchers (macOS / Windows; console attached)
run-bg.bat              Windows launcher, detached via pythonw (no console)
packaging/
  make_app.sh                    builds ~/Applications/ChatGPT Dictate.app (macOS)
  com.user.chatgpt-dictate.plist LaunchAgent template (macOS)
```
