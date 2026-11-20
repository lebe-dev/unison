package main

import (
	"context"
	"errors"
	"flag"
	"os"
	"runtime"
	"time"

	"github.com/lebe-dev/unison/internal/daemon"
	"github.com/lebe-dev/unison/platform/linux/keystore"
)

func init() {
	register("keystore", runKeystore)
}

// Names of the secret of the round trip. The service stays apart from the one of
// the user.
const (
	probeService = "app.unison.test.probe"
	probeSecret  = "probe/secret"
)

// probeSecretValue is the value of the round trip. It is not a secret of
// anyone.
const probeSecretValue = "unison probe"

// probeTimeout bounds the round trip and every prompt.
const probeTimeout = 60 * time.Second

// runKeystore reports which backend the data directory uses, the state of the
// Secret Service of the session and a round trip of a test secret.
func runKeystore(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("keystore", flag.ExitOnError)
	dataDir := flags.String("data-dir", "", "data directory, the default one when empty")
	_ = flags.Parse(args)
	if runtime.GOOS != "linux" {
		status("platform", "unsupported on this platform")
		return nil
	}

	dir, err := daemon.ResolveDataDir(*dataDir)
	if err != nil {
		return err
	}
	status("data dir", dir)
	defaultDir, err := daemon.DefaultDataDir()
	if err != nil {
		defaultDir = ""
	}
	status("service", keystore.Service(dir, defaultDir))
	reportSession()

	service := keystore.Service(dir, defaultDir)
	k, err := keystore.Open(dir, service, keystore.Options{PromptTimeout: probeTimeout})
	if err != nil {
		status("backend", "unavailable")
		status("error", err.Error())
		return nil
	}
	status("backend", string(k.Backend()))
	if warning := k.Warning(); warning != "" {
		status("warning", warning)
	}
	if err := roundTrip(ctx, k.Backend()); err != nil {
		status("round trip", "failed")
		return err
	}
	status("round trip", "ok")
	return nil
}

// reportSession prints the Secret Service of the session.
func reportSession() {
	session, err := keystore.Inspect()
	if err != nil {
		status("session bus", "unknown")
		status("error", err.Error())
		return
	}
	status("session bus", yesNo(session.Bus))
	if !session.Bus {
		status("secret service", "unknown")
		status("collection", "unknown")
		return
	}
	status("secret service", yesNo(session.Service))
	switch {
	case !session.Service:
		status("collection", "missing")
	case session.Alias == "":
		status("collection", "missing")
	case session.Locked:
		status("collection", session.Alias+" locked")
	default:
		status("collection", session.Alias+" unlocked")
	}
}

// roundTrip stores, reads and deletes the test secret through the backend.
func roundTrip(ctx context.Context, backend keystore.Backend) error {
	dir, err := os.MkdirTemp("", "unison-probe-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	k, err := keystore.Open(dir, probeService, keystore.Options{Backend: backend, PromptTimeout: probeTimeout})
	if err != nil {
		return err
	}
	// The name is fixed, so a run cleans up after a crashed one as well.
	if err := k.Delete(ctx, probeSecret); err != nil {
		return err
	}
	if err := k.Store(ctx, probeSecret, []byte(probeSecretValue)); err != nil {
		return err
	}
	secret, err := k.Load(ctx, probeSecret)
	if err != nil {
		return err
	}
	if string(secret) != probeSecretValue {
		return errors.New("the loaded secret differs from the stored one")
	}
	return k.Delete(ctx, probeSecret)
}
