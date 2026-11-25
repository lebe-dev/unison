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

// introduce runs the subcommands of the mediated pairing. A command line
// without a known subcommand introduces the two devices of the arguments.
func (a *app) introduce(ctx context.Context, args []string) error {
	command, rest := subcommand(args, "role")
	switch command {
	case "list", "ls":
		return a.introduceList(ctx, rest)
	case "accept":
		return a.introduceAccept(ctx, rest)
	case "decline", "reject":
		return a.introduceDecline(ctx, rest)
	default:
		return a.introduceStart(ctx, args)
	}
}

// introduceStart opens an exchange between two devices this one knows and waits
// for both users to accept it. This device is the mediator.
func (a *app) introduceStart(ctx context.Context, args []string) error {
	flags := a.newFlagSet("introduce")
	roleName := flags.String("role", "personal", "role of the new pair: personal or friends")
	positional, err := parseCommand(flags, args)
	if err != nil {
		return err
	}
	if len(positional) != 2 {
		if len(positional) == 0 {
			return a.usageError("introduce needs two devices or a subcommand: list, accept, decline")
		}
		return a.usageError("introduce needs two devices")
	}
	role, err := pairRole(*roleName)
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
	first, err := resolveDevice(devices, positional[0])
	if err != nil {
		return err
	}
	second, err := resolveDevice(devices, positional[1])
	if err != nil {
		return err
	}
	if first.GetDeviceId() == second.GetDeviceId() {
		return a.usageError("introduce needs two different devices")
	}

	response, err := client.StartMediatedPair(ctx, connect.NewRequest(&localv1.StartMediatedPairRequest{
		PeerA: first.GetDeviceId(),
		PeerB: second.GetDeviceId(),
		Role:  role,
	}))
	if err != nil {
		return fmt.Errorf("start the introduction: %w", err)
	}

	pair := response.Msg.GetPair()
	a.showIntroduction(pair, first, second, role)
	return a.followIntroduction(ctx, client, pair.GetSessionId(), first, second)
}

// showIntroduction prints the parties, the role and the code of a started
// exchange.
func (a *app) showIntroduction(pair *localv1.MediatedPair, first, second *localv1.Device, role localv1.Role) {
	a.info("introducing %s to %s", deviceLabel(first.GetDeviceId(), first.GetName()), deviceLabel(second.GetDeviceId(), second.GetName()))
	a.info("role    %s", roleText(role))
	a.info("session %s", pair.GetSessionId())
	if pair.GetExpiresAtUnixMs() != 0 {
		a.info("expires %s", formatTime(pair.GetExpiresAtUnixMs()))
	}
	if !sasEmpty(pair.GetSas()) {
		a.info("")
		a.info("code    %s   %s", sasText(pair.GetSas()), strings.Join(pair.GetSas().GetEmoji(), " "))
		a.info("")
		a.info("check that both devices show the same code")
	}
	a.info("waiting for both devices to confirm")
}

// followIntroduction reports the progress of an exchange until it completes or
// ends. An interrupt cancels the exchange for both endpoints.
func (a *app) followIntroduction(ctx context.Context, client localv1connect.UnisonServiceClient, sessionID string, first, second *localv1.Device) error {
	low, high := first, second
	if low.GetDeviceId() > high.GetDeviceId() {
		low, high = high, low
	}
	progress := &introduceProgress{
		low:  deviceLabel(low.GetDeviceId(), low.GetName()),
		high: deviceLabel(high.GetDeviceId(), high.GetName()),
	}

	events := a.watch(ctx, client)
	for {
		select {
		case <-ctx.Done():
			a.cancelIntroduction(ctx, client, sessionID)
			return errors.New("interrupted")
		case item, ok := <-events:
			if !ok {
				if ctx.Err() != nil {
					a.cancelIntroduction(ctx, client, sessionID)
					return errors.New("interrupted")
				}
				return errors.New("the event stream of the daemon ended")
			}
			if item.err != nil {
				if ctx.Err() != nil {
					a.cancelIntroduction(ctx, client, sessionID)
					return errors.New("interrupted")
				}
				return fmt.Errorf("watch the daemon: %w", item.err)
			}
			if !mediatedKind(item.event.GetKind()) {
				continue
			}
			pair := item.event.GetMediatedPair()
			if pair.GetSessionId() != sessionID {
				continue
			}
			if a.json {
				if err := a.writeEventJSON(item.event); err != nil {
					return err
				}
			}
			progress.report(a, pair)
			switch item.event.GetKind() {
			case localv1.EventKind_EVENT_KIND_MEDIATED_PAIR_COMPLETED:
				a.info("introduced %s to %s as %s", mediatedDeviceName(pair.GetPeerA()), mediatedDeviceName(pair.GetPeerB()), roleText(pair.GetRole()))
				return nil
			case localv1.EventKind_EVENT_KIND_MEDIATED_PAIR_CANCELED:
				return fmt.Errorf("the introduction ended: %s", mediatedCancelReasonText(pair.GetCancelReason()))
			}
		}
	}
}

// cancelIntroduction ends an exchange after an interrupt. The call does not
// inherit the canceled context, so it still reaches the daemon.
func (a *app) cancelIntroduction(ctx context.Context, client localv1connect.UnisonServiceClient, sessionID string) {
	cancelCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), closeTimeout)
	defer cancel()

	if _, err := client.CancelMediatedPair(cancelCtx, connect.NewRequest(&localv1.CancelMediatedPairRequest{SessionId: sessionID})); err != nil {
		a.info("cancel the introduction: %v", err)
	} else {
		a.info("the introduction was canceled")
	}
}

// introduceProgress reports which endpoint of the exchange accepted already.
// The mediator learns the higher endpoint from its readiness and the lower one
// from its keys.
type introduceProgress struct {
	low, high         string
	lowDone, highDone bool
	bothShown         bool
}

// report prints the acceptances a state of the exchange adds.
func (p *introduceProgress) report(a *app, pair *localv1.MediatedPair) {
	switch pair.GetState() {
	case localv1.MediatedPairState_MEDIATED_PAIR_STATE_READY:
		if !p.highDone {
			p.highDone = true
			p.announce(a)
		}
	case localv1.MediatedPairState_MEDIATED_PAIR_STATE_AWAITING_FINISH:
		if !p.lowDone {
			p.lowDone = true
			p.announce(a)
		}
	case localv1.MediatedPairState_MEDIATED_PAIR_STATE_COMPLETED:
		p.lowDone, p.highDone = true, true
		p.announce(a)
	}
}

// announce prints the confirmation of an endpoint and what the exchange waits
// for.
func (p *introduceProgress) announce(a *app) {
	if p.lowDone && p.highDone {
		if !p.bothShown {
			p.bothShown = true
			a.info("%s and %s confirmed", p.low, p.high)
		}
		return
	}
	if p.lowDone {
		a.info("%s confirmed, waiting for %s", p.low, p.high)
		return
	}
	a.info("%s confirmed, waiting for %s", p.high, p.low)
}

// introduceList prints the exchanges that wait for a decision.
func (a *app) introduceList(ctx context.Context, args []string) error {
	flags := a.newFlagSet("introduce list")
	positional, err := parseCommand(flags, args)
	if err != nil {
		return err
	}
	if len(positional) != 0 {
		return a.usageError("introduce list takes no arguments")
	}

	client, err := a.api()
	if err != nil {
		return err
	}
	response, err := client.ListMediatedPairs(ctx, connect.NewRequest(&localv1.ListMediatedPairsRequest{}))
	if err != nil {
		return fmt.Errorf("read the introductions: %w", err)
	}
	if a.json {
		return a.writeJSON(response.Msg)
	}
	if len(response.Msg.GetPairs()) == 0 {
		a.info("no introductions")
		return nil
	}

	rows := make([][]string, 0, len(response.Msg.GetPairs())+1)
	rows = append(rows, []string{"SESSION", "DIRECTION", "STATE", "ROLE", "DEVICES", "CODE", "EXPIRES"})
	for _, pair := range response.Msg.GetPairs() {
		rows = append(rows, []string{
			shortSessionID(pair.GetSessionId()),
			mediatedDirectionText(pair.GetDirection()),
			mediatedStateText(pair.GetState()),
			roleText(pair.GetRole()),
			mediatedPairDevices(pair),
			sasText(pair.GetSas()),
			formatTime(pair.GetExpiresAtUnixMs()),
		})
	}
	return a.writeRows(rows)
}

// introduceAccept accepts an incoming introduction of this device.
func (a *app) introduceAccept(ctx context.Context, args []string) error {
	flags := a.newFlagSet("introduce accept")
	enable := flags.Bool("friends-discovery", false, "turn the search for Friends devices on after the introduction")
	positional, err := parseCommand(flags, args)
	if err != nil {
		return err
	}
	if len(positional) != 1 {
		return a.usageError("introduce accept takes one session id")
	}

	client, err := a.api()
	if err != nil {
		return err
	}
	pair, err := a.mediatedPair(ctx, client, positional[0])
	if err != nil {
		return err
	}
	response, err := client.ConfirmMediatedPair(ctx, connect.NewRequest(&localv1.ConfirmMediatedPairRequest{
		SessionId:              pair.GetSessionId(),
		EnableFriendsDiscovery: *enable,
	}))
	if err != nil {
		return fmt.Errorf("accept the introduction: %w", err)
	}
	pair = updatedMediatedPair(response.Msg.GetPair(), pair)
	if a.json {
		return a.writeJSON(&localv1.ConfirmMediatedPairResponse{Pair: pair})
	}

	a.info("accepted the introduction of %s via %s", mediatedDeviceName(pair.GetPeer()), mediatedDeviceName(pair.GetMediator()))
	if pair.GetRole() == localv1.Role_ROLE_FRIENDS {
		if *enable {
			a.info("the search for Friends devices is on, so the devices can meet now")
		} else {
			a.info("turn the search for Friends devices on, on both devices, so they can meet")
		}
	}
	return nil
}

// introduceDecline declines an incoming introduction of this device.
func (a *app) introduceDecline(ctx context.Context, args []string) error {
	flags := a.newFlagSet("introduce decline")
	positional, err := parseCommand(flags, args)
	if err != nil {
		return err
	}
	if len(positional) != 1 {
		return a.usageError("introduce decline takes one session id")
	}

	client, err := a.api()
	if err != nil {
		return err
	}
	pair, err := a.mediatedPair(ctx, client, positional[0])
	if err != nil {
		return err
	}
	response, err := client.RejectMediatedPair(ctx, connect.NewRequest(&localv1.RejectMediatedPairRequest{SessionId: pair.GetSessionId()}))
	if err != nil {
		return fmt.Errorf("decline the introduction: %w", err)
	}
	pair = updatedMediatedPair(response.Msg.GetPair(), pair)
	if a.json {
		return a.writeJSON(&localv1.RejectMediatedPairResponse{Pair: pair})
	}
	a.info("declined the introduction of %s via %s", mediatedDeviceName(pair.GetPeer()), mediatedDeviceName(pair.GetMediator()))
	return nil
}

// mediatedPair returns the exchange a reference points at.
func (a *app) mediatedPair(ctx context.Context, client localv1connect.UnisonServiceClient, reference string) (*localv1.MediatedPair, error) {
	response, err := client.ListMediatedPairs(ctx, connect.NewRequest(&localv1.ListMediatedPairsRequest{}))
	if err != nil {
		return nil, fmt.Errorf("read the introductions: %w", err)
	}
	return resolveMediatedPair(response.Msg.GetPairs(), reference)
}

// updatedMediatedPair keeps the offered exchange when the answer of a decision
// carries only its identifier, which happens when the exchange completed.
func updatedMediatedPair(updated, known *localv1.MediatedPair) *localv1.MediatedPair {
	if updated.GetSessionId() == "" || updated.GetRole() == localv1.Role_ROLE_UNSPECIFIED {
		return known
	}
	return updated
}

// resolveMediatedPair returns the exchange a reference points at. A reference is
// a prefix of the session id and it has to be unique.
func resolveMediatedPair(pairs []*localv1.MediatedPair, reference string) (*localv1.MediatedPair, error) {
	reference = strings.ToLower(strings.TrimSpace(reference))
	if reference == "" {
		return nil, errors.New("the session id is empty")
	}

	var matches []*localv1.MediatedPair
	for _, pair := range pairs {
		if strings.HasPrefix(strings.ToLower(pair.GetSessionId()), reference) {
			matches = append(matches, pair)
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("no introduction matches %q", reference)
	case 1:
		return matches[0], nil
	default:
		return nil, fmt.Errorf("%q matches %d introductions", reference, len(matches))
	}
}

// mediatedKind reports whether an event belongs to the mediated pairing flow.
func mediatedKind(kind localv1.EventKind) bool {
	switch kind {
	case localv1.EventKind_EVENT_KIND_MEDIATED_PAIR_OFFER,
		localv1.EventKind_EVENT_KIND_MEDIATED_PAIR_UPDATED,
		localv1.EventKind_EVENT_KIND_MEDIATED_PAIR_COMPLETED,
		localv1.EventKind_EVENT_KIND_MEDIATED_PAIR_CANCELED:
		return true
	default:
		return false
	}
}

// mediatedPairDevices describes the devices of an exchange in one cell.
func mediatedPairDevices(pair *localv1.MediatedPair) string {
	if pair.GetDirection() == localv1.MediatedPairDirection_MEDIATED_PAIR_DIRECTION_MEDIATOR {
		return mediatedDeviceName(pair.GetPeerA()) + " <-> " + mediatedDeviceName(pair.GetPeerB())
	}
	return mediatedDeviceName(pair.GetPeer()) + " via " + mediatedDeviceName(pair.GetMediator())
}

// mediatedDeviceName returns the name of an endpoint, or its fingerprint when
// the name is not known.
func mediatedDeviceName(device *localv1.MediatedDevice) string {
	if device.GetName() != "" {
		return device.GetName()
	}
	return fingerprintOf(device.GetDeviceId())
}

// mediatedDirectionText returns the side of an exchange.
func mediatedDirectionText(direction localv1.MediatedPairDirection) string {
	switch direction {
	case localv1.MediatedPairDirection_MEDIATED_PAIR_DIRECTION_MEDIATOR:
		return "mediator"
	case localv1.MediatedPairDirection_MEDIATED_PAIR_DIRECTION_INCOMING:
		return "incoming"
	default:
		return "unknown"
	}
}

// mediatedStateText returns the progress of an exchange.
func mediatedStateText(state localv1.MediatedPairState) string {
	switch state {
	case localv1.MediatedPairState_MEDIATED_PAIR_STATE_WAITING:
		return "waiting"
	case localv1.MediatedPairState_MEDIATED_PAIR_STATE_READY:
		return "ready"
	case localv1.MediatedPairState_MEDIATED_PAIR_STATE_AWAITING_FINISH:
		return "awaiting finish"
	case localv1.MediatedPairState_MEDIATED_PAIR_STATE_COMPLETED:
		return "completed"
	case localv1.MediatedPairState_MEDIATED_PAIR_STATE_CANCELED:
		return "canceled"
	default:
		return "unknown"
	}
}

// mediatedCancelReasonText describes why an exchange ended without a new pair.
func mediatedCancelReasonText(reason localv1.MediatedPairCancelReason) string {
	switch reason {
	case localv1.MediatedPairCancelReason_MEDIATED_PAIR_CANCEL_REASON_USER_REJECTED:
		return "a device declined"
	case localv1.MediatedPairCancelReason_MEDIATED_PAIR_CANCEL_REASON_MEDIATOR_CANCELED:
		return "the mediator canceled"
	case localv1.MediatedPairCancelReason_MEDIATED_PAIR_CANCEL_REASON_EXPIRED:
		return "the exchange expired"
	case localv1.MediatedPairCancelReason_MEDIATED_PAIR_CANCEL_REASON_REVOKED:
		return "a device was revoked"
	case localv1.MediatedPairCancelReason_MEDIATED_PAIR_CANCEL_REASON_ROLE_MISMATCH:
		return "a device is paired with another role"
	case localv1.MediatedPairCancelReason_MEDIATED_PAIR_CANCEL_REASON_ALREADY_PAIRED:
		return "the devices are paired already"
	case localv1.MediatedPairCancelReason_MEDIATED_PAIR_CANCEL_REASON_BUSY:
		return "a device is busy with another exchange"
	case localv1.MediatedPairCancelReason_MEDIATED_PAIR_CANCEL_REASON_PEER_OFFLINE:
		return "a device went offline"
	case localv1.MediatedPairCancelReason_MEDIATED_PAIR_CANCEL_REASON_FAILED:
		return "the exchange failed"
	default:
		return "the exchange ended"
	}
}

// sasText returns the digits of a verification code in two groups.
func sasText(sas *localv1.Sas) string {
	digits := sas.GetDigits()
	if len(digits) == 6 {
		return digits[:3] + " " + digits[3:]
	}
	return digits
}

// sasEmpty reports a verification code that was not derived.
func sasEmpty(sas *localv1.Sas) bool {
	return sas.GetDigits() == "" && len(sas.GetEmoji()) == 0
}

// shortSessionID returns the readable prefix of a session id.
func shortSessionID(id string) string {
	const width = 12
	if len(id) <= width {
		return id
	}
	return id[:width]
}
