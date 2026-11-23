package clipboard

import (
	"context"
	"errors"
)

// errValueTooLarge reports a value above MaxValueBytes.
var errValueTooLarge = errors.New("clipboard: value is larger than the limit")

// chunkSource delivers a clipboard value part by part. A single property is one
// part; the INCR protocol is a sequence of parts.
type chunkSource interface {
	// next returns the next part of the value and whether more parts follow.
	next(ctx context.Context) (chunk []byte, more bool, err error)
}

// readValue collects the parts of a value up to the limit.
func readValue(ctx context.Context, src chunkSource, limit int) ([]byte, error) {
	var value []byte
	for {
		chunk, more, err := src.next(ctx)
		if err != nil {
			return nil, err
		}
		if len(value)+len(chunk) > limit {
			return nil, errValueTooLarge
		}
		value = append(value, chunk...)
		if !more {
			return value, nil
		}
	}
}

// wholeSource is a value that arrives in one part.
type wholeSource struct {
	data []byte
}

// next returns the whole value once.
func (s wholeSource) next(context.Context) ([]byte, bool, error) {
	return s.data, false, nil
}
