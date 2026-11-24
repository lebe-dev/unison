package api_test

import (
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/lebe-dev/unison/core/api"
	"github.com/lebe-dev/unison/core/ports/fake"
	"github.com/lebe-dev/unison/core/transport/vnet"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
	"github.com/lebe-dev/unison/proto/unison/local/v1/localv1connect"
)

// TestMediatedPairRPCs checks the local API of an introduction end to end: the
// mediator starts it, both devices accept it and every client sees the new
// device.
func TestMediatedPairRPCs(t *testing.T) {
	network := vnet.NewNetwork()
	t.Cleanup(func() { _ = network.Close() })
	bus := fake.NewBus()

	alpha := newNode(t, network, bus, 32000)
	beta := newNode(t, network, bus, 33000)
	gamma := newNode(t, network, bus, 34000)
	for _, n := range []*testNode{alpha, beta, gamma} {
		n.start(t)
	}
	alpha.pair(t, beta)
	beta.pair(t, alpha)
	beta.pair(t, gamma)
	gamma.pair(t, beta)

	alphaClient := newClient(t, alpha.Node, api.Options{Version: "1.0.0", DeviceName: "alpha"})
	betaClient := newClient(t, beta.Node, api.Options{Version: "1.0.0", DeviceName: "beta"})
	gammaClient := newClient(t, gamma.Node, api.Options{Version: "1.0.0", DeviceName: "gamma"})
	ctx := callTimeout(t)

	waitOnline(t, betaClient, alpha.DeviceID())
	waitOnline(t, betaClient, gamma.DeviceID())
	waitOnline(t, alphaClient, beta.DeviceID())
	waitOnline(t, gammaClient, beta.DeviceID())

	// Both endpoints report the capability of the mediator, so a client can
	// offer the action.
	devices, err := betaClient.ListDevices(ctx, connect.NewRequest(&localv1.ListDevicesRequest{}))
	if err != nil {
		t.Fatalf("list devices: %v", err)
	}
	for _, device := range devices.Msg.GetDevices() {
		if !device.GetSupportsMediatedPair() {
			t.Fatalf("device %s does not report the capability", device.GetName())
		}
	}

	started, err := betaClient.StartMediatedPair(ctx, connect.NewRequest(&localv1.StartMediatedPairRequest{
		PeerA: alpha.DeviceID().String(),
		PeerB: gamma.DeviceID().String(),
		Role:  localv1.Role_ROLE_PERSONAL,
	}))
	if err != nil {
		t.Fatalf("start the exchange: %v", err)
	}
	sessionID := started.Msg.GetPair().GetSessionId()
	if sessionID == "" {
		t.Fatal("the exchange has no session id")
	}
	if started.Msg.GetPair().GetSas() == nil {
		t.Fatal("the mediator shows no verification code")
	}

	offerAlpha := waitMediatedOffer(t, alphaClient)
	offerGamma := waitMediatedOffer(t, gammaClient)
	if offerAlpha.GetSessionId() != sessionID || offerGamma.GetSessionId() != sessionID {
		t.Fatalf("the offers carry other session ids")
	}
	if offerAlpha.GetSas().GetDigits() != offerGamma.GetSas().GetDigits() {
		t.Fatal("the endpoints show different verification codes")
	}
	if offerAlpha.GetMediator().GetName() == "" || offerAlpha.GetMediator().GetDeviceId() != beta.DeviceID().String() {
		t.Fatalf("the offer names the mediator %q", offerAlpha.GetMediator().GetName())
	}

	if _, err := alphaClient.ConfirmMediatedPair(ctx, connect.NewRequest(&localv1.ConfirmMediatedPairRequest{SessionId: sessionID})); err != nil {
		t.Fatalf("alpha confirms: %v", err)
	}
	if _, err := gammaClient.ConfirmMediatedPair(ctx, connect.NewRequest(&localv1.ConfirmMediatedPairRequest{SessionId: sessionID})); err != nil {
		t.Fatalf("gamma confirms: %v", err)
	}

	waitOnline(t, alphaClient, gamma.DeviceID())
	waitOnline(t, gammaClient, alpha.DeviceID())

	pairs, err := betaClient.ListMediatedPairs(ctx, connect.NewRequest(&localv1.ListMediatedPairsRequest{}))
	if err != nil {
		t.Fatalf("list the exchanges: %v", err)
	}
	if len(pairs.Msg.GetPairs()) != 0 {
		t.Fatalf("%d exchanges are still active", len(pairs.Msg.GetPairs()))
	}

	devices, err = alphaClient.ListDevices(ctx, connect.NewRequest(&localv1.ListDevicesRequest{}))
	if err != nil {
		t.Fatalf("list devices: %v", err)
	}
	found := false
	for _, device := range devices.Msg.GetDevices() {
		if device.GetDeviceId() == gamma.DeviceID().String() {
			found = true
			if device.GetIntroducedBy() != beta.DeviceID().String() {
				t.Fatalf("the new device came from %q", device.GetIntroducedBy())
			}
		}
	}
	if !found {
		t.Fatal("the new device is not in the list")
	}
}

// waitMediatedOffer waits for the offer of an exchange.
func waitMediatedOffer(t *testing.T, client localv1connect.UnisonServiceClient) *localv1.MediatedPair {
	t.Helper()

	ctx := callTimeout(t)
	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		pairs, err := client.ListMediatedPairs(ctx, connect.NewRequest(&localv1.ListMediatedPairsRequest{}))
		if err != nil {
			t.Fatalf("list the exchanges: %v", err)
		}
		for _, pair := range pairs.Msg.GetPairs() {
			if pair.GetDirection() == localv1.MediatedPairDirection_MEDIATED_PAIR_DIRECTION_INCOMING {
				return pair
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no offer arrived")
	return nil
}
