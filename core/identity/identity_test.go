package identity

import (
	"context"
	"crypto/ed25519"
	"errors"
	"slices"
	"testing"

	"lukechampine.com/blake3"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/core/ports/fake"
)

// testSeeds returns a fixed pair of identity seeds.
func testSeeds() (edSeed, mldsaSeed []byte) {
	edSeed = make([]byte, SeedSize)
	mldsaSeed = make([]byte, SeedSize)
	for i := range edSeed {
		edSeed[i] = byte(i + 1)
		mldsaSeed[i] = byte(0x80 + i)
	}
	return edSeed, mldsaSeed
}

// newTestIdentity builds an identity from the fixed test seeds.
func newTestIdentity(t *testing.T) *Identity {
	t.Helper()
	id, err := FromSeeds(testSeeds())
	if err != nil {
		t.Fatalf("from seeds: %v", err)
	}
	return id
}

func TestLoadOrCreateStoresAndReloads(t *testing.T) {
	ctx := context.Background()
	keystore := fake.NewKeystore()

	first, err := LoadOrCreate(ctx, keystore)
	if err != nil {
		t.Fatalf("load or create: %v", err)
	}
	for _, name := range []string{ed25519SeedEntry, mldsaSeedEntry} {
		seed, err := keystore.Load(ctx, name)
		if err != nil {
			t.Fatalf("load %s: %v", name, err)
		}
		if len(seed) != SeedSize {
			t.Fatalf("load %s: got %d bytes, want %d", name, len(seed), SeedSize)
		}
	}

	second, err := LoadOrCreate(ctx, keystore)
	if err != nil {
		t.Fatalf("load or create again: %v", err)
	}
	if first.DeviceID() != second.DeviceID() {
		t.Fatalf("device id changed: %s, then %s", first.DeviceID(), second.DeviceID())
	}
	if !first.Public().Ed25519.Equal(second.Public().Ed25519) {
		t.Fatal("ed25519 public key changed")
	}
	if !slices.Equal(first.Public().MLDSABytes(), second.Public().MLDSABytes()) {
		t.Fatal("mldsa public key changed")
	}
	if first.Fingerprint() != second.Fingerprint() {
		t.Fatalf("fingerprint changed: %s, then %s", first.Fingerprint(), second.Fingerprint())
	}
}

func TestLoadOrCreateReplacesPartialPair(t *testing.T) {
	ctx := context.Background()
	keystore := fake.NewKeystore()

	edSeed, _ := testSeeds()
	if err := keystore.Store(ctx, ed25519SeedEntry, edSeed); err != nil {
		t.Fatalf("store ed25519 seed: %v", err)
	}

	id, err := LoadOrCreate(ctx, keystore)
	if err != nil {
		t.Fatalf("load or create: %v", err)
	}
	if slices.Equal(id.Public().Ed25519, ed25519.NewKeyFromSeed(edSeed).Public().(ed25519.PublicKey)) {
		t.Fatal("kept the ed25519 seed of an incomplete pair")
	}
	for _, name := range []string{ed25519SeedEntry, mldsaSeedEntry} {
		if _, err := keystore.Load(ctx, name); err != nil {
			t.Fatalf("load %s: %v", name, err)
		}
	}

	reloaded, err := LoadOrCreate(ctx, keystore)
	if err != nil {
		t.Fatalf("load or create again: %v", err)
	}
	if reloaded.DeviceID() != id.DeviceID() {
		t.Fatalf("device id changed: %s, then %s", id.DeviceID(), reloaded.DeviceID())
	}
}

func TestLoadOrCreateRejectsCorruptSeed(t *testing.T) {
	ctx := context.Background()
	keystore := fake.NewKeystore()

	if err := keystore.Store(ctx, ed25519SeedEntry, []byte{1, 2, 3}); err != nil {
		t.Fatalf("store ed25519 seed: %v", err)
	}
	_, mldsaSeed := testSeeds()
	if err := keystore.Store(ctx, mldsaSeedEntry, mldsaSeed); err != nil {
		t.Fatalf("store mldsa seed: %v", err)
	}

	if _, err := LoadOrCreate(ctx, keystore); err == nil {
		t.Fatal("load or create: got nil error, want a seed length error")
	}
	stored, err := keystore.Load(ctx, ed25519SeedEntry)
	if err != nil {
		t.Fatalf("load ed25519 seed: %v", err)
	}
	if want := []byte{1, 2, 3}; !slices.Equal(stored, want) {
		t.Fatalf("corrupt seed was overwritten: got %v, want %v", stored, want)
	}
}

func TestLoadOrCreateNilKeystore(t *testing.T) {
	if _, err := LoadOrCreate(context.Background(), nil); err == nil {
		t.Fatal("load or create: got nil error, want an error")
	}
}

func TestLoadOrCreateKeystoreError(t *testing.T) {
	ctx := context.Background()

	loadErr := errors.New("keystore unavailable")
	if _, err := LoadOrCreate(ctx, &errorKeystore{loadErr: loadErr}); !errors.Is(err, loadErr) {
		t.Fatalf("load error: got %v, want %v", err, loadErr)
	}

	storeErr := errors.New("keystore read only")
	if _, err := LoadOrCreate(ctx, &errorKeystore{storeErr: storeErr}); !errors.Is(err, storeErr) {
		t.Fatalf("store error: got %v, want %v", err, storeErr)
	}
}

func TestFromSeedsRejectsBadLengths(t *testing.T) {
	edSeed, mldsaSeed := testSeeds()

	if _, err := FromSeeds(edSeed[:16], mldsaSeed); err == nil {
		t.Fatal("short ed25519 seed: got nil error, want an error")
	}
	if _, err := FromSeeds(edSeed, mldsaSeed[:16]); err == nil {
		t.Fatal("short mldsa seed: got nil error, want an error")
	}
}

func TestDeviceIDIsDerivedFromPublicKeys(t *testing.T) {
	id, err := FromSeeds(testSeeds())
	if err != nil {
		t.Fatalf("from seeds: %v", err)
	}

	edPub := id.Public().Ed25519
	mldsaPub := id.Public().MLDSABytes()
	if got, want := id.DeviceID(), DeviceIDFromKeys(edPub, mldsaPub); got != want {
		t.Fatalf("device id: got %s, want %s", got, want)
	}

	var keyed []byte
	keyed = append(keyed, edPub...)
	keyed = append(keyed, mldsaPub...)
	if got, want := id.DeviceID(), model.DeviceID(blake3.Sum256(keyed)); got != want {
		t.Fatalf("device id: got %s, want blake3 %s", got, want)
	}
}

func TestDeviceIDIsDeterministic(t *testing.T) {
	first := newTestIdentity(t)
	second := newTestIdentity(t)

	if first.DeviceID() != second.DeviceID() {
		t.Fatalf("device id: got %s and %s", first.DeviceID(), second.DeviceID())
	}
	if got, want := first.Fingerprint(), model.FingerprintOf(first.DeviceID()); got != want {
		t.Fatalf("fingerprint: got %s, want %s", got, want)
	}

	edSeed, mldsaSeed := testSeeds()
	mldsaSeed[0] ^= 0xff
	other, err := FromSeeds(edSeed, mldsaSeed)
	if err != nil {
		t.Fatalf("from seeds: %v", err)
	}
	if other.DeviceID() == first.DeviceID() {
		t.Fatalf("device id did not change with different seeds: %s", other.DeviceID())
	}
}

// errorKeystore fails loads and stores with the given errors. Missing entries
// are reported as ports.ErrNotFound when no load error is set.
type errorKeystore struct {
	loadErr  error
	storeErr error
}

func (s *errorKeystore) Load(context.Context, string) ([]byte, error) {
	if s.loadErr != nil {
		return nil, s.loadErr
	}
	return nil, ports.ErrNotFound
}

func (s *errorKeystore) Store(context.Context, string, []byte) error {
	return s.storeErr
}

func (s *errorKeystore) Delete(context.Context, string) error {
	return nil
}
