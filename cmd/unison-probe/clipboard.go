package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"time"

	"github.com/lebe-dev/unison/core/ports"
	"github.com/lebe-dev/unison/platform/linux/clipboard"
)

func init() {
	register("clipboard", runClipboard)
}

// probeClipboard is the part of a clipboard backend the probe uses.
type probeClipboard interface {
	Watch(context.Context) (<-chan ports.Snapshot, error)
	Write(context.Context, ports.Content) error
	Read(context.Context) (clipboard.Report, error)
	Close() error
}

// runClipboard reports and writes the clipboard of the session. The content is
// never printed: only targets, MIME types, sizes and hashes.
func runClipboard(ctx context.Context, args []string) error {
	if runtime.GOOS != "linux" {
		status("platform", "unsupported on this platform")
		return nil
	}
	if len(args) == 0 {
		return errors.New("usage: clipboard watch|read|write <text>")
	}
	mode, rest := args[0], args[1:]
	var err error
	switch mode {
	case "watch":
		err = watchClipboard(ctx, rest)
	case "read":
		err = readClipboard(ctx, rest)
	case "write":
		err = writeClipboard(ctx, rest)
	default:
		return fmt.Errorf("unknown clipboard mode %q", mode)
	}
	return clipboardError(err)
}

// clipboardError turns the errors of both backends into a readable line.
func clipboardError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, clipboard.ErrNoDisplay):
		return errors.New("DISPLAY is not set; run inside a graphical session")
	case errors.Is(err, clipboard.ErrNoWaylandDisplay):
		return errors.New("the Wayland compositor is not reachable; check WAYLAND_DISPLAY and XDG_RUNTIME_DIR")
	case errors.Is(err, clipboard.ErrNoDataControl):
		return errors.New("the compositor does not offer the data-control protocol; use --backend x11")
	default:
		return err
	}
}

// openClipboard returns the clipboard of the backend. The display comes from
// the environment of the session.
func openClipboard(backend string, onReport func(clipboard.Report)) (probeClipboard, error) {
	switch backend {
	case "", "x11":
		return clipboard.NewX11(clipboard.X11Options{OnReport: onReport})
	case "wayland":
		return clipboard.NewWayland(clipboard.WaylandOptions{OnReport: onReport})
	case "auto":
		if wayland, err := clipboard.NewWayland(clipboard.WaylandOptions{OnReport: onReport}); err == nil {
			return wayland, nil
		}
		return clipboard.NewX11(clipboard.X11Options{OnReport: onReport})
	default:
		return nil, fmt.Errorf("unknown backend %q, want x11, wayland or auto", backend)
	}
}

// watchClipboard prints a report for every clipboard change.
func watchClipboard(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("clipboard watch", flag.ExitOnError)
	backend := flags.String("backend", "x11", "clipboard backend: x11, wayland or auto")
	duration := flags.Duration("duration", 0, "stop after this long, run until interrupted when zero")
	_ = flags.Parse(args)
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()
	if *duration > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *duration)
		defer cancel()
	}
	clip, err := openClipboard(*backend, printEvent)
	if err != nil {
		return err
	}
	defer func() { _ = clip.Close() }()
	snapshots, err := clip.Watch(ctx)
	if err != nil {
		return err
	}
	status("watching", "the clipboard, press Ctrl+C to stop")
	status("backend", *backend)
	for range snapshots {
	}
	return nil
}

// readClipboard prints the current clipboard state.
func readClipboard(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("clipboard read", flag.ExitOnError)
	backend := flags.String("backend", "x11", "clipboard backend: x11, wayland or auto")
	_ = flags.Parse(args)
	clip, err := openClipboard(*backend, nil)
	if err != nil {
		return err
	}
	defer func() { _ = clip.Close() }()
	report, err := clip.Read(ctx)
	if err != nil {
		return err
	}
	if len(report.Targets) == 0 {
		status("clipboard", "empty")
		return nil
	}
	printReport(report, "")
	return nil
}

// writeClipboard takes ownership of the clipboard with the given text and
// reports the result.
func writeClipboard(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("clipboard write", flag.ExitOnError)
	backend := flags.String("backend", "x11", "clipboard backend: x11, wayland or auto")
	hold := flags.Duration("hold", 0, "keep the selection after the write for this long")
	_ = flags.Parse(args)
	text := strings.Join(flags.Args(), " ")
	if text == "" {
		return errors.New("usage: clipboard write [--backend x11|wayland|auto] [--hold 30s] <text>")
	}
	clip, err := openClipboard(*backend, nil)
	if err != nil {
		return err
	}
	defer func() { _ = clip.Close() }()
	err = clip.Write(ctx, ports.Content{
		Representations: []ports.Representation{{
			MIME: clipboard.MIMETextPlain,
			Data: []byte(text),
		}},
	})
	if err != nil {
		return err
	}
	report, err := clip.Read(ctx)
	if err != nil {
		return err
	}
	printReport(report, "")
	// The value of a selection lives only while its owner runs, so a check of
	// another client needs the probe to keep the ownership.
	if *hold > 0 {
		timer := time.NewTimer(*hold)
		defer timer.Stop()
		select {
		case <-ctx.Done():
		case <-timer.C:
		}
	}
	return nil
}

// printEvent prints one watched clipboard state.
func printEvent(report clipboard.Report) {
	fmt.Println()
	printReport(report, report.At.Format(time.RFC3339Nano))
}

// printReport prints targets, flags and representations of a clipboard state.
func printReport(report clipboard.Report, event string) {
	if event != "" {
		status("event", event)
	}
	status("targets", fmt.Sprintf("%s (%d)", strings.Join(report.Targets, ", "), len(report.Targets)))
	switch {
	case report.Owner == 0:
		status("owner", "none")
	case report.OwnerIsUs:
		status("owner", fmt.Sprintf("0x%x (this process)", report.Owner))
	default:
		status("owner", fmt.Sprintf("0x%x (another client)", report.Owner))
	}
	status("sensitive", yesNo(report.Snapshot.Sensitive))
	status("owned by us", yesNo(report.Snapshot.OwnedByUs))
	representations := report.Snapshot.Content.Representations
	if len(representations) == 0 {
		status("content", "none")
		return
	}
	for _, representation := range representations {
		sum := sha256.Sum256(representation.Data)
		status("content", fmt.Sprintf("%s %d B sha256:%s",
			representation.MIME,
			len(representation.Data),
			hex.EncodeToString(sum[:]),
		))
	}
}
