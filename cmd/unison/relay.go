package main

import (
	"context"
	"flag"
	"fmt"
	"time"

	"connectrpc.com/connect"

	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
)

// relay runs the subcommands of the relay access. Without a subcommand it shows
// the state.
func (a *app) relay(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return a.relayStatus(ctx, nil)
	}
	command, rest := args[0], args[1:]
	switch command {
	case "status":
		return a.relayStatus(ctx, rest)
	case "set":
		return a.relaySet(ctx, rest)
	case "off":
		return a.relayOff(ctx, rest)
	default:
		return a.usageError("unknown relay subcommand %q", command)
	}
}

// relayStatus prints the relay access of the group and the state of the client.
func (a *app) relayStatus(ctx context.Context, args []string) error {
	flags := a.newFlagSet("relay")
	if err := parseFlags(flags, args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return a.usageError("relay takes no arguments")
	}

	client, err := a.api()
	if err != nil {
		return err
	}
	response, err := client.GetRelay(ctx, connect.NewRequest(&localv1.GetRelayRequest{}))
	if err != nil {
		return fmt.Errorf("read the relay settings: %w", err)
	}
	if a.json {
		return a.writeJSON(response.Msg)
	}
	a.writeRelay(response.Msg.GetRelay())
	return nil
}

// relaySet stores the relay address and the access token of the group. The other
// Personal devices receive them with the group data.
func (a *app) relaySet(ctx context.Context, args []string) error {
	flags := a.newFlagSet("relay set")
	var token string
	var filesOnRequest bool
	flags.StringVar(&token, "token", "", "access token of the relay")
	flags.BoolVar(&filesOnRequest, "files-on-request", false, "transfer files over the relay only when asked")
	valued := map[string]bool{"token": true, "endpoint": true}
	flagArgs, rest := splitFlags(args, valued)
	if err := parseFlags(flags, flagArgs); err != nil {
		return err
	}
	if len(rest) != 1 {
		return a.usageError("relay set takes one address")
	}
	if token == "" {
		return a.usageError("relay set needs --token")
	}

	var filesSet bool
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "files-on-request" {
			filesSet = true
		}
	})

	client, err := a.api()
	if err != nil {
		return err
	}
	request := &localv1.SetRelayRequest{Url: rest[0], Token: &token}
	if filesSet {
		request.FilesOnRequest = &filesOnRequest
	}
	response, err := client.SetRelay(ctx, connect.NewRequest(request))
	if err != nil {
		return fmt.Errorf("set the relay: %w", err)
	}
	if a.json {
		return a.writeJSON(response.Msg)
	}
	a.info("the group uses the relay at %s", rest[0])
	a.writeRelay(response.Msg.GetRelay())
	return nil
}

// relayOff turns the relay off for the group.
func (a *app) relayOff(ctx context.Context, args []string) error {
	flags := a.newFlagSet("relay off")
	if err := parseFlags(flags, args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return a.usageError("relay off takes no arguments")
	}

	client, err := a.api()
	if err != nil {
		return err
	}
	response, err := client.RelayOff(ctx, connect.NewRequest(&localv1.RelayOffRequest{}))
	if err != nil {
		return fmt.Errorf("turn the relay off: %w", err)
	}
	if a.json {
		return a.writeJSON(response.Msg)
	}
	a.info("the group does not use a relay")
	return nil
}

// writeRelayStatus writes one line about the relay for the status output.
func (a *app) writeRelayStatus(relay *localv1.Relay) {
	if relay.GetUrl() == "" {
		return
	}
	a.field("relay", fmt.Sprintf("%s (%s)", relay.GetUrl(), relayStateText(relay)))
}

// writeRelay writes the human readable form of the relay state.
func (a *app) writeRelay(relay *localv1.Relay) {
	if relay.GetUrl() == "" {
		a.field("relay", "off")
		return
	}
	a.field("relay", relay.GetUrl())
	a.field("relay state", relayStateText(relay))
	a.field("relay token", tokenText(relay.GetTokenSet()))
	a.field("relay files", filesText(relay.GetFilesOnRequest()))
	if at := relay.GetChangedAtUnixMs(); at != 0 {
		a.field("relay changed", time.UnixMilli(at).Format(time.RFC3339))
	}
}

// relayStateText describes the connection to the relay.
func relayStateText(relay *localv1.Relay) string {
	if !relay.GetConnected() {
		return "not connected"
	}
	switch relay.GetTransport() {
	case localv1.RelayTransport_RELAY_TRANSPORT_QUIC:
		return "connected over QUIC"
	case localv1.RelayTransport_RELAY_TRANSPORT_WSS:
		return "connected over WSS"
	default:
		return "connected"
	}
}

// tokenText reports whether an access token is stored.
func tokenText(set bool) string {
	if !set {
		return "missing"
	}
	return "stored"
}

// filesText describes the rule for files over the relay.
func filesText(onRequest bool) string {
	if onRequest {
		return "only on request"
	}
	return "allowed"
}
