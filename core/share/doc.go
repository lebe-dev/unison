// Package share transfers files between devices the user picks.
//
// A sender stores the files in the blob store and offers them to the peers. A
// receiver asks the trust matrix: an offer of a Personal peer is accepted at
// once, an offer of a Friends peer waits for the user. The accepted files are
// downloaded with the blob protocol, which resumes an interrupted transfer and
// verifies every chunk, and are written to the downloads directory or to the
// platform sink. Nothing of an undecided offer is written to the disk; it
// expires after a timeout and every peer has a limit of pending offers.
package share
