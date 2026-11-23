package relay

import (
	"bytes"
	"testing"

	relayv1 "github.com/lebe-dev/unison/proto/unison/relay/v1"
)

// FuzzRelayFrame checks that a frame of a relay is either refused or carries
// fields inside the limits of the client.
func FuzzRelayFrame(f *testing.F) {
	seed, err := datagramFrame(ID{1}, []byte("payload"))
	if err != nil {
		f.Fatalf("build a datagram: %v", err)
	}
	f.Add(seed)
	result, err := marshalFrame(&relayv1.Frame{Body: &relayv1.Frame_RegisterResult{
		RegisterResult: &relayv1.RegisterResult{
			Status: relayv1.RelayStatus_RELAY_STATUS_OK,
			Limits: &relayv1.Limits{
				MaxDatagramBytes: 1400,
				MaxMailboxes:     8,
				RatePerSecond:    200,
				RateBurst:        400,
			},
		},
	}})
	if err == nil {
		f.Add(result)
	}
	f.Add([]byte{})
	f.Add([]byte{0x0a, 0x00})
	f.Add(make([]byte, mailboxIDLen))

	f.Fuzz(func(t *testing.T, data []byte) {
		frame, err := parseFrame(data, maxRelayDatagrams+frameOverheadBytes)
		if err != nil {
			return
		}
		if datagram := frame.GetDatagram(); datagram != nil {
			if _, ok := mailboxIDOf(datagram.GetMailboxId()); ok && len(datagram.GetPayload()) > maxRelayDatagrams {
				t.Fatalf("accepted a datagram of %d bytes", len(datagram.GetPayload()))
			}
		}
		if result := frame.GetRegisterResult(); result != nil {
			reg, err := registrationOf(result)
			if err != nil {
				return
			}
			limits := reg.Limits
			if limits.MaxDatagramBytes <= 0 || limits.MaxDatagramBytes > maxRelayDatagrams {
				t.Fatalf("accepted %d datagram bytes", limits.MaxDatagramBytes)
			}
			if limits.MaxMailboxes <= 0 || limits.MaxMailboxes > maxMailboxes {
				t.Fatalf("accepted %d mailboxes", limits.MaxMailboxes)
			}
			if reg.ProtocolVersion == 0 {
				t.Fatal("accepted a registration without a protocol version")
			}
		}
		_, _ = readStreamFrame(bytes.NewReader(data), maxRelayDatagrams+frameOverheadBytes)
	})
}
