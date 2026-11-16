package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"connectrpc.com/connect"

	"github.com/lebe-dev/unison/core/model"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
	"github.com/lebe-dev/unison/proto/unison/local/v1/localv1connect"
)

// historyLimit is the number of items the history lists by default.
const historyLimit = 20

// maxItemReference is the number of items a reference of an item is looked up
// in.
const maxItemReference = 1000

// history runs the history subcommands.
func (a *app) history(ctx context.Context, args []string) error {
	rest := args
	if len(args) > 0 {
		switch args[0] {
		case "fetch":
			return a.historyFetch(ctx, args[1:])
		case "apply":
			return a.historyApply(ctx, args[1:])
		case "ls":
		default:
			return a.usageError("unknown history command %q", args[0])
		}
		rest = args[1:]
	}
	return a.historyList(ctx, rest)
}

// historyList prints the entries of the history.
func (a *app) historyList(ctx context.Context, args []string) error {
	flags := a.newFlagSet("history")
	kind := flags.String("type", "", "only items of the type text, image or file")
	limit := flags.Uint("limit", historyLimit, "number of items to list")
	if err := parseFlags(flags, args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return a.usageError("history takes no arguments")
	}

	itemKind, err := kindOf(*kind)
	if err != nil {
		return a.usageError("%v", err)
	}
	client, err := a.api()
	if err != nil {
		return err
	}
	response, err := client.ListHistory(ctx, connect.NewRequest(&localv1.ListHistoryRequest{
		Kind:  itemKind,
		Limit: uint32(min(*limit, maxItemReference)),
	}))
	if err != nil {
		return fmt.Errorf("read the history: %w", err)
	}

	if a.json {
		for _, entry := range response.Msg.GetItems() {
			entry.Item = withoutPayload(entry.GetItem())
		}
		return a.writeJSON(response.Msg)
	}
	if len(response.Msg.GetItems()) == 0 {
		a.info("the history is empty")
		return nil
	}

	devices, err := a.devicesOf(ctx, client)
	if err != nil {
		return err
	}
	return a.writeHistory(response.Msg.GetItems(), devices)
}

// historyApply writes one entry to the local clipboard. It is how a user takes
// an item that a device with the confirmation flag held back.
func (a *app) historyApply(ctx context.Context, args []string) error {
	flags := a.newFlagSet("history apply")
	order, positional := splitFlags(args, map[string]bool{"endpoint": true})
	if err := parseFlags(flags, append(order, positional...)); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return a.usageError("history apply takes one item id")
	}

	client, err := a.api()
	if err != nil {
		return err
	}
	id, err := resolveItem(ctx, client, flags.Arg(0))
	if err != nil {
		return err
	}
	response, err := client.ApplyHistoryItem(ctx, connect.NewRequest(&localv1.ApplyHistoryItemRequest{Id: id}))
	if err != nil {
		return fmt.Errorf("apply the item: %w", err)
	}
	if a.json {
		return a.writeJSON(response.Msg)
	}
	a.info("applied the item to the clipboard")
	return nil
}

// historyFetch downloads the payloads of one entry.
func (a *app) historyFetch(ctx context.Context, args []string) error {
	flags := a.newFlagSet("history fetch")
	path := flags.String("o", "", "write the payloads to this path")
	output := flags.String("output", "", "write the payloads to this path")
	if err := parseFlags(flags, args); err != nil {
		return err
	}
	if *path == "" {
		*path = *output
	}
	if flags.NArg() != 1 {
		return a.usageError("history fetch takes one item id")
	}

	client, err := a.api()
	if err != nil {
		return err
	}
	id, err := resolveItem(ctx, client, flags.Arg(0))
	if err != nil {
		return err
	}
	response, err := client.FetchHistoryItem(ctx, connect.NewRequest(&localv1.FetchHistoryItemRequest{
		Id:   id,
		Path: *path,
	}))
	if err != nil {
		return fmt.Errorf("fetch the item: %w", err)
	}

	if a.json {
		if response.Msg.Item != nil {
			response.Msg.Item.Item = withoutPayload(response.Msg.Item.GetItem())
		}
		return a.writeJSON(response.Msg)
	}
	files := response.Msg.GetFiles()
	if len(files) == 0 {
		a.info("the item is available on this device")
		return nil
	}
	for _, file := range files {
		a.info("saved %s", file)
	}
	return nil
}

// writeHistory prints the entries as a table.
func (a *app) writeHistory(items []*localv1.HistoryItem, devices []*localv1.Device) error {
	names := make(map[string]string, len(devices))
	for _, device := range devices {
		names[device.GetDeviceId()] = device.GetName()
	}

	rows := make([][]string, 0, len(items)+1)
	rows = append(rows, []string{"ID", "TYPE", "FROM", "CREATED", "SIZE", "SUMMARY"})
	for _, entry := range items {
		item := entry.GetItem()
		rows = append(rows, []string{
			item.GetId(),
			kindText(item.GetKind()),
			originText(item.GetOrigin(), names),
			formatTime(item.GetCreatedAtUnixMs()),
			sizeText(itemSize(item)),
			itemSummary(entry),
		})
	}
	return a.writeRows(rows)
}

// resolveItem returns the full id of the item a reference points at. A
// reference is a prefix of the id and it has to be unique.
func resolveItem(ctx context.Context, client localv1connect.UnisonServiceClient, reference string) (string, error) {
	reference = strings.ToLower(strings.TrimSpace(reference))
	if reference == "" {
		return "", errors.New("the item id is empty")
	}
	if id, err := model.ParseClipID(reference); err == nil {
		return id.String(), nil
	}

	response, err := client.ListHistory(ctx, connect.NewRequest(&localv1.ListHistoryRequest{
		Limit: maxItemReference,
	}))
	if err != nil {
		return "", fmt.Errorf("read the history: %w", err)
	}

	var matches []string
	for _, entry := range response.Msg.GetItems() {
		id := entry.GetItem().GetId()
		if strings.HasPrefix(strings.ToLower(id), reference) {
			matches = append(matches, id)
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no item matches %q", reference)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("%q matches %d items, use more of the id", reference, len(matches))
	}
}

// itemSummary describes an item in one line.
func itemSummary(entry *localv1.HistoryItem) string {
	label := ""
	if !entry.GetComplete() {
		label = " (not downloaded)"
	}

	item := entry.GetItem()
	if text, err := plainText(item); err == nil {
		text = strings.SplitN(text, "\n", 2)[0]
		if runes := []rune(text); len(runes) > 60 {
			text = string(runes[:60]) + "..."
		}
		return text + label
	}
	for _, representation := range item.GetRepresentations() {
		if name := representation.GetFileName(); name != "" {
			return name + label
		}
	}
	return kindText(item.GetKind()) + label
}

// itemSize returns the payload size of an item.
func itemSize(item *localv1.ClipItem) uint64 {
	var size uint64
	for _, representation := range item.GetRepresentations() {
		size += representation.GetSize()
	}
	return size
}

// sizeText formats a payload size.
func sizeText(size uint64) string {
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}

	value := float64(size)
	for _, suffix := range []string{"KiB", "MiB", "GiB"} {
		value /= 1024
		if value < 1024 {
			return fmt.Sprintf("%.1f %s", value, suffix)
		}
	}
	return fmt.Sprintf("%.1f TiB", value/1024)
}

// kindOf parses the type filter of the history.
func kindOf(value string) (localv1.Kind, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "all":
		return localv1.Kind_KIND_UNSPECIFIED, nil
	case "text":
		return localv1.Kind_KIND_TEXT, nil
	case "image":
		return localv1.Kind_KIND_IMAGE, nil
	case "file", "files":
		return localv1.Kind_KIND_FILES, nil
	default:
		return localv1.Kind_KIND_UNSPECIFIED, fmt.Errorf("unknown type %q, want text, image or file", value)
	}
}

// kindText returns the name of a kind.
func kindText(kind localv1.Kind) string {
	switch kind {
	case localv1.Kind_KIND_TEXT:
		return "text"
	case localv1.Kind_KIND_IMAGE:
		return "image"
	case localv1.Kind_KIND_FILES:
		return "files"
	default:
		return "unknown"
	}
}

// originText returns the name of the device that captured the item or a
// fingerprint when it is not a paired device anymore.
func originText(origin string, names map[string]string) string {
	if name := names[origin]; name != "" {
		return name
	}
	return fingerprintOf(origin)
}
