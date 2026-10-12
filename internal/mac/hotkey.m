// Global hotkey via Carbon's RegisterEventHotKey: no Accessibility permission,
// no event tap, no polling -- the system delivers press and release events.
#import <Cocoa/Cocoa.h>
#include <Carbon/Carbon.h>

#include "mac.h"
#include "_cgo_export.h"

static EventHandlerRef gHandler;
static EventHotKeyRef gHotKey;
static uint32_t gKeyCode, gModifiers; // last registered, for dm_hotkey_suspend
static BOOL gSuspended;

static OSStatus dm_hotkey_event(EventHandlerCallRef next, EventRef ev, void *ud) {
	goHotkey(GetEventKind(ev) == kEventHotKeyPressed ? 1 : 0);
	return noErr;
}

static int dm_hotkey_register_main(uint32_t keycode, uint32_t modifiers) {
	if (!gHandler) {
		EventTypeSpec types[2] = {
			{kEventClassKeyboard, kEventHotKeyPressed},
			{kEventClassKeyboard, kEventHotKeyReleased},
		};
		OSStatus st = InstallEventHandler(GetApplicationEventTarget(), dm_hotkey_event, 2, types, NULL, &gHandler);
		if (st != noErr) return (int)st;
	}
	if (gHotKey) {
		UnregisterEventHotKey(gHotKey);
		gHotKey = NULL;
	}
	EventHotKeyID hid = {'DCTR', 1};
	OSStatus st = RegisterEventHotKey(keycode, modifiers, hid, GetApplicationEventTarget(), 0, &gHotKey);
	if (st == noErr) {
		gKeyCode = keycode;
		gModifiers = modifiers;
		gSuspended = NO;
	}
	return (int)st;
}

int dm_hotkey_register(uint32_t keycode, uint32_t modifiers) {
	if ([NSThread isMainThread]) return dm_hotkey_register_main(keycode, modifiers);
	__block int st = 0;
	dispatch_sync(dispatch_get_main_queue(), ^{ st = dm_hotkey_register_main(keycode, modifiers); });
	return st;
}

void dm_hotkey_unregister(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		if (gHotKey) {
			UnregisterEventHotKey(gHotKey);
			gHotKey = NULL;
		}
	});
}

// While the Settings window records a new shortcut, the current one must not
// fire (Carbon would swallow the key and start a dictation).
void dm_hotkey_suspend(int on) {
	void (^run)(void) = ^{
		if (on && gHotKey) {
			UnregisterEventHotKey(gHotKey);
			gHotKey = NULL;
			gSuspended = YES;
		} else if (!on && gSuspended) {
			gSuspended = NO;
			EventHotKeyID hid = {'DCTR', 1};
			RegisterEventHotKey(gKeyCode, gModifiers, hid, GetApplicationEventTarget(), 0, &gHotKey);
		}
	};
	if ([NSThread isMainThread]) run(); else dispatch_async(dispatch_get_main_queue(), run);
}
