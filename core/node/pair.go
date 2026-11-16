package node

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"time"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/pairing"
	"github.com/lebe-dev/unison/core/ports"
)

// PairingCandidate is a device that is open for pairing on the local network.
type PairingCandidate struct {
	// Addr is the address to pass to StartPairing.
	Addr string
	// TXT is the service metadata of the candidate.
	TXT map[string]string
	// FirstSeen is when the candidate was discovered.
	FirstSeen time.Time
}

// pairingWindow is the open pairing window of the node.
type pairingWindow struct {
	window     *pairing.Window
	ctx        context.Context
	cancel     context.CancelFunc
	candidates map[string]PairingCandidate
}

// OpenPairing opens a pairing window that proposes role to the devices it
// pairs with. The window closes on timeout, on ClosePairing or when the node
// stops.
func (n *Node) OpenPairing(role model.Role) error {
	if err := n.usable(); err != nil {
		return err
	}
	if role != model.RolePersonal && role != model.RoleFriends {
		return fmt.Errorf("node: role %s cannot be proposed", role)
	}

	n.mu.Lock()
	if n.pairing != nil {
		n.mu.Unlock()
		return ErrPairingOpen
	}
	ctx, cancel := context.WithCancel(n.ctx)
	pw := &pairingWindow{
		ctx:        ctx,
		cancel:     cancel,
		candidates: make(map[string]PairingCandidate),
	}
	n.pairing = pw
	n.mu.Unlock()

	window, err := pairing.Open(ctx, pairing.Options{
		Identity:     n.identity,
		Trust:        n.trust,
		Discovery:    pairingDiscovery{Discovery: n.cfg.Discovery},
		Name:         n.cfg.Name,
		Platform:     n.cfg.Platform,
		Role:         role,
		Clock:        n.clock,
		Logger:       n.log,
		ListenPacket: n.listenPairing,
		Config:       n.cfg.Transport,
	})
	if err != nil {
		cancel()
		n.mu.Lock()
		if n.pairing == pw {
			n.pairing = nil
		}
		n.mu.Unlock()
		return err
	}

	n.mu.Lock()
	pw.window = window
	n.mu.Unlock()

	n.spawn(func() { n.watchPairing(pw, window) })
	return nil
}

// ClosePairing closes the pairing window. It is safe to call when no window is
// open.
func (n *Node) ClosePairing() error {
	n.mu.Lock()
	pw := n.pairing
	n.pairing = nil
	n.mu.Unlock()

	if pw == nil {
		return nil
	}
	if pw.cancel != nil {
		pw.cancel()
	}
	if pw.window == nil {
		return nil
	}
	return pw.window.Close()
}

// PairingDeadline returns when the window closes and whether a window is open.
func (n *Node) PairingDeadline() (time.Time, bool) {
	pw := n.pairingWindow()
	if pw == nil {
		return time.Time{}, false
	}
	return pw.window.Deadline(), true
}

// PairingAddr returns the address of the pairing listener. It is nil when no
// window is open.
func (n *Node) PairingAddr() net.Addr {
	pw := n.pairingWindow()
	if pw == nil {
		return nil
	}
	return pw.window.Addr()
}

// ExtendPairing restarts the auto-close timer of the window.
func (n *Node) ExtendPairing() error {
	pw := n.pairingWindow()
	if pw == nil {
		return ErrPairingClosed
	}
	return pw.window.Extend()
}

// PairingQR returns the pairing code of the open window. addrs are the addresses
// of the pairing listener that the scanning device can reach; a listener bound to
// a wildcard address is replaced by the addresses of the local interfaces. The
// code carries a one time token, so it must not be logged or stored.
func (n *Node) PairingQR(addrs ...string) (pairing.QR, error) {
	pw := n.pairingWindow()
	if pw == nil {
		return pairing.QR{}, ErrPairingClosed
	}
	return pw.window.QR(addrs...)
}

// ScanPairingQR pairs this device with the device of a pairing code. The
// exchange runs in the background and reports through the events. The scanning
// device accepted the pair by reading the code, so only the device that showed
// the code asks its user.
func (n *Node) ScanPairingQR(uri string) error {
	if err := n.usable(); err != nil {
		return err
	}
	if _, err := pairing.ParseQR(uri); err != nil {
		return err
	}

	opts := pairing.Options{
		Identity:     n.identity,
		Trust:        n.trust,
		Name:         n.cfg.Name,
		Platform:     n.cfg.Platform,
		Clock:        n.clock,
		Logger:       n.log,
		ListenPacket: n.listenPairing,
		Config:       n.cfg.Transport,
	}
	n.spawn(func() {
		ctx, cancel := context.WithTimeout(n.ctx, pairing.DefaultRequestTimeout)
		defer cancel()
		err := pairing.Scan(ctx, opts, uri, n.pairingEvent)
		if err != nil && n.ctx.Err() == nil {
			n.log.Info("pairing code scan failed", "error", err)
		}
	})
	return nil
}

// PairingCandidates returns the discovered devices that are open for pairing.
func (n *Node) PairingCandidates() []PairingCandidate {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.pairing == nil {
		return nil
	}
	candidates := make([]PairingCandidate, 0, len(n.pairing.candidates))
	for _, candidate := range n.pairing.candidates {
		candidates = append(candidates, candidate)
	}
	slices.SortFunc(candidates, func(a, b PairingCandidate) int {
		return strings.Compare(a.Addr, b.Addr)
	})
	return candidates
}

// StartPairing starts a pairing exchange with the candidate address. The
// exchange runs in the background and reports through the events.
func (n *Node) StartPairing(addr string) error {
	if addr == "" {
		return errors.New("node: empty pairing address")
	}
	pw := n.pairingWindow()
	if pw == nil {
		return ErrPairingClosed
	}

	n.spawn(func() {
		if err := pw.window.Dial(pw.ctx, addr); err != nil && pw.ctx.Err() == nil {
			n.log.Info("pairing exchange failed", "addr", addr, "error", err)
		}
	})
	return nil
}

// ConfirmPairing accepts the request of the device.
func (n *Node) ConfirmPairing(ctx context.Context, id model.DeviceID) error {
	pw := n.pairingWindow()
	if pw == nil {
		return ErrPairingClosed
	}
	return pw.window.Confirm(ctx, id)
}

// RejectPairing refuses the request of the device.
func (n *Node) RejectPairing(ctx context.Context, id model.DeviceID) error {
	pw := n.pairingWindow()
	if pw == nil {
		return ErrPairingClosed
	}
	return pw.window.Reject(ctx, id)
}

// pairingWindow returns the open window, or nil.
func (n *Node) pairingWindow() *pairingWindow {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.pairing == nil || n.pairing.window == nil {
		return nil
	}
	return n.pairing
}

// watchPairing forwards the window events until the window closes.
func (n *Node) watchPairing(pw *pairingWindow, window *pairing.Window) {
	for event := range window.Events() {
		n.pairingWindowEvent(pw, event)
	}

	n.mu.Lock()
	if n.pairing == pw {
		n.pairing = nil
	}
	n.mu.Unlock()
	pw.cancel()
}

// pairingWindowEvent publishes one event of the open window.
func (n *Node) pairingWindowEvent(pw *pairingWindow, event pairing.Event) {
	switch event.Kind {
	case pairing.EventCandidate:
		n.rememberPairingCandidate(pw, event)
		n.publish(Event{Kind: EventPairingCandidate, Addr: event.Addr})
	case pairing.EventClosed:
		n.publish(Event{Kind: EventPairingClosed})
	default:
		n.pairingEvent(event)
	}
}

// pairingEvent publishes one event of a pairing exchange.
func (n *Node) pairingEvent(event pairing.Event) {
	switch event.Kind {
	case pairing.EventRequest:
		n.log.Info("pairing request", "peer", event.DeviceID, "role", event.Role.String(),
			"method", event.Method)
		n.publish(Event{
			Kind:     EventPairingRequest,
			Peer:     event.DeviceID,
			Name:     event.Name,
			Platform: event.Platform,
			Role:     event.Role,
			SAS:      event.SAS,
			Method:   event.Method,
		})
	case pairing.EventPaired:
		n.log.Info("pairing completed", "peer", event.DeviceID, "role", event.Role.String(),
			"method", event.Method)
		n.publish(Event{
			Kind:     EventPairingPaired,
			Peer:     event.DeviceID,
			Name:     event.Name,
			Platform: event.Platform,
			Role:     event.Role,
			SAS:      event.SAS,
			Method:   event.Method,
		})
		n.syncLinks()
		if n.gossip != nil {
			n.gossip.Notify(n.ctx)
		}
	case pairing.EventFailed:
		n.log.Info("pairing exchange failed", "peer", event.DeviceID,
			"method", event.Method, "error", event.Err)
		n.publish(Event{
			Kind:     EventPairingFailed,
			Peer:     event.DeviceID,
			Name:     event.Name,
			Platform: event.Platform,
			Role:     event.Role,
			Method:   event.Method,
			Err:      event.Err,
		})
	}
}

// syncLinks starts the connection loops of every stored device and retries the
// discovered addresses. It runs after a pair, which may have added the devices
// of the group.
func (n *Node) syncLinks() {
	for _, peer := range n.pairedPeers(n.ctx) {
		n.startLink(peer).retry()
	}
	n.probeCandidates(n.ctx)
}

// rememberPairingCandidate caches a discovered pairing candidate.
func (n *Node) rememberPairingCandidate(pw *pairingWindow, event pairing.Event) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.pairing != pw || event.Addr == "" {
		return
	}
	if _, ok := pw.candidates[event.Addr]; !ok && len(pw.candidates) >= maxCandidates {
		oldest := ""
		var seen time.Time
		for addr, candidate := range pw.candidates {
			if oldest == "" || candidate.FirstSeen.Before(seen) {
				oldest, seen = addr, candidate.FirstSeen
			}
		}
		delete(pw.candidates, oldest)
	}
	pw.candidates[event.Addr] = PairingCandidate{
		Addr:      event.Addr,
		TXT:       event.TXT,
		FirstSeen: n.clock.Now(),
	}
}

// listenPairing binds a packet connection for a pairing exchange.
func (n *Node) listenPairing(ctx context.Context) (net.PacketConn, error) {
	return n.cfg.ListenPacket(ctx, 0)
}

// serviceStopper is a discovery that withdraws one announcement.
type serviceStopper interface {
	StopService(service ports.Service) error
}

// pairingDiscovery adapts the injected discovery to the pairing window, which
// withdraws its announcement when it closes.
type pairingDiscovery struct {
	ports.Discovery
}

// StopService withdraws the pairing announcement. A discovery without the call
// keeps the announcement until it stops.
func (d pairingDiscovery) StopService(service ports.Service) error {
	stopper, ok := d.Discovery.(serviceStopper)
	if !ok {
		return nil
	}
	return stopper.StopService(service)
}
