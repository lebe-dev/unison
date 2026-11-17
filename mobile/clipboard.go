package mobile

import (
	"context"
	"errors"
	"fmt"

	"google.golang.org/protobuf/proto"

	"github.com/lebe-dev/unison/core/ports"
	mobilev1 "github.com/lebe-dev/unison/proto/unison/mobile/v1"
)

// clipboard sends clipboard content to the platform. The host reads the local
// clipboard itself and submits the content through SubmitClip, so Watch never
// reports a change.
type clipboard struct {
	platform Platform
}

var _ ports.Clipboard = (*clipboard)(nil)

// newClipboard returns the clipboard bridge of the platform.
func newClipboard(platform Platform) *clipboard {
	return &clipboard{platform: platform}
}

// Watch blocks until ctx is done. The channel reports nothing.
func (c *clipboard) Watch(ctx context.Context) (<-chan ports.Snapshot, error) {
	ch := make(chan ports.Snapshot)
	go func() {
		defer close(ch)
		<-ctx.Done()
	}()
	return ch, nil
}

// Write hands content to the platform clipboard.
func (c *clipboard) Write(ctx context.Context, content ports.Content) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	payload := contentToPayload(content)
	if len(payload.GetParts()) == 0 {
		return errors.New("mobile: clipboard content is empty")
	}
	data, err := proto.Marshal(payload)
	if err != nil {
		return fmt.Errorf("mobile: encode clipboard content: %w", err)
	}
	writeClipboard(c.platform, data)
	return nil
}

// writeClipboard calls the platform and keeps a panic from reaching the core.
func writeClipboard(platform Platform, data []byte) {
	defer func() {
		_ = recover()
	}()
	platform.WriteClipboard(data)
}

// contentToPayload converts clipboard content to its mobile form.
func contentToPayload(content ports.Content) *mobilev1.ClipPayload {
	payload := &mobilev1.ClipPayload{ItemId: content.ItemID}
	for _, rep := range content.Representations {
		payload.Parts = append(payload.Parts, &mobilev1.ClipPart{
			MimeType: rep.MIME,
			Data:     append([]byte(nil), rep.Data...),
		})
	}
	if first := payload.GetParts(); len(first) > 0 {
		payload.MimeType = first[0].GetMimeType()
		payload.Data = first[0].GetData()
		payload.FileName = first[0].GetFileName()
	}
	return payload
}

// contentFromPayload converts a mobile payload to clipboard content.
func contentFromPayload(payload *mobilev1.ClipPayload) (ports.Content, error) {
	if payload == nil {
		return ports.Content{}, errors.New("mobile: clipboard payload is empty")
	}

	parts := payload.GetParts()
	if len(parts) == 0 {
		parts = []*mobilev1.ClipPart{{
			MimeType: payload.GetMimeType(),
			Data:     payload.GetData(),
			FileName: payload.GetFileName(),
		}}
	}

	reps := make([]ports.Representation, 0, len(parts))
	for _, part := range parts {
		if len(part.GetData()) == 0 {
			continue
		}
		reps = append(reps, ports.Representation{
			MIME: part.GetMimeType(),
			Data: append([]byte(nil), part.GetData()...),
		})
	}
	if len(reps) == 0 {
		return ports.Content{}, errors.New("mobile: clipboard payload has no data")
	}
	return ports.Content{Representations: reps}, nil
}
