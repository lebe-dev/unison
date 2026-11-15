package daemon

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/lebe-dev/unison/core/blob"
	"github.com/lebe-dev/unison/core/clip"
	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/core/store"
	"github.com/lebe-dev/unison/core/trust"
)

// settingDeviceName is the settings key of the device name chosen on this
// device.
const settingDeviceName = "device.name"

// maxNameLength bounds the device name in bytes, the local API uses the same
// limit.
const maxNameLength = 128

// defaultDeviceName returns the name of the host, which is the name a device
// starts with.
func defaultDeviceName() string {
	name, err := os.Hostname()
	if err != nil {
		return "Unison"
	}
	name = strings.TrimSpace(strings.TrimSuffix(name, ".local"))
	if name == "" {
		return "Unison"
	}
	return name
}

// bootstrap returns the device name and the discovery key of a data directory.
// Both are needed before the node starts: the name goes into the handshake and
// the discovery key tags the announcements of the device. The store is opened
// for the lookup and closed again, because the node opens it on its own.
func bootstrap(ctx context.Context, dataDir string, keystore ports.Keystore, log *slog.Logger) (string, []byte) {
	migrations, err := bootstrapMigrations()
	if err != nil {
		log.Warn("reading the settings failed", "error", err)
		return "", nil
	}
	db, err := store.Open(ctx, store.Options{Dir: dataDir, Keystore: keystore, Migrations: migrations})
	if err != nil {
		log.Warn("reading the settings failed", "error", err)
		return "", nil
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Warn("closing the settings store failed", "error", err)
		}
	}()

	name, err := db.Settings().GetString(ctx, settingDeviceName, "")
	if err != nil {
		log.Warn("reading the device name from the settings failed", "error", err)
		name = ""
	}

	key, err := trust.Open(db)
	if err != nil {
		log.Warn("reading the discovery key failed", "error", err)
		return strings.TrimSpace(name), nil
	}
	discoveryKey, err := key.DiscoveryKey(ctx)
	switch {
	case errors.Is(err, trust.ErrSecretNotFound):
		// The device has no Personal group yet, so it is announced without a
		// tag until it pairs.
	case err != nil:
		log.Warn("reading the discovery key failed", "error", err)
	}
	return strings.TrimSpace(name), discoveryKey
}

// bootstrapMigrations returns the migrations of the domains the node owns. A
// store that is opened before the node needs them, otherwise a database
// written by the node looks newer than the known schema.
func bootstrapMigrations() ([]store.Migration, error) {
	var migrations []store.Migration
	for _, source := range []func() ([]store.Migration, error){trust.Migrations, clip.Migrations, blob.Migrations} {
		found, err := source()
		if err != nil {
			return nil, err
		}
		migrations = append(migrations, found...)
	}
	return migrations, nil
}

// keyProvider resolves the discovery key of the device. The key lives in the
// trust store, which the node opens, and it changes when the device pairs.
type keyProvider struct {
	mu sync.RWMutex
	fn func(ctx context.Context) ([]byte, error)
}

// get returns the discovery key, or nil while the device has none.
func (p *keyProvider) get(ctx context.Context) ([]byte, error) {
	p.mu.RLock()
	fn := p.fn
	p.mu.RUnlock()
	if fn == nil {
		return nil, nil
	}
	return fn(ctx)
}

// use replaces the source of the discovery key.
func (p *keyProvider) use(fn func(ctx context.Context) ([]byte, error)) {
	p.mu.Lock()
	p.fn = fn
	p.mu.Unlock()
}
