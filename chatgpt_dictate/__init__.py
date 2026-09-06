"""ChatGPT-powered global dictation for macOS.

A menu-bar tool: press a hotkey anywhere, speak, and the transcribed text is typed
into whatever field has focus. Transcription reuses your logged-in chatgpt.com web
session via an embedded (hidden) QtWebEngine page -- no API key.

Runs on the stock macOS toolchain: /usr/bin/python3 + the already-installed PySide6.
No third-party packages are imported.
"""

__version__ = "0.1.0"
