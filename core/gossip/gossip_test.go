package gossip

import (
	"bytes"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/trust"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// testDeviceID returns a deterministic device ID.
func testDeviceID(seed byte) model.DeviceID {
	return model.DeviceID(bytes.Repeat([]byte{seed}, 32))
}

// head builds an introduction head.
func head(subject model.DeviceID, at time.Time) trust.IntroductionHead {
	return trust.IntroductionHead{Subject: subject, IssuedAt: at}
}

func TestMissingIntroductions(t *testing.T) {
	at := time.UnixMilli(1_760_000_000_000)
	a, b, c := testDeviceID(1), testDeviceID(2), testDeviceID(3)
	heads := []trust.IntroductionHead{
		head(a, at),
		head(b, at),
		head(c, at.Add(-time.Hour)),
	}
	peer := map[model.DeviceID]int64{
		a: at.UnixMilli(),
		b: at.Add(-time.Minute).UnixMilli(),
	}

	missing := missingIntroductions(heads, peer)
	if !slices.Equal(missing, []model.DeviceID{b, c}) {
		t.Fatalf("missing introductions %v", missing)
	}

	if got := missingIntroductions(heads, nil); len(got) != 3 {
		t.Fatalf("a peer without heads gets %d introductions", len(got))
	}

	many := make([]trust.IntroductionHead, 0, MaxRecordsPerDigest+10)
	for i := range MaxRecordsPerDigest + 10 {
		id := testDeviceID(byte(i % 250))
		id[31] = byte(i)
		many = append(many, head(id, at))
	}
	if got := missingIntroductions(many, nil); len(got) != MaxRecordsPerDigest {
		t.Fatalf("the answer carries %d introductions, limit %d", len(got), MaxRecordsPerDigest)
	}
}

func TestMissingGroupData(t *testing.T) {
	at := time.UnixMilli(1_760_000_000_000)
	local := trust.GroupData{
		DiscoveryKey: bytes.Repeat([]byte{1}, 32),
		DiscoveryAt:  at,
		Relay:        trust.RelaySettings{URL: "https://relay.example", Token: "secret"},
		RelayAt:      at,
	}

	group := missingGroupData(local, &wirev1.GossipDigest{})
	if group == nil {
		t.Fatal("a peer without group data gets nothing")
	}
	if !bytes.Equal(group.GetDiscoveryKey(), local.DiscoveryKey) || group.GetDiscoveryTsUnixMs() != at.UnixMilli() {
		t.Fatalf("discovery key in %+v", group)
	}
	if group.GetRelayUrl() != local.Relay.URL || string(group.GetRelayToken()) != local.Relay.Token {
		t.Fatalf("relay settings in %+v", group)
	}

	upToDate := &wirev1.GossipDigest{
		DiscoveryTsUnixMs: at.UnixMilli(),
		RelayTsUnixMs:     at.UnixMilli(),
	}
	if group := missingGroupData(local, upToDate); group != nil {
		t.Fatalf("an up to date peer gets %+v", group)
	}
	if group := missingGroupData(trust.GroupData{}, &wirev1.GossipDigest{}); group != nil {
		t.Fatalf("a device without group data sends %+v", group)
	}
}

func TestParseDigest(t *testing.T) {
	at := time.UnixMilli(1_760_000_000_000)
	a := testDeviceID(1)

	heads, err := parseDigest(&wirev1.GossipDigest{Introductions: []*wirev1.IntroductionHead{
		{Subject: a.Bytes(), TsUnixMs: at.UnixMilli()},
		{Subject: a.Bytes(), TsUnixMs: at.Add(-time.Hour).UnixMilli()},
	}})
	if err != nil {
		t.Fatalf("parse digest: %v", err)
	}
	if len(heads) != 1 || heads[a] != at.UnixMilli() {
		t.Fatalf("parsed heads %v", heads)
	}

	cases := map[string]*wirev1.GossipDigest{
		"short subject": {Introductions: []*wirev1.IntroductionHead{{Subject: []byte{1}, TsUnixMs: at.UnixMilli()}}},
		"no time":       {Introductions: []*wirev1.IntroductionHead{{Subject: a.Bytes()}}},
		"too many":      {Introductions: make([]*wirev1.IntroductionHead, MaxDigestHeads+1)},
	}
	for name, digest := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseDigest(digest); !errors.Is(err, ErrProtocol) {
				t.Fatalf("digest %q: %v", name, err)
			}
		})
	}
}

func TestGroupFromProto(t *testing.T) {
	at := time.UnixMilli(1_760_000_000_000)
	message := &wirev1.GroupData{
		DiscoveryKey:      bytes.Repeat([]byte{1}, 32),
		DiscoveryTsUnixMs: at.UnixMilli(),
		RelayUrl:          "https://relay.example",
		RelayToken:        []byte("secret"),
		RelayTsUnixMs:     at.UnixMilli(),
	}
	data, err := groupFromProto(message)
	if err != nil {
		t.Fatalf("decode group data: %v", err)
	}
	if !bytes.Equal(data.DiscoveryKey, message.GetDiscoveryKey()) || !data.DiscoveryAt.Equal(at) {
		t.Fatalf("decoded discovery key %+v", data)
	}
	if data.Relay.URL != message.GetRelayUrl() || data.Relay.Token != "secret" || !data.RelayAt.Equal(at) {
		t.Fatalf("decoded relay settings %+v", data)
	}

	cases := map[string]*wirev1.GroupData{
		"short key":    {DiscoveryKey: []byte{1}},
		"long address": {RelayUrl: string(bytes.Repeat([]byte{'a'}, trust.MaxRelayURLLength+1))},
		"long token":   {RelayToken: bytes.Repeat([]byte{'a'}, trust.MaxRelayTokenLength+1)},
	}
	for name, message := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := groupFromProto(message); !errors.Is(err, ErrProtocol) {
				t.Fatalf("group data %q: %v", name, err)
			}
		})
	}
}
