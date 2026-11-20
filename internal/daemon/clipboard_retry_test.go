package daemon

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

// retryRecorder is a clipboard that becomes available after a number of
// attempts and records the waits.
type retryRecorder struct {
	noClipboard
	attempts int
	success  int
	waits    []time.Duration
}

// RetryClipboard reports the clipboard as available after the configured
// number of attempts.
func (r *retryRecorder) RetryClipboard() bool {
	r.attempts++
	return r.attempts >= r.success
}

// TestRetryClipboardBackoff checks that the waits double up to two minutes and
// are one minute from then on.
func TestRetryClipboardBackoff(t *testing.T) {
	clip := &retryRecorder{success: 10}
	wait := func(_ context.Context, delay time.Duration) bool {
		clip.waits = append(clip.waits, delay)
		return true
	}
	retryClipboardEvery(context.Background(), clip, slog.New(slog.DiscardHandler),
		wait, time.Second, 2*time.Minute, time.Minute)

	want := []time.Duration{
		time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second,
		16 * time.Second, 32 * time.Second, 64 * time.Second, 2 * time.Minute,
		time.Minute, time.Minute,
	}
	if len(clip.waits) != len(want) {
		t.Fatalf("%d attempts, want %d: %v", len(clip.waits), len(want), clip.waits)
	}
	for i := range want {
		if clip.waits[i] != want[i] {
			t.Fatalf("wait %d is %s, want %s", i, clip.waits[i], want[i])
		}
	}
}

// TestRetryClipboardStopsWhenAvailable checks that the loop ends with the
// first successful attempt.
func TestRetryClipboardStopsWhenAvailable(t *testing.T) {
	clip := &retryRecorder{success: 2}
	calls := 0
	wait := func(context.Context, time.Duration) bool {
		calls++
		return true
	}
	retryClipboardEvery(context.Background(), clip, slog.New(slog.DiscardHandler),
		wait, time.Millisecond, time.Minute, time.Minute)
	if calls != 2 {
		t.Fatalf("%d waits, want 2", calls)
	}
}

// TestRetryClipboardEndsWithContext checks that the loop stops when the daemon
// stops.
func TestRetryClipboardEndsWithContext(t *testing.T) {
	clip := &retryRecorder{success: 100}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	retryClipboardEvery(ctx, clip, slog.New(slog.DiscardHandler),
		waitOrDone, time.Hour, time.Hour, time.Hour)
	if clip.attempts != 0 {
		t.Fatalf("%d attempts after the context ended", clip.attempts)
	}
}

// availableRecorder is a retryRecorder whose clipboard already works.
type availableRecorder struct{ retryRecorder }

// ClipboardStatus reports a working clipboard.
func (*availableRecorder) ClipboardStatus() (string, string) { return ClipboardX11, "" }

// TestRetryClipboardSkipsAvailable checks that a working clipboard is left
// alone.
func TestRetryClipboardSkipsAvailable(t *testing.T) {
	clip := &availableRecorder{}
	retryClipboard(context.Background(), clip, slog.New(slog.DiscardHandler))
	if clip.attempts != 0 {
		t.Fatalf("%d attempts on a working clipboard", clip.attempts)
	}
}

// TestRetryClipboardWithoutRetrier checks that a clipboard that needs no retry
// ends the loop at once.
func TestRetryClipboardWithoutRetrier(t *testing.T) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		retryClipboard(context.Background(), NoClipboard(), slog.New(slog.DiscardHandler))
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("retryClipboard did not return for a clipboard without a retry")
	}
}
