package daemon

import (
	"context"
	"log/slog"
	"time"

	"github.com/lebe-dev/unison/core/ports"
)

// Clipboard retry timings. The daemon may start before the user signs in, so it
// keeps reaching for the display of the session.
const (
	// clipboardRetryDelay is the first wait between two attempts.
	clipboardRetryDelay = time.Second
	// clipboardRetryMaxDelay is the longest wait of the backoff.
	clipboardRetryMaxDelay = 2 * time.Minute
	// clipboardRetrySteadyDelay is the wait after the backoff reached its end.
	clipboardRetrySteadyDelay = time.Minute
)

// clipboardRetrier is a clipboard that reaches the system after the daemon
// started.
type clipboardRetrier interface {
	// RetryClipboard tries to reach the system clipboard and reports whether
	// it is available now.
	RetryClipboard() bool
}

// retryClipboard reaches for the system clipboard until it answers or ctx ends.
func retryClipboard(ctx context.Context, clip ports.Clipboard, log *slog.Logger) {
	if _, available := clipboardBackend(clip); available {
		return
	}
	retryClipboardEvery(ctx, clip, log, waitOrDone, clipboardRetryDelay, clipboardRetryMaxDelay, clipboardRetrySteadyDelay)
}

// clipboardBackend reports the backend of a clipboard that reports one, and
// whether the clipboard is available.
func clipboardBackend(clip ports.Clipboard) (string, bool) {
	reporter, ok := clip.(clipboardStatusReporter)
	if !ok {
		return "", false
	}
	backend, _ := reporter.ClipboardStatus()
	return backend, backend != ClipboardNone
}

// retryClipboardEvery retries the clipboard with the given waits. The wait
// doubles up to maxDelay and stays steadyDelay afterwards.
func retryClipboardEvery(ctx context.Context, clip ports.Clipboard, log *slog.Logger,
	wait func(context.Context, time.Duration) bool, delay, maxDelay, steadyDelay time.Duration) {
	retrier, ok := clip.(clipboardRetrier)
	if !ok {
		return
	}
	steady := false
	for {
		if !wait(ctx, delay) {
			return
		}
		if retrier.RetryClipboard() {
			log.Info("the clipboard is available")
			return
		}
		switch {
		case steady:
			delay = steadyDelay
		case delay >= maxDelay:
			steady, delay = true, steadyDelay
		default:
			delay = min(2*delay, maxDelay)
		}
	}
}

// waitOrDone waits for the delay and reports false when ctx ended.
func waitOrDone(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
