// Putting the transcript where the cursor is: synthetic Unicode keystrokes
// (needs Accessibility), or the clipboard (needs nothing).
#import <Cocoa/Cocoa.h>
#include <ApplicationServices/ApplicationServices.h>
#include <unistd.h>

#include "mac.h"

int dm_ax_trusted(void) {
	return AXIsProcessTrusted() ? 1 : 0;
}

void dm_ax_prompt(void) {
	// Shows the system "would like to control this computer" dialog and adds
	// Dictator to the Accessibility list (switched off) if it isn't there.
	NSDictionary *opts = @{(__bridge NSString *)kAXTrustedCheckOptionPrompt : @YES};
	AXIsProcessTrustedWithOptions((__bridge CFDictionaryRef)opts);
}

// Post a key down + up with exactly these modifier flags, so a key the user is
// still holding (⌥ from the hotkey, say) can't turn "a" into "å".
static void dm_post_key(CGKeyCode code, CGEventFlags flags, const UniChar *chars, UniCharCount n) {
	CGEventSourceRef src = CGEventSourceCreate(kCGEventSourceStateHIDSystemState);
	for (int down = 1; down >= 0; down--) {
		CGEventRef ev = CGEventCreateKeyboardEvent(src, code, down);
		if (!ev) break;
		CGEventSetFlags(ev, flags);
		if (n > 0) CGEventKeyboardSetUnicodeString(ev, n, chars);
		CGEventPost(kCGHIDEventTap, ev);
		CFRelease(ev);
	}
	if (src) CFRelease(src);
}

static void dm_post_unicode(const UniChar *chars, UniCharCount n) {
	dm_post_key(0, 0, chars, n);
}

void dm_type_text(const char *utf8, int chunk, int delay_ms) {
	NSString *s = [NSString stringWithUTF8String:utf8];
	NSUInteger len = s.length;
	if (chunk < 1) chunk = 1;
	UniChar buf[64];
	if (chunk > 64) chunk = 64;
	NSUInteger i = 0;
	while (i < len) {
		NSUInteger n = MIN((NSUInteger)chunk, len - i);
		// never split a surrogate pair across two events
		if (n < len - i && CFStringIsSurrogateHighCharacter([s characterAtIndex:i + n - 1])) {
			n = (n > 1) ? n - 1 : n + 1;
		}
		[s getCharacters:buf range:NSMakeRange(i, n)];
		dm_post_unicode(buf, n);
		i += n;
		if (delay_ms > 0 && i < len) usleep((useconds_t)delay_ms * 1000);
	}
}

void dm_press_cmd_v(void) {
	dm_post_key((CGKeyCode)9 /* kVK_ANSI_V */, kCGEventFlagMaskCommand, NULL, 0);
}

void dm_clipboard_set(const char *utf8) {
	NSString *s = [NSString stringWithUTF8String:utf8];
	void (^set)(void) = ^{
		NSPasteboard *pb = [NSPasteboard generalPasteboard];
		[pb clearContents];
		[pb setString:s forType:NSPasteboardTypeString];
	};
	if ([NSThread isMainThread]) set(); else dispatch_sync(dispatch_get_main_queue(), set);
}

char *dm_clipboard_get(void) {
	__block char *out = NULL;
	void (^get)(void) = ^{
		NSString *s = [[NSPasteboard generalPasteboard] stringForType:NSPasteboardTypeString];
		if (s) out = strdup(s.UTF8String);
	};
	if ([NSThread isMainThread]) get(); else dispatch_sync(dispatch_get_main_queue(), get);
	return out;
}

int dm_frontmost_pid(void) {
	__block int pid = 0;
	void (^get)(void) = ^{
		pid = (int)[NSWorkspace sharedWorkspace].frontmostApplication.processIdentifier;
	};
	if ([NSThread isMainThread]) get(); else dispatch_sync(dispatch_get_main_queue(), get);
	return pid;
}
