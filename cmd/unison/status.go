package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"

	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
)

// status prints the state of the daemon, of the network and of the peers.
func (a *app) status(ctx context.Context, args []string) error {
	flags := a.newFlagSet("status")
	if err := parseFlags(flags, args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return a.usageError("status takes no arguments")
	}

	client, err := a.api()
	if err != nil {
		return err
	}
	response, err := client.GetStatus(ctx, connect.NewRequest(&localv1.GetStatusRequest{}))
	if err != nil {
		return fmt.Errorf("read the status: %w", err)
	}
	if a.json {
		return a.writeJSON(response.Msg)
	}
	a.writeStatus(response.Msg)
	return nil
}

// writeStatus writes the human readable form of a status.
func (a *app) writeStatus(status *localv1.GetStatusResponse) {
	state := "stopped"
	if status.GetRunning() {
		state = "running"
	}
	a.field("daemon", fmt.Sprintf("%s %s", status.GetVersion(), state))

	device := status.GetDeviceName()
	if id := status.GetDeviceId(); id != "" {
		device = fmt.Sprintf("%s (%s)", device, fingerprintOf(id))
	}
	a.field("device", device)

	if !status.GetRunning() {
		return
	}
	if addrs := status.GetAddrs(); len(addrs) > 0 {
		a.field("address", strings.Join(addrs, ", "))
	}
	a.field("network", networkText(status.GetNetwork()))
	a.field("clipboard", pauseText(status.GetPause()))
	if search := status.GetFriendsDiscovery(); search.GetOn() {
		a.field("friends", fmt.Sprintf("searching until %s", time.UnixMilli(search.GetUntilUnixMs()).Format("15:04")))
	}

	online, offline := 0, 0
	for _, device := range status.GetDevices() {
		if device.GetOnline() {
			online++
			continue
		}
		offline++
	}
	a.field("peers", fmt.Sprintf("%d online, %d offline", online, offline))
}
