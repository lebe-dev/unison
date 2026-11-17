package relay

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/lebe-dev/unison/internal/observability"
)

// Timeouts and bounds of the WebSocket fallback.
const (
	// wsPingInterval is the keepalive interval of a WebSocket connection.
	wsPingInterval = 30 * time.Second
	// wsWriteTimeout bounds one write to the relay.
	wsWriteTimeout = 10 * time.Second
	// wsOutQueue bounds the frames waiting for the socket.
	wsOutQueue = 64
)

// errNotRelay reports a server that answers but does not speak the relay
// protocol.
var errNotRelay = errors.New("relay: the server is not a relay")

// handshakeError is a WebSocket handshake that the server answered with an HTTP
// status other than 101.
type handshakeError struct {
	status int
	err    error
}

// Error describes the answer of the server.
func (e *handshakeError) Error() string {
	return fmt.Sprintf("relay: the server answered HTTP %d: %v", e.status, e.err)
}

// Unwrap returns the error of the WebSocket library.
func (e *handshakeError) Unwrap() error {
	return e.err
}

// wsCarrier is the WebSocket fallback: one binary message carries one frame.
type wsCarrier struct {
	conn    *websocket.Conn
	timeout time.Duration

	out  chan []byte
	done chan struct{}
	once sync.Once
	wg   sync.WaitGroup
	dead atomic.Bool
}

// wsCarrier implements carrier.
var _ carrier = (*wsCarrier)(nil)

// dialWebSocket opens the WebSocket connection to the relay.
func (d *Dialer) dialWebSocket(ctx context.Context) (carrier, error) {
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig:     d.cfg.tlsConfig(d.eps.serverName),
			TLSHandshakeTimeout: d.cfg.DialTimeout,
		},
	}
	conn, resp, err := websocket.Dial(ctx, d.eps.webSocket, &websocket.DialOptions{
		HTTPClient:   client,
		Subprotocols: []string{ALPN},
	})
	if err != nil {
		if resp != nil {
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusSwitchingProtocols {
				err = &handshakeError{status: resp.StatusCode, err: err}
			}
		}
		return nil, fmt.Errorf("relay: dial %s: %w", d.eps.webSocket, err)
	}
	if conn.Subprotocol() != ALPN {
		_ = conn.CloseNow()
		return nil, fmt.Errorf("relay: %s answered without the relay protocol: %w", d.eps.webSocket, errNotRelay)
	}
	conn.SetReadLimit(int64(maxRelayDatagrams + frameOverheadBytes))

	c := &wsCarrier{
		conn:    conn,
		timeout: registerTimeout,
		out:     make(chan []byte, wsOutQueue),
		done:    make(chan struct{}),
	}
	c.wg.Add(2)
	//nolint:gosec // The carrier outlives the request that opened it.
	go c.writeLoop()
	//nolint:gosec // The carrier outlives the request that opened it.
	go c.pingLoop()
	return c, nil
}

// register joins the mailboxes and reads the limits of the relay.
func (c *wsCarrier) register(ctx context.Context, token string, ids []ID) (Limits, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	data, err := marshalFrame(registerFrame(token, ids))
	if err != nil {
		return Limits{}, err
	}
	if err := c.write(ctx, data); err != nil {
		return Limits{}, fmt.Errorf("relay: write registration: %w", err)
	}

	for {
		typ, data, err := c.conn.Read(ctx)
		if err != nil {
			return Limits{}, fmt.Errorf("relay: read registration: %w", err)
		}
		if typ != websocket.MessageBinary {
			continue
		}
		frame, err := parseFrame(data, maxRegisterBytes)
		if err != nil {
			return Limits{}, err
		}
		result := frame.GetRegisterResult()
		if result == nil {
			continue
		}
		return limitsOf(result)
	}
}

// send queues one frame for the socket.
func (c *wsCarrier) send(frame []byte) error {
	if c.dead.Load() {
		return net.ErrClosed
	}
	select {
	case c.out <- frame:
		return nil
	default:
		// A congested link loses datagrams anyway.
		return errDropped
	}
}

// receive hands the received frames to fn until the connection ends.
func (c *wsCarrier) receive(ctx context.Context, fn func([]byte)) error {
	for {
		typ, data, err := c.conn.Read(ctx)
		if err != nil {
			return err
		}
		if typ != websocket.MessageBinary {
			continue
		}
		fn(data)
	}
}

// transport returns the kind of the carrier.
func (*wsCarrier) transport() Transport { return TransportWebSocket }

// close ends the connection and waits for the goroutines.
func (c *wsCarrier) close() error {
	c.once.Do(func() {
		c.dead.Store(true)
		close(c.done)
		_ = c.conn.CloseNow()
	})
	c.wg.Wait()
	return nil
}

// write sends one message with a timeout.
func (c *wsCarrier) write(ctx context.Context, data []byte) error {
	ctx, cancel := context.WithTimeout(ctx, wsWriteTimeout)
	defer cancel()
	return c.conn.Write(ctx, websocket.MessageBinary, data)
}

// writeLoop sends the queued frames.
func (c *wsCarrier) writeLoop() {
	defer c.wg.Done()
	defer observability.Recover()

	for {
		select {
		case <-c.done:
			return
		case data := <-c.out:
			if err := c.write(context.Background(), data); err != nil {
				c.fail()
				return
			}
		}
	}
}

// pingLoop detects a dead WebSocket connection.
func (c *wsCarrier) pingLoop() {
	defer c.wg.Done()
	defer observability.Recover()

	ticker := time.NewTicker(wsPingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), wsPingInterval)
			err := c.conn.Ping(ctx)
			cancel()
			if err != nil && !errors.Is(err, context.Canceled) {
				c.fail()
				return
			}
		}
	}
}

// fail ends the connection after an error.
func (c *wsCarrier) fail() {
	c.once.Do(func() {
		c.dead.Store(true)
		close(c.done)
		_ = c.conn.CloseNow()
	})
}
