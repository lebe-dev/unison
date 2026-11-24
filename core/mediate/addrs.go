package mediate

import (
	"fmt"
	"net/netip"
	"unicode"
	"unicode/utf8"

	"github.com/lebe-dev/unison/core/model"
)

// validateName checks a display name announced by the peer or the mediator.
func validateName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: empty name", ErrInvalidOffer)
	}
	if len(name) > MaxNameLength || !utf8.ValidString(name) {
		return fmt.Errorf("%w: name of %d bytes", ErrInvalidOffer, len(name))
	}
	for _, r := range name {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return fmt.Errorf("%w: invisible character in the name", ErrInvalidOffer)
		}
	}
	return nil
}

// normalizeAddrs validates address hints and returns the at most MaxAddrs they
// keep. A hint is an IP literal with a port. Names, zones, unspecified,
// multicast and loopback addresses are dropped; loopback is kept only when the
// signalling session itself runs over loopback, which a development setup uses.
func normalizeAddrs(addrs []string, allowLoopback bool) []string {
	if len(addrs) > MaxAddrs {
		return nil
	}
	out := make([]string, 0, len(addrs))
	for _, raw := range addrs {
		if len(raw) == 0 || len(raw) > MaxAddrLength {
			continue
		}
		addrPort, err := netip.ParseAddrPort(raw)
		if err != nil || addrPort.Port() == 0 {
			continue
		}
		addr := addrPort.Addr()
		switch {
		case addr.Zone() != "":
			continue
		case addr.IsUnspecified(), addr.IsMulticast(), addr.IsLinkLocalMulticast():
			continue
		case addr.IsLoopback() && !allowLoopback:
			continue
		}
		out = append(out, addrPort.String())
		if len(out) == MaxAddrs {
			break
		}
	}
	return out
}

// sanitizeName returns the name when it passes validation, or an empty name.
// The name of an offer is display-only, so an invalid one is dropped instead of
// the whole offer.
func sanitizeName(name string) string {
	if validateName(name) != nil {
		return ""
	}
	return name
}

// platformByte encodes a platform for a transcript.
func platformByte(platform model.Platform) byte {
	return byte(platform)
}

// roleByte encodes a role for a transcript.
func roleByte(role model.Role) byte {
	return byte(role)
}
