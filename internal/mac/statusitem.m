#import <Cocoa/Cocoa.h>

#include "mac.h"
#include "_cgo_export.h"

@interface DMMenuTarget : NSObject
@end
@implementation DMMenuTarget
- (void)pick:(NSMenuItem *)item {
	goMenu((int)item.tag);
}
@end

static NSStatusItem *gItem;
static NSMenu *gMenu;
static DMMenuTarget *gTarget;
static NSImage *gIcons[4];

// Microphone glyph, drawn with paths so no image assets ship.
static NSImage *dm_mic(NSColor *color, BOOL template) {
	NSImage *img = [NSImage imageWithSize:NSMakeSize(18, 18) flipped:NO drawingHandler:^BOOL(NSRect r) {
		[color set];
		NSBezierPath *body = [NSBezierPath bezierPathWithRoundedRect:NSMakeRect(6.5, 7, 5, 9) xRadius:2.5 yRadius:2.5];
		[body fill];
		NSBezierPath *cradle = [NSBezierPath bezierPath];
		cradle.lineWidth = 1.4;
		cradle.lineCapStyle = NSLineCapStyleRound;
		[cradle appendBezierPathWithArcWithCenter:NSMakePoint(9, 10) radius:4.6 startAngle:200 endAngle:340 clockwise:NO];
		[cradle moveToPoint:NSMakePoint(9, 5.4)];
		[cradle lineToPoint:NSMakePoint(9, 3)];
		[cradle moveToPoint:NSMakePoint(6.5, 2.6)];
		[cradle lineToPoint:NSMakePoint(11.5, 2.6)];
		[cradle stroke];
		return YES;
	}];
	img.template = template;
	return img;
}

static NSMenuItem *dm_add(NSString *title, int tag) {
	NSMenuItem *it = [[NSMenuItem alloc] initWithTitle:title action:@selector(pick:) keyEquivalent:@""];
	it.target = gTarget;
	it.tag = tag;
	[gMenu addItem:it];
	return it;
}

void dm_status_init(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		if (gItem) return;
		gTarget = [[DMMenuTarget alloc] init];
		gIcons[DM_ICON_IDLE] = dm_mic([NSColor blackColor], YES);
		gIcons[DM_ICON_RECORDING] = dm_mic([NSColor systemRedColor], NO);
		gIcons[DM_ICON_BUSY] = dm_mic([NSColor systemBlueColor], NO);
		gIcons[DM_ICON_ERROR] = dm_mic([NSColor systemOrangeColor], NO);

		gMenu = [[NSMenu alloc] init];
		gMenu.autoenablesItems = NO;
		NSMenuItem *status = dm_add(@"Starting…", DM_ITEM_STATUS);
		status.enabled = NO;
		dm_add(@"Add OpenRouter key…", DM_ITEM_ADD_KEY);
		[gMenu addItem:[NSMenuItem separatorItem]];

		NSMenuItem *modeItem = [[NSMenuItem alloc] initWithTitle:@"Mode" action:nil keyEquivalent:@""];
		NSMenu *modeMenu = [[NSMenu alloc] init];
		modeMenu.autoenablesItems = NO;
		NSMenuItem *toggle = [[NSMenuItem alloc] initWithTitle:@"Toggle (tap on / tap off)" action:@selector(pick:) keyEquivalent:@""];
		toggle.target = gTarget; toggle.tag = DM_ITEM_MODE_TOGGLE;
		NSMenuItem *ptt = [[NSMenuItem alloc] initWithTitle:@"Push-to-talk (hold)" action:@selector(pick:) keyEquivalent:@""];
		ptt.target = gTarget; ptt.tag = DM_ITEM_MODE_PTT;
		[modeMenu addItem:toggle];
		[modeMenu addItem:ptt];
		modeItem.submenu = modeMenu;
		[gMenu addItem:modeItem];

		dm_add(@"Fix grammar automatically", DM_ITEM_CLEANUP);
		dm_add(@"Start at login", DM_ITEM_LOGIN);
		[gMenu addItem:[NSMenuItem separatorItem]];
		NSMenuItem *raw = dm_add(@"Copy last raw transcript", DM_ITEM_COPY_RAW);
		raw.enabled = NO;
		NSMenuItem *settings = dm_add(@"Settings…", DM_ITEM_SETTINGS);
		settings.keyEquivalent = @",";
		dm_add(@"Open log", DM_ITEM_LOG);
		[gMenu addItem:[NSMenuItem separatorItem]];
		dm_add(@"About Dictator", DM_ITEM_ABOUT);
		NSMenuItem *quit = dm_add(@"Quit Dictator", DM_ITEM_QUIT);
		quit.keyEquivalent = @"q";

		gItem = [[NSStatusBar systemStatusBar] statusItemWithLength:NSSquareStatusItemLength];
		gItem.button.image = gIcons[DM_ICON_IDLE];
		gItem.button.toolTip = @"Dictator";
		gItem.menu = gMenu;
	});
}

void dm_status_icon(int state) {
	dispatch_async(dispatch_get_main_queue(), ^{
		if (gItem && state >= 0 && state < 4) gItem.button.image = gIcons[state];
	});
}

static NSMenuItem *dm_find(int tag) {
	NSMenuItem *it = [gMenu itemWithTag:tag];
	if (it) return it;
	for (NSMenuItem *m in gMenu.itemArray) {
		if (m.submenu) {
			NSMenuItem *s = [m.submenu itemWithTag:tag];
			if (s) return s;
		}
	}
	return nil;
}

void dm_item_title(int tag, const char *title) {
	NSString *t = [NSString stringWithUTF8String:title];
	dispatch_async(dispatch_get_main_queue(), ^{
		dm_find(tag).title = t;
		if (tag == DM_ITEM_STATUS && gItem) gItem.button.toolTip = [@"Dictator — " stringByAppendingString:t];
	});
}

void dm_item_checked(int tag, int on) {
	dispatch_async(dispatch_get_main_queue(), ^{
		dm_find(tag).state = on ? NSControlStateValueOn : NSControlStateValueOff;
	});
}

void dm_item_enabled(int tag, int on) {
	dispatch_async(dispatch_get_main_queue(), ^{ dm_find(tag).enabled = on != 0; });
}

void dm_item_hidden(int tag, int hidden) {
	dispatch_async(dispatch_get_main_queue(), ^{ dm_find(tag).hidden = hidden != 0; });
}
