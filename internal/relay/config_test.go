package relay

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	relayv1 "github.com/lebe-dev/unison/proto/unison/relay/v1"
)

// TestParseConfigDefaults reads the package defaults.
func TestParseConfigDefaults(t *testing.T) {
	cfg, err := ParseConfig(flag.NewFlagSet("test", flag.ContinueOnError), nil, emptyEnv)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.QUICAddr != DefaultQUICAddr || cfg.WSAddr != DefaultWSAddr {
		t.Fatalf("addresses: got %q and %q", cfg.QUICAddr, cfg.WSAddr)
	}
	if cfg.MaxDatagramBytes != DefaultMaxDatagramBytes || cfg.MaxMailboxes != DefaultMaxMailboxes {
		t.Fatalf("limits: got %+v", cfg)
	}
}

// TestParseConfigEnv reads the environment.
func TestParseConfigEnv(t *testing.T) {
	env := map[string]string{
		"UNISON_RELAY_QUIC_ADDR":          "127.0.0.1:9443",
		"UNISON_RELAY_WS_ADDR":            "127.0.0.1:9444",
		"UNISON_RELAY_TLS_CERT":           "/tmp/cert.pem",
		"UNISON_RELAY_TLS_KEY":            "/tmp/key.pem",
		"UNISON_RELAY_TOKENS":             "one, two",
		"UNISON_RELAY_TOKENS_FILE":        "/tmp/tokens",
		"UNISON_RELAY_MAX_DATAGRAM_BYTES": "1200",
		"UNISON_RELAY_QUOTA_BYTES":        "2048",
		"UNISON_RELAY_QUOTA_WINDOW":       "1h",
		"UNISON_RELAY_LOG_LEVEL":          "debug",
	}

	cfg, err := ParseConfig(flag.NewFlagSet("test", flag.ContinueOnError), nil, mapEnv(env))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.QUICAddr != env["UNISON_RELAY_QUIC_ADDR"] || cfg.WSAddr != env["UNISON_RELAY_WS_ADDR"] {
		t.Fatalf("addresses: got %q and %q", cfg.QUICAddr, cfg.WSAddr)
	}
	if cfg.TLSCertFile != "/tmp/cert.pem" || cfg.TLSKeyFile != "/tmp/key.pem" {
		t.Fatalf("TLS files: got %q and %q", cfg.TLSCertFile, cfg.TLSKeyFile)
	}
	if len(cfg.Tokens) != 2 || cfg.Tokens[0] != "one" || cfg.Tokens[1] != "two" {
		t.Fatalf("tokens: got %q", cfg.Tokens)
	}
	if cfg.MaxDatagramBytes != 1200 || cfg.QuotaBytes != 2048 || cfg.QuotaWindow != time.Hour {
		t.Fatalf("limits: got %+v", cfg)
	}
	if cfg.LogLevel != "debug" {
		t.Fatalf("log level: got %q", cfg.LogLevel)
	}
}

// TestParseConfigFlags overrides the environment with flags.
func TestParseConfigFlags(t *testing.T) {
	env := mapEnv(map[string]string{"UNISON_RELAY_QUOTA_BYTES": "2048"})
	args := []string{"-quota-bytes", "4096", "-tokens", "flag-token", "-rate", "10"}
	cfg, err := ParseConfig(flag.NewFlagSet("test", flag.ContinueOnError), args, env)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.QuotaBytes != 4096 || cfg.RatePerSecond != 10 {
		t.Fatalf("limits: got %+v", cfg)
	}
	if len(cfg.Tokens) != 1 || cfg.Tokens[0] != "flag-token" {
		t.Fatalf("tokens: got %q", cfg.Tokens)
	}
}

// TestTokensNotInUsage keeps the tokens out of the usage text.
func TestTokensNotInUsage(t *testing.T) {
	var out strings.Builder
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(&out)
	env := mapEnv(map[string]string{"UNISON_RELAY_TOKENS": "secret-token"})
	if _, err := ParseConfig(fs, nil, env); err != nil {
		t.Fatalf("parse: %v", err)
	}
	fs.PrintDefaults()
	if strings.Contains(out.String(), "secret-token") {
		t.Fatalf("usage prints a token:\n%s", out.String())
	}
}

// TestParseConfigBadEnv reports an unparsable environment variable.
func TestParseConfigBadEnv(t *testing.T) {
	env := mapEnv(map[string]string{"UNISON_RELAY_QUOTA_BYTES": "many"})
	_, err := ParseConfig(flag.NewFlagSet("test", flag.ContinueOnError), nil, env)
	if err == nil {
		t.Fatal("expected an error")
	}
}

// TestParseConfigHelp returns the help request of the flag package.
func TestParseConfigHelp(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(discard{})
	_, err := ParseConfig(fs, []string{"-h"}, emptyEnv)
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("error: got %v, want %v", err, flag.ErrHelp)
	}
}

// TestConfigValidate covers the required fields and the bounds.
func TestConfigValidate(t *testing.T) {
	valid := func() Config {
		cfg := DefaultConfig()
		cfg.TLSCertFile = "cert.pem"
		cfg.TLSKeyFile = "key.pem"
		return cfg
	}

	tests := []struct {
		name   string
		change func(*Config)
		wantOK bool
	}{
		{name: "defaults", change: func(*Config) {}, wantOK: true},
		{name: "no certificate", change: func(c *Config) { c.TLSCertFile = "" }},
		{name: "no key", change: func(c *Config) { c.TLSKeyFile = "" }},
		{name: "zero mailboxes", change: func(c *Config) { c.MaxMailboxes = 0 }},
		{name: "tiny datagram", change: func(c *Config) { c.MaxDatagramBytes = 8 }},
		{name: "huge datagram", change: func(c *Config) { c.MaxDatagramBytes = 1 << 20 }},
		{name: "negative rate", change: func(c *Config) { c.RatePerSecond = -1 }},
		{name: "zero burst", change: func(c *Config) { c.RateBurst = 0 }},
		{name: "zero quota", change: func(c *Config) { c.QuotaBytes = 0 }},
		{name: "negative window", change: func(c *Config) { c.QuotaWindow = -time.Second }},
		{name: "zero connections", change: func(c *Config) { c.MaxConnections = 0 }},
		{name: "zero mailboxes total", change: func(c *Config) { c.MaxMailboxesTotal = 0 }},
		{name: "unknown log level", change: func(c *Config) { c.LogLevel = "verbose" }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := valid()
			test.change(&cfg)
			err := cfg.Validate()
			if test.wantOK && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !test.wantOK && err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

// TestTokensFile reads the tokens from a file.
func TestTokensFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tokens")
	content := "# access tokens\n\n  file-token  \nsecond-token\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write tokens: %v", err)
	}

	cfg := testConfig(t)
	cfg.Tokens = []string{"extra-token"}
	cfg.TokensFile = path
	server := startServer(t, cfg)
	box := newMailboxID("pair")

	dialQUIC(t, server, "file-token", box).expectStatus(relayv1.RelayStatus_RELAY_STATUS_OK)
	dialWS(t, server, "second-token", box).expectStatus(relayv1.RelayStatus_RELAY_STATUS_OK)
	dialQUIC(t, server, "extra-token", newMailboxID("other")).expectStatus(relayv1.RelayStatus_RELAY_STATUS_OK)
}

// TestStartWithoutTokens refuses to start without access tokens.
func TestStartWithoutTokens(t *testing.T) {
	cfg := testConfig(t)
	cfg.Tokens = nil
	server, err := New(cfg)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	if err := server.Start(); err == nil {
		t.Fatal("expected an error")
	}
}

// emptyEnv returns no environment variables.
func emptyEnv(string) string { return "" }

// mapEnv returns an environment from a map.
func mapEnv(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

// discard drops the output of the flag package.
type discard struct{}

// Write drops the bytes.
func (discard) Write(p []byte) (int, error) { return len(p), nil }
