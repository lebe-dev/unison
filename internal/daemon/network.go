package daemon

import (
	"context"
	"net"
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/lebe-dev/unison/core/model"
)

const (
	// networkWatchInterval is how often the daemon compares the addresses of
	// its interfaces.
	networkWatchInterval = 5 * time.Second
	// networkSettleDelay is how long the daemon lets a network change settle,
	// so one Wi-Fi switch is not reported twice.
	networkSettleDelay = 2 * time.Second
)

// watchNetwork reports every change of the local addresses to report until ctx
// ends, with the state of the new network. The desktop has no network monitor
// of the platform, so the daemon follows the interfaces itself.
func watchNetwork(ctx context.Context, report func(model.NetworkState)) {
	watchAddresses(ctx, networkWatchInterval, networkSettleDelay, localAddresses, func(fingerprint string) {
		report(networkStateOf(fingerprint))
	})
}

// networkStateOf returns the state of a network with the fingerprint of
// localAddresses. A device without an address another device can use is
// offline.
func networkStateOf(fingerprint string) model.NetworkState {
	return model.NetworkState{Online: fingerprint != ""}
}

// watchAddresses reports the new fingerprint once a change settled. The first
// fingerprint is the starting point and is not reported.
func watchAddresses(ctx context.Context, interval, settle time.Duration, fingerprint func() string, report func(string)) {
	last := fingerprint()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		next := fingerprint()
		if next == last {
			continue
		}
		if !settleAddresses(ctx, settle) {
			return
		}
		// The addresses may have moved on while the change was settling.
		next = fingerprint()
		if next == last {
			continue
		}
		last = next
		report(next)
	}
}

// settleAddresses waits for the settle delay and reports whether ctx is still
// alive.
func settleAddresses(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// localAddresses describes the addresses of this device that another device can
// use, ordered by interface. A change of the string is a change of the network.
func localAddresses() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}

	var b strings.Builder
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs := peerAddrs(&iface)
		if len(addrs) == 0 {
			continue
		}
		b.WriteString(iface.Name)
		b.WriteByte(':')
		b.WriteString(strings.Join(addrs, ","))
		b.WriteByte('|')
	}
	return b.String()
}

// peerAddrs returns the addresses of the interface that another device can use,
// sorted so the order of the system does not look like a change.
func peerAddrs(iface *net.Interface) []string {
	list, err := iface.Addrs()
	if err != nil {
		return nil
	}

	addrs := make([]string, 0, len(list))
	for _, entry := range list {
		if !usableAddr(entry) {
			continue
		}
		addrs = append(addrs, entry.String())
	}
	slices.Sort(addrs)
	return addrs
}

// usableAddr reports whether another device can reach this address: loopback,
// link local and multicast addresses stay inside one host or one link.
func usableAddr(entry net.Addr) bool {
	ipNet, ok := entry.(*net.IPNet)
	if !ok {
		return false
	}
	addr, ok := netip.AddrFromSlice(ipNet.IP)
	if !ok {
		return false
	}
	addr = addr.Unmap()
	if addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsUnspecified() || addr.IsMulticast() {
		return false
	}
	return addr.IsGlobalUnicast()
}
