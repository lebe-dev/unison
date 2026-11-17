package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/lebe-dev/unison/core/api"
	"github.com/lebe-dev/unison/core/api/localconn"
	"github.com/lebe-dev/unison/core/discovery"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/node"
	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/internal/observability"
)

// Runtime limits.
const (
	// dialTimeout bounds the check whether an instance answers on the endpoint.
	dialTimeout = 2 * time.Second
	// shutdownTimeout bounds stopping the node when the daemon exits.
	shutdownTimeout = 15 * time.Second
)

// ErrRunning reports a second instance on the same data directory.
var ErrRunning = errors.New("daemon: another instance is running")

// Options configures Run.
type Options struct {
	// DataDir is the directory of the database, the logs and the lock file.
	DataDir string
	// Endpoint is the local API endpoint. The endpoint of the data directory
	// is used when empty.
	Endpoint string
	// Version is the daemon version reported by the local API.
	Version string
	// Name is the device name announced to peers. The name stored in the
	// settings and the name of the host are used when empty.
	Name string
	// Platform is the platform this build reports to peers.
	Platform model.Platform
	// Keystore keeps the identity secrets and the data encryption key.
	Keystore ports.Keystore
	// Clipboard is the local clipboard.
	Clipboard ports.Clipboard
	// Discovery finds the devices of the local network. Multicast DNS is used
	// when nil.
	Discovery ports.Discovery
	// Clock is the time source. The system clock is used when nil.
	Clock ports.Clock
	// LogLevel is the minimum level of the log.
	LogLevel slog.Level
	// Stderr copies the log to this writer. The log goes to the file only when
	// it is nil.
	Stderr io.Writer
}

// withDefaults validates the options and fills in the defaults.
func (o Options) withDefaults() (Options, error) {
	if o.Keystore == nil {
		return o, errors.New("daemon: keystore is required")
	}
	if o.Clipboard == nil {
		return o, errors.New("daemon: clipboard is required")
	}
	if o.Platform == model.PlatformUnknown {
		return o, errors.New("daemon: the platform of this build is not supported")
	}
	if len(o.Name) > maxNameLength {
		return o, fmt.Errorf("daemon: the device name is longer than %d bytes", maxNameLength)
	}

	dataDir, err := ResolveDataDir(o.DataDir)
	if err != nil {
		return o, err
	}
	o.DataDir = dataDir
	if o.Endpoint == "" {
		if o.Endpoint, err = SocketPath(dataDir); err != nil {
			return o, err
		}
	}
	return o, nil
}

// Run runs the daemon until ctx is done. It returns ErrRunning when another
// instance owns the data directory.
func Run(ctx context.Context, opts Options) error {
	opts, err := opts.withDefaults()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(opts.DataDir, 0o700); err != nil {
		return fmt.Errorf("daemon: create the data directory: %w", err)
	}

	lockPath := LockFile(opts.DataDir)
	instance, err := acquireLock(lockPath)
	if errors.Is(err, ErrLocked) {
		return fmt.Errorf("%w: the lock file %s is held, stop that instance first", ErrRunning, lockPath)
	}
	if err != nil {
		return err
	}
	defer func() { _ = instance.release() }()

	if endpointAlive(ctx, opts.Endpoint) {
		return fmt.Errorf("%w: an instance answers on %s", ErrRunning, opts.Endpoint)
	}

	log, closeLog, err := openLog(opts.DataDir, opts.LogLevel, opts.Stderr)
	if err != nil {
		return err
	}
	defer closeLog()

	if err := removeStaleSocket(opts.Endpoint); err != nil {
		return err
	}
	if warner, ok := opts.Clipboard.(startupWarner); ok {
		if warning := warner.StartupWarning(ctx); warning != "" {
			log.Warn(warning)
		}
	}

	name, discoveryKey := bootstrap(ctx, opts.DataDir, opts.Keystore, log)
	if opts.Name != "" {
		name = opts.Name
	}
	if name == "" {
		name = defaultDeviceName()
	}
	key := &keyProvider{}
	if discoveryKey != nil {
		key.use(func(context.Context) ([]byte, error) { return discoveryKey, nil })
	}

	n, disc, err := newNode(opts, name, key, log)
	if err != nil {
		return err
	}
	defer stopNode(n, log)

	log.Info("starting daemon",
		"version", opts.Version,
		"name", name,
		"platform", opts.Platform.String(),
		"data_dir", opts.DataDir,
		"endpoint", opts.Endpoint,
		"clipboard", clipboardMode(opts.Clipboard),
	)
	if err := n.Start(ctx); err != nil {
		return fmt.Errorf("daemon: start the node: %w", err)
	}
	key.use(n.Trust().DiscoveryKey)
	key.useKeys(n.Trust().DiscoveryKeys)
	key.useFriends(n.Trust().FriendsDiscoveryKeys)

	serveCtx, stopServing := context.WithCancel(context.Background())
	defer stopServing()
	serveDone, err := serve(serveCtx, n, opts, name, localNetworkBlocked(disc))
	if err != nil {
		return err
	}

	subscriber := n.Subscribe()
	defer subscriber.Close()
	eventsCtx, stopEvents := context.WithCancel(context.Background())
	eventsDone := make(chan struct{})
	defer func() {
		stopEvents()
		<-eventsDone
	}()
	go func() {
		defer close(eventsDone)
		defer observability.Recover()
		logEvents(eventsCtx, log, subscriber)
	}()

	log.Info("daemon started", "device_id", n.DeviceID().String())
	select {
	case <-ctx.Done():
		log.Info("stopping daemon")
	case err := <-serveDone:
		if err != nil {
			return fmt.Errorf("daemon: the local API stopped: %w", err)
		}
		return errors.New("daemon: the local API stopped")
	}

	stopEvents()
	stopServing()
	if err := <-serveDone; err != nil {
		return fmt.Errorf("daemon: the local API stopped: %w", err)
	}
	stopNode(n, log)
	log.Info("daemon stopped")
	return nil
}

// newNode assembles the node from the options and the platform ports.
func newNode(opts Options, name string, key *keyProvider, log *slog.Logger) (*node.Node, ports.Discovery, error) {
	disc := opts.Discovery
	if disc == nil {
		var err error
		disc, err = discovery.New(discovery.Options{
			Key:         key.get,
			Keys:        key.keys,
			FriendsKeys: key.friendsKeys,
			Clock:       opts.Clock,
			Logger:      log,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("daemon: start discovery: %w", err)
		}
	}

	n, err := node.New(node.Config{
		DataDir:   opts.DataDir,
		Name:      name,
		Platform:  opts.Platform,
		Keystore:  opts.Keystore,
		Clipboard: opts.Clipboard,
		Discovery: disc,
		Clock:     opts.Clock,
		Logger:    log,
	})
	if err != nil {
		return nil, nil, err
	}
	return n, disc, nil
}

// localNetworkReporter is a discovery that knows whether the system lets its
// sends reach the local network.
type localNetworkReporter interface {
	LocalNetworkBlocked() bool
}

// localNetworkBlocked returns the check of the local network access of disc,
// or nil when the discovery cannot tell.
func localNetworkBlocked(disc ports.Discovery) func() bool {
	reporter, ok := disc.(localNetworkReporter)
	if !ok {
		return nil
	}
	return reporter.LocalNetworkBlocked
}

// serve binds the endpoint and serves the local API on it. The returned channel
// reports the end of the server.
func serve(ctx context.Context, n *node.Node, opts Options, name string, lanBlocked func() bool) (<-chan error, error) {
	// SetClip copies text from the clients into the pipeline, which records and
	// announces it.
	service, err := api.New(n, api.Options{
		Version:             opts.Version,
		DeviceName:          name,
		LocalClip:           n.Clip(),
		LocalNetworkBlocked: lanBlocked,
	})
	if err != nil {
		return nil, err
	}
	_, handler := service.Handler()

	listener, err := localconn.Listen(opts.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("daemon: listen on the local API endpoint: %w", err)
	}

	done := make(chan error, 1)
	go func() {
		defer observability.Recover()
		done <- localconn.Serve(ctx, listener, handler)
	}()
	return done, nil
}

// stopNode stops the node within the shutdown timeout. A node that is stopped
// already is released again without effect.
func stopNode(n *node.Node, log *slog.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := n.Stop(ctx); err != nil {
		log.Warn("stopping the node failed", "error", err)
		observability.CaptureException(err)
	}
}

// endpointAlive reports whether an instance answers on the endpoint.
func endpointAlive(ctx context.Context, endpoint string) bool {
	ctx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()

	conn, err := localconn.Dial(ctx, endpoint)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// clipboardMode names the clipboard in the log.
func clipboardMode(clip ports.Clipboard) string {
	if _, ok := clip.(noClipboard); ok {
		return "none"
	}
	return "system"
}

// startupWarner reports a condition of a platform adapter that the user has to
// know about when the daemon starts.
type startupWarner interface {
	// StartupWarning returns a message for the log, or an empty string.
	StartupWarning(ctx context.Context) string
}
