// Package keystore keeps secrets in files protected with Windows DPAPI.
//
// Every secret is a separate file in the keys directory. Protected blobs belong
// to the Windows user account that wrote them: other accounts and other
// machines cannot read them.
package keystore

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lebe-dev/unison/core/ports"
)

// ErrUnsupported reports that DPAPI is unavailable because the binary was built
// for a platform other than Windows.
var ErrUnsupported = errors.New("keystore: DPAPI is available on Windows only")

// errInvalidName reports a secret name that does not map to a file inside the
// keys directory.
var errInvalidName = errors.New("keystore: invalid secret name")

// fileExt is appended to the secret name to get the file name.
const fileExt = ".bin"

// maxElemLen is the file name limit of Win32. Names are counted in bytes, which
// keeps non-ASCII names under the limit.
const maxElemLen = 255

// Keystore is a ports.Keystore that keeps DPAPI protected blobs in files.
type Keystore struct {
	dir     string
	entropy []byte
}

var _ ports.Keystore = (*Keystore)(nil)

// New returns a Keystore that keeps its files in dir. Entropy is optional
// application data mixed into the protection: blobs written with one entropy
// value cannot be read with another. Entropy may be nil and is copied.
func New(dir string, entropy []byte) (*Keystore, error) {
	if dir == "" {
		return nil, errors.New("keystore: empty directory")
	}
	return &Keystore{dir: filepath.Clean(dir), entropy: bytes.Clone(entropy)}, nil
}

// Dir returns the directory that holds the protected blobs.
func (k *Keystore) Dir() string { return k.dir }

// DefaultDir returns the per user directory Unison keeps its protected blobs in.
func DefaultDir() (string, error) {
	root := os.Getenv("LOCALAPPDATA")
	if root == "" {
		return "", errors.New("keystore: LOCALAPPDATA is not set")
	}
	return filepath.Join(root, "Unison", "keys"), nil
}

// path returns the file of the secret. Slashes in the name map to
// subdirectories of the keys directory.
func (k *Keystore) path(name string) (string, error) {
	if err := validateName(name); err != nil {
		return "", err
	}
	return filepath.Join(k.dir, filepath.FromSlash(name)+fileExt), nil
}

// validateName accepts names made of plain path elements: it rejects anything
// that could resolve outside the keys directory or that Windows refuses in a
// file name.
func validateName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: empty", errInvalidName)
	}
	for _, elem := range strings.Split(name, "/") {
		if err := validateElem(elem); err != nil {
			return fmt.Errorf("%w: %q", errInvalidName, name)
		}
	}
	return nil
}

func validateElem(elem string) error {
	if elem == "" || elem == "." || elem == ".." {
		return errInvalidName
	}
	if len(elem)+len(fileExt) > maxElemLen {
		return errInvalidName
	}
	// Windows silently drops trailing dots and spaces.
	if strings.HasSuffix(elem, ".") || strings.HasSuffix(elem, " ") {
		return errInvalidName
	}
	for _, r := range elem {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(`<>:"|?*\`, r) {
			return errInvalidName
		}
	}
	if isDeviceName(elem) {
		return errInvalidName
	}
	return nil
}

// isDeviceName reports whether the file name would address a DOS device.
func isDeviceName(elem string) bool {
	base := elem
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}
	base = strings.ToLower(base)
	switch base {
	case "con", "prn", "aux", "nul", "clock$":
		return true
	}
	if len(base) == 4 && base[3] >= '1' && base[3] <= '9' {
		prefix := base[:3]
		return prefix == "com" || prefix == "lpt"
	}
	return false
}

// writeFile replaces path with data atomically. The data goes to a temporary
// file in the same directory, which is synced and then renamed over path, so a
// reader sees either the old or the new content.
func writeFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
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
