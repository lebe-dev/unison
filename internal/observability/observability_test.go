package observability

import (
	"testing"

	"github.com/getsentry/sentry-go"
)

// TestBeforeSendRedactsPaths checks that the message and every exception value
// lose their absolute paths.
func TestBeforeSendRedactsPaths(t *testing.T) {
	enabled.Store(true)
	t.Cleanup(func() { enabled.Store(false) })

	event := sentry.NewEvent()
	event.Message = "open /Users/alice/secret.txt: permission denied"
	event.Exception = []sentry.Exception{
		{Type: "openError", Value: "open /tmp/unison.db: permission denied"},
		{Value: "open /opt/unison/data: no such file"},
	}

	got := beforeSend(event, nil)
	if got == nil {
		t.Fatal("the event was dropped with consent")
	}
	if want := "open [path]: permission denied"; got.Message != want {
		t.Errorf("message is %q, want %q", got.Message, want)
	}
	if got.Exception[0].Type != "openError" {
		t.Errorf("exception type is %q, want openError", got.Exception[0].Type)
	}
	if want := "open [path]: permission denied"; got.Exception[0].Value != want {
		t.Errorf("first exception is %q, want %q", got.Exception[0].Value, want)
	}
	if want := "open [path]: no such file"; got.Exception[1].Value != want {
		t.Errorf("second exception is %q, want %q", got.Exception[1].Value, want)
	}
}

// TestBeforeSendDropsEventsWithoutConsent checks that nothing is sent without
// consent.
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

// TestClientOptionsStayIdle checks that the SDK starts no periodic work: the
// telemetry buffer wakes the process ten times a second, and client reports
// would send the counts of dropped events without consent.
func TestClientOptionsStayIdle(t *testing.T) {
	opts := clientOptions("https://key@example.com/1", "1.2.3")

	if !opts.DisableTelemetryBuffer {
		t.Error("the telemetry buffer is enabled")
	}
	if !opts.DisableClientReports {
		t.Error("client reports are enabled")
	}
	if opts.Release != "unison@1.2.3" {
		t.Errorf("release is %q, want unison@1.2.3", opts.Release)
	}
}
