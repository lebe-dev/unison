package keystore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/lebe-dev/unison/core/ports"
)

// fakeConnect returns a connect function for the fake keyring.
func fakeConnect(ring *fakeKeyring) func() (busConn, error) {
	return func() (busConn, error) { return ring, nil }
}

// noBus reports a session without a bus.
func noBus() (busConn, error) {
	return nil, errors.New("no session bus")
}

// TestOpenDetectsSecretService picks the Secret Service when the session has
// one.
func TestOpenDetectsSecretService(t *testing.T) {
	ring := newFakeKeyring()
	k, err := Open(t.TempDir(), testService, Options{connect: fakeConnect(ring)})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got := k.Backend(); got != BackendSecretService {
		t.Fatalf("Backend() = %q, want %q", got, BackendSecretService)
	}
}

// TestOpenWithoutServiceUsesFiles picks files when the service is missing.
func TestOpenWithoutServiceUsesFiles(t *testing.T) {
	ring := newFakeKeyring()
	ring.service = false
	k, err := Open(t.TempDir(), testService, Options{connect: fakeConnect(ring)})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got := k.Backend(); got != BackendFile {
		t.Fatalf("Backend() = %q, want %q", got, BackendFile)
	}
	if k.Warning() == "" {
		t.Fatal("the file backend reports no warning")
	}
}

// TestOpenWithoutUsableCollectionUsesFiles picks files when the service is on
// the bus but exports no collection that the secrets could go to, as an
// autologin session with a locked login keyring does.
func TestOpenWithoutUsableCollectionUsesFiles(t *testing.T) {
	dir := t.TempDir()
	ring := newFakeKeyring()
	ring.absentCollections = map[dbus.ObjectPath]bool{
		ring.defaultAlias: true,
		ring.loginAlias:   true,
	}
	k, err := Open(dir, testService, Options{connect: fakeConnect(ring)})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got := k.Backend(); got != BackendFile {
		t.Fatalf("Backend() = %q, want %q", got, BackendFile)
	}
	if warning := k.Warning(); !strings.Contains(warning, "no usable collection") {
		t.Fatalf("Warning() = %q, want the reason of the fallback", warning)
	}
	if err := k.Store(context.Background(), "test/secret", []byte("value")); err != nil {
		t.Fatalf("Store: %v", err)
	}
	if ring.created != 0 {
		t.Fatalf("the keyring got %d items, want none", ring.created)
	}
}

// TestOpenRecordedSecretServiceWithoutCollection keeps the backend of a
// directory whose keyring lost its collection instead of moving the keys.
func TestOpenRecordedSecretServiceWithoutCollection(t *testing.T) {
	dir := t.TempDir()
	writeRecord(t, dir, BackendSecretService)
	ring := newFakeKeyring()
	ring.absentCollections = map[dbus.ObjectPath]bool{
		ring.defaultAlias: true,
		ring.loginAlias:   true,
	}
	if _, err := Open(dir, testService, Options{connect: fakeConnect(ring)}); !errors.Is(err, ErrKeyringUnavailable) {
		t.Fatalf("Open = %v, want ErrKeyringUnavailable", err)
	}
}

// TestOpenFallsBackWhenKeyringIsLocked keeps the daemon startable when nobody
// answers the prompt of a locked keyring: the keys go to files.
func TestOpenFallsBackWhenKeyringIsLocked(t *testing.T) {
	ring := newFakeKeyring()
	ring.collectionLocked = true
	ring.silentPrompt = true
	k, err := Open(t.TempDir(), testService, Options{
		connect:       fakeConnect(ring),
		PromptTimeout: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got := k.Backend(); got != BackendFile {
		t.Fatalf("Backend() = %q, want %q", got, BackendFile)
	}
	if warning := k.Warning(); !strings.Contains(warning, "locked") {
		t.Fatalf("Warning() = %q, want the reason of the fallback", warning)
	}
	if err := k.Store(context.Background(), "test/secret", []byte("value")); err != nil {
		t.Fatalf("Store: %v", err)
	}
}

// TestOpenWithoutBusUsesFiles picks files when the session has no bus.
func TestOpenWithoutBusUsesFiles(t *testing.T) {
	k, err := Open(t.TempDir(), testService, Options{connect: noBus})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got := k.Backend(); got != BackendFile {
		t.Fatalf("Backend() = %q, want %q", got, BackendFile)
	}
}

// TestOpenRecordsBackendOnFirstStore records the backend before the first
// secret and keeps it.
func TestOpenRecordsBackendOnFirstStore(t *testing.T) {
	dir := t.TempDir()
	ring := newFakeKeyring()
	ring.service = false
	opts := Options{connect: fakeConnect(ring)}
	k, err := Open(dir, testService, opts)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	record := filepath.Join(dir, keysDirName, backendFileName)
	if _, err := os.Stat(record); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the backend was recorded before the first secret: %v", err)
	}
	if err := k.Store(context.Background(), "store.dek", []byte("key")); err != nil {
		t.Fatalf("Store: %v", err)
	}
	data, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != string(BackendFile) {
		t.Fatalf("recorded backend is %q, want %q", data, BackendFile)
	}
	info, err := os.Stat(record)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("record mode is %o, want 600", got)
	}

	// A second open reads the record, and the Secret Service of the session no
	// longer matters.
	ringBack := newFakeKeyring()
	again, err := Open(dir, testService, Options{connect: fakeConnect(ringBack)})
	if err != nil {
		t.Fatalf("Open again: %v", err)
	}
	if got := again.Backend(); got != BackendFile {
		t.Fatalf("Backend() = %q, want the recorded %q", got, BackendFile)
	}
	secret, err := again.Load(context.Background(), "store.dek")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(secret) != "key" {
		t.Fatalf("loaded secret is %q, want %q", secret, "key")
	}
}

// TestOpenReadsRecordedSecretService uses the recorded backend even when a new
// directory would pick files.
func TestOpenReadsRecordedSecretService(t *testing.T) {
	dir := t.TempDir()
	writeRecord(t, dir, BackendSecretService)
	ring := newFakeKeyring()
	k, err := Open(dir, testService, Options{connect: fakeConnect(ring)})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got := k.Backend(); got != BackendSecretService {
		t.Fatalf("Backend() = %q, want %q", got, BackendSecretService)
	}
	if k.Warning() != "" {
		t.Fatalf("the Secret Service backend reports the warning %q", k.Warning())
	}
}

// TestOpenRecordedSecretServiceWithoutService reports the missing service.
func TestOpenRecordedSecretServiceWithoutService(t *testing.T) {
	dir := t.TempDir()
	writeRecord(t, dir, BackendSecretService)
	ring := newFakeKeyring()
	ring.service = false
	if _, err := Open(dir, testService, Options{connect: fakeConnect(ring)}); !errors.Is(err, ErrKeyringUnavailable) {
		t.Fatalf("Open = %v, want ErrKeyringUnavailable", err)
	}
}

// TestOpenRecordedSecretServiceWithoutBus reports a session without a bus.
func TestOpenRecordedSecretServiceWithoutBus(t *testing.T) {
	dir := t.TempDir()
	writeRecord(t, dir, BackendSecretService)
	if _, err := Open(dir, testService, Options{connect: noBus}); !errors.Is(err, ErrKeyringUnavailable) {
		t.Fatalf("Open = %v, want ErrKeyringUnavailable", err)
	}
}

// TestOpenForcedBackend uses the forced backend on a new directory.
func TestOpenForcedBackend(t *testing.T) {
	ring := newFakeKeyring()
	k, err := Open(t.TempDir(), testService, Options{Backend: BackendSecretService, connect: fakeConnect(ring)})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got := k.Backend(); got != BackendSecretService {
		t.Fatalf("Backend() = %q, want %q", got, BackendSecretService)
	}

	dir := t.TempDir()
	k, err = Open(dir, testService, Options{Backend: BackendFile, connect: fakeConnect(ring)})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got := k.Backend(); got != BackendFile {
		t.Fatalf("Backend() = %q, want %q", got, BackendFile)
	}
}

// TestOpenRefusesBackendChange refuses a forced backend on a directory that
// recorded another one.
func TestOpenRefusesBackendChange(t *testing.T) {
	dir := t.TempDir()
	writeRecord(t, dir, BackendFile)
	_, err := Open(dir, testService, Options{Backend: BackendSecretService})
	if !errors.Is(err, ErrBackendMismatch) {
		t.Fatalf("Open = %v, want ErrBackendMismatch", err)
	}
}

// TestOpenRejectsUnknownValues rejects unknown backends in the record and in
// the options.
func TestOpenRejectsUnknownValues(t *testing.T) {
	dir := t.TempDir()
	writeRecord(t, dir, "vault")
	if _, err := Open(dir, testService, Options{}); err == nil {
		t.Fatal("Open accepted an unknown recorded backend")
	}
	if _, err := Open(t.TempDir(), testService, Options{Backend: "vault"}); err == nil {
		t.Fatal("Open accepted an unknown forced backend")
	}
}

// TestOpenRejectsEmptyArguments rejects a missing data directory or service.
func TestOpenRejectsEmptyArguments(t *testing.T) {
	if _, err := Open("", testService, Options{}); err == nil {
		t.Fatal("Open accepted an empty data directory")
	}
	if _, err := Open(t.TempDir(), "", Options{}); err == nil {
		t.Fatal("Open accepted an empty service name")
	}
}

// TestOpenWithoutBusOnOtherPlatforms reports the missing platform on every
// system but Linux.
func TestOpenWithoutBusOnOtherPlatforms(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("Linux opens a session bus")
	}
	k, err := Open(t.TempDir(), testService, Options{})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got := k.Backend(); got != BackendFile {
		t.Fatalf("Backend() = %q, want %q", got, BackendFile)
	}
	dir := t.TempDir()
	writeRecord(t, dir, BackendSecretService)
	if _, err := Open(dir, testService, Options{}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Open of a Secret Service directory = %v, want ErrUnsupported", err)
	}
}

// TestKeystoreStoresThroughBackend checks the full store, load and delete of
// an opened keystore.
func TestKeystoreStoresThroughBackend(t *testing.T) {
	ring := newFakeKeyring()
	k, err := Open(t.TempDir(), testService, Options{connect: fakeConnect(ring)})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ctx := context.Background()
	if err := k.Store(ctx, "store.dek", []byte("key")); err != nil {
		t.Fatalf("Store: %v", err)
	}
	secret, err := k.Load(ctx, "store.dek")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(secret) != "key" {
		t.Fatalf("loaded secret is %q, want %q", secret, "key")
	}
	if err := k.Delete(ctx, "store.dek"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := k.Load(ctx, "store.dek"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("Load after Delete = %v, want ErrNotFound", err)
	}
}

// writeRecord writes the backend record of a data directory.
func writeRecord(t *testing.T, dir string, backend Backend) {
	t.Helper()
	keys := filepath.Join(dir, keysDirName)
	if err := ensureDir(keys); err != nil {
		t.Fatalf("ensureDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(keys, backendFileName), []byte(backend), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

// TestServiceNames checks that the service of a data directory comes from the
// shared naming rule.
func TestServiceNames(t *testing.T) {
	if got, want := Service("/home/u/.local/share/unison", "/home/u/.local/share/unison"), "app.unison"; got != want {
		t.Fatalf("Service = %q, want %q", got, want)
	}
	first := Service("/home/u/.local/share/unison-2", "/home/u/.local/share/unison")
	second := Service("/home/u/.local/share/unison-3", "/home/u/.local/share/unison")
	if first == second {
		t.Fatalf("both directories map to %q", first)
	}
}
