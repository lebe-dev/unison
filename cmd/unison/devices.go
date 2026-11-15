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

// devices runs the device subcommands. Without a subcommand it lists the
// devices.
func (a *app) devices(ctx context.Context, args []string) error {
	flags := a.newFlagSet("devices")
	if err := parseFlags(flags, args); err != nil {
		return err
	}
	rest := flags.Args()
	if len(rest) == 0 {
		return a.devicesList(ctx, rest)
	}
	command, rest := rest[0], rest[1:]
	switch command {
	case "ls":
		return a.devicesList(ctx, rest)
	case "rename":
		return a.devicesRename(ctx, rest)
	default:
		return a.usageError("unknown devices command %q", command)
	}
}

// devicesList prints the paired devices.
func (a *app) devicesList(ctx context.Context, args []string) error {
	flags := a.newFlagSet("devices")
	if err := parseFlags(flags, args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return a.usageError("devices takes no arguments")
	}

	client, err := a.api()
	if err != nil {
		return err
	}
	devices, err := a.devicesOf(ctx, client)
	if err != nil {
		return err
	}
	if a.json {
		return a.writeJSON(&localv1.ListDevicesResponse{Devices: devices})
	}
	if len(devices) == 0 {
		a.info("no paired devices")
		return nil
	}
	return a.writeTable(devices)
}

// devicesRename changes the display name of a device.
func (a *app) devicesRename(ctx context.Context, args []string) error {
	flags := a.newFlagSet("devices rename")
	if err := parseFlags(flags, args); err != nil {
		return err
	}
	if flags.NArg() != 2 {
		return a.usageError("devices rename needs an id and a name")
	}
	reference, name := flags.Arg(0), strings.TrimSpace(flags.Arg(1))
	if name == "" {
		return a.usageError("the device name is empty")
	}

	client, err := a.api()
	if err != nil {
		return err
	}
	devices, err := a.devicesOf(ctx, client)
	if err != nil {
		return err
	}
	device, err := resolveDevice(devices, reference)
	if err != nil {
		return err
	}

	request := &localv1.RenameDeviceRequest{DeviceId: device.GetDeviceId(), Name: name}
	if _, err := client.RenameDevice(ctx, connect.NewRequest(request)); err != nil {
		return fmt.Errorf("rename the device: %w", err)
	}
	device.Name = name
	if a.json {
		return a.writeJSON(device)
	}
	a.info("renamed %s to %q", fingerprintOf(device.GetDeviceId()), name)
	return nil
}

// devicesOf returns the paired devices. The local API does not serve the device
// list yet, so the status carries it.
func (a *app) devicesOf(ctx context.Context, client localv1connect.UnisonServiceClient) ([]*localv1.Device, error) {
	response, err := client.GetStatus(ctx, connect.NewRequest(&localv1.GetStatusRequest{}))
	if err != nil {
		return nil, fmt.Errorf("read the devices: %w", err)
	}
	if !response.Msg.GetRunning() {
		return nil, errors.New("the daemon is not running")
	}
	return response.Msg.GetDevices(), nil
}

// resolveDevice finds the device a reference points at. A reference is the name
// of a device, a prefix of its name, a prefix of its fingerprint or a prefix of
// its device ID, and it has to be unique.
func resolveDevice(devices []*localv1.Device, reference string) (*localv1.Device, error) {
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return nil, errors.New("the device reference is empty")
	}

	fingerprint := strings.ToUpper(strings.ReplaceAll(reference, "-", ""))
	identifier := strings.ToLower(reference)
	tiers := []func(*localv1.Device) bool{
		func(device *localv1.Device) bool {
			return strings.EqualFold(device.GetName(), reference)
		},
		func(device *localv1.Device) bool {
			short := strings.ReplaceAll(fingerprintOf(device.GetDeviceId()), "-", "")
			return fingerprint != "" && strings.HasPrefix(short, fingerprint)
		},
		func(device *localv1.Device) bool {
			return strings.HasPrefix(strings.ToLower(device.GetDeviceId()), identifier)
		},
		func(device *localv1.Device) bool {
			return strings.HasPrefix(strings.ToLower(device.GetName()), identifier)
		},
	}
	for _, match := range tiers {
		device, err := singleMatch(reference, devices, match)
		if err != nil || device != nil {
			return device, err
		}
	}
	return nil, fmt.Errorf("no device matches %q", reference)
}

// singleMatch returns the only device that matches, an error when several do
// and nil when none does.
func singleMatch(reference string, devices []*localv1.Device, match func(*localv1.Device) bool) (*localv1.Device, error) {
	var matches []*localv1.Device
	for _, device := range devices {
		if match(device) {
			matches = append(matches, device)
		}
	}
	switch len(matches) {
	case 0:
		return nil, nil
	case 1:
		return matches[0], nil
	default:
		return nil, ambiguousDevice(reference, matches)
	}
}

// ambiguousDevice reports a reference that several devices match.
func ambiguousDevice(reference string, devices []*localv1.Device) error {
	names := make([]string, 0, len(devices))
	for _, device := range devices {
		names = append(names, fmt.Sprintf("%s (%s)", fingerprintOf(device.GetDeviceId()), device.GetName()))
	}
	return fmt.Errorf("%q matches %d devices: %s", reference, len(devices), strings.Join(names, ", "))
}
