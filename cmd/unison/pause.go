package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"

	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
)

// pause prints or changes the pause of the clipboard synchronization.
func (a *app) pause(ctx context.Context, args []string) error {
	flags := a.newFlagSet("pause")
	positional, err := parseCommand(flags, args)
	if err != nil {
		return err
	}
	if len(positional) > 1 {
		return a.usageError("pause takes one argument: a duration, on or off")
	}

	client, err := a.api()
	if err != nil {
		return err
	}
	if len(positional) == 0 {
		response, err := client.GetPause(ctx, connect.NewRequest(&localv1.GetPauseRequest{}))
		if err != nil {
			return fmt.Errorf("read the pause: %w", err)
		}
		if a.json {
			return a.writeJSON(response.Msg)
		}
		a.info("clipboard synchronization: %s", pauseText(response.Msg.GetPause()))
		return nil
	}

	request, err := pauseRequest(positional[0])
	if err != nil {
		return a.usageError("%v", err)
	}
	response, err := client.SetPause(ctx, connect.NewRequest(request))
	if err != nil {
		return fmt.Errorf("set the pause: %w", err)
	}
	if a.json {
		return a.writeJSON(response.Msg)
	}
	a.info("clipboard synchronization: %s", pauseText(response.Msg.GetPause()))
	return nil
}

// pauseRequest returns the request of a pause argument: a duration, on or off.
func pauseRequest(argument string) (*localv1.SetPauseRequest, error) {
	switch strings.ToLower(argument) {
	case "on":
		return &localv1.SetPauseRequest{Mode: localv1.PauseMode_PAUSE_MODE_INDEFINITE}, nil
	case "off":
		return &localv1.SetPauseRequest{Mode: localv1.PauseMode_PAUSE_MODE_OFF}, nil
	}

	duration, err := time.ParseDuration(argument)
	if err != nil || duration <= 0 {
		return nil, fmt.Errorf("unknown pause %q, use a duration, on or off", argument)
	}
	return &localv1.SetPauseRequest{
		Mode:        localv1.PauseMode_PAUSE_MODE_UNTIL,
		UntilUnixMs: time.Now().Add(duration).UnixMilli(),
	}, nil
}
