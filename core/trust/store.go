package trust

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/lebe-dev/unison/core/identity"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/store"
)

// migrationsBase is the version block owned by the trust domain.
const migrationsBase = 100

// MaxAddrs bounds the cached addresses of a peer.
const MaxAddrs = 8

// maxAddrLength bounds one cached address in bytes.
const maxAddrLength = 255

// Table and column names used by the store and by the sealing binding.
const (
	peersTable    = "peers"
	secretsTable  = "group_secrets"
	profileColumn = "profile"
	addrsColumn   = "addrs"
)

// peerColumns lists the columns of a peer row in the scan order.
const peerColumns = "device_id, ed25519_pub, mldsa_pub, role, revoked, added_at, last_seen_at, profile, addrs"

// Queries that read one peer by its unique keys.
const (
	peerByIDQuery  = "SELECT " + peerColumns + " FROM peers WHERE device_id = ?"
	peerByKeyQuery = "SELECT " + peerColumns + " FROM peers WHERE ed25519_pub = ?"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations returns the schema migrations owned by this package.
func Migrations() ([]store.Migration, error) {
	return store.MigrationsFromFS(migrationsFS, "migrations", migrationsBase)
}

// ListFilter selects the peers returned by List.
type ListFilter struct {
	// Role limits the result to one role. The zero value keeps every role.
	Role model.Role
	// IncludeRevoked adds revoked devices to the result.
	IncludeRevoked bool
}

// Store keeps the peer records and the group secrets in the database.
type Store struct {
	db *store.Store
}

// Open returns the trust store backed by db.
func Open(db *store.Store) (*Store, error) {
	if db == nil {
		return nil, errors.New("trust: nil store")
	}
	return &Store{db: db}, nil
}

// Add stores a new peer. A device that is on the deny-list or already known is
// rejected. A zero AddedAt becomes the current time and a zero LastSeenAt
// becomes AddedAt.
func (s *Store) Add(ctx context.Context, peer Peer) error {
	if peer.Revoked {
		return fmt.Errorf("%w: a revoked peer cannot be added", ErrInvalidPeer)
	}
	if err := peer.Validate(); err != nil {
		return err
	}
	if peer.AddedAt.IsZero() {
		peer.AddedAt = time.Now()
	}
	if peer.LastSeenAt.IsZero() {
		peer.LastSeenAt = peer.AddedAt
	}
	if peer.Role == model.RolePersonal && !peer.Confirm.Any() {
		defaults, err := s.SelfConfirmations(ctx)
		if err != nil {
			return fmt.Errorf("trust: read the default confirmation flags: %w", err)
		}
		peer.Confirm = defaults
	}

	return s.db.WithTx(ctx, func(tx *sql.Tx) error {
		if err := checkDenyList(ctx, tx, peer); err != nil {
			return err
		}
		return insertPeer(ctx, tx, s.db.Sealer(), peer)
	})
}

// insertPeer stores a validated peer that passed the deny-list check.
func insertPeer(ctx context.Context, tx *sql.Tx, sealer *store.Sealer, peer Peer) error {
	peer.LastAddrs = boundAddrs(peer.LastAddrs)

	sealedProfile, err := sealPeerProfile(sealer, peer)
	if err != nil {
		return err
	}
	sealedAddrs, err := sealPeerAddrs(sealer, peer)
	if err != nil {
		return err
	}

	const query = `INSERT INTO peers
		(device_id, ed25519_pub, mldsa_pub, role, revoked, added_at, last_seen_at, profile, addrs)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	args := []any{
		peer.Public.DeviceID.Bytes(),
		peer.Public.Ed25519,
		peer.Public.MLDSABytes(),
		int(peer.Role),
		peer.Revoked,
		unixMilli(peer.AddedAt),
		unixMilli(peer.LastSeenAt),
		sealedProfile,
		sealedAddrs,
	}
	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("trust: add peer: %w", err)
	}
	return nil
}

// Get returns the peer with the given device ID, including a revoked one.
func (s *Store) Get(ctx context.Context, id model.DeviceID) (Peer, error) {
	if id.IsZero() {
		return Peer{}, fmt.Errorf("%w: empty device id", ErrInvalidPeer)
	}
	var peer Peer
	err := s.db.View(ctx, func(tx *sql.Tx) error {
		var err error
		peer, err = readPeer(ctx, tx, s.db.Sealer(), peerByIDQuery, id.Bytes())
		return err
	})
	return peer, err
}

// ByEd25519 returns the peer that owns the Ed25519 public key, including a
// revoked one. It is the lookup used to match a peer certificate against the
// trust store.
func (s *Store) ByEd25519(ctx context.Context, pub []byte) (Peer, error) {
	if len(pub) != ed25519.PublicKeySize {
		return Peer{}, fmt.Errorf("%w: ed25519 public key: got %d bytes, want %d", ErrInvalidPeer, len(pub), ed25519.PublicKeySize)
	}
	var peer Peer
	err := s.db.View(ctx, func(tx *sql.Tx) error {
		var err error
		peer, err = readPeer(ctx, tx, s.db.Sealer(), peerByKeyQuery, pub)
		return err
	})
	return peer, err
}

// List returns the stored peers ordered by the time they were added. Revoked
// devices are left out unless the filter asks for them.
func (s *Store) List(ctx context.Context, filter ListFilter) ([]Peer, error) {
	//nolint:gosec // The query is built from fixed column names and placeholders.
	query := "SELECT " + peerColumns + " FROM peers"
	conditions := make([]string, 0, 2)
	args := make([]any, 0, 2)
	if filter.Role != model.RoleUnspecified {
		conditions = append(conditions, "role = ?")
		args = append(args, int(filter.Role))
	}
	if !filter.IncludeRevoked {
		conditions = append(conditions, "revoked = 0")
	}
	if len(conditions) > 0 {
		//nolint:gosec // The conditions are fixed clauses with placeholders.
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY added_at, device_id"

	peers := make([]Peer, 0, 4)
	err := s.db.View(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("trust: list peers: %w", err)
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			peer, err := scanPeer(rows, s.db.Sealer())
			if err != nil {
				return err
			}
			peers = append(peers, peer)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("trust: list peers: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return peers, nil
}

// Rename sets the display name of a peer that the user chose on this device.
// The name stays when the peer announces its own name in Hello.
func (s *Store) Rename(ctx context.Context, id model.DeviceID, name string) error {
	if err := validateName(name); err != nil {
		return err
	}
	return s.db.WithTx(ctx, func(tx *sql.Tx) error {
		peer, err := s.activePeer(ctx, tx, id)
		if err != nil {
			return err
		}
		peer.Name = name
		peer.NameLocal = true
		return s.writeProfile(ctx, tx, peer)
	})
}

// SetAnnouncedName records the name a peer announced. A name the user chose on
// this device is kept.
func (s *Store) SetAnnouncedName(ctx context.Context, id model.DeviceID, name string) error {
	if err := validateName(name); err != nil {
		return err
	}
	if name == "" {
		return nil
	}
	return s.db.WithTx(ctx, func(tx *sql.Tx) error {
		peer, err := s.activePeer(ctx, tx, id)
		if err != nil {
			return err
		}
		if peer.NameLocal || peer.Name == name {
			return nil
		}
		peer.Name = name
		return s.writeProfile(ctx, tx, peer)
	})
}

// SetRequireConfirmation sets a confirmation flag of this device for a
// Personal peer. It reports whether the flag changed. A flag cannot be enabled
// for a Friends peer.
func (s *Store) SetRequireConfirmation(ctx context.Context, id model.DeviceID, scope Scope, require bool) (bool, error) {
	if !scope.Valid() {
		return false, fmt.Errorf("%w: unknown confirmation scope %d", ErrInvalidPeer, scope)
	}
	changed := false
	err := s.db.WithTx(ctx, func(tx *sql.Tx) error {
		peer, err := s.activePeer(ctx, tx, id)
		if err != nil {
			return err
		}
		if require && peer.Role != model.RolePersonal {
			return fmt.Errorf("%w: confirmation applies to personal peers only", ErrInvalidPeer)
		}
		if peer.Confirm.Get(scope) == require {
			return nil
		}
		peer.Confirm = peer.Confirm.With(scope, require)
		changed = true
		return s.writeProfile(ctx, tx, peer)
	})
	return changed, err
}

// SetConfirmRemote records the confirmation flags the peer reported for this
// device. It reports whether they changed. The flags of this device for the
// peer stay unchanged: the peer owns these and this device only shows them.
func (s *Store) SetConfirmRemote(ctx context.Context, id model.DeviceID, flags Confirmations) (bool, error) {
	changed := false
	err := s.db.WithTx(ctx, func(tx *sql.Tx) error {
		peer, err := s.activePeer(ctx, tx, id)
		if err != nil {
			return err
		}
		if peer.ConfirmRemote == flags {
			return nil
		}
		peer.ConfirmRemote = flags
		changed = true
		return s.writeProfile(ctx, tx, peer)
	})
	return changed, err
}

// SetPlatform records the operating system a peer announced in its hello.
func (s *Store) SetPlatform(ctx context.Context, id model.DeviceID, platform model.Platform) error {
	if platform == model.PlatformUnknown {
		return fmt.Errorf("%w: unknown platform", ErrInvalidPeer)
	}
	return s.db.WithTx(ctx, func(tx *sql.Tx) error {
		peer, err := s.activePeer(ctx, tx, id)
		if err != nil {
			return err
		}
		if peer.Platform == platform {
			return nil
		}
		peer.Platform = platform
		return s.writeProfile(ctx, tx, peer)
	})
}

// MarkRevoked revokes a peer and drops its cached addresses. The record stays
// as a deny-list entry. Revoking twice is not an error.
func (s *Store) MarkRevoked(ctx context.Context, id model.DeviceID) error {
	if id.IsZero() {
		return fmt.Errorf("%w: empty device id", ErrInvalidPeer)
	}
	return s.db.WithTx(ctx, func(tx *sql.Tx) error {
		peer, err := readPeer(ctx, tx, s.db.Sealer(), peerByIDQuery, id.Bytes())
		if err != nil {
			return err
		}
		peer.Revoked = true
		peer.LastAddrs = nil
		if err := s.writeAddrs(ctx, tx, peer); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE peers SET revoked = 1 WHERE device_id = ?", id.Bytes()); err != nil {
			return fmt.Errorf("trust: revoke peer: %w", err)
		}
		return nil
	})
}

// TouchSeen records that the peer was reachable at at. A zero time means now.
func (s *Store) TouchSeen(ctx context.Context, id model.DeviceID, at time.Time) error {
	if at.IsZero() {
		at = time.Now()
	}
	return s.db.WithTx(ctx, func(tx *sql.Tx) error {
		if _, err := s.activePeer(ctx, tx, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE peers SET last_seen_at = ? WHERE device_id = ?", unixMilli(at), id.Bytes()); err != nil {
			return fmt.Errorf("trust: mark peer seen: %w", err)
		}
		return nil
	})
}

// UpdateAddrs replaces the cached addresses of a peer. Addresses are stored in
// the given order, most recent first, without duplicates and with at most
// MaxAddrs entries.
func (s *Store) UpdateAddrs(ctx context.Context, id model.DeviceID, addrs []string) error {
	return s.db.WithTx(ctx, func(tx *sql.Tx) error {
		peer, err := s.activePeer(ctx, tx, id)
		if err != nil {
			return err
		}
		peer.LastAddrs = boundAddrs(addrs)
		return s.writeAddrs(ctx, tx, peer)
	})
}

// activePeer loads a peer for update and rejects a revoked one.
func (s *Store) activePeer(ctx context.Context, tx *sql.Tx, id model.DeviceID) (Peer, error) {
	if id.IsZero() {
		return Peer{}, fmt.Errorf("%w: empty device id", ErrInvalidPeer)
	}
	peer, err := readPeer(ctx, tx, s.db.Sealer(), peerByIDQuery, id.Bytes())
	if err != nil {
		return Peer{}, err
	}
	if peer.Revoked {
		return Peer{}, ErrPeerRevoked
	}
	return peer, nil
}

// writeProfile stores the sealed profile of the peer.
func (s *Store) writeProfile(ctx context.Context, tx *sql.Tx, peer Peer) error {
	sealed, err := sealPeerProfile(s.db.Sealer(), peer)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE peers SET profile = ? WHERE device_id = ?", sealed, peer.Public.DeviceID.Bytes()); err != nil {
		return fmt.Errorf("trust: write peer profile: %w", err)
	}
	return nil
}

// writeAddrs stores the sealed address cache of the peer.
func (s *Store) writeAddrs(ctx context.Context, tx *sql.Tx, peer Peer) error {
	return writePeerAddrs(ctx, tx, s.db.Sealer(), peer)
}

// rowScanner is implemented by *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

// readPeer reads one peer with a query from this package.
func readPeer(ctx context.Context, tx *sql.Tx, sealer *store.Sealer, query string, arg any) (Peer, error) {
	row := tx.QueryRowContext(ctx, query, arg)
	peer, err := scanPeer(row, sealer)
	if errors.Is(err, sql.ErrNoRows) {
		return Peer{}, ErrPeerNotFound
	}
	if err != nil {
		return Peer{}, err
	}
	return peer, nil
}

// scanPeer reads a peer row and opens its sealed columns.
func scanPeer(scanner rowScanner, sealer *store.Sealer) (Peer, error) {
	var (
		deviceID, edPub, mldsaPub  []byte
		role                       int64
		revoked                    bool
		addedAt, lastSeenAt        int64
		sealedProfile, sealedAddrs []byte
	)
	if err := scanner.Scan(&deviceID, &edPub, &mldsaPub, &role, &revoked, &addedAt, &lastSeenAt, &sealedProfile, &sealedAddrs); err != nil {
		return Peer{}, err
	}

	id, err := model.DeviceIDFromBytes(deviceID)
	if err != nil {
		return Peer{}, fmt.Errorf("trust: read peer: %w", err)
	}
	public, err := identity.NewPublicIdentity(edPub, mldsaPub)
	if err != nil {
		return Peer{}, fmt.Errorf("trust: read peer: %w", err)
	}
	if public.DeviceID != id {
		return Peer{}, errors.New("trust: read peer: device id does not match the public keys")
	}
	profile, err := openPeerProfile(sealer, id, sealedProfile)
	if err != nil {
		return Peer{}, err
	}
	addrs, err := openPeerAddrs(sealer, id, sealedAddrs)
	if err != nil {
		return Peer{}, err
	}

	peer := Peer{
		Public:    public,
		Name:      profile.Name,
		NameLocal: profile.NameLocal,
		Platform:  model.Platform(profile.Platform),
		Role:      model.Role(role),
		Confirm: Confirmations{
			Clipboard: profile.RequireConfirmation,
			Files:     profile.RequireFileConfirmation,
		},
		ConfirmRemote: Confirmations{
			Clipboard: profile.RequireConfirmationRemote,
			Files:     profile.RequireFileConfirmationRemote,
		},
		RemoteSettingsDenied: profile.RemoteSettingsDenied,
		AddedAt:              timeFromMilli(addedAt),
		LastSeenAt:           timeFromMilli(lastSeenAt),
		LastAddrs:            addrs,
		Revoked:              revoked,
	}
	if profile.IntroducedBy != "" {
		peer.IntroducedBy, err = model.ParseDeviceID(profile.IntroducedBy)
		if err != nil {
			return Peer{}, fmt.Errorf("trust: read peer: %w", err)
		}
	}
	return peer, nil
}

// checkDenyList rejects a peer whose device ID or Ed25519 key is already stored.
func checkDenyList(ctx context.Context, tx *sql.Tx, peer Peer) error {
	const revokedQuery = "SELECT 1 FROM peers WHERE revoked = 1 AND (device_id = ? OR ed25519_pub = ?)"
	var found int
	err := tx.QueryRowContext(ctx, revokedQuery, peer.Public.DeviceID.Bytes(), peer.Public.Ed25519).Scan(&found)
	switch {
	case err == nil:
		return ErrPeerRevoked
	case !errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("trust: check deny-list: %w", err)
	}

	const existsQuery = "SELECT 1 FROM peers WHERE device_id = ? OR ed25519_pub = ?"
	err = tx.QueryRowContext(ctx, existsQuery, peer.Public.DeviceID.Bytes(), peer.Public.Ed25519).Scan(&found)
	switch {
	case err == nil:
		return ErrPeerExists
	case !errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("trust: check existing peer: %w", err)
	}
	return nil
}

// boundAddrs deduplicates addresses and keeps at most MaxAddrs of them.
func boundAddrs(addrs []string) []string {
	kept := make([]string, 0, min(len(addrs), MaxAddrs))
	seen := make(map[string]struct{}, len(addrs))
	for _, addr := range addrs {
		addr = strings.TrimSpace(addr)
		if addr == "" || len(addr) > maxAddrLength {
			continue
		}
		if _, ok := seen[addr]; ok {
			continue
		}
		seen[addr] = struct{}{}
		kept = append(kept, addr)
		if len(kept) == MaxAddrs {
			break
		}
	}
	return kept
}

// peerRowKey binds a sealed column to its peer and column name, so a value
// moved to another row or column does not open.
func peerRowKey(id model.DeviceID, column string) string {
	return id.String() + "/" + column
}

// profileVersion is the version of the sealed peer profile.
const profileVersion = 1

// peerProfile is the sealed part of a peer record.
type peerProfile struct {
	Version                   int    `json:"version"`
	Name                      string `json:"name"`
	NameLocal                 bool   `json:"name_local,omitempty"`
	Platform                  uint8  `json:"platform"`
	RequireConfirmation       bool   `json:"require_confirmation"`
	RequireConfirmationRemote bool   `json:"require_confirmation_remote,omitempty"`
	RequireFileConfirmation   bool   `json:"require_file_confirmation,omitempty"`
	// RequireFileConfirmationRemote is the files flag the peer reported.
	RequireFileConfirmationRemote bool `json:"require_file_confirmation_remote,omitempty"`
	// RemoteSettingsDenied reports a refused permission to change the
	// confirmation flags of this device remotely.
	RemoteSettingsDenied bool   `json:"remote_settings_denied,omitempty"`
	IntroducedBy         string `json:"introduced_by,omitempty"`
}

// sealPeerProfile encodes and seals the profile of the peer.
func sealPeerProfile(sealer *store.Sealer, peer Peer) ([]byte, error) {
	profile := peerProfile{
		Version:                       profileVersion,
		Name:                          peer.Name,
		NameLocal:                     peer.NameLocal,
		Platform:                      uint8(peer.Platform),
		RequireConfirmation:           peer.Confirm.Clipboard,
		RequireConfirmationRemote:     peer.ConfirmRemote.Clipboard,
		RequireFileConfirmation:       peer.Confirm.Files,
		RequireFileConfirmationRemote: peer.ConfirmRemote.Files,
		RemoteSettingsDenied:          peer.RemoteSettingsDenied,
	}
	if !peer.IntroducedBy.IsZero() {
		profile.IntroducedBy = peer.IntroducedBy.String()
	}
	data, err := json.Marshal(profile)
	if err != nil {
		return nil, fmt.Errorf("trust: encode peer profile: %w", err)
	}
	sealed, err := sealer.Seal(peersTable, peerRowKey(peer.Public.DeviceID, profileColumn), data)
	if err != nil {
		return nil, fmt.Errorf("trust: seal peer profile: %w", err)
	}
	return sealed, nil
}

// openPeerProfile opens and decodes the profile of the peer.
func openPeerProfile(sealer *store.Sealer, id model.DeviceID, sealed []byte) (peerProfile, error) {
	data, err := sealer.Open(peersTable, peerRowKey(id, profileColumn), sealed)
	if err != nil {
		return peerProfile{}, fmt.Errorf("trust: open peer profile: %w", err)
	}
	var profile peerProfile
	if err := json.Unmarshal(data, &profile); err != nil {
		return peerProfile{}, fmt.Errorf("trust: decode peer profile: %w", err)
	}
	if profile.Version != profileVersion {
		return peerProfile{}, fmt.Errorf("trust: peer profile version %d", profile.Version)
	}
	return profile, nil
}

// sealPeerAddrs encodes and seals the address cache of the peer.
func sealPeerAddrs(sealer *store.Sealer, peer Peer) ([]byte, error) {
	data, err := json.Marshal(slices.Clone(peer.LastAddrs))
	if err != nil {
		return nil, fmt.Errorf("trust: encode peer addresses: %w", err)
	}
	sealed, err := sealer.Seal(peersTable, peerRowKey(peer.Public.DeviceID, addrsColumn), data)
	if err != nil {
		return nil, fmt.Errorf("trust: seal peer addresses: %w", err)
	}
	return sealed, nil
}

// openPeerAddrs opens and decodes the address cache of the peer.
func openPeerAddrs(sealer *store.Sealer, id model.DeviceID, sealed []byte) ([]string, error) {
	data, err := sealer.Open(peersTable, peerRowKey(id, addrsColumn), sealed)
	if err != nil {
		return nil, fmt.Errorf("trust: open peer addresses: %w", err)
	}
	var addrs []string
	if err := json.Unmarshal(data, &addrs); err != nil {
		return nil, fmt.Errorf("trust: decode peer addresses: %w", err)
	}
	return addrs, nil
}

// unixMilli encodes a time in milliseconds, keeping the zero time as 0.
func unixMilli(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

// timeFromMilli decodes a time encoded by unixMilli.
func timeFromMilli(ms int64) time.Time {
	if ms == 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}
