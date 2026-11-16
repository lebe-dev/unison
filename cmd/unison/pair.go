package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"

	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
	"github.com/lebe-dev/unison/proto/unison/local/v1/localv1connect"
)

// closeTimeout bounds the call that closes the pairing window on the way out.
const closeTimeout = 5 * time.Second

// candidatePoll is how often the command asks the daemon for the devices that
// are open for pairing. The events alone are not enough: a device the daemon
// found before the event stream was open is never reported again.
const candidatePoll = 2 * time.Second

// awaitState is what the pairing command waits for on standard input.
type awaitState int

const (
	awaitNothing awaitState = iota
	awaitDevice
	awaitCode
)

// eventItem is one item of the event stream: an event or the error that ended
// the stream.
type eventItem struct {
	event *localv1.Event
	err   error
}

// pair opens a pairing window, shows the discovered devices and the short
// authentication code of an exchange, and confirms it.
func (a *app) pair(ctx context.Context, args []string) error {
	flags := a.newFlagSet("pair")
	roleName := flags.String("role", "personal", "role to propose: personal or friends")
	if err := parseFlags(flags, args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return a.usageError("pair takes no arguments")
	}
	role, err := pairRole(*roleName)
	if err != nil {
		return a.usageError("%v", err)
	}

	client, err := a.api()
	if err != nil {
		return err
	}
	opened, err := client.OpenPairingWindow(ctx, connect.NewRequest(&localv1.OpenPairingWindowRequest{Role: role}))
	if err != nil {
		return fmt.Errorf("open the pairing window: %w", err)
	}

	// The window outlives the command, so it has to be closed even when the
	// command is interrupted.
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), closeTimeout)
		defer cancel()
		_, err := client.ClosePairingWindow(closeCtx, connect.NewRequest(&localv1.ClosePairingWindowRequest{}))
		if err != nil {
			a.info("close the pairing window: %v", err)
		}
	}()

	pairCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	window := opened.Msg.GetWindow()
	a.info("pairing window open until %s", formatTime(window.GetExpiresAtUnixMs()))
	a.info("proposing role %s", roleText(role))
	a.showPairingQr(pairCtx, client)

	events := a.watch(pairCtx, client)
	lines := readLines(pairCtx, a.stdin)

	var candidates []string
	var codeDevice string
	state := awaitNothing
	exchange := false

	candidates = a.pairingCandidates(pairCtx, client, candidates)
	if len(candidates) > 0 {
		a.showCandidates(candidates)
		state = awaitDevice
	}

	poll := time.NewTicker(candidatePoll)
	defer poll.Stop()

	for {
		select {
		case <-pairCtx.Done():
			return errors.New("interrupted")
		case <-poll.C:
			candidates = a.pairingCandidates(pairCtx, client, candidates)
			if state == awaitNothing && !exchange && len(candidates) > 0 {
				a.showCandidates(candidates)
				state = awaitDevice
			}
		case item, ok := <-events:
			if !ok {
				return errors.New("the event stream of the daemon ended")
			}
			if item.err != nil {
				return fmt.Errorf("watch the daemon: %w", item.err)
			}
			if a.json && pairingKind(item.event.GetKind()) {
				if err := a.writeEventJSON(item.event); err != nil {
					return err
				}
			}
			switch item.event.GetKind() {
			case localv1.EventKind_EVENT_KIND_PAIRING_CANDIDATE:
				addr := item.event.GetAddr()
				if !slices.Contains(candidates, addr) {
					candidates = append(candidates, addr)
				}
				if state == awaitNothing && !exchange {
					a.showCandidates(candidates)
					state = awaitDevice
				}
			case localv1.EventKind_EVENT_KIND_PAIRING_REQUEST:
				codeDevice = item.event.GetDeviceId()
				exchange = true
				a.showCode(item.event)
				state = awaitCode
			case localv1.EventKind_EVENT_KIND_PAIRING_PAIRED:
				a.info("paired with %s", deviceLabel(item.event.GetDeviceId(), item.event.GetName()))
				return nil
			case localv1.EventKind_EVENT_KIND_PAIRING_FAILED:
				return fmt.Errorf("pairing failed: %s", item.event.GetError())
			case localv1.EventKind_EVENT_KIND_PAIRING_CLOSED:
				return errors.New("the pairing window closed")
			}
		case line, ok := <-lines:
			if !ok {
				return errors.New("standard input closed, the code cannot be confirmed")
			}
			switch state {
			case awaitDevice:
				addr, ok := chooseCandidate(line, candidates)
				if !ok {
					a.info("no device %q", strings.TrimSpace(line))
					a.showCandidates(candidates)
					continue
				}
				if addr == "" {
					state = awaitNothing
					a.info("waiting for a device to start pairing")
					continue
				}
				if _, err := client.StartSas(pairCtx, connect.NewRequest(&localv1.StartSasRequest{Addr: addr})); err != nil {
					return fmt.Errorf("start pairing with %s: %w", addr, err)
				}
				exchange = true
				state = awaitNothing
				a.info("pairing with %s", addr)
			case awaitCode:
				answer := strings.ToLower(strings.TrimSpace(line))
				state = awaitNothing
				if answer != "y" && answer != "yes" {
					_, err := client.RejectSas(pairCtx, connect.NewRequest(&localv1.RejectSasRequest{DeviceId: codeDevice}))
					if err != nil {
						return fmt.Errorf("reject the pairing: %w", err)
					}
					return errors.New("pairing rejected")
				}
				if _, err := client.ConfirmSas(pairCtx, connect.NewRequest(&localv1.ConfirmSasRequest{DeviceId: codeDevice})); err != nil {
					return fmt.Errorf("confirm the code: %w", err)
				}
				a.info("waiting for the other device to confirm")
			case awaitNothing:
				if len(candidates) == 0 {
					a.info("no devices found yet")
					continue
				}
				a.showCandidates(candidates)
				state = awaitDevice
			}
		}
	}
}

// pairingKind reports whether an event belongs to the pairing flow.
func pairingKind(kind localv1.EventKind) bool {
	switch kind {
	case localv1.EventKind_EVENT_KIND_PAIRING_CANDIDATE,
		localv1.EventKind_EVENT_KIND_PAIRING_REQUEST,
		localv1.EventKind_EVENT_KIND_PAIRING_PAIRED,
		localv1.EventKind_EVENT_KIND_PAIRING_FAILED,
		localv1.EventKind_EVENT_KIND_PAIRING_CLOSED:
		return true
	default:
		return false
	}
}

// pairRole returns the role a pairing window proposes.
func pairRole(value string) (localv1.Role, error) {
	switch strings.ToLower(value) {
	case "personal":
		return localv1.Role_ROLE_PERSONAL, nil
	case "friends":
		return localv1.Role_ROLE_FRIENDS, nil
	default:
		return localv1.Role_ROLE_UNSPECIFIED, fmt.Errorf("unknown role %q, use personal or friends", value)
	}
}

// watch opens the event stream of the daemon. The stream runs in the background
// because the response of a server stream arrives with the first event.
func (a *app) watch(ctx context.Context, client localv1connect.UnisonServiceClient) <-chan eventItem {
	items := make(chan eventItem)
	go func() {
		defer close(items)

		stream, err := client.WatchEvents(ctx, connect.NewRequest(&localv1.WatchEventsRequest{}))
		if err != nil {
			select {
			case items <- eventItem{err: err}:
			case <-ctx.Done():
			}
			return
		}
		defer func() { _ = stream.Close() }()

		for stream.Receive() {
			select {
			case items <- eventItem{event: stream.Msg().GetEvent()}:
			case <-ctx.Done():
				return
			}
		}
		if err := stream.Err(); err != nil {
			select {
			case items <- eventItem{err: err}:
			case <-ctx.Done():
			}
		}
	}()
	return items
}

// readLines returns the lines of r. The channel is closed when r ends.
func readLines(ctx context.Context, r io.Reader) <-chan string {
	lines := make(chan string)
	go func() {
		defer close(lines)

		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			select {
			case lines <- scanner.Text():
			case <-ctx.Done():
				return
			}
		}
	}()
	return lines
}

// pairingCandidates returns the addresses of the devices that are open for
// pairing, merged into the ones already known.
func (a *app) pairingCandidates(ctx context.Context, client localv1connect.UnisonServiceClient, known []string) []string {
	response, err := client.ListPairingCandidates(ctx, connect.NewRequest(&localv1.ListPairingCandidatesRequest{}))
	if err != nil {
		// The event stream reports a window that closed or a daemon that went
		// away, so the poll stays quiet.
		return known
	}
	for _, candidate := range response.Msg.GetCandidates() {
		addr := candidate.GetAddr()
		if addr != "" && !slices.Contains(known, addr) {
			known = append(known, addr)
		}
	}
	return known
}

// showCandidates prints the discovered devices and asks which one to pair with.
func (a *app) showCandidates(candidates []string) {
	a.info("found %s:", countText(len(candidates), "device", "devices"))
	for i, addr := range candidates {
		a.info("  %d. %s", i+1, addr)
	}
	_, _ = fmt.Fprintf(a.stderr, "choose a device [1-%d], Enter to wait for an incoming request: ", len(candidates))
}

// chooseCandidate returns the address the line selects. An empty line returns
// an empty address, which means waiting for an incoming request. The second
// value reports whether the line was understood.
func chooseCandidate(line string, candidates []string) (string, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", true
	}
	index, err := strconv.Atoi(line)
	if err != nil || index < 1 || index > len(candidates) {
		return "", false
	}
	return candidates[index-1], true
}

// showCode prints the short authentication code of an exchange and asks for the
// confirmation. The device of a scanned QR code shows no code: the pair is
// confirmed by the code that was read.
func (a *app) showCode(event *localv1.Event) {
	a.info("")
	a.info("device  %s", deviceLabel(event.GetDeviceId(), event.GetName()))
	a.info("role    %s", roleText(event.GetRole()))
	if event.GetPairingMethod() == localv1.PairingMethod_PAIRING_METHOD_QR {
		a.info("")
		a.info("the device scanned the square of this one")
		_, _ = fmt.Fprint(a.stderr, "add the device? [y/N]: ")
		return
	}

	code := event.GetSas().GetDigits()
	if len(code) == 6 {
		code = code[:3] + " " + code[3:]
	}
	a.info("code    %s   %s", code, strings.Join(event.GetSas().GetEmoji(), " "))
	a.info("")
	a.info("check that the other device shows the same code")
	_, _ = fmt.Fprint(a.stderr, "confirm the code? [y/N]: ")
}

// deviceLabel returns the name of a device with its fingerprint.
func deviceLabel(id, name string) string {
	label := fingerprintOf(id)
	if name == "" {
		return label
	}
	if label == "" {
		return name
	}
	return fmt.Sprintf("%s (%s)", name, label)
}
