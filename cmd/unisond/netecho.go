package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

// Defaults of the network echo.
const (
	defaultNetEchoListen = "0.0.0.0:47811"
	defaultNetEchoWait   = 3 * time.Second
	maxNetEchoPayload    = 1024
)

// netEchoProbe is the payload of a probe; the echo sends it back unchanged.
var netEchoProbe = []byte("unison-net-echo")

// netEchoOptions holds the settings of the network echo.
type netEchoOptions struct {
	listen string
	peer   string
	wait   time.Duration
}

// runNetEcho probes a peer or answers probes and returns the exit code. It
// shows whether UDP reaches a machine in both directions without any other
// service, which is what the network check of the kit needs.
func runNetEcho(ctx context.Context, options netEchoOptions, out io.Writer) int {
	if options.peer != "" {
		return probeNetEcho(ctx, options.peer, options.wait, out)
	}

	conn, err := net.ListenPacket(netEchoNetwork(options.listen), options.listen)
	if err != nil {
		_, _ = fmt.Fprintf(out, "net-echo: listen on %s: %v\n", options.listen, err)
		return exitError
	}
	defer func() { _ = conn.Close() }()
	_, _ = fmt.Fprintf(out, "net-echo: listening on %s\n", conn.LocalAddr())

	return serveNetEcho(ctx, conn, options.wait, out)
}

// netEchoNetwork returns the network of a listen address, so an IPv4 address
// binds an IPv4 socket and the echo reports the address the peer must use.
func netEchoNetwork(address string) string {
	host, _, err := net.SplitHostPort(address)
	if err != nil || host == "" {
		return "udp"
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return "udp"
	}
	if ip.To4() != nil {
		return "udp4"
	}
	return "udp6"
}

// serveNetEcho answers every datagram with the same payload. A positive wait
// bounds the run to the first probe and makes a missing probe a failure.
func serveNetEcho(ctx context.Context, conn net.PacketConn, wait time.Duration, out io.Writer) int {
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()

	if wait > 0 {
		if err := conn.SetReadDeadline(time.Now().Add(wait)); err != nil {
			_, _ = fmt.Fprintf(out, "net-echo: set the deadline: %v\n", err)
			return exitError
		}
	}

	buffer := make([]byte, maxNetEchoPayload)
	for {
		n, addr, err := conn.ReadFrom(buffer)
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				_, _ = fmt.Fprintf(out, "net-echo: no probe in %s\n", wait)
				return exitError
			}
			if ctx.Err() != nil {
				return exitOK
			}
			_, _ = fmt.Fprintf(out, "net-echo: read: %v\n", err)
			return exitError
		}
		if _, err := conn.WriteTo(buffer[:n], addr); err != nil {
			_, _ = fmt.Fprintf(out, "net-echo: answer %s: %v\n", addr, err)
			return exitError
		}
		_, _ = fmt.Fprintf(out, "net-echo: probe from %s\n", addr)
		if wait > 0 {
			_, _ = fmt.Fprintf(out, "net-echo: the peer reaches this machine\n")
			return exitOK
		}
	}
}

// probeNetEcho sends one probe and waits for the echo.
func probeNetEcho(ctx context.Context, address string, wait time.Duration, out io.Writer) int {
	if wait <= 0 {
		wait = defaultNetEchoWait
	}

	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "udp", address)
	if err != nil {
		_, _ = fmt.Fprintf(out, "net-echo: dial %s: %v\n", address, err)
		return exitError
	}
	defer func() { _ = conn.Close() }()

	if err := conn.SetDeadline(time.Now().Add(wait)); err != nil {
		_, _ = fmt.Fprintf(out, "net-echo: set the deadline: %v\n", err)
		return exitError
	}

	started := time.Now()
	if _, err := conn.Write(netEchoProbe); err != nil {
		_, _ = fmt.Fprintf(out, "net-echo: send to %s: %v\n", address, err)
		return exitError
	}

	reply := make([]byte, maxNetEchoPayload)
	n, err := conn.Read(reply)
	if err != nil {
		_, _ = fmt.Fprintf(out, "net-echo: no echo from %s in %s\n", address, wait)
		return exitError
	}
	if !bytes.Equal(reply[:n], netEchoProbe) {
		_, _ = fmt.Fprintf(out, "net-echo: %s answered with %q\n", address, reply[:n])
		return exitError
	}

	_, _ = fmt.Fprintf(out, "net-echo: echo from %s in %s\n", address, time.Since(started).Round(time.Millisecond))
	return exitOK
}
