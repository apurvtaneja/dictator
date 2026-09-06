"""Throwaway diagnostic: log what happens to Windows keyboard focus during dictation.

Run this in one terminal, the app in another, then dictate once:

    python tools/focus_trace.py

It logs two different things, because "focus" on Windows has two layers and the
bug could be in either:

  * FOREGROUND  -- which top-level window owns input (what SendInput types into)
  * FOCUS       -- which control inside that window has the caret

A HUD that steals the foreground shows up as a FOREGROUND line naming a python
process. A hotkey whose Alt keypress leaks through to the app and opens its menu
bar shows up as a FOCUS line going to 0 / a menu, with FOREGROUND unchanged.

Read-only: injects nothing, changes no system setting.
"""
from __future__ import annotations

import ctypes
import sys
import time
from ctypes import wintypes

u32 = ctypes.windll.user32
u32.GetForegroundWindow.restype = wintypes.HWND
u32.GetWindowLongPtrW.restype = ctypes.c_ssize_t
u32.GetWindowLongPtrW.argtypes = [wintypes.HWND, ctypes.c_int]

GWL_EXSTYLE = -20
EX_BITS = {
    0x00000008: "TOPMOST",
    0x00000080: "TOOLWINDOW",
    0x08000000: "NOACTIVATE",
    0x00000020: "TRANSPARENT",
    0x00080000: "LAYERED",
}

# GUITHREADINFO.flags
GUI_INMENUMODE = 0x04
GUI_INMOVESIZE = 0x02
GUI_POPUPMENUMODE = 0x10
GUI_SYSTEMMENUMODE = 0x20
FLAG_NAMES = {
    GUI_INMENUMODE: "IN_MENU_MODE",
    GUI_POPUPMENUMODE: "POPUP_MENU",
    GUI_SYSTEMMENUMODE: "SYSTEM_MENU",
    GUI_INMOVESIZE: "IN_MOVESIZE",
}


class GUITHREADINFO(ctypes.Structure):
    _fields_ = [
        ("cbSize", wintypes.DWORD),
        ("flags", wintypes.DWORD),
        ("hwndActive", wintypes.HWND),
        ("hwndFocus", wintypes.HWND),
        ("hwndCapture", wintypes.HWND),
        ("hwndMenuOwner", wintypes.HWND),
        ("hwndMoveSize", wintypes.HWND),
        ("hwndCaret", wintypes.HWND),
        ("rcCaret", wintypes.RECT),
    ]


T0 = time.time()


def stamp() -> str:
    return f"{time.time() - T0:8.2f}s"


def proc_name(pid: int) -> str:
    PROCESS_QUERY_LIMITED_INFORMATION = 0x1000
    k32 = ctypes.windll.kernel32
    h = k32.OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, False, pid)
    if not h:
        return "?"
    try:
        size = wintypes.DWORD(260)
        buf = ctypes.create_unicode_buffer(size.value)
        if k32.QueryFullProcessImageNameW(h, 0, buf, ctypes.byref(size)):
            return buf.value.rsplit("\\", 1)[-1]
        return "?"
    finally:
        k32.CloseHandle(h)


def describe(h) -> str:
    if not h:
        return "<none>"
    n = u32.GetWindowTextLengthW(h)
    title = ctypes.create_unicode_buffer(n + 1)
    u32.GetWindowTextW(h, title, n + 1)
    cls = ctypes.create_unicode_buffer(256)
    u32.GetClassNameW(h, cls, 256)
    pid = wintypes.DWORD()
    u32.GetWindowThreadProcessId(h, ctypes.byref(pid))
    ex = u32.GetWindowLongPtrW(wintypes.HWND(h), GWL_EXSTYLE) & 0xFFFFFFFF
    flags = ",".join(nm for b, nm in EX_BITS.items() if ex & b) or "-"
    return (f"hwnd=0x{h:X} {proc_name(pid.value)}(pid={pid.value}) "
            f"class={cls.value!r} ex=[{flags}] title={title.value[:50]!r}")


def gui_info(hwnd):
    tid = u32.GetWindowThreadProcessId(hwnd, None) if hwnd else 0
    gti = GUITHREADINFO()
    gti.cbSize = ctypes.sizeof(GUITHREADINFO)
    if not u32.GetGUIThreadInfo(tid, ctypes.byref(gti)):
        return None
    return gti


def main() -> int:
    print("focus_trace: watching foreground + focus. Ctrl+C to stop.\n"
          "Now dictate once into the field that normally breaks.\n", flush=True)

    last_fg = None
    last_focus = None
    last_flags = None

    try:
        while True:
            fg = u32.GetForegroundWindow()
            fg_key = int(fg) if fg else 0
            if fg_key != last_fg:
                last_fg = fg_key
                print(f"{stamp()}  FOREGROUND -> {describe(fg)}", flush=True)

            gti = gui_info(fg)
            if gti is not None:
                focus_key = int(gti.hwndFocus) if gti.hwndFocus else 0
                if focus_key != last_focus:
                    last_focus = focus_key
                    if focus_key:
                        print(f"{stamp()}    focus   -> {describe(gti.hwndFocus)}", flush=True)
                    else:
                        print(f"{stamp()}    focus   -> <NONE>  "
                              f"(no control has the caret — typing goes nowhere)", flush=True)

                mode = gti.flags & (GUI_INMENUMODE | GUI_POPUPMENUMODE |
                                    GUI_SYSTEMMENUMODE | GUI_INMOVESIZE)
                if mode != last_flags:
                    last_flags = mode
                    names = ",".join(nm for b, nm in FLAG_NAMES.items() if mode & b)
                    if names:
                        print(f"{stamp()}    ** {names} — the window is in menu mode; "
                              f"an Alt keypress leaked through to it", flush=True)

            time.sleep(0.03)
    except KeyboardInterrupt:
        print("\nstopped.", flush=True)
    return 0


if __name__ == "__main__":
    sys.exit(main())
