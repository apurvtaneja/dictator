@echo off
REM Start ChatGPT Dictate detached: no console window, and this terminal returns
REM immediately. Use this when you just want it running in the background.
REM
REM pythonw.exe is the console-less build of the interpreter, so there is no
REM window to keep open and none to close by accident. Quit it from the tray
REM menu (right-click the tray icon > Quit) rather than with Ctrl+C.
REM
REM Trade-off: no console means no output. If the tray icon never appears, run
REM   run.bat
REM instead -- it keeps the console so you can read the reason.
cd /d "%~dp0"
start "" pythonw -m chatgpt_dictate %*
