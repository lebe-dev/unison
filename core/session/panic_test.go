package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lebe-dev/unison/core/model"
	"github.com/lebe-dev/unison/core/trust"
	wirev1 "github.com/lebe-dev/unison/proto/unison/wire/v1"
)

// TestControlHandlerPanicClosesSession checks that a panic of a handler, which
// input of the peer caused, ends the session instead of the process.
func TestControlHandlerPanicClosesSession(t *testing.T) {
	h := newHarness(t, model.RolePersonal, model.RolePersonal)

	client, server := h.open(func(_, server *Session) {
		if err := server.Handle(KindClipAnnounce, func(context.Context, *wirev1.ControlMessage) error {
			panic("handler bug")
		}); err != nil {
			t.Fatalf("handle clip announce: %v", err)
		}
	})

	if err := client.Send(context.Background(), clipAnnounce(1)); err != nil {
		t.Fatalf("send: %v", err)
	}
	waitDone(t, server)
	if err := server.Err(); !errors.Is(err, ErrHandlerPanic) {
		t.Fatalf("session error = %v, want ErrHandlerPanic", err)
	}
	waitDone(t, client)
}

// TestRequestHandlerPanicClosesSession checks the same for a request stream.
func TestRequestHandlerPanicClosesSession(t *testing.T) {
	h := newHarness(t, model.RolePersonal, model.RolePersonal)

	client, server := h.open(func(_, server *Session) {
		if err := server.HandleRequest("test.request", trust.ActionNone, func(context.Context, *Request) error {
			panic("handler bug")
		}); err != nil {
			t.Fatalf("handle request: %v", err)
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := client.OpenRequest(ctx, &wirev1.StreamHeader{PayloadType: "test.request"})
	if err != nil {
		t.Fatalf("open request: %v", err)
	}
	defer func() { _ = req.Close() }()

	waitDone(t, server)
	if err := server.Err(); !errors.Is(err, ErrHandlerPanic) {
		t.Fatalf("session error = %v, want ErrHandlerPanic", err)
	}
}
