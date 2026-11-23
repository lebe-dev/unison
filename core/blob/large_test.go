//go:build bloblarge

package blob

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"lukechampine.com/blake3"
)

// maxHeapGrowth bounds the heap growth of a streaming round trip. It is far
// below the content size, so buffering the whole blob would fail the test.
const maxHeapGrowth = 256 << 20

func TestLargeBlobKeepsMemoryBounded(t *testing.T) {
	const size = 1200 << 20

	ctx := context.Background()
	s := newTestStore(t, size+(64<<20))

	base := heapBytes()
	peak := &atomic.Uint64{}
	peak.Store(base)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go sampleHeap(&wg, stop, peak)
	stopSampling := func() {
		close(stop)
		wg.Wait()
	}

	src := patternReader{size: size}
	info, err := s.Put(ctx, src, size)
	if err != nil {
		stopSampling()
		t.Fatalf("put %d bytes: %v", size, err)
	}

	written, err := os.Stat(s.path(info.Hash))
	if err != nil {
		stopSampling()
		t.Fatalf("stat blob file: %v", err)
	}
	if want := newLayout(size).fileSize(); written.Size() != want {
		stopSampling()
		t.Fatalf("blob file is %d bytes, want %d", written.Size(), want)
	}

	read := checksum(t, s, info.Hash, size)
	stopSampling()
	if read != info.Hash {
		t.Fatalf("content hash is %s, want %s", hashHex(read), hashHex(info.Hash))
	}

	grown := int64(peak.Load()) - int64(base)
	t.Logf("stored %d MiB, heap grew by %d MiB", size>>20, grown>>20)
	if grown > maxHeapGrowth {
		t.Fatalf("heap grew by %d MiB, want at most %d MiB", grown>>20, int64(maxHeapGrowth)>>20)
	}
}

// checksum reads the whole blob back and returns the BLAKE3 hash of what it
// read, verifying every chunk on the way.
func checksum(t *testing.T, s *Store, hash [32]byte, size int64) [32]byte {
	t.Helper()

	r, err := s.Open(context.Background(), hash)
	if err != nil {
		t.Fatalf("open blob: %v", err)
	}
	defer func() { _ = r.Close() }()

	hasher := blake3.New(32, nil)
	buf := make([]byte, ChunkSize)
	for off := int64(0); off < size; off += int64(len(buf)) {
		n, err := r.ReadAt(buf, off)
		if err != nil {
			t.Fatalf("read at %d: %v", off, err)
		}
		if _, err := hasher.Write(buf[:n]); err != nil {
			t.Fatalf("hash content: %v", err)
		}
	}

	var sum [32]byte
	copy(sum[:], hasher.Sum(nil))
	return sum
}

// sampleHeap records the highest heap size until stop is closed.
func sampleHeap(wg *sync.WaitGroup, stop <-chan struct{}, peak *atomic.Uint64) {
	defer wg.Done()

	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			if now := uint64(heapBytes()); now > peak.Load() {
				peak.Store(now)
			}
		}
	}
}

// heapBytes returns the size of the live heap.
func heapBytes() uint64 {
	runtime.GC()
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return stats.HeapAlloc
}

// patternReader produces deterministic content of a known size without holding
// it in memory.
type patternReader struct {
	size int64
}

// ReadAt fills p with the content at off.
func (p patternReader) ReadAt(b []byte, off int64) (int, error) {
	if off < 0 {
		return 0, errors.New("blob: negative offset")
	}
	remaining := p.size - off
	if remaining <= 0 {
		return 0, io.EOF
	}

	n := len(b)
	shorter := false
	if int64(n) > remaining {
		n = int(remaining)
		shorter = true
	}
	for i := 0; i < n; {
		pos := uint64(off) + uint64(i)
		var word [8]byte
		binary.LittleEndian.PutUint64(word[:], pos/8*0x9e3779b97f4a7c15+0x0123456789abcdef)
		i += copy(b[i:n], word[:])
	}
	if shorter {
		return n, io.EOF
	}
	return n, nil
}
