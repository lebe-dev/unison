package api

import (
	"context"
	"errors"
	"net/http"

	"connectrpc.com/connect"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/node"
	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/core/trust"
	"github.com/lebe-dev/unison/internal/journal"
	"github.com/lebe-dev/unison/internal/observability"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
	"github.com/lebe-dev/unison/proto/unison/local/v1/localv1connect"
)

// Request and response size limits.
const (
	maxRequestBytes  = 1 << 20
	maxResponseBytes = 1 << 20
)

// ErrNotRunning reports a call that needs a started node.
var ErrNotRunning = errors.New("api: node is not running")

// LocalClip copies content that was copied on this device into the core
// pipeline, which records it in the history and announces it to the peers.
type LocalClip interface {
	Submit(ctx context.Context, content ports.Content) (model.ClipItem, error)
}

// Options configures a Service.
type Options struct {
	// Version is the daemon version reported to the clients.
	Version string
	// DeviceName is the name the node runs with. It is the name reported until
	// a device name is stored in the settings.
	DeviceName string
	// LocalClip makes SetClip available. Without it SetClip reports that the
	// call is not implemented.
	LocalClip LocalClip
	// Clock is the time source. The system clock is used when nil.
	Clock ports.Clock
	// LocalNetworkBlocked reports that the system refuses the sends of the
	// daemon to the local network. The access is reported as granted when nil.
	LocalNetworkBlocked func() bool
	// Journal is the application log the user can turn on and share. The
	// setting reports that the log is off when nil.
	Journal *journal.Store
}

// Service implements the local API over a node.
type Service struct {
	node       *node.Node
	version    string
	name       string
	localClip  LocalClip
	clock      ports.Clock
	lanBlocked func() bool
	journal    *journal.Store
}

// compile time check of the generated handler interface.
var _ localv1connect.UnisonServiceHandler = (*Service)(nil)

// New creates the local API service over n. The node does not have to be
// started yet.
func New(n *node.Node, opts Options) (*Service, error) {
	if n == nil {
		return nil, errors.New("api: node is required")
	}
	// The consent gates the events of the whole process, so it is applied
	// before the first call. A store that cannot tell it keeps reporting off.
	if n.Store() != nil {
		enabled, err := n.SentryEnabled(context.Background())
		observability.SetEnabled(err == nil && enabled)
	}
	clock := opts.Clock
	if clock == nil {
		clock = ports.SystemClock
	}
	return &Service{
		node:       n,
		version:    opts.Version,
		name:       opts.DeviceName,
		localClip:  opts.LocalClip,
		clock:      clock,
		lanBlocked: opts.LocalNetworkBlocked,
		journal:    opts.Journal,
	}, nil
}

// Handler returns the path the service is mounted on and the HTTP handler that
// serves it. The size limits apply on top of the given options.
func (s *Service) Handler(opts ...connect.HandlerOption) (string, http.Handler) {
	opts = append(opts,
		connect.WithReadMaxBytes(maxRequestBytes),
		connect.WithSendMaxBytes(maxResponseBytes),
	)
	return localv1connect.NewUnisonServiceHandler(s, opts...)
}

// running reports whether the node has started.
func (s *Service) running() bool {
	return !s.node.DeviceID().IsZero()
}

// requireRunning returns ErrNotRunning for a node that has not started.
func (s *Service) requireRunning() error {
	if s.running() {
		return nil
	}
	return ErrNotRunning
}

// pauseState returns the current pause of the clipboard pipeline.
func (s *Service) pauseState(ctx context.Context) (*localv1.Pause, error) {
	if err := s.requireRunning(); err != nil {
		return nil, err
	}
	paused, until, err := s.node.Clip().Paused(ctx)
	if err != nil {
		return nil, err
	}
	pause := &localv1.Pause{Paused: paused}
	if !paused {
		return pause, nil
	}
	if until.IsZero() {
		pause.Indefinite = true
		return pause, nil
	}
	pause.UntilUnixMs = until.UnixMilli()
	return pause, nil
}

// devices returns the paired devices with their live connection state.
func (s *Service) devices(ctx context.Context, includeRevoked bool) ([]*localv1.Device, error) {
	peers, err := s.node.Trust().List(ctx, trust.ListFilter{IncludeRevoked: includeRevoked})
	if err != nil {
		return nil, err
	}
	states, err := s.node.Peers(ctx)
	if err != nil {
		return nil, err
	}

	live := make(map[model.DeviceID]model.PeerState, len(states))
	for _, state := range states {
		live[state.DeviceID] = state
	}

	devices := make([]*localv1.Device, 0, len(peers))
	for _, peer := range peers {
		devices = append(devices, deviceToProto(peer, live[peer.DeviceID()]))
	}
	return devices, nil
}
