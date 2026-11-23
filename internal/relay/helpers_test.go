package relay

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/quic-go/quic-go"
	"google.golang.org/protobuf/proto"

	relayv1 "github.com/lebe-dev/unison/proto/unison/relay/v1"
)

// testToken is the access token of the test servers.
const testToken = "test-token"

// otherToken is the access token of the second test group.
const otherToken = "other-token"

// testTimeout bounds the waits of the tests.
const testTimeout = 5 * time.Second

// testConfig returns a loopback configuration with a fresh certificate.
func testConfig(t *testing.T) Config {
	t.Helper()

	certFile, keyFile := testCertificate(t)
	cfg := DefaultConfig()
	cfg.QUICAddr = "127.0.0.1:0"
	cfg.WSAddr = "127.0.0.1:0"
	cfg.HTTPAddr = "127.0.0.1:0"
	cfg.TLSCertFile = certFile
	cfg.TLSKeyFile = keyFile
	cfg.Tokens = []string{testToken}
	cfg.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	return cfg
}

// twoTokenConfig returns a loopback configuration that serves two groups.
func twoTokenConfig(t *testing.T) Config {
	t.Helper()

	cfg := testConfig(t)
	cfg.Tokens = []string{testToken, otherToken}
	return cfg
}

// startServer starts a server for the configuration and stops it when the test
// ends.
func startServer(t *testing.T, cfg Config) *Server {
	t.Helper()

	server, err := New(cfg)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	if err := server.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Errorf("close server: %v", err)
		}
	})
	return server
}

// testCertificate writes a self-signed certificate for 127.0.0.1 and returns
// the file paths.
func testCertificate(t *testing.T) (certFile, keyFile string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "relay test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.IPv6loopback},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}

	dir := t.TempDir()
	certFile = filepath.Join(dir, "cert.pem")
	keyFile = filepath.Join(dir, "key.pem")
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(certFile, certPEM, 0o600); err != nil {
		t.Fatalf("write certificate: %v", err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return certFile, keyFile
}

// newMailboxID returns a stable mailbox id for a name.
func newMailboxID(name string) mailboxID {
	var id mailboxID
	sum := sha256.Sum256([]byte(name))
	copy(id[:], sum[:mailboxIDLen])
	return id
}

// testClient is a minimal relay client.
type testClient struct {
	t     *testing.T
	conn  *quic.Conn
	sock  *websocket.Conn
	reply *relayv1.RegisterResult

	incoming chan *relayv1.Datagram
	answers  chan *relayv1.UpdateResult
	closed   chan struct{}
}

// dialQUIC dials the QUIC transport and registers the mailboxes.
func dialQUIC(t *testing.T, server *Server, token string, boxes ...mailboxID) *testClient {
	t.Helper()
	return dialQUICIDs(t, server, token, wireMailboxes(boxes))
}

// dialQUICIDs dials the QUIC transport and registers raw mailbox ids.
func dialQUICIDs(t *testing.T, server *Server, token string, boxes [][]byte) *testClient {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	conn, err := quic.DialAddr(ctx, server.Addrs().QUIC, tlsClientConfig(), &quic.Config{EnableDatagrams: true})
	if err != nil {
		t.Fatalf("dial quic: %v", err)
	}

	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	register := &relayv1.Frame{Body: &relayv1.Frame_Register{
		Register: &relayv1.Register{Token: []byte(token), MailboxIds: boxes},
	}}
	data, err := proto.Marshal(register)
	if err != nil {
		t.Fatalf("marshal register: %v", err)
	}
	if err := writeStreamFrame(stream, data); err != nil {
		t.Fatalf("write register: %v", err)
	}
	if err := stream.SetReadDeadline(time.Now().Add(testTimeout)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	reply, err := readStreamFrame(bufio.NewReader(stream), maxRegisterBytes)
	if err != nil {
		t.Fatalf("read register result: %v", err)
	}
	_ = stream.Close()

	client := &testClient{
		t:        t,
		conn:     conn,
		reply:    reply.GetRegisterResult(),
		incoming: make(chan *relayv1.Datagram, 16),
		answers:  make(chan *relayv1.UpdateResult, 16),
		closed:   make(chan struct{}),
	}
	t.Cleanup(func() {
		_ = conn.CloseWithError(0, "")
	})
	go client.readQUIC()
	return client
}

// dialWS dials the WebSocket transport and registers the mailboxes.
func dialWS(t *testing.T, server *Server, token string, boxes ...mailboxID) *testClient {
	t.Helper()
	return dialWSIDs(t, server, token, wireMailboxes(boxes))
}

// dialWSIDs dials the WebSocket transport and registers raw mailbox ids.
func dialWSIDs(t *testing.T, server *Server, token string, boxes [][]byte) *testClient {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	httpClient := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12},
	}}
	url := "wss://" + server.Addrs().WS + WSPath
	// The body of a successful handshake is the connection itself, which the
	// WebSocket client owns, so it must not be closed here.
	sock, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{ //nolint:bodyclose
		HTTPClient:   httpClient,
		Subprotocols: []string{WSSubprotocol},
	})
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}

	register := &relayv1.Frame{Body: &relayv1.Frame_Register{
		Register: &relayv1.Register{Token: []byte(token), MailboxIds: boxes},
	}}
	data, err := proto.Marshal(register)
	if err != nil {
		t.Fatalf("marshal register: %v", err)
	}
	if err := sock.Write(ctx, websocket.MessageBinary, data); err != nil {
		t.Fatalf("write register: %v", err)
	}
	_, data, err = sock.Read(ctx)
	if err != nil {
		t.Fatalf("read register result: %v", err)
	}
	frame, err := parseFrame(data, maxRegisterBytes)
	if err != nil {
		t.Fatalf("parse register result: %v", err)
	}

	client := &testClient{
		t:        t,
		sock:     sock,
		reply:    frame.GetRegisterResult(),
		incoming: make(chan *relayv1.Datagram, 16),
		answers:  make(chan *relayv1.UpdateResult, 16),
		closed:   make(chan struct{}),
	}
	t.Cleanup(func() {
		_ = sock.Close(websocket.StatusNormalClosure, "")
	})
	go client.readWS()
	return client
}

// readQUIC collects the datagrams of a QUIC connection.
func (c *testClient) readQUIC() {
	defer close(c.closed)
	for {
		data, err := c.conn.ReceiveDatagram(context.Background())
		if err != nil {
			return
		}
		if !c.deliver(data) {
			return
		}
	}
}

// readWS collects the datagrams of a WebSocket connection.
func (c *testClient) readWS() {
	defer close(c.closed)
	for {
		_, data, err := c.sock.Read(context.Background())
		if err != nil {
			return
		}
		if !c.deliver(data) {
			return
		}
	}
}

// deliver queues a datagram frame of the client.
func (c *testClient) deliver(data []byte) bool {
	frame, err := parseFrame(data, maxRegisterBytes)
	if err != nil {
		return true
	}
	if answer := frame.GetUpdateResult(); answer != nil {
		select {
		case c.answers <- answer:
			return true
		case <-time.After(testTimeout):
			return false
		}
	}
	datagram := frame.GetDatagram()
	if datagram == nil {
		return true
	}
	select {
	case c.incoming <- datagram:
		return true
	case <-time.After(testTimeout):
		return false
	}
}

// send sends one datagram to a mailbox.
func (c *testClient) send(id mailboxID, payload []byte) error {
	frame := &relayv1.Frame{Body: &relayv1.Frame_Datagram{Datagram: &relayv1.Datagram{
		MailboxId: id[:],
		Payload:   payload,
	}}}
	data, err := proto.Marshal(frame)
	if err != nil {
		return err
	}
	if c.conn != nil {
		return c.conn.SendDatagram(data)
	}
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	return c.sock.Write(ctx, websocket.MessageBinary, data)
}

// update sends a replacement of the mailbox set.
func (c *testClient) update(seq uint64, boxes ...mailboxID) error {
	frame := &relayv1.Frame{Body: &relayv1.Frame_Update{Update: &relayv1.Update{
		Seq:        seq,
		MailboxIds: wireMailboxes(boxes),
	}}}
	data, err := proto.Marshal(frame)
	if err != nil {
		return err
	}
	if c.conn != nil {
		return c.conn.SendDatagram(data)
	}
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	return c.sock.Write(ctx, websocket.MessageBinary, data)
}

// expectAnswer waits for the answer to an update and checks it.
func (c *testClient) expectAnswer(seq uint64, status relayv1.RelayStatus) {
	c.t.Helper()

	select {
	case answer := <-c.answers:
		if answer.GetSeq() != seq || answer.GetStatus() != status {
			c.t.Fatalf("update answer is seq %d status %s, want seq %d status %s",
				answer.GetSeq(), answer.GetStatus(), seq, status)
		}
	case <-time.After(testTimeout):
		c.t.Fatalf("no answer to update %d", seq)
	}
}

// expectNoAnswer fails when an update answer arrives within the grace period.
func (c *testClient) expectNoAnswer() {
	c.t.Helper()

	select {
	case answer := <-c.answers:
		c.t.Fatalf("unexpected answer to update %d", answer.GetSeq())
	case <-time.After(150 * time.Millisecond):
	}
}

// recv waits for the next datagram.
func (c *testClient) recv() (*relayv1.Datagram, error) {
	select {
	case datagram := <-c.incoming:
		return datagram, nil
	case <-c.closed:
		return nil, errors.New("connection closed")
	case <-time.After(testTimeout):
		return nil, errors.New("timeout")
	}
}

// expectNothing fails when a datagram arrives within the grace period.
func (c *testClient) expectNothing() {
	c.t.Helper()

	select {
	case datagram := <-c.incoming:
		c.t.Fatalf("unexpected datagram of %d bytes", len(datagram.GetPayload()))
	case <-time.After(150 * time.Millisecond):
	}
}

// expectClosed waits until the connection ends.
func (c *testClient) expectClosed() {
	c.t.Helper()

	select {
	case <-c.closed:
	case <-time.After(testTimeout):
		c.t.Fatal("connection still open")
	}
}

// wireMailboxes converts mailbox ids to the wire form.
func wireMailboxes(ids []mailboxID) [][]byte {
	out := make([][]byte, 0, len(ids))
	for _, id := range ids {
		out = append(out, id[:])
	}
	return out
}

// httpGet fetches a URL with a timeout.
func httpGet(t *testing.T, url string) (*http.Response, error) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	return http.DefaultClient.Do(request)
}

// tlsClientConfig returns the TLS configuration of a test client.
func tlsClientConfig() *tls.Config {
	return &tls.Config{
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS13,
		NextProtos:         []string{ALPN},
	}
}

// dialRawQUIC connects to the QUIC transport without registering.
func dialRawQUIC(t *testing.T, server *Server, cfg *quic.Config) *quic.Conn {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	conn, err := quic.DialAddr(ctx, server.Addrs().QUIC, tlsClientConfig(), cfg)
	if err != nil {
		t.Fatalf("dial quic: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.CloseWithError(0, "")
	})
	return conn
}

// waitFor polls a condition until it holds.
func waitFor(t *testing.T, what string, condition func() bool) {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("condition %q did not hold", what)
}

// drainQuiet is how long drain waits for the line to stay quiet.
const drainQuiet = 200 * time.Millisecond

// drain collects the datagrams that arrive until the line stays quiet.
func (c *testClient) drain() []*relayv1.Datagram {
	var out []*relayv1.Datagram
	for {
		select {
		case datagram := <-c.incoming:
			out = append(out, datagram)
		case <-c.closed:
			for {
				select {
				case datagram := <-c.incoming:
					out = append(out, datagram)
				default:
					return out
				}
			}
		case <-time.After(drainQuiet):
			return out
		}
	}
}

// registerStream sends a first frame on a new stream and returns the answer.
func registerStream(t *testing.T, conn *quic.Conn, frame *relayv1.Frame) *relayv1.RegisterResult {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	data, err := proto.Marshal(frame)
	if err != nil {
		t.Fatalf("marshal frame: %v", err)
	}
	if err := writeStreamFrame(stream, data); err != nil {
		t.Fatalf("write frame: %v", err)
	}
	if err := stream.SetReadDeadline(time.Now().Add(testTimeout)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	reply, err := readStreamFrame(bufio.NewReader(stream), maxRegisterBytes)
	if err != nil {
		t.Fatalf("read answer: %v", err)
	}
	_ = stream.Close()
	return reply.GetRegisterResult()
}
