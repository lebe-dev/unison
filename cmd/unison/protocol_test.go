package main

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/protocol"
	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
	"github.com/lebe-dev/unison/proto/unison/local/v1/localv1connect"
)

// protocolClient serves fixed answers of the local API. Only the methods a test
// needs are implemented; the rest of the interface panics when called.
type protocolClient struct {
	localv1connect.UnisonServiceClient

	status  *localv1.GetStatusResponse
	relay   *localv1.Relay
	devices []*localv1.Device
}

// GetStatus returns the fixed status.
func (c *protocolClient) GetStatus(context.Context, *connect.Request[localv1.GetStatusRequest]) (*connect.Response[localv1.GetStatusResponse], error) {
	return connect.NewResponse(c.status), nil
}

// GetRelay returns the fixed relay.
func (c *protocolClient) GetRelay(context.Context, *connect.Request[localv1.GetRelayRequest]) (*connect.Response[localv1.GetRelayResponse], error) {
	return connect.NewResponse(&localv1.GetRelayResponse{Relay: c.relay}), nil
}

// ListDevices returns the fixed devices.
func (c *protocolClient) ListDevices(context.Context, *connect.Request[localv1.ListDevicesRequest]) (*connect.Response[localv1.ListDevicesResponse], error) {
	return connect.NewResponse(&localv1.ListDevicesResponse{Devices: c.devices}), nil
}

// newProtocolApp returns an app that talks to the client and keeps its output.
func newProtocolApp(client *protocolClient) (*app, *safeBuffer, *safeBuffer) {
	stdout, stderr := &safeBuffer{}, &safeBuffer{}
	return &app{stdout: stdout, stderr: stderr, apiClient: client}, stdout, stderr
}

// TestStatusProtocolNotices checks that the status warns about a newer daemon,
// device and relay.
func TestStatusProtocolNotices(t *testing.T) {
	newer := protocol.Version + 1
	devices := []*localv1.Device{
		{DeviceId: model.DeviceID{1}.String(), Name: "MacBook", ProtocolVersion: newer, ProtocolNewer: true},
		{DeviceId: model.DeviceID{2}.String(), Name: "Phone", ProtocolVersion: protocol.Version},
	}
	client := &protocolClient{
		status: &localv1.GetStatusResponse{
			Version:         testVersion,
			Running:         true,
			ProtocolVersion: newer,
			Devices:         devices,
		},
		relay: &localv1.Relay{
			Url:             "relay.example.com:443",
			Connected:       true,
			ProtocolVersion: newer,
			ProtocolNewer:   true,
		},
	}
	instance, stdout, stderr := newProtocolApp(client)
	if err := instance.status(t.Context(), nil); err != nil {
		t.Fatalf("status: %v", err)
	}

	for _, want := range []string{
		fmt.Sprintf("this client speaks protocol %d, the daemon speaks protocol %d: update this client", protocol.Version, newer),
		fmt.Sprintf("device MacBook (%s) speaks a newer protocol (%d); update this client", fingerprintOf(devices[0].GetDeviceId()), newer),
		fmt.Sprintf("the relay speaks a newer protocol (%d); update this client", newer),
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("the status does not report %q:\n%s", want, stdout.String())
		}
	}
	if strings.Contains(stdout.String(), "Phone speaks") {
		t.Errorf("the status warns about a device with the same protocol:\n%s", stdout.String())
	}
	if stderr.String() != "" {
		t.Errorf("the status wrote to stderr: %q", stderr.String())
	}
}

// TestStatusProtocolNoticesJSON checks that the warnings of --json go to stderr
// and leave stdout as valid JSON.
func TestStatusProtocolNoticesJSON(t *testing.T) {
	client := &protocolClient{status: &localv1.GetStatusResponse{
		Version:         testVersion,
		Running:         true,
		ProtocolVersion: protocol.Version + 1,
	}}
	instance, stdout, stderr := newProtocolApp(client)
	instance.json = true
	if err := instance.status(t.Context(), nil); err != nil {
		t.Fatalf("status: %v", err)
	}

	var status localv1.GetStatusResponse
	if err := protojson.Unmarshal([]byte(stdout.String()), &status); err != nil {
		t.Fatalf("stdout is not valid JSON %q: %v", stdout.String(), err)
	}
	if strings.Contains(stdout.String(), "update this client") {
		t.Errorf("the JSON output carries a notice:\n%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), fmt.Sprintf("the daemon speaks protocol %d", protocol.Version+1)) {
		t.Errorf("stderr does not carry the notice:\n%s", stderr.String())
	}
}

// TestStatusProtocolOlder checks that a daemon with the same or an older
// protocol is not reported.
func TestStatusProtocolOlder(t *testing.T) {
	for _, version := range []uint32{protocol.Version, protocol.Version - 1} {
		client := &protocolClient{status: &localv1.GetStatusResponse{
			Version:         testVersion,
			ProtocolVersion: version,
		}}
		instance, stdout, stderr := newProtocolApp(client)
		if err := instance.status(t.Context(), nil); err != nil {
			t.Fatalf("status: %v", err)
		}
		if strings.Contains(stdout.String(), "protocol") || stderr.String() != "" {
			t.Errorf("protocol %d is reported, stdout %q stderr %q", version, stdout.String(), stderr.String())
		}
	}
}

// TestDevicesProtocolNotice checks the warning of a newer device in the list.
func TestDevicesProtocolNotice(t *testing.T) {
	id := model.DeviceID{3}.String()
	client := &protocolClient{devices: []*localv1.Device{{
		DeviceId:        id,
		Name:            "MacBook",
		ProtocolVersion: protocol.Version + 1,
		ProtocolNewer:   true,
	}}}
	instance, stdout, stderr := newProtocolApp(client)
	if err := instance.devicesList(t.Context(), nil); err != nil {
		t.Fatalf("devices: %v", err)
	}

	want := fmt.Sprintf("device MacBook (%s) speaks a newer protocol (%d); update this client", fingerprintOf(id), protocol.Version+1)
	if !strings.Contains(stdout.String(), want) {
		t.Errorf("the list does not report %q:\n%s", want, stdout.String())
	}
	if stderr.String() != "" {
		t.Errorf("the list wrote to stderr: %q", stderr.String())
	}
}

// TestDevicesProtocolNoticeJSON checks that --json keeps stdout free of the
// warning.
func TestDevicesProtocolNoticeJSON(t *testing.T) {
	client := &protocolClient{devices: []*localv1.Device{{
		DeviceId:        model.DeviceID{4}.String(),
		Name:            "MacBook",
		ProtocolVersion: protocol.Version + 1,
		ProtocolNewer:   true,
	}}}
	instance, stdout, stderr := newProtocolApp(client)
	instance.json = true
	if err := instance.devicesList(t.Context(), nil); err != nil {
		t.Fatalf("devices: %v", err)
	}

	var list localv1.ListDevicesResponse
	if err := protojson.Unmarshal([]byte(stdout.String()), &list); err != nil {
		t.Fatalf("stdout is not valid JSON %q: %v", stdout.String(), err)
	}
	if strings.Contains(stdout.String(), "update this client") {
		t.Errorf("the JSON output carries a notice:\n%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "speaks a newer protocol") {
		t.Errorf("stderr does not carry the notice:\n%s", stderr.String())
	}
}

// TestRelayProtocolNotice checks the warning of a newer relay.
func TestRelayProtocolNotice(t *testing.T) {
	client := &protocolClient{relay: &localv1.Relay{
		Url:             "relay.example.com:443",
		Connected:       true,
		ProtocolVersion: protocol.Version + 1,
		ProtocolNewer:   true,
	}}
	instance, stdout, stderr := newProtocolApp(client)
	if err := instance.relayStatus(t.Context(), nil); err != nil {
		t.Fatalf("relay: %v", err)
	}

	if want := fmt.Sprintf("the relay speaks a newer protocol (%d); update this client", protocol.Version+1); !strings.Contains(stdout.String(), want) {
		t.Errorf("the command does not report %q:\n%s", want, stdout.String())
	}
	if stderr.String() != "" {
		t.Errorf("the command wrote to stderr: %q", stderr.String())
	}
}

// TestRelayProtocolNoticeJSON checks the warning of a newer relay with --json.
func TestRelayProtocolNoticeJSON(t *testing.T) {
	client := &protocolClient{relay: &localv1.Relay{
		Url:             "relay.example.com:443",
		ProtocolVersion: protocol.Version + 1,
		ProtocolNewer:   true,
	}}
	instance, stdout, stderr := newProtocolApp(client)
	instance.json = true
	if err := instance.relayStatus(t.Context(), nil); err != nil {
		t.Fatalf("relay: %v", err)
	}

	var relay localv1.GetRelayResponse
	if err := protojson.Unmarshal([]byte(stdout.String()), &relay); err != nil {
		t.Fatalf("stdout is not valid JSON %q: %v", stdout.String(), err)
	}
	if !strings.Contains(stderr.String(), fmt.Sprintf("the relay speaks a newer protocol (%d)", protocol.Version+1)) {
		t.Errorf("stderr does not carry the notice:\n%s", stderr.String())
	}
}

// TestRelayProtocolNoticeSame checks that a relay with the same protocol is
// not reported.
func TestRelayProtocolNoticeSame(t *testing.T) {
	client := &protocolClient{relay: &localv1.Relay{
		Url:             "relay.example.com:443",
		Connected:       true,
		ProtocolVersion: protocol.Version,
	}}
	instance, stdout, stderr := newProtocolApp(client)
	if err := instance.relayStatus(t.Context(), nil); err != nil {
		t.Fatalf("relay: %v", err)
	}
	if strings.Contains(stdout.String(), "protocol") || stderr.String() != "" {
		t.Errorf("the relay is reported, stdout %q stderr %q", stdout.String(), stderr.String())
	}
}
