"""Cross-platform start/stop/error cues. Best-effort; never raises.

Windows is deliberately silent: the only no-dependency option there is
winsound.MessageBeep, which plays the system alert sounds (the "ding" and the
critical-stop chime). Those are jarring for something that fires on every
dictation, so the HUD overlay is the only feedback on Windows.
"""
from __future__ import annotations

import subprocess

from ._platform import IS_MAC, IS_WIN

_MAC_SOUNDS = {
    "start": "/System/Library/Sounds/Tink.aiff",
    "stop": "/System/Library/Sounds/Pop.aiff",
    "error": "/System/Library/Sounds/Basso.aiff",
}


def play(kind: str) -> None:
    try:
        if IS_MAC:
            path = _MAC_SOUNDS.get(kind)
            if path:
                subprocess.Popen(["/usr/bin/afplay", path])
        elif IS_WIN:
            return  # silent by design -- see the module docstring
        else:
            for player in (["paplay", "/usr/share/sounds/freedesktop/stereo/bell.oga"],
                           ["aplay", "-q", "/usr/share/sounds/alsa/Front_Center.wav"]):
                try:
                    subprocess.Popen(player, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
                    break
                except Exception:
                    continue
    except Exception:
        pass
