package session

import (
	"fmt"
	"slices"
	"unicode"
	"unicode/utf8"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/model/protoconv"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// PeerHello is the handshake information the peer announced.
type PeerHello struct {
	// Version is the protocol version of the peer.
	Version uint32
	// Capabilities are the optional features the peer announced.
	Capabilities []string
	// Name is the device name of the peer.
	Name string
	// Platform is the operating system of the peer.
	Platform model.Platform
	// RequireConfirmation reports that the peer does not apply clipboard items
	// without a prompt.
	RequireConfirmation bool
}

// parseHello validates a Hello message and returns the announced information.
func parseHello(msg *wirev1.Hello) (PeerHello, error) {
	hello := PeerHello{
		Version:  msg.GetProtocolVersion(),
		Name:     msg.GetName(),
		Platform: protoconv.PlatformFromProto(msg.GetPlatform()),
	}
	if policy := msg.GetSelfPolicy(); policy != nil {
		hello.RequireConfirmation = policy.GetRequireConfirmation()
	}

	if err := validateName(hello.Name); err != nil {
		return hello, err
	}
	capabilities := msg.GetCapabilities()
	if len(capabilities) > MaxCapabilities {
		return hello, fmt.Errorf("%w: %d capabilities, limit %d", ErrInvalidHello, len(capabilities), MaxCapabilities)
	}
	for _, capability := range capabilities {
		if err := validateCapability(capability); err != nil {
			return hello, err
		}
	}
	hello.Capabilities = slices.Clone(capabilities)
	return hello, nil
}

// localHello returns the Hello message of this session.
func (s *Session) localHello() *wirev1.ControlMessage {
	hello := &wirev1.Hello{
		ProtocolVersion: ProtocolVersion,
		Capabilities:    slices.Clone(s.cfg.Capabilities),
		Name:            s.cfg.Name,
		Platform:        protoconv.PlatformToProto(s.cfg.Platform),
		SelfPolicy:      &wirev1.PeerPolicy{RequireConfirmation: s.cfg.RequireConfirmation},
	}
	return &wirev1.ControlMessage{Body: &wirev1.ControlMessage_Hello{Hello: hello}}
}

// validateName checks a device name taken from Hello.
func validateName(name string) error {
	if len(name) > MaxNameLength || !utf8.ValidString(name) {
		return fmt.Errorf("%w: invalid name of %d bytes", ErrInvalidHello, len(name))
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return fmt.Errorf("%w: control character in the name", ErrInvalidHello)
		}
	}
	return nil
}

// validateCapability checks one capability name taken from Hello.
func validateCapability(capability string) error {
	if capability == "" || len(capability) > MaxCapabilityLength || !utf8.ValidString(capability) {
		return fmt.Errorf("%w: invalid capability of %d bytes", ErrInvalidHello, len(capability))
	}
	return nil
}
