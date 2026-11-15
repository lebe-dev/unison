//go:build darwin && cgo && pasteboardtest

package clipboard

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AppKit

#import <AppKit/AppKit.h>
#include <stdlib.h>
#include <string.h>

// uc_test_save serializes every item and type of the general pasteboard into a
// binary property list allocated with malloc, so the test run can put the user
// content back.
static void *uc_test_save(size_t *len) {
	@autoreleasepool {
		NSMutableArray *items = [NSMutableArray array];
		for (NSPasteboardItem *item in [[NSPasteboard generalPasteboard] pasteboardItems]) {
			NSMutableDictionary *entry = [NSMutableDictionary dictionary];
			for (NSPasteboardType type in [item types]) {
				NSData *data = [item dataForType:type];
				if (data != nil) {
					entry[type] = data;
				}
			}
			[items addObject:entry];
		}
		NSData *plist = [NSPropertyListSerialization
			dataWithPropertyList:items
			format:NSPropertyListBinaryFormat_v1_0
			options:0
			error:NULL];
		if (plist == nil) {
			return NULL;
		}
		void *buffer = malloc(plist.length);
		if (buffer == NULL) {
			return NULL;
		}
		memcpy(buffer, plist.bytes, plist.length);
		*len = plist.length;
		return buffer;
	}
}

static void uc_test_restore(const void *bytes, size_t len) {
	@autoreleasepool {
		NSPasteboard *pb = [NSPasteboard generalPasteboard];
		[pb clearContents];
		if (bytes == NULL || len == 0) {
			return;
		}
		NSData *plist = [NSData dataWithBytes:bytes length:len];
		NSArray *items = [NSPropertyListSerialization
			propertyListWithData:plist
			options:NSPropertyListImmutable
			format:NULL
			error:NULL];
		if (items == nil) {
			return;
		}
		NSMutableArray *objects = [NSMutableArray array];
		for (NSDictionary *entry in items) {
			NSPasteboardItem *item = [[NSPasteboardItem alloc] init];
			for (NSPasteboardType type in entry) {
				[item setData:entry[type] forType:type];
			}
			[objects addObject:item];
			[item release];
		}
		[pb writeObjects:objects];
	}
}

static void uc_test_write(const char *text) {
	@autoreleasepool {
		NSPasteboard *pb = [NSPasteboard generalPasteboard];
		NSPasteboardItem *item = [[NSPasteboardItem alloc] init];
		[item setString:[NSString stringWithUTF8String:text]
			forType:@"public.utf8-plain-text"];
		[pb clearContents];
		[pb writeObjects:@[item]];
		[item release];
	}
}

// uc_test_write_marked writes text together with a marker type.
static void uc_test_write_marked(const char *text, const char *type) {
	@autoreleasepool {
		NSPasteboard *pb = [NSPasteboard generalPasteboard];
		NSPasteboardItem *item = [[NSPasteboardItem alloc] init];
		[item setString:[NSString stringWithUTF8String:text]
			forType:@"public.utf8-plain-text"];
		[item setData:[NSData data]
			forType:[NSString stringWithUTF8String:type]];
		[pb clearContents];
		[pb writeObjects:@[item]];
		[item release];
	}
}
*/
import "C"

import "unsafe"

// savePasteboard serializes the current pasteboard for a later restore.
func savePasteboard() []byte {
	var size C.size_t
	ptr := C.uc_test_save(&size)
	if ptr == nil {
		return nil
	}
	defer C.free(ptr)
	return C.GoBytes(ptr, C.int(size))
}

// restorePasteboard puts back the content captured by savePasteboard.
func restorePasteboard(saved []byte) {
	ptr := C.CBytes(saved)
	defer C.free(ptr)
	C.uc_test_restore(ptr, C.size_t(len(saved)))
}

// writePlainText replaces the pasteboard with plain text only.
func writePlainText(text string) {
	c := C.CString(text)
	defer C.free(unsafe.Pointer(c))
	C.uc_test_write(c)
}

// writeMarkedText replaces the pasteboard with plain text and a marker type.
func writeMarkedText(text, marker string) {
	c := C.CString(text)
	defer C.free(unsafe.Pointer(c))
	m := C.CString(marker)
	defer C.free(unsafe.Pointer(m))
	C.uc_test_write_marked(c, m)
}
