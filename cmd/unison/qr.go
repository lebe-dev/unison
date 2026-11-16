package main

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	qrcode "github.com/skip2/go-qrcode"

	localv1 "github.com/lebe-dev/unison/proto/unison/local/v1"
	"github.com/lebe-dev/unison/proto/unison/local/v1/localv1connect"
)

// showPairingQr prints the pairing code of the window and the text behind it.
// The code carries a one time token, so it only goes to the terminal of the
// user: nothing here reaches the log of the daemon or the event stream.
func (a *app) showPairingQr(ctx context.Context, client localv1connect.UnisonServiceClient) {
	response, err := client.GetPairingQr(ctx, connect.NewRequest(&localv1.GetPairingQrRequest{}))
	if err != nil {
		a.info("the code for a phone is unavailable: %v", err)
		return
	}
	uri := response.Msg.GetQr().GetUri()
	if uri == "" {
		return
	}
	drawing, err := qrText(uri)
	if err != nil {
		a.info("the code for a phone cannot be drawn: %v", err)
		return
	}

	out := a.stdout
	if a.json {
		// The event stream stays machine readable, so the drawing and the code
		// of the window go to the progress stream.
		out = a.stderr
	}
	_, _ = fmt.Fprint(out, drawing)
	_, _ = fmt.Fprintf(out, "qr: %s\n", uri)
	_, _ = fmt.Fprintln(out, "scan the square with the device that should join this one")
}

// qrText returns the terminal drawing of a pairing code.
func qrText(uri string) (string, error) {
	code, err := qrcode.New(uri, qrcode.Medium)
	if err != nil {
		return "", err
	}
	return code.ToSmallString(false), nil
}
