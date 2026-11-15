package fake

import (
	"context"
	"slices"
	"sync"

	"github.com/lebe-dev/unison/core/ports"
)

// watchBuffer is the number of snapshots buffered per watcher.
const watchBuffer = 16

// Clipboard is an in-memory ports.Clipboard with injectable events.
type Clipboard struct {
	mu       sync.Mutex
	next     int
	watchers map[int]chan ports.Snapshot
	last     ports.Content
	written  bool
}

// NewClipboard creates an empty clipboard.
func NewClipboard() *Clipboard {
	return &Clipboard{watchers: make(map[int]chan ports.Snapshot)}
}

var _ ports.Clipboard = (*Clipboard)(nil)

// Watch registers a watcher that receives snapshots passed to Emit.
func (c *Clipboard) Watch(ctx context.Context) (<-chan ports.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	id := c.next
	c.next++
	ch := make(chan ports.Snapshot, watchBuffer)
	c.watchers[id] = ch
	c.mu.Unlock()

	go func() {
		<-ctx.Done()
		c.removeWatch(id)
	}()
	return ch, nil
}

// Write records content as the last written clipboard content.
// It does not emit a snapshot: tests emit the events they need.
func (c *Clipboard) Write(ctx context.Context, content ports.Content) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.last = cloneContent(content)
	c.written = true
	return nil
}

// Emit delivers snapshot to every active watcher. Watchers with a full buffer miss it.
func (c *Clipboard) Emit(snapshot ports.Snapshot) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, ch := range c.watchers {
		select {
		case ch <- snapshot:
		default:
		}
	}
}

// LastWrite returns the content passed to the last Write and whether there was one.
func (c *Clipboard) LastWrite() (ports.Content, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return cloneContent(c.last), c.written
}

func (c *Clipboard) removeWatch(id int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch, ok := c.watchers[id]
	if !ok {
		return
	}
	delete(c.watchers, id)
	close(ch)
}

func cloneContent(content ports.Content) ports.Content {
	reps := make([]ports.Representation, len(content.Representations))
	for i, rep := range content.Representations {
		rep.Data = slices.Clone(rep.Data)
		reps[i] = rep
	}
	return ports.Content{Representations: reps}
}
