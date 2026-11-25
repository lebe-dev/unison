package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/lebe-dev/unison/core/api"
	"github.com/lebe-dev/unison/core/mediate"
	"github.com/lebe-dev/unison/core/model"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
)

// personalChain starts three Personal devices and connects them as a chain
// through the mediator beta.
func personalChain(t *testing.T, base int) (*testDevice, *testDevice, *testDevice) {
	t.Helper()

	alpha, beta, gamma := newChain(t, base)
	connectPersonal(t, alpha, beta)
	connectPersonal(t, beta, gamma)
	return alpha, beta, gamma
}

// newChain starts three devices of the virtual network.
func newChain(t *testing.T, base int) (*testDevice, *testDevice, *testDevice) {
	t.Helper()

	network, bus := newNetwork(t)
	alpha := newDevice(t, network, bus, base, api.Options{})
	beta := newDevice(t, network, bus, base+1000, api.Options{})
	gamma := newDevice(t, network, bus, base+2000, api.Options{})
	for _, device := range []*testDevice{alpha, beta, gamma} {
		device.start(t)
	}
	return alpha, beta, gamma
}

// connectPersonal pairs two devices and waits until they are online.
func connectPersonal(t *testing.T, one, other *testDevice) {
	t.Helper()

	one.pair(t, other)
	other.pair(t, one)
	waitOnline(t, one, other)
	waitOnline(t, other, one)
}

// waitIncomingExchange waits until the device holds an incoming exchange of the
// mediator.
func waitIncomingExchange(t *testing.T, d *testDevice, mediator model.DeviceID) mediate.Exchange {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		for _, exchange := range d.MediatedPairs() {
			if exchange.Direction == mediate.DirectionIncoming && exchange.Mediator.ID == mediator {
				return exchange
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s received no introduction from %s", d.name, mediator)
	return mediate.Exchange{}
}

// waitTrustedRole waits until the device stores the subject with the role.
func waitTrustedRole(t *testing.T, d *testDevice, subject *testDevice, role model.Role) {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		peer, err := d.Trust().Get(context.Background(), subject.DeviceID())
		if err == nil {
			if peer.Role != role {
				t.Fatalf("%s stores %s as %s, want %s", d.name, subject.name, peer.Role, role)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s does not trust %s within %v", d.name, subject.name, testTimeout)
}

// waitNoExchanges waits until the device holds no exchange.
func waitNoExchanges(t *testing.T, d *testDevice) {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		if len(d.MediatedPairs()) == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s still holds %d exchanges", d.name, len(d.MediatedPairs()))
}

// TestIntroducePersonal introduces two devices of a Personal group and checks
// that both users accept with the same code.
func TestIntroducePersonal(t *testing.T) {
	alpha, beta, gamma := personalChain(t, 40000)

	running := startSession(t, "introduce", alpha.name, gamma.name, "--endpoint", beta.endpoint)
	offerAlpha := waitIncomingExchange(t, alpha, beta.DeviceID())
	offerGamma := waitIncomingExchange(t, gamma, beta.DeviceID())
	if offerAlpha.SAS.String() != offerGamma.SAS.String() {
		t.Fatalf("the verification codes differ: %s and %s", offerAlpha.SAS, offerGamma.SAS)
	}
	output := running.waitOutput(t, running.stdout, "code")
	for _, want := range []string{alpha.name, gamma.name, "personal", offerAlpha.SessionID} {
		if !strings.Contains(output, want) {
			t.Errorf("the command does not report %q:\n%s", want, output)
		}
	}

	code, stdout, stderr := runCommand(t, "", "introduce", "accept", offerAlpha.SessionID[:8], "--endpoint", alpha.endpoint)
	if code != exitOK {
		t.Fatalf("introduce accept exited with %d, stdout %q stderr %q", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "accepted the introduction") || !strings.Contains(stdout, gamma.name) {
		t.Errorf("the answer of alpha is %q", stdout)
	}
	code, stdout, stderr = runCommand(t, "", "introduce", "accept", offerGamma.SessionID[:8], "--endpoint", gamma.endpoint)
	if code != exitOK {
		t.Fatalf("introduce accept exited with %d, stdout %q stderr %q", code, stdout, stderr)
	}

	if code := running.wait(t); code != exitOK {
		t.Fatalf("introduce exited with %d, stdout %q stderr %q", code, running.stdout.String(), running.stderr.String())
	}
	output = running.stdout.String()
	for _, want := range []string{"confirmed", "introduced", alpha.name, gamma.name} {
		if !strings.Contains(output, want) {
			t.Errorf("the command does not report %q:\n%s", want, output)
		}
	}

	waitTrustedRole(t, alpha, gamma, model.RolePersonal)
	waitTrustedRole(t, gamma, alpha, model.RolePersonal)
}

// TestIntroduceFriends introduces a Personal device and a Friends device as
// Friends and turns the search of the new pair on with the flag.
func TestIntroduceFriends(t *testing.T) {
	alpha, beta, gamma := newChain(t, 43000)
	connectPersonal(t, alpha, beta)
	enableFriends(t, beta, gamma)
	addFriend(t, beta, gamma)
	addFriend(t, gamma, beta)
	waitOnline(t, beta, gamma)
	waitOnline(t, gamma, beta)

	if on, _, err := alpha.FriendsDiscovery(context.Background()); err != nil || on {
		t.Fatalf("the search of alpha is on before the introduction (%v)", err)
	}

	running := startSession(t, "introduce", alpha.name, gamma.name, "--role", "friends", "--endpoint", beta.endpoint)
	offerAlpha := waitIncomingExchange(t, alpha, beta.DeviceID())
	offerGamma := waitIncomingExchange(t, gamma, beta.DeviceID())
	if offerAlpha.Role != model.RoleFriends || offerGamma.Role != model.RoleFriends {
		t.Fatalf("the offered roles are %s and %s", offerAlpha.Role, offerGamma.Role)
	}
	if offerAlpha.SAS.String() != offerGamma.SAS.String() {
		t.Fatalf("the verification codes differ: %s and %s", offerAlpha.SAS, offerGamma.SAS)
	}

	code, stdout, stderr := runCommand(t, "", "introduce", "accept", offerAlpha.SessionID, "--friends-discovery=true", "--endpoint", alpha.endpoint)
	if code != exitOK {
		t.Fatalf("introduce accept exited with %d, stdout %q stderr %q", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "search for Friends devices is on") {
		t.Errorf("the answer of alpha is %q", stdout)
	}
	if code, stdout, stderr := runCommand(t, "", "introduce", "accept", offerGamma.SessionID, "--friends-discovery=true", "--endpoint", gamma.endpoint); code != exitOK {
		t.Fatalf("introduce accept exited with %d, stdout %q stderr %q", code, stdout, stderr)
	}

	if code := running.wait(t); code != exitOK {
		t.Fatalf("introduce exited with %d, stdout %q stderr %q", code, running.stdout.String(), running.stderr.String())
	}

	waitTrustedRole(t, alpha, gamma, model.RoleFriends)
	waitTrustedRole(t, gamma, alpha, model.RoleFriends)
	waitOnline(t, alpha, gamma)
	waitOnline(t, gamma, alpha)
	if on, _, err := alpha.FriendsDiscovery(context.Background()); err != nil || !on {
		t.Fatalf("the search of alpha is off after the introduction (%v)", err)
	}
}

// TestIntroduceDecline declines an incoming introduction and checks that the
// exchange ends on every device.
func TestIntroduceDecline(t *testing.T) {
	alpha, beta, gamma := personalChain(t, 49000)

	running := startSession(t, "introduce", alpha.name, gamma.name, "--endpoint", beta.endpoint)
	offerAlpha := waitIncomingExchange(t, alpha, beta.DeviceID())
	_ = waitIncomingExchange(t, gamma, beta.DeviceID())

	code, stdout, stderr := runCommand(t, "", "introduce", "decline", offerAlpha.SessionID[:8], "--endpoint", alpha.endpoint)
	if code != exitOK {
		t.Fatalf("introduce decline exited with %d, stdout %q stderr %q", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "declined the introduction") || !strings.Contains(stdout, gamma.name) {
		t.Errorf("the answer of alpha is %q", stdout)
	}

	if code := running.wait(t); code != exitFailure {
		t.Fatalf("the refused introduce exited with %d", code)
	}
	if !strings.Contains(running.stderr.String(), "a device declined") {
		t.Errorf("the command does not report the decline:\n%s", running.stderr.String())
	}
	waitNoExchanges(t, beta)
	waitNoExchanges(t, alpha)
	waitNoExchanges(t, gamma)
}

// TestIntroduceList checks the table and the JSON answer of the active
// exchanges.
func TestIntroduceList(t *testing.T) {
	alpha, beta, gamma := personalChain(t, 46000)

	running := startSession(t, "introduce", alpha.name, gamma.name, "--endpoint", beta.endpoint)
	offer := waitIncomingExchange(t, alpha, beta.DeviceID())
	_ = waitIncomingExchange(t, gamma, beta.DeviceID())

	code, stdout, stderr := runCommand(t, "", "introduce", "list", "--endpoint", beta.endpoint)
	if code != exitOK {
		t.Fatalf("introduce list exited with %d, stderr %q", code, stderr)
	}
	for _, want := range []string{"SESSION", "DIRECTION", "STATE", "ROLE", "DEVICES", "CODE", alpha.name, gamma.name, "mediator", "waiting", "personal", shortSessionID(offer.SessionID)} {
		if !strings.Contains(stdout, want) {
			t.Errorf("the table does not report %q:\n%s", want, stdout)
		}
	}

	code, stdout, stderr = runCommand(t, "", "--json", "introduce", "list", "--endpoint", beta.endpoint)
	if code != exitOK {
		t.Fatalf("introduce list --json exited with %d, stderr %q", code, stderr)
	}
	var response localv1.ListMediatedPairsResponse
	if err := protojson.Unmarshal([]byte(stdout), &response); err != nil {
		t.Fatalf("decode the answer %q: %v", stdout, err)
	}
	if len(response.GetPairs()) != 1 {
		t.Fatalf("the answer lists %d exchanges", len(response.GetPairs()))
	}
	pair := response.GetPairs()[0]
	if pair.GetSessionId() != offer.SessionID {
		t.Errorf("the answer names the session %q, want %q", pair.GetSessionId(), offer.SessionID)
	}
	if pair.GetDirection() != localv1.MediatedPairDirection_MEDIATED_PAIR_DIRECTION_MEDIATOR {
		t.Errorf("the answer reports the direction %s", pair.GetDirection())
	}
	if pair.GetPeerA().GetDeviceId() != alpha.DeviceID().String() || pair.GetPeerB().GetDeviceId() != gamma.DeviceID().String() {
		t.Errorf("the answer names the peers %q and %q", pair.GetPeerA().GetDeviceId(), pair.GetPeerB().GetDeviceId())
	}
	if pair.GetSas().GetDigits() != offer.SAS.Digits {
		t.Errorf("the answer has the code %q, want %q", pair.GetSas().GetDigits(), offer.SAS.Digits)
	}

	code, stdout, stderr = runCommand(t, "", "introduce", "list", "--endpoint", alpha.endpoint)
	if code != exitOK {
		t.Fatalf("introduce list exited with %d, stderr %q", code, stderr)
	}
	for _, want := range []string{"incoming", gamma.name, beta.name} {
		if !strings.Contains(stdout, want) {
			t.Errorf("the table of alpha does not report %q:\n%s", want, stdout)
		}
	}

	// An interrupt cancels the exchange on every device.
	running.stop()
	if code := running.wait(t); code != exitFailure {
		t.Fatalf("the interrupted introduce exited with %d", code)
	}
	if !strings.Contains(running.stdout.String(), "the introduction was canceled") {
		t.Errorf("the command does not report the cancellation:\n%s", running.stdout.String())
	}
	waitNoExchanges(t, beta)
	waitNoExchanges(t, alpha)
	waitNoExchanges(t, gamma)
}
