// macOS additions to the NSApplication delegate created by Wails v2, which
// implements neither a Dock reopen handler nor a way to tell an explicit quit
// from a window close (both reach Go as the same "Q" message).

#import <Cocoa/Cocoa.h>
#import <objc/runtime.h>
#include "_cgo_export.h"

static IMP pandoOriginalShouldTerminate;

// Dock icon click: bring back a minimised (Dock) or hidden (tray) window.
static BOOL pandoShouldHandleReopen(id self, SEL _cmd, NSApplication *app, BOOL hasVisibleWindows) {
    [NSApp unhide:nil];
    for (NSWindow *window in [NSApp windows]) {
        if (![window canBecomeMainWindow]) {
            continue;
        }
        if ([window isMiniaturized]) {
            [window deminiaturize:nil];
        }
        [window makeKeyAndOrderFront:nil];
    }
    [NSApp activateIgnoringOtherApps:YES];
    return NO;
}

// System quit (Dock "Quit", app menu role, logout): mark it before Wails
// forwards it to OnBeforeClose.
static NSApplicationTerminateReply pandoShouldTerminate(id self, SEL _cmd, NSApplication *sender) {
    pandoAppWillTerminate();
    if (pandoOriginalShouldTerminate == NULL) {
        return NSTerminateNow;
    }
    return ((NSApplicationTerminateReply (*)(id, SEL, NSApplication *))pandoOriginalShouldTerminate)(self, _cmd, sender);
}

void pandoInstallAppHooks(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        id delegate = [NSApp delegate];
        if (delegate == nil) {
            return;
        }
        Class cls = [delegate class];

        SEL reopen = @selector(applicationShouldHandleReopen:hasVisibleWindows:);
        class_replaceMethod(cls, reopen, (IMP)pandoShouldHandleReopen, "c@:@c");

        SEL terminate = @selector(applicationShouldTerminate:);
        Method m = class_getInstanceMethod(cls, terminate);
        if (m != NULL && pandoOriginalShouldTerminate == NULL) {
            pandoOriginalShouldTerminate = method_setImplementation(m, (IMP)pandoShouldTerminate);
        }
    });
}
