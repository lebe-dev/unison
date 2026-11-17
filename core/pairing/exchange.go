package pairing

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/quic-go/quic-go"
	"lukechampine.com/blake3"

	"github.com/lebe-dev/unison/core/identity"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/model/protoconv"
	"github.com/lebe-dev/unison/core/transport"
	"github.com/lebe-dev/unison/core/trust"
	"github.com/lebe-dev/unison/internal/observability"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// Constants of the pairing exchange.
const (
	// rendezvousLabel is the hash prefix of the relay rendezvous key.
	rendezvousLabel = "unison/1 rendezvous"
	// keySize is the length of a discovery or rendezvous key in bytes.
	keySize = 32
	// maxNameLength bounds the display name of a device in bytes.
	maxNameLength = 128
	// maxIntroductions bounds the introductions of one pairing exchange.
	maxIntroductions = 64
	// closeTimeout bounds the wait for the peer to close the stream.
	closeTimeout = 5 * time.Second
)

// Signature types and layout of the messages the QR flow signs.
const (
	// qrRequestType is the signature type of a QR pairing request.
	qrRequestType = "pair-qr-request"
	// qrAcceptType is the signature type of the accept of a QR pairing.
	qrAcceptType = "pair-qr-accept"
	// qrMessageVersion is the layout version of the signed QR messages.
	qrMessageVersion byte = 1
)

// flow is the way an exchange is started.
type flow struct {
	// method is the flow of the exchange.
	method Method
	// token is the token of the QR code, QR only.
	token Token
	// pin is the certificate fingerprint of the QR code, QR only.
	pin identity.CertFingerprint
	// id is the device of the QR code, QR only.
	id model.DeviceID
	// claimed reports that the window holds the token of this exchange.
	claimed bool
}

// checkConnIdentity binds the identity a message claims to the certificate of
// the connection. Without it any device could present the identity of another
// one, and the SAS would be computed over keys that the connection does not
// authenticate.
func checkConnIdentity(conn transport.PairingPeer, peer peerInfo) error {
	if conn.DeviceID != peer.id {
		return fmt.Errorf("%w: identity %s, certificate of %s", ErrInvalidPeer, peer.id, conn.DeviceID)
	}
	return nil
}

// checkScan binds the answer of a QR exchange to the code: the certificate of the
// connection and the identity of the answer belong to the device that showed the
// code.
func (fl flow) checkScan(conn transport.PairingPeer, peer peerInfo) error {
	switch {
	case conn.CertFingerprint != fl.pin:
		return fmt.Errorf("%w: certificate fingerprint mismatch", ErrInvalidPeer)
	case peer.id != fl.id:
		return fmt.Errorf("%w: identity of %s, code of %s", ErrInvalidPeer, peer.id, fl.id)
	}
	return nil
}

// peerInfo is the remote device of an exchange.
type peerInfo struct {
	id       model.DeviceID
	public   identity.PublicIdentity
	name     string
	platform model.Platform
	role     model.Role
	sas      SAS
	ekm      []byte
}

// exchange is one pairing exchange in progress.
type exchange struct {
	mu       sync.Mutex
	decided  bool
	accepted bool
	decision chan bool
}

// decide records the decision of the local user. Repeating the same decision is
// a no-op, changing it is refused.
func (e *exchange) decide(accepted bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.decided {
		if e.accepted == accepted {
			return nil
		}
		return ErrDecided
	}
	e.decided = true
	e.accepted = accepted
	e.decision <- accepted
	return nil
}

// run performs one pairing exchange and reports its failure.
func (w *Window) run(ctx context.Context, conn *transport.PairingConn, initiator bool, fl flow) error {
	peer, err := w.exchange(ctx, conn, initiator, fl)
	if err == nil {
		return nil
	}
	w.reportFailure(peer, fl, err)
	return err
}

// exchange runs the pairing protocol on a connection. The initiator proposes the
// role and the responder confirms it. In the SAS flow both devices show the SAS
// and decide separately; in the QR flow the token of the code is the decision of
// the scanning device. The group data is exchanged only after both accepted.
func (w *Window) exchange(ctx context.Context, conn *transport.PairingConn, initiator bool, fl flow) (peer peerInfo, err error) {
	ctx, cancel := context.WithTimeout(ctx, w.opts.RequestTimeout)
	defer cancel()

	stream, err := openStream(ctx, conn, initiator)
	if err != nil {
		return peerInfo{}, err
	}
	teardown := &streamTeardown{stream: stream}
	defer teardown.finish()
	if deadline, ok := ctx.Deadline(); ok {
		if err := stream.SetDeadline(deadline); err != nil {
			return peerInfo{}, fmt.Errorf("pairing: set deadline: %w", err)
		}
	}
	reader := newFrameReader(stream)

	peer, fl, err = w.handshake(ctx, conn, reader, stream, initiator, fl)
	if fl.claimed {
		// A failed or refused exchange gives the token back, so the user can
		// scan the same code again.
		defer func() {
			if fl.claimed {
				w.releaseToken(fl.token, false)
			}
		}()
	}
	if err != nil {
		return peer, err
	}

	ex, err := w.register(peer)
	if err != nil {
		return peer, err
	}
	defer w.unregister(peer.id)

	// The QR flow shows no short code: the scanning device confirmed the pair
	// with the code of the other device.
	sas := peer.sas
	if fl.method != MethodSAS {
		sas = SAS{}
	}

	w.emit(Event{
		Kind:     EventRequest,
		Method:   fl.method,
		DeviceID: peer.id,
		Name:     peer.name,
		Platform: peer.platform,
		Role:     peer.role,
		SAS:      sas,
	})

	if err := w.awaitDecisions(ctx, reader, stream, ex, fl, initiator); err != nil {
		return peer, err
	}
	if err := w.accept(ctx, reader, stream, peer, initiator, fl); err != nil {
		return peer, err
	}
	// A window may close as soon as it reports the pair, and closing the window
	// closes the connection of the exchange. The peer has to hold the whole
	// accept frame before the event, so the stream ends first.
	teardown.finish()
	if fl.claimed {
		// The device that showed the code gets a fresh one before the UI hears
		// about the pair, so it never reads the spent code again.
		w.releaseToken(fl.token, true)
		fl.claimed = false
	}

	w.emit(Event{
		Kind:     EventPaired,
		Method:   fl.method,
		DeviceID: peer.id,
		Name:     peer.name,
		Platform: peer.platform,
		Role:     peer.role,
		SAS:      sas,
	})
	return peer, nil
}

// handshake exchanges the identities of the devices, checks the flow of the
// request and derives the SAS.
func (w *Window) handshake(ctx context.Context, conn *transport.PairingConn, reader *frameReader, stream *quic.Stream, initiator bool, fl flow) (peerInfo, flow, error) {
	if initiator {
		request, err := w.request(fl)
		if err != nil {
			return peerInfo{}, fl, err
		}
		if err := writeMessage(stream, request); err != nil {
			return peerInfo{}, fl, fmt.Errorf("pairing: send request: %w", err)
		}
		response := &wirev1.PairResponse{}
		if err := reader.readMessage(response); err != nil {
			if errors.Is(err, io.EOF) && fl.method == MethodQR {
				// The window refuses a code without saying why: the token may
				// be spent, the signature may be wrong or the role may differ.
				return peerInfo{}, fl, fmt.Errorf("pairing: the other device did not accept the code: %w", err)
			}
			return peerInfo{}, fl, fmt.Errorf("pairing: read response: %w", err)
		}
		peer, err := w.peerFromResponse(ctx, response)
		if err != nil {
			return peer, fl, err
		}
		if err := checkConnIdentity(conn.Peer(), peer); err != nil {
			return peer, fl, err
		}
		if fl.method == MethodQR {
			if err := fl.checkScan(conn.Peer(), peer); err != nil {
				return peer, fl, err
			}
		}
		peer, err = peer.withSAS(conn, w.opts.Identity.Public())
		return peer, fl, err
	}

	request := &wirev1.PairRequest{}
	if err := reader.readMessage(request); err != nil {
		return peerInfo{}, fl, fmt.Errorf("pairing: read request: %w", err)
	}
	peer, err := w.peerFromRequest(request)
	if err != nil {
		return peer, fl, err
	}
	if err := checkConnIdentity(conn.Peer(), peer); err != nil {
		return peer, fl, err
	}
	if len(request.Token) > 0 {
		fl, err = w.qrRequest(request, peer)
		if err != nil {
			return peer, fl, err
		}
	}
	if err := writeMessage(stream, w.response(peer)); err != nil {
		return peer, fl, fmt.Errorf("pairing: send response: %w", err)
	}
	// The identity is not secret and the initiator needs it to learn why the
	// exchange ends, so the trust checks follow the response.
	if err := w.checkPeer(ctx, peer); err != nil {
		// Say that the pair was refused instead of closing the stream under
		// the initiator.
		_ = writeMessage(stream, &wirev1.PairDecision{Accepted: false})
		return peer, fl, err
	}
	peer, err = peer.withSAS(conn, w.opts.Identity.Public())
	return peer, fl, err
}

// request returns the message that opens an exchange. It proposes the role the
// window is opened with and, in the QR flow, carries the token and the signature
// that binds the request to the code and to the device that showed it.
func (w *Window) request(fl flow) (*wirev1.PairRequest, error) {
	local := w.opts.Identity.Public()
	request := &wirev1.PairRequest{
		Ed25519Pub: local.Ed25519,
		MldsaPub:   local.MLDSABytes(),
		DeviceName: w.opts.Name,
		Platform:   protoconv.PlatformToProto(w.opts.Platform),
		Role:       protoconv.RoleToProto(w.opts.Role),
	}
	if fl.method != MethodQR {
		return request, nil
	}
	if fl.token.IsZero() || fl.pin.IsZero() {
		return nil, fmt.Errorf("%w: code without a token or a fingerprint", ErrQR)
	}
	sig, err := w.opts.Identity.Sign(qrRequestType,
		qrRequestMessage(fl.token, fl.pin, w.opts.Name, w.opts.Platform, w.opts.Role))
	if err != nil {
		return nil, fmt.Errorf("pairing: sign request: %w", err)
	}
	request.Token = fl.token[:]
	request.Ed25519Sig = sig.Ed25519
	request.MldsaSig = sig.MLDSA
	return request, nil
}

// response returns the reply to a request. The accepting device repeats the
// proposed role, which is the role both devices store.
func (w *Window) response(peer peerInfo) *wirev1.PairResponse {
	local := w.opts.Identity.Public()
	return &wirev1.PairResponse{
		Ed25519Pub: local.Ed25519,
		MldsaPub:   local.MLDSABytes(),
		DeviceName: w.opts.Name,
		Platform:   protoconv.PlatformToProto(w.opts.Platform),
		Role:       protoconv.RoleToProto(peer.role),
	}
}

// peerFromRequest validates a request of a device that wants to pair.
func (w *Window) peerFromRequest(request *wirev1.PairRequest) (peerInfo, error) {
	return w.peerInfo(request.Ed25519Pub, request.MldsaPub, request.DeviceName, request.Platform, request.Role)
}

// qrRequest checks the token and the signature of a request that carries the
// code of this window, and takes the token for the exchange. The signature
// covers the token, the fingerprint of the device that showed the code, the
// display name, the platform and the role, so a request cannot be moved to
// another window or replayed with other fields.
func (w *Window) qrRequest(request *wirev1.PairRequest, peer peerInfo) (flow, error) {
	qr := flow{method: MethodQR}
	if len(request.Token) != tokenSize {
		return qr, fmt.Errorf("%w: token of %d bytes", ErrToken, len(request.Token))
	}
	if peer.role != w.opts.Role {
		return qr, fmt.Errorf("%w: role %s, proposed %s", ErrRoleMismatch, peer.role, w.opts.Role)
	}
	var token Token
	copy(token[:], request.Token)

	message := qrRequestMessage(token, w.opts.Identity.CertFingerprint(), peer.name, peer.platform, peer.role)
	signature := identity.Signature{Ed25519: request.Ed25519Sig, MLDSA: request.MldsaSig}
	if err := peer.public.Verify(qrRequestType, message, signature); err != nil {
		return qr, fmt.Errorf("%w: pairing request signature: %w", ErrInvalidPeer, err)
	}
	if !w.claimToken(request.Token) {
		return qr, ErrToken
	}
	qr.token = token
	qr.claimed = true
	return qr, nil
}

// qrRequestMessage returns the message the scanning device signs.
func qrRequestMessage(token Token, target identity.CertFingerprint, name string, platform model.Platform, role model.Role) []byte {
	message := make([]byte, 0, 1+tokenSize+len(target)+2+binary.MaxVarintLen64+len(name))
	message = append(message, qrMessageVersion)
	message = append(message, token[:]...)
	message = append(message, target.Bytes()...)
	message = append(message, byte(platform), byte(role))
	message = binary.AppendUvarint(message, uint64(len(name)))
	return append(message, name...)
}

// qrAcceptMessage returns the message the device that showed the code signs.
func qrAcceptMessage(token Token, initiator model.DeviceID, role model.Role) []byte {
	message := make([]byte, 0, 1+tokenSize+len(initiator)+1)
	message = append(message, qrMessageVersion)
	message = append(message, token[:]...)
	message = append(message, initiator[:]...)
	return append(message, byte(role))
}

// peerFromResponse checks the reply of the device the exchange was sent to.
func (w *Window) peerFromResponse(ctx context.Context, response *wirev1.PairResponse) (peerInfo, error) {
	peer, err := w.peerInfo(response.Ed25519Pub, response.MldsaPub, response.DeviceName, response.Platform, response.Role)
	if err != nil {
		return peer, err
	}
	if peer.role != w.opts.Role {
		return peer, fmt.Errorf("%w: role %s, proposed %s", ErrInvalidPeer, peer.role, w.opts.Role)
	}
	return peer, w.checkPeer(ctx, peer)
}

// peerInfo validates the identity fields of a pairing message.
func (w *Window) peerInfo(edPub, mldsaPub []byte, name string, platform wirev1.Platform, role wirev1.Role) (peerInfo, error) {
	public, err := identity.NewPublicIdentity(edPub, mldsaPub)
	if err != nil {
		return peerInfo{}, fmt.Errorf("%w: %w", ErrInvalidPeer, err)
	}
	if name == "" || len(name) > maxNameLength || !utf8.ValidString(name) {
		return peerInfo{}, fmt.Errorf("%w: invalid device name", ErrInvalidPeer)
	}
	known := protoconv.PlatformFromProto(platform)
	if known == model.PlatformUnknown {
		return peerInfo{}, fmt.Errorf("%w: unknown platform", ErrInvalidPeer)
	}
	knownRole := protoconv.RoleFromProto(role)
	if knownRole != model.RolePersonal && knownRole != model.RoleFriends {
		return peerInfo{}, fmt.Errorf("%w: unknown role", ErrInvalidPeer)
	}
	return peerInfo{id: public.DeviceID, public: public, name: name, platform: known, role: knownRole}, nil
}

// checkPeer refuses the local device itself, a revoked device and a device that
// proposes another role than the stored one.
func (w *Window) checkPeer(ctx context.Context, peer peerInfo) error {
	if peer.id == w.opts.Identity.DeviceID() {
		return fmt.Errorf("%w: pairing with itself", ErrInvalidPeer)
	}
	stored, err := w.opts.Trust.Get(ctx, peer.id)
	switch {
	case err == nil:
		if stored.Revoked {
			return ErrPeerRevoked
		}
		if stored.Role != peer.role {
			return fmt.Errorf("%w: stored %s, proposed %s", ErrRoleMismatch, stored.Role, peer.role)
		}
	case errors.Is(err, trust.ErrPeerNotFound):
	default:
		return fmt.Errorf("pairing: read trust store: %w", err)
	}
	return nil
}

// withSAS derives the SAS of the session and keeps the exporter key material for
// the rendezvous key.
func (p peerInfo) withSAS(conn *transport.PairingConn, local identity.PublicIdentity) (peerInfo, error) {
	state := conn.ConnectionState().TLS
	ekm, err := state.ExportKeyingMaterial(sasLabel, nil, sasKeySize)
	if err != nil {
		return p, fmt.Errorf("pairing: export keying material: %w", err)
	}
	sas, err := computeSAS(ekm, local, p.public)
	if err != nil {
		return p, err
	}
	p.sas = sas
	p.ekm = ekm
	return p, nil
}

// register adds an exchange to the window.
func (w *Window) register(peer peerInfo) (*exchange, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil, ErrClosed
	}
	if _, ok := w.requests[peer.id]; ok {
		return nil, ErrPending
	}
	if len(w.requests) >= maxPending {
		return nil, ErrBusy
	}
	ex := &exchange{decision: make(chan bool, 1)}
	w.requests[peer.id] = ex
	return ex, nil
}

// unregister removes an exchange from the window.
func (w *Window) unregister(id model.DeviceID) {
	w.mu.Lock()
	delete(w.requests, id)
	w.mu.Unlock()
}

// awaitDecisions exchanges the local and the remote decision. The scanning
// device of a QR exchange accepted the pair by reading the code, so only the
// device that shows the code waits for its user. No group data is sent before
// both devices accepted.
func (w *Window) awaitDecisions(ctx context.Context, reader *frameReader, stream *quic.Stream, ex *exchange, fl flow, initiator bool) error {
	type answer struct {
		accepted bool
		err      error
	}
	answers := make(chan answer, 1)
	go func() {
		defer observability.Recover()
		message := &wirev1.PairDecision{}
		err := reader.readMessage(message)
		answers <- answer{accepted: message.Accepted, err: err}
	}()

	local := fl.method == MethodQR && initiator
	if local {
		if err := writeMessage(stream, &wirev1.PairDecision{Accepted: true}); err != nil {
			return fmt.Errorf("pairing: send decision: %w", err)
		}
	}
	var remote bool
	for !local || !remote {
		select {
		case accepted := <-ex.decision:
			local = true
			if err := writeMessage(stream, &wirev1.PairDecision{Accepted: accepted}); err != nil {
				return fmt.Errorf("pairing: send decision: %w", err)
			}
			if !accepted {
				return ErrRejected
			}
		case decision := <-answers:
			remote = true
			if decision.err != nil {
				return fmt.Errorf("pairing: read decision: %w", decision.err)
			}
			if !decision.accepted {
				return ErrRejected
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// accept exchanges the group data of the pair and writes the peer to the trust
// store. Both devices agreed to the pair before it runs.
func (w *Window) accept(ctx context.Context, reader *frameReader, stream *quic.Stream, peer peerInfo, initiator bool, fl flow) error {
	if initiator {
		return w.acceptResponse(ctx, reader, peer, fl)
	}
	return w.sendAccept(ctx, stream, peer, fl)
}

// sendAccept stores the peer, then sends the group data to the device that
// started the exchange.
func (w *Window) sendAccept(ctx context.Context, stream *quic.Stream, peer peerInfo, fl flow) error {
	discoveryKey, err := w.discoveryKey(ctx, peer.role)
	if err != nil {
		return err
	}
	rendezvous := rendezvousKey(peer.ekm)
	if err := w.recordPeer(ctx, peer, discoveryKey, rendezvous); err != nil {
		return err
	}
	introductions, err := w.introductions(ctx, peer)
	if err != nil {
		return err
	}

	local := w.opts.Identity.Public()
	message := &wirev1.PairAccept{
		Ed25519Pub:    local.Ed25519,
		MldsaPub:      local.MLDSABytes(),
		DeviceName:    w.opts.Name,
		Role:          protoconv.RoleToProto(peer.role),
		Platform:      protoconv.PlatformToProto(w.opts.Platform),
		Introductions: introductions,
		DiscoveryKey:  discoveryKey,
		RendezvousKey: rendezvous,
	}
	if fl.method == MethodQR {
		signature, err := w.opts.Identity.Sign(qrAcceptType, qrAcceptMessage(fl.token, peer.id, peer.role))
		if err != nil {
			return fmt.Errorf("pairing: sign accept: %w", err)
		}
		message.Ed25519Sig = signature.Ed25519
		message.MldsaSig = signature.MLDSA
	}
	if err := writeMessage(stream, message); err != nil {
		return fmt.Errorf("pairing: send accept: %w", err)
	}
	return nil
}

// introductions signs the introductions of the known Personal peers for the
// device that started the exchange and records the introduction of that device,
// so the rest of the group learns about it over gossip. A Friends pair exchanges
// no introductions.
func (w *Window) introductions(ctx context.Context, peer peerInfo) ([]*wirev1.Introduction, error) {
	if peer.role != model.RolePersonal {
		return nil, nil
	}
	now := w.clock.Now()

	local, err := trust.NewIntroduction(w.opts.Identity, trust.IntroductionInfo{
		Subject:  peer.public,
		Name:     peer.name,
		Platform: peer.platform,
	}, now)
	if err != nil {
		return nil, err
	}
	if _, err := w.opts.Trust.AddIntroduction(ctx, local); err != nil {
		return nil, fmt.Errorf("pairing: record introduction: %w", err)
	}

	known, err := w.opts.Trust.List(ctx, trust.ListFilter{Role: model.RolePersonal})
	if err != nil {
		return nil, fmt.Errorf("pairing: list peers: %w", err)
	}
	introductions := make([]*wirev1.Introduction, 0, min(len(known), maxIntroductions))
	for _, stored := range known {
		if stored.DeviceID() == peer.id {
			continue
		}
		intro, err := trust.NewIntroduction(w.opts.Identity, trust.IntroductionInfo{
			Subject:  stored.Public,
			Name:     stored.Name,
			Platform: stored.Platform,
		}, now)
		if err != nil {
			return nil, err
		}
		introductions = append(introductions, intro.Proto())
		if len(introductions) == maxIntroductions {
			break
		}
	}
	return introductions, nil
}

// acceptResponse reads the group data of the accepting device and stores the
// peer.
func (w *Window) acceptResponse(ctx context.Context, reader *frameReader, peer peerInfo, fl flow) error {
	message := &wirev1.PairAccept{}
	if err := reader.readMessage(message); err != nil {
		return fmt.Errorf("pairing: read accept: %w", err)
	}
	if err := checkAccept(peer, message); err != nil {
		return err
	}
	if fl.method == MethodQR {
		if err := w.checkAcceptSignature(peer, message, fl.token); err != nil {
			return err
		}
	}
	if err := w.recordPeer(ctx, peer, message.DiscoveryKey, rendezvousKey(peer.ekm)); err != nil {
		return err
	}
	w.applyIntroductions(ctx, peer, message.Introductions)
	return nil
}

// checkAcceptSignature verifies that the device that showed the code accepted
// this device with this role and with the token of its code.
func (w *Window) checkAcceptSignature(peer peerInfo, message *wirev1.PairAccept, token Token) error {
	signature := identity.Signature{Ed25519: message.Ed25519Sig, MLDSA: message.MldsaSig}
	accept := qrAcceptMessage(token, w.opts.Identity.DeviceID(), peer.role)
	if err := peer.public.Verify(qrAcceptType, accept, signature); err != nil {
		return fmt.Errorf("%w: accept signature: %w", ErrInvalidPeer, err)
	}
	return nil
}

// applyIntroductions stores the introductions of the accepting device. Only
// records signed by the device of the exchange are accepted; the others are
// dropped, because nothing vouches for them yet.
func (w *Window) applyIntroductions(ctx context.Context, peer peerInfo, records []*wirev1.Introduction) {
	if peer.role != model.RolePersonal || len(records) > maxIntroductions {
		if len(records) > 0 && w.log != nil {
			w.log.Info("pairing: introductions dropped", "peer", peer.id, "count", len(records))
		}
		return
	}
	for _, record := range records {
		intro, err := trust.IntroductionFromProto(record)
		if err != nil {
			w.logIntroductionRefused(peer, err)
			continue
		}
		if intro.Issuer != peer.id {
			w.logIntroductionRefused(peer, fmt.Errorf("%w: introduction by %s", ErrInvalidPeer, intro.Issuer))
			continue
		}
		if intro.DeviceID() == peer.id || intro.DeviceID() == w.opts.Identity.DeviceID() {
			w.logIntroductionRefused(peer, fmt.Errorf("%w: introduction of a device of the exchange", ErrInvalidPeer))
			continue
		}
		if _, err := w.opts.Trust.ApplyIntroduction(ctx, intro); err != nil {
			w.logIntroductionRefused(peer, err)
		}
	}
}

// logIntroductionRefused reports an introduction that was not applied.
func (w *Window) logIntroductionRefused(peer peerInfo, err error) {
	if w.log == nil {
		return
	}
	w.log.Info("pairing: introduction refused", "peer", peer.id, "error", err)
}

// checkAccept verifies that the accepting device repeats the identity it
// exchanged and derives the same rendezvous key.
func checkAccept(peer peerInfo, message *wirev1.PairAccept) error {
	if !bytes.Equal(message.Ed25519Pub, peer.public.Ed25519) || !bytes.Equal(message.MldsaPub, peer.public.MLDSABytes()) {
		return fmt.Errorf("%w: accepting device sent another identity", ErrInvalidPeer)
	}
	if role := protoconv.RoleFromProto(message.Role); role != peer.role {
		return fmt.Errorf("%w: role %s, proposed %s", ErrInvalidPeer, role, peer.role)
	}
	if !bytes.Equal(message.RendezvousKey, rendezvousKey(peer.ekm)) {
		return fmt.Errorf("%w: rendezvous key mismatch", ErrInvalidPeer)
	}
	if peer.role == model.RolePersonal {
		if len(message.DiscoveryKey) != keySize {
			return fmt.Errorf("%w: discovery key of %d bytes", ErrInvalidPeer, len(message.DiscoveryKey))
		}
		return nil
	}
	if len(message.DiscoveryKey) != 0 {
		return fmt.Errorf("%w: group key for the %s role", ErrInvalidPeer, peer.role)
	}
	return nil
}

// recordPeer writes the peer and the group secrets of a completed pairing. A
// device that is already paired keeps its record, only the display name is
// updated. Another role is refused: the role of a paired device changes only
// through revocation.
func (w *Window) recordPeer(ctx context.Context, peer peerInfo, discoveryKey, rendezvous []byte) error {
	stored, err := w.opts.Trust.Get(ctx, peer.id)
	switch {
	case err == nil:
		if stored.Revoked {
			return ErrPeerRevoked
		}
		if stored.Role != peer.role {
			return fmt.Errorf("%w: stored %s, proposed %s", ErrRoleMismatch, stored.Role, peer.role)
		}
		if stored.Name != peer.name {
			if err := w.opts.Trust.Rename(ctx, peer.id, peer.name); err != nil {
				return fmt.Errorf("pairing: update peer name: %w", err)
			}
		}
	case errors.Is(err, trust.ErrPeerNotFound):
		err := w.opts.Trust.Add(ctx, trust.Peer{
			Public:   peer.public,
			Name:     peer.name,
			Platform: peer.platform,
			Role:     peer.role,
		})
		if err != nil {
			return fmt.Errorf("pairing: add peer: %w", err)
		}
	default:
		return fmt.Errorf("pairing: read trust store: %w", err)
	}

	if len(discoveryKey) > 0 {
		if err := w.opts.Trust.SetDiscoveryKey(ctx, discoveryKey); err != nil {
			return fmt.Errorf("pairing: store discovery key: %w", err)
		}
	}
	if err := w.opts.Trust.SetRendezvousKey(ctx, peer.id, rendezvous); err != nil {
		return fmt.Errorf("pairing: store rendezvous key: %w", err)
	}
	return nil
}

// discoveryKey returns the key of the Personal group to share with the peer. A
// Friends device receives none.
func (w *Window) discoveryKey(ctx context.Context, role model.Role) ([]byte, error) {
	if role != model.RolePersonal {
		return nil, nil
	}
	key, err := w.opts.Trust.EnsureDiscoveryKey(ctx)
	if err != nil {
		return nil, fmt.Errorf("pairing: read discovery key: %w", err)
	}
	return key, nil
}

// rendezvousKey derives the relay rendezvous key of the pair from the exporter
// key material of the pairing session.
func rendezvousKey(ekm []byte) []byte {
	hash := blake3.New(keySize, nil)
	_, _ = hash.Write([]byte(rendezvousLabel))
	_, _ = hash.Write(ekm)
	return hash.Sum(nil)
}

// openStream opens the pairing stream. The initiator opens it, the responder
// accepts it.
func openStream(ctx context.Context, conn *transport.PairingConn, initiator bool) (*quic.Stream, error) {
	if initiator {
		stream, err := conn.OpenStream(ctx)
		if err != nil {
			return nil, fmt.Errorf("pairing: open stream: %w", err)
		}
		return stream, nil
	}
	stream, err := conn.AcceptStream(ctx)
	if err != nil {
		return nil, fmt.Errorf("pairing: accept stream: %w", err)
	}
	return stream, nil
}

// streamTeardown ends the stream of an exchange once. It closes the send side
// and waits for the peer to close its own, so that the last frame is delivered
// before the connection is closed.
type streamTeardown struct {
	stream *quic.Stream
	once   sync.Once
}

// finish ends the send side of the stream and waits for the peer to end its own.
// A peer that stays silent is not waited for longer than closeTimeout.
func (t *streamTeardown) finish() {
	t.once.Do(func() {
		_ = t.stream.Close()
		_ = t.stream.SetReadDeadline(time.Now().Add(closeTimeout))
		_, _ = io.Copy(io.Discard, t.stream)
	})
}

// reportFailure reports an exchange that ended without a new pair.
func (w *Window) reportFailure(peer peerInfo, fl flow, err error) {
	if w.ctx.Err() != nil {
		return
	}
	if w.log != nil {
		w.log.Info("pairing exchange failed", "device", peer.id, "method", fl.method, "error", err)
	}
	w.emit(Event{
		Kind:     EventFailed,
		Method:   fl.method,
		DeviceID: peer.id,
		Name:     peer.name,
		Platform: peer.platform,
		Role:     peer.role,
		Err:      err,
	})
}
