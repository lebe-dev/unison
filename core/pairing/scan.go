package pairing

import (
	"context"
	"errors"
	"fmt"
	"net"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/core/transport"
)

// Scan pairs this device with the device of a pairing code. The scanning device
// connects to an address of the code, accepts only the certificate the code
// pins, and sends the token with a signed request. The showing device asks its
// user to confirm the pair; the scanning device accepted it by reading the code.
//
// Only the identity, trust store, name, platform, clock, logger, request timeout
// and transport settings of opts are used, and the role of the code replaces
// opts.Role. The exchange runs without a window of its own, so nothing is
// announced on the local network. onEvent receives the events of the exchange
// and may be nil.
func Scan(ctx context.Context, opts Options, uri string, onEvent func(Event)) error {
	code, err := ParseQR(uri)
	if err != nil {
		return err
	}
	switch {
	case opts.Identity == nil:
		return errors.New("pairing: nil identity")
	case opts.Trust == nil:
		return errors.New("pairing: nil trust store")
	case opts.Name == "":
		return errors.New("pairing: empty device name")
	}
	opts.Role = code.Role
	if opts.Clock == nil {
		opts.Clock = ports.SystemClock
	}
	if opts.ListenPacket == nil {
		opts.ListenPacket = listenUDP
	}
	if opts.RequestTimeout <= 0 {
		opts.RequestTimeout = DefaultRequestTimeout
	}

	w := &Window{
		opts:     opts,
		clock:    opts.Clock,
		log:      opts.Logger,
		requests: make(map[model.DeviceID]*exchange),
		conns:    make(map[*transport.PairingConn]struct{}),
		closedCh: make(chan struct{}),
	}
	if onEvent != nil {
		w.sink = onEvent
	}
	w.ctx, w.cancel = context.WithCancel(ctx)
	defer w.closeScan()

	return w.scan(w.ctx, code)
}

// scan connects to the device of the code and runs one exchange. A failure to
// reach the device is reported like a failed exchange, so the screen that waits
// for the result learns about it.
func (w *Window) scan(ctx context.Context, code QR) error {
	fl := flow{
		method: MethodQR,
		token:  code.Token,
		pin:    code.Fingerprint,
		id:     code.DeviceID,
	}
	conn, dialer, err := w.connectCode(ctx, code)
	if err != nil {
		w.reportFailure(peerInfo{id: code.DeviceID}, fl, err)
		return err
	}
	defer func() { _ = dialer.Close() }()
	defer w.untrack(conn)
	defer func() { _ = conn.Close() }()

	return w.run(ctx, conn, true, fl)
}

// connectCode opens a tracked connection to the device of the code. The caller
// closes the connection and the dialer.
func (w *Window) connectCode(ctx context.Context, code QR) (*transport.PairingConn, *transport.Dialer, error) {
	pc, err := w.opts.ListenPacket(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("pairing: listen: %w", err)
	}
	// The dialer owns the packet connection and closes it.
	dialer, err := transport.NewDialer(pc, w.opts.Identity, unpairedVerifier{}, w.opts.Config)
	if err != nil {
		_ = pc.Close()
		return nil, nil, err
	}

	conn, err := w.dialCode(ctx, dialer, code)
	if err != nil {
		_ = dialer.Close()
		return nil, nil, err
	}
	if err := w.track(conn); err != nil {
		_ = conn.Close()
		_ = dialer.Close()
		return nil, nil, err
	}
	return conn, dialer, nil
}

// dialCode connects to the first address of the code that answers.
func (w *Window) dialCode(ctx context.Context, dialer *transport.Dialer, code QR) (*transport.PairingConn, error) {
	timeout := w.opts.Config.HandshakeTimeout
	if timeout <= 0 {
		timeout = transport.HandshakeTimeout
	}

	var failures []error
	for _, addr := range code.Addrs {
		target, err := net.ResolveUDPAddr("udp", addr)
		if err != nil {
			failures = append(failures, fmt.Errorf("pairing: address %q: %w", addr, err))
			continue
		}
		attempt, cancel := context.WithTimeout(ctx, timeout)
		conn, err := dialer.DialPairing(attempt, target, code.Fingerprint)
		cancel()
		if err == nil {
			return conn, nil
		}
		failures = append(failures, err)
		if ctx.Err() != nil {
			break
		}
	}
	return nil, fmt.Errorf("pairing: connect to the device of the code: %w", errors.Join(failures...))
}

// closeScan releases a window that has no listener or announcement.
func (w *Window) closeScan() {
	w.mu.Lock()
	already := w.closed
	w.closed = true
	w.mu.Unlock()
	if already {
		return
	}
	w.cancel()
	close(w.closedCh)
}
