# Dictator

Press a hotkey anywhere on your Mac and speak. Clean, correctly punctuated text lands where your
cursor is as soon as you stop. A small language model fixes grammar,
removes filler words, and keeps only what you meant when you correct yourself mid-sentence
("at three, no, four" → "at four").

- **Your own OpenRouter key**, pay as you go. A typical 15-second dictation costs about
  **$0.0006** with the default models, so $1 covers roughly 1,500. Cheaper models are one click
  away in Settings.
- **Tiny and frugal:** one ~14 MB native app (Go + AppKit), about 20–30 MB of memory, and 0% CPU
  while idle. The microphone is open only while you're dictating.
- **Works on locked-down work Macs:** without Accessibility permission the text is copied
  and you press ⌘V. HTTPS uses the system trust store, so TLS-inspecting proxies like
  Zscaler work.

macOS 13 or later, Apple silicon or Intel.

---

## Install

With Homebrew:

```sh
brew install --cask apurvtaneja/tap/dictator
```

Or download `Dictator-x.y.z.zip` from [Releases](https://github.com/apurvtaneja/dictator/releases),
unzip it, and move **Dictator.app** to Applications.

First launch, one time only:

1. Open Dictator from Applications. It isn't notarized by Apple yet, so macOS blocks it: click
   **Done**, not "Move to Trash".
2. Open System Settings ▸ Privacy & Security, scroll down to **Security** and click **Open
   Anyway** next to *"Dictator" was blocked*. The button only appears after step 1, for about
   an hour. Confirm, then click **Open**.
3. Paste your OpenRouter API key ([openrouter.ai/keys](https://openrouter.ai/keys)) in the
   Settings window, click **Check**, then **Save**.
4. Allow the **Microphone** when asked.
5. To have text typed for you, turn Dictator on in System Settings ▸ Privacy & Security ▸
   **Accessibility**. Without it, text is copied and you press ⌘V.

Then press ⌥D anywhere to dictate. Upgrades keep these permissions.

### Build from source

```sh
brew install go          # once (needs Xcode Command Line Tools: xcode-select --install)
make cert                # once: a local signing identity, so macOS remembers permissions
make install             # builds dist/Dictator.app and copies it to /Applications
```

Use **/Applications/Dictator.app**. That's the copy macOS grants microphone and Accessibility
permission to, and the one that starts at login. `dist/` holds build output: the same app before
it's copied, plus bare binaries for the command line.

Why `make cert`: without it the app is signed ad hoc, and macOS ties the Accessibility
permission to that exact build. After every rebuild the switch in System Settings still looks
on, but Dictator can't type and falls back to the clipboard. With the certificate the permission
survives rebuilds. If it ever gets stuck, remove Dictator from the Accessibility list with
**–** and add it again.

Open **Dictator** from Launchpad or Spotlight. A microphone icon appears in the menu bar;
there is no Dock icon.

1. **Add your key.** Settings opens on first run. Paste an OpenRouter API key
   ([openrouter.ai/keys](https://openrouter.ai/keys)) and press **Check**, then **Save**.
   The key is saved in `~/.config/dictator/openrouter.key`, readable only by you.
2. **Dictate.** Press **⌥D** and speak. Each time you pause briefly, that phrase is typed
   about 2 seconds later while you keep talking. Recording stops by itself after a longer
   silence, or press ⌥D again. macOS asks for microphone access the first time.
3. **Auto-typing.** Allow Dictator under System Settings ▸ Privacy & Security ▸
   **Accessibility** (it asks on first launch). Until then, the text is copied and the
   indicator says *Copied — allow Dictator in Accessibility to type*.

The on-screen indicator shows **Recording** (red) while you speak, then **Finishing** /
**Polishing** for the last phrase and **Inserted / Copied** (green) at the end.

### Live typing vs. after you finish

Settings ▸ General ▸ **Typing**:

- **After I finish speaking** (default). Everything is transcribed and cleaned up in one go
  when you stop, so cleanup sees the whole paragraph and corrections across a pause
  ("…on Tuesday." pause "No, Wednesday") are merged.
- **Live.** Only used while grammar cleanup is off; with cleanup on, Dictator always waits
  for the whole dictation, since phrases cleaned one at a time lose the paragraph's thread.
  The recording is cut at each pause of about 0.7 s, once you've said at least 1.5 s, and each
  phrase is transcribed and typed in order while you carry on. OpenRouter's speech-to-text
  takes whole audio files, so text arrives phrase by phrase, not word by word.

If you switch to another app mid-dictation, typing stops and the rest is copied to the
clipboard, so nothing lands in the wrong window.

## How it works

```
⌥D (Carbon hotkey, no polling)
 └─ AudioQueue mic capture, 16 kHz mono ── silence + pause detection (RMS per 100 ms)
     └─ AAC encoding by macOS (≈4 KB per second of speech), a new file at each pause
         └─ per phrase, in parallel: OpenRouter /audio/transcriptions ── openai/gpt-4o-mini-transcribe
             └─ in order: OpenRouter /chat/completions ── deepseek/deepseek-v4.1-flash (cleanup)
                 └─ typed into the focused field (CGEvent Unicode) or copied to the clipboard
```

The defaults were chosen by benchmarking the models on OpenRouter (October 2026) on tricky
dictations: fillers, self-corrections, technical terms, dictated questions, and text that looks
like an instruction.

- **Speech-to-text:** `openai/gpt-4o-mini-transcribe` was fast (~1.1 s) and accurate on
  technical terms. Alternatives in Settings: `mistralai/voxtral-mini-transcribe` (about as
  fast), `elevenlabs/scribe-v2` (very accurate, a little slower), and
  `openai/whisper-large-v3-turbo` (cheapest at ~$0.01 per hour, but it mishears more).
- **Cleanup:** `deepseek/deepseek-v4.1-flash` was the fastest model that got every test right
  (~0.7 s), and it keeps your wording. OpenRouter may route the text to DeepSeek's own servers;
  if that matters to you, choose `anthropic/claude-haiku-5.5` (~1.2 s, careful, lightly
  polishes tone). `google/gemini-2.5-flash-lite` is the cheapest but misses some
  self-corrections. Reasoning is switched off, so cleanup adds only a short round trip. The prompt treats your words as text to edit, never as instructions: a dictated
  question comes back as a question, not an answer. If the model fails, times out, or returns
  something that doesn't look like an edit, you get the raw transcript instead. A dictation is
  never lost. **Menu ▸ Copy last raw transcript** gives you the unedited version.
- **Nothing is sent** when no speech was detected, and phrases that are only a breath or a
  click are skipped. "Thank you."-style transcripts of near-silence are dropped.

## Efficiency

Measured on a MacBook Air, idle, compared with the previous Python + Chromium version:

| | Before | Now |
|---|---|---|
| Memory (physical footprint) | 167 MB in 2 processes | **20–30 MB, 1 process** |
| Idle wakeups | ~16 / s | **~0.5–1.5 / s** (≈ 3 / s after the Settings window has been used) |
| CPU, idle / per 15 s dictation | — | **0.0% / 0.17 s** |
| App size | 390 MB | **14 MB** (universal) |
| Upload per 15 s dictation | — | **~85 KB** (AAC), instead of ~480 KB as WAV |

How it stays this lean:
- Everything is event-driven, with no timers while idle.
- The mic and audio thread exist only while recording.
- The HUD animation runs in Core Animation, not in the app.
- The Settings window is built when opened and freed when closed.
- One HTTP/2 connection is warmed up while you speak and closed after 90 s idle.

Run `make measure` to check these numbers yourself.

## Cost

How long $5 of OpenRouter credit lasts. The numbers are the costs OpenRouter reported for a
17-second dictation in October 2026. Speech-to-text is billed by audio length. Cleanup is billed
mostly per dictation, because its instructions are longer than your text. The table assumes
dictations of about 20 s, so about three cleanups per minute of speech.

| Speech model + cleanup model | Per minute of speech | $5 lasts | At 10 min/day | At 30 min/day |
|---|---|---|---|---|
| **gpt-4o-mini-transcribe + DeepSeek V4.1 Flash** (default) | $0.0023 | ~37 h | ~7 months | ~2.5 months |
| gpt-4o-mini-transcribe + Claude Haiku 5.5 | $0.0022 | ~38 h | ~7.5 months | ~2.5 months |
| ElevenLabs Scribe v2 + Claude Haiku 5.5 | $0.0021 | ~40 h | ~8 months | ~2.5 months |
| Voxtral Mini Transcribe + DeepSeek V4.1 Flash | $0.0032 | ~26 h | ~5 months | ~7 weeks |
| Whisper large-v3-turbo + DeepSeek V4.1 Flash | $0.0005 | ~165 h | ~2.5 years | ~11 months |
| Whisper large-v3-turbo + Gemini 2.5 Flash Lite | $0.00034 | ~245 h | ~4 years | ~1.3 years |

Measured per dictation (about 17 s of speech):

| Speech model | Cost | Cleanup model | Cost |
|---|---|---|---|
| gpt-4o-mini-transcribe | $0.00054 (≈ $0.002/min) | DeepSeek V4.1 Flash | $0.00007–0.00026 |
| ElevenLabs Scribe v2 | $0.00051 (≈ $0.002/min) | Claude Haiku 5.5 | $0.00008 |
| Voxtral Mini Transcribe | $0.00080 (≈ $0.003/min) | Gemini 2.5 Flash Lite | $0.00005 |
| Whisper large-v3-turbo | $0.00006 (≈ $0.0002/min) | | |

- On the default models, a typical 20-second dictation costs about $0.0008, so $5 covers about
  6,500 dictations.
- With the paid speech models, speech-to-text is 80–90% of the cost, so choose a cleanup model
  for quality and privacy rather than price.
- DeepSeek's cost varies up to about 4×. Dictator asks OpenRouter for the fastest provider,
  which isn't always the cheapest, and DeepSeek charges double on weekdays from 01:00 to 04:00
  and 06:00 to 10:00 UTC. Claude Haiku's price is steady.
- Whisper is about 10× cheaper but mishears more words, especially in mixed-language speech.
- Hinglish adds a few lines to the cleanup instructions, about $0.00002 per dictation.
- OpenRouter adds a small fee when you buy credits by card, so $5 paid gives a little less
  than $5 of credit. Run `dictator --mic-test` to see the exact cost of a dictation.

Dictator keeps its own tally from the exact cost OpenRouter reports with each request. The menu
shows what you've spent today and this month. Settings shows the all-time total, your measured
cost per minute and how long $5 lasts at that rate. The tally is stored in
`~/.config/dictator/usage.json` and counts this Mac only.

## Settings

Open them from the menu ▸ **Settings…** (⌘,). They're stored in `~/.config/dictator/config.json`.

| Key | Default | |
|---|---|---|
| `hotkey` | `option+d` | in Settings, click the field and press the keys; in the file e.g. `cmd+shift+d`, `f6` |
| `mode` | `toggle` | `toggle` (tap on / tap off) or `ptt` (hold to talk) |
| `stt_model` | `openai/gpt-4o-mini-transcribe` | any OpenRouter speech-to-text model |
| `language` | `""` (auto) | ISO code such as `en`, if auto-detection guesses wrong; `hinglish` writes Hindi in Roman script (needs `cleanup`) |
| `cleanup` | `true` | grammar cleanup on/off (also in the menu) |
| `cleanup_model` | `deepseek/deepseek-v4.1-flash` | any OpenRouter chat model with reasoning that can be switched off |
| `cleanup_instructions` | `""` | e.g. "British spelling; my name is spelled Apurv" |
| `live` | `false` | type phrase by phrase while you speak (only with `cleanup` off); `false` = all at once when you stop |
| `live_pause_ms` | `700` | the pause that ends a phrase in live mode |
| `insert` | `auto` | `auto`, `clipboard`, `keystroke`, `paste` |
| `paste_threshold` | `280` | longer texts are pasted instead of typed |
| `trailing_space` | `true` | |
| `silence_auto_stop` / `silence_ms` / `silence_min_ms` / `silence_threshold` | `true` / `1800` / `700` / `0.02` | silence auto-stop (toggle mode only) |
| `max_seconds` | `120` | |
| `show_hud` / `hud_corner` | `true` / `bottom` | `bottom`, `bottom-right`, `top-right` |
| `sound_cues` | `true` | |

## Command line

The app binary is also a CLI:
`/Applications/Dictator.app/Contents/MacOS/Dictator`, or `dist/dictator-arm64` after `make`.

```sh
dictator -d                 # start in the background, return to the shell
dictator --status | --stop | --restart
dictator --minimal -d       # headless: no menu-bar icon, no HUD, no sounds, clipboard only
dictator --mic-test         # speak once, print raw + cleaned text, timings and cost
dictator --check            # diagnostics: key, permissions, proxy, OpenRouter reachability
dictator --set-key          # save the API key (reads stdin)
```

Feature flags apply **to that run only** and never change `config.json`:
`--no-tray`, `--no-hud`, `--no-sound`, `--clipboard`, `--insert`, `--mode`, `--hotkey`,
`--stt-model`, `--cleanup` / `--no-cleanup`, `--cleanup-model`, `--config PATH`.

Environment:
- `OPENROUTER_API_KEY` overrides the saved key.
- `OPENROUTER_BASE_URL` points at an OpenRouter-compatible gateway.
- `HTTPS_PROXY` overrides the system proxy.

## Corporate / managed Macs

- **No Accessibility permission?** Dictator copies the text and you press ⌘V. That's one keystroke.
- **TLS inspection (Zscaler etc.):** Go verifies certificates through macOS's Security
  framework, so the corporate root CA in the System keychain is trusted automatically.
- **Proxies:** the system HTTPS proxy is used automatically. PAC files aren't evaluated yet;
  `--check` tells you if one is configured, and you can set `HTTPS_PROXY` instead.
- **openrouter.ai blocked?** Point `OPENROUTER_BASE_URL` at an approved gateway that speaks
  the same API.

## Development

```sh
make cert       # once: local signing identity (keeps Accessibility across rebuilds)
make test       # unit tests (config, cleanup guard, hotkeys, silence/phrase detection, API client, daemon)
make run        # run the bare binary in this terminal
make            # build the universal dist/Dictator.app
make measure    # footprint of the running app
```

Layout:

```
cmd/dictator/        CLI flags, process control, --check / --mic-test
internal/app/        the controller: hotkey → record → transcribe → clean → insert
internal/audio/      AudioQueue capture, AAC encoding, cutting at pauses, silence detection (capture.m)
internal/openrouter/ minimal OpenRouter client (transcriptions, chat, key info)
internal/cleanup/    the grammar-cleanup prompt and its safety guard
internal/mac/        AppKit: menu-bar item, Carbon hotkey + recorder field, HUD, typing, Settings window
internal/config/     ~/.config/dictator/config.json (per-key saves)
internal/apikey/     the API key file (0600)
internal/autostart/  start at login (SMAppService; LaunchAgent for bare binaries)
internal/daemon/     -d / --stop / --status, single-instance flock
internal/sysproxy/   macOS system proxy for HTTPS
internal/hotkey/     "option+d" → key code + modifiers
packaging/           Info.plist, entitlements, icon, measure.sh, make-cert.sh
```

There are no third-party Go modules: only the standard library and Apple frameworks through cgo.

Publishing notes are in [docs/PUBLISHING.md](docs/PUBLISHING.md).
