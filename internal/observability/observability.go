// Package observability reports errors to Sentry.
//
// The DSN and the environment are set at build time from the SENTRY_DSN
// variable. Reporting stays off until the user grants consent at runtime.
// Events carry only the stack, the release, the environment and the tags of
// the component; extras, breadcrumbs and request data are dropped.
package observability

import (
	"sync/atomic"
	"time"

	"github.com/getsentry/sentry-go"

	"github.com/lebe-dev/unison/internal/redact"
)

// Values set at build time with -ldflags.
var (
	// buildDSN is empty when the build has no Sentry DSN.
	buildDSN = ""
	// buildEnvironment is "debug" or "release".
	buildEnvironment = "debug"
)

// component is the tag of the binary that reports the event.
var component string

// initialized reports whether a client is ready.
var initialized atomic.Bool

// enabled reports whether the user granted consent.
var enabled atomic.Bool

// Options describe the build that reports the events.
type Options struct {
	// Component names the binary in the component tag, for example daemon.
	Component string
	// Version is the version of the build.
	Version string
	// DSN overrides the DSN baked into the build. A server reads it from the
	// environment.
	DSN string
}

// Init starts the SDK. It reports whether a DSN was available, so a build
// without a DSN reports nothing.
func Init(opts Options) bool {
	dsn := opts.DSN
	if dsn == "" {
		dsn = buildDSN
	}
	if dsn == "" {
		return false
	}

	component = opts.Component
	if err := sentry.Init(clientOptions(dsn, opts.Version)); err != nil {
		return false
	}
	initialized.Store(true)
	return true
}

// clientOptions configures the SDK. The client runs for the whole life of the
// process, also without consent, so it must not do periodic work: the
// telemetry buffer polls ten times a second and keeps a phone from sleeping,
// and client reports would send the counts of the dropped events.
func clientOptions(dsn, version string) sentry.ClientOptions {
	return sentry.ClientOptions{
		Dsn:                    dsn,
		Release:                "unison@" + version,
		Environment:            buildEnvironment,
		AttachStacktrace:       true,
		EnableTracing:          false,
		DataCollection:         dataCollection(),
		MaxBreadcrumbs:         -1,
		DisableTelemetryBuffer: true,
		DisableClientReports:   true,
		BeforeSend:             beforeSend,
		BeforeBreadcrumb: func(*sentry.Breadcrumb, *sentry.BreadcrumbHint) *sentry.Breadcrumb {
			return nil
		},
	}
}

// dataCollection keeps cookies, headers, query parameters and bodies out of the
// events.
func dataCollection() *sentry.DataCollection {
	off := &sentry.KeyValueCollectionBehavior{Mode: sentry.CollectionOff}
	return &sentry.DataCollection{
		UserInfo:    sentry.Set(false),
		Cookies:     off,
		HTTPHeaders: &sentry.HeaderCollectionConfig{Request: off, Response: off},
		HTTPBodies:  []sentry.BodyType{},
		QueryParams: off,
	}
}

// beforeSend drops the events without consent and scrubs the ones that are
// sent.
func beforeSend(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
	if !enabled.Load() {
		return nil
	}
	event.User = sentry.User{}
	event.ServerName = ""
	event.Request = nil
	event.Breadcrumbs = nil
	event.Modules = nil
	delete(event.Contexts, "device")
	delete(event.Contexts, "browser")
	event.Message = redact.Paths(event.Message)
	for i := range event.Exception {
		event.Exception[i].Value = redact.Paths(event.Exception[i].Value)
	}
	if component != "" {
		if event.Tags == nil {
			event.Tags = make(map[string]string, 1)
		}
		event.Tags["component"] = component
	}
	return event
}

// Configured reports whether the build has a DSN and the SDK started.
func Configured() bool {
	return initialized.Load()
}

// SetEnabled grants or withdraws the consent to report.
func SetEnabled(on bool) {
	enabled.Store(on)
}

// Enabled reports whether the consent is granted.
func Enabled() bool {
	return enabled.Load()
}

// CaptureException reports err when the consent is granted.
func CaptureException(err error) {
	if err == nil || !enabled.Load() {
		return
	}
	sentry.CaptureException(err)
}

// CaptureMessage reports a message when the consent is granted.
func CaptureMessage(message string) {
	if message == "" || !enabled.Load() {
		return
	}
	sentry.CaptureMessage(message)
}

// Recover reports the panic of the current goroutine and raises it again, so
// the process behaves as it would without the defer.
func Recover() {
	value := recover()
	if value == nil {
		return
	}
	CapturePanic(value)
	panic(value)
}

// HandlePanic reports the panic and stops it. Use it only at boundaries that
// already kept the panic from the process.
func HandlePanic() {
	if value := recover(); value != nil {
		CapturePanic(value)
	}
}

// CapturePanic reports a value taken from recover. The caller keeps the
// control flow, so it is used where the panic already turned into another
// result.
func CapturePanic(value any) {
	if value == nil || !enabled.Load() {
		return
	}
	sentry.CurrentHub().Recover(value)
}

// Flush sends the queued events and waits up to the timeout.
func Flush(timeout time.Duration) {
	if !initialized.Load() {
		return
	}
	sentry.Flush(timeout)
}
