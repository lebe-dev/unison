// Package mobile exposes a narrow API of the core to Android through gomobile.
package mobile

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"google.golang.org/protobuf/proto"

	"github.com/lebe-dev/unison/core/model/protoconv"
	mobilev1 "github.com/lebe-dev/unison/proto/unison/mobile/v1"
)

// Platform is implemented by the Android app. It receives callbacks from the
// core on the core's own goroutines.
type Platform interface {
	// OnEvent receives a serialized unison.mobile.v1.Event.
	OnEvent(evt []byte)
	// WriteClipboard writes a serialized unison.mobile.v1.ClipPayload to the
	// system clipboard.
	WriteClipboard(item []byte)
	// OpenBlobSink returns a file descriptor for an incoming blob.
	OpenBlobSink(meta []byte) int
	// KeystoreUnwrap decrypts a key blob with the platform keystore.
	KeystoreUnwrap(blob []byte) []byte
	// KeystoreWrap encrypts a key with the platform keystore.
	KeystoreWrap(plain []byte) []byte
	// Announce publishes or withdraws a serialized unison.mobile.v1.Announcement
	// through the platform discovery.
	Announce(info []byte)
}

// Errors of the facade.
var (
	errNotRunning = errors.New("mobile: core is not running")
	errRunning    = errors.New("mobile: core is already running")
	errPlatform   = errors.New("mobile: platform is nil")
)

// version is the version of the core, set at link time.
var version = "dev"

// facade is the running core.
var (
	globalMu sync.Mutex
	global   *core
)

// Start prepares the core with its configuration and the platform callbacks.
func Start(cfg []byte, p Platform) (err error) {
	defer recoverErr(&err)

	if p == nil {
		return errPlatform
	}
	parsed, err := parseConfig(cfg)
	if err != nil {
		return err
	}

	globalMu.Lock()
	defer globalMu.Unlock()
	if global != nil {
		return errRunning
	}
	c, err := startCore(context.Background(), parsed, p)
	if err != nil {
		return err
	}
	global = c
	return nil
}

// Stop releases the resources held by the core.
func Stop() (err error) {
	defer recoverErr(&err)

	globalMu.Lock()
	c := global
	global = nil
	globalMu.Unlock()
	if c == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), stopTimeout)
	defer cancel()
	return c.stop(ctx)
}

// Version returns the version of the core.
func Version() (v string) {
	defer func() {
		if recover() != nil {
			v = version
		}
	}()
	return version
}

// Call runs one local API request in process. It returns a serialized
// unison.mobile.v1.CallResponse; the code of a failure is set inside it. A
// response without fields comes back as a nil slice, which the host receives as
// a null array.
func Call(req []byte) (out []byte, err error) {
	defer recoverErr(&err)

	request := &mobilev1.CallRequest{}
	if len(req) > maxCallBytes {
		return nil, errors.New("mobile: call request is too large")
	}
	if len(req) > 0 {
		if err := proto.Unmarshal(req, request); err != nil {
			return nil, fmt.Errorf("mobile: decode call: %w", err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()

	response := runningCore().call(ctx, request)
	data, err := proto.Marshal(response)
	if err != nil {
		return nil, fmt.Errorf("mobile: encode call response: %w", err)
	}
	return data, nil
}

// SubmitClip copies a serialized unison.mobile.v1.ClipPayload read from the
// local clipboard into the core and announces it to the peers.
func SubmitClip(item []byte) (err error) {
	defer recoverErr(&err)

	c := runningCore()
	if c == nil {
		return errNotRunning
	}
	payload := &mobilev1.ClipPayload{}
	if len(item) > 0 {
		if err := proto.Unmarshal(item, payload); err != nil {
			return fmt.Errorf("mobile: decode clip payload: %w", err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	return c.submitClip(ctx, payload)
}

// SubmitFile copies the file behind fd into the blob store. The caller hands
// the descriptor over: the facade closes it.
func SubmitFile(meta []byte, fd int) (err error) {
	defer recoverErr(&err)

	c := runningCore()
	if c == nil {
		return errNotRunning
	}
	payload := &mobilev1.FilePayload{}
	if len(meta) > 0 {
		if err := proto.Unmarshal(meta, payload); err != nil {
			return fmt.Errorf("mobile: decode file payload: %w", err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	return c.submitFile(ctx, payload, fd)
}

// AddPeerCandidate passes a discovered service instance to the core. service is
// "main" or "pairing", txt is a serialized unison.mobile.v1.CandidateTxt.
func AddPeerCandidate(service string, addr string, txt []byte) {
	defer func() {
		_ = recover()
	}()

	c := runningCore()
	if c == nil {
		return
	}
	c.bridge.Add(service, addr, txt)
}

// NetworkChanged reports the network conditions of the device. state is a
// serialized unison.mobile.v1.NetworkState.
func NetworkChanged(state []byte) {
	defer func() {
		_ = recover()
	}()

	c := runningCore()
	if c == nil {
		return
	}
	msg := &mobilev1.NetworkState{}
	if len(state) > 0 {
		if err := proto.Unmarshal(state, msg); err != nil {
			c.log.Debug("ignored network state", "error", err)
			return
		}
	}
	c.node.NetworkChanged(protoconv.NetworkStateFromMobile(msg))
}

// runningCore returns the running core, or nil.
func runningCore() *core {
	globalMu.Lock()
	defer globalMu.Unlock()
	return global
}

// recoverErr turns a panic into an error.
func recoverErr(err *error) {
	if recovered := recover(); recovered != nil {
		*err = fmt.Errorf("mobile: panic: %v", recovered)
	}
}
