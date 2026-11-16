package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"connectrpc.com/connect"

	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
	"github.com/lebe-dev/unison/proto/unison/local/v1/localv1connect"
)

// transfers runs the file transfer subcommands. Without a subcommand it lists
// the transfers.
func (a *app) transfers(ctx context.Context, args []string) error {
	command, rest := subcommand(args)
	switch command {
	case "", "ls":
		return a.transfersList(ctx, rest)
	case "cancel":
		return a.transfersCancel(ctx, rest)
	default:
		return a.usageError("unknown transfers command %q", command)
	}
}

// transfersList prints the file transfers of this device, newest first.
func (a *app) transfersList(ctx context.Context, args []string) error {
	flags := a.newFlagSet("transfers")
	positional, err := parseCommand(flags, args)
	if err != nil {
		return err
	}
	if len(positional) != 0 {
		return a.usageError("transfers takes no arguments")
	}

	client, err := a.api()
	if err != nil {
		return err
	}
	response, err := client.ListTransfers(ctx, connect.NewRequest(&localv1.ListTransfersRequest{}))
	if err != nil {
		return fmt.Errorf("read the transfers: %w", err)
	}
	if a.json {
		return a.writeJSON(response.Msg)
	}
	if len(response.Msg.GetTransfers()) == 0 {
		a.info("no transfers")
		return nil
	}

	rows := make([][]string, 0, len(response.Msg.GetTransfers())+1)
	rows = append(rows, []string{"ID", "DIRECTION", "DEVICE", "STATE", "FILES", "PROGRESS", "CREATED"})
	for _, transfer := range response.Msg.GetTransfers() {
		rows = append(rows, []string{
			transfer.GetId(),
			directionText(transfer.GetDirection()),
			transfer.GetDeviceName(),
			transferStateText(transfer.GetState()),
			transferFiles(transfer),
			progressText(transfer),
			formatTime(transfer.GetCreatedAtUnixMs()),
		})
	}
	return a.writeRows(rows)
}

// transfersCancel stops one transfer.
func (a *app) transfersCancel(ctx context.Context, args []string) error {
	flags := a.newFlagSet("transfers cancel")
	positional, err := parseCommand(flags, args)
	if err != nil {
		return err
	}
	if len(positional) != 1 {
		return a.usageError("transfers cancel takes one transfer id")
	}

	client, err := a.api()
	if err != nil {
		return err
	}
	id, err := resolveTransfer(ctx, client, positional[0])
	if err != nil {
		return err
	}
	response, err := client.CancelTransfer(ctx, connect.NewRequest(&localv1.CancelTransferRequest{Id: id}))
	if err != nil {
		return fmt.Errorf("cancel the transfer: %w", err)
	}
	if a.json {
		return a.writeJSON(response.Msg)
	}
	a.info("canceled the transfer")
	return nil
}

// resolveTransfer returns the full id of the transfer a reference points at. A
// reference is a prefix of the id and it has to be unique.
func resolveTransfer(ctx context.Context, client localv1connect.UnisonServiceClient, reference string) (string, error) {
	reference = strings.ToLower(strings.TrimSpace(reference))
	if reference == "" {
		return "", errors.New("the transfer id is empty")
	}

	response, err := client.ListTransfers(ctx, connect.NewRequest(&localv1.ListTransfersRequest{}))
	if err != nil {
		return "", fmt.Errorf("read the transfers: %w", err)
	}
	var matches []string
	for _, transfer := range response.Msg.GetTransfers() {
		if strings.HasPrefix(strings.ToLower(transfer.GetId()), reference) {
			matches = append(matches, transfer.GetId())
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no transfer matches %q", reference)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("%q matches %d transfers", reference, len(matches))
	}
}

// directionText names the direction of a transfer.
func directionText(direction localv1.TransferDirection) string {
	switch direction {
	case localv1.TransferDirection_TRANSFER_DIRECTION_OUTGOING:
		return "out"
	case localv1.TransferDirection_TRANSFER_DIRECTION_INCOMING:
		return "in"
	default:
		return "unknown"
	}
}

// transferStateText names the state of a transfer in lower case.
func transferStateText(state localv1.TransferState) string {
	name := strings.ToLower(strings.TrimPrefix(state.String(), "TRANSFER_STATE_"))
	if state == localv1.TransferState_TRANSFER_STATE_UNSPECIFIED || name == "" {
		return "unknown"
	}
	return name
}

// progressText describes the bytes of a transfer that moved so far.
func progressText(transfer *localv1.Transfer) string {
	return fmt.Sprintf("%s / %s", sizeText(transfer.GetBytes()), sizeText(transfer.GetTotalBytes()))
}

// transferFiles describes the files of a transfer in one line.
func transferFiles(transfer *localv1.Transfer) string {
	files := transfer.GetFiles()
	switch len(files) {
	case 0:
		return "no files"
	case 1:
		return files[0].GetName()
	default:
		return countText(len(files), "file", "files")
	}
}
