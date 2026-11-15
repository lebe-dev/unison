package trust

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/lebe-dev/unison/core/identity"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/store"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// Limits and identifiers of the revocation records.
const (
	// MaxRevocations bounds the stored revocation records.
	MaxRevocations = 4096
	// RevocationSignatureType is the signature type of a revocation.
	RevocationSignatureType = "revocation"
	// revocationVersion is the version of the signed revocation message.
	revocationVersion = 1
	// revocationsTable holds the sealed revocation records.
	revocationsTable = "revocations"
)

// Errors of the revocation records.
var (
	// ErrSelfRevocation reports a signed revocation of the local device. The
	// local device is not a peer of itself, so the record cannot be applied
	// here; the caller leaves the group instead.
	ErrSelfRevocation = errors.New("trust: the local device is revoked")
	// ErrRevocationNotFound reports a subject without a stored revocation.
	ErrRevocationNotFound = errors.New("trust: revocation not found")
)

// Revocation is a signed statement of a Personal device that another device is
// no longer trusted. A revocation is irreversible: the subject stays on the
// deny-list.
type Revocation struct {
	// Subject is the revoked device.
	Subject model.DeviceID
	// Issuer is the device that signed the statement.
	Issuer model.DeviceID
	// IssuedAt is the signing time.
	IssuedAt time.Time
	// Signature is the hybrid signature of the issuer.
	Signature identity.Signature
}

// RevocationHead is the part of a stored revocation that a gossip digest
// carries.
type RevocationHead struct {
	Subject  model.DeviceID
	IssuedAt time.Time
}

// NewRevocation signs a revocation of subject by the local device.
func NewRevocation(local *identity.Identity, subject model.DeviceID, at time.Time) (Revocation, error) {
	if local == nil {
		return Revocation{}, errors.New("trust: nil identity")
	}
	revocation := Revocation{
		Subject:  subject,
		Issuer:   local.DeviceID(),
		IssuedAt: at,
	}
	if err := revocation.Validate(); err != nil {
		return Revocation{}, err
	}
	signature, err := local.Sign(RevocationSignatureType, revocation.message())
	if err != nil {
		return Revocation{}, fmt.Errorf("trust: sign revocation: %w", err)
	}
	revocation.Signature = signature
	return revocation, nil
}

// Validate checks the fields of the record.
func (r Revocation) Validate() error {
	if r.Subject.IsZero() {
		return fmt.Errorf("%w: revocation without a subject", ErrInvalidPeer)
	}
	if r.Issuer.IsZero() {
		return fmt.Errorf("%w: revocation without an issuer", ErrInvalidPeer)
	}
	if r.IssuedAt.IsZero() {
		return fmt.Errorf("%w: revocation without a time", ErrInvalidPeer)
	}
	return nil
}

// Verify checks the signature of the issuer. A device may revoke itself, so the
// subject and the issuer are allowed to be the same device.
func (r Revocation) Verify(issuer identity.PublicIdentity) error {
	if r.Issuer != issuer.DeviceID {
		return fmt.Errorf("%w: revocation of %s signed by %s", ErrInvalidPeer, r.Subject, issuer.DeviceID)
	}
	if err := r.Validate(); err != nil {
		return err
	}
	if err := issuer.Verify(RevocationSignatureType, r.message(), r.Signature); err != nil {
		return fmt.Errorf("trust: verify revocation: %w", err)
	}
	return nil
}

// Proto returns the wire form of the record.
func (r Revocation) Proto() *wirev1.Revocation {
	return &wirev1.Revocation{
		Subject:    r.Subject.Bytes(),
		Issuer:     r.Issuer.Bytes(),
		TsUnixMs:   unixMilli(r.IssuedAt),
		Ed25519Sig: r.Signature.Ed25519,
		MldsaSig:   r.Signature.MLDSA,
	}
}

// RevocationFromProto decodes and validates a wire revocation.
func RevocationFromProto(message *wirev1.Revocation) (Revocation, error) {
	if message == nil {
		return Revocation{}, fmt.Errorf("%w: empty revocation", ErrInvalidPeer)
	}
	subject, err := model.DeviceIDFromBytes(message.GetSubject())
	if err != nil {
		return Revocation{}, fmt.Errorf("%w: %w", ErrInvalidPeer, err)
	}
	issuer, err := model.DeviceIDFromBytes(message.GetIssuer())
	if err != nil {
		return Revocation{}, fmt.Errorf("%w: %w", ErrInvalidPeer, err)
	}
	revocation := Revocation{
		Subject:  subject,
		Issuer:   issuer,
		IssuedAt: timeFromMilli(message.GetTsUnixMs()),
		Signature: identity.Signature{
			Ed25519: message.GetEd25519Sig(),
			MLDSA:   message.GetMldsaSig(),
		},
	}
	if err := revocation.Validate(); err != nil {
		return Revocation{}, err
	}
	return revocation, nil
}

// message returns the bytes the issuer signs. The encoding is explicit, so a
// record keeps its meaning when the wire message gains fields.
func (r Revocation) message() []byte {
	message := make([]byte, 0, 1+32+32+8)
	message = append(message, revocationVersion)
	message = append(message, r.Subject.Bytes()...)
	message = append(message, r.Issuer.Bytes()...)
	return binary.BigEndian.AppendUint64(message, uint64(unixMilli(r.IssuedAt)))
}

// AddRevocation stores a revocation signed by the local device and marks the
// subject revoked. The signature is taken as is: the record is created here, so
// the caller has to sign it with the identity of the local device.
func (s *Store) AddRevocation(ctx context.Context, revocation Revocation) (bool, error) {
	if err := revocation.Validate(); err != nil {
		return false, err
	}
	if err := checkNotAhead(revocation.IssuedAt); err != nil {
		return false, err
	}

	fresh := false
	err := s.db.WithTx(ctx, func(tx *sql.Tx) error {
		var err error
		fresh, err = revokeSubject(ctx, tx, s.db.Sealer(), revocation)
		return err
	})
	if err != nil {
		return false, err
	}
	return fresh, nil
}

// ApplyRevocation verifies a revocation of another device and marks the subject
// revoked. It reports whether this record revoked the subject.
//
// Every check fails closed: the issuer has to be a stored, non-revoked Personal
// peer and the signature and the signing time have to verify. A valid
// revocation of the local device is reported as ErrSelfRevocation.
func (s *Store) ApplyRevocation(ctx context.Context, local model.DeviceID, revocation Revocation) (bool, error) {
	if local.IsZero() {
		return false, fmt.Errorf("%w: empty local device id", ErrInvalidPeer)
	}
	if err := revocation.Validate(); err != nil {
		return false, err
	}
	if err := checkNotAhead(revocation.IssuedAt); err != nil {
		return false, err
	}

	fresh := false
	err := s.db.WithTx(ctx, func(tx *sql.Tx) error {
		issuer, err := readPeer(ctx, tx, s.db.Sealer(), peerByIDQuery, revocation.Issuer.Bytes())
		if err != nil {
			return fmt.Errorf("%w: issuer %s: %w", ErrInvalidPeer, revocation.Issuer, err)
		}
		if issuer.Revoked {
			return fmt.Errorf("%w: issuer %s is revoked", ErrInvalidPeer, revocation.Issuer)
		}
		if issuer.Role != model.RolePersonal {
			return fmt.Errorf("%w: issuer %s is not personal", ErrInvalidPeer, revocation.Issuer)
		}
		if err := revocation.Verify(issuer.Public); err != nil {
			return err
		}
		if revocation.Subject == local {
			return ErrSelfRevocation
		}
		fresh, err = revokeSubject(ctx, tx, s.db.Sealer(), revocation)
		return err
	})
	if err != nil {
		return false, err
	}
	return fresh, nil
}

// RevocationHeads returns the subject and the signing time of every stored
// revocation, ordered by subject.
func (s *Store) RevocationHeads(ctx context.Context) ([]RevocationHead, error) {
	heads := make([]RevocationHead, 0, 8)
	err := s.db.View(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, "SELECT subject, ts FROM revocations ORDER BY subject")
		if err != nil {
			return fmt.Errorf("trust: list revocations: %w", err)
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			var (
				subject []byte
				ts      int64
			)
			if err := rows.Scan(&subject, &ts); err != nil {
				return err
			}
			id, err := model.DeviceIDFromBytes(subject)
			if err != nil {
				return fmt.Errorf("trust: read revocation: %w", err)
			}
			heads = append(heads, RevocationHead{Subject: id, IssuedAt: timeFromMilli(ts)})
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return heads, nil
}

// RevocationBySubject returns the stored revocation of the subject.
func (s *Store) RevocationBySubject(ctx context.Context, id model.DeviceID) (Revocation, error) {
	if id.IsZero() {
		return Revocation{}, fmt.Errorf("%w: empty device id", ErrInvalidPeer)
	}
	var (
		revocation Revocation
		sealed     []byte
	)
	err := s.db.View(ctx, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, "SELECT record FROM revocations WHERE subject = ?", id.Bytes()).Scan(&sealed)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrRevocationNotFound
		}
		if err != nil {
			return fmt.Errorf("trust: read revocation: %w", err)
		}
		revocation, err = openRevocation(s.db.Sealer(), id, sealed)
		return err
	})
	if err != nil {
		return Revocation{}, err
	}
	return revocation, nil
}

// revokeSubject stores the record and marks its subject revoked. It reports
// whether the record is newer than the stored one. The caller must hold a
// transaction.
func revokeSubject(ctx context.Context, tx *sql.Tx, sealer *store.Sealer, revocation Revocation) (bool, error) {
	stored, known, err := revocationTime(ctx, tx, revocation.Subject)
	if err != nil {
		return false, err
	}
	if known && unixMilli(revocation.IssuedAt) <= stored {
		return false, nil
	}
	if !known {
		count, err := revocationCount(ctx, tx)
		if err != nil {
			return false, err
		}
		if count >= MaxRevocations {
			return false, fmt.Errorf("%w: %d revocation records", ErrInvalidPeer, count)
		}
	}

	if _, err := tx.ExecContext(ctx, "UPDATE peers SET revoked = 1 WHERE device_id = ?", revocation.Subject.Bytes()); err != nil {
		return false, fmt.Errorf("trust: revoke peer: %w", err)
	}
	if err := revokePeerRow(ctx, tx, sealer, revocation.Subject); err != nil {
		return false, err
	}
	if err := dropIntroduction(ctx, tx, revocation.Subject); err != nil {
		return false, err
	}
	if err := dropRendezvousKey(ctx, tx, revocation.Subject); err != nil {
		return false, err
	}
	if err := writeRevocation(ctx, tx, sealer, revocation); err != nil {
		return false, err
	}
	return true, nil
}

// revokePeerRow checks that the subject is a stored peer and drops the address
// cache that stops being useful. A revocation of a device the store does not
// know is refused, so a record alone never adds a peer.
func revokePeerRow(ctx context.Context, tx *sql.Tx, sealer *store.Sealer, id model.DeviceID) error {
	peer, err := readPeer(ctx, tx, sealer, peerByIDQuery, id.Bytes())
	if err != nil {
		return err
	}
	peer.LastAddrs = nil
	return writePeerAddrs(ctx, tx, sealer, peer)
}

// writePeerAddrs stores the sealed address cache of the peer.
func writePeerAddrs(ctx context.Context, tx *sql.Tx, sealer *store.Sealer, peer Peer) error {
	sealed, err := sealPeerAddrs(sealer, peer)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE peers SET addrs = ? WHERE device_id = ?", sealed, peer.Public.DeviceID.Bytes()); err != nil {
		return fmt.Errorf("trust: write peer addresses: %w", err)
	}
	return nil
}

// dropIntroduction removes the introduction of a subject, so it stops
// spreading through the group.
func dropIntroduction(ctx context.Context, tx *sql.Tx, subject model.DeviceID) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM introductions WHERE subject = ?", subject.Bytes()); err != nil {
		return fmt.Errorf("trust: drop introduction: %w", err)
	}
	return nil
}

// dropRendezvousKey removes the relay rendezvous key of a peer.
func dropRendezvousKey(ctx context.Context, tx *sql.Tx, id model.DeviceID) error {
	name, err := rendezvousKeyName(id)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM group_secrets WHERE name = ?", name); err != nil {
		return fmt.Errorf("trust: drop rendezvous key: %w", err)
	}
	return nil
}

// revocationTime returns the signing time of the stored revocation and whether
// the subject has one.
func revocationTime(ctx context.Context, tx *sql.Tx, subject model.DeviceID) (int64, bool, error) {
	var ts int64
	err := tx.QueryRowContext(ctx, "SELECT ts FROM revocations WHERE subject = ?", subject.Bytes()).Scan(&ts)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return 0, false, nil
	case err != nil:
		return 0, false, fmt.Errorf("trust: read revocation time: %w", err)
	default:
		return ts, true, nil
	}
}

// revocationCount returns the number of stored revocation records.
func revocationCount(ctx context.Context, tx *sql.Tx) (int, error) {
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM revocations").Scan(&count); err != nil {
		return 0, fmt.Errorf("trust: count revocations: %w", err)
	}
	return count, nil
}

// writeRevocation stores the sealed record of a revocation.
func writeRevocation(ctx context.Context, tx *sql.Tx, sealer *store.Sealer, revocation Revocation) error {
	sealed, err := sealRevocation(sealer, revocation)
	if err != nil {
		return err
	}
	const query = `INSERT INTO revocations (subject, issuer, ts, record) VALUES (?, ?, ?, ?)
		ON CONFLICT (subject) DO UPDATE SET
			issuer = excluded.issuer, ts = excluded.ts, record = excluded.record`
	_, err = tx.ExecContext(ctx, query, revocation.Subject.Bytes(), revocation.Issuer.Bytes(), unixMilli(revocation.IssuedAt), sealed)
	if err != nil {
		return fmt.Errorf("trust: write revocation: %w", err)
	}
	return nil
}

// revocationRowKey binds a sealed record to its subject.
func revocationRowKey(id model.DeviceID) string {
	return id.String()
}

// sealRevocation encodes and seals the record of a revocation.
func sealRevocation(sealer *store.Sealer, revocation Revocation) ([]byte, error) {
	data, err := proto.Marshal(revocation.Proto())
	if err != nil {
		return nil, fmt.Errorf("trust: encode revocation: %w", err)
	}
	sealed, err := sealer.Seal(revocationsTable, revocationRowKey(revocation.Subject), data)
	if err != nil {
		return nil, fmt.Errorf("trust: seal revocation: %w", err)
	}
	return sealed, nil
}

// openRevocation opens and decodes the sealed record of a revocation.
func openRevocation(sealer *store.Sealer, id model.DeviceID, sealed []byte) (Revocation, error) {
	data, err := sealer.Open(revocationsTable, revocationRowKey(id), sealed)
	if err != nil {
		return Revocation{}, fmt.Errorf("trust: open revocation: %w", err)
	}
	message := &wirev1.Revocation{}
	if err := proto.Unmarshal(data, message); err != nil {
		return Revocation{}, fmt.Errorf("trust: decode revocation: %w", err)
	}
	revocation, err := RevocationFromProto(message)
	if err != nil {
		return Revocation{}, err
	}
	if revocation.Subject != id {
		return Revocation{}, fmt.Errorf("%w: stored revocation of another subject", ErrInvalidPeer)
	}
	return revocation, nil
}
