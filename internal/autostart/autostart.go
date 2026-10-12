// Package autostart manages "start at login".
//
// Inside Dictator.app it uses SMAppService (macOS 13+), so the app is listed
// properly under System Settings ▸ General ▸ Login Items. A bare binary (dev
// builds, `go run`) falls back to a per-user LaunchAgent.
package autostart

import (
	"bytes"
	"encoding/xml"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/apurvtaneja/dictator/internal/config"
	"github.com/apurvtaneja/dictator/internal/mac"
)

const label = "io.github.apurvtaneja.dictator"

var plistPath = filepath.Join(os.Getenv("HOME"), "Library", "LaunchAgents", label+".plist")

// Enabled reports whether Dictator starts at login.
func Enabled() bool {
	switch mac.LoginStatus() {
	case 1, 2:
		return true
	case 0:
		return false
	}
	_, err := os.Stat(plistPath)
	return err == nil
}

// NeedsApproval is true when macOS wants the user to allow it in System Settings.
func NeedsApproval() bool { return mac.LoginStatus() == 2 }

func Set(on bool) error {
	if mac.InBundle() {
		removeAgent() // never both mechanisms at once
		if !mac.SetLogin(on) {
			return errString("macOS refused to change the login item")
		}
		return nil
	}
	if !on {
		removeAgent()
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return writeAgent(exe)
}

// Tidy reconciles a LaunchAgent left by the Python version or a dev build.
// Inside the app bundle it moves the user's choice over to SMAppService. In a
// bare binary it only repairs an agent whose program no longer exists -- it
// never repoints a working one at whichever copy happens to run.
func Tidy() {
	data, err := os.ReadFile(plistPath)
	if err != nil {
		return
	}
	if mac.InBundle() {
		removeAgent()
		if mac.LoginStatus() == 0 {
			_ = mac.SetLogin(true)
		}
		return
	}
	if prog := agentProgram(data); prog == "" || !exists(prog) {
		if exe, err := os.Executable(); err == nil {
			removeAgent()
			_ = writeAgent(exe)
		}
	}
}

func agentProgram(plist []byte) string {
	const open = "<key>ProgramArguments</key>"
	i := bytes.Index(plist, []byte(open))
	if i < 0 {
		return ""
	}
	rest := plist[i:]
	a := bytes.Index(rest, []byte("<string>"))
	b := bytes.Index(rest, []byte("</string>"))
	if a < 0 || b < a {
		return ""
	}
	return string(rest[a+len("<string>") : b])
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func removeAgent() {
	if _, err := os.Stat(plistPath); err != nil {
		return
	}
	_ = exec.Command("/bin/launchctl", "bootout", "gui/"+strconv.Itoa(os.Getuid())+"/"+label).Run()
	_ = os.Remove(plistPath)
}

func writeAgent(exe string) error {
	if err := os.MkdirAll(filepath.Dir(plistPath), 0o755); err != nil {
		return err
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>` + label + `</string>
	<key>ProgramArguments</key><array><string>` + xmlEscape(exe) + `</string></array>
	<key>RunAtLoad</key><true/>
	<key>KeepAlive</key><false/>
	<key>ProcessType</key><string>Interactive</string>
	<key>StandardOutPath</key><string>` + xmlEscape(config.LogPath) + `</string>
	<key>StandardErrorPath</key><string>` + xmlEscape(config.LogPath) + `</string>
</dict>
</plist>
`
	return os.WriteFile(plistPath, []byte(plist), 0o644)
}

func xmlEscape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

type errString string

func (e errString) Error() string { return string(e) }
