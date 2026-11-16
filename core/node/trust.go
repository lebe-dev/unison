package node

import (
	"context"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/trust"
)

// announcedTrust records the names the peers announce. A name the user chose on
// this device stays.
type announcedTrust struct {
	*trust.Store
}

// Rename stores the name a peer announces in Hello or at pairing.
func (t announcedTrust) Rename(ctx context.Context, id model.DeviceID, name string) error {
	return t.SetAnnouncedName(ctx, id, name)
}

// announcedTrust returns the trust store that the sessions and the pairing
// window record the announced names in.
func (n *Node) announcedTrust() announcedTrust {
	return announcedTrust{Store: n.trust}
}
