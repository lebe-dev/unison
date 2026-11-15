package identity

import (
	"context"
	"crypto/ed25519"
	"crypto/mldsa"
	"crypto/rand"
	"crypto/tls"
	"errors"
	"fmt"
	"time"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports"
)

// Keystore entries holding the identity seeds.
const (
	ed25519SeedEntry = "identity/ed25519-seed"
	mldsaSeedEntry   = "identity/mldsa65-seed"
)

// SeedSize is the length of an identity seed in bytes.
const SeedSize = 32

// certificateValidity is how long a generated certificate stays valid.
const certificateValidity = 10 * 365 * 24 * time.Hour

// Identity is the local device identity: a hybrid key pair, its public half and
// the self-signed TLS certificate. It is safe for concurrent use.
type Identity struct {
	edKey  ed25519.PrivateKey
	pqKey  *mldsa.PrivateKey
	public PublicIdentity
	cert   tls.Certificate
	certFP CertFingerprint
}

// LoadOrCreate loads the identity seeds from keystore, generating and storing a
// new pair when the keystore holds none. A partially stored pair is replaced.
func LoadOrCreate(ctx context.Context, keystore ports.Keystore) (*Identity, error) {
	if keystore == nil {
		return nil, errors.New("identity: nil keystore")
	}

	edSeed, err := loadSeed(ctx, keystore, ed25519SeedEntry)
	if err != nil {
		return nil, err
	}
	mldsaSeed, err := loadSeed(ctx, keystore, mldsaSeedEntry)
	if err != nil {
		return nil, err
	}
	if edSeed == nil || mldsaSeed == nil {
		return create(ctx, keystore)
	}
	return FromSeeds(edSeed, mldsaSeed)
}

// loadSeed returns the stored seed, or nil when it is absent.
func loadSeed(ctx context.Context, keystore ports.Keystore, name string) ([]byte, error) {
	seed, err := keystore.Load(ctx, name)
	switch {
	case err == nil:
		return seed, nil
	case errors.Is(err, ports.ErrNotFound):
		return nil, nil
	default:
		return nil, fmt.Errorf("identity: load %s: %w", name, err)
	}
}

// create generates a new key pair and stores both seeds.
func create(ctx context.Context, keystore ports.Keystore) (*Identity, error) {
	edSeed := make([]byte, ed25519.SeedSize)
	if _, err := rand.Read(edSeed); err != nil {
		return nil, fmt.Errorf("identity: generate ed25519 seed: %w", err)
	}
	mldsaSeed := make([]byte, mldsa.PrivateKeySize)
	if _, err := rand.Read(mldsaSeed); err != nil {
		return nil, fmt.Errorf("identity: generate mldsa seed: %w", err)
	}

	id, err := FromSeeds(edSeed, mldsaSeed)
	if err != nil {
		return nil, err
	}
	if err := keystore.Store(ctx, ed25519SeedEntry, edSeed); err != nil {
		return nil, fmt.Errorf("identity: store %s: %w", ed25519SeedEntry, err)
	}
	if err := keystore.Store(ctx, mldsaSeedEntry, mldsaSeed); err != nil {
		return nil, fmt.Errorf("identity: store %s: %w", mldsaSeedEntry, err)
	}
	return id, nil
}

// FromSeeds builds an identity from an Ed25519 seed and an ML-DSA-65 seed.
func FromSeeds(edSeed, mldsaSeed []byte) (*Identity, error) {
	if len(edSeed) != ed25519.SeedSize {
		return nil, fmt.Errorf("identity: ed25519 seed: got %d bytes, want %d", len(edSeed), ed25519.SeedSize)
	}
	edKey := ed25519.NewKeyFromSeed(edSeed)
	pqKey, err := mldsa.NewPrivateKey(mldsa.MLDSA65(), mldsaSeed)
	if err != nil {
		return nil, fmt.Errorf("identity: mldsa seed: %w", err)
	}

	public, err := NewPublicIdentity(edKey.Public().(ed25519.PublicKey), pqKey.PublicKey().Bytes())
	if err != nil {
		return nil, err
	}
	cert, err := newCertificate(edKey, public.DeviceID, time.Now())
	if err != nil {
		return nil, err
	}

	return &Identity{
		edKey:  edKey,
		pqKey:  pqKey,
		public: public,
		cert:   cert,
		certFP: CertFingerprintOfDER(cert.Certificate[0]),
	}, nil
}

// DeviceID returns the device ID of the identity.
func (i *Identity) DeviceID() model.DeviceID {
	return i.public.DeviceID
}

// Fingerprint returns the human readable fingerprint of the device ID.
func (i *Identity) Fingerprint() model.Fingerprint {
	return i.public.Fingerprint()
}

// Public returns the public half of the identity.
func (i *Identity) Public() PublicIdentity {
	return i.public
}

// Certificate returns the self-signed certificate for mutual TLS.
func (i *Identity) Certificate() tls.Certificate {
	return i.cert
}

// CertFingerprint returns the fingerprint of the certificate, used as the value
// pinned over an out of band channel.
func (i *Identity) CertFingerprint() CertFingerprint {
	return i.certFP
}
