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
	"github.com/lebe-dev/unison/core/model/protoconv"
	"github.com/lebe-dev/unison/core/store"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// Limits of the introduction records.
const (
	// MaxIntroductions bounds the introduction records of one device.
	MaxIntroductions = 4096
	// introductionVersion is the version of the signed introduction message.
	introductionVersion = 1
	// introductionsTable holds the sealed introduction records.
	introductionsTable = "introductions"
)

// IntroductionSignatureType is the signature type of an introduction.
const IntroductionSignatureType = "introduction"

// MaxClockSkew bounds how far in the future a signed record may be dated. A
// record far ahead of the local clock would block every later one.
const MaxClockSkew = 24 * time.Hour

// ErrIntroductionNotFound reports a subject without a stored introduction.
var ErrIntroductionNotFound = errors.New("trust: introduction not found")

// Introduction is a signed statement of a Personal device that another device
// belongs to the group. Friends devices are never introduced.
type Introduction struct {
	// Subject is the introduced device.
	Subject identity.PublicIdentity
	// Role is the trust role of the subject. Only the Personal role is
	// accepted.
	Role model.Role
	// Issuer is the device that signed the statement.
	Issuer model.DeviceID
	// IssuedAt is the signing time.
	IssuedAt time.Time
	// SubjectName is the display name of the subject known to the issuer.
	SubjectName string
	// Platform is the operating system of the subject.
	Platform model.Platform
	// Signature is the hybrid signature of the issuer.
	Signature identity.Signature
}

// IntroductionInfo describes the device an introduction is about.
type IntroductionInfo struct {
	// Subject is the introduced device.
	Subject identity.PublicIdentity
	// Name is the display name of the subject.
	Name string
	// Platform is the operating system of the subject.
	Platform model.Platform
}

// IntroductionHead is the part of a stored introduction that a digest carries.
type IntroductionHead struct {
	Subject  model.DeviceID
	IssuedAt time.Time
}

// NewIntroduction signs an introduction of the subject by the local device.
func NewIntroduction(local *identity.Identity, subject IntroductionInfo, at time.Time) (Introduction, error) {
	intro := Introduction{
		Subject:     subject.Subject,
		Role:        model.RolePersonal,
		Issuer:      local.DeviceID(),
		IssuedAt:    at,
		SubjectName: subject.Name,
		Platform:    subject.Platform,
	}
	if err := intro.validate(); err != nil {
		return Introduction{}, err
	}
	signature, err := local.Sign(IntroductionSignatureType, intro.message())
	if err != nil {
		return Introduction{}, fmt.Errorf("trust: sign introduction: %w", err)
	}
	intro.Signature = signature
	return intro, nil
}

// DeviceID returns the device ID of the subject.
func (i Introduction) DeviceID() model.DeviceID {
	return i.Subject.DeviceID
}

// Validate checks the fields of the record.
func (i Introduction) Validate() error {
	return i.validate()
}

// Verify checks the signature of the issuer.
func (i Introduction) Verify(issuer identity.PublicIdentity) error {
	if i.Issuer != issuer.DeviceID {
		return fmt.Errorf("%w: introduction of %s signed by %s", ErrInvalidPeer, i.Issuer, issuer.DeviceID)
	}
	if err := i.validate(); err != nil {
		return err
	}
	if err := issuer.Verify(IntroductionSignatureType, i.message(), i.Signature); err != nil {
		return fmt.Errorf("trust: verify introduction: %w", err)
	}
	return nil
}

// Proto returns the wire form of the record.
func (i Introduction) Proto() *wirev1.Introduction {
	return &wirev1.Introduction{
		SubjectEd25519: i.Subject.Ed25519,
		SubjectMldsa:   i.Subject.MLDSABytes(),
		Role:           protoconv.RoleToProto(i.Role),
		Issuer:         i.Issuer.Bytes(),
		TsUnixMs:       unixMilli(i.IssuedAt),
		Ed25519Sig:     i.Signature.Ed25519,
		MldsaSig:       i.Signature.MLDSA,
		SubjectName:    i.SubjectName,
		Platform:       protoconv.PlatformToProto(i.Platform),
	}
}

// IntroductionFromProto decodes and validates a wire introduction.
func IntroductionFromProto(message *wirev1.Introduction) (Introduction, error) {
	if message == nil {
		return Introduction{}, fmt.Errorf("%w: empty introduction", ErrInvalidPeer)
	}
	public, err := identity.NewPublicIdentity(message.GetSubjectEd25519(), message.GetSubjectMldsa())
	if err != nil {
		return Introduction{}, fmt.Errorf("%w: %w", ErrInvalidPeer, err)
	}
	issuer, err := model.DeviceIDFromBytes(message.GetIssuer())
	if err != nil {
		return Introduction{}, fmt.Errorf("%w: %w", ErrInvalidPeer, err)
	}
	intro := Introduction{
		Subject:     public,
		Role:        protoconv.RoleFromProto(message.GetRole()),
		Issuer:      issuer,
		IssuedAt:    timeFromMilli(message.GetTsUnixMs()),
		SubjectName: message.GetSubjectName(),
		Platform:    protoconv.PlatformFromProto(message.GetPlatform()),
		Signature: identity.Signature{
			Ed25519: message.GetEd25519Sig(),
			MLDSA:   message.GetMldsaSig(),
		},
	}
	if err := intro.validate(); err != nil {
		return Introduction{}, err
	}
	return intro, nil
}

// validate checks the fields of the record. An unknown platform is kept as
// unknown: the display data is informative and the subject announces the real
// one in the handshake.
func (i Introduction) validate() error {
	if i.Role != model.RolePersonal {
		return fmt.Errorf("%w: an introduction carries the personal role, not %s", ErrInvalidPeer, i.Role)
	}
	if i.Issuer.IsZero() {
		return fmt.Errorf("%w: introduction without an issuer", ErrInvalidPeer)
	}
	if i.Subject.DeviceID.IsZero() {
		return fmt.Errorf("%w: introduction without a subject", ErrInvalidPeer)
	}
	public, err := identity.NewPublicIdentity(i.Subject.Ed25519, i.Subject.MLDSABytes())
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidPeer, err)
	}
	if public.DeviceID != i.Subject.DeviceID {
		return fmt.Errorf("%w: subject device id does not match the public keys", ErrInvalidPeer)
	}
	if i.Subject.DeviceID == i.Issuer {
		return fmt.Errorf("%w: a device cannot introduce itself", ErrInvalidPeer)
	}
	if i.IssuedAt.IsZero() {
		return fmt.Errorf("%w: introduction without a time", ErrInvalidPeer)
	}
	if i.Platform > model.PlatformAndroid {
		return fmt.Errorf("%w: unknown platform %d", ErrInvalidPeer, i.Platform)
	}
	return validateName(i.SubjectName)
}

// message returns the bytes the issuer signs. The encoding is explicit, so a
// record keeps its meaning when the wire message gains fields.
func (i Introduction) message() []byte {
	name := []byte(i.SubjectName)
	message := make([]byte, 0, 1+32+1952+1+1+32+8+2+len(name))
	message = append(message, introductionVersion)
	message = append(message, i.Subject.Ed25519...)
	message = append(message, i.Subject.MLDSABytes()...)
	message = append(message, byte(i.Role), byte(i.Platform))
	message = append(message, i.Issuer.Bytes()...)
	message = binary.BigEndian.AppendUint64(message, uint64(unixMilli(i.IssuedAt)))
	message = binary.BigEndian.AppendUint16(message, uint16(len(name)))
	return append(message, name...)
}

// AddIntroduction stores a signed record without touching the peer list. It is
// how the local device keeps the introductions it issues, so they can be sent
// to the rest of the group. A record that is not newer than the stored one for
// the same subject is ignored.
func (s *Store) AddIntroduction(ctx context.Context, intro Introduction) (bool, error) {
	if err := intro.validate(); err != nil {
		return false, err
	}
	if err := checkNotAhead(intro.IssuedAt); err != nil {
		return false, err
	}
	sealed, err := sealIntroduction(s.db.Sealer(), intro)
	if err != nil {
		return false, err
	}

	stored := false
	err = s.db.WithTx(ctx, func(tx *sql.Tx) error {
		fresh, err := introductionIsNewer(ctx, tx, intro)
		if err != nil || !fresh {
			return err
		}
		count, err := introductionCount(ctx, tx)
		if err != nil {
			return err
		}
		if count >= MaxIntroductions {
			return fmt.Errorf("%w: %d introduction records", ErrInvalidPeer, count)
		}
		stored = true
		return writeIntroduction(ctx, tx, intro, sealed)
	})
	if err != nil {
		return false, err
	}
	return stored, nil
}

// ApplyIntroduction verifies an introduction against the trust store and adds
// the introduced device with its issuer. It reports whether the device became a
// new peer.
//
// Every check fails closed: the issuer has to be a stored, non-revoked Personal
// peer, the signature has to verify, the subject must not be on the deny-list
// and an existing device keeps the role and the issuer it has.
func (s *Store) ApplyIntroduction(ctx context.Context, intro Introduction) (bool, error) {
	if err := intro.validate(); err != nil {
		return false, err
	}
	if err := checkNotAhead(intro.IssuedAt); err != nil {
		return false, err
	}
	sealed, err := sealIntroduction(s.db.Sealer(), intro)
	if err != nil {
		return false, err
	}

	added := false
	err = s.db.WithTx(ctx, func(tx *sql.Tx) error {
		issuer, err := readPeer(ctx, tx, s.db.Sealer(), peerByIDQuery, intro.Issuer.Bytes())
		if err != nil {
			return fmt.Errorf("%w: issuer %s: %w", ErrInvalidPeer, intro.Issuer, err)
		}
		if issuer.Revoked {
			return fmt.Errorf("%w: issuer %s is revoked", ErrInvalidPeer, intro.Issuer)
		}
		if issuer.Role != model.RolePersonal {
			return fmt.Errorf("%w: issuer %s is not personal", ErrInvalidPeer, intro.Issuer)
		}
		if err := intro.Verify(issuer.Public); err != nil {
			return err
		}

		fresh, err := introductionIsNewer(ctx, tx, intro)
		if err != nil {
			return err
		}
		if !fresh {
			return nil
		}
		if err := checkIntroductionDenyList(ctx, tx, intro); err != nil {
			return err
		}

		count, err := introductionCount(ctx, tx)
		if err != nil {
			return err
		}
		if count >= MaxIntroductions {
			return fmt.Errorf("%w: %d introduction records", ErrInvalidPeer, count)
		}
		if err := writeIntroduction(ctx, tx, intro, sealed); err != nil {
			return err
		}
		return addIntroducedPeer(ctx, tx, s.db.Sealer(), intro, &added)
	})
	if err != nil {
		return false, err
	}
	return added, nil
}

// IntroductionHeads returns the subject and the signing time of every stored
// introduction, ordered by subject.
func (s *Store) IntroductionHeads(ctx context.Context) ([]IntroductionHead, error) {
	heads := make([]IntroductionHead, 0, 8)
	err := s.db.View(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, "SELECT subject, ts FROM introductions ORDER BY subject")
		if err != nil {
			return fmt.Errorf("trust: list introductions: %w", err)
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
				return fmt.Errorf("trust: read introduction: %w", err)
			}
			heads = append(heads, IntroductionHead{Subject: id, IssuedAt: timeFromMilli(ts)})
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return heads, nil
}

// IntroductionBySubject returns the stored introduction of the subject.
func (s *Store) IntroductionBySubject(ctx context.Context, id model.DeviceID) (Introduction, error) {
	if id.IsZero() {
		return Introduction{}, fmt.Errorf("%w: empty device id", ErrInvalidPeer)
	}
	var (
		intro  Introduction
		sealed []byte
	)
	err := s.db.View(ctx, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, "SELECT record FROM introductions WHERE subject = ?", id.Bytes()).Scan(&sealed)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrIntroductionNotFound
		}
		if err != nil {
			return fmt.Errorf("trust: read introduction: %w", err)
		}
		intro, err = openIntroduction(s.db.Sealer(), id, sealed)
		return err
	})
	if err != nil {
		return Introduction{}, err
	}
	return intro, nil
}

// IntroductionIssuer returns the issuer of the stored introduction of the
// subject, or ErrIntroductionNotFound.
func (s *Store) IntroductionIssuer(ctx context.Context, id model.DeviceID) (model.DeviceID, error) {
	if id.IsZero() {
		return model.DeviceID{}, fmt.Errorf("%w: empty device id", ErrInvalidPeer)
	}
	var issuer []byte
	err := s.db.View(ctx, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, "SELECT issuer FROM introductions WHERE subject = ?", id.Bytes()).Scan(&issuer)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrIntroductionNotFound
		}
		return err
	})
	if err != nil {
		return model.DeviceID{}, err
	}
	return model.DeviceIDFromBytes(issuer)
}

// introductionIsNewer reports whether the record is newer than the stored one,
// so that a replay of an old record changes nothing.
func introductionIsNewer(ctx context.Context, tx *sql.Tx, intro Introduction) (bool, error) {
	var ts int64
	err := tx.QueryRowContext(ctx, "SELECT ts FROM introductions WHERE subject = ?", intro.DeviceID().Bytes()).Scan(&ts)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return true, nil
	case err != nil:
		return false, fmt.Errorf("trust: read introduction time: %w", err)
	default:
		return unixMilli(intro.IssuedAt) > ts, nil
	}
}

// introductionCount returns the number of stored introduction records.
func introductionCount(ctx context.Context, tx *sql.Tx) (int, error) {
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM introductions").Scan(&count); err != nil {
		return 0, fmt.Errorf("trust: count introductions: %w", err)
	}
	return count, nil
}

// checkIntroductionDenyList rejects a subject that is on the deny-list.
func checkIntroductionDenyList(ctx context.Context, tx *sql.Tx, intro Introduction) error {
	const query = "SELECT 1 FROM peers WHERE revoked = 1 AND (device_id = ? OR ed25519_pub = ?)"
	var found int
	err := tx.QueryRowContext(ctx, query, intro.DeviceID().Bytes(), intro.Subject.Ed25519).Scan(&found)
	switch {
	case err == nil:
		return fmt.Errorf("%w: subject %s is revoked", ErrPeerRevoked, intro.DeviceID())
	case !errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("trust: check deny-list: %w", err)
	}
	return nil
}

// addIntroducedPeer adds the subject of the record as a peer. A known device
// keeps its record: an introduction never changes the role or the issuer of a
// paired device.
func addIntroducedPeer(ctx context.Context, tx *sql.Tx, sealer *store.Sealer, intro Introduction, added *bool) error {
	stored, err := readPeer(ctx, tx, sealer, peerByIDQuery, intro.DeviceID().Bytes())
	switch {
	case err == nil:
		if stored.Revoked {
			return ErrPeerRevoked
		}
		if stored.Role != model.RolePersonal {
			return fmt.Errorf("%w: subject %s has role %s", ErrInvalidPeer, intro.DeviceID(), stored.Role)
		}
		return nil
	case errors.Is(err, ErrPeerNotFound):
	default:
		return err
	}

	peer := Peer{
		Public:       intro.Subject,
		Name:         intro.SubjectName,
		Platform:     intro.Platform,
		Role:         model.RolePersonal,
		IntroducedBy: intro.Issuer,
	}
	if err := peer.Validate(); err != nil {
		return err
	}
	peer.AddedAt = time.Now()
	if err := insertPeer(ctx, tx, sealer, peer); err != nil {
		return err
	}
	*added = true
	return nil
}

// writeIntroduction stores the sealed record of an introduction.
func writeIntroduction(ctx context.Context, tx *sql.Tx, intro Introduction, sealed []byte) error {
	const query = `INSERT INTO introductions (subject, issuer, ts, record) VALUES (?, ?, ?, ?)
		ON CONFLICT (subject) DO UPDATE SET
			issuer = excluded.issuer, ts = excluded.ts, record = excluded.record`
	_, err := tx.ExecContext(ctx, query, intro.DeviceID().Bytes(), intro.Issuer.Bytes(), unixMilli(intro.IssuedAt), sealed)
	if err != nil {
		return fmt.Errorf("trust: write introduction: %w", err)
	}
	return nil
}

// checkNotAhead rejects a time too far in the future.
func checkNotAhead(at time.Time) error {
	if at.After(time.Now().Add(MaxClockSkew)) {
		return fmt.Errorf("%w: signing time is too far ahead", ErrInvalidPeer)
	}
	return nil
}

// introductionRowKey binds a sealed record to its subject.
func introductionRowKey(id model.DeviceID) string {
	return id.String()
}

// sealIntroduction encodes and seals the record of an introduction.
func sealIntroduction(sealer *store.Sealer, intro Introduction) ([]byte, error) {
	data, err := proto.Marshal(intro.Proto())
	if err != nil {
		return nil, fmt.Errorf("trust: encode introduction: %w", err)
	}
	sealed, err := sealer.Seal(introductionsTable, introductionRowKey(intro.DeviceID()), data)
	if err != nil {
		return nil, fmt.Errorf("trust: seal introduction: %w", err)
	}
	return sealed, nil
}

// openIntroduction opens and decodes the sealed record of an introduction.
func openIntroduction(sealer *store.Sealer, id model.DeviceID, sealed []byte) (Introduction, error) {
	data, err := sealer.Open(introductionsTable, introductionRowKey(id), sealed)
	if err != nil {
		return Introduction{}, fmt.Errorf("trust: open introduction: %w", err)
	}
	message := &wirev1.Introduction{}
	if err := proto.Unmarshal(data, message); err != nil {
		return Introduction{}, fmt.Errorf("trust: decode introduction: %w", err)
	}
	intro, err := IntroductionFromProto(message)
	if err != nil {
		return Introduction{}, err
	}
	if intro.DeviceID() != id {
		return Introduction{}, fmt.Errorf("%w: stored introduction of another subject", ErrInvalidPeer)
	}
	return intro, nil
}
