#!/bin/sh
# Build a tiny .app wrapper so macOS attaches Microphone/Accessibility permissions
# to a stable identity, gives the tool a Login-Items entry, and hides it from the Dock.
#
# This only writes files (a plist + a shell launcher + a generated icon). It does not
# install any software.
#
# Usage:   sh packaging/make_app.sh            -> ~/Applications/ChatGPT Dictate.app
#          sh packaging/make_app.sh --launch-agent   also writes a LaunchAgent

set -e

REPO_DIR=$(cd "$(dirname "$0")/.." && pwd)
APP_DIR="$HOME/Applications/ChatGPT Dictate.app"
PY=/usr/bin/python3

echo "repo : $REPO_DIR"
echo "app  : $APP_DIR"

rm -rf "$APP_DIR"
mkdir -p "$APP_DIR/Contents/MacOS" "$APP_DIR/Contents/Resources"

cat > "$APP_DIR/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key>            <string>ChatGPT Dictate</string>
  <key>CFBundleDisplayName</key>     <string>ChatGPT Dictate</string>
  <key>CFBundleIdentifier</key>      <string>com.user.chatgpt-dictate</string>
  <key>CFBundleVersion</key>         <string>0.1.0</string>
  <key>CFBundleShortVersionString</key> <string>0.1.0</string>
  <key>CFBundlePackageType</key>     <string>APPL</string>
  <key>CFBundleExecutable</key>      <string>run</string>
  <key>CFBundleIconFile</key>        <string>icon.icns</string>
  <key>LSUIElement</key>             <true/>
  <key>LSMinimumSystemVersion</key>  <string>12.0</string>
  <key>NSMicrophoneUsageDescription</key>
  <string>ChatGPT Dictate records your voice so ChatGPT can transcribe it into text.</string>
  <key>NSBluetoothAlwaysUsageDescription</key>
  <string>Security keys and passkeys may use Bluetooth during ChatGPT sign-in.</string>
  <key>NSBluetoothPeripheralUsageDescription</key>
  <string>Security keys and passkeys may use Bluetooth during ChatGPT sign-in.</string>
  <key>NSCameraUsageDescription</key>
  <string>Scanning a passkey QR code during ChatGPT sign-in may use the camera.</string>
</dict>
</plist>
PLIST

cat > "$APP_DIR/Contents/MacOS/run" <<LAUNCH
#!/bin/sh
export PYTHONPATH="$REPO_DIR:\$PYTHONPATH"
LOG="\$HOME/.config/chatgpt-dictate/dictate.log"
mkdir -p "\$(dirname "\$LOG")"
exec "$PY" -m chatgpt_dictate >> "\$LOG" 2>&1
LAUNCH
chmod +x "$APP_DIR/Contents/MacOS/run"

# generated icon (no external assets)
"$PY" - "$APP_DIR/Contents/Resources/icon.icns" <<'PYICON'
import struct, sys, zlib
# minimal 512x512 solid-ish PNG -> .icns (ic09). Good enough for a menu-bar tool.
def png(w, h, rgba):
    def chunk(t, d):
        return struct.pack(">I", len(d)) + t + d + struct.pack(">I", zlib.crc32(t + d) & 0xffffffff)
    sig = b"\x89PNG\r\n\x1a\n"
    ihdr = struct.pack(">IIBBBBB", w, h, 8, 6, 0, 0, 0)
    raw = bytearray()
    for y in range(h):
        raw.append(0)
        raw.extend(rgba(y))
    idat = zlib.compress(bytes(raw), 9)
    return sig + chunk(b"IHDR", ihdr) + chunk(b"IDAT", idat) + chunk(b"IEND", b"")

W = H = 512
def row(y):
    out = bytearray()
    for x in range(W):
        cx, cy = x - W/2, y - H/2
        inside = (cx*cx + cy*cy) ** 0.5 < W*0.42
        if inside:
            out += bytes((10, 132, 255, 255))
        else:
            out += bytes((0, 0, 0, 0))
    return out

data = png(W, H, row)
icns = b"icns" + struct.pack(">I", 8 + 8 + len(data)) + b"ic09" + struct.pack(">I", 8 + len(data)) + data
open(sys.argv[1], "wb").write(icns)
print("wrote", sys.argv[1])
PYICON

echo "Built $APP_DIR"
echo
echo "Next:"
echo "  1. open \"$APP_DIR\"   (Finder may prompt on first launch)"
echo "  2. System Settings > Privacy & Security > Microphone   -> enable ChatGPT Dictate"
echo "  3. System Settings > Privacy & Security > Accessibility -> add & enable ChatGPT Dictate"
echo "  4. System Settings > General > Login Items              -> add it (optional autostart)"

if [ "$1" = "--launch-agent" ]; then
  PLIST_OUT="$HOME/Library/LaunchAgents/com.user.chatgpt-dictate.plist"
  mkdir -p "$HOME/Library/LaunchAgents"
  cat > "$PLIST_OUT" <<AGENT
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>            <string>com.user.chatgpt-dictate</string>
  <key>ProgramArguments</key>
  <array>
    <string>$APP_DIR/Contents/MacOS/run</string>
  </array>
  <key>RunAtLoad</key>        <true/>
  <key>KeepAlive</key>        <false/>
  <key>ProcessType</key>      <string>Interactive</string>
</dict>
</plist>
AGENT
  echo "Wrote $PLIST_OUT"
  echo "Load it with:  launchctl load -w \"$PLIST_OUT\""
fi
