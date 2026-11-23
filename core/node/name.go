package node

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/lebe-dev/unison/core/session"
)

// SettingDeviceName is the settings key of the device name announced to peers.
const SettingDeviceName = "device.name"

// Name returns the device name announced to peers. It is the name the node
// started with until SetDeviceName changes it.
func (n *Node) Name() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.name
}

// SetDeviceName stores the name announced to peers and reopens the sessions, so
// every peer records the new name. The stored name is used by the next start
// too.
func (n *Node) SetDeviceName(ctx context.Context, name string) error {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return errors.New("node: device name is required")
	case !utf8.ValidString(name):
		return errors.New("node: device name is not valid UTF-8")
	case len(name) > session.MaxNameLength:
		return fmt.Errorf("node: device name is longer than %d bytes", session.MaxNameLength)
	}
	if err := n.usable(); err != nil {
		return err
	}
	if err := n.db.Settings().SetString(ctx, SettingDeviceName, name); err != nil {
		return fmt.Errorf("node: store the device name: %w", err)
	}

	n.mu.Lock()
	n.name = name
	links := make([]*link, 0, len(n.links))
	for _, link := range n.links {
		links = append(links, link)
	}
	n.mu.Unlock()

	// The peers take the name from the handshake, so the sessions are opened
	// again.
	for _, link := range links {
		link.close()
	}
	n.log.Info("device name changed", "name", name)
	return nil
}
