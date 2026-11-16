package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"connectrpc.com/connect"

	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
)

// requests runs the request subcommands. Without a subcommand it lists the
// requests.
func (a *app) requests(ctx context.Context, args []string) error {
	command, rest := subcommand(args)
	switch command {
	case "", "ls":
		return a.requestsList(ctx, rest)
	case "accept":
		return a.requestsDecide(ctx, rest, true)
	case "decline":
		return a.requestsDecide(ctx, rest, false)
	default:
		return a.usageError("unknown requests command %q", command)
	}
}

// requestsList prints the incoming file transfer requests that wait for a
// decision.
func (a *app) requestsList(ctx context.Context, args []string) error {
	flags := a.newFlagSet("requests")
	order, positional := splitFlags(args, map[string]bool{"endpoint": true})
	if err := parseFlags(flags, append(order, positional...)); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return a.usageError("requests takes no arguments")
	}

	client, err := a.api()
	if err != nil {
		return err
	}
	response, err := client.ListRequests(ctx, connect.NewRequest(&localv1.ListRequestsRequest{}))
	if err != nil {
		return fmt.Errorf("read the requests: %w", err)
	}
	if a.json {
		return a.writeJSON(response.Msg)
	}
	if len(response.Msg.GetRequests()) == 0 {
		a.info("no transfer requests")
		return nil
	}

	rows := make([][]string, 0, len(response.Msg.GetRequests())+1)
	rows = append(rows, []string{"ID", "FROM", "FILES", "SIZE", "EXPIRES"})
	for _, request := range response.Msg.GetRequests() {
		rows = append(rows, []string{
			request.GetId(),
			request.GetDeviceName(),
			requestFiles(request),
			sizeText(request.GetTotalBytes()),
			formatTime(request.GetExpiresAtUnixMs()),
		})
	}
	return a.writeRows(rows)
}

// requestsDecide accepts or declines one request.
func (a *app) requestsDecide(ctx context.Context, args []string, accept bool) error {
	name := "requests accept"
	if !accept {
		name = "requests decline"
	}
	flags := a.newFlagSet(name)
	order, positional := splitFlags(args, map[string]bool{"endpoint": true})
	if err := parseFlags(flags, append(order, positional...)); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return a.usageError("%s takes one request id", name)
	}

	client, err := a.api()
	if err != nil {
		return err
	}
	requests, err := client.ListRequests(ctx, connect.NewRequest(&localv1.ListRequestsRequest{}))
	if err != nil {
		return fmt.Errorf("read the requests: %w", err)
	}
	id, err := resolveRequest(requests.Msg.GetRequests(), flags.Arg(0))
	if err != nil {
		return err
	}

	response, err := client.DecideRequest(ctx, connect.NewRequest(&localv1.DecideRequestRequest{
		Id:     id,
		Accept: accept,
	}))
	if err != nil {
		return fmt.Errorf("decide the request: %w", err)
	}
	if a.json {
		return a.writeJSON(response.Msg)
	}
	request := response.Msg.GetRequest()
	if !accept {
		a.info("declined %s from %s", requestFiles(request), request.GetDeviceName())
		return nil
	}
	a.info("accepted %s from %s", requestFiles(request), request.GetDeviceName())
	return nil
}

// resolveRequest returns the full id of the request a reference points at. A
// reference is a prefix of the id and it has to be unique.
func resolveRequest(requests []*localv1.ShareRequest, reference string) (string, error) {
	reference = strings.ToLower(strings.TrimSpace(reference))
	if reference == "" {
		return "", errors.New("the request id is empty")
	}

	var matches []string
	for _, request := range requests {
		if strings.HasPrefix(strings.ToLower(request.GetId()), reference) {
			matches = append(matches, request.GetId())
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no request matches %q", reference)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("%q matches %d requests", reference, len(matches))
	}
}

// requestFiles describes the files of a request in one line.
func requestFiles(request *localv1.ShareRequest) string {
	files := request.GetFiles()
	switch len(files) {
	case 0:
		return "no files"
	case 1:
		return files[0].GetName()
	default:
		return countText(len(files), "file", "files")
	}
}
