package discovery

import (
	"strings"
	"testing"
	"time"
)

// testKey is the key used by the tag vectors.
var testKey = []byte("0123456789abcdef0123456789abcdef")

// epochTime returns the start of epoch.
func epochTime(epoch int64) time.Time {
	return time.Unix(epoch*int64(EpochPeriod/time.Second), 0).UTC()
}

func TestTagVectors(t *testing.T) {
	vectors := []struct {
		epoch int64
		tag   string
	}{
		{0, "r2ffkbe3kxzai"},
		{1, "lm6xtpxkblwgm"},
		{1000, "36xx75urveyaq"},
		{2945376, "gu4wgfcd55tqa"},
	}

	for _, v := range vectors {
		if got := Tag(testKey, epochTime(v.epoch)); got != v.tag {
			t.Errorf("Tag at epoch %d = %q, want %q", v.epoch, got, v.tag)
		}
	}
}

func TestTagLength(t *testing.T) {
	tag := Tag(testKey, epochTime(42))
	if len(tag) != TagLength {
		t.Fatalf("tag %q has %d characters, want %d", tag, len(tag), TagLength)
	}
}

func TestTagEmptyKey(t *testing.T) {
	if tag := Tag(nil, epochTime(42)); tag != "" {
		t.Fatalf("Tag without a key = %q, want an empty tag", tag)
	}
	if MatchTag(nil, "r2ffkbe3kxzai", epochTime(0)) {
		t.Fatal("MatchTag without a key matched")
	}
}

func TestTagEpochBoundaries(t *testing.T) {
	const epoch = 1000
	start := epochTime(epoch)
	tag := Tag(testKey, start)

	if got := Tag(testKey, start.Add(599*time.Second)); got != tag {
		t.Fatalf("tag changed inside the epoch: %q, want %q", got, tag)
	}
	if got := Tag(testKey, start.Add(EpochPeriod)); got == tag {
		t.Fatal("tag did not change in the next epoch")
	}

	cases := []struct {
		name string
		at   time.Time
		want bool
	}{
		{"same epoch", start, true},
		{"last second of the epoch", start.Add(599 * time.Second), true},
		{"next epoch", start.Add(EpochPeriod), true},
		{"previous epoch", start.Add(-EpochPeriod), true},
		{"two epochs ahead", start.Add(2 * EpochPeriod), false},
		{"two epochs behind", start.Add(-2 * EpochPeriod), false},
	}
	for _, c := range cases {
		if got := MatchTag(testKey, tag, c.at); got != c.want {
			t.Errorf("MatchTag at %s = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestMatchTagInvalid(t *testing.T) {
	now := epochTime(1000)
	tag := Tag(testKey, now)

	if MatchTag([]byte("another key"), tag, now) {
		t.Fatal("matched a tag of another key")
	}
	if MatchTag(testKey, "", now) {
		t.Fatal("matched an empty tag")
	}
	if MatchTag(testKey, tag[:len(tag)-1], now) {
		t.Fatal("matched a truncated tag")
	}
	if MatchTag(testKey, tag+"x", now) {
		t.Fatal("matched a long tag")
	}
	if !MatchTag(testKey, strings.ToUpper(tag), now) {
		t.Fatal("did not match an upper case tag")
	}
}
