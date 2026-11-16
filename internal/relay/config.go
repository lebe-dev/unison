package relay

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// Application protocols and addresses.
const (
	// ALPN is the application protocol of the QUIC transport.
	ALPN = "unison-relay/1"
	// WSPath is the HTTP path of the WebSocket transport.
	WSPath = "/relay"
	// WSSubprotocol is the WebSocket subprotocol of the transport.
	WSSubprotocol = ALPN

	// DefaultQUICAddr is the listen address of the QUIC transport.
	DefaultQUICAddr = ":8443"
	// DefaultWSAddr is the listen address of the WebSocket fallback.
	DefaultWSAddr = ":8443"
	// DefaultHTTPAddr is the listen address of health and metrics.
	DefaultHTTPAddr = ":8080"
)

// Limits and timeouts of a server.
const (
	// DefaultMaxMailboxes bounds the mailboxes of one connection.
	DefaultMaxMailboxes = 8
	// DefaultMaxDatagramBytes bounds the payload of one datagram. A datagram
	// travels in a single QUIC packet, so it stays below the path MTU. The QUIC
	// connection between devices runs inside these datagrams and needs at least
	// 1200 byte packets, which is 1224 bytes of payload after the frame headers.
	DefaultMaxDatagramBytes = 1400
	// DefaultRatePerSecond limits the datagrams of one token per second.
	DefaultRatePerSecond = 200
	// DefaultRateBurst is the burst of the datagram rate.
	DefaultRateBurst = 400
	// DefaultQuotaBytes is the traffic quota of one token per window.
	DefaultQuotaBytes = 1 << 30
	// DefaultQuotaWindow is the length of the quota window.
	DefaultQuotaWindow = 24 * time.Hour
	// DefaultMaxConnections bounds the concurrent connections.
	DefaultMaxConnections = 4096
	// DefaultMaxMailboxesTotal bounds the mailboxes of the server.
	DefaultMaxMailboxesTotal = 100000
	// DefaultPingInterval is the WebSocket keepalive interval.
	DefaultPingInterval = 30 * time.Second
	// DefaultRegisterTimeout bounds the registration of a connection.
	DefaultRegisterTimeout = 10 * time.Second
)

// Other bounds of the protocol.
const (
	// maxTokenBytes bounds the access token of a registration.
	maxTokenBytes = 512
	// maxTokensFileBytes bounds the token file.
	maxTokensFileBytes = 1 << 20
	// outQueue is the number of datagrams waiting for a client.
	outQueue = 64
	// maxLimitsMailboxes is the largest value of Limits.max_mailboxes. It keeps
	// the registration frame within its limit.
	maxLimitsMailboxes = 256
	// maxLimitsDatagramBytes is the largest value of Limits.max_datagram_bytes
	// and of the rate values. A datagram has to fit into one QUIC packet, so a
	// value above the path MTU only leads to drops.
	maxLimitsDatagramBytes = 4096
)

// Config describes a relay server.
type Config struct {
	// QUICAddr is the UDP address of the QUIC transport.
	QUICAddr string
	// WSAddr is the TCP address of the WebSocket fallback. It serves TLS.
	WSAddr string
	// HTTPAddr is the TCP address of the health and metrics endpoints.
	HTTPAddr string
	// TLSCertFile is the PEM certificate, optionally with its chain.
	TLSCertFile string
	// TLSKeyFile is the PEM private key of the certificate.
	TLSKeyFile string
	// Tokens are the access tokens, given in clear form by the operator and
	// kept as hashes in memory.
	Tokens []string
	// TokensFile is a file with one token per line. Empty lines and lines
	// starting with # are skipped.
	TokensFile string
	// LogLevel is one of debug, info, warn or error.
	LogLevel string
	// Version is the server version reported in the logs and metrics.
	Version string
	// Logger overrides the JSON logger on stderr.
	Logger *slog.Logger

	// MaxMailboxes bounds the mailboxes of one connection.
	MaxMailboxes int
	// MaxDatagramBytes bounds the payload of one datagram.
	MaxDatagramBytes int
	// RatePerSecond limits the datagrams of one token per second.
	RatePerSecond int
	// RateBurst is the burst of the datagram rate.
	RateBurst int
	// QuotaBytes is the traffic quota of one token per window.
	QuotaBytes int64
	// QuotaWindow is the length of the quota window.
	QuotaWindow time.Duration
	// MaxConnections bounds the concurrent connections.
	MaxConnections int
	// MaxMailboxesTotal bounds the mailboxes of the server.
	MaxMailboxesTotal int
	// PingInterval is the WebSocket keepalive interval.
	PingInterval time.Duration
	// RegisterTimeout bounds the registration of a connection.
	RegisterTimeout time.Duration
}

// DefaultConfig returns a configuration with the package defaults.
func DefaultConfig() Config {
	return Config{
		QUICAddr:          DefaultQUICAddr,
		WSAddr:            DefaultWSAddr,
		HTTPAddr:          DefaultHTTPAddr,
		LogLevel:          "info",
		Version:           "dev",
		MaxMailboxes:      DefaultMaxMailboxes,
		MaxDatagramBytes:  DefaultMaxDatagramBytes,
		RatePerSecond:     DefaultRatePerSecond,
		RateBurst:         DefaultRateBurst,
		QuotaBytes:        DefaultQuotaBytes,
		QuotaWindow:       DefaultQuotaWindow,
		MaxConnections:    DefaultMaxConnections,
		MaxMailboxesTotal: DefaultMaxMailboxesTotal,
		PingInterval:      DefaultPingInterval,
		RegisterTimeout:   DefaultRegisterTimeout,
	}
}

// withDefaults fills the zero fields with the package defaults.
func (c Config) withDefaults() Config {
	def := DefaultConfig()
	if c.QUICAddr == "" {
		c.QUICAddr = def.QUICAddr
	}
	if c.WSAddr == "" {
		c.WSAddr = def.WSAddr
	}
	if c.HTTPAddr == "" {
		c.HTTPAddr = def.HTTPAddr
	}
	if c.LogLevel == "" {
		c.LogLevel = def.LogLevel
	}
	if c.Version == "" {
		c.Version = def.Version
	}
	if c.MaxMailboxes == 0 {
		c.MaxMailboxes = def.MaxMailboxes
	}
	if c.MaxDatagramBytes == 0 {
		c.MaxDatagramBytes = def.MaxDatagramBytes
	}
	if c.RatePerSecond == 0 {
		c.RatePerSecond = def.RatePerSecond
	}
	if c.RateBurst == 0 {
		c.RateBurst = def.RateBurst
	}
	if c.QuotaBytes == 0 {
		c.QuotaBytes = def.QuotaBytes
	}
	if c.QuotaWindow == 0 {
		c.QuotaWindow = def.QuotaWindow
	}
	if c.MaxConnections == 0 {
		c.MaxConnections = def.MaxConnections
	}
	if c.MaxMailboxesTotal == 0 {
		c.MaxMailboxesTotal = def.MaxMailboxesTotal
	}
	if c.PingInterval == 0 {
		c.PingInterval = def.PingInterval
	}
	if c.RegisterTimeout == 0 {
		c.RegisterTimeout = def.RegisterTimeout
	}
	return c
}

// Validate reports a configuration the server cannot run with.
func (c Config) Validate() error {
	switch {
	case c.TLSCertFile == "":
		return errors.New("relay: TLS certificate file is required")
	case c.TLSKeyFile == "":
		return errors.New("relay: TLS key file is required")
	case c.QUICAddr == "":
		return errors.New("relay: QUIC address is required")
	case c.WSAddr == "":
		return errors.New("relay: WebSocket address is required")
	case c.MaxMailboxes < 1 || c.MaxMailboxes > maxLimitsMailboxes:
		return fmt.Errorf("relay: max mailboxes must be between 1 and %d", maxLimitsMailboxes)
	case c.MaxDatagramBytes < 64 || c.MaxDatagramBytes > maxLimitsDatagramBytes:
		return fmt.Errorf("relay: max datagram bytes must be between 64 and %d", maxLimitsDatagramBytes)
	case c.RatePerSecond < 0:
		return errors.New("relay: rate must not be negative")
	case c.RatePerSecond > maxLimitsDatagramBytes:
		return fmt.Errorf("relay: rate must not exceed %d", maxLimitsDatagramBytes)
	case c.RateBurst < 1:
		return errors.New("relay: rate burst must be positive")
	case c.RateBurst > maxLimitsDatagramBytes:
		return fmt.Errorf("relay: rate burst must not exceed %d", maxLimitsDatagramBytes)
	case c.QuotaBytes < 1:
		return errors.New("relay: quota bytes must be positive")
	case c.QuotaWindow < 0:
		return errors.New("relay: quota window must not be negative")
	case c.MaxConnections < 1:
		return errors.New("relay: max connections must be positive")
	case c.MaxMailboxesTotal < 1:
		return errors.New("relay: max mailboxes total must be positive")
	case c.PingInterval < 0:
		return errors.New("relay: ping interval must not be negative")
	case c.RegisterTimeout <= 0:
		return errors.New("relay: register timeout must be positive")
	}
	if _, err := logLevel(c.LogLevel); err != nil {
		return err
	}
	return nil
}

// ParseConfig reads the configuration from the environment and the flags. The
// environment provides the defaults and the flags override them. The caller
// owns the flag set, so it can set the usage text.
func ParseConfig(fs *flag.FlagSet, args []string, getenv func(string) string) (Config, error) {
	cfg := DefaultConfig()
	if err := cfg.loadEnv(getenv); err != nil {
		return Config{}, err
	}
	cfg.registerFlags(fs)
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// registerFlags binds the configuration fields to flags.
func (c *Config) registerFlags(fs *flag.FlagSet) {
	fs.StringVar(&c.QUICAddr, "quic-addr", c.QUICAddr, "QUIC listen address (UDP)")
	fs.StringVar(&c.WSAddr, "ws-addr", c.WSAddr, "WebSocket fallback listen address (TCP, TLS)")
	fs.StringVar(&c.HTTPAddr, "http-addr", c.HTTPAddr, "health and metrics listen address")
	fs.StringVar(&c.TLSCertFile, "tls-cert", c.TLSCertFile, "TLS certificate file")
	fs.StringVar(&c.TLSKeyFile, "tls-key", c.TLSKeyFile, "TLS private key file")
	fs.StringVar(&c.TokensFile, "tokens-file", c.TokensFile, "file with one access token per line")
	fs.StringVar(&c.LogLevel, "log-level", c.LogLevel, "log level: debug, info, warn or error")
	fs.IntVar(&c.MaxMailboxes, "max-mailboxes", c.MaxMailboxes, "mailboxes per connection")
	fs.IntVar(&c.MaxDatagramBytes, "max-datagram-bytes", c.MaxDatagramBytes, "payload bytes of one datagram")
	fs.IntVar(&c.RatePerSecond, "rate", c.RatePerSecond, "datagrams per second per token")
	fs.IntVar(&c.RateBurst, "rate-burst", c.RateBurst, "burst of the datagram rate")
	fs.Int64Var(&c.QuotaBytes, "quota-bytes", c.QuotaBytes, "traffic quota per token per window, in bytes")
	fs.DurationVar(&c.QuotaWindow, "quota-window", c.QuotaWindow, "length of the quota window")
	fs.IntVar(&c.MaxConnections, "max-connections", c.MaxConnections, "concurrent connections")
	fs.IntVar(&c.MaxMailboxesTotal, "max-mailboxes-total", c.MaxMailboxesTotal, "mailboxes of the server")
	fs.DurationVar(&c.PingInterval, "ping-interval", c.PingInterval, "WebSocket keepalive interval")
	fs.DurationVar(&c.RegisterTimeout, "register-timeout", c.RegisterTimeout, "registration timeout")
	fs.Var(tokensFlag{target: &c.Tokens}, "tokens", "comma separated access tokens")
}

// tokensFlag collects the comma separated tokens of one flag.
type tokensFlag struct {
	target *[]string
}

// String returns an empty string, so usage never prints the tokens.
func (t tokensFlag) String() string {
	return ""
}

// Set replaces the tokens with a comma separated list.
func (t tokensFlag) Set(value string) error {
	parts := strings.Split(value, ",")
	tokens := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			tokens = append(tokens, part)
		}
	}
	*t.target = tokens
	return nil
}

// loadEnv applies the environment over the defaults.
func (c *Config) loadEnv(getenv func(string) string) error {
	e := &env{get: getenv}
	c.QUICAddr = e.string("UNISON_RELAY_QUIC_ADDR", c.QUICAddr)
	c.WSAddr = e.string("UNISON_RELAY_WS_ADDR", c.WSAddr)
	c.HTTPAddr = e.string("UNISON_RELAY_HTTP_ADDR", c.HTTPAddr)
	c.TLSCertFile = e.string("UNISON_RELAY_TLS_CERT", c.TLSCertFile)
	c.TLSKeyFile = e.string("UNISON_RELAY_TLS_KEY", c.TLSKeyFile)
	c.TokensFile = e.string("UNISON_RELAY_TOKENS_FILE", c.TokensFile)
	c.LogLevel = e.string("UNISON_RELAY_LOG_LEVEL", c.LogLevel)
	c.Tokens = e.list("UNISON_RELAY_TOKENS", c.Tokens)
	c.MaxMailboxes = e.integer("UNISON_RELAY_MAX_MAILBOXES", c.MaxMailboxes)
	c.MaxDatagramBytes = e.integer("UNISON_RELAY_MAX_DATAGRAM_BYTES", c.MaxDatagramBytes)
	c.RatePerSecond = e.integer("UNISON_RELAY_RATE", c.RatePerSecond)
	c.RateBurst = e.integer("UNISON_RELAY_RATE_BURST", c.RateBurst)
	c.QuotaBytes = e.size("UNISON_RELAY_QUOTA_BYTES", c.QuotaBytes)
	c.QuotaWindow = e.duration("UNISON_RELAY_QUOTA_WINDOW", c.QuotaWindow)
	c.MaxConnections = e.integer("UNISON_RELAY_MAX_CONNECTIONS", c.MaxConnections)
	c.MaxMailboxesTotal = e.integer("UNISON_RELAY_MAX_MAILBOXES_TOTAL", c.MaxMailboxesTotal)
	c.PingInterval = e.duration("UNISON_RELAY_PING_INTERVAL", c.PingInterval)
	c.RegisterTimeout = e.duration("UNISON_RELAY_REGISTER_TIMEOUT", c.RegisterTimeout)
	return e.err
}

// credentials returns the access tokens, with the tokens of the file in
// addition to the ones given directly.
func (c Config) credentials() ([]string, error) {
	tokens := make([]string, 0, len(c.Tokens))
	for _, token := range c.Tokens {
		if token = strings.TrimSpace(token); token != "" {
			tokens = append(tokens, token)
		}
	}
	if c.TokensFile == "" {
		return tokens, nil
	}
	file, err := os.Open(c.TokensFile)
	if err != nil {
		return nil, fmt.Errorf("relay: read tokens: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	data, err := io.ReadAll(io.LimitReader(file, maxTokensFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("relay: read tokens: %w", err)
	}
	if len(data) > maxTokensFileBytes {
		return nil, fmt.Errorf("relay: tokens file is larger than %d bytes", maxTokensFileBytes)
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		tokens = append(tokens, line)
	}
	return tokens, nil
}

// logLevel converts the configured name.
func logLevel(name string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info", "":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	}
	return 0, fmt.Errorf("relay: unknown log level %q", name)
}

// env reads environment variables and keeps the first error.
type env struct {
	get func(string) string
	err error
}

// string returns the value of key, or def when the variable is empty.
func (e *env) string(key, def string) string {
	if value := strings.TrimSpace(e.get(key)); value != "" {
		return value
	}
	return def
}

// list returns the comma separated value of key, or def when it is empty.
func (e *env) list(key string, def []string) []string {
	value := strings.TrimSpace(e.get(key))
	if value == "" {
		return def
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// integer returns the integer value of key, or def when it is empty.
func (e *env) integer(key string, def int) int {
	value := strings.TrimSpace(e.get(key))
	if value == "" {
		return def
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		e.fail(key, err)
		return def
	}
	return n
}

// size returns the positive integer value of key, or def when it is empty.
func (e *env) size(key string, def int64) int64 {
	value := strings.TrimSpace(e.get(key))
	if value == "" {
		return def
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		e.fail(key, err)
		return def
	}
	return n
}

// duration returns the duration value of key, or def when it is empty.
func (e *env) duration(key string, def time.Duration) time.Duration {
	value := strings.TrimSpace(e.get(key))
	if value == "" {
		return def
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		e.fail(key, err)
		return def
	}
	return d
}

// fail keeps the first parsing error.
func (e *env) fail(key string, err error) {
	if e.err == nil {
		e.err = fmt.Errorf("relay: environment variable %s: %w", key, err)
	}
}
