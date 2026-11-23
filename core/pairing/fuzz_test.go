package pairing

import (
	"bytes"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/trust"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// FuzzPairingFrame checks that an arbitrary frame of a pairing connection is
// either refused or describes a consistent device.
func FuzzPairingFrame(f *testing.F) {
	request := &wirev1.PairRequest{
		Token:      make([]byte, 16),
		Ed25519Pub: make([]byte, 32),
		MldsaPub:   make([]byte, 4),
		DeviceName: "phone",
		Platform:   wirev1.Platform_PLATFORM_ANDROID,
		Role:       wirev1.Role_ROLE_PERSONAL,
	}
	if data, err := frameOf(request); err == nil {
		f.Add(data)
	}
	response := &wirev1.PairResponse{
		Ed25519Pub: make([]byte, 32),
		MldsaPub:   make([]byte, 4),
		DeviceName: "laptop",
		Platform:   wirev1.Platform_PLATFORM_MACOS,
		Role:       wirev1.Role_ROLE_PERSONAL,
	}
	if data, err := frameOf(response); err == nil {
		f.Add(data)
	}
	if data, err := frameOf(&wirev1.PairDecision{Accepted: true}); err == nil {
		f.Add(data)
	}
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		window := &Window{}
		checkRequests(t, window, data)
		checkResponses(t, window, data)
		checkAccepts(t, window, data)
	})
}

// checkRequests checks the request frames of data.
func checkRequests(t *testing.T, window *Window, data []byte) {
	t.Helper()
	reader := newFrameReader(bytes.NewReader(data))
	for range 3 {
		request := &wirev1.PairRequest{}
		if err := reader.readMessage(request); err != nil {
			return
		}
		peer, err := window.peerInfo(request.GetEd25519Pub(), request.GetMldsaPub(), request.GetDeviceName(), request.GetPlatform(), request.GetRole())
		if err != nil {
			continue
		}
		checkPeer(t, peer, model.RolePersonal, model.RoleFriends)
	}
}

// checkResponses checks the response frames of data.
func checkResponses(t *testing.T, window *Window, data []byte) {
	t.Helper()
	reader := newFrameReader(bytes.NewReader(data))
	for range 3 {
		response := &wirev1.PairResponse{}
		if err := reader.readMessage(response); err != nil {
			return
		}
		peer, err := window.peerInfo(response.GetEd25519Pub(), response.GetMldsaPub(), response.GetDeviceName(), response.GetPlatform(), response.GetRole())
		if err != nil {
			continue
		}
		checkPeer(t, peer, model.RolePersonal, model.RoleFriends)
	}
}

// checkAccepts checks the accept frames of data. The identity of the frame is
// used as the identity of the peer, so the check reaches the group data.
func checkAccepts(t *testing.T, window *Window, data []byte) {
	t.Helper()
	reader := newFrameReader(bytes.NewReader(data))
	for range 3 {
		accept := &wirev1.PairAccept{}
		if err := reader.readMessage(accept); err != nil {
			return
		}
		peer, err := window.peerInfo(accept.GetEd25519Pub(), accept.GetMldsaPub(), accept.GetDeviceName(), wirev1.Platform_PLATFORM_MACOS, accept.GetRole())
		if err != nil {
			continue
		}
		if len(accept.GetIntroductions()) > maxIntroductions {
			t.Fatalf("accepted %d introductions", len(accept.GetIntroductions()))
		}
		for _, record := range accept.GetIntroductions() {
			_, _ = trust.IntroductionFromProto(record)
		}
		if err := checkAccept(peer, accept); err != nil {
			continue
		}
		if peer.role == model.RolePersonal && len(accept.GetDiscoveryKey()) != keySize {
			t.Fatalf("accepted a discovery key of %d bytes", len(accept.GetDiscoveryKey()))
		}
		if peer.role != model.RolePersonal && len(accept.GetDiscoveryKey()) != 0 {
			t.Fatalf("accepted %d bytes of a group key for the %s role", len(accept.GetDiscoveryKey()), peer.role)
		}
	}
}

// checkPeer checks the fields the parser accepts.
func checkPeer(t *testing.T, peer peerInfo, roles ...model.Role) {
	t.Helper()
	if peer.name == "" || len(peer.name) > maxNameLength {
		t.Fatalf("accepted a name of %d bytes", len(peer.name))
	}
	if peer.id.IsZero() {
		t.Fatalf("accepted an empty device id")
	}
	if peer.id != peer.public.DeviceID {
		t.Fatalf("the device id does not match the public keys")
	}
	if peer.platform == model.PlatformUnknown {
		t.Fatalf("accepted an unknown platform")
	}
	for _, role := range roles {
		if peer.role == role {
			return
		}
	}
	t.Fatalf("accepted the role %s", peer.role)
}

// frameOf writes msg into a pairing frame.
func frameOf(msg proto.Message) ([]byte, error) {
	var buf bytes.Buffer
	if err := writeMessage(&buf, msg); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
