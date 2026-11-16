package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"

	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
	"github.com/lebe-dev/unison/proto/unison/local/v1/localv1connect"
)

// devices runs the device subcommands. Without a subcommand it lists the
// devices.
func (a *app) devices(ctx context.Context, args []string) error {
	command, rest := subcommand(args, "wait")
	switch command {
	case "", "ls":
		return a.devicesList(ctx, rest)
	case "rename":
		return a.devicesRename(ctx, rest)
	case "confirm":
		return a.devicesConfirm(ctx, rest)
	case "address":
		return a.devicesAddress(ctx, rest)
	case "revoke":
		return a.devicesRevoke(ctx, rest)
	default:
		return a.usageError("unknown devices command %q", command)
	}
}

// devicesList prints the paired devices.
func (a *app) devicesList(ctx context.Context, args []string) error {
	flags := a.newFlagSet("devices")
	positional, err := parseCommand(flags, args)
	if err != nil {
		return err
	}
	if len(positional) != 0 {
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

// devicesRename changes the display name of a device. The name of a peer is
// local to this device; the name of this device is announced to the peers.
func (a *app) devicesRename(ctx context.Context, args []string) error {
	flags := a.newFlagSet("devices rename")
	positional, err := parseCommand(flags, args)
	if err != nil {
		return err
	}
	if len(positional) != 2 {
		return a.usageError("devices rename needs an id and a name")
	}
	reference, name := positional[0], strings.TrimSpace(positional[1])
	if name == "" {
		return a.usageError("the device name is empty")
	}

	client, err := a.api()
	if err != nil {
		return err
	}
	target, self, err := a.renameTarget(ctx, client, reference)
	if err != nil {
		return err
	}

	request := &localv1.RenameDeviceRequest{DeviceId: target, Name: name}
	response, err := client.RenameDevice(ctx, connect.NewRequest(request))
	if err != nil {
		return fmt.Errorf("rename the device: %w", err)
	}
	if a.json {
		return a.writeJSON(response.Msg)
	}
	if self {
		a.info("renamed this device to %q", name)
		return nil
	}
	a.info("renamed %s to %q", fingerprintOf(target), name)
	return nil
}

// renameTarget resolves the device a rename refers to. The local device is
// named by self.
func (a *app) renameTarget(ctx context.Context, client localv1connect.UnisonServiceClient, reference string) (string, bool, error) {
	if reference == "self" {
		status, err := client.GetStatus(ctx, connect.NewRequest(&localv1.GetStatusRequest{}))
		if err != nil {
			return "", false, fmt.Errorf("read the local device: %w", err)
		}
		if !status.Msg.GetRunning() {
			return "", false, errors.New("the daemon is not running")
		}
		return status.Msg.GetDeviceId(), true, nil
	}
	devices, err := a.devicesOf(ctx, client)
	if err != nil {
		return "", false, err
	}
	device, err := resolveDevice(devices, reference)
	if err != nil {
		return "", false, err
	}
	return device.GetDeviceId(), false, nil
}

// devicesConfirm sets a confirmation flag: of this device for every device, of
// this device for a paired one, or with --peer of the paired device for this
// one. A paired device changes its own flag, so that request travels there.
func (a *app) devicesConfirm(ctx context.Context, args []string) error {
	flags := a.newFlagSet("devices confirm")
	files := flags.Bool("files", false, "the flag of the file transfers instead of the clipboard")
	peer := flags.Bool("peer", false, "change the flag of the paired device for this device")
	positional, err := parseCommand(flags, args)
	if err != nil {
		return err
	}
	if len(positional) != 2 {
		return a.usageError("devices confirm needs a device and on or off")
	}
	reference := positional[0]
	state := strings.ToLower(positional[1])

	var require bool
	switch state {
	case "on":
		require = true
	case "off":
		require = false
	default:
		return a.usageError("confirmation is on or off, not %q", state)
	}

	client, err := a.api()
	if err != nil {
		return err
	}
	scope, scopeText := localv1.ConfirmationScope_CONFIRMATION_SCOPE_CLIPBOARD, "clipboard"
	if *files {
		scope, scopeText = localv1.ConfirmationScope_CONFIRMATION_SCOPE_FILES, "files"
	}
	side := localv1.ConfirmationSide_CONFIRMATION_SIDE_LOCAL
	if *peer {
		if reference == "self" {
			return a.usageError("--peer needs a paired device, not self")
		}
		side = localv1.ConfirmationSide_CONFIRMATION_SIDE_PEER
	}
	request := &localv1.SetDeviceConfirmationRequest{Require: require, Scope: scope, Side: side}
	label := "every device"
	if reference != "self" {
		devices, err := a.devicesOf(ctx, client)
		if err != nil {
			return err
		}
		device, err := resolveDevice(devices, reference)
		if err != nil {
			return err
		}
		request.DeviceId = device.GetDeviceId()
		label = fingerprintOf(device.GetDeviceId())
		if *peer {
			label = "this device on " + label
		}
	} else {
		request.Self = true
	}

	if _, err := client.SetDeviceConfirmation(ctx, connect.NewRequest(request)); err != nil {
		return fmt.Errorf("set the confirmation flag: %w", err)
	}
	if a.json {
		return a.writeJSON(&localv1.SetDeviceConfirmationResponse{})
	}
	a.info("%s confirmation for %s is %s", scopeText, label, state)
	return nil
}

// addressWait is how long devices address waits for the connection by default.
const addressWait = 5 * time.Second

// onlinePoll is how often devices address asks for the state of the device.
const onlinePoll = 100 * time.Millisecond

// devicesAddress stores an address of a paired device that the user entered and
// waits for the daemon to connect to it. The address is tried before the
// discovered and cached ones, so it connects devices on a network where
// discovery does not work.
func (a *app) devicesAddress(ctx context.Context, args []string) error {
	flags := a.newFlagSet("devices address")
	wait := flags.Duration("wait", addressWait, "how long to wait for the connection")
	positional, err := parseCommand(flags, args)
	if err != nil {
		return err
	}
	if len(positional) != 2 {
		return a.usageError("devices address needs a device and an address")
	}
	reference, value := positional[0], positional[1]
	addr, err := peerAddress(value)
	if err != nil {
		return a.usageError("%v", err)
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

	request := &localv1.SetPeerAddressRequest{DeviceId: device.GetDeviceId(), Addr: addr}
	response, err := client.SetPeerAddress(ctx, connect.NewRequest(request))
	if err != nil {
		return fmt.Errorf("set the address of the device: %w", err)
	}
	if a.json {
		return a.writeJSON(response.Msg)
	}
	a.info("address %s saved for %s", addr, deviceLabel(device.GetDeviceId(), device.GetName()))
	if *wait <= 0 {
		return nil
	}
	online, err := a.waitOnline(ctx, client, device.GetDeviceId(), *wait)
	if err != nil {
		return err
	}
	if !online {
		a.info("the device is not online yet, the daemon keeps trying")
		return nil
	}
	a.info("the device is online")
	return nil
}

// waitOnline waits until the daemon reports the device online. The address is
// stored either way, so a timeout is not an error.
func (a *app) waitOnline(ctx context.Context, client localv1connect.UnisonServiceClient, id string, limit time.Duration) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()

	ticker := time.NewTicker(onlinePoll)
	defer ticker.Stop()
	for {
		devices, err := a.devicesOf(ctx, client)
		if err != nil {
			if ctx.Err() != nil {
				return false, nil
			}
			return false, err
		}
		for _, device := range devices {
			if device.GetDeviceId() == id && device.GetOnline() {
				return true, nil
			}
		}
		select {
		case <-ctx.Done():
			return false, nil
		case <-ticker.C:
		}
	}
}

// peerAddress validates a host:port address of a paired device. An IPv6 host is
// written in brackets.
func peerAddress(value string) (string, error) {
	addr := strings.TrimSpace(value)
	if addr == "" {
		return "", errors.New("the address is empty")
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", err
	}
	if host == "" {
		return "", errors.New("the address has no host")
	}
	if number, err := strconv.Atoi(port); err != nil || number < 1 || number > 65535 {
		return "", fmt.Errorf("the address %q has no port between 1 and 65535", addr)
	}
	return addr, nil
}

// devicesRevoke revokes a paired device or, with self, leaves the group. Both
// are irreversible and ask for a confirmation.
func (a *app) devicesRevoke(ctx context.Context, args []string) error {
	flags := a.newFlagSet("devices revoke")
	assumeYes := flags.Bool("yes", false, "revoke without asking for a confirmation")
	positional, err := parseCommand(flags, args)
	if err != nil {
		return err
	}
	if len(positional) != 1 {
		return a.usageError("devices revoke needs one device")
	}
	reference := positional[0]

	client, err := a.api()
	if err != nil {
		return err
	}
	request := &localv1.RevokeDeviceRequest{}
	question := "revoke this device? This cannot be undone. [y/N]: "
	self := reference == "self"
	if self {
		status, err := client.GetStatus(ctx, connect.NewRequest(&localv1.GetStatusRequest{}))
		if err != nil {
			return fmt.Errorf("read the local device: %w", err)
		}
		if !status.Msg.GetRunning() {
			return errors.New("the daemon is not running")
		}
		request.DeviceId = status.Msg.GetDeviceId()
		question = "leave the group and revoke this device? This cannot be undone. [y/N]: "
	} else {
		devices, err := a.devicesOf(ctx, client)
		if err != nil {
			return err
		}
		device, err := resolveDevice(devices, reference)
		if err != nil {
			return err
		}
		request.DeviceId = device.GetDeviceId()
	}

	if !*assumeYes {
		ok, err := a.confirm(question)
		if err != nil {
			return err
		}
		if !ok {
			a.info("nothing was revoked")
			return nil
		}
	}

	if _, err := client.RevokeDevice(ctx, connect.NewRequest(request)); err != nil {
		return fmt.Errorf("revoke the device: %w", err)
	}
	if a.json {
		return a.writeJSON(&localv1.RevokeDeviceResponse{})
	}
	if self {
		a.info("left the group")
		return nil
	}
	a.info("revoked %s", fingerprintOf(request.DeviceId))
	return nil
}

// confirm asks a question on stderr and reads one line of the input.
func (a *app) confirm(question string) (bool, error) {
	_, _ = fmt.Fprint(a.stderr, question)
	scanner := bufio.NewScanner(a.stdin)
	if !scanner.Scan() {
		return false, scanner.Err()
	}
	answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
	return answer == "y" || answer == "yes", nil
}

// devicesOf returns the paired devices.
func (a *app) devicesOf(ctx context.Context, client localv1connect.UnisonServiceClient) ([]*localv1.Device, error) {
	response, err := client.ListDevices(ctx, connect.NewRequest(&localv1.ListDevicesRequest{}))
	if err != nil {
		return nil, fmt.Errorf("read the devices: %w", err)
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
