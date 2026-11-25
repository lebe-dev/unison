package session

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/model"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// TestHasCapability checks the capabilities the peer announced.
func TestHasCapability(t *testing.T) {
	h := newHarness(t, model.RolePersonal, model.RolePersonal)
	h.clientCfg.Capabilities = []string{"mediated-pair/1"}
	h.serverCfg.Capabilities = []string{"another-capability"}

	client, server := h.open(nil)

	if !server.HasCapability("mediated-pair/1") {
		t.Error("the server does not see the capability of the client")
	}
	if !client.HasCapability("another-capability") {
		t.Error("the client does not see the capability of the server")
	}
	if client.HasCapability("mediated-pair/1") {
		t.Error("the client reports its own capability as the capability of the peer")
	}
	if client.HasCapability("absent") {
		t.Error("an absent capability is reported")
	}
}

// TestHandlerViolationCounts checks that a handler marks a protocol violation
// without ending the session before the threshold.
func TestHandlerViolationCounts(t *testing.T) {
	h := newHarness(t, model.RolePersonal, model.RolePersonal)
	client, server := h.open(func(_, server *Session) {
		err := server.Handle(KindClipAnnounce, func(context.Context, *wirev1.ControlMessage) error {
			return fmt.Errorf("%w: role escalation", ErrViolation)
		})
		if err != nil {
			t.Fatalf("handle clip announce: %v", err)
		}
	})

	if err := client.Send(context.Background(), clipAnnounce(1)); err != nil {
		t.Fatalf("send: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for server.Violations() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := server.Violations(); got != 1 {
		t.Fatalf("violations = %d, want 1", got)
	}
	select {
	case <-server.Done():
		t.Fatal("one violation ended the session")
	default:
	}
}
