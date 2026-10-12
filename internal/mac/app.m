#import <Cocoa/Cocoa.h>
#import <ServiceManagement/ServiceManagement.h>

#include "mac.h"
#include "_cgo_export.h"

@interface DMAppDelegate : NSObject <NSApplicationDelegate>
@end

@implementation DMAppDelegate
- (void)applicationDidFinishLaunching:(NSNotification *)n {
	goAppReady();
}
- (NSApplicationTerminateReply)applicationShouldTerminate:(NSApplication *)app {
	return NSTerminateNow;
}
@end

static DMAppDelegate *gDelegate;

// An accessory app has no visible menu bar, but key equivalents still route
// through mainMenu -- without an Edit menu, ⌘V wouldn't paste the API key into
// the Settings window.
static void dm_install_edit_menu(void) {
	NSMenu *main = [[NSMenu alloc] init];
	NSMenuItem *appItem = [[NSMenuItem alloc] init];
	NSMenu *appMenu = [[NSMenu alloc] init];
	[appMenu addItemWithTitle:@"Quit Dictator" action:@selector(terminate:) keyEquivalent:@"q"];
	appItem.submenu = appMenu;
	[main addItem:appItem];

	NSMenuItem *editItem = [[NSMenuItem alloc] init];
	NSMenu *edit = [[NSMenu alloc] initWithTitle:@"Edit"];
	[edit addItemWithTitle:@"Undo" action:@selector(undo:) keyEquivalent:@"z"];
	NSMenuItem *redo = [edit addItemWithTitle:@"Redo" action:@selector(redo:) keyEquivalent:@"z"];
	redo.keyEquivalentModifierMask = NSEventModifierFlagCommand | NSEventModifierFlagShift;
	[edit addItem:[NSMenuItem separatorItem]];
	[edit addItemWithTitle:@"Cut" action:@selector(cut:) keyEquivalent:@"x"];
	[edit addItemWithTitle:@"Copy" action:@selector(copy:) keyEquivalent:@"c"];
	[edit addItemWithTitle:@"Paste" action:@selector(paste:) keyEquivalent:@"v"];
	[edit addItemWithTitle:@"Select All" action:@selector(selectAll:) keyEquivalent:@"a"];
	[edit addItemWithTitle:@"Close Window" action:@selector(performClose:) keyEquivalent:@"w"];
	editItem.submenu = edit;
	[main addItem:editItem];
	NSApp.mainMenu = main;
}

void dm_run(void) {
	@autoreleasepool {
		[NSApplication sharedApplication];
		// Menu-bar agent: no Dock icon, never steals the active Space.
		[NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
		gDelegate = [[DMAppDelegate alloc] init];
		NSApp.delegate = gDelegate;
		dm_install_edit_menu();
		[NSApp run];
	}
}

void dm_quit(void) {
	dispatch_async(dispatch_get_main_queue(), ^{ [NSApp terminate:nil]; });
}

void dm_open_url(const char *url) {
	NSString *s = [NSString stringWithUTF8String:url];
	dispatch_async(dispatch_get_main_queue(), ^{
		NSURL *u = [NSURL URLWithString:s];
		if (u) [[NSWorkspace sharedWorkspace] openURL:u];
	});
}

void dm_sound(const char *name) {
	NSString *s = [NSString stringWithUTF8String:name];
	dispatch_async(dispatch_get_main_queue(), ^{ [[NSSound soundNamed:s] play]; });
}

int dm_in_bundle(void) {
	return [[NSBundle mainBundle] bundleIdentifier] != nil;
}

int dm_login_status(void) {
	if (!dm_in_bundle()) return -1;
	switch ([SMAppService mainAppService].status) {
	case SMAppServiceStatusEnabled: return 1;
	case SMAppServiceStatusRequiresApproval: return 2;
	default: return 0;
	}
}

int dm_login_set(int on) {
	if (!dm_in_bundle()) return -1;
	NSError *err = nil;
	BOOL ok = on ? [[SMAppService mainAppService] registerAndReturnError:&err]
	             : [[SMAppService mainAppService] unregisterAndReturnError:&err];
	if (!ok) NSLog(@"login item: %@", err);
	return ok ? 0 : 1;
}

void dm_about(const char *version) {
	NSString *v = [NSString stringWithUTF8String:version];
	dispatch_async(dispatch_get_main_queue(), ^{
		NSString *credits = @"Press a hotkey anywhere, speak, and clean text lands where your cursor is.\n"
		                    @"Speech-to-text and grammar cleanup by models on OpenRouter, using your own key.\n\n"
		                    @"github.com/apurvtaneja/dictator";
		NSDictionary *opts = @{
			NSAboutPanelOptionApplicationName : @"Dictator",
			NSAboutPanelOptionApplicationVersion : v,
			NSAboutPanelOptionVersion : @"",
			NSAboutPanelOptionCredits : [[NSAttributedString alloc] initWithString:credits attributes:@{
				NSFontAttributeName : [NSFont systemFontOfSize:[NSFont smallSystemFontSize]],
				NSForegroundColorAttributeName : [NSColor secondaryLabelColor],
			}],
		};
		[NSApp activateIgnoringOtherApps:YES];
		[NSApp orderFrontStandardAboutPanelWithOptions:opts];
	});
}
