// Package mac is the thin AppKit layer: menu-bar item, global hotkey, HUD,
// text insertion and the Settings window. The Objective-C side lives in the .m
// files; this file is the Go face of it. Events from AppKit arrive on Events.
package mac

/*
#cgo CFLAGS: -fobjc-arc -Wno-deprecated-declarations
#cgo LDFLAGS: -framework Cocoa -framework Carbon -framework QuartzCore -framework ServiceManagement -framework ApplicationServices
#include <stdlib.h>
#include "mac.h"
*/
import "C"

import (
	"unsafe"
)

// Menu item tags (mirrors mac.h).
const (
	ItemStatus     = C.DM_ITEM_STATUS
	ItemAddKey     = C.DM_ITEM_ADD_KEY
	ItemModeToggle = C.DM_ITEM_MODE_TOGGLE
	ItemModePTT    = C.DM_ITEM_MODE_PTT
	ItemCleanup    = C.DM_ITEM_CLEANUP
	ItemLogin      = C.DM_ITEM_LOGIN
	ItemCopyRaw    = C.DM_ITEM_COPY_RAW
	ItemSettings   = C.DM_ITEM_SETTINGS
	ItemLog        = C.DM_ITEM_LOG
	ItemAbout      = C.DM_ITEM_ABOUT
	ItemQuit       = C.DM_ITEM_QUIT
	ItemUsage      = C.DM_ITEM_USAGE
)

// Menu-bar icon states.
const (
	IconIdle      = C.DM_ICON_IDLE
	IconRecording = C.DM_ICON_RECORDING
	IconBusy      = C.DM_ICON_BUSY
	IconError     = C.DM_ICON_ERROR
)

// HUD kinds.
const (
	HUDHide      = C.DM_HUD_HIDE
	HUDRecording = C.DM_HUD_RECORDING
	HUDWorking   = C.DM_HUD_WORKING
	HUDPolishing = C.DM_HUD_POLISHING
	HUDSuccess   = C.DM_HUD_SUCCESS
	HUDError     = C.DM_HUD_ERROR
)

// Event is something that happened on the AppKit side.
type Event struct {
	Kind    EventKind
	Tag     int    // menu item
	Pressed bool   // hotkey
	Data    string // settings JSON / key to check
}

type EventKind int

const (
	EvReady EventKind = iota
	EvHotkey
	EvMenu
	EvSettingsSave
	EvSettingsCheckKey
)

// Events delivers AppKit events. Buffered: AppKit callbacks never block.
var Events = make(chan Event, 32)

func send(e Event) {
	select {
	case Events <- e:
	default: // consumer stalled: dropping beats freezing the UI thread
	}
}

//export goAppReady
func goAppReady() { send(Event{Kind: EvReady}) }

//export goHotkey
func goHotkey(pressed C.int) { send(Event{Kind: EvHotkey, Pressed: pressed != 0}) }

//export goMenu
func goMenu(tag C.int) { send(Event{Kind: EvMenu, Tag: int(tag)}) }

//export goSettingsSave
func goSettingsSave(json *C.char) { send(Event{Kind: EvSettingsSave, Data: C.GoString(json)}) }

//export goSettingsCheckKey
func goSettingsCheckKey(key *C.char) { send(Event{Kind: EvSettingsCheckKey, Data: C.GoString(key)}) }

func cstr(s string) (*C.char, func()) {
	p := C.CString(s)
	return p, func() { C.free(unsafe.Pointer(p)) }
}

// Run starts AppKit on the calling (main, locked) thread. It never returns.
func Run() { C.dm_run() }

func Quit() { C.dm_quit() }

func OpenURL(u string) {
	p, free := cstr(u)
	defer free()
	C.dm_open_url(p)
}

func Sound(name string) {
	p, free := cstr(name)
	defer free()
	C.dm_sound(p)
}

func InBundle() bool { return C.dm_in_bundle() != 0 }

func About(version string) {
	p, free := cstr(version)
	defer free()
	C.dm_about(p)
}

// LoginStatus: -1 not in an app bundle, 0 off, 1 on, 2 needs approval in System Settings.
func LoginStatus() int      { return int(C.dm_login_status()) }
func SetLogin(on bool) bool { return C.dm_login_set(b2i(on)) == 0 }

func StatusInit()          { C.dm_status_init() }
func StatusIcon(state int) { C.dm_status_icon(C.int(state)) }

func ItemTitle(tag int, title string) {
	p, free := cstr(title)
	defer free()
	C.dm_item_title(C.int(tag), p)
}
func ItemChecked(tag int, on bool) { C.dm_item_checked(C.int(tag), b2i(on)) }
func ItemEnabled(tag int, on bool) { C.dm_item_enabled(C.int(tag), b2i(on)) }
func ItemHidden(tag int, h bool)   { C.dm_item_hidden(C.int(tag), b2i(h)) }

// RegisterHotkey returns the Carbon OSStatus (0 = ok).
func RegisterHotkey(keyCode, modifiers uint32) int {
	return int(C.dm_hotkey_register(C.uint32_t(keyCode), C.uint32_t(modifiers)))
}
func UnregisterHotkey() { C.dm_hotkey_unregister() }

// HUD shows the overlay; corner: 0 bottom, 1 bottom-right, 2 top-right.
func HUD(kind int, text string, autohideMs, corner int) {
	p, free := cstr(text)
	defer free()
	C.dm_hud_show(C.int(kind), p, C.int(autohideMs), C.int(corner))
}

func AXTrusted() bool { return C.dm_ax_trusted() != 0 }

// AXPrompt asks macOS to show its Accessibility permission dialog.
func AXPrompt() { C.dm_ax_prompt() }

// TypeText posts the text as Unicode key events. Blocks; call off the main thread.
func TypeText(s string, chunk, delayMs int) {
	p, free := cstr(s)
	defer free()
	C.dm_type_text(p, C.int(chunk), C.int(delayMs))
}

func PressCmdV() { C.dm_press_cmd_v() }

// FrontmostPID is the process of the frontmost app (where keystrokes go).
func FrontmostPID() int { return int(C.dm_frontmost_pid()) }

func SetClipboard(s string) {
	p, free := cstr(s)
	defer free()
	C.dm_clipboard_set(p)
}

func Clipboard() string {
	p := C.dm_clipboard_get()
	if p == nil {
		return ""
	}
	defer C.free(unsafe.Pointer(p))
	return C.GoString(p)
}

func SettingsOpen(json string) {
	p, free := cstr(json)
	defer free()
	C.dm_settings_open(p)
}

// SettingsStatus shows a message next to the key ("key") or the Save button ("save").
func SettingsStatus(field, text string, ok bool) {
	pf, freef := cstr(field)
	defer freef()
	pt, freet := cstr(text)
	defer freet()
	C.dm_settings_status(pf, pt, b2i(ok))
}

func SettingsClose() { C.dm_settings_close() }

func b2i(b bool) C.int {
	if b {
		return 1
	}
	return 0
}
