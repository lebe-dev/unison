package main

import (
	"fmt"
	"net"
	"net/netip"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// Address ranks of one device, from the first to dial to the last.
const (
	rankGlobal4 = iota
	rankGlobal6
	rankName
	rankLinkLocal
	rankLoopback
	rankOther
)

// pairingCandidate is one device that is open for pairing. Discovery reports an
// address per interface, so the addresses of a device are grouped by the port of
// its pairing listener: one window listens on one port.
type pairingCandidate struct {
	// Port is the port of the pairing listener of the device.
	Port int
	// Addrs are the addresses of the device, the first one to dial first.
	Addrs []string
}

// groupCandidates groups the discovered addresses into devices. The name and the
// device ID of a device are known only after pairing, so the port of the
// pairing listener is what tells two devices apart.
func groupCandidates(addrs []string) []pairingCandidate {
	byPort := make(map[int][]string, len(addrs))
	for _, addr := range addrs {
		port, ok := portOfAddr(addr)
		if !ok {
			continue
		}
		if !slices.Contains(byPort[port], addr) {
			byPort[port] = append(byPort[port], addr)
		}
	}

	ports := make([]int, 0, len(byPort))
	for port := range byPort {
		ports = append(ports, port)
	}
	sort.Ints(ports)

	candidates := make([]pairingCandidate, 0, len(ports))
	for _, port := range ports {
		list := byPort[port]
		sort.Slice(list, func(i, j int) bool {
			left, right := addrRank(list[i]), addrRank(list[j])
			if left != right {
				return left < right
			}
			return list[i] < list[j]
		})
		candidates = append(candidates, pairingCandidate{Port: port, Addrs: list})
	}
	return candidates
}

// portOfAddr returns the port of a host:port address.
func portOfAddr(addr string) (int, bool) {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return 0, false
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return 0, false
	}
	return number, true
}

// addrRank orders the addresses of one device: a global address before a
// link-local one, and loopback last.
func addrRank(addr string) int {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return rankOther
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return rankName
	}
	switch {
	case ip.IsLoopback():
		return rankLoopback
	case ip.IsLinkLocalUnicast():
		return rankLinkLocal
	case ip.Is4():
		return rankGlobal4
	default:
		return rankGlobal6
	}
}

// showCandidates prints one line per device and asks which one to pair with.
func (a *app) showCandidates(candidates []pairingCandidate) {
	a.info("found %s:", countText(len(candidates), "device", "devices"))
	for i, candidate := range candidates {
		a.info("  %d. port %d, %s: %s", i+1, candidate.Port,
			countText(len(candidate.Addrs), "address", "addresses"),
			strings.Join(candidate.Addrs, ", "))
	}
	a.info("a device has no name until it is paired, so each one is listed by the port of its window")
	_, _ = fmt.Fprintf(a.stderr, "choose a device [1-%d], an address [1.1-%d.%d] or Enter to wait for an incoming request: ",
		len(candidates), len(candidates), len(candidates[len(candidates)-1].Addrs))
}

// chooseCandidate returns the address the line selects. An empty line returns an
// empty address, which means waiting for an incoming request. A device is
// selected by its number, and one of its addresses by the number of the device
// and the number of the address. The second value reports whether the line was
// understood.
func chooseCandidate(line string, candidates []pairingCandidate) (string, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", true
	}
	device, addr, hasAddr := strings.Cut(line, ".")
	index, err := strconv.Atoi(device)
	if err != nil || index < 1 || index > len(candidates) {
		return "", false
	}
	chosen := candidates[index-1]
	if !hasAddr {
		return chosen.Addrs[0], true
	}
	index, err = strconv.Atoi(addr)
	if err != nil || index < 1 || index > len(chosen.Addrs) {
		return "", false
	}
	return chosen.Addrs[index-1], true
}
