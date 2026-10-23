// macOS menu bar icon for the desktop wrapper. Everything touching AppKit runs
// on the main thread, which Wails' NSApplication run loop services.

#import <Cocoa/Cocoa.h>
#include "_cgo_export.h"

@interface PandoTrayTarget : NSObject
- (void)itemClicked:(id)sender;
@end

@implementation PandoTrayTarget
- (void)itemClicked:(id)sender {
    // Bring the app forward first: the window actions alone do not activate
    // an app whose window was hidden to the menu bar.
    [NSApp activateIgnoringOtherApps:YES];
    pandoTrayItemClicked((int)[sender tag]);
}
@end

static NSStatusItem *pandoStatusItem;
static PandoTrayTarget *pandoTarget;

void pandoTrayStart(const void *icon, int iconLen) {
    // Copied now: the caller frees the buffer when this returns.
    NSData *data = [NSData dataWithBytes:icon length:iconLen];
    dispatch_async(dispatch_get_main_queue(), ^{
        if (pandoStatusItem != nil) {
            return;
        }
        pandoTarget = [PandoTrayTarget new];
        pandoStatusItem = [[NSStatusBar systemStatusBar] statusItemWithLength:NSSquareStatusItemLength];
        NSImage *image = [[NSImage alloc] initWithData:data];
        [image setSize:NSMakeSize(18, 18)];
        pandoStatusItem.button.image = image;
        pandoStatusItem.button.toolTip = @"Pando";
        pandoStatusItem.visible = YES;
        pandoTrayReady();
    });
}

void pandoTraySetMenu(const char *spec) {
    NSString *lines = [NSString stringWithUTF8String:spec];
    dispatch_async(dispatch_get_main_queue(), ^{
        if (pandoStatusItem == nil) {
            return;
        }
        NSMenu *menu = [[NSMenu alloc] initWithTitle:@"Pando"];
        [menu setAutoenablesItems:NO];
        for (NSString *line in [lines componentsSeparatedByString:@"\n"]) {
            NSArray<NSString *> *fields = [line componentsSeparatedByString:@"\t"];
            if (fields.count < 3) {
                continue;
            }
            NSString *kind = fields[0];
            if ([kind isEqualToString:@"s"]) {
                [menu addItem:[NSMenuItem separatorItem]];
                continue;
            }
            NSMenuItem *item = [[NSMenuItem alloc] initWithTitle:fields[2]
                                                          action:@selector(itemClicked:)
                                                   keyEquivalent:@""];
            item.target = pandoTarget;
            item.tag = [fields[1] integerValue];
            item.enabled = ![kind isEqualToString:@"d"];
            [menu addItem:item];
        }
        pandoStatusItem.menu = menu;
    });
}

void pandoTrayStop(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (pandoStatusItem == nil) {
            return;
        }
        [[NSStatusBar systemStatusBar] removeStatusItem:pandoStatusItem];
        pandoStatusItem = nil;
    });
}
