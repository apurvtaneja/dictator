// A shortcut recorder: click it, press the keys, and it holds the new spec.
#import <Cocoa/Cocoa.h>

@interface DMHotkeyField : NSButton
@property(copy) NSString *spec; // "ctrl+option+d", in hotkey.Parse syntax
// names maps key codes (as decimal strings) to key names, from hotkey.Names.
- (instancetype)initWithSpec:(NSString *)spec names:(NSDictionary<NSString *, NSString *> *)names;
- (void)stopRecording;
@end
