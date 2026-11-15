// Package blob stores content addressed files. A blob is addressed by the
// BLAKE3 hash of its whole content and is kept as one MiB chunks, each one
// encrypted with the data encryption key of the node and verified against the
// hash tree of the blob. Complete and partial blobs share the same directory: a
// partial blob records the chunks it already holds, so a transfer resumes where
// it stopped. The size of the store is capped and least recently used blobs are
// evicted to stay within it.
package blob
