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
// content back. A file URL is saved as its text: the raw file URL data carries
// a reference to the writing process and does not survive it.
static void *uc_test_save(size_t *len) {
	@autoreleasepool {
		NSMutableArray *items = [NSMutableArray array];
		for (NSPasteboardItem *item in [[NSPasteboard generalPasteboard] pasteboardItems]) {
			NSMutableDictionary *entry = [NSMutableDictionary dictionary];
			for (NSPasteboardType type in [item types]) {
				NSData *data = NULL;
				if ([type isEqualToString:@"public.file-url"]) {
					data = [[item stringForType:type] dataUsingEncoding:NSUTF8StringEncoding];
				} else {
					data = [item dataForType:type];
				}
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
				if ([type isEqualToString:@"public.file-url"]) {
					NSString *value = [[NSString alloc] initWithData:entry[type]
						encoding:NSUTF8StringEncoding];
					if (value != nil) {
						[item setString:value forType:type];
					}
					[value release];
					continue;
				}
				[item setData:entry[type] forType:type];
			}
			[objects addObject:item];
			[item release];
		}
		[pb writeObjects:objects];
		// Reading every item back starts the hand-off to the server, which
		// otherwise drops the items this process never touched.
		for (NSPasteboardItem *written in [pb pasteboardItems]) {
			for (NSPasteboardType type in [written types]) {
				(void)[written dataForType:type];
			}
		}
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

// uc_test_write_image writes image data as TIFF, which is what a screenshot
// offers next to PNG. with_png adds the PNG type too.
static int uc_test_write_image(const void *png, size_t len, int with_png) {
	@autoreleasepool {
		NSData *data = [NSData dataWithBytes:png length:len];
		NSBitmapImageRep *rep = [NSBitmapImageRep imageRepWithData:data];
		if (rep == nil) {
			return 1;
		}
		NSData *tiff = [rep representationUsingType:NSBitmapImageFileTypeTIFF properties:@{}];
		if (tiff == nil) {
			return 1;
		}
		NSPasteboardItem *item = [[NSPasteboardItem alloc] init];
		[item setData:tiff forType:@"public.tiff"];
		if (with_png) {
			[item setData:data forType:@"public.png"];
		}
		NSPasteboard *pb = [NSPasteboard generalPasteboard];
		[pb clearContents];
		BOOL written = [pb writeObjects:@[item]];
		[item release];
		return written ? 0 : 1;
	}
}

// uc_test_write_files writes one file URL item per path, like Finder does.
static void uc_test_write_files(const char *const *paths, size_t count) {
	@autoreleasepool {
		NSMutableArray *objects = [NSMutableArray arrayWithCapacity:count];
		for (size_t i = 0; i < count; i++) {
			NSString *path = [NSString stringWithUTF8String:paths[i]];
			if (path == nil) {
				continue;
			}
			NSPasteboardItem *item = [[NSPasteboardItem alloc] init];
			[item setString:[[NSURL fileURLWithPath:path] absoluteString]
				forType:@"public.file-url"];
			[objects addObject:item];
			[item release];
		}
		NSPasteboard *pb = [NSPasteboard generalPasteboard];
		[pb clearContents];
		[pb writeObjects:objects];
	}
}

// uc_test_write_marked_files writes one file URL item per path and adds a
// marker type to every item.
static void uc_test_write_marked_files(const char *const *paths, size_t count, const char *type) {
	@autoreleasepool {
		NSString *marker = [NSString stringWithUTF8String:type];
		NSMutableArray *objects = [NSMutableArray arrayWithCapacity:count];
		for (size_t i = 0; i < count; i++) {
			NSString *path = [NSString stringWithUTF8String:paths[i]];
			if (path == nil) {
				continue;
			}
			NSPasteboardItem *item = [[NSPasteboardItem alloc] init];
			[item setString:[[NSURL fileURLWithPath:path] absoluteString]
				forType:@"public.file-url"];
			[item setData:[NSData data] forType:marker];
			[objects addObject:item];
			[item release];
		}
		NSPasteboard *pb = [NSPasteboard generalPasteboard];
		[pb clearContents];
		[pb writeObjects:objects];
	}
}

// uc_test_item_count returns the number of pasteboard items.
static size_t uc_test_item_count(void) {
	@autoreleasepool {
		return [[[NSPasteboard generalPasteboard] pasteboardItems] count];
	}
}

// uc_test_file_path returns the file path of one pasteboard item, allocated
// with malloc, or NULL.
static char *uc_test_file_path(size_t index) {
	@autoreleasepool {
		NSArray *items = [[NSPasteboard generalPasteboard] pasteboardItems];
		if (index >= [items count]) {
			return NULL;
		}
		NSString *value = [items[index] stringForType:@"public.file-url"];
		if (value == nil) {
			return NULL;
		}
		NSURL *url = [NSURL URLWithString:value];
		if (url == nil || ![url isFileURL]) {
			return NULL;
		}
		const char *path = [[url path] fileSystemRepresentation];
		return path == NULL ? NULL : strdup(path);
	}
}

// uc_test_data returns the data of one type from the pasteboard, allocated with
// malloc, or NULL.
static void *uc_test_data(const char *name, size_t *len) {
	@autoreleasepool {
		NSData *data = [[NSPasteboard generalPasteboard]
			dataForType:[NSString stringWithUTF8String:name]];
		if (data == nil) {
			return NULL;
		}
		void *buffer = malloc(data.length);
		if (buffer == NULL) {
			return NULL;
		}
		memcpy(buffer, data.bytes, data.length);
		*len = data.length;
		return buffer;
	}
}
*/
import "C"

import (
	"testing"
	"unsafe"
)

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

// writeImage replaces the pasteboard with image data offered as public.tiff.
// withPNG adds the PNG type next to it, the way a screenshot does.
func writeImage(t *testing.T, data []byte, withPNG bool) {
	t.Helper()
	ptr := C.CBytes(data)
	defer C.free(ptr)
	if C.uc_test_write_image(ptr, C.size_t(len(data)), cBool(withPNG)) != 0 {
		t.Fatal("writing the image to the pasteboard failed")
	}
}

// writeFileURLs replaces the pasteboard with one file URL item per path.
func writeFileURLs(t *testing.T, paths ...string) {
	t.Helper()
	writeFilesWithMarker(t, "", paths...)
}

// writeFilesWithMarker writes file URL items and adds a marker type to every
// item when marker is not empty.
func writeFilesWithMarker(t *testing.T, marker string, paths ...string) {
	t.Helper()
	if len(paths) == 0 {
		t.Fatal("no paths to write")
	}
	values := make([]*C.char, 0, len(paths))
	for _, path := range paths {
		value := C.CString(path)
		defer C.free(unsafe.Pointer(value))
		values = append(values, value)
	}
	if marker == "" {
		C.uc_test_write_files(&values[0], C.size_t(len(values)))
		return
	}
	name := C.CString(marker)
	defer C.free(unsafe.Pointer(name))
	C.uc_test_write_marked_files(&values[0], C.size_t(len(values)), name)
}

// pasteboardItemCount returns the number of items on the pasteboard.
func pasteboardItemCount() int {
	return int(C.uc_test_item_count())
}

// pasteboardFilePaths returns the file path of every file URL item.
func pasteboardFilePaths(t *testing.T) []string {
	t.Helper()
	paths := make([]string, 0, pasteboardItemCount())
	for i := range pasteboardItemCount() {
		path := C.uc_test_file_path(C.size_t(i))
		if path == nil {
			continue
		}
		paths = append(paths, C.GoString(path))
		C.free(unsafe.Pointer(path))
	}
	return paths
}

// pasteboardData returns the data of one type on the pasteboard.
func pasteboardData(t *testing.T, typ string) []byte {
	t.Helper()
	name := C.CString(typ)
	defer C.free(unsafe.Pointer(name))
	var size C.size_t
	ptr := C.uc_test_data(name, &size)
	if ptr == nil {
		return nil
	}
	defer C.free(ptr)
	return C.GoBytes(ptr, C.int(size))
}
