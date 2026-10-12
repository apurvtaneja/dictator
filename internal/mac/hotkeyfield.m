// The hotkey recorder in Settings. While it listens, a local event monitor
// takes every key event in the window (so Return/Esc don't hit Save/Cancel) and
// the global hotkey is suspended (so the current one doesn't start a dictation).
#import "hotkeyfield.h"

#include "mac.h"

static const NSEventModifierFlags kMods = NSEventModifierFlagControl | NSEventModifierFlagOption |
                                          NSEventModifierFlagShift | NSEventModifierFlagCommand;
static const unsigned short kEscape = 0x35;

// Modifier symbols in the order macOS menus show them.
static NSString *dm_mod_symbols(NSEventModifierFlags m) {
	NSMutableString *s = [NSMutableString string];
	if (m & NSEventModifierFlagControl) [s appendString:@"⌃"];
	if (m & NSEventModifierFlagOption) [s appendString:@"⌥"];
	if (m & NSEventModifierFlagShift) [s appendString:@"⇧"];
	if (m & NSEventModifierFlagCommand) [s appendString:@"⌘"];
	return s;
}

static NSString *dm_key_symbol(NSString *name) {
	NSDictionary *special = @{
		@"space" : @"Space", @"return" : @"↩", @"enter" : @"↩", @"tab" : @"⇥",
		@"escape" : @"⎋", @"esc" : @"⎋",
	};
	return special[name] ?: name.uppercaseString;
}

static BOOL dm_is_fkey(NSString *name) {
	return name.length >= 2 && [name characterAtIndex:0] == 'f' &&
	       [[NSCharacterSet decimalDigitCharacterSet] characterIsMember:[name characterAtIndex:1]];
}

// "ctrl+option+d" -> "⌃⌥D"
static NSString *dm_pretty(NSString *spec) {
	NSEventModifierFlags m = 0;
	NSString *key = @"";
	for (NSString *t in [spec.lowercaseString componentsSeparatedByString:@"+"]) {
		if ([@[ @"ctrl", @"control", @"⌃" ] containsObject:t]) m |= NSEventModifierFlagControl;
		else if ([@[ @"option", @"opt", @"alt", @"⌥" ] containsObject:t]) m |= NSEventModifierFlagOption;
		else if ([@[ @"shift", @"⇧" ] containsObject:t]) m |= NSEventModifierFlagShift;
		else if ([@[ @"cmd", @"command", @"⌘" ] containsObject:t]) m |= NSEventModifierFlagCommand;
		else if (t.length) key = dm_key_symbol(t);
	}
	return [dm_mod_symbols(m) stringByAppendingString:key];
}

@implementation DMHotkeyField {
	NSDictionary<NSString *, NSString *> *_names;
	id _monitor;
}

- (instancetype)initWithSpec:(NSString *)spec names:(NSDictionary<NSString *, NSString *> *)names {
	self = [super init];
	_names = names ?: @{};
	self.spec = spec ?: @"";
	self.bezelStyle = NSBezelStyleRounded;
	[self setButtonType:NSButtonTypePushOnPushOff];
	self.target = self;
	self.action = @selector(clicked:);
	self.toolTip = @"Click, then press the shortcut you want";
	[self.widthAnchor constraintEqualToConstant:200].active = YES;
	[self showSpec];
	return self;
}

- (void)showSpec {
	self.state = NSControlStateValueOff;
	self.title = self.spec.length ? dm_pretty(self.spec) : @"Click to set";
}

- (void)clicked:(id)sender {
	if (_monitor) [self stopRecording];
	else [self startRecording];
}

- (void)startRecording {
	dm_hotkey_suspend(1);
	self.state = NSControlStateValueOn;
	self.title = @"Type shortcut…  (Esc cancels)";
	__weak DMHotkeyField *weakSelf = self;
	NSEventMask mask = NSEventMaskKeyDown | NSEventMaskFlagsChanged | NSEventMaskLeftMouseDown;
	_monitor = [NSEvent addLocalMonitorForEventsMatchingMask:mask handler:^NSEvent *(NSEvent *e) {
		return [weakSelf handle:e];
	}];
}

- (void)stopRecording {
	if (!_monitor) return;
	[NSEvent removeMonitor:_monitor];
	_monitor = nil;
	dm_hotkey_suspend(0);
	[self showSpec];
}

- (NSEvent *)handle:(NSEvent *)e {
	if (e.type == NSEventTypeLeftMouseDown) {
		// a click on this button toggles via clicked:; a click elsewhere cancels
		NSPoint p = [self convertPoint:e.locationInWindow fromView:nil];
		if (e.window != self.window || !NSPointInRect(p, self.bounds)) [self stopRecording];
		return e;
	}
	NSEventModifierFlags mods = e.modifierFlags & kMods;
	if (e.type == NSEventTypeFlagsChanged) {
		self.title = mods ? [dm_mod_symbols(mods) stringByAppendingString:@"…"] : @"Type shortcut…  (Esc cancels)";
		return nil;
	}
	if (e.isARepeat) return nil;
	if (e.keyCode == kEscape && !mods) {
		[self stopRecording];
		return nil;
	}
	NSString *name = _names[[NSString stringWithFormat:@"%u", e.keyCode]];
	if (!name) {
		self.title = @"That key isn't supported";
		return nil;
	}
	// a bare (or shift-only) letter would hijack normal typing
	if ((mods == 0 || mods == NSEventModifierFlagShift) && !dm_is_fkey(name)) {
		self.title = @"Add ⌃, ⌥ or ⌘ to the key";
		return nil;
	}
	NSMutableArray *parts = [NSMutableArray array];
	if (mods & NSEventModifierFlagControl) [parts addObject:@"ctrl"];
	if (mods & NSEventModifierFlagOption) [parts addObject:@"option"];
	if (mods & NSEventModifierFlagShift) [parts addObject:@"shift"];
	if (mods & NSEventModifierFlagCommand) [parts addObject:@"cmd"];
	[parts addObject:name];
	self.spec = [parts componentsJoinedByString:@"+"];
	[self stopRecording];
	return nil;
}

@end
