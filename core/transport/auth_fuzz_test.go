package transport

import (
	"bytes"
	"testing"

	"github.com/lebe-dev/unison/core/model"
)

// FuzzAuthProof feeds arbitrary proof frames to the check that follows the
// TLS handshake. A frame may pass only for the peer of the connection, and a
// frame that passes must fail on any other connection.
func FuzzAuthProof(f *testing.F) {
	peer := testIdentity(f, 2)
	ekm := bytes.Repeat([]byte{0x5a}, authEKMSize)
	other := bytes.Repeat([]byte{0xa5}, authEKMSize)
	conn := &Conn{peer: peerOf(peer, model.RolePersonal)}

	signature, err := peer.SignMLDSA(authType, authMessage(ekm, model.RolePersonal, peer.DeviceID()))
	if err != nil {
		f.Fatalf("sign: %v", err)
	}
	valid := authFrame(model.RolePersonal, peer.DeviceID(), signature)
	if err := conn.checkAuthProof(valid, ekm); err != nil {
		f.Fatalf("the valid proof was refused: %v", err)
	}
	f.Add(valid)
	f.Add(authFrame(model.RoleFriends, peer.DeviceID(), signature))
	f.Add(authFrame(model.RolePersonal, testIdentity(f, 3).DeviceID(), signature))
	f.Add(valid[:len(valid)-1])
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, frame []byte) {
		if err := conn.checkAuthProof(frame, ekm); err != nil {
			return
		}
		if frame[1] != byte(model.RolePersonal) || !bytes.Equal(frame[2:2+authDeviceSize], peer.DeviceID().Bytes()) {
			t.Fatal("a proof for another role or device passed")
		}
		if err := conn.checkAuthProof(frame, other); err == nil {
			t.Fatal("a proof passed on a connection it was not made for")
		}
	})
}
