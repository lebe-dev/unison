package daemon

import (
	"context"

	"github.com/lebe-dev/unison/core/ports"
)

// NoClipboard returns a clipboard that has no content and ignores writes. It
// keeps a second daemon on one machine away from the system clipboard.
func NoClipboard() ports.Clipboard { return noClipboard{} }

// noClipboard is a clipboard without a pasteboard behind it.
type noClipboard struct{}

var _ ports.Clipboard = noClipboard{}

// Watch reports no snapshots until ctx is done.
func (noClipboard) Watch(ctx context.Context) (<-chan ports.Snapshot, error) {
	ch := make(chan ports.Snapshot)
	go func() {
		<-ctx.Done()
		close(ch)
	}()
	return ch, nil
}

// Write discards the content.
func (noClipboard) Write(ctx context.Context, _ ports.Content) error {
	return ctx.Err()
}
