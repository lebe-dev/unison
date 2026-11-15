package session

import (
	"fmt"

	"github.com/lebe-dev/unison/core/trust"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// Kind identifies the payload of a control message.
type Kind uint8

// Control message kinds, one per variant of ControlMessage.
const (
	// KindUnknown is the zero value and marks a message without a payload.
	KindUnknown Kind = iota
	// KindHello is the session handshake.
	KindHello
	// KindAuthProof is the authentication proof of the transport.
	KindAuthProof
	// KindClipAnnounce announces a clipboard item.
	KindClipAnnounce
	// KindHistorySync announces a history version vector.
	KindHistorySync
	// KindHistoryBatch carries history entries.
	KindHistoryBatch
	// KindHaveBlob asks whether the peer stores a blob.
	KindHaveBlob
	// KindHaveBlobResp answers a blob query.
	KindHaveBlobResp
	// KindShareOffer offers files to the peer.
	KindShareOffer
	// KindShareDecision answers a share offer.
	KindShareDecision
	// KindIntroduction introduces another device.
	KindIntroduction
	// KindRevocation revokes a device.
	KindRevocation
	// KindSetPeerPolicy changes the clipboard policy of a device.
	KindSetPeerPolicy
	// KindGossipDigest reports the trust state of a device.
	KindGossipDigest
	// KindGossipRecords carries the trust data a peer lacks.
	KindGossipRecords
	// KindShareCancel stops a share transfer.
	KindShareCancel
	// KindShareResult reports the final outcome of a share transfer.
	KindShareResult
)

// String returns the name of the kind.
func (k Kind) String() string {
	switch k {
	case KindHello:
		return "hello"
	case KindAuthProof:
		return "auth-proof"
	case KindClipAnnounce:
		return "clip-announce"
	case KindHistorySync:
		return "history-sync"
	case KindHistoryBatch:
		return "history-batch"
	case KindHaveBlob:
		return "have-blob"
	case KindHaveBlobResp:
		return "have-blob-resp"
	case KindShareOffer:
		return "share-offer"
	case KindShareDecision:
		return "share-decision"
	case KindIntroduction:
		return "introduction"
	case KindRevocation:
		return "revocation"
	case KindSetPeerPolicy:
		return "set-peer-policy"
	case KindGossipDigest:
		return "gossip-digest"
	case KindGossipRecords:
		return "gossip-records"
	case KindShareCancel:
		return "share-cancel"
	case KindShareResult:
		return "share-result"
	default:
		return "unknown"
	}
}

// kindOf returns the kind of the message payload. An empty or unknown payload is
// an error: the session drops such a message instead of guessing.
func kindOf(msg *wirev1.ControlMessage) (Kind, error) {
	if msg == nil {
		return KindUnknown, fmt.Errorf("%w: empty message", ErrUnknownKind)
	}
	switch msg.GetBody().(type) {
	case *wirev1.ControlMessage_Hello:
		return KindHello, nil
	case *wirev1.ControlMessage_AuthProof:
		return KindAuthProof, nil
	case *wirev1.ControlMessage_ClipAnnounce:
		return KindClipAnnounce, nil
	case *wirev1.ControlMessage_HistorySync:
		return KindHistorySync, nil
	case *wirev1.ControlMessage_HistoryBatch:
		return KindHistoryBatch, nil
	case *wirev1.ControlMessage_HaveBlob:
		return KindHaveBlob, nil
	case *wirev1.ControlMessage_HaveBlobResp:
		return KindHaveBlobResp, nil
	case *wirev1.ControlMessage_ShareOffer:
		return KindShareOffer, nil
	case *wirev1.ControlMessage_ShareDecision:
		return KindShareDecision, nil
	case *wirev1.ControlMessage_Introduction:
		return KindIntroduction, nil
	case *wirev1.ControlMessage_Revocation:
		return KindRevocation, nil
	case *wirev1.ControlMessage_SetPeerPolicy:
		return KindSetPeerPolicy, nil
	case *wirev1.ControlMessage_GossipDigest:
		return KindGossipDigest, nil
	case *wirev1.ControlMessage_GossipRecords:
		return KindGossipRecords, nil
	case *wirev1.ControlMessage_ShareCancel:
		return KindShareCancel, nil
	case *wirev1.ControlMessage_ShareResult:
		return KindShareResult, nil
	default:
		return KindUnknown, fmt.Errorf("%w: payload %T", ErrUnknownKind, msg.GetBody())
	}
}

// actionOf returns the capability a message needs. A kind that is missing here
// is refused: a new message kind has to state its capability explicitly.
//
// The share messages return ActionNone on purpose. They belong to an exchange
// started by an offer, and the share service authorizes them against the offer
// it remembers: an offer itself needs the receive-share capability, which a
// Friends device has only after the user confirms it. ShareDecision and
// ShareResult answer an offer of the local device, so they grant the sender no
// capability of its own.
func actionOf(kind Kind) (trust.Action, error) {
	switch kind {
	case KindClipAnnounce:
		return trust.ActionReceiveClipAnnounce, nil
	case KindHistorySync, KindHistoryBatch:
		return trust.ActionSyncHistory, nil
	case KindHaveBlob, KindHaveBlobResp:
		return trust.ActionFetchBlob, nil
	case KindShareOffer, KindShareDecision, KindShareCancel, KindShareResult:
		return trust.ActionNone, nil
	case KindIntroduction:
		return trust.ActionIntroduce, nil
	case KindRevocation:
		return trust.ActionRevoke, nil
	case KindSetPeerPolicy:
		return trust.ActionSetRemotePolicy, nil
	case KindGossipDigest, KindGossipRecords:
		return trust.ActionIntroduce, nil
	default:
		return trust.ActionNone, fmt.Errorf("%w: kind %s", ErrUnknownKind, kind)
	}
}
