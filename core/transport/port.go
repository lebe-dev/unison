package transport

import (
	"context"
	"errors"
	"fmt"
	"net"
)

// PortSettingsKey is the settings key holding the port of the main listener.
const PortSettingsKey = "transport.main_port"

// Settings stores the persisted listener port. The settings store implements
// it.
type Settings interface {
	GetInt(ctx context.Context, key string, def int64) (int64, error)
	SetInt(ctx context.Context, key string, value int64) error
}

// MainPort returns the UDP port of the main listener. The first call picks a
// free port and stores it, so the port survives restarts and the addresses
// cached by peers keep working.
func MainPort(ctx context.Context, settings Settings) (int, error) {
	if settings == nil {
		return 0, errors.New("transport: nil settings")
	}

	port, err := settings.GetInt(ctx, PortSettingsKey, 0)
	if err != nil {
		return 0, fmt.Errorf("transport: read main port: %w", err)
	}
	if port > 0 && port <= 65535 {
		return int(port), nil
	}

	port, err = freePort()
	if err != nil {
		return 0, err
	}
	if err := settings.SetInt(ctx, PortSettingsKey, port); err != nil {
		return 0, fmt.Errorf("transport: store main port: %w", err)
	}
	return int(port), nil
}

// freePort asks the kernel for a free UDP port. The port is released right
// away, so another process can take it before the listener binds it.
func freePort() (int64, error) {
	pc, err := net.ListenPacket("udp", ":0")
	if err != nil {
		return 0, fmt.Errorf("transport: pick a port: %w", err)
	}
	addr, ok := pc.LocalAddr().(*net.UDPAddr)
	if !ok {
		_ = pc.Close()
		return 0, fmt.Errorf("transport: picked port has address %T", pc.LocalAddr())
	}
	port := int64(addr.Port)
	if err := pc.Close(); err != nil {
		return 0, fmt.Errorf("transport: release the picked port: %w", err)
	}
	return port, nil
}
