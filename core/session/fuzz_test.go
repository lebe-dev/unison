package session

import (
	"testing"

	"google.golang.org/protobuf/proto"

	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// FuzzParseHello checks that an arbitrary Hello is either refused or announces
// a name, a platform and capabilities inside the limits.
func FuzzParseHello(f *testing.F) {
	seed := &wirev1.Hello{
		ProtocolVersion: ProtocolVersion,
		Capabilities:    []string{"clip.image", "share"},
		Name:            "workstation",
		Platform:        wirev1.Platform_PLATFORM_MACOS,
	}
	if data, err := proto.Marshal(seed); err == nil {
		f.Add(data)
	}
	f.Add([]byte{})
	f.Add([]byte{0x08, 0x01})
	f.Add([]byte{0x08, 0x01, 0x1a, 0x02, 0x08, 0x01})
	f.Add([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x01})

	f.Fuzz(func(t *testing.T, data []byte) {
		msg := &wirev1.Hello{}
		if err := proto.Unmarshal(data, msg); err != nil {
			return
		}
		hello, err := parseHello(msg)
		if err != nil {
			return
		}
		if len(hello.Name) > MaxNameLength {
			t.Fatalf("accepted a name of %d bytes", len(hello.Name))
		}
		if len(hello.Capabilities) > MaxCapabilities {
			t.Fatalf("accepted %d capabilities", len(hello.Capabilities))
		}
		for _, capability := range hello.Capabilities {
			if capability == "" || len(capability) > MaxCapabilityLength {
				t.Fatalf("accepted a capability of %d bytes", len(capability))
			}
		}
	})
}

// FuzzStreamHeader checks that an arbitrary stream header is either refused or
// can be routed to a handler.
func FuzzStreamHeader(f *testing.F) {
	seed := &wirev1.StreamHeader{
		Kind:        wirev1.StreamKind_STREAM_KIND_REQUEST,
		TransferId:  "0d1f0e3c",
		PayloadType: "blob.request",
	}
	if data, err := proto.Marshal(seed); err == nil {
		f.Add(data)
	}
	f.Add([]byte{})
	f.Add([]byte{0x08, 0x01})
	f.Add([]byte{0x08, 0x02, 0x32, 0x03, 0x61, 0x62, 0x63})

	f.Fuzz(func(t *testing.T, data []byte) {
		header := &wirev1.StreamHeader{}
		if err := proto.Unmarshal(data, header); err != nil {
			return
		}
		if err := validateStreamHeader(header); err != nil {
			return
		}
		if len(header.GetPayloadType()) == 0 || len(header.GetPayloadType()) > MaxPayloadTypeLength {
			t.Fatalf("accepted a payload type of %d bytes", len(header.GetPayloadType()))
		}
		if len(header.GetTransferId()) > MaxTransferIDLength {
			t.Fatalf("accepted a transfer id of %d bytes", len(header.GetTransferId()))
		}

		for _, kind := range []wirev1.StreamKind{
			wirev1.StreamKind_STREAM_KIND_REQUEST,
			wirev1.StreamKind_STREAM_KIND_TRANSFER,
		} {
			got, err := streamHeader(header, kind)
			if err != nil {
				continue
			}
			if got.GetKind() != kind {
				t.Fatalf("stream header of kind %s was sent as %s", kind, got.GetKind())
			}
		}
	})
}
