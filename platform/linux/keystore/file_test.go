package keystore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lebe-dev/unison/core/ports"
)

// newTestFileStore returns a file backend in a temporary directory.
func newTestFileStore(t *testing.T) (*fileStore, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), keysDirName)
	return &fileStore{dir: dir}, dir
}

// TestFileStoreRoundTrip stores, reads and deletes a secret.
func TestFileStoreRoundTrip(t *testing.T) {
	s, _ := newTestFileStore(t)
	ctx := context.Background()

	if err := s.store(ctx, "identity/ed25519-seed", []byte("seed material")); err != nil {
		t.Fatalf("store: %v", err)
	}
	secret, err := s.load(ctx, "identity/ed25519-seed")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if string(secret) != "seed material" {
		t.Fatalf("loaded secret is %q, want %q", secret, "seed material")
	}
	if err := s.store(ctx, "identity/ed25519-seed", []byte("new seed")); err != nil {
		t.Fatalf("store over an existing secret: %v", err)
	}
	secret, err = s.load(ctx, "identity/ed25519-seed")
	if err != nil {
		t.Fatalf("load after overwrite: %v", err)
	}
	if string(secret) != "new seed" {
		t.Fatalf("loaded secret is %q, want %q", secret, "new seed")
	}
	if err := s.remove(ctx, "identity/ed25519-seed"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.load(ctx, "identity/ed25519-seed"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("load after delete = %v, want ErrNotFound", err)
	}
	if err := s.remove(ctx, "identity/ed25519-seed"); err != nil {
		t.Fatalf("delete of a missing secret: %v", err)
	}
}

// TestFileStorePermissions checks the modes of the directory and the files.
func TestFileStorePermissions(t *testing.T) {
	s, dir := newTestFileStore(t)
	if err := s.store(context.Background(), "identity/ed25519-seed", []byte("seed")); err != nil {
		t.Fatalf("store: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, "identity"))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("secret directory mode is %o, want 700", got)
	}
	info, err = os.Stat(filepath.Join(dir, "identity", "ed25519-seed"))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("secret file mode is %o, want 600", got)
	}
}

// TestFileStoreLeavesNoTemporaryFile checks that a store leaves one file only.
func TestFileStoreLeavesNoTemporaryFile(t *testing.T) {
	s, dir := newTestFileStore(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if err := s.store(ctx, "store.dek", []byte("key")); err != nil {
			t.Fatalf("store: %v", err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "store.dek" {
		t.Fatalf("directory holds %v, want only store.dek", names(entries))
	}
}

// TestFileStoreRefusesWidePermissions rejects a secret other users can read.
func TestFileStoreRefusesWidePermissions(t *testing.T) {
	s, dir := newTestFileStore(t)
	ctx := context.Background()
	if err := s.store(ctx, "store.dek", []byte("key")); err != nil {
		t.Fatalf("store: %v", err)
	}
	if err := os.Chmod(filepath.Join(dir, "store.dek"), 0o644); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	if _, err := s.load(ctx, "store.dek"); !errors.Is(err, errWidePermissions) {
		t.Fatalf("load = %v, want errWidePermissions", err)
	}
}

// TestFileStoreRefusesOtherFiles rejects a secret file that is not a regular
// file, such as a symbolic link.
func TestFileStoreRefusesOtherFiles(t *testing.T) {
	s, dir := newTestFileStore(t)
	ctx := context.Background()
	if err := ensureDir(dir); err != nil {
		t.Fatalf("ensureDir: %v", err)
	}
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("key"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "store.dek")); err != nil {
		t.Fatalf("Symlink: %v", err)
	}
	if _, err := s.load(ctx, "store.dek"); !errors.Is(err, errNotRegular) {
		t.Fatalf("load = %v, want errNotRegular", err)
	}
}

// TestFileStoreRejectsBadNames keeps secret names inside the keys directory.
func TestFileStoreRejectsBadNames(t *testing.T) {
	s, _ := newTestFileStore(t)
	for _, name := range []string{"", "/abs", "a/", "a//b", ".", "..", "../escape", "a/../b", "backend", "backend/x", "nul\x00byte"} {
		if _, err := s.path(name); !errors.Is(err, errInvalidName) {
			t.Errorf("path(%q) = %v, want errInvalidName", name, err)
		}
	}
	for _, name := range []string{"store.dek", "identity/ed25519-seed", "rendezvous/00ff", "name with space"} {
		if _, err := s.path(name); err != nil {
			t.Errorf("path(%q) = %v, want no error", name, err)
		}
	}
}

// TestFileStoreReportsBadDirectory reports a keys directory below a file.
func TestFileStoreReportsBadDirectory(t *testing.T) {
	root := t.TempDir()
	blocker := filepath.Join(root, keysDirName)
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	s := &fileStore{dir: blocker}
	if err := s.store(context.Background(), "store.dek", []byte("key")); err == nil {
		t.Fatal("store below a regular file returned no error")
	}
}

// TestFileStoreCanceledContext stops before touching the files.
func TestFileStoreCanceledContext(t *testing.T) {
	s, dir := newTestFileStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.store(ctx, "store.dek", []byte("key")); !errors.Is(err, context.Canceled) {
		t.Fatalf("store = %v, want context.Canceled", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("stored %v, want no files", names(entries))
	}
}

// names returns the names of the directory entries.
func names(entries []os.DirEntry) []string {
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.Name())
	}
	return out
}

// TestEnsureDirTightensMode tightens a directory other users can read.
func TestEnsureDirTightensMode(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "keys")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if err := ensureDir(dir); err != nil {
		t.Fatalf("ensureDir: %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("directory mode is %o, want 700", got)
	}
}

// TestValidateName error text names the secret.
func TestValidateNameErrorText(t *testing.T) {
	err := validateName("backend")
	if err == nil {
		t.Fatal("validateName accepted the backend file name")
	}
	if !strings.Contains(err.Error(), "backend") {
		t.Fatalf("error %q does not name the secret", err)
	}
}
