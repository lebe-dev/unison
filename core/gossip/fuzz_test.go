package gossip

import (
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/lebe-dev/unison/core/trust"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// FuzzGossipDigest checks that an arbitrary digest is either refused or yields
// heads inside the limits.
func FuzzGossipDigest(f *testing.F) {
	subject := make([]byte, 32)
	subject[0] = 1
	seed := &wirev1.GossipDigest{
		Introductions:     []*wirev1.IntroductionHead{{Subject: subject, TsUnixMs: 1763000000000}},
		Revocations:       []*wirev1.RevocationHead{{Subject: subject, TsUnixMs: 1763000000000}},
		DiscoveryTsUnixMs: 1763000000000,
	}
	if data, err := proto.Marshal(seed); err == nil {
		f.Add(data)
	}
	f.Add([]byte{})
	f.Add([]byte{0x0a, 0x02, 0x0a, 0x00})

	f.Fuzz(func(t *testing.T, data []byte) {
		digest := &wirev1.GossipDigest{}
		if err := proto.Unmarshal(data, digest); err != nil {
			return
		}
		state, err := parseDigest(digest)
		if err != nil {
			return
		}
		if len(state.introductions) > MaxDigestHeads {
			t.Fatalf("accepted %d introduction heads", len(state.introductions))
		}
		if len(state.revocations) > MaxDigestHeads {
			t.Fatalf("accepted %d revocation heads", len(state.revocations))
		}
		for subject, at := range state.introductions {
			if subject.IsZero() || at <= 0 {
				t.Fatalf("accepted the introduction head %s at %d", subject, at)
			}
		}
		for subject, at := range state.revocations {
			if subject.IsZero() || at <= 0 {
				t.Fatalf("accepted the revocation head %s at %d", subject, at)
			}
		}
	})
}

// FuzzGossipRecords checks that every record of an arbitrary message is either
// refused on its own or describes a consistent trust record.
func FuzzGossipRecords(f *testing.F) {
	subject := make([]byte, 32)
	subject[0] = 1
	issuer := make([]byte, 32)
	issuer[0] = 2
	seed := &wirev1.GossipRecords{
		Revocations: []*wirev1.Revocation{{Subject: subject, Issuer: issuer, TsUnixMs: 1763000000000}},
		Group: &wirev1.GroupData{
			DiscoveryKey:      make([]byte, 32),
			DiscoveryTsUnixMs: 1763000000000,
			RelayUrl:          "wss://relay.example",
			RelayToken:        []byte("token"),
			RelayTsUnixMs:     1763000000000,
		},
	}
	if data, err := proto.Marshal(seed); err == nil {
		f.Add(data)
	}
	f.Add([]byte{})
	f.Add([]byte{0x12, 0x00})

	f.Fuzz(func(t *testing.T, data []byte) {
		records := &wirev1.GossipRecords{}
		if err := proto.Unmarshal(data, records); err != nil {
			return
		}

		for _, record := range records.GetIntroductions() {
			intro, err := trust.IntroductionFromProto(record)
			if err != nil {
				continue
			}
			if intro.Subject.DeviceID.IsZero() || intro.Issuer.IsZero() {
				t.Fatalf("accepted an introduction without a subject or an issuer")
			}
		}
		for _, record := range records.GetRevocations() {
			revocation, err := trust.RevocationFromProto(record)
			if err != nil {
				continue
			}
			if revocation.Subject.IsZero() || revocation.Issuer.IsZero() {
				t.Fatalf("accepted a revocation without a subject or an issuer")
			}
		}
		if group := records.GetGroup(); group != nil {
			if _, err := groupFromProto(group); err != nil {
				return
			}
			if len(group.GetDiscoveryKey()) != 0 && len(group.GetDiscoveryKey()) != 32 {
				t.Fatalf("accepted a discovery key of %d bytes", len(group.GetDiscoveryKey()))
			}
		}
	})
}
