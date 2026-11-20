//go:build windows

package localconn

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"strings"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

// pipePrefix is the namespace of named pipes.
const pipePrefix = `\\.\pipe\`

// pipeBuffers is the size of the input and output buffer of a pipe instance.
const pipeBuffers = 64 << 10

// platformPath returns the named pipe of the current user.
func platformPath() (string, error) {
	sid, err := currentUserSID()
	if err != nil {
		return "", err
	}
	return pipePrefix + "unison-" + sid, nil
}

// EndpointIn returns the endpoint of a server that keeps data in dir. Named
// pipes share one namespace, so the name is random instead of the directory.
func EndpointIn(_ string, _ string) (string, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("localconn: generate the endpoint name: %w", err)
	}
	return pipePrefix + "unison-" + hex.EncodeToString(buf[:]), nil
}

// Listen binds the named pipe of the endpoint. The pipe descriptor grants
// access to the current user only.
func Listen(path string) (net.Listener, error) {
	if !strings.HasPrefix(strings.ToLower(path), pipePrefix) {
		return nil, fmt.Errorf("localconn: endpoint %q is not a named pipe", path)
	}
	sid, err := currentUserSID()
	if err != nil {
		return nil, err
	}

	listener, err := winio.ListenPipe(path, &winio.PipeConfig{
		SecurityDescriptor: "D:P(A;;GA;;;" + sid + ")",
		InputBufferSize:    pipeBuffers,
		OutputBufferSize:   pipeBuffers,
	})
	if err != nil {
		return nil, fmt.Errorf("localconn: bind the endpoint: %w", err)
	}
	return listener, nil
}

// Dial connects to the named pipe of the endpoint.
func Dial(ctx context.Context, path string) (net.Conn, error) {
	if !strings.HasPrefix(strings.ToLower(path), pipePrefix) {
		return nil, fmt.Errorf("localconn: endpoint %q is not a named pipe", path)
	}
	return winio.DialPipeContext(ctx, path)
}

// currentUserSID returns the SID of the user running the process.
func currentUserSID() (string, error) {
	token := windows.GetCurrentProcessToken()
	user, err := token.GetTokenUser()
	if err != nil {
		return "", fmt.Errorf("localconn: read the user of the process: %w", err)
	}
	return user.User.Sid.String(), nil
}
