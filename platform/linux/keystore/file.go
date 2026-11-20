package keystore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lebe-dev/unison/core/ports"
)

var (
	// errInvalidName reports a secret name that does not map to a file inside
	// the keys directory.
	errInvalidName = errors.New("keystore: invalid secret name")
	// errNotRegular reports a locked file that is not a plain file.
	errNotRegular = errors.New("keystore: the secret file is not a regular file")
	// errWidePermissions reports a secret file other users can read.
	errWidePermissions = errors.New("keystore: the secret file is readable by other users")
	// errForeignOwner reports a secret file of another user.
	errForeignOwner = errors.New("keystore: the secret file belongs to another user")
)

// fileStore keeps secrets as plain files below the keys directory. Nothing is
// encrypted: a key stored next to the ciphertext would protect neither, so the
// files rely on the permissions of the directory.
type fileStore struct {
	dir string
}

// path returns the file of a secret. Slashes in the name map to subdirectories
// of the keys directory.
func (s *fileStore) path(name string) (string, error) {
	if err := validateName(name); err != nil {
		return "", err
	}
	return filepath.Join(s.dir, filepath.FromSlash(name)), nil
}

// validateName accepts names made of plain path elements and keeps them away
// from the file that records the backend.
func validateName(name string) error {
	if name == "" || strings.ContainsRune(name, 0) {
		return errInvalidName
	}
	elements := strings.Split(name, "/")
	if elements[0] == backendFileName {
		return fmt.Errorf("%w: %q", errInvalidName, name)
	}
	for _, elem := range elements {
		if elem == "" || elem == "." || elem == ".." {
			return fmt.Errorf("%w: %q", errInvalidName, name)
		}
	}
	return nil
}

// load returns the secret stored under name.
func (s *fileStore) load(ctx context.Context, name string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := s.path(name)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("keystore: load %q: %w", name, ports.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("keystore: load %q: %w", name, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("keystore: load %q: %w", name, errNotRegular)
	}
	if err := checkFile(info); err != nil {
		return nil, fmt.Errorf("keystore: load %q: %w", name, err)
	}
	secret, err := os.ReadFile(path) //nolint:gosec // The path is built from a validated secret name.
	if err != nil {
		return nil, fmt.Errorf("keystore: load %q: %w", name, err)
	}
	return secret, nil
}

// store replaces the secret stored under name.
func (s *fileStore) store(ctx context.Context, name string, secret []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := s.path(name)
	if err != nil {
		return err
	}
	if err := writeFile(filepath.Dir(path), path, secret); err != nil {
		return fmt.Errorf("keystore: store %q: %w", name, err)
	}
	return nil
}

// remove deletes the secret stored under name.
func (s *fileStore) remove(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := s.path(name)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("keystore: delete %q: %w", name, err)
	}
	return nil
}

// writeFile replaces path with data atomically. The data goes to a temporary
// file in the same directory, which is synced and then renamed over path, so a
// reader sees either the old or the new content.
func writeFile(dir, path string, data []byte) error {
	if err := ensureDir(dir); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// ensureDir creates dir readable by its owner only.
func ensureDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	//nolint:gosec // A directory needs the execute bit, the owner keeps it to itself.
	return os.Chmod(dir, 0o700)
}
