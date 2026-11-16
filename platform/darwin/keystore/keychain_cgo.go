//go:build darwin && cgo

package keystore

/*
#cgo LDFLAGS: -framework Security -framework CoreFoundation

#include <limits.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <Security/Security.h>

// uk_keychain stores the keychain every item lives in: the default keychain of
// the process, or the login keychain of the user when the process has none. The
// reference is kept for the lifetime of the process.
static OSStatus uk_keychain(SecKeychainRef *keychain) {
#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Wdeprecated-declarations"
	OSStatus status = SecKeychainCopyDefault(keychain);
	if (status == errSecSuccess && *keychain != NULL) {
		return errSecSuccess;
	}
	if (*keychain != NULL) {
		CFRelease(*keychain);
		*keychain = NULL;
	}
	const char *home = getenv("HOME");
	if (home == NULL || home[0] == '\0') {
		return status;
	}
	static const char *files[] = { "login.keychain-db", "login.keychain" };
	for (size_t i = 0; i < sizeof(files) / sizeof(files[0]); i++) {
		char path[PATH_MAX];
		int size = snprintf(path, sizeof(path), "%s/Library/Keychains/%s", home, files[i]);
		if (size < 0 || (size_t)size >= sizeof(path) || access(path, F_OK) != 0) {
			continue;
		}
		*keychain = NULL;
		if (SecKeychainOpen(path, keychain) == errSecSuccess && *keychain != NULL) {
			return errSecSuccess;
		}
		if (*keychain != NULL) {
			CFRelease(*keychain);
			*keychain = NULL;
		}
	}
#pragma clang diagnostic pop
	return status;
}

// uk_query builds the item attributes shared by every operation. The returned
// dictionary matches only non-synchronizable items of the given service in the
// given keychain, so no operation falls back to the default keychain.
static CFMutableDictionaryRef uk_query(SecKeychainRef keychain, const char *service, const char *account) {
	CFMutableDictionaryRef query = CFDictionaryCreateMutable(
		kCFAllocatorDefault, 0,
		&kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	if (query == NULL) {
		return NULL;
	}
	CFStringRef svc = CFStringCreateWithCString(
		kCFAllocatorDefault, service, kCFStringEncodingUTF8);
	CFStringRef acc = CFStringCreateWithCString(
		kCFAllocatorDefault, account, kCFStringEncodingUTF8);
	if (svc == NULL || acc == NULL) {
		if (svc != NULL) {
			CFRelease(svc);
		}
		if (acc != NULL) {
			CFRelease(acc);
		}
		CFRelease(query);
		return NULL;
	}
	CFDictionarySetValue(query, kSecClass, kSecClassGenericPassword);
	CFDictionarySetValue(query, kSecAttrService, svc);
	CFDictionarySetValue(query, kSecAttrAccount, acc);
	CFDictionarySetValue(query, kSecAttrSynchronizable, kCFBooleanFalse);
	CFDictionarySetValue(query, kSecUseKeychain, keychain);
	CFRelease(svc);
	CFRelease(acc);
	return query;
}

static OSStatus uk_store(SecKeychainRef keychain, const char *service, const char *account, const void *data, size_t len) {
	CFDataRef value = CFDataCreate(kCFAllocatorDefault, data, (CFIndex)len);
	if (value == NULL) {
		return errSecAllocate;
	}
	CFMutableDictionaryRef item = uk_query(keychain, service, account);
	if (item == NULL) {
		CFRelease(value);
		return errSecAllocate;
	}
	CFDictionarySetValue(item, kSecValueData, value);
	CFDictionarySetValue(item, kSecAttrAccessible, kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly);

	OSStatus status = SecItemAdd(item, NULL);
	CFRelease(item);
	if (status == errSecDuplicateItem) {
		CFMutableDictionaryRef match = uk_query(keychain, service, account);
		CFMutableDictionaryRef attrs = CFDictionaryCreateMutable(
			kCFAllocatorDefault, 0,
			&kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
		if (match == NULL || attrs == NULL) {
			if (match != NULL) {
				CFRelease(match);
			}
			if (attrs != NULL) {
				CFRelease(attrs);
			}
			CFRelease(value);
			return errSecAllocate;
		}
		CFDictionarySetValue(attrs, kSecValueData, value);
		CFDictionarySetValue(attrs, kSecAttrAccessible, kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly);
		status = SecItemUpdate(match, attrs);
		CFRelease(attrs);
		CFRelease(match);
	}
	CFRelease(value);
	return status;
}

// uk_result carries the outcome of a load. An empty secret has a size of 0.
typedef struct {
	OSStatus status;
	void *data;
	size_t len;
} uk_result;

// uk_load copies the item data into a buffer allocated with malloc, which the
// caller releases with free.
static uk_result uk_load(SecKeychainRef keychain, const char *service, const char *account) {
	uk_result res = { errSecSuccess, NULL, 0 };
	CFMutableDictionaryRef query = uk_query(keychain, service, account);
	if (query == NULL) {
		res.status = errSecAllocate;
		return res;
	}
	CFDictionarySetValue(query, kSecReturnData, kCFBooleanTrue);
	CFDictionarySetValue(query, kSecMatchLimit, kSecMatchLimitOne);

	CFTypeRef result = NULL;
	res.status = SecItemCopyMatching(query, &result);
	CFRelease(query);
	if (res.status != errSecSuccess) {
		return res;
	}
	if (result == NULL || CFGetTypeID(result) != CFDataGetTypeID()) {
		if (result != NULL) {
			CFRelease(result);
		}
		res.status = errSecDecode;
		return res;
	}

	CFIndex len = CFDataGetLength((CFDataRef)result);
	void *buf = malloc((size_t)len > 0 ? (size_t)len : 1);
	if (buf == NULL) {
		CFRelease(result);
		res.status = errSecAllocate;
		return res;
	}
	if (len > 0) {
		memcpy(buf, CFDataGetBytePtr((CFDataRef)result), (size_t)len);
	}
	CFRelease(result);
	res.data = buf;
	res.len = (size_t)len;
	return res;
}

static OSStatus uk_delete(SecKeychainRef keychain, const char *service, const char *account) {
	CFMutableDictionaryRef query = uk_query(keychain, service, account);
	if (query == NULL) {
		return errSecAllocate;
	}
	OSStatus status = SecItemDelete(query);
	CFRelease(query);
	return status;
}
*/
import "C"

import (
	"context"
	"fmt"
	"sync"
	"unsafe"

	"github.com/lebe-dev/unison/core/ports"
)

// keychainOnce resolves the keychain of the process. The reference lives as
// long as the process does.
var (
	keychainOnce sync.Once
	keychainRef  C.SecKeychainRef
	keychainErr  error
)

// keychain returns the keychain every operation names, or ErrNoKeychain. The
// Keychain API never falls back to a default keychain of its own, so a process
// without one fails here instead of waiting for a dialog.
func keychain() (C.SecKeychainRef, error) {
	keychainOnce.Do(func() {
		var ref C.SecKeychainRef
		if C.uk_keychain(&ref) != C.errSecSuccess {
			keychainErr = ErrNoKeychain
			return
		}
		keychainRef = ref
	})
	return keychainRef, keychainErr
}

// Load returns the secret stored under name, or ports.ErrNotFound.
func (k *Keystore) Load(ctx context.Context, name string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	chain, err := keychain()
	if err != nil {
		return nil, fmt.Errorf("keystore: load %q: %w", name, err)
	}
	service, account := k.cstrings(name)
	defer C.free(unsafe.Pointer(service))
	defer C.free(unsafe.Pointer(account))

	res := C.uk_load(chain, service, account)
	if res.status == C.errSecItemNotFound {
		return nil, fmt.Errorf("keystore: load %q: %w", name, ports.ErrNotFound)
	}
	if res.status != C.errSecSuccess {
		return nil, fmt.Errorf("keystore: load %q: %w", name, &Error{Op: "load", Status: int32(res.status)})
	}
	defer C.free(res.data)
	return C.GoBytes(res.data, C.int(res.len)), nil
}

// Store replaces the secret stored under name.
func (k *Keystore) Store(ctx context.Context, name string, secret []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	chain, err := keychain()
	if err != nil {
		return fmt.Errorf("keystore: store %q: %w", name, err)
	}
	service, account := k.cstrings(name)
	defer C.free(unsafe.Pointer(service))
	defer C.free(unsafe.Pointer(account))

	// The Keychain keeps its own copy, so the C buffer is cleared right away.
	data := C.CBytes(secret)
	status := C.uk_store(chain, service, account, data, C.size_t(len(secret)))
	if len(secret) > 0 {
		C.memset(data, 0, C.size_t(len(secret)))
	}
	C.free(data)
	if status != C.errSecSuccess {
		return fmt.Errorf("keystore: store %q: %w", name, &Error{Op: "store", Status: int32(status)})
	}
	return nil
}

// Delete removes the secret stored under name. Deleting a missing secret is
// not an error.
func (k *Keystore) Delete(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	chain, err := keychain()
	if err != nil {
		return fmt.Errorf("keystore: delete %q: %w", name, err)
	}
	service, account := k.cstrings(name)
	defer C.free(unsafe.Pointer(service))
	defer C.free(unsafe.Pointer(account))

	status := C.uk_delete(chain, service, account)
	if status == C.errSecSuccess || status == C.errSecItemNotFound {
		return nil
	}
	return fmt.Errorf("keystore: delete %q: %w", name, &Error{Op: "delete", Status: int32(status)})
}

func (k *Keystore) cstrings(name string) (service, account *C.char) {
	return C.CString(k.service), C.CString(name)
}
