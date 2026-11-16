package pairing

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strings"

	"github.com/lebe-dev/unison/core/identity"
	"github.com/lebe-dev/unison/core/model"
)

// Errors reported by the QR code.
var (
	// ErrQR reports a pairing code that is malformed or does not carry the
	// expected fields.
	ErrQR = errors.New("pairing: bad pairing code")
)

// Layout of the pairing code.
const (
	// qrPrefix opens every pairing code.
	qrPrefix = "unison://pair?"
	// qrVersion is the version of the code layout.
	qrVersion = "1"
	// maxQRLength bounds the text of a code in bytes. The text reaches a camera
	// and a parser of another device, so its size is limited from the start.
	maxQRLength = 512
	// maxQRAddresses bounds the addresses of one code.
	maxQRAddresses = 4
	// maxAddressLength bounds one address in bytes.
	maxAddressLength = 64
)

// Parameter names of the pairing code.
const (
	qrParamVersion     = "v"
	qrParamDeviceID    = "id"
	qrParamFingerprint = "fp"
	qrParamToken       = "t"
	qrParamAddrs       = "a"
	qrParamRole        = "role"
)

// qrParams lists the parameters of a code. A code with another parameter, or
// without one of these, is refused.
var qrParams = [...]string{
	qrParamVersion,
	qrParamDeviceID,
	qrParamFingerprint,
	qrParamToken,
	qrParamAddrs,
	qrParamRole,
}

// QR is the content of a pairing QR code. The device that shows the code owns
// the pairing window it describes; the device that scans it connects to one of
// the addresses and sends the token.
type QR struct {
	// DeviceID identifies the device that shows the code.
	DeviceID model.DeviceID
	// Fingerprint is the SHA-256 of the TLS certificate of the pairing
	// listener. The scanning device accepts that certificate only.
	Fingerprint identity.CertFingerprint
	// Token is the one time token of the code.
	Token Token
	// Addrs are the addresses of the pairing listener.
	Addrs []string
	// Role is the role the showing device proposes for the pair.
	Role model.Role
}

// URI returns the text of the code. The token is a secret of the pairing
// window, so the text must not be logged or stored.
func (q QR) URI() (string, error) {
	addrs, err := q.checkedAddrs()
	if err != nil {
		return "", err
	}
	if q.DeviceID.IsZero() {
		return "", fmt.Errorf("%w: device id is missing", ErrQR)
	}
	if q.Fingerprint.IsZero() {
		return "", fmt.Errorf("%w: certificate fingerprint is missing", ErrQR)
	}
	if q.Token.IsZero() {
		return "", fmt.Errorf("%w: token is missing", ErrQR)
	}
	if q.Role != model.RolePersonal && q.Role != model.RoleFriends {
		return "", fmt.Errorf("%w: role %s cannot be proposed", ErrQR, q.Role)
	}

	uri := qrPrefix + qrParamVersion + "=" + qrVersion +
		"&" + qrParamDeviceID + "=" + q.DeviceID.String() +
		"&" + qrParamFingerprint + "=" + q.Fingerprint.String() +
		"&" + qrParamToken + "=" + q.Token.String() +
		"&" + qrParamAddrs + "=" + strings.Join(addrs, ",") +
		"&" + qrParamRole + "=" + q.Role.String()
	if len(uri) > maxQRLength {
		return "", fmt.Errorf("%w: code of %d bytes, limit %d", ErrQR, len(uri), maxQRLength)
	}
	return uri, nil
}

// ParseQR parses the text of a pairing code. It is strict: an unknown parameter,
// a missing parameter, a duplicate, a non canonical value or a code that is too
// long is refused.
func ParseQR(uri string) (QR, error) {
	if uri == "" {
		return QR{}, fmt.Errorf("%w: empty code", ErrQR)
	}
	if len(uri) > maxQRLength {
		return QR{}, fmt.Errorf("%w: code of %d bytes, limit %d", ErrQR, len(uri), maxQRLength)
	}
	rest, ok := strings.CutPrefix(uri, qrPrefix)
	if !ok {
		return QR{}, fmt.Errorf("%w: not a pairing code", ErrQR)
	}

	values, err := parseQRParams(rest)
	if err != nil {
		return QR{}, err
	}
	if version := values[qrParamVersion]; version != qrVersion {
		return QR{}, fmt.Errorf("%w: version %q", ErrQR, version)
	}

	id, err := parseLowerHexID(values[qrParamDeviceID])
	if err != nil {
		return QR{}, err
	}
	fingerprint, err := parseLowerHexFingerprint(values[qrParamFingerprint])
	if err != nil {
		return QR{}, err
	}
	token, err := ParseToken(values[qrParamToken])
	if err != nil {
		return QR{}, err
	}
	addrs, err := parseAddresses(values[qrParamAddrs])
	if err != nil {
		return QR{}, err
	}

	var role model.Role
	switch roleName := values[qrParamRole]; roleName {
	case model.RolePersonal.String():
		role = model.RolePersonal
	case model.RoleFriends.String():
		role = model.RoleFriends
	default:
		return QR{}, fmt.Errorf("%w: role %q", ErrQR, roleName)
	}

	qr := QR{DeviceID: id, Fingerprint: fingerprint, Token: token, Addrs: addrs, Role: role}
	// The builder refuses values the parser accepts, so a code that cannot be
	// written again is refused here rather than later.
	if _, err := qr.URI(); err != nil {
		return QR{}, err
	}
	return qr, nil
}

// parseQRParams splits the query of a code into its parameters and checks the
// set of names.
func parseQRParams(query string) (map[string]string, error) {
	if query == "" {
		return nil, fmt.Errorf("%w: no parameters", ErrQR)
	}
	values := make(map[string]string, len(qrParams))
	for _, part := range strings.Split(query, "&") {
		name, value, ok := strings.Cut(part, "=")
		if !ok || value == "" {
			return nil, fmt.Errorf("%w: parameter %q", ErrQR, part)
		}
		if !slices.Contains(qrParams[:], name) {
			return nil, fmt.Errorf("%w: unknown parameter %q", ErrQR, name)
		}
		if _, duplicate := values[name]; duplicate {
			return nil, fmt.Errorf("%w: duplicate parameter %q", ErrQR, name)
		}
		values[name] = value
	}
	if len(values) != len(qrParams) {
		return nil, fmt.Errorf("%w: %d parameters, want %d", ErrQR, len(values), len(qrParams))
	}
	return values, nil
}

// parseLowerHexID parses the device ID of a code.
func parseLowerHexID(value string) (model.DeviceID, error) {
	var id model.DeviceID
	if !isLowerHex(value, len(id)*2) {
		return id, fmt.Errorf("%w: device id", ErrQR)
	}
	parsed, err := model.ParseDeviceID(value)
	if err != nil {
		return id, fmt.Errorf("%w: %w", ErrQR, err)
	}
	return parsed, nil
}

// parseLowerHexFingerprint parses the certificate fingerprint of a code.
func parseLowerHexFingerprint(value string) (identity.CertFingerprint, error) {
	var fingerprint identity.CertFingerprint
	if !isLowerHex(value, len(fingerprint)*2) {
		return fingerprint, fmt.Errorf("%w: certificate fingerprint", ErrQR)
	}
	parsed, err := identity.ParseCertFingerprint(value)
	if err != nil {
		return fingerprint, fmt.Errorf("%w: %w", ErrQR, err)
	}
	return parsed, nil
}

// parseAddresses splits and checks the address list of a code.
func parseAddresses(value string) ([]string, error) {
	parts := strings.Split(value, ",")
	if len(parts) > maxQRAddresses {
		return nil, fmt.Errorf("%w: %d addresses, limit %d", ErrQR, len(parts), maxQRAddresses)
	}
	addrs := make([]string, 0, len(parts))
	for _, part := range parts {
		addr, err := parseAddress(part)
		if err != nil {
			return nil, err
		}
		if slices.Contains(addrs, addr) {
			continue
		}
		addrs = append(addrs, addr)
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("%w: no address", ErrQR)
	}
	return addrs, nil
}

// parseAddress checks an address of a pairing code and returns its canonical
// form: an IP literal with a port, without a zone.
func parseAddress(value string) (string, error) {
	if value == "" || len(value) > maxAddressLength {
		return "", fmt.Errorf("%w: address of %d bytes", ErrQR, len(value))
	}
	if strings.ContainsAny(value, "% \t") {
		return "", fmt.Errorf("%w: address %q", ErrQR, value)
	}
	addr, err := netip.ParseAddrPort(value)
	if err != nil {
		return "", fmt.Errorf("%w: address %q", ErrQR, value)
	}
	switch {
	case addr.Port() == 0:
		return "", fmt.Errorf("%w: address %q without a port", ErrQR, value)
	case addr.Addr().IsUnspecified():
		return "", fmt.Errorf("%w: address %q is not reachable", ErrQR, value)
	}
	return addr.String(), nil
}

// checkedAddrs returns the addresses of the code, checked and without
// duplicates.
func (q QR) checkedAddrs() ([]string, error) {
	if len(q.Addrs) == 0 {
		return nil, fmt.Errorf("%w: no address", ErrQR)
	}
	if len(q.Addrs) > maxQRAddresses {
		return nil, fmt.Errorf("%w: %d addresses, limit %d", ErrQR, len(q.Addrs), maxQRAddresses)
	}
	addrs := make([]string, 0, len(q.Addrs))
	for _, value := range q.Addrs {
		addr, err := parseAddress(value)
		if err != nil {
			return nil, err
		}
		if slices.Contains(addrs, addr) {
			continue
		}
		addrs = append(addrs, addr)
	}
	return addrs, nil
}

// isLowerHex reports whether s is a lowercase hex string of exactly n bytes.
func isLowerHex(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// LocalAddresses returns the addresses of the local interfaces with the given
// port, IPv4 first. Loopback addresses are used only when the device has no
// other unicast address, and link-local addresses are skipped because they need
// a zone.
func LocalAddresses(port int) ([]string, error) {
	if port <= 0 || port > 65535 {
		return nil, fmt.Errorf("pairing: port %d", port)
	}
	interfaces, err := net.InterfaceAddrs()
	if err != nil {
		return nil, fmt.Errorf("pairing: list addresses: %w", err)
	}

	var global, loopback []netip.Addr
	for _, entry := range interfaces {
		ipNet, ok := entry.(*net.IPNet)
		if !ok {
			continue
		}
		addr, ok := netip.AddrFromSlice(ipNet.IP)
		if !ok {
			continue
		}
		addr = addr.Unmap()
		switch {
		case addr.IsLoopback():
			loopback = append(loopback, addr)
		case addr.IsLinkLocalUnicast(), !addr.IsGlobalUnicast():
			continue
		default:
			global = append(global, addr)
		}
	}

	unique := uniqueAddrs(global)
	if len(unique) == 0 {
		unique = uniqueAddrs(loopback)
	}
	addrs := make([]string, 0, min(len(unique), maxQRAddresses))
	for _, addr := range unique {
		if len(addrs) == maxQRAddresses {
			break
		}
		addrs = append(addrs, netip.AddrPortFrom(addr, uint16(port)).String())
	}
	if len(addrs) == 0 {
		return nil, errors.New("pairing: no local address")
	}
	return addrs, nil
}

// uniqueAddrs sorts the addresses, IPv4 first, and drops the duplicates.
func uniqueAddrs(addrs []netip.Addr) []netip.Addr {
	slices.SortFunc(addrs, func(a, b netip.Addr) int {
		if a.Is4() != b.Is4() {
			if a.Is4() {
				return -1
			}
			return 1
		}
		return a.Compare(b)
	})
	unique := make([]netip.Addr, 0, len(addrs))
	for _, addr := range addrs {
		if len(unique) > 0 && unique[len(unique)-1] == addr {
			continue
		}
		unique = append(unique, addr)
	}
	return unique
}
