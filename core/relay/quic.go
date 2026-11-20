package relay

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"sync/atomic"
	"time"

	"github.com/quic-go/quic-go"
)

// Timeouts of the relay connection itself.
const (
	// quicKeepAlive keeps the connection alive on an idle relay.
	quicKeepAlive = 15 * time.Second
	// quicIdleTimeout closes a connection whose peer stays silent.
	quicIdleTimeout = 60 * time.Second
	// registerTimeout bounds the registration exchange.
	registerTimeout = 10 * time.Second
	// streamBufferSize is the read buffer of the registration stream.
	streamBufferSize = 512
)

// quicCarrier is the QUIC transport to the relay. Datagrams are QUIC DATAGRAM
// frames and the registration runs on the first stream.
type quicCarrier struct {
	conn    *quic.Conn
	timeout time.Duration
	closed  atomic.Bool
}

// quicCarrier implements carrier.
var _ carrier = (*quicCarrier)(nil)

// dialQUIC opens the QUIC connection to the relay.
func (d *Dialer) dialQUIC(ctx context.Context) (carrier, error) {
	cfg := &quic.Config{
		EnableDatagrams:       true,
		KeepAlivePeriod:       quicKeepAlive,
		MaxIdleTimeout:        quicIdleTimeout,
		HandshakeIdleTimeout:  d.cfg.DialTimeout,
		InitialPacketSize:     outerPacketSize,
		MaxIncomingStreams:    4,
		MaxIncomingUniStreams: -1,
	}
	tlsConfig := d.cfg.tlsConfig(d.eps.serverName)
	tlsConfig.NextProtos = []string{ALPN}

	conn, err := quic.DialAddr(ctx, d.eps.quic, tlsConfig, cfg)
	if err != nil {
		return nil, fmt.Errorf("relay: dial %s: %w", d.eps.quic, err)
	}
	return &quicCarrier{conn: conn, timeout: registerTimeout}, nil
}

// register joins the mailboxes and reads the answer of the relay.
func (c *quicCarrier) register(ctx context.Context, token string, ids []ID) (registration, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	stream, err := c.conn.OpenStreamSync(ctx)
	if err != nil {
		return registration{}, fmt.Errorf("relay: open registration stream: %w", err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		if err := stream.SetDeadline(deadline); err != nil {
			return registration{}, err
		}
	}

	data, err := marshalFrame(registerFrame(token, ids))
	if err != nil {
		return registration{}, err
	}
	if err := writeStreamFrame(stream, data); err != nil {
		return registration{}, fmt.Errorf("relay: write registration: %w", err)
	}

	frame, err := readStreamFrame(bufio.NewReaderSize(stream, streamBufferSize), maxRegisterBytes)
	if err != nil {
		return registration{}, fmt.Errorf("relay: read registration: %w", err)
	}
	// The relay closes a refused connection only after the client closes the
	// stream, otherwise the answer is lost.
	_ = stream.Close()

	result := frame.GetRegisterResult()
	if result == nil {
		return registration{}, fmt.Errorf("%w: unexpected answer", errMalformedFrame)
	}
	return registrationOf(result)
}

// send transmits one frame as a QUIC datagram.
func (c *quicCarrier) send(frame []byte) error {
	if c.closed.Load() {
		return net.ErrClosed
	}
	err := c.conn.SendDatagram(frame)
	if err == nil {
		return nil
	}
	var tooLarge *quic.DatagramTooLargeError
	if errors.As(err, &tooLarge) {
		return fmt.Errorf("%w: %w", ErrDatagramTooLarge, err)
	}
	return err
}

// receive hands the received datagrams to fn until the connection ends.
func (c *quicCarrier) receive(ctx context.Context, fn func([]byte)) error {
	for {
		data, err := c.conn.ReceiveDatagram(ctx)
		if err != nil {
			return err
		}
		fn(data)
	}
}

// transport returns the kind of the carrier.
func (*quicCarrier) transport() Transport { return TransportQUIC }

// close ends the connection.
func (c *quicCarrier) close() error {
	if !c.closed.CompareAndSwap(false, true) {
		return nil
	}
	return c.conn.CloseWithError(0, "")
}
