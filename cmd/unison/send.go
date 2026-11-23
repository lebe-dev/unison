package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"connectrpc.com/connect"

	"github.com/lebe-dev/unison/core/download"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
)

// stdinName is the default name of the payload read from standard input.
const stdinName = "stdin"

// stringList collects a flag that may be repeated.
type stringList []string

// String returns the collected values.
func (l *stringList) String() string {
	return strings.Join(*l, ",")
}

// Set appends the values of one flag, which may be separated by commas.
func (l *stringList) Set(value string) error {
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		*l = append(*l, part)
	}
	return nil
}

// send offers files of this device to the paired devices.
func (a *app) send(ctx context.Context, args []string) error {
	flags := a.newFlagSet("send")
	var to stringList
	flags.Var(&to, "to", "device to send the files to, may be repeated")
	name := flags.String("name", stdinName, "file name of the standard input payload")
	positional, err := parseCommand(flags, args)
	if err != nil {
		return err
	}
	if len(positional) != 1 {
		return a.usageError("send takes one path or - for standard input")
	}
	if len(to) == 0 {
		return a.usageError("send needs at least one --to <device>")
	}

	path := positional[0]
	var cleanup func()
	if path == "-" {
		written, remove, err := a.writeStdin(*name)
		if err != nil {
			return err
		}
		path, cleanup = written, remove
		defer cleanup()
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve the path: %w", err)
	}
	if info, err := os.Stat(absolute); err != nil {
		return fmt.Errorf("read the file: %w", err)
	} else if !info.Mode().IsRegular() {
		return errors.New("send takes a regular file")
	}

	client, err := a.api()
	if err != nil {
		return err
	}
	devices, err := a.devicesOf(ctx, client)
	if err != nil {
		return err
	}
	targets := make([]string, 0, len(to))
	for _, reference := range to {
		device, err := resolveDevice(devices, reference)
		if err != nil {
			return err
		}
		targets = append(targets, device.GetDeviceId())
	}

	response, err := client.SendFiles(ctx, connect.NewRequest(&localv1.SendFilesRequest{
		Paths:     []string{absolute},
		DeviceIds: targets,
	}))
	if err != nil {
		return fmt.Errorf("send the file: %w", err)
	}
	if a.json {
		return a.writeJSON(response.Msg)
	}
	for _, transfer := range response.Msg.GetTransfers() {
		a.info("offered %s to %s", transferText(transfer), transfer.GetDeviceName())
	}
	if len(response.Msg.GetTransfers()) == 0 {
		a.info("nothing was offered")
	}
	return nil
}

// writeStdin copies standard input to a temporary file and returns its path and
// the function that removes it.
func (a *app) writeStdin(name string) (string, func(), error) {
	name = download.SafeName(strings.TrimSpace(name))
	if name == "" || name == "file" {
		name = stdinName
	}
	dir, err := os.MkdirTemp("", "unison-send-")
	if err != nil {
		return "", nil, fmt.Errorf("create a temporary directory: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(dir) }

	path := filepath.Join(dir, name)
	//nolint:gosec // The file lives in the temporary directory of the command.
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("create a temporary file: %w", err)
	}
	_, copyErr := io.Copy(file, a.stdin)
	closeErr := file.Close()
	if copyErr != nil {
		cleanup()
		return "", nil, fmt.Errorf("read standard input: %w", copyErr)
	}
	if closeErr != nil {
		cleanup()
		return "", nil, fmt.Errorf("close the temporary file: %w", closeErr)
	}
	return path, cleanup, nil
}

// transferText describes the files of a transfer in one line.
func transferText(transfer *localv1.Transfer) string {
	files := transfer.GetFiles()
	switch len(files) {
	case 0:
		return "no files"
	case 1:
		return fmt.Sprintf("%q (%s)", files[0].GetName(), sizeText(files[0].GetSize()))
	default:
		return fmt.Sprintf("%d files (%s)", len(files), sizeText(transfer.GetTotalBytes()))
	}
}
