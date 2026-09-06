"""Entry point: `python3 -m chatgpt_dictate`.

Sets up the QApplication, a menu-bar (tray) icon, and the DictateApp orchestrator.
`python3 -m chatgpt_dictate --check` runs diagnostics and exits.
"""
from __future__ import annotations

import signal
import sys

from PySide6.QtCore import Qt
from PySide6.QtGui import QAction, QColor, QIcon, QPainter, QPixmap
from PySide6.QtWidgets import QApplication, QMenu, QMessageBox, QSystemTrayIcon

from . import config, textinsert
from ._platform import IS_MAC, IS_WIN, PASTE_HINT

_DOT_COLORS = {
    "idle": "#8e8e93",
    "loading": "#ff9f0a",
    "ready": "#8e8e93",
    "recording": "#ff453a",
    "transcribing": "#0a84ff",
    "load-failed": "#ff453a",
}


def _dot_icon(state: str) -> QIcon:
    color = QColor(_DOT_COLORS.get(state, "#8e8e93"))
    pm = QPixmap(22, 22)
    pm.fill(Qt.transparent)
    p = QPainter(pm)
    p.setRenderHint(QPainter.Antialiasing)
    p.setBrush(color)
    p.setPen(Qt.NoPen)
    p.drawEllipse(4, 4, 14, 14)
    p.end()
    return QIcon(pm)


def run_check() -> int:
    cfg = config.load()
    print("chatgpt-dictate diagnostics")
    print("  python           :", sys.version.split()[0], sys.executable)
    try:
        from PySide6 import QtCore
        print("  PySide6 / Qt     :", QtCore.__version__, "/", QtCore.qVersion())
    except Exception as e:
        print("  PySide6          : ERROR", e)
    try:
        from PySide6.QtWebEngineWidgets import QWebEngineView  # noqa: F401
        print("  QtWebEngine      : import OK")
    except Exception as e:
        print("  QtWebEngine      : ERROR", e)
    import sys as _sys
    print("  platform         :", _sys.platform)
    if IS_MAC:
        print("  synthetic input  :", "Accessibility granted" if textinsert.accessibility_ok()
              else "Accessibility NOT granted (auto-typing off; clipboard + " + PASTE_HINT + " used)")
    else:
        print("  synthetic input  :", "available (auto-typing on)" if textinsert.accessibility_ok()
              else "unavailable (clipboard mode)")
    print("  config file      :", config.CONFIG_PATH, "(exists)" if config.CONFIG_PATH.exists() else "(will be created)")
    print("  web profile dir  :", config.PROFILE_DIR)
    print("  hotkey           :", cfg["hotkey"])
    print("  mode             :", cfg["mode"])
    print("  transcribe target:", cfg["chatgpt_origin"] + cfg["transcribe_path"],
          f"(field={cfg['audio_field']!r}, response_key={cfg['response_key']!r})")
    return 0


def run_mic_test() -> int:
    """Load the engine, record ~3s, and print exactly what the microphone did."""
    from PySide6.QtCore import QTimer
    from PySide6.QtWebEngineCore import QWebEngineProfile
    from .webengine_backend import ChatGPTWebBackend

    config.ensure_exists()
    cfg = config.load()
    if "--visible" in sys.argv:
        cfg["show_engine_window"] = True
    app = QApplication(sys.argv)

    profile = QWebEngineProfile("chatgpt-dictate", app)
    profile.setPersistentStoragePath(str(config.PROFILE_DIR))
    profile.setCachePath(str(config.PROFILE_DIR / "cache"))
    profile.setPersistentCookiesPolicy(
        QWebEngineProfile.PersistentCookiesPolicy.ForcePersistentCookies
    )
    be = ChatGPTWebBackend(profile, cfg, app)

    be.state_changed.connect(lambda s: print("  state:", s, flush=True))
    be.auth_changed.connect(lambda a: print("  auth:", a, flush=True))
    be.failed.connect(lambda k: (print("  FAILED:", k, flush=True), app.quit()))
    be.transcribed.connect(lambda t: (print("  TRANSCRIBED:", repr(t), flush=True), app.quit()))

    def go():
        print("  -> start_recording (speak now for 4s)", flush=True)
        be.start_recording()
        QTimer.singleShot(4000, lambda: (print("  -> stop_recording", flush=True), be.stop_recording()))

    def probe():
        be.probe_state(lambda s: print("  __dictate.state():", s, flush=True))

    QTimer.singleShot(6000, go)          # give the page time to load + sign-in check
    for ms in range(4000, 34000, 2000):  # probe every 2s
        QTimer.singleShot(ms, probe)
    QTimer.singleShot(40000, app.quit)   # hard stop
    print("mic test: loading chatgpt.com engine…", flush=True)
    return app.exec()


def run_hud_test() -> int:
    """Cycle the floating HUD through its states so it can be eyeballed."""
    from PySide6.QtCore import QTimer
    from .hud import HUD

    app = QApplication(sys.argv)
    app.setQuitOnLastWindowClosed(False)
    hud = HUD(config.load().get("hud_corner", "bottom"))
    steps = [
        (300, "recording", ""),
        (2500, "transcribing", ""),
        (4500, "copied", "Copied — press ⌘V"),
        (8200, "error", "No speech detected"),
        (12000, "hide", ""),
    ]
    for ms, state, msg in steps:
        QTimer.singleShot(ms, lambda s=state, m=msg: (print("HUD:", s, flush=True), hud.set_state(s, m)))
    QTimer.singleShot(13000, app.quit)
    print("hud test: watch the on-screen overlay cycle states…", flush=True)
    return app.exec()


def main() -> int:
    if "--check" in sys.argv:
        return run_check()
    if "--mic-test" in sys.argv:
        return run_mic_test()
    if "--hud-test" in sys.argv:
        return run_hud_test()

    signal.signal(signal.SIGINT, signal.SIG_DFL)  # let Ctrl+C actually quit

    config.ensure_exists()
    cfg = config.load()

    # single instance: two copies would corrupt the shared web profile
    from PySide6.QtCore import QLockFile
    lock = QLockFile(str(config.CONFIG_DIR / "dictate.lock"))
    lock.setStaleLockTime(30_000)
    if not lock.tryLock(200):
        print(
            "ChatGPT Dictate is already running.\n"
            "Quit the other copy first (Ctrl+C in its terminal, or its menu-bar ▸ Quit).",
            flush=True,
        )
        return 1

    app = QApplication(sys.argv)
    app._dictate_lock = lock  # keep the lock alive for the process lifetime
    app.setApplicationName("ChatGPT Dictate")
    app.setQuitOnLastWindowClosed(False)

    # Become a background agent so showing the HUD / engine windows never drags the
    # user to this process's Space (the terminal's desktop). This is what a proper
    # menu-bar app does; harmless when already launched from the .app bundle.
    try:
        from .nativewin import set_accessory_policy
        set_accessory_policy()
    except Exception:
        pass

    if not QSystemTrayIcon.isSystemTrayAvailable():
        QMessageBox.critical(None, "ChatGPT Dictate", "No system tray available.")
        return 1

    from .app import DictateApp
    engine = DictateApp(cfg)

    tray = QSystemTrayIcon(_dot_icon("idle"))
    tray.setToolTip("ChatGPT Dictate — starting…")

    menu = QMenu()
    act_status = QAction("Starting…", menu)
    act_status.setEnabled(False)
    menu.addAction(act_status)
    menu.addSeparator()

    act_relogin = QAction("Sign in / re-login…", menu)
    act_relogin.triggered.connect(engine.force_relogin)
    menu.addAction(act_relogin)

    mode_menu = menu.addMenu("Mode")
    act_toggle = QAction("Toggle (tap on / tap off)", mode_menu, checkable=True)
    act_ptt = QAction("Push-to-talk (hold)", mode_menu, checkable=True)
    act_toggle.setChecked(cfg.get("mode") == "toggle")
    act_ptt.setChecked(cfg.get("mode") == "ptt")
    act_toggle.triggered.connect(lambda: (_set_mode(engine, act_toggle, act_ptt, "toggle")))
    act_ptt.triggered.connect(lambda: (_set_mode(engine, act_toggle, act_ptt, "ptt")))
    mode_menu.addAction(act_toggle)
    mode_menu.addAction(act_ptt)

    act_config = QAction("Open config file", menu)
    act_config.triggered.connect(lambda: _open_path(config.CONFIG_PATH))
    menu.addAction(act_config)

    act_log = QAction("Open log file", menu)
    act_log.triggered.connect(lambda: _open_path(config.LOG_PATH))
    menu.addAction(act_log)

    menu.addSeparator()
    act_quit = QAction("Quit", menu)
    act_quit.triggered.connect(app.quit)
    menu.addAction(act_quit)

    tray.setContextMenu(menu)
    tray.show()

    if IS_MAC:
        tray_hint = ("  • Look for a small round dot in the menu bar (near the clock).\n"
                     "  • On Macs with a notch, a full menu bar can hide new icons behind the\n"
                     "    notch — quit some menu-bar apps, or use packaging/make_app.sh.\n")
    elif IS_WIN:
        tray_hint = ("  • Look for the round icon in the system tray (click the ^ to show hidden\n"
                     "    icons). Use packaging/run.bat (pythonw) to run without a console window.\n")
    else:
        tray_hint = "  • Look for the tray icon in your notification area.\n"
    print(
        "\nChatGPT Dictate is running.\n"
        f"  • Hotkey: {cfg['hotkey']}\n"
        f"{tray_hint}"
        "  • Leave this window open; press Ctrl+C here to quit.\n",
        flush=True,
    )

    def on_status(text: str) -> None:
        act_status.setText(text)
        tray.setToolTip(f"ChatGPT Dictate — {text}")
        print("•", text, flush=True)

    def on_engine_state(state: str) -> None:
        tray.setIcon(_dot_icon(state))

    def on_notice(title: str, message: str) -> None:
        tray.showMessage(title, message, QSystemTrayIcon.Information, 4000)
        print(f"• {title}: {message}", flush=True)

    engine.status.connect(on_status)
    engine.engine_state.connect(on_engine_state)
    engine.notice.connect(on_notice)

    hud = None
    if cfg.get("show_hud", True):
        from .hud import HUD
        hud = HUD(cfg.get("hud_corner", "bottom"))
        engine.hud_state.connect(hud.set_state)
        app._dictate_hud = hud  # keep a reference alive

    if cfg.get("insert", "auto") != "clipboard" and not textinsert.accessibility_ok():
        # only macOS reaches here (Windows/Linux typing needs no permission)
        print(
            "  Note: Accessibility not granted — running in clipboard mode.\n"
            f"  After you dictate, press {PASTE_HINT} to paste. (Auto-typing needs an admin\n"
            "  to add this app under Privacy & Security ▸ Accessibility.)",
            flush=True,
        )

    app.aboutToQuit.connect(engine.dispose)

    for arg in sys.argv:
        if arg.startswith("--selftest="):
            secs = float(arg.split("=", 1)[1])
            from PySide6.QtCore import QTimer
            QTimer.singleShot(int(secs * 1000), app.quit)

    return app.exec()


def _set_mode(engine, act_toggle, act_ptt, mode: str) -> None:
    engine.set_mode(mode)
    act_toggle.setChecked(mode == "toggle")
    act_ptt.setChecked(mode == "ptt")


def _open_path(path) -> None:
    import subprocess
    from pathlib import Path
    p = Path(path)
    if not p.exists() and p.suffix == ".json":
        config.ensure_exists()
    p.parent.mkdir(parents=True, exist_ok=True)
    if not p.exists():
        p.touch()
    subprocess.Popen(["open", str(p)])


if __name__ == "__main__":
    sys.exit(main())
