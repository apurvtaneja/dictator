// Floating status pill: a non-activating, click-through panel that never takes
// focus (the user's text field must keep it). The pulsing dot is a Core
// Animation layer animation, rendered by the window server -- the app itself
// does no per-frame work. Nothing exists until the first show.
#import <Cocoa/Cocoa.h>
#import <QuartzCore/QuartzCore.h>

#include "mac.h"

static NSPanel *gPanel;
static NSTextField *gLabel;
static CALayer *gDot;
static NSUInteger gGen;

static void dm_hud_build(void) {
	gPanel = [[NSPanel alloc] initWithContentRect:NSMakeRect(0, 0, 220, 40)
	                                    styleMask:NSWindowStyleMaskBorderless | NSWindowStyleMaskNonactivatingPanel
	                                      backing:NSBackingStoreBuffered
	                                        defer:YES];
	gPanel.level = NSStatusWindowLevel;
	gPanel.collectionBehavior = NSWindowCollectionBehaviorCanJoinAllSpaces |
	                            NSWindowCollectionBehaviorStationary |
	                            NSWindowCollectionBehaviorFullScreenAuxiliary |
	                            NSWindowCollectionBehaviorIgnoresCycle;
	gPanel.ignoresMouseEvents = YES;
	gPanel.opaque = NO;
	gPanel.backgroundColor = [NSColor clearColor];
	gPanel.hasShadow = YES;
	gPanel.hidesOnDeactivate = NO;
	gPanel.releasedWhenClosed = NO;
	gPanel.appearance = [NSAppearance appearanceNamed:NSAppearanceNameVibrantDark];

	NSVisualEffectView *bg = [[NSVisualEffectView alloc] initWithFrame:gPanel.contentView.bounds];
	bg.material = NSVisualEffectMaterialHUDWindow;
	bg.blendingMode = NSVisualEffectBlendingModeBehindWindow;
	bg.state = NSVisualEffectStateActive;
	bg.wantsLayer = YES;
	bg.layer.cornerRadius = 20;
	bg.layer.masksToBounds = YES;
	bg.autoresizingMask = NSViewWidthSizable | NSViewHeightSizable;
	gPanel.contentView = bg;

	gDot = [CALayer layer];
	gDot.frame = CGRectMake(16, 15, 10, 10);
	gDot.cornerRadius = 5;
	[bg.layer addSublayer:gDot];

	gLabel = [NSTextField labelWithString:@""];
	gLabel.font = [NSFont systemFontOfSize:13 weight:NSFontWeightMedium];
	gLabel.textColor = [NSColor whiteColor];
	gLabel.frame = NSMakeRect(36, 11, 170, 18);
	[bg addSubview:gLabel];
}

static void dm_hud_place(int corner) {
	NSPoint mouse = [NSEvent mouseLocation];
	NSScreen *screen = [NSScreen mainScreen];
	for (NSScreen *s in [NSScreen screens]) {
		if (NSPointInRect(mouse, s.frame)) { screen = s; break; }
	}
	NSRect vf = screen.visibleFrame;
	NSRect f = gPanel.frame;
	NSPoint o;
	switch (corner) {
	case 1: o = NSMakePoint(NSMaxX(vf) - f.size.width - 24, NSMinY(vf) + 24); break;               // bottom-right
	case 2: o = NSMakePoint(NSMaxX(vf) - f.size.width - 24, NSMaxY(vf) - f.size.height - 24); break; // top-right
	default: o = NSMakePoint(NSMidX(vf) - f.size.width / 2, NSMinY(vf) + 56); break;                // bottom
	}
	[gPanel setFrameOrigin:o];
}

static void dm_hud_fade(NSUInteger gen) {
	[NSAnimationContext runAnimationGroup:^(NSAnimationContext *ctx) {
		ctx.duration = 0.25;
		gPanel.animator.alphaValue = 0;
	} completionHandler:^{
		if (gen != gGen) return; // shown again meanwhile
		[gDot removeAllAnimations];
		[gPanel orderOut:nil];
	}];
}

void dm_hud_show(int kind, const char *text, int autohide_ms, int corner) {
	NSString *t = [NSString stringWithUTF8String:text ? text : ""];
	dispatch_async(dispatch_get_main_queue(), ^{
		if (kind == DM_HUD_HIDE) {
			if (gPanel && gPanel.visible) dm_hud_fade(++gGen);
			return;
		}
		if (!gPanel) dm_hud_build();
		NSUInteger gen = ++gGen;

		NSColor *c;
		BOOL pulse = YES;
		switch (kind) {
		case DM_HUD_RECORDING: c = [NSColor systemRedColor]; break;
		case DM_HUD_WORKING: c = [NSColor systemBlueColor]; break;
		case DM_HUD_POLISHING: c = [NSColor systemPurpleColor]; break;
		case DM_HUD_SUCCESS: c = [NSColor systemGreenColor]; pulse = NO; break;
		default: c = [NSColor systemOrangeColor]; pulse = NO; break;
		}
		[CATransaction begin];
		[CATransaction setDisableActions:YES];
		gDot.backgroundColor = c.CGColor;
		[CATransaction commit];
		[gDot removeAllAnimations];
		if (pulse) {
			CABasicAnimation *a = [CABasicAnimation animationWithKeyPath:@"opacity"];
			a.fromValue = @1.0;
			a.toValue = @0.3;
			a.duration = 0.6;
			a.autoreverses = YES;
			a.repeatCount = HUGE_VALF;
			a.timingFunction = [CAMediaTimingFunction functionWithName:kCAMediaTimingFunctionEaseInEaseOut];
			[gDot addAnimation:a forKey:@"pulse"];
		}

		gLabel.stringValue = t;
		[gLabel sizeToFit];
		CGFloat w = MAX(150, MIN(460, gLabel.frame.size.width + 56));
		NSRect lf = gLabel.frame;
		lf.origin = NSMakePoint(36, (40 - lf.size.height) / 2);
		gLabel.frame = lf;
		NSRect f = gPanel.frame;
		f.size = NSMakeSize(w, 40);
		[gPanel setFrame:f display:NO];
		dm_hud_place(corner);
		gPanel.alphaValue = 1;
		[gPanel orderFrontRegardless]; // shows without activating the app

		if (autohide_ms > 0) {
			dispatch_after(dispatch_time(DISPATCH_TIME_NOW, (int64_t)autohide_ms * NSEC_PER_MSEC),
			               dispatch_get_main_queue(), ^{
				if (gen == gGen) dm_hud_fade(gen);
			});
		}
	});
}
