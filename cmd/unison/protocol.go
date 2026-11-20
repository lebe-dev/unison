package main

import (
	"github.com/lebe-dev/unison/core/protocol"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
)

// The protocol notices go through info, so with --json they land on stderr and
// stdout keeps carrying only JSON.

// writeDaemonProtocolNotice warns when the daemon speaks a newer protocol than
// this client, because then the client has to be updated.
func (a *app) writeDaemonProtocolNotice(status *localv1.GetStatusResponse) {
	if protocol.Compare(status.GetProtocolVersion()) != protocol.PeerNewer {
		return
	}
	a.info("this client speaks protocol %d, the daemon speaks protocol %d: update this client",
		protocol.Version, status.GetProtocolVersion())
}

// writeDeviceProtocolNotices warns about every device that speaks a newer
// protocol than this client.
func (a *app) writeDeviceProtocolNotices(devices []*localv1.Device) {
	for _, device := range devices {
		if !device.GetProtocolNewer() {
			continue
		}
		a.info("device %s speaks a newer protocol (%d); update this client",
			deviceLabel(device.GetDeviceId(), device.GetName()), device.GetProtocolVersion())
	}
}

// writeRelayProtocolNotice warns when the relay speaks a newer protocol than
// this client.
func (a *app) writeRelayProtocolNotice(relay *localv1.Relay) {
	if !relay.GetProtocolNewer() {
		return
	}
	a.info("the relay speaks a newer protocol (%d); update this client", relay.GetProtocolVersion())
}
