package discovery

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"strings"
	"time"
)

const (
	// EpochPeriod is the lifetime of one tag epoch.
	EpochPeriod = 10 * time.Minute
	// TagLength is the length of the encoded tag.
	TagLength = 13
	// tagBytes is the number of HMAC bytes kept in a tag.
	tagBytes = 8
)

// tagEncoding is unpadded lowercase base32, as used in TXT records.
var tagEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// Epoch returns the tag epoch of t.
func Epoch(t time.Time) int64 {
	return t.Unix() / int64(EpochPeriod/time.Second)
}

// Tag returns the tag of key for the epoch of t. An empty key produces an
// empty tag, so a node without a group secret advertises no tag at all.
func Tag(key []byte, t time.Time) string {
	if len(key) == 0 {
		return ""
	}
	return tagForEpoch(key, Epoch(t))
}

// MatchTag reports whether tag was produced by key in the epoch of t, in the
// epoch before or in the epoch after. The window covers clock skew between
// nodes.
func MatchTag(key []byte, tag string, t time.Time) bool {
	if len(key) == 0 {
		return false
	}
	tag = strings.ToLower(tag)
	if len(tag) != TagLength {
		return false
	}

	epoch := Epoch(t)
	for _, e := range [...]int64{epoch - 1, epoch, epoch + 1} {
		if subtle.ConstantTimeCompare([]byte(tagForEpoch(key, e)), []byte(tag)) == 1 {
			return true
		}
	}
	return false
}

// MatchAnyTag reports whether tag was produced by any of keys. The group keeps
// the previous key of a rotation for a transition period, so the tags of both
// keys match.
func MatchAnyTag(keys [][]byte, tag string, t time.Time) bool {
	for _, key := range keys {
		if MatchTag(key, tag, t) {
			return true
		}
	}
	return false
}

// tagForEpoch returns the tag of key for epoch. The epoch is encoded as a
// big-endian integer.
func tagForEpoch(key []byte, epoch int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(epoch))

	mac := hmac.New(sha256.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	return encodeLabel(sum[:tagBytes])
}

// encodeLabel returns the unpadded lowercase base32 of b.
func encodeLabel(b []byte) string {
	return strings.ToLower(tagEncoding.EncodeToString(b))
}
