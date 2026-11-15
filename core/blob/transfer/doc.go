// Package transfer moves blobs between peers.
//
// A peer asks the others whether they store a blob with a HaveBlob control
// message. The content is served on a request stream: the caller writes a
// BlobRequest and the source answers with the hash tree of the blob followed by
// the requested chunks. The tree lets the receiver verify every chunk against
// the hash of the whole content before the chunk is stored, so a source cannot
// make the receiver write content it did not ask for.
//
// A fetch asks the sources for ranges of missing chunks round robin, and keeps
// what it received when it is interrupted: the next fetch starts from the
// chunks that are still missing. A source that sends corrupt or unexpected
// frames is dropped and its ranges are requested from the remaining sources.
package transfer
