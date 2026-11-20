package node

import (
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/protocol"
)

// TestLinkProtocolVersion checks that the version of the peer reaches the live
// state of the link, which is what the local API reports.
func TestLinkProtocolVersion(t *testing.T) {
	link := newLink(model.DeviceID{1}, time.Unix(1_700_000_000, 0))
	link.setProtocolVersion(protocol.Version + 1)

	state := link.state()
	if !state.ProtocolKnown || state.ProtocolVersion != protocol.Version+1 {
		t.Fatalf("state = %+v, want protocol version %d", state, protocol.Version+1)
	}
	if state := newLink(model.DeviceID{2}, time.Now()).state(); state.ProtocolKnown {
		t.Fatalf("state = %+v, want no protocol version", state)
	}
}

// TestTakesOver checks which connection a link keeps when another one arrives.
func TestTakesOver(t *testing.T) {
	low := model.DeviceID{1}
	high := model.DeviceID{2}

	cases := []struct {
		name            string
		local, peer     model.DeviceID
		incoming        bool
		currentIncoming bool
		current, next   model.Path
		want            bool
	}{
		{
			name:  "outgoing after outgoing",
			local: low, peer: high, incoming: false, currentIncoming: false,
			current: model.PathLAN, next: model.PathLAN, want: false,
		},
		{
			name:  "incoming after incoming",
			local: low, peer: high, incoming: true, currentIncoming: true,
			current: model.PathLAN, next: model.PathLAN, want: false,
		},
		{
			name:  "incoming of the lower peer replaces our connection",
			local: low, peer: high, incoming: true, currentIncoming: true,
			current: model.PathRelay, next: model.PathLAN, want: true,
		},
		{
			name:  "local dial replaces the connection of the higher peer",
			local: low, peer: high, incoming: false, currentIncoming: true,
			current: model.PathLAN, next: model.PathLAN, want: true,
		},
		{
			name:  "simultaneous dial keeps the connection of the lower peer",
			local: low, peer: high, incoming: true, currentIncoming: false,
			current: model.PathLAN, next: model.PathLAN, want: false,
		},
		{
			name:  "the connection of the lower peer stays",
			local: high, peer: low, incoming: false, currentIncoming: true,
			current: model.PathLAN, next: model.PathLAN, want: false,
		},
		{
			name:  "local network replaces the relay path",
			local: high, peer: low, incoming: false, currentIncoming: false,
			current: model.PathRelay, next: model.PathLAN, want: true,
		},
		{
			name:  "relay does not replace the local network path",
			local: high, peer: low, incoming: false, currentIncoming: false,
			current: model.PathLAN, next: model.PathRelay, want: false,
		},
	}

	for _, c := range cases {
		if got := takesOver(c.local, c.peer, c.incoming, c.currentIncoming, c.current, c.next); got != c.want {
			t.Errorf("%s: takesOver = %v, want %v", c.name, got, c.want)
		}
	}
}
