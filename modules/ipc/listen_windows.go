//go:build windows

package ipc

import (
	"fmt"
	"net"

	"github.com/Microsoft/go-winio"
)

// Listen opens a platform IPC listener on path.
// On Windows path must be a named-pipe path such as \\.\pipe\red-panda-xxx.
func Listen(path string) (net.Listener, error) {
	if path == "" {
		return nil, fmt.Errorf("ipc: empty listen path")
	}
	ln, err := winio.ListenPipe(path, &winio.PipeConfig{
		// Byte-stream mode matches unix sockets and stdio framing.
		MessageMode:      false,
		InputBufferSize:  64 * 1024,
		OutputBufferSize: 64 * 1024,
	})
	if err != nil {
		return nil, fmt.Errorf("ipc: listen pipe %s: %w", path, err)
	}
	return ln, nil
}

// ListenAddr opens a listener for the given address.
func ListenAddr(addr Addr) (net.Listener, error) {
	if addr.IsZero() {
		return nil, fmt.Errorf("ipc: empty address")
	}
	if addr.Network != "" && addr.Network != NetworkWindowsPipe {
		return nil, fmt.Errorf("ipc: unsupported network %q on windows", addr.Network)
	}
	return Listen(addr.Path)
}
