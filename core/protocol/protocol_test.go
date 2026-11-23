package protocol

import "testing"

// TestCompare checks the relation of a peer version to the current one.
func TestCompare(t *testing.T) {
	tests := []struct {
		name string
		peer uint32
		want Relation
	}{
		{name: "same", peer: Version, want: Same},
		{name: "newer", peer: Version + 1, want: PeerNewer},
		{name: "older", peer: Version - 1, want: PeerOlder},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Compare(tt.peer); got != tt.want {
				t.Fatalf("compare %d = %s, want %s", tt.peer, got, tt.want)
			}
		})
	}
}

// TestRelationString checks the names reported for the log and the API.
func TestRelationString(t *testing.T) {
	if got := Same.String(); got != "same" {
		t.Fatalf("same = %q", got)
	}
	if got := PeerNewer.String(); got != "peer-newer" {
		t.Fatalf("peer newer = %q", got)
	}
	if got := PeerOlder.String(); got != "peer-older" {
		t.Fatalf("peer older = %q", got)
	}
}
