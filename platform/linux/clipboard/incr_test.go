package clipboard

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

// fakeSource delivers a fixed sequence of chunks.
type fakeSource struct {
	chunks [][]byte
	err    error
}

func (f *fakeSource) next(context.Context) ([]byte, bool, error) {
	if len(f.chunks) == 0 {
		return nil, false, f.err
	}
	chunk := f.chunks[0]
	f.chunks = f.chunks[1:]
	return chunk, len(f.chunks) > 0 || f.err != nil, nil
}

func TestReadValueIncrements(t *testing.T) {
	src := &fakeSource{chunks: [][]byte{[]byte("hello "), []byte("incremental"), []byte(" world")}}
	got, err := readValue(context.Background(), src, MaxValueBytes)
	if err != nil {
		t.Fatalf("readValue: %v", err)
	}
	if want := "hello incremental world"; string(got) != want {
		t.Fatalf("readValue() = %q, want %q", got, want)
	}
}

func TestReadValueSinglePart(t *testing.T) {
	got, err := readValue(context.Background(), wholeSource{data: []byte("text")}, MaxValueBytes)
	if err != nil {
		t.Fatalf("readValue: %v", err)
	}
	if string(got) != "text" {
		t.Fatalf("readValue() = %q, want %q", got, "text")
	}
}

func TestReadValueEmpty(t *testing.T) {
	got, err := readValue(context.Background(), &fakeSource{}, MaxValueBytes)
	if err != nil {
		t.Fatalf("readValue: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("readValue() = %q, want empty", got)
	}
}

func TestReadValueLimit(t *testing.T) {
	src := &fakeSource{chunks: [][]byte{[]byte("12345"), []byte("67890")}}
	_, err := readValue(context.Background(), src, 8)
	if !errors.Is(err, errValueTooLarge) {
		t.Fatalf("readValue error = %v, want errValueTooLarge", err)
	}
}

func TestReadValueSourceError(t *testing.T) {
	want := errors.New("owner is gone")
	src := &fakeSource{chunks: [][]byte{[]byte("part")}, err: want}
	_, err := readValue(context.Background(), src, MaxValueBytes)
	if !errors.Is(err, want) {
		t.Fatalf("readValue error = %v, want %v", err, want)
	}
}

func TestReadValueChunksAreCopied(t *testing.T) {
	buffer := []byte("abc")
	src := &fakeSource{chunks: [][]byte{buffer}}
	got, err := readValue(context.Background(), src, MaxValueBytes)
	if err != nil {
		t.Fatalf("readValue: %v", err)
	}
	buffer[0] = 'X'
	if !bytes.Equal(got, []byte("abc")) {
		t.Fatalf("readValue() = %q after the source buffer changed, want %q", got, "abc")
	}
}
