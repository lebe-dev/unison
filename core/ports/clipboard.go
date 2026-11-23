package ports

import "context"

// Representation is one MIME representation of clipboard content.
type Representation struct {
	// MIME is the content type, for example "text/plain".
	MIME string
	// Data is the payload in memory.
	Data []byte
	// FilePath is the path of a file payload. It is empty when Data is set.
	FilePath string
}

// Content is clipboard content as a set of MIME representations.
type Content struct {
	// Representations holds every representation the platform can provide.
	Representations []Representation
	// ItemID is the string form of the clip item being written. It is empty
	// for content read from the clipboard. A platform may tag the clipboard
	// with it so other apps can tell the write from a copy of the user.
	ItemID string
}

// Snapshot is an observed state of the local clipboard.
type Snapshot struct {
	// Content is the clipboard content at the moment of observation.
	Content Content
	// Sensitive marks content the platform tagged as sensitive.
	Sensitive bool
	// OwnedByUs marks content written by this application, where the platform can tell.
	OwnedByUs bool
}

// Clipboard reads and writes the local clipboard.
type Clipboard interface {
	// Watch delivers a snapshot for every clipboard change until ctx is done.
	// The channel is closed when watching stops. Own writes may or may not be reported.
	Watch(ctx context.Context) (<-chan Snapshot, error)
	// Write replaces the clipboard content and marks it as written by this application.
	Write(ctx context.Context, content Content) error
}
