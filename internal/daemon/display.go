package daemon

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// displayAddress returns the address of the X server a DISPLAY value names. A
// value without a host is the unix socket of a local server, every other value
// is a TCP server.
func displayAddress(display, socketDir string) (network, address string, err error) {
	value := strings.TrimSpace(display)
	if value == "" {
		return "", "", errors.New("daemon: the display is empty")
	}
	host, rest, ok := splitDisplay(value)
	if !ok {
		return "", "", fmt.Errorf("daemon: the display %q has no number", value)
	}
	number, err := displayNumber(rest)
	if err != nil {
		return "", "", fmt.Errorf("daemon: the display %q: %w", value, err)
	}
	if host == "" || host == "unix" {
		return "unix", fmt.Sprintf("%s/X%d", socketDir, number), nil
	}
	return "tcp", net.JoinHostPort(host, strconv.Itoa(6000+number)), nil
}

// splitDisplay cuts a DISPLAY value into the host and the display number with
// the screen.
func splitDisplay(display string) (host, rest string, ok bool) {
	// An IPv6 host is written in brackets, so the colon of the number is the
	// last one.
	if strings.HasPrefix(display, "[") {
		end := strings.IndexByte(display, ']')
		if end < 0 || end+1 >= len(display) || display[end+1] != ':' {
			return "", "", false
		}
		return display[1:end], display[end+2:], true
	}
	index := strings.LastIndexByte(display, ':')
	if index < 0 {
		return "", "", false
	}
	return display[:index], display[index+1:], true
}

// displayNumber parses the display and screen part of a DISPLAY value.
func displayNumber(rest string) (int, error) {
	number, _, _ := strings.Cut(rest, ".")
	if number == "" {
		return 0, errors.New("the display number is empty")
	}
	value, err := strconv.Atoi(number)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("the display number %q is not a number", number)
	}
	return value, nil
}
