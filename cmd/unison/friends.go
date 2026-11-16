package main

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect"

	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
)

// friends runs the subcommands of the search for Friends devices.
func (a *app) friends(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return a.usageError("friends needs a subcommand: search")
	}
	command, rest := args[0], args[1:]
	switch command {
	case "search":
		return a.friendsSearch(ctx, rest)
	default:
		return a.usageError("unknown friends subcommand %q", command)
	}
}

// friendsSearch turns the temporary search for Friends devices on or off.
func (a *app) friendsSearch(ctx context.Context, args []string) error {
	flags := a.newFlagSet("friends search")
	off := flags.Bool("off", false, "turn the search off")
	if err := parseFlags(flags, args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return a.usageError("friends search takes no arguments")
	}

	client, err := a.api()
	if err != nil {
		return err
	}
	response, err := client.SetFriendsDiscovery(ctx, connect.NewRequest(&localv1.SetFriendsDiscoveryRequest{On: !*off}))
	if err != nil {
		return fmt.Errorf("change the friends discovery: %w", err)
	}
	if a.json {
		return a.writeJSON(response.Msg)
	}

	search := response.Msg.GetFriendsDiscovery()
	if !search.GetOn() {
		a.info("friends discovery: off")
		return nil
	}
	a.info("friends discovery: on until %s", time.UnixMilli(search.GetUntilUnixMs()).Format("15:04"))
	a.info("turn it on on the other device too, otherwise the devices do not meet")
	return nil
}
