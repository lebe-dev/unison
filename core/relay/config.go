package relay

import (
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/lebe-dev/unison/core/ports"
)

// Protocol constants of the relay.
const (
	// ALPN is the application protocol of the relay transports.
	ALPN = "unison-relay/1"
	// WSPath is the path of the WebSocket endpoint.
	WSPath = "/relay"
	// DefaultPort is the port of the relay when the address has none.
	DefaultPort = "443"
)

// Defaults of a client.
const (
	// DefaultDialTimeout bounds one attempt to reach the relay. It is the time
	// UDP gets before the client falls back to WebSocket.
	DefaultDialTimeout = 3 * time.Second
	// DefaultReconnectDelay is the first delay after a failed connection.
	DefaultReconnectDelay = time.Second
	// DefaultReconnectLimit caps the delay after failed connections.
	DefaultReconnectLimit = 30 * time.Second
	// DefaultQueue is the number of datagrams queued for the local packets.
	// Datagrams beyond the queue are dropped, like on a congested link.
	DefaultQueue = 256
	// DefaultRollover is how long before UTC midnight the next day mailbox is
	// registered as well.
	DefaultRollover = 30 * time.Minute
)

// Errors of a client.
var (
	// ErrClosed reports a dialer that was closed.
	ErrClosed = errors.New("relay: closed")
	// ErrAlreadyOpen reports a rendezvous key that already has a connection.
	ErrAlreadyOpen = errors.New("relay: mailbox is already open")
	// ErrDatagramTooSmall reports relay datagrams that cannot carry a QUIC
	// packet.
	ErrDatagramTooSmall = errors.New("relay: datagrams are too small for QUIC")
	// ErrRejected reports a registration the relay refused.
	ErrRejected = errors.New("relay: registration refused")
	// ErrDatagramTooLarge reports a payload that does not fit into one relay
	// datagram.
	ErrDatagramTooLarge = errors.New("relay: datagram too large")
)

// Config describes a relay client.
type Config struct {
	// URL is the relay endpoint, either host:port or https://host:port. The
	// QUIC transport talks UDP to it and the WebSocket fallback TCP to the same
	// port.
	URL string
	// QUICAddr overrides the UDP endpoint of the QUIC transport. It is derived
	// from URL when empty.
	QUICAddr string
	// Token is the access token of the group.
	Token string
	// TLS configures the connection to the relay. The system roots and the
	// server name of URL are used when nil.
	TLS *tls.Config
	// Logger receives connection events. Nothing is logged when nil.
	Logger *slog.Logger
	// Clock is the time source that selects the mailbox day. The system clock
	// is used when nil.
	Clock ports.Clock
	// DialTimeout bounds one attempt to reach the relay.
	// DefaultDialTimeout is used when zero.
	DialTimeout time.Duration
	// ReconnectDelay is the first delay after a failed connection.
	// DefaultReconnectDelay is used when zero.
	ReconnectDelay time.Duration
	// ReconnectLimit caps the delay after failed connections.
	// DefaultReconnectLimit is used when zero.
	ReconnectLimit time.Duration
	// Queue bounds the datagrams waiting for the local packets.
	// DefaultQueue is used when zero.
	Queue int
	// Rollover is how long before UTC midnight the client registers the
	// mailbox of the next day. DefaultRollover is used when zero.
	Rollover time.Duration
}

// withDefaults fills the zero fields with the package defaults.
func (c Config) withDefaults() Config {
	if c.DialTimeout <= 0 {
		c.DialTimeout = DefaultDialTimeout
	}
	if c.ReconnectDelay <= 0 {
		c.ReconnectDelay = DefaultReconnectDelay
	}
	if c.ReconnectLimit < c.ReconnectDelay {
		c.ReconnectLimit = max(DefaultReconnectLimit, c.ReconnectDelay)
	}
	if c.Queue <= 0 {
		c.Queue = DefaultQueue
	}
	if c.Rollover <= 0 {
		c.Rollover = DefaultRollover
	}
	return c
}

// validate checks the fields the user supplies.
func (c Config) validate() error {
	switch {
	case c.URL == "":
		return errors.New("relay: relay address is required")
	case c.Token == "":
		return errors.New("relay: access token is required")
	}
	return nil
}

// endpoints are the addresses a client connects to.
type endpoints struct {
	// quic is the UDP address of the QUIC transport.
	quic string
	// webSocket is the URL of the WebSocket fallback.
	webSocket string
	// serverName is the TLS server name of the relay.
	serverName string
}

// endpoints derives the addresses of the transports from the configuration.
func (c Config) endpoints() (endpoints, error) {
	host, port, err := splitRelayURL(c.URL)
	if err != nil {
		return endpoints{}, err
	}
	ep := endpoints{
		quic:       net.JoinHostPort(host, port),
		webSocket:  "wss://" + net.JoinHostPort(host, port) + WSPath,
		serverName: host,
	}
	if c.QUICAddr != "" {
		if _, _, err := net.SplitHostPort(c.QUICAddr); err != nil {
			return endpoints{}, fmt.Errorf("relay: QUIC address %q: %w", c.QUICAddr, err)
		}
		ep.quic = c.QUICAddr
	}
	return ep, nil
}

// ValidateURL checks a relay address of a client: an optional https or wss
// scheme, a host and an optional port. A plain host:port address is accepted.
func ValidateURL(raw string) error {
	host, port, err := splitRelayURL(raw)
	if err != nil {
		return err
	}
	if strings.ContainsAny(host, " /\\?#") {
		return fmt.Errorf("relay: relay address %q: invalid host", raw)
	}
	number, err := strconv.Atoi(port)
	if err != nil {
		return fmt.Errorf("relay: relay address %q: port %q is not a number", raw, port)
	}
	if number < 1 || number > 65535 {
		return fmt.Errorf("relay: relay address %q: port %d is out of range", raw, number)
	}
	return nil
}

// splitRelayURL returns the host and the port of a relay address.
func splitRelayURL(raw string) (host, port string, err error) {
	address := strings.TrimSpace(raw)
	if address == "" {
		return "", "", errors.New("relay: empty relay address")
	}
	if strings.Contains(address, "://") {
		parsed, err := url.Parse(address)
		if err != nil {
			return "", "", fmt.Errorf("relay: relay address %q: %w", raw, err)
		}
		if parsed.Scheme != "https" && parsed.Scheme != "wss" {
			return "", "", fmt.Errorf("relay: relay address %q: scheme %q is not TLS", raw, parsed.Scheme)
		}
		if parsed.RawQuery != "" || (parsed.Path != "" && parsed.Path != "/") {
			return "", "", fmt.Errorf("relay: relay address %q: no path or query", raw)
		}
		if parsed.User != nil || parsed.Fragment != "" {
			return "", "", fmt.Errorf("relay: relay address %q: no user or fragment", raw)
		}
		address = parsed.Host
	}
	host, port, err = net.SplitHostPort(address)
	if err != nil {
		// A plain address without a port, including a bare IPv6 one.
		host = strings.Trim(address, "[]")
		port = DefaultPort
	}
	if host == "" {
		return "", "", fmt.Errorf("relay: relay address %q has no host", raw)
	}
	return host, port, nil
}

// tlsConfig returns the TLS configuration of the connection to the relay.
func (c Config) tlsConfig(serverName string) *tls.Config {
	var cfg *tls.Config
	if c.TLS != nil {
		cfg = c.TLS.Clone()
	} else {
		cfg = &tls.Config{MinVersion: tls.VersionTLS13}
	}
	if cfg.ServerName == "" {
		cfg.ServerName = serverName
	}
	return cfg
}
