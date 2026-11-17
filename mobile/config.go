package mobile

import (
	"errors"
	"log/slog"
	"os"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/lebe-dev/unison/core/node"
	"github.com/lebe-dev/unison/core/ports"
	mobilev1 "github.com/lebe-dev/unison/proto/unison/mobile/v1"
)

// Time bounds of the facade operations.
const (
	startTimeout = 30 * time.Second
	callTimeout  = 30 * time.Second
	stopTimeout  = 10 * time.Second
	// flushTimeout bounds the wait for the queued reports when the core stops.
	flushTimeout = 2 * time.Second
	// transferTimeout bounds the calls that move a whole file, which takes
	// longer than any other call on a slow link or through the relay.
	transferTimeout = 30 * time.Minute
)

// callTimeoutOf returns the time bound of one local API method.
func callTimeoutOf(method string) time.Duration {
	switch methodName(method) {
	case "FetchHistoryItem":
		return transferTimeout
	default:
		return callTimeout
	}
}

// Size limits of the facade.
const (
	// maxCallBytes bounds one call request and one call response.
	maxCallBytes = 1 << 20
	// maxNameBytes bounds the device name.
	maxNameBytes = 128
)

// defaultDeviceName is used when the host does not configure a name.
const defaultDeviceName = "Android device"

// config describes the core of the host. The test hooks are zero outside tests.
type config struct {
	dataDir   string
	name      string
	blobLimit int64
	debug     bool
	clock     ports.Clock
	listen    node.ListenPacketFunc
	limits    node.Limits
	logger    *slog.Logger
}

// parseConfig decodes the serialized configuration.
func parseConfig(data []byte) (config, error) {
	msg := &mobilev1.Config{}
	if len(data) > 0 {
		if err := proto.Unmarshal(data, msg); err != nil {
			return config{}, errors.New("mobile: decode config: " + err.Error())
		}
	}
	if msg.GetDataDir() == "" {
		return config{}, errors.New("mobile: data directory is required")
	}

	name := msg.GetDeviceName()
	if name == "" {
		name = defaultDeviceName
	}
	if len(name) > maxNameBytes {
		return config{}, errors.New("mobile: device name is too long")
	}
	return config{
		dataDir:   msg.GetDataDir(),
		name:      name,
		blobLimit: msg.GetBlobLimitBytes(),
		debug:     msg.GetDebugLog(),
	}, nil
}

// loggerOf returns the logger of the core.
func (c config) loggerOf() *slog.Logger {
	if c.logger != nil {
		return c.logger
	}
	level := slog.LevelInfo
	if c.debug {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}
