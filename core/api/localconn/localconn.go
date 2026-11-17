// Package localconn serves the local API over a unix socket or a named pipe.
// The endpoint speaks HTTP/2 in clear text, so the clients use the generated
// Connect clients over a plain connection.
package localconn

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/lebe-dev/unison/proto/unison/local/v1/localv1connect"
)

// EnvPath names the environment variable that overrides the endpoint of the
// daemon.
const EnvPath = "UNISON_LOCAL_API"

// BaseURL is the base URL of the generated clients. The host is not resolved:
// every request goes through the endpoint.
const BaseURL = "http://unison"

// Endpoint limits.
const (
	// maxConnections bounds the clients served at the same time.
	maxConnections = 16
	// idleTimeout closes a client without an active stream.
	idleTimeout = 5 * time.Minute
	// maxHeaderBytes bounds the request head of one client.
	maxHeaderBytes = 64 << 10
	// readHeaderTimeout bounds the time a client takes to send its request head.
	readHeaderTimeout = 10 * time.Second
)

// Errors reported by the endpoint.
var (
	// ErrInsecureEndpoint reports an endpoint that other users can reach.
	ErrInsecureEndpoint = errors.New("localconn: endpoint is reachable by other users")
	// ErrNotSocket reports a path that is not a socket.
	ErrNotSocket = errors.New("localconn: endpoint is not a socket")
)

// DefaultPath returns the endpoint of the daemon: the value of EnvPath when it
// is set, the endpoint of the platform otherwise.
func DefaultPath() (string, error) {
	if path := os.Getenv(EnvPath); path != "" {
		return path, nil
	}
	return platformPath()
}

// Serve serves handler over HTTP/2 on the endpoint until ctx is done.
func Serve(ctx context.Context, ln net.Listener, handler http.Handler) error {
	protocols := new(http.Protocols)
	protocols.SetUnencryptedHTTP2(true)
	server := &http.Server{
		Handler:           handler,
		Protocols:         protocols,
		IdleTimeout:       idleTimeout,
		ReadHeaderTimeout: readHeaderTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
	}

	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = server.Close()
		case <-done:
		}
	}()

	err := server.Serve(&limitListener{listener: ln, slots: make(chan struct{}, maxConnections)})
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// NewHTTPClient returns an HTTP client that sends every request to the
// endpoint.
func NewHTTPClient(path string) *http.Client {
	protocols := new(http.Protocols)
	protocols.SetUnencryptedHTTP2(true)
	transport := &http.Transport{
		Protocols: protocols,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return Dial(ctx, path)
		},
		MaxIdleConns:    maxConnections,
		IdleConnTimeout: idleTimeout,
	}
	return &http.Client{Transport: transport}
}

// NewClient returns a client of the local API served at path.
func NewClient(path string) localv1connect.UnisonServiceClient {
	return localv1connect.NewUnisonServiceClient(NewHTTPClient(path), BaseURL)
}

// limitListener bounds the number of connections served at the same time. A
// connection above the limit is closed instead of queued.
type limitListener struct {
	listener net.Listener
	slots    chan struct{}
}

// Accept returns the next connection with a free slot.
func (l *limitListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.listener.Accept()
		if err != nil {
			return nil, err
		}

		select {
		case l.slots <- struct{}{}:
			return &limitConn{Conn: conn, release: func() { <-l.slots }}, nil
		default:
			_ = conn.Close()
		}
	}
}

// Close closes the endpoint.
func (l *limitListener) Close() error {
	return l.listener.Close()
}

// Addr returns the address of the endpoint.
func (l *limitListener) Addr() net.Addr {
	return l.listener.Addr()
}

// limitConn releases its slot when it is closed.
type limitConn struct {
	net.Conn
	release func()

	once sync.Once
}

// Close closes the connection and releases the slot.
func (c *limitConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(c.release)
	return err
}
