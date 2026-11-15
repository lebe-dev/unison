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
