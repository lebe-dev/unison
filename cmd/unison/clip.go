package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"

	"github.com/lebe-dev/unison/core/clip"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
	"github.com/lebe-dev/unison/proto/unison/local/v1/localv1connect"
)

// clip runs the clipboard subcommands.
func (a *app) clip(ctx context.Context, args []string) error {
	command, rest := subcommand(args, "from")
	switch command {
	case "get":
		return a.clipGet(ctx, rest)
	case "set":
		return a.clipSet(ctx, rest)
	case "":
		flags := a.newFlagSet("clip")
		if err := parseFlags(flags, rest); err != nil {
			return err
		}
		return a.usageError("clip needs a command: get or set")
	default:
		return a.usageError("unknown clip command %q", command)
	}
}

// clipGet prints the last clipboard item. The text is written as it is, without
// a trailing newline. With --from the last item of one paired device is printed
// instead.
func (a *app) clipGet(ctx context.Context, args []string) error {
	flags := a.newFlagSet("clip get")
	from := flags.String("from", "", "print the last item of this device")
	if err := parseFlags(flags, args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return a.usageError("clip get takes no arguments")
	}

	client, err := a.api()
	if err != nil {
		return err
	}
	item, err := a.clipItem(ctx, client, *from)
	if err != nil {
		return err
	}
	if a.json {
		if item == nil {
			return a.writeJSON(&localv1.GetClipResponse{})
		}
		return a.writeJSON(&localv1.GetClipResponse{Item: item})
	}
	if item == nil {
		return errors.New("the clipboard history is empty")
	}
	text, err := plainText(item)
	if err != nil {
		return err
	}
	_, err = io.WriteString(a.stdout, text)
	return err
}

// clipItem returns the last item of the clipboard or of one device.
func (a *app) clipItem(ctx context.Context, client localv1connect.UnisonServiceClient, from string) (*localv1.ClipItem, error) {
	if from == "" {
		response, err := client.GetClip(ctx, connect.NewRequest(&localv1.GetClipRequest{}))
		if err != nil {
			return nil, fmt.Errorf("read the clipboard: %w", err)
		}
		return response.Msg.GetItem(), nil
	}

	devices, err := a.devicesOf(ctx, client)
	if err != nil {
		return nil, err
	}
	device, err := resolveDevice(devices, from)
	if err != nil {
		return nil, err
	}
	response, err := client.ListHistory(ctx, connect.NewRequest(&localv1.ListHistoryRequest{
		DeviceId: device.GetDeviceId(),
		Limit:    1,
	}))
	if err != nil {
		return nil, fmt.Errorf("read the history of the device: %w", err)
	}
	if entries := response.Msg.GetItems(); len(entries) > 0 {
		return entries[0].GetItem(), nil
	}
	return nil, nil
}

// clipSet copies the standard input to the clipboard and announces it to the
// peers.
func (a *app) clipSet(ctx context.Context, args []string) error {
	flags := a.newFlagSet("clip set")
	if err := parseFlags(flags, args); err != nil {
		return err
	}
	if flags.NArg() > 1 || (flags.NArg() == 1 && flags.Arg(0) != "-") {
		return a.usageError("clip set reads the standard input, pass - for the same")
	}

	text, err := readText(a.stdin)
	if err != nil {
		return err
	}

	client, err := a.api()
	if err != nil {
		return err
	}
	response, err := client.SetClip(ctx, connect.NewRequest(&localv1.SetClipRequest{Text: text}))
	if err != nil {
		return fmt.Errorf("set the clipboard: %w", err)
	}
	if a.json {
		response.Msg.Item = withoutPayload(response.Msg.GetItem())
		return a.writeJSON(response.Msg)
	}
	a.info("clipboard updated (%d bytes)", len(text))
	return nil
}

// withoutPayload returns a copy of an item without its payload bytes, so that
// an answer never repeats the text it was given.
func withoutPayload(item *localv1.ClipItem) *localv1.ClipItem {
	if item == nil {
		return nil
	}
	cleared, ok := proto.Clone(item).(*localv1.ClipItem)
	if !ok {
		return item
	}
	for _, representation := range cleared.GetRepresentations() {
		representation.InlineBytes = nil
	}
	return cleared
}

// readText reads the text of standard input and bounds it by the inline limit
// of the local API.
func readText(r io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(r, clip.MaxInlineText+1))
	if err != nil {
		return "", fmt.Errorf("read the standard input: %w", err)
	}
	switch {
	case len(data) == 0:
		return "", errors.New("the standard input is empty")
	case len(data) > clip.MaxInlineText:
		return "", fmt.Errorf("the text is longer than %d bytes", clip.MaxInlineText)
	}
	return string(data), nil
}

// plainText returns the plain text representation of an item.
func plainText(item *localv1.ClipItem) (string, error) {
	for _, representation := range item.GetRepresentations() {
		if representation.GetMimeType() != clip.MIMETextPlain {
			continue
		}
		if len(representation.GetInlineBytes()) == 0 && representation.GetBlake3() != "" {
			return "", errors.New("the text is not carried by the local API")
		}
		return string(representation.GetInlineBytes()), nil
	}
	return "", errors.New("the last clipboard item has no plain text")
}
