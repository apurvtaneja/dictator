// The Settings window. It's created when opened and released when closed, so
// it costs nothing the rest of the time. Go owns the data: dm_settings_open
// gets a JSON snapshot, Save hands JSON back to goSettingsSave, and Go replies
// with dm_settings_status / dm_settings_close.
#import <Cocoa/Cocoa.h>

#include "hotkeyfield.h"
#include "mac.h"
#include "_cgo_export.h"

@interface DMSettings : NSObject <NSWindowDelegate, NSComboBoxDelegate>
@property(strong) NSWindow *window;
@property(strong) NSMutableDictionary<NSString *, NSControl *> *fields;
@property(strong) NSTextField *keyStatus;
@property(strong) NSTextField *saveStatus;
@property(strong) DMHotkeyField *hotkeyField;
@property(strong) NSMutableDictionary<NSString *, NSTextField *> *notes; // model picker key -> its caption
@property(strong) NSDictionary *input;
@end

static DMSettings *gSettings;

static NSTextField *dm_label(NSString *s) {
	NSTextField *l = [NSTextField labelWithString:s];
	l.alignment = NSTextAlignmentRight;
	return l;
}

static NSTextField *dm_caption(NSString *s) {
	NSTextField *l = [NSTextField wrappingLabelWithString:s ?: @""];
	l.font = [NSFont systemFontOfSize:[NSFont smallSystemFontSize]];
	l.textColor = [NSColor secondaryLabelColor];
	l.preferredMaxLayoutWidth = 330;
	return l;
}

static NSTextField *dm_header(NSString *s) {
	NSTextField *l = [NSTextField labelWithString:s];
	l.font = [NSFont boldSystemFontOfSize:[NSFont systemFontSize]];
	return l;
}

@implementation DMSettings

- (NSTextField *)text:(NSString *)key width:(CGFloat)w placeholder:(NSString *)ph secure:(BOOL)secure {
	NSTextField *f = secure ? [[NSSecureTextField alloc] init] : [[NSTextField alloc] init];
	f.placeholderString = ph;
	id v = self.input[key];
	if ([v isKindOfClass:[NSString class]]) f.stringValue = v;
	else if ([v isKindOfClass:[NSNumber class]]) f.stringValue = [v stringValue];
	[f.widthAnchor constraintEqualToConstant:w].active = YES;
	self.fields[key] = f;
	return f;
}

- (NSComboBox *)combo:(NSString *)key presets:(NSArray *)presets {
	NSComboBox *c = [[NSComboBox alloc] init];
	for (id p in presets) [c addItemWithObjectValue:p];
	c.stringValue = self.input[key] ?: @"";
	c.completes = YES;
	c.numberOfVisibleItems = 8;
	c.delegate = self;
	[c.widthAnchor constraintEqualToConstant:330].active = YES;
	self.fields[key] = c;
	return c;
}

// The caption under a model picker describes whichever model is chosen.
- (NSTextField *)noteFor:(NSString *)key {
	NSTextField *l = dm_caption(@"");
	self.notes[key] = l;
	[self showNote:key value:self.input[key]];
	return l;
}

- (void)showNote:(NSString *)key value:(NSString *)value {
	NSDictionary *notes = self.input[[key stringByAppendingString:@"_notes"]];
	NSString *v = [value isKindOfClass:[NSString class]] ? value : @"";
	NSString *note = [notes isKindOfClass:[NSDictionary class]] ? notes[v] : nil;
	if (!note) note = v.length ? @"Custom model: any OpenRouter model ID works." : @"";
	self.notes[key].stringValue = note;
}

- (NSString *)keyOf:(id)control {
	for (NSString *k in self.fields)
		if (self.fields[k] == control) return k;
	return nil;
}

- (void)comboBoxSelectionDidChange:(NSNotification *)n {
	NSComboBox *c = n.object;
	NSString *k = [self keyOf:c];
	if (k && self.notes[k]) [self showNote:k value:c.objectValueOfSelectedItem];
}

- (void)controlTextDidChange:(NSNotification *)n {
	NSString *k = [self keyOf:n.object];
	if (k && self.notes[k]) [self showNote:k value:((NSControl *)n.object).stringValue];
}

- (NSButton *)check:(NSString *)key title:(NSString *)title {
	NSButton *b = [NSButton checkboxWithTitle:title target:nil action:nil];
	b.state = [self.input[key] boolValue] ? NSControlStateValueOn : NSControlStateValueOff;
	self.fields[key] = b;
	return b;
}

- (NSPopUpButton *)popup:(NSString *)key items:(NSArray<NSArray *> *)items {
	NSPopUpButton *p = [[NSPopUpButton alloc] init];
	for (NSArray *it in items) {
		[p addItemWithTitle:it[1]];
		p.lastItem.representedObject = it[0];
	}
	NSString *cur = self.input[key];
	for (NSMenuItem *mi in p.itemArray)
		if ([mi.representedObject isEqual:cur]) [p selectItem:mi];
	self.fields[key] = p;
	return p;
}

- (NSView *)row:(NSArray<NSView *> *)views {
	NSStackView *s = [NSStackView stackViewWithViews:views];
	s.spacing = 8;
	s.alignment = NSLayoutAttributeCenterY;
	return s;
}

- (NSGridView *)grid {
	NSGridView *g = [NSGridView gridViewWithNumberOfColumns:2 rows:0];
	g.rowSpacing = 8;
	g.columnSpacing = 10;
	g.rowAlignment = NSGridRowAlignmentFirstBaseline;
	return g;
}

- (void)section:(NSString *)title in:(NSGridView *)g {
	NSGridRow *r = [g addRowWithViews:@[ dm_header(title), [NSGridCell emptyContentView] ]];
	r.topPadding = g.numberOfRows > 1 ? 12 : 0;
	[r mergeCellsInRange:NSMakeRange(0, 2)];
	[r cellAtIndex:0].xPlacement = NSGridCellPlacementLeading;
}

- (NSTabViewItem *)tab:(NSString *)title grid:(NSGridView *)g {
	[g columnAtIndex:0].xPlacement = NSGridCellPlacementTrailing;
	NSView *box = [[NSView alloc] init];
	g.translatesAutoresizingMaskIntoConstraints = NO;
	[box addSubview:g];
	[NSLayoutConstraint activateConstraints:@[
		[g.topAnchor constraintEqualToAnchor:box.topAnchor constant:16],
		[g.leadingAnchor constraintEqualToAnchor:box.leadingAnchor constant:16],
		[g.trailingAnchor constraintLessThanOrEqualToAnchor:box.trailingAnchor constant:-16],
		[g.bottomAnchor constraintLessThanOrEqualToAnchor:box.bottomAnchor constant:-16],
	]];
	NSTabViewItem *item = [[NSTabViewItem alloc] initWithIdentifier:title];
	item.label = title;
	item.view = box;
	return item;
}

- (void)build {
	self.fields = [NSMutableDictionary dictionary];
	self.notes = [NSMutableDictionary dictionary];
	NSDictionary *in = self.input;
	BOOL hasKey = [in[@"has_key"] boolValue];
	NSView *empty = [NSGridCell emptyContentView];

	// ---------------- tab 1: transcription + cleanup
	NSGridView *t = [self grid];
	[self section:@"OpenRouter" in:t];
	NSTextField *key = [self text:@"api_key" width:330
	                  placeholder:(hasKey ? @"Saved — leave blank to keep" : @"sk-or-v1-…")
	                       secure:YES];
	[t addRowWithViews:@[ dm_label(@"API key"), key ]];
	NSButton *getKey = [NSButton buttonWithTitle:@"Get a key…" target:self action:@selector(getKey:)];
	NSButton *check = [NSButton buttonWithTitle:@"Check" target:self action:@selector(checkKey:)];
	[t addRowWithViews:@[ empty, [self row:@[ getKey, check ]] ]];
	self.keyStatus = dm_caption(in[@"key_hint"]);
	[t addRowWithViews:@[ empty, self.keyStatus ]];
	[t addRowWithViews:@[ dm_label(@"Speech model"), [self combo:@"stt_model" presets:in[@"stt_presets"]] ]];
	[t addRowWithViews:@[ empty, [self noteFor:@"stt_model"] ]];
	[t addRowWithViews:@[ dm_label(@"Language"),
	                      [self text:@"language" width:120 placeholder:@"auto (or en, de…)" secure:NO] ]];

	[self section:@"Grammar cleanup" in:t];
	[t addRowWithViews:@[ empty, [self check:@"cleanup" title:@"Fix grammar, filler words and self-corrections"] ]];
	[t addRowWithViews:@[ dm_label(@"Cleanup model"), [self combo:@"cleanup_model" presets:in[@"cleanup_presets"]] ]];
	[t addRowWithViews:@[ empty, [self noteFor:@"cleanup_model"] ]];
	[t addRowWithViews:@[ dm_label(@"Extra instructions"),
	                      [self text:@"cleanup_instructions" width:330
	                     placeholder:@"e.g. British spelling; spell my name “Apurv”" secure:NO] ]];

	// ---------------- tab 2: general
	NSGridView *g = [self grid];
	[self section:@"Hotkey" in:g];
	self.hotkeyField = [[DMHotkeyField alloc] initWithSpec:in[@"hotkey"] names:in[@"hotkey_keys"]];
	self.fields[@"hotkey"] = self.hotkeyField;
	[g addRowWithViews:@[ dm_label(@"Hotkey"), self.hotkeyField ]];
	[g addRowWithViews:@[ empty, dm_caption(@"Click, then press the keys you want.") ]];
	[g addRowWithViews:@[ dm_label(@"Mode"),
	                      [self popup:@"mode" items:@[ @[ @"toggle", @"Toggle (tap on / tap off)" ],
	                                                   @[ @"ptt", @"Push-to-talk (hold)" ] ]] ]];
	NSButton *login = [self check:@"start_at_login" title:@"Start Dictator when I log in"];
	login.enabled = [in[@"login_supported"] boolValue];
	[g addRowWithViews:@[ empty, login ]];

	[self section:@"Text insertion" in:g];
	[g addRowWithViews:@[ dm_label(@"Typing"),
	                      [self popup:@"typing" items:@[ @[ @"live", @"Live — type each phrase as I pause" ],
	                                                     @[ @"finish", @"After I finish speaking" ] ]] ]];
	[g addRowWithViews:@[ empty, dm_caption(@"Live types while you talk. A correction across a pause "
	                                         @"(“…Tuesday.” pause “No, Wednesday”) can’t be merged; "
	                                         @"“After I finish” cleans up the whole dictation at once.") ]];
	[g addRowWithViews:@[ dm_label(@"Insert"),
	                      [self popup:@"insert" items:@[ @[ @"auto", @"Type it if possible, else copy" ],
	                                                     @[ @"clipboard", @"Copy only — I press ⌘V" ],
	                                                     @[ @"keystroke", @"Type as keystrokes" ],
	                                                     @[ @"paste", @"Copy and press ⌘V for me" ] ]] ]];
	[g addRowWithViews:@[ empty, [self check:@"trailing_space" title:@"Add a space after the text"] ]];
	BOOL ax = [in[@"accessibility"] boolValue];
	if (ax) {
		[g addRowWithViews:@[ empty, dm_caption(@"✓ Typing allowed (Accessibility granted)") ]];
	} else {
		[g addRowWithViews:@[ empty, dm_caption(@"Typing needs Accessibility. Until then, text is copied and you press ⌘V. "
		                                         @"If Dictator is already switched on there, remove it with – and add it "
		                                         @"again: the permission belongs to an older build.") ]];
		[g addRowWithViews:@[ empty, [NSButton buttonWithTitle:@"Open Accessibility Settings…" target:self action:@selector(openAX:)] ]];
	}

	[self section:@"Recording" in:g];
	[g addRowWithViews:@[ empty, [self check:@"silence_auto_stop" title:@"Stop automatically after a pause (toggle mode)"] ]];
	[g addRowWithViews:@[ dm_label(@"Pause length (ms)"), [self text:@"silence_ms" width:80 placeholder:@"1800" secure:NO] ]];
	[g addRowWithViews:@[ dm_label(@"Silence level"), [self text:@"silence_threshold" width:80 placeholder:@"0.02" secure:NO] ]];
	[g addRowWithViews:@[ dm_label(@"Max length (s)"), [self text:@"max_seconds" width:80 placeholder:@"120" secure:NO] ]];

	[self section:@"Feedback" in:g];
	[g addRowWithViews:@[ empty, [self check:@"show_hud" title:@"Show the on-screen indicator"] ]];
	[g addRowWithViews:@[ dm_label(@"Position"),
	                      [self popup:@"hud_corner" items:@[ @[ @"bottom", @"Bottom centre" ],
	                                                         @[ @"bottom-right", @"Bottom right" ],
	                                                         @[ @"top-right", @"Top right" ] ]] ]];
	[g addRowWithViews:@[ empty, [self check:@"sound_cues" title:@"Play start/stop sounds"] ]];

	NSTabView *tabs = [[NSTabView alloc] init];
	[tabs addTabViewItem:[self tab:@"Transcription" grid:t]];
	[tabs addTabViewItem:[self tab:@"General" grid:g]];
	CGFloat h = MAX(t.fittingSize.height, g.fittingSize.height) + 32;
	CGFloat w = MAX(t.fittingSize.width, g.fittingSize.width) + 32;
	[tabs.widthAnchor constraintEqualToConstant:w + 20].active = YES;
	[tabs.heightAnchor constraintEqualToConstant:h + 36].active = YES;

	// ---------------- buttons
	self.saveStatus = dm_caption(@"");
	self.saveStatus.textColor = [NSColor systemRedColor];
	NSButton *cancel = [NSButton buttonWithTitle:@"Cancel" target:self action:@selector(cancel:)];
	cancel.keyEquivalent = @"\033";
	NSButton *save = [NSButton buttonWithTitle:@"Save" target:self action:@selector(save:)];
	save.keyEquivalent = @"\r";
	NSStackView *buttons = [[NSStackView alloc] init];
	buttons.spacing = 8;
	[buttons addView:self.saveStatus inGravity:NSStackViewGravityLeading];
	[buttons addView:cancel inGravity:NSStackViewGravityTrailing];
	[buttons addView:save inGravity:NSStackViewGravityTrailing];

	NSStackView *root = [NSStackView stackViewWithViews:@[ tabs, buttons ]];
	[buttons.widthAnchor constraintEqualToAnchor:tabs.widthAnchor].active = YES; // needs a shared superview first
	root.orientation = NSUserInterfaceLayoutOrientationVertical;
	root.alignment = NSLayoutAttributeTrailing;
	root.spacing = 12;

	self.window = [[NSWindow alloc] initWithContentRect:NSMakeRect(0, 0, 600, 500)
	                                          styleMask:NSWindowStyleMaskTitled | NSWindowStyleMaskClosable
	                                            backing:NSBackingStoreBuffered
	                                              defer:YES];
	self.window.title = @"Dictator Settings";
	self.window.releasedWhenClosed = NO;
	self.window.delegate = self;
	// Pin the content inside explicit margins and size the window from that:
	// a stack view's own edge insets weren't counted, which clipped Save.
	NSView *container = [[NSView alloc] init];
	root.translatesAutoresizingMaskIntoConstraints = NO;
	[container addSubview:root];
	[NSLayoutConstraint activateConstraints:@[
		[root.topAnchor constraintEqualToAnchor:container.topAnchor constant:14],
		[root.leadingAnchor constraintEqualToAnchor:container.leadingAnchor constant:20],
		[root.trailingAnchor constraintEqualToAnchor:container.trailingAnchor constant:-20],
		[root.bottomAnchor constraintEqualToAnchor:container.bottomAnchor constant:-18],
	]];
	self.window.contentView = container;
	[self.window setContentSize:container.fittingSize];
	[self.window center];
	if ([in[@"focus_key"] boolValue]) self.window.initialFirstResponder = key;
}

- (NSDictionary *)collect {
	NSMutableDictionary *out = [NSMutableDictionary dictionary];
	[self.fields enumerateKeysAndObjectsUsingBlock:^(NSString *k, NSControl *c, BOOL *stop) {
		// DMHotkeyField and NSPopUpButton are NSButton subclasses: test them first.
		if ([c isKindOfClass:[DMHotkeyField class]]) {
			out[k] = ((DMHotkeyField *)c).spec ?: @"";
		} else if ([c isKindOfClass:[NSPopUpButton class]]) {
			out[k] = ((NSPopUpButton *)c).selectedItem.representedObject ?: @"";
		} else if ([c isKindOfClass:[NSButton class]]) {
			// @YES/@NO serialise as JSON true/false; @(a == b) would be an int (1/0)
			out[k] = (((NSButton *)c).state == NSControlStateValueOn) ? @YES : @NO;
		} else {
			out[k] = [c.stringValue stringByTrimmingCharactersInSet:[NSCharacterSet whitespaceAndNewlineCharacterSet]];
		}
	}];
	return out;
}

- (void)getKey:(id)s {
	[[NSWorkspace sharedWorkspace] openURL:[NSURL URLWithString:@"https://openrouter.ai/keys"]];
}

- (void)openAX:(id)s {
	[[NSWorkspace sharedWorkspace] openURL:
		[NSURL URLWithString:@"x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility"]];
}

- (void)checkKey:(id)s {
	self.keyStatus.textColor = [NSColor secondaryLabelColor];
	self.keyStatus.stringValue = @"Checking…";
	goSettingsCheckKey((char *)self.fields[@"api_key"].stringValue.UTF8String);
}

- (void)save:(id)s {
	self.saveStatus.stringValue = @"";
	NSData *d = [NSJSONSerialization dataWithJSONObject:[self collect] options:0 error:nil];
	NSString *json = [[NSString alloc] initWithData:d encoding:NSUTF8StringEncoding];
	goSettingsSave((char *)json.UTF8String);
}

- (void)cancel:(id)s {
	[self.window close];
}

- (void)windowWillClose:(NSNotification *)n {
	// Drop focus first: a focused secure field keeps Secure Input (and its
	// event-tap timers) alive even after the window is gone.
	[self.window makeFirstResponder:nil];
	[self.hotkeyField stopRecording]; // gives the global hotkey back
	self.window.delegate = nil;
	self.window.contentView = nil;
	dispatch_async(dispatch_get_main_queue(), ^{
		gSettings = nil; // release everything
		// Back to a menu-bar agent, and give focus back to the previous app.
		[NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
		[NSApp hide:nil];
	});
}
@end

void dm_settings_open(const char *json) {
	NSData *d = [NSData dataWithBytes:json length:strlen(json)];
	dispatch_async(dispatch_get_main_queue(), ^{
		if (!gSettings) {
			gSettings = [[DMSettings alloc] init];
			gSettings.input = [NSJSONSerialization JSONObjectWithData:d options:0 error:nil] ?: @{};
			[gSettings build];
		}
		// A menu-bar agent (accessory app) often never becomes the active app,
		// even when its window is clicked -- then typing and ⌘V go to whatever
		// app was in front. While Settings is open, behave as a regular app
		// (Dock icon, ⌘-Tab, keyboard focus); windowWillClose switches back.
		[NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
		if (@available(macOS 14.0, *)) [NSApp activate];
		[NSApp activateIgnoringOtherApps:YES];
		[gSettings.window makeKeyAndOrderFront:nil];
		[gSettings.window orderFrontRegardless];

		// dev aid: DICTATOR_SETTINGS_SELFTEST=1 presses Save once (unchanged
		// form), exercising the real collect → JSON → Go validation path.
		if (getenv("DICTATOR_SETTINGS_SELFTEST")) [gSettings save:nil];

		// dev aid: DICTATOR_SNAPSHOT=/path.png renders the window to a PNG
		const char *snap = getenv("DICTATOR_SNAPSHOT");
		if (snap) {
			const char *tab = getenv("DICTATOR_SNAPSHOT_TAB");
			if (tab) {
				// the tab view sits inside the root stack view: search depth-first
				NSMutableArray<NSView *> *todo = [NSMutableArray arrayWithObject:gSettings.window.contentView];
				while (todo.count) {
					NSView *sv = todo.lastObject;
					[todo removeLastObject];
					if ([sv isKindOfClass:[NSTabView class]]) [(NSTabView *)sv selectTabViewItemAtIndex:atoi(tab)];
					else [todo addObjectsFromArray:sv.subviews];
				}
			}
			NSView *v = gSettings.window.contentView.superview;
			[v layoutSubtreeIfNeeded];
			NSBitmapImageRep *rep = [v bitmapImageRepForCachingDisplayInRect:v.bounds];
			[v cacheDisplayInRect:v.bounds toBitmapImageRep:rep];
			[[rep representationUsingType:NSBitmapImageFileTypePNG properties:@{}]
				writeToFile:[NSString stringWithUTF8String:snap] atomically:YES];
		}
	});
}

void dm_settings_status(const char *field, const char *text, int ok) {
	NSString *f = [NSString stringWithUTF8String:field];
	NSString *t = [NSString stringWithUTF8String:text];
	dispatch_async(dispatch_get_main_queue(), ^{
		if (!gSettings) return;
		NSTextField *l = [f isEqualToString:@"key"] ? gSettings.keyStatus : gSettings.saveStatus;
		l.stringValue = t;
		l.textColor = ok ? [NSColor systemGreenColor] : [NSColor systemRedColor];
	});
}

void dm_settings_close(void) {
	dispatch_async(dispatch_get_main_queue(), ^{ [gSettings.window close]; });
}
