"""Cross-platform start/stop/error cues. Best-effort; never raises."""
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
            import winsound
            flag = {
                "start": winsound.MB_OK,
                "stop": winsound.MB_ICONASTERISK,
                "error": winsound.MB_ICONHAND,
            }.get(kind, winsound.MB_OK)
            winsound.MessageBeep(flag)
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
