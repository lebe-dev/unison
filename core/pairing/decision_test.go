package pairing

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/quic-go/quic-go"
)

// TestDecisionError checks the message of a pairing that the other device
// abandoned: a closed stream is not a defect of this device.
func TestDecisionError(t *testing.T) {
	tests := map[string]struct {
		err  error
		want string
	}{
		"eof":        {err: io.EOF, want: "the other device closed the connection"},
		"short read": {err: io.ErrUnexpectedEOF, want: "the other device closed the connection"},
		"cancelled":  {err: &quic.StreamError{ErrorCode: 0}, want: "the other device closed the connection"},
		"protocol":   {err: &quic.StreamError{ErrorCode: 3}, want: "read decision"},
		"other":      {err: errors.New("boom"), want: "read decision"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			err := decisionError(test.err)
			if err == nil {
				t.Fatal("decisionError = nil")
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("decisionError = %q, want %q in it", err, test.want)
			}
		})
	}
}
