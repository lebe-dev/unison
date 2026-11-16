package relay

import (
	"encoding/binary"
	"time"

	"lukechampine.com/blake3"
)

// mailboxIDLen is the size of a mailbox id on the wire.
const mailboxIDLen = 16

// dayLength is the length of a mailbox day.
const dayLength = 24 * time.Hour

// ID is the identifier of a relay mailbox. The relay treats it as opaque.
type ID [mailboxIDLen]byte

// String returns the lower case hex form of the id.
func (m ID) String() string {
	const digits = "0123456789abcdef"
	out := make([]byte, 2*mailboxIDLen)
	for i, b := range m {
		out[2*i] = digits[b>>4]
		out[2*i+1] = digits[b&0x0f]
	}
	return string(out)
}

// MailboxID returns the mailbox of a pair on the UTC day of t. The id is
// BLAKE3(rendezvous_key || day)[:16], where day counts the days since the Unix
// epoch as a big-endian 64 bit integer. The hash hides the key, and every day
// gives an unrelated id, so the relay cannot follow a pair across days.
func MailboxID(rendezvousKey []byte, t time.Time) ID {
	hash := blake3.New(32, nil)
	_, _ = hash.Write(rendezvousKey)

	var day [8]byte
	binary.BigEndian.PutUint64(day[:], uint64(t.UTC().Unix()/int64(dayLength/time.Second)))
	_, _ = hash.Write(day[:])

	var id ID
	copy(id[:], hash.Sum(nil))
	return id
}

// Mailboxes returns the mailboxes a pair registers at t: the current UTC day
// and, once midnight is within rollover, the next one, so a pair stays
// reachable across the day boundary.
func Mailboxes(rendezvousKey []byte, t time.Time, rollover time.Duration) []ID {
	utc := t.UTC()
	ids := []ID{MailboxID(rendezvousKey, utc)}
	if rollover > 0 {
		if next := nextMidnight(utc); next.Sub(utc) <= rollover {
			ids = append(ids, MailboxID(rendezvousKey, next))
		}
	}
	return ids
}

// nextMidnight returns the next UTC midnight after t.
func nextMidnight(t time.Time) time.Time {
	utc := t.UTC()
	return time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC).Add(dayLength)
}
