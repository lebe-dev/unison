package trust

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/lebe-dev/unison/core/identity"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/store"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// PolicySignatureType is the signature type of a peer policy.
const PolicySignatureType = "peer-policy"

// policyVersion is the version of the signed policy message. Version 2 added
// the scope; a version 1 policy does not verify any more.
const policyVersion = 2

// policyTsKeyPrefix starts the settings key of the signing time of the last
// policy applied from one issuer.
const policyTsKeyPrefix = "trust.policy_ts."

// ErrForeignPolicy reports a policy that targets another device.
var ErrForeignPolicy = errors.New("trust: policy for another device")

// ErrPolicyReplay reports a policy that is not newer than the applied one.
var ErrPolicyReplay = errors.New("trust: peer policy is not newer")

// ErrRemoteSettingsDenied reports a policy from a device that may not change
// the settings of this device remotely.
var ErrRemoteSettingsDenied = errors.New("trust: remote settings changes are not allowed")

// SetPeerPolicy is a signed request of a Personal device that the target device
// asks its user before it takes what the issuer sends in the scope. The issuer
// is the device that signed the request.
type SetPeerPolicy struct {
	// Target is the device the policy applies to.
	Target model.DeviceID
	// Scope is the content the flag covers.
	Scope Scope
	// RequireConfirmation is the new state of the flag.
	RequireConfirmation bool
	// SignedAt is the signing time.
	SignedAt time.Time
	// Signature is the hybrid signature of the issuer.
	Signature identity.Signature
}

// NewSetPeerPolicy signs a change of a confirmation flag of target for the
// local device.
func NewSetPeerPolicy(local *identity.Identity, target model.DeviceID, scope Scope, require bool, at time.Time) (SetPeerPolicy, error) {
	if local == nil {
		return SetPeerPolicy{}, errors.New("trust: nil identity")
	}
	policy := SetPeerPolicy{
		Target:              target,
		Scope:               scope,
		RequireConfirmation: require,
		SignedAt:            at,
	}
	if err := policy.Validate(); err != nil {
		return SetPeerPolicy{}, err
	}
	signature, err := local.Sign(PolicySignatureType, policy.message(local.DeviceID()))
	if err != nil {
		return SetPeerPolicy{}, fmt.Errorf("trust: sign peer policy: %w", err)
	}
	policy.Signature = signature
	return policy, nil
}

// Validate checks the fields of the policy.
func (p SetPeerPolicy) Validate() error {
	if p.Target.IsZero() {
		return fmt.Errorf("%w: policy without a target", ErrInvalidPeer)
	}
	if !p.Scope.Valid() {
		return fmt.Errorf("%w: policy without a scope", ErrInvalidPeer)
	}
	if p.SignedAt.IsZero() {
		return fmt.Errorf("%w: policy without a time", ErrInvalidPeer)
	}
	return nil
}

// Verify checks the signature of the issuer.
func (p SetPeerPolicy) Verify(issuer identity.PublicIdentity) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if err := issuer.Verify(PolicySignatureType, p.message(issuer.DeviceID), p.Signature); err != nil {
		return fmt.Errorf("trust: verify peer policy: %w", err)
	}
	return nil
}

// Proto returns the wire form of the policy.
func (p SetPeerPolicy) Proto() *wirev1.SetPeerPolicy {
	return &wirev1.SetPeerPolicy{
		Target:              p.Target.Bytes(),
		Scope:               p.Scope.Proto(),
		RequireConfirmation: p.RequireConfirmation,
		TsUnixMs:            unixMilli(p.SignedAt),
		Ed25519Sig:          p.Signature.Ed25519,
		MldsaSig:            p.Signature.MLDSA,
	}
}

// SetPeerPolicyFromProto decodes and validates a wire policy.
func SetPeerPolicyFromProto(message *wirev1.SetPeerPolicy) (SetPeerPolicy, error) {
	if message == nil {
		return SetPeerPolicy{}, fmt.Errorf("%w: empty peer policy", ErrInvalidPeer)
	}
	target, err := model.DeviceIDFromBytes(message.GetTarget())
	if err != nil {
		return SetPeerPolicy{}, fmt.Errorf("%w: %w", ErrInvalidPeer, err)
	}
	scope, err := ScopeFromProto(message.GetScope())
	if err != nil {
		return SetPeerPolicy{}, err
	}
	policy := SetPeerPolicy{
		Target:              target,
		Scope:               scope,
		RequireConfirmation: message.GetRequireConfirmation(),
		SignedAt:            timeFromMilli(message.GetTsUnixMs()),
		Signature: identity.Signature{
			Ed25519: message.GetEd25519Sig(),
			MLDSA:   message.GetMldsaSig(),
		},
	}
	if err := policy.Validate(); err != nil {
		return SetPeerPolicy{}, err
	}
	return policy, nil
}

// message returns the bytes the issuer signs. The encoding is explicit, so a
// policy keeps its meaning when the wire message gains fields.
func (p SetPeerPolicy) message(issuer model.DeviceID) []byte {
	message := make([]byte, 0, 1+32+32+1+1+8)
	message = append(message, policyVersion)
	message = append(message, issuer.Bytes()...)
	message = append(message, p.Target.Bytes()...)
	message = append(message, byte(p.Scope))
	if p.RequireConfirmation {
		message = append(message, 1)
	} else {
		message = append(message, 0)
	}
	return binary.BigEndian.AppendUint64(message, uint64(unixMilli(p.SignedAt)))
}

// ApplySetPeerPolicy applies the policy of a Personal peer: the flag of the
// local device for the issuer. It reports whether the flag changed.
//
// Every check fails closed: the policy has to target the local device, come from
// a stored non-revoked Personal peer, carry a valid signature and be newer than
// the last policy applied from the same issuer.
func (s *Store) ApplySetPeerPolicy(ctx context.Context, local, from model.DeviceID, policy SetPeerPolicy) (bool, error) {
	if local.IsZero() || from.IsZero() {
		return false, fmt.Errorf("%w: empty device id", ErrInvalidPeer)
	}
	if err := policy.Validate(); err != nil {
		return false, err
	}
	if policy.Target != local {
		return false, fmt.Errorf("%w: target %s is not this device", ErrForeignPolicy, policy.Target)
	}

	changed := false
	err := s.db.WithTx(ctx, func(tx *sql.Tx) error {
		issuer, err := readPeer(ctx, tx, s.db.Sealer(), peerByIDQuery, from.Bytes())
		if err != nil {
			return fmt.Errorf("%w: issuer %s: %w", ErrInvalidPeer, from, err)
		}
		if issuer.Revoked {
			return fmt.Errorf("%w: issuer %s is revoked", ErrInvalidPeer, from)
		}
		if issuer.Role != model.RolePersonal {
			return fmt.Errorf("%w: issuer %s is not personal", ErrInvalidPeer, from)
		}
		settings := store.NewSettings(tx, s.db.Sealer())
		allowed, err := settings.GetBool(ctx, allowRemoteSettingsKey, true)
		if err != nil {
			return err
		}
		if !allowed || issuer.RemoteSettingsDenied {
			return fmt.Errorf("%w: issuer %s", ErrRemoteSettingsDenied, from)
		}
		if err := checkNotAhead(policy.SignedAt); err != nil {
			return err
		}
		if err := policy.Verify(issuer.Public); err != nil {
			return err
		}

		tsKey := policyTsKeyPrefix + from.String()
		applied, err := settings.GetInt(ctx, tsKey, 0)
		if err != nil {
			return err
		}
		if unixMilli(policy.SignedAt) <= applied {
			return fmt.Errorf("%w: signed at %d, applied at %d", ErrPolicyReplay, unixMilli(policy.SignedAt), applied)
		}
		if err := settings.SetInt(ctx, tsKey, unixMilli(policy.SignedAt)); err != nil {
			return err
		}
		if issuer.Confirm.Get(policy.Scope) == policy.RequireConfirmation {
			return nil
		}
		issuer.Confirm = issuer.Confirm.With(policy.Scope, policy.RequireConfirmation)
		changed = true
		return s.writeProfile(ctx, tx, issuer)
	})
	if err != nil {
		return false, err
	}
	return changed, nil
}
