package session

import (
	"bytes"
	"errors"
	"io"
	"testing"

	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// TestFrameRoundTrip checks that a message survives a frame.
func TestFrameRoundTrip(t *testing.T) {
	sent := clipAnnounce(3)
	var buf bytes.Buffer
	if err := writeFrame(&buf, sent, MaxControlMessage); err != nil {
		t.Fatalf("write frame: %v", err)
	}

	got := &wirev1.ControlMessage{}
	if err := newFrameReader(&buf).readMessage(MaxControlMessage, got); err != nil {
		t.Fatalf("read message: %v", err)
	}
	if got.GetClipAnnounce().GetItem().GetSeq() != 3 {
		t.Fatalf("sequence = %d, want 3", got.GetClipAnnounce().GetItem().GetSeq())
	}
}

// TestFrameSizeLimit checks that a payload over the limit is refused before it
// is allocated.
func TestFrameSizeLimit(t *testing.T) {
	var buf bytes.Buffer
	buf.Write([]byte{0xff, 0xff, 0x7f})

	_, err := newFrameReader(&buf).readFrame(MaxControlMessage)
	if !errors.Is(err, ErrFrame) {
		t.Fatalf("read error = %v, want ErrFrame", err)
	}

	big := clipAnnounce(1)
	big.GetClipAnnounce().Item.Representations = []*wirev1.Representation{{InlineBytes: make([]byte, MaxControlMessage)}}
	if err := writeFrame(io.Discard, big, MaxControlMessage); !errors.Is(err, ErrMessageTooLarge) {
		t.Fatalf("write error = %v, want ErrMessageTooLarge", err)
	}
}

// TestFrameTruncated checks that a frame without its payload is reported.
func TestFrameTruncated(t *testing.T) {
	reader := newFrameReader(bytes.NewReader([]byte{0x04, 0x01, 0x02}))
	if _, err := reader.readFrame(MaxControlMessage); !errors.Is(err, ErrFrame) {
		t.Fatalf("read error = %v, want ErrFrame", err)
	}
}

// TestFrameZeroLength checks that an empty frame is refused.
func TestFrameZeroLength(t *testing.T) {
	reader := newFrameReader(bytes.NewReader([]byte{0x00}))
	if _, err := reader.readFrame(MaxControlMessage); !errors.Is(err, ErrFrame) {
		t.Fatalf("read error = %v, want ErrFrame", err)
	}
}

// FuzzReadFrame checks that arbitrary input never makes the framing allocate
// over the limit or panic.
func FuzzReadFrame(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0x00})
	f.Add([]byte{0x01, 0x08})
	f.Add([]byte{0x02, 0x08, 0x01})
	f.Add([]byte{0xff, 0xff, 0x7f})

	f.Fuzz(func(t *testing.T, data []byte) {
		const limit = 64

		sizeReader := newFrameReader(bytes.NewReader(data))
		for range 4 {
			frame, err := sizeReader.readFrame(limit)
			if err != nil {
				break
			}
			if len(frame) > limit {
				t.Fatalf("frame of %d bytes over the limit %d", len(frame), limit)
			}
		}

		messageReader := newFrameReader(bytes.NewReader(data))
		for range 4 {
			msg := &wirev1.ControlMessage{}
			if err := messageReader.readMessage(limit, msg); err != nil {
				if !errors.Is(err, ErrFrame) && !errors.Is(err, ErrMessage) && !errors.Is(err, io.EOF) {
					t.Fatalf("unexpected error %v", err)
				}
				return
			}
		}
	})
}
