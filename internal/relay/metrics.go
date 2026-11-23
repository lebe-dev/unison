package relay

import (
	"fmt"
	"io"
	"sync/atomic"
	"time"

	relayv1 "github.com/lebe-dev/unison/proto/unison/relay/v1"
)

// Transport names. They are the label values of the metrics.
const (
	transportQUIC = iota
	transportWS
	numTransports
)

// transportLabels holds the label value of every transport.
var transportLabels = [numTransports]string{"quic", "ws"}

// Reasons a datagram is dropped. They are the label values of the drop metric.
const (
	dropBackpressure = iota
	dropTooLarge
	dropNoPeer
	dropNotMember
	dropRateLimit
	dropMalformed
	dropWrite
	numDrops
)

// dropLabels holds the label value of every drop reason.
var dropLabels = [numDrops]string{
	"backpressure",
	"too_large",
	"no_peer",
	"not_member",
	"rate_limit",
	"malformed",
	"write",
}

// numStatuses is the number of registration statuses with a counter.
const numStatuses = 8

// statusLabels holds the label value of every registration status.
var statusLabels = [numStatuses]string{
	"unspecified",
	"ok",
	"unauthorized",
	"quota_exceeded",
	"invalid_argument",
	"unavailable",
	"datagrams_required",
	"shutting_down",
}

// statusLabel returns the label value of a status.
func statusLabel(status relayv1.RelayStatus) string {
	if index := int(status); index >= 0 && index < numStatuses {
		return statusLabels[index]
	}
	return "unknown"
}

// metrics collects the server counters for the Prometheus endpoint. Labels
// never carry mailbox ids, tokens or addresses.
type metrics struct {
	start   time.Time
	version string

	connections   [numTransports]atomic.Int64
	opened        [numTransports]atomic.Int64
	registrations [numStatuses]atomic.Int64
	updates       [numStatuses]atomic.Int64
	datagramsIn   [numTransports]atomic.Int64
	datagramsOut  [numTransports]atomic.Int64
	bytesIn       [numTransports]atomic.Int64
	bytesOut      [numTransports]atomic.Int64
	drops         [numDrops]atomic.Int64
	mailboxes     atomic.Int64
}

// newMetrics returns empty counters.
func newMetrics(version string) *metrics {
	return &metrics{start: time.Now(), version: version}
}

// connect counts a new connection.
func (m *metrics) connect(transport int) {
	m.connections[transport].Add(1)
	m.opened[transport].Add(1)
}

// disconnect counts an ended connection.
func (m *metrics) disconnect(transport int) {
	m.connections[transport].Add(-1)
}

// registration counts the outcome of a registration.
func (m *metrics) registration(status relayv1.RelayStatus) {
	if index := int(status); index >= 0 && index < numStatuses {
		m.registrations[index].Add(1)
	}
}

// update counts the outcome of a mailbox update.
func (m *metrics) update(status relayv1.RelayStatus) {
	if index := int(status); index >= 0 && index < numStatuses {
		m.updates[index].Add(1)
	}
}

// received counts an incoming datagram.
func (m *metrics) received(transport, size int) {
	m.datagramsIn[transport].Add(1)
	m.bytesIn[transport].Add(int64(size))
}

// sent counts an outgoing datagram.
func (m *metrics) sent(transport, size int) {
	m.datagramsOut[transport].Add(1)
	m.bytesOut[transport].Add(int64(size))
}

// dropped counts a datagram that the relay did not forward.
func (m *metrics) dropped(reason int) {
	m.drops[reason].Add(1)
}

// write renders the counters in the Prometheus text format.
func (m *metrics) write(w io.Writer) {
	line := func(format string, args ...any) {
		_, _ = fmt.Fprintf(w, format, args...)
	}
	label := func(values ...string) string {
		out := "{"
		for i, value := range values {
			if i > 0 {
				out += ","
			}
			out += value
		}
		return out + "}"
	}

	line("# HELP unison_relay_info Relay build information.\n")
	line("# TYPE unison_relay_info gauge\n")
	line("unison_relay_info{version=%q} 1\n", m.version)

	line("# HELP unison_relay_uptime_seconds Seconds since the start.\n")
	line("# TYPE unison_relay_uptime_seconds gauge\n")
	line("unison_relay_uptime_seconds %g\n", time.Since(m.start).Seconds())

	line("# HELP unison_relay_connections Open client connections.\n")
	line("# TYPE unison_relay_connections gauge\n")
	for i, name := range transportLabels {
		line("unison_relay_connections%s %d\n", label(fmt.Sprintf("transport=%q", name)), m.connections[i].Load())
	}

	line("# HELP unison_relay_connections_total Accepted client connections.\n")
	line("# TYPE unison_relay_connections_total counter\n")
	for i, name := range transportLabels {
		line("unison_relay_connections_total%s %d\n", label(fmt.Sprintf("transport=%q", name)), m.opened[i].Load())
	}

	line("# HELP unison_relay_registrations_total Registrations by outcome.\n")
	line("# TYPE unison_relay_registrations_total counter\n")
	for i, name := range statusLabels {
		line("unison_relay_registrations_total%s %d\n", label(fmt.Sprintf("status=%q", name)), m.registrations[i].Load())
	}

	line("# HELP unison_relay_mailbox_updates_total Mailbox updates by outcome.\n")
	line("# TYPE unison_relay_mailbox_updates_total counter\n")
	for i, name := range statusLabels {
		line("unison_relay_mailbox_updates_total%s %d\n", label(fmt.Sprintf("status=%q", name)), m.updates[i].Load())
	}

	line("# HELP unison_relay_mailboxes Mailboxes with at least one member.\n")
	line("# TYPE unison_relay_mailboxes gauge\n")
	line("unison_relay_mailboxes %d\n", m.mailboxes.Load())

	line("# HELP unison_relay_datagrams_total Datagrams by direction.\n")
	line("# TYPE unison_relay_datagrams_total counter\n")
	for i, name := range transportLabels {
		line("unison_relay_datagrams_total%s %d\n", label(fmt.Sprintf("transport=%q", name), `direction="in"`), m.datagramsIn[i].Load())
		line("unison_relay_datagrams_total%s %d\n", label(fmt.Sprintf("transport=%q", name), `direction="out"`), m.datagramsOut[i].Load())
	}

	line("# HELP unison_relay_datagram_bytes_total Datagram bytes by direction.\n")
	line("# TYPE unison_relay_datagram_bytes_total counter\n")
	for i, name := range transportLabels {
		line("unison_relay_datagram_bytes_total%s %d\n", label(fmt.Sprintf("transport=%q", name), `direction="in"`), m.bytesIn[i].Load())
		line("unison_relay_datagram_bytes_total%s %d\n", label(fmt.Sprintf("transport=%q", name), `direction="out"`), m.bytesOut[i].Load())
	}

	line("# HELP unison_relay_dropped_datagrams_total Datagrams the relay did not forward.\n")
	line("# TYPE unison_relay_dropped_datagrams_total counter\n")
	for i, name := range dropLabels {
		line("unison_relay_dropped_datagrams_total%s %d\n", label(fmt.Sprintf("reason=%q", name)), m.drops[i].Load())
	}
}
