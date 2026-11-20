// Package keystore keeps Unison secrets in the Secret Service of the desktop
// session, and in plain files with 0600 permissions when the session has none.
//
// The backend is chosen once and recorded in the keys directory, so a session
// that loses its Secret Service does not silently move the keys to files.
package keystore

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/lebe-dev/unison/core/ports"
)

// DefaultService is the service name Unison secrets are stored under.
const DefaultService = "app.unison"

// DefaultPromptTimeout bounds the wait for a system prompt such as unlocking
// the keyring.
const DefaultPromptTimeout = 60 * time.Second

// Names inside the data directory.
const (
	keysDirName     = "keys"
	backendFileName = "backend"
)

// Backend names the place secrets are kept in.
type Backend string

// Backends of the keystore.
const (
	// BackendSecretService keeps secrets in the Secret Service of the session.
	BackendSecretService Backend = "secret-service"
	// BackendFile keeps secrets in files below the keys directory.
	BackendFile Backend = "file"
)

var (
	// ErrUnsupported reports that the Secret Service is unavailable because the
	// binary was built for a platform other than Linux.
	ErrUnsupported = errors.New("keystore: the Secret Service is available on Linux only")
	// ErrKeyringUnavailable reports that the keys were created with the Secret
	// Service and the session no longer has one.
	ErrKeyringUnavailable = errors.New("keystore: the Secret Service of the session is not available")
	// ErrKeyringLocked reports that the keyring is locked and the user did not
	// unlock it.
	ErrKeyringLocked = errors.New("keystore: the keyring is locked")
	// ErrBackendMismatch reports that another backend is forced on a directory
	// that already recorded one.
	ErrBackendMismatch = errors.New("keystore: the data directory uses another backend")
)

// Options configures Open.
type Options struct {
	// Backend forces a backend instead of the recorded or the detected one.
	// A directory that already recorded another backend is refused.
	Backend Backend
	// PromptTimeout bounds the wait for a system prompt.
	// DefaultPromptTimeout is used when it is zero.
	PromptTimeout time.Duration
	// Logger receives the backend and the names and sizes of the secrets. A nil
	// logger discards the messages.
	Logger *slog.Logger

	// connect opens the session bus. Tests replace it.
	connect func() (busConn, error)
}

// withDefaults fills in the optional fields.
func (o Options) withDefaults() Options {
	if o.PromptTimeout <= 0 {
		o.PromptTimeout = DefaultPromptTimeout
	}
	if o.Logger == nil {
		o.Logger = slog.New(slog.DiscardHandler)
	}
	if o.connect == nil {
		o.connect = openSessionBus
	}
	return o
}

// Keystore is a ports.Keystore that keeps secrets in one backend of a data
// directory.
type Keystore struct {
	backend Backend
	service string
	logger  *slog.Logger
	dir     string
	files   *fileStore
	secrets *secretService

	warning string

	mu       sync.Mutex
	recorded bool
}

var _ ports.Keystore = (*Keystore)(nil)

// Open returns the keystore of dataDir for the secrets of service. The backend
// is the recorded one of the directory, or the forced one, or, on a directory
// without keys, the Secret Service when the session has it and files otherwise.
func Open(dataDir, service string, opts Options) (*Keystore, error) {
	if dataDir == "" {
		return nil, errors.New("keystore: empty data directory")
	}
	if service == "" {
		return nil, errors.New("keystore: empty service name")
	}
	opts = opts.withDefaults()
	dir := filepath.Join(dataDir, keysDirName)
	recorded, err := readBackendFile(dir)
	if err != nil {
		return nil, err
	}
	backend, err := chooseBackend(recorded, opts)
	if err != nil {
		return nil, err
	}

	k := &Keystore{
		backend:  backend,
		service:  service,
		logger:   opts.Logger,
		dir:      dir,
		recorded: recorded != "",
	}
	if backend == BackendFile {
		k.files = &fileStore{dir: dir}
		k.warning = fileWarning
		k.logger.Info("secrets are stored in files", "backend", string(backend), "dir", dir)
		return k, nil
	}
	secrets, err := openSecretService(opts)
	if err == nil {
		err = secrets.ready(context.Background())
	}
	if err == nil {
		k.backend = BackendSecretService
		k.secrets = secrets
		k.logger.Info("secrets are stored in the Secret Service", "backend", string(k.backend), "service", service)
		return k, nil
	}
	if k.recorded || opts.Backend != "" {
		return nil, err
	}
	// A first run without a usable keyring keeps the keys in files: the daemon
	// must start even when the Secret Service of the session has no collection.
	k.backend = BackendFile
	k.files = &fileStore{dir: dir}
	k.warning = fileWarning + ": " + err.Error()
	k.logger.Warn("the Secret Service is not usable, the secrets are stored in files", "error", err, "dir", dir)
	return k, nil
}

// fileWarning is the warning of the file backend.
const fileWarning = "the keys are stored in files with 0600 permissions"

// chooseBackend returns the backend of a directory with the recorded backend
// and the options of the call.
func chooseBackend(recorded string, opts Options) (Backend, error) {
	if opts.Backend != "" {
		if recorded != "" && Backend(recorded) != opts.Backend {
			return "", fmt.Errorf("keystore: the data directory keeps its secrets in %s, moving them to %s needs a migration: %w", recorded, opts.Backend, ErrBackendMismatch)
		}
		if opts.Backend != BackendFile && opts.Backend != BackendSecretService {
			return "", fmt.Errorf("keystore: unknown backend %q", opts.Backend)
		}
		return opts.Backend, nil
	}
	switch Backend(recorded) {
	case "":
		return BackendSecretService, nil
	case BackendFile, BackendSecretService:
		return Backend(recorded), nil
	default:
		return "", fmt.Errorf("keystore: unknown backend %q in %s", recorded, backendFileName)
	}
}

// openSecretService connects to the Secret Service and opens a session with it.
func openSecretService(opts Options) (*secretService, error) {
	conn, err := opts.connect()
	if err != nil {
		if errors.Is(err, ErrUnsupported) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %w", ErrKeyringUnavailable, err)
	}
	owned, err := serviceOnBus(conn)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrKeyringUnavailable, err)
	}
	if !owned {
		return nil, ErrKeyringUnavailable
	}
	return newSecretService(conn, opts.PromptTimeout, opts.Logger)
}

// Backend returns the backend the keystore uses.
func (k *Keystore) Backend() Backend { return k.backend }

// Warning returns what the user should know about the backend, or an empty
// string when there is nothing to say.
func (k *Keystore) Warning() string {
	if k.backend != BackendFile {
		return ""
	}
	if k.warning != "" {
		return k.warning
	}
	return fileWarning
}

// Load returns the secret stored under name, or ports.ErrNotFound.
func (k *Keystore) Load(ctx context.Context, name string) ([]byte, error) {
	if k.files != nil {
		return k.files.load(ctx, name)
	}
	secret, err := k.secrets.load(ctx, k.service, name)
	if err != nil {
		return nil, err
	}
	k.logger.Debug("secret loaded", "backend", string(k.backend), "name", name, "size", len(secret))
	return secret, nil
}

// Store replaces the secret stored under name.
func (k *Keystore) Store(ctx context.Context, name string, secret []byte) error {
	if err := k.recordBackend(); err != nil {
		return err
	}
	if k.files != nil {
		if err := k.files.store(ctx, name, secret); err != nil {
			return err
		}
	} else if err := k.secrets.store(ctx, k.service, name, secret); err != nil {
		return err
	}
	k.logger.Info("secret stored", "backend", string(k.backend), "name", name, "size", len(secret))
	return nil
}

// Delete removes the secret stored under name. Deleting a missing secret is
// not an error.
func (k *Keystore) Delete(ctx context.Context, name string) error {
	if k.files != nil {
		if err := k.files.remove(ctx, name); err != nil {
			return err
		}
	} else if err := k.secrets.remove(ctx, k.service, name); err != nil {
		return err
	}
	k.logger.Info("secret deleted", "backend", string(k.backend), "name", name)
	return nil
}

// recordBackend writes the backend of the directory before the first secret,
// and never changes it afterwards.
func (k *Keystore) recordBackend() error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.recorded {
		return nil
	}
	if err := writeFile(k.dir, filepath.Join(k.dir, backendFileName), []byte(k.backend)); err != nil {
		return fmt.Errorf("keystore: record the backend: %w", err)
	}
	k.recorded = true
	return nil
}

// readBackendFile returns the recorded backend of a keys directory, or an empty
// string when the directory has none.
func readBackendFile(dir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dir, backendFileName)) //nolint:gosec // The path is the keys directory of the data directory.
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("keystore: read the backend: %w", err)
	}
	value := strings.TrimSpace(string(data))
	switch Backend(value) {
	case BackendFile, BackendSecretService:
		return value, nil
	default:
		return "", fmt.Errorf("keystore: unknown backend %q in %s", value, backendFileName)
	}
}
