package observability

import (
	"testing"

	"github.com/getsentry/sentry-go"
)

func TestBeforeSendDropsEventsWithoutConsent(t *testing.T) {
	enabled.Store(false)
	t.Cleanup(func() { enabled.Store(false) })

	if event := beforeSend(sentry.NewEvent(), nil); event != nil {
		t.Fatal("an event was kept without consent")
	}
}

func TestBeforeSendScrubsTheEvent(t *testing.T) {
	enabled.Store(true)
	component = "daemon"
	t.Cleanup(func() {
		enabled.Store(false)
		component = ""
	})

	event := sentry.NewEvent()
	event.User = sentry.User{ID: "secret"}
	event.ServerName = "workstation"
	event.Request = &sentry.Request{URL: "https://example.com"}
	event.Breadcrumbs = []*sentry.Breadcrumb{{Message: "secret"}}
	event.Modules = map[string]string{"module": "1.0"}
	event.Contexts = map[string]sentry.Context{
		"device":  {},
		"browser": {},
		"runtime": {},
	}

	got := beforeSend(event, nil)
	if got == nil {
		t.Fatal("the event was dropped with consent")
	}
	if got.User.ID != "" {
		t.Error("the user was kept")
	}
	if got.ServerName != "" {
		t.Error("the server name was kept")
	}
	if got.Request != nil {
		t.Error("the request was kept")
	}
	if len(got.Breadcrumbs) != 0 {
		t.Error("the breadcrumbs were kept")
	}
	if len(got.Modules) != 0 {
		t.Error("the modules were kept")
	}
	if _, ok := got.Contexts["device"]; ok {
		t.Error("the device context was kept")
	}
	if _, ok := got.Contexts["browser"]; ok {
		t.Error("the browser context was kept")
	}
	if _, ok := got.Contexts["runtime"]; !ok {
		t.Error("the runtime context was dropped")
	}
	if got.Tags["component"] != "daemon" {
		t.Errorf("the component tag is %q", got.Tags["component"])
	}
}
