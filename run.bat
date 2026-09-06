@echo off
REM Run ChatGPT Dictate on Windows (keeps a console so you can see status / Ctrl+C to quit).
REM For a background launch with no console window, use:  pythonw -m chatgpt_dictate
cd /d "%~dp0"
python -m chatgpt_dictate %*
