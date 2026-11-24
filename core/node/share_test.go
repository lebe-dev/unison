package node

import (
	"bytes"
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/blob"
	"github.com/lebe-dev/unison/core/history"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/policy"
	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/share"
	"github.com/lebe-dev/unison/core/transport/vnet"
	"github.com/lebe-dev/unison/core/trust"
)

// shareNodes starts two paired nodes with the given role for each other.
func shareNodes(t *testing.T, base int, role model.Role) (*testNode, *testNode) {
	t.Helper()

	return shareNodesWith(t, base, role, nil)
}

// shareNodesWith starts two paired nodes and lets the test change their
// settings.
func shareNodesWith(t *testing.T, base int, role model.Role, tune func(*Config)) (*testNode, *testNode) {
	t.Helper()

	network := vnet.NewNetwork()
	t.Cleanup(func() { _ = network.Close() })
	bus := fake.NewBus()

	a := newTestNodeWith(t, network, bus, base, t.TempDir(), fake.NewKeystore(), tune)
	b := newTestNodeWith(t, network, bus, base+1000, t.TempDir(), fake.NewKeystore(), tune)
	a.start(t)
	b.start(t)
	if role == model.RoleFriends {
		enableFriends(t, a, b)
	}

	a.pairAs(t, b, role)
	b.pairAs(t, a, role)
	waitOnline(t, a, b.DeviceID())
	waitOnline(t, b, a.DeviceID())
	small, large := smaller(a, b)
	waitSingleConnection(t, small, large)
	return a, b
}

// pairAs stores the other node with the given role.
func (n *testNode) pairAs(t *testing.T, other *testNode, role model.Role) {
	t.Helper()

	err := n.AddPeer(context.Background(), trust.Peer{
		Public:   other.Identity().Public(),
		Name:     other.cfg.Name,
		Platform: model.PlatformMacOS,
		Role:     role,
	})
	if err != nil {
		t.Fatalf("add peer: %v", err)
	}
}

// allowLargeShares raises the files limit of the node, so a large offer of a
// Personal device is downloaded at once.
func allowLargeShares(n *testNode) {
	config := n.Policy().Config()
	config.Unmetered.Files.MaxSize = 1 << 30
	n.Policy().SetConfig(config)
}

// shareFile writes a file of the given size and returns its path.
func shareFile(t *testing.T, name string, size int64) (string, []byte) {
	t.Helper()

	data := make([]byte, size)
	if size > 0 {
		if _, err := rand.Read(data); err != nil {
			t.Fatalf("read random bytes: %v", err)
		}
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	return path, data
}

// waitTransfer waits for a transfer of the node to reach the state.
func waitTransfer(t *testing.T, n *testNode, id string, state share.State) share.Transfer {
	t.Helper()
	return waitTransferWithin(t, n, id, state, testTimeout)
}

// waitTransferWithin waits for a transfer of the node to reach the state.
func waitTransferWithin(t *testing.T, n *testNode, id string, state share.State, timeout time.Duration) share.Transfer {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		record, err := n.Share().Transfer(id)
		if err == nil && record.State == state {
			return record
		}
		if err == nil && terminal(record.State) {
			t.Fatalf("transfer %s is %s (%s), want %s", id, record.State, record.Err, state)
		}
		time.Sleep(10 * time.Millisecond)
	}
	record, err := n.Share().Transfer(id)
	t.Fatalf("transfer %s is %s (%v), want %s within %v", id, record.State, err, state, timeout)
	return share.Transfer{}
}

// waitIncoming waits for the incoming transfer of the peer.
func waitIncoming(t *testing.T, n *testNode, peer model.DeviceID, state share.State) share.Transfer {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		for _, record := range n.Share().Transfers() {
			if record.Direction != share.DirectionIncoming || record.Peer != peer {
				continue
			}
			if record.State == state {
				return record
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no incoming transfer from %s in state %s within %v", peer, state, testTimeout)
	return share.Transfer{}
}

// downloads returns the files of the downloads directory of the node.
func downloads(t *testing.T, n *testNode) []string {
	t.Helper()

	entries, err := os.ReadDir(n.cfg.ShareDownloads)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("read the downloads directory: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

// readSaved reads the file that was saved under the name.
func readSaved(t *testing.T, n *testNode, name string) []byte {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(n.cfg.ShareDownloads, name))
	if err != nil {
		t.Fatalf("read the saved file: %v", err)
	}
	return data
}

// dropSession ends the session of the node with the peer.
func dropSession(t *testing.T, n *testNode, peer model.DeviceID) {
	t.Helper()

	l := n.lookup(peer)
	if l == nil {
		t.Fatalf("the node has no link to %s", peer)
	}
	l.close()
}

// TestSharePersonalPeer checks that a file reaches a Personal device and is
// saved in its downloads directory.
func TestSharePersonalPeer(t *testing.T) {
	a, b := shareNodes(t, 52000, model.RolePersonal)
	events := a.subscribe(t)

	path, data := shareFile(t, "report.bin", 3<<20)
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	ids, err := a.Share().SendPaths(ctx, []string{path}, []model.DeviceID{b.DeviceID()})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("got %d transfers, want one", len(ids))
	}

	done := waitIncoming(t, b, a.DeviceID(), share.StateDone)
	if len(done.Paths) != 1 {
		t.Fatalf("the transfer saved %d files, want one", len(done.Paths))
	}
	if got := readSaved(t, b, filepath.Base(done.Paths[0])); !bytes.Equal(got, data) {
		t.Fatalf("the saved file differs from the source (%d bytes)", len(got))
	}
	if done.Bytes != uint64(len(data)) || done.TotalBytes != uint64(len(data)) {
		t.Fatalf("the transfer reports %d of %d bytes", done.Bytes, done.TotalBytes)
	}

	outgoing := waitTransfer(t, a, ids[0], share.StateDone)
	if outgoing.Bytes != uint64(len(data)) {
		t.Fatalf("the sender reports %d of %d bytes", outgoing.Bytes, outgoing.TotalBytes)
	}
	waitEvent(t, events, EventTransfer, b.DeviceID())
}

// TestShareFriendWaitsForDecision checks that an offer of a Friends device
// writes nothing until it is accepted.
func TestShareFriendWaitsForDecision(t *testing.T) {
	a, b := shareNodes(t, 54000, model.RoleFriends)

	path, data := shareFile(t, "photo.jpg", 2<<20)
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	ids, err := a.Share().SendPaths(ctx, []string{path}, []model.DeviceID{b.DeviceID()})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	pending := waitIncoming(t, b, a.DeviceID(), share.StatePending)
	if names := downloads(t, b); len(names) != 0 {
		t.Fatalf("an undecided offer wrote %v", names)
	}
	if pending.Files[0].Name != "photo.jpg" {
		t.Fatalf("the offer holds %q", pending.Files[0].Name)
	}
	if state, err := a.Share().Transfer(ids[0]); err != nil || state.State != share.StateOffered {
		t.Fatalf("the sender state is %s (%v), want offered", state.State, err)
	}

	if err := b.Share().Accept(ctx, pending.ID); err != nil {
		t.Fatalf("accept: %v", err)
	}
	done := waitTransfer(t, b, pending.ID, share.StateDone)
	if got := readSaved(t, b, filepath.Base(done.Paths[0])); !bytes.Equal(got, data) {
		t.Fatalf("the saved file differs from the source (%d bytes)", len(got))
	}
	waitTransfer(t, a, ids[0], share.StateDone)
}

// TestSharePersonalFileConfirmation checks that the files flag of a device
// holds the offers of a Personal device until the user accepts them, and that
// the clipboard flag does not.
func TestSharePersonalFileConfirmation(t *testing.T) {
	a, b := shareNodes(t, 53000, model.RolePersonal)
	events := b.subscribe(t)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if err := b.SetConfirmation(ctx, a.DeviceID(), trust.ScopeClipboard, true); err != nil {
		t.Fatalf("set the clipboard flag: %v", err)
	}
	path, _ := shareFile(t, "free.bin", 1<<10)
	if _, err := a.Share().SendPaths(ctx, []string{path}, []model.DeviceID{b.DeviceID()}); err != nil {
		t.Fatalf("send: %v", err)
	}
	waitIncoming(t, b, a.DeviceID(), share.StateDone)

	if err := b.SetConfirmation(ctx, a.DeviceID(), trust.ScopeFiles, true); err != nil {
		t.Fatalf("set the files flag: %v", err)
	}
	path, data := shareFile(t, "held.bin", 1<<20)
	ids, err := a.Share().SendPaths(ctx, []string{path}, []model.DeviceID{b.DeviceID()})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	pending := waitIncoming(t, b, a.DeviceID(), share.StatePending)
	if pending.Files[0].Name != "held.bin" {
		t.Fatalf("the offer holds %q", pending.Files[0].Name)
	}
	waitEvent(t, events, EventTransfer, a.DeviceID())
	if requests := b.Share().Requests(); len(requests) != 1 || requests[0].ID != pending.ID {
		t.Fatalf("the requests are %+v, want the held offer", requests)
	}

	if err := b.Share().Accept(ctx, pending.ID); err != nil {
		t.Fatalf("accept: %v", err)
	}
	done := waitTransfer(t, b, pending.ID, share.StateDone)
	if got := readSaved(t, b, filepath.Base(done.Paths[0])); !bytes.Equal(got, data) {
		t.Fatalf("the saved file differs from the source (%d bytes)", len(got))
	}
	waitTransfer(t, a, ids[0], share.StateDone)
}

// TestShareAboveFilesLimitWaits checks that an offer of a Personal device above
// the files limit of the policy waits for the user, and one within it does not.
func TestShareAboveFilesLimitWaits(t *testing.T) {
	a, b := shareNodes(t, 36000, model.RolePersonal)

	config := b.Policy().Config()
	config.Unmetered.Files.MaxSize = 1 << 20
	b.Policy().SetConfig(config)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	path, _ := shareFile(t, "small.bin", 1<<20)
	if _, err := a.Share().SendPaths(ctx, []string{path}, []model.DeviceID{b.DeviceID()}); err != nil {
		t.Fatalf("send: %v", err)
	}
	waitIncoming(t, b, a.DeviceID(), share.StateDone)

	path, data := shareFile(t, "large.bin", 1<<20+1)
	ids, err := a.Share().SendPaths(ctx, []string{path}, []model.DeviceID{b.DeviceID()})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	pending := waitIncoming(t, b, a.DeviceID(), share.StatePending)
	if pending.Files[0].Name != "large.bin" {
		t.Fatalf("the offer holds %q", pending.Files[0].Name)
	}
	for _, name := range downloads(t, b) {
		if name == "large.bin" {
			t.Fatal("an offer above the limit was saved before a decision")
		}
	}

	if err := b.Share().Accept(ctx, pending.ID); err != nil {
		t.Fatalf("accept: %v", err)
	}
	done := waitTransfer(t, b, pending.ID, share.StateDone)
	if got := readSaved(t, b, filepath.Base(done.Paths[0])); !bytes.Equal(got, data) {
		t.Fatalf("the saved file differs from the source (%d bytes)", len(got))
	}
	waitTransfer(t, a, ids[0], share.StateDone)
}

// TestSharePolicyRejects checks that an offer the policy refuses is declined
// without waiting for the user.
func TestSharePolicyRejects(t *testing.T) {
	a, b := shareNodes(t, 38000, model.RolePersonal)

	config := b.Policy().Config()
	config.Unmetered.Files.Action = policy.ActionReject
	b.Policy().SetConfig(config)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	path, _ := shareFile(t, "refused.bin", 1<<10)
	ids, err := a.Share().SendPaths(ctx, []string{path}, []model.DeviceID{b.DeviceID()})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	waitTransfer(t, a, ids[0], share.StateDeclined)
	if names := downloads(t, b); len(names) != 0 {
		t.Fatalf("a refused offer wrote %v", names)
	}
	if requests := b.Share().Requests(); len(requests) != 0 {
		t.Fatalf("a refused offer waits for a decision: %+v", requests)
	}
}

// TestShareAboveStoreLimitInParts checks that an offer whose files together do
// not fit the blob store of the receiver still arrives when each file fits on
// its own: the files are downloaded and saved one by one, so the store only
// stages them.
func TestShareAboveStoreLimitInParts(t *testing.T) {
	a, b := shareNodes(t, 39500, model.RolePersonal)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if err := b.Store().Settings().SetInt(ctx, blob.SettingMaxSize, 3<<20); err != nil {
		t.Fatalf("set the blob limit: %v", err)
	}

	first, firstData := shareFile(t, "first.bin", 2<<20)
	second, secondData := shareFile(t, "second.bin", 2<<20)
	ids, err := a.Share().SendPaths(ctx, []string{first, second}, []model.DeviceID{b.DeviceID()})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	waitTransfer(t, a, ids[0], share.StateDone)
	if got := readSaved(t, b, "first.bin"); !bytes.Equal(got, firstData) {
		t.Fatalf("the first saved file differs from the source (%d bytes)", len(got))
	}
	if got := readSaved(t, b, "second.bin"); !bytes.Equal(got, secondData) {
		t.Fatalf("the second saved file differs from the source (%d bytes)", len(got))
	}
}

// TestShareAboveStoreLimitDeclined checks that an offer with a file larger than
// the blob store of the receiver is declined before anything is downloaded.
func TestShareAboveStoreLimitDeclined(t *testing.T) {
	a, b := shareNodes(t, 39700, model.RolePersonal)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if err := b.Store().Settings().SetInt(ctx, blob.SettingMaxSize, 1<<20); err != nil {
		t.Fatalf("set the blob limit: %v", err)
	}

	path, _ := shareFile(t, "huge.bin", 1<<20+1)
	ids, err := a.Share().SendPaths(ctx, []string{path}, []model.DeviceID{b.DeviceID()})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	waitTransfer(t, a, ids[0], share.StateDeclined)
	if names := downloads(t, b); len(names) != 0 {
		t.Fatalf("a declined offer wrote %v", names)
	}
}

// TestShareFriendRejects checks that a rejected offer leaves nothing on the
// disk and reaches its final state on both sides.
func TestShareFriendRejects(t *testing.T) {
	a, b := shareNodes(t, 56000, model.RoleFriends)

	path, _ := shareFile(t, "document.pdf", 1<<20)
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	ids, err := a.Share().SendPaths(ctx, []string{path}, []model.DeviceID{b.DeviceID()})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	pending := waitIncoming(t, b, a.DeviceID(), share.StatePending)
	if err := b.Share().Reject(ctx, pending.ID); err != nil {
		t.Fatalf("reject: %v", err)
	}
	waitTransfer(t, b, pending.ID, share.StateDeclined)
	waitTransfer(t, a, ids[0], share.StateDeclined)
	if names := downloads(t, b); len(names) != 0 {
		t.Fatalf("a rejected offer wrote %v", names)
	}
}

// TestShareCancelStopsTransfer checks that the receiver can stop a running
// transfer.
func TestShareCancelStopsTransfer(t *testing.T) {
	a, b := shareNodes(t, 58000, model.RolePersonal)
	allowLargeShares(b)

	path, _ := shareFile(t, "video.bin", 64<<20)
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	ids, err := a.Share().SendPaths(ctx, []string{path}, []model.DeviceID{b.DeviceID()})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	incoming := waitIncoming(t, b, a.DeviceID(), share.StateTransferring)
	if err := b.Share().Cancel(ctx, incoming.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	waitTransfer(t, b, incoming.ID, share.StateCanceled)
	waitTransfer(t, a, ids[0], share.StateCanceled)
	if names := downloads(t, b); len(names) != 0 {
		t.Fatalf("a canceled transfer wrote %v", names)
	}
	now := time.Now()
	for _, record := range b.Share().Transfers() {
		if record.ID != incoming.ID {
			continue
		}
		for _, file := range record.Files {
			if _, err := b.Blobs().Stat(ctx, file.Hash); err == nil {
				t.Fatalf("the chunk store kept the payload of a canceled transfer after %v", now)
			}
		}
	}
}

// TestShareResumesAfterInterruption checks that a download continues from the
// chunks it already has when the peer comes back.
func TestShareResumesAfterInterruption(t *testing.T) {
	runShareResume(t, 61000, 32<<20)
}

// runShareResume transfers one file of size bytes to a Personal device and
// interrupts the download after the first chunk.
func runShareResume(t *testing.T, base int, size int64) {
	t.Helper()

	a, b := shareNodes(t, base, model.RolePersonal)
	allowLargeShares(b)
	timeout := 10 * testTimeout

	path, data := shareFile(t, "archive.bin", size)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ids, err := a.Share().SendPaths(ctx, []string{path}, []model.DeviceID{b.DeviceID()})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	incoming := waitIncoming(t, b, a.DeviceID(), share.StateTransferring)
	hash := incoming.Files[0].Hash

	waitProgress(t, b, incoming.ID)
	dropSession(t, b, a.DeviceID())

	stored, err := b.Blobs().Stat(ctx, hash)
	if err != nil {
		t.Fatalf("read the partial blob: %v", err)
	}
	if !stored.Partial {
		t.Fatal("the interrupted payload is complete")
	}
	missing, err := b.Blobs().Missing(ctx, hash)
	if err != nil {
		t.Fatalf("read the missing chunks: %v", err)
	}
	if len(missing) == 0 {
		t.Fatal("the interrupted payload misses no chunk")
	}

	done := waitTransferWithin(t, b, incoming.ID, share.StateDone, timeout)
	if got := readSaved(t, b, filepath.Base(done.Paths[0])); !bytes.Equal(got, data) {
		t.Fatalf("the resumed file differs from the source (%d bytes)", len(got))
	}
	if left, err := b.Blobs().Missing(ctx, hash); err != nil || len(left) != 0 {
		t.Fatalf("the resumed payload misses %d chunks: %v", len(left), err)
	}
	waitTransferWithin(t, a, ids[0], share.StateDone, timeout)
}

// terminal reports a transfer state that does not change by itself.
func terminal(state share.State) bool {
	switch state {
	case share.StateDone, share.StateDeclined, share.StateCanceled, share.StateFailed:
		return true
	default:
		return false
	}
}

// waitProgress waits until a transfer moved at least one chunk.
func waitProgress(t *testing.T, n *testNode, id string) {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		record, err := n.Share().Transfer(id)
		if err == nil && record.Bytes > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("transfer %s made no progress within %v", id, testTimeout)
}

// TestShareRecordsReceivedImageAndFilesInHistory checks that completed incoming
// share transfers are recorded in history as image or file items.
func TestShareRecordsReceivedImageAndFilesInHistory(t *testing.T) {
	a, b := shareNodes(t, 63000, model.RolePersonal)
	allowLargeShares(b)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	imagePath, _ := shareFile(t, "screenshot.png", 1024)
	ids, err := a.Share().SendPaths(ctx, []string{imagePath}, []model.DeviceID{b.DeviceID()})
	if err != nil {
		t.Fatalf("send image: %v", err)
	}
	incomingImg := waitIncoming(t, b, a.DeviceID(), share.StateDone)
	waitTransfer(t, a, ids[0], share.StateDone)

	images, err := b.History().List(ctx, history.ListFilter{Kind: model.KindImage})
	if err != nil {
		t.Fatalf("list image history: %v", err)
	}
	if len(images) != 1 {
		t.Fatalf("got %d image history items, want 1", len(images))
	}
	if !images[0].Complete || images[0].Source != model.SourceShare {
		t.Fatalf("image item complete=%v source=%s, want complete share", images[0].Complete, images[0].Source)
	}
	if images[0].Representations[0].FileName != "screenshot.png" || images[0].Representations[0].Hash != incomingImg.Files[0].Hash {
		t.Fatalf("unexpected image representation: %+v", images[0].Representations[0])
	}

	filePath, _ := shareFile(t, "notes.txt", 512)
	ids, err = a.Share().SendPaths(ctx, []string{filePath}, []model.DeviceID{b.DeviceID()})
	if err != nil {
		t.Fatalf("send file: %v", err)
	}
	waitTransfer(t, a, ids[0], share.StateDone)

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		files, err := b.History().List(ctx, history.ListFilter{Kind: model.KindFiles})
		if err != nil {
			t.Fatalf("list file history: %v", err)
		}
		if len(files) == 1 {
			if !files[0].Complete || files[0].Source != model.SourceShare {
				t.Fatalf("file item complete=%v source=%s, want complete share", files[0].Complete, files[0].Source)
			}
			if files[0].Representations[0].FileName != "notes.txt" {
				t.Fatalf("unexpected file name %q, want notes.txt", files[0].Representations[0].FileName)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("shared file did not appear in history")
}
