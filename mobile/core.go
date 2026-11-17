package mobile

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"google.golang.org/protobuf/proto"

	"github.com/lebe-dev/unison/core/api"
	"github.com/lebe-dev/unison/core/clip"
	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/node"
	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/core/share"
	"github.com/lebe-dev/unison/internal/journal"
	"github.com/lebe-dev/unison/internal/observability"
	mobilev1 "github.com/lebe-dev/unison/proto/unison/mobile/v1"
)

// core is a running node with the local API and the event relay.
type core struct {
	platform Platform
	dataDir  string
	node     *node.Node
	api      *api.Service
	sub      *node.Subscriber
	bridge   *discoveryBridge
	journal  *journal.Store
	log      *slog.Logger
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

// startCore opens the node and starts the event relay.
func startCore(ctx context.Context, cfg config, platform Platform) (*core, error) {
	clock := cfg.clock
	if clock == nil {
		clock = ports.SystemClock
	}
	// The journal opens before the node, so a failure to start is recorded
	// once the user turned the log on.
	appLog, journalErr := journal.Open(cfg.dataDir)
	keepJournal := false
	defer func() {
		if !keepJournal && appLog != nil {
			_ = appLog.Close()
		}
	}()
	log := cfg.loggerOf(appLog)
	if journalErr != nil {
		log.Warn("the application log is not available", "error", journalErr)
	}

	keys, err := newKeystore(cfg.dataDir, platform)
	if err != nil {
		return nil, err
	}
	bridge := newDiscoveryBridge(platform, clock, log)

	n, err := node.New(node.Config{
		DataDir:      cfg.dataDir,
		Name:         cfg.name,
		Platform:     model.PlatformAndroid,
		Keystore:     keys,
		Clipboard:    newClipboard(platform),
		Discovery:    bridge,
		Clock:        clock,
		Logger:       log,
		Limits:       cfg.limits,
		BlobLimit:    cfg.blobLimit,
		ShareSink:    blobSink{platform: platform, log: log},
		ListenPacket: cfg.listen,
	})
	if err != nil {
		return nil, err
	}
	// The consent is applied before the node starts, so a startup failure is
	// still reported for a user who already granted it.
	if enabled, err := n.SentryEnabled(ctx); err == nil {
		observability.SetEnabled(enabled)
	} else {
		observability.SetEnabled(false)
	}
	bridge.key = func(ctx context.Context) ([]byte, error) {
		return n.Trust().DiscoveryKey(ctx)
	}

	startCtx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()
	if err := n.Start(startCtx); err != nil {
		observability.CaptureException(err)
		return nil, err
	}

	service, err := api.New(n, api.Options{
		Version:    version,
		DeviceName: cfg.name,
		LocalClip:  localClip{pipeline: n.Clip()},
		Journal:    appLog,
	})
	if err != nil {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), stopTimeout)
		defer stopCancel()
		_ = n.Stop(stopCtx)
		return nil, err
	}

	relayCtx, stopRelay := context.WithCancel(context.Background())
	c := &core{
		platform: platform,
		dataDir:  cfg.dataDir,
		node:     n,
		api:      service,
		sub:      n.Subscribe(),
		bridge:   bridge,
		journal:  appLog,
		log:      log,
		cancel:   stopRelay,
	}
	c.wg.Add(1)
	go c.relay(relayCtx)
	keepJournal = true
	return c, nil
}

// stop ends the event relay and the node.
func (c *core) stop(ctx context.Context) error {
	c.cancel()
	c.sub.Close()
	c.wg.Wait()
	if c.journal != nil {
		_ = c.journal.Close()
	}
	return c.node.Stop(ctx)
}

// relay delivers the node events to the platform until the core stops. The
// platform callback runs on this goroutine and does not hold up the node.
func (c *core) relay(ctx context.Context) {
	defer c.wg.Done()
	defer observability.Recover()

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-c.sub.Events():
			if !ok {
				return
			}
			data, err := proto.Marshal(eventToMobile(event))
			if err != nil {
				c.log.Warn("encoding an event failed", "error", err)
				continue
			}
			c.deliver(data)
		}
	}
}

// deliver calls the platform and keeps a panic from reaching the relay.
func (c *core) deliver(data []byte) {
	defer func() {
		if value := recover(); value != nil {
			observability.CapturePanic(value)
			c.log.Warn("the platform event callback panicked")
		}
	}()
	c.platform.OnEvent(data)
}

// submitClip copies clipboard content into the pipeline and announces it.
func (c *core) submitClip(ctx context.Context, payload *mobilev1.ClipPayload) error {
	content, err := contentFromPayload(payload)
	if err != nil {
		return err
	}
	_, err = c.node.Clip().Submit(ctx, content)
	return err
}

// submitFile copies the descriptor into the blob store and offers it to the
// peers. The descriptor is closed.
func (c *core) submitFile(ctx context.Context, meta *mobilev1.FilePayload, fd int) error {
	if fd < 0 {
		return errors.New("mobile: file descriptor is required")
	}
	src := os.NewFile(uintptr(fd), "submit")
	if src == nil {
		return errors.New("mobile: invalid file descriptor")
	}
	defer func() {
		_ = src.Close()
	}()

	size := meta.GetSize()
	if size < 0 {
		return errors.New("mobile: file size is negative")
	}
	limit, err := c.node.Blobs().Limit(ctx)
	if err != nil {
		return err
	}
	if size > limit {
		return errors.New("mobile: file is larger than the blob limit")
	}

	dir := filepath.Join(c.dataDir, "tmp")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mobile: create temporary directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "submit-*")
	if err != nil {
		return fmt.Errorf("mobile: create temporary file: %w", err)
	}
	name := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(name)
	}()

	written, err := io.Copy(tmp, io.LimitReader(src, limit+1))
	if err != nil {
		return fmt.Errorf("mobile: read file: %w", err)
	}
	if written > limit {
		return errors.New("mobile: file is larger than the blob limit")
	}
	if size > 0 && written != size {
		return errors.New("mobile: file size does not match its metadata")
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("mobile: rewind temporary file: %w", err)
	}
	info, err := c.node.Blobs().Put(ctx, tmp, written)
	if err != nil {
		return err
	}
	return c.shareFile(ctx, meta, info.Hash, written)
}

// shareFile offers a stored file to the devices of the payload.
func (c *core) shareFile(ctx context.Context, meta *mobilev1.FilePayload, hash [32]byte, size int64) error {
	devices, err := shareDevices(meta.GetDeviceIds())
	if err != nil {
		return err
	}
	targets := devices
	if len(targets) == 0 {
		targets = c.node.Share().Peers()
	}
	if len(targets) == 0 {
		return share.ErrNoPeers
	}

	name := meta.GetFileName()
	if name == "" {
		name = "file"
	}
	_, err = c.node.Share().Send(ctx, []share.File{{
		Name: name,
		Size: size,
		Hash: hash,
		MIME: meta.GetMimeType(),
	}}, targets)
	return err
}

// shareDevices parses the device IDs of a share request.
func shareDevices(values []string) ([]model.DeviceID, error) {
	devices := make([]model.DeviceID, 0, len(values))
	for _, value := range values {
		device, err := model.ParseDeviceID(value)
		if err != nil || device.IsZero() {
			return nil, errors.New("mobile: invalid device id")
		}
		devices = append(devices, device)
	}
	return devices, nil
}

// localClip copies content copied on this device into the node pipeline.
type localClip struct {
	pipeline *clip.Pipeline
}

var _ api.LocalClip = localClip{}

// Submit records content and announces it to the peers.
func (l localClip) Submit(ctx context.Context, content ports.Content) (model.ClipItem, error) {
	return l.pipeline.Submit(ctx, content)
}

// blobSink writes the files a peer shares into the sink of the platform.
type blobSink struct {
	platform Platform
	log      *slog.Logger
}

var _ share.Sink = blobSink{}

// Write hands one file to the platform and copies the payload into it. It
// returns the file name, which is what the platform shows to the user.
func (b blobSink) Write(_ context.Context, file share.File, src io.Reader) (path string, err error) {
	defer func() {
		if value := recover(); value != nil {
			observability.CapturePanic(value)
			path, err = "", errors.New("mobile: the platform sink panicked")
		}
	}()

	meta, err := proto.Marshal(&mobilev1.FilePayload{
		MimeType: file.MIME,
		FileName: file.Name,
		Size:     file.Size,
	})
	if err != nil {
		return "", fmt.Errorf("mobile: encode the file metadata: %w", err)
	}
	fd := b.platform.OpenBlobSink(meta)
	if fd < 0 {
		return "", errors.New("mobile: the platform did not open a sink")
	}
	sink := os.NewFile(uintptr(fd), "sink")
	if sink == nil {
		return "", errors.New("mobile: invalid sink descriptor")
	}
	defer func() { _ = sink.Close() }()

	written, err := io.Copy(sink, src)
	if err != nil {
		return "", fmt.Errorf("mobile: write the file: %w", err)
	}
	if written != file.Size {
		return "", fmt.Errorf("mobile: wrote %d bytes of %d", written, file.Size)
	}
	b.log.Info("saved a shared file", "mime", file.MIME, "size", file.Size)
	return file.Name, nil
}
