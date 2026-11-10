//go:build darwin

package main

// Wails' file dialogs use UTType. The wails CLI links this framework itself;
// plain `go build -tags desktop,production` (CI) needs it spelled out.

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework UniformTypeIdentifiers -framework AppKit
#import <AppKit/AppKit.h>

// The label is copied here; AppKit is only touched on the main thread.
static void setBadge(const char *s) {
	NSString *label = s[0] ? [NSString stringWithUTF8String:s] : nil;
	dispatch_async(dispatch_get_main_queue(), ^{ [[NSApp dockTile] setBadgeLabel:label]; });
}
*/
import "C"
import "unsafe"

// setDockBadge shows label on the Dock icon; "" clears it.
func setDockBadge(label string) {
	s := C.CString(label)
	defer C.free(unsafe.Pointer(s))
	C.setBadge(s)
}
