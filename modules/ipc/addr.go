package ipc

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// EnvAddr is the environment variable used to pass the IPC endpoint from
// parent process to child process.
const EnvAddr = "RED_PANDA_IPC_ADDR"

// Network identifies the platform transport.
const (
	NetworkWindowsPipe = "winio"
	NetworkUnix        = "unix"
)

// Addr is a platform-specific IPC endpoint address.
type Addr struct {
	// Network is "winio" on Windows and "unix" elsewhere.
	Network string `json:"network"`
	// Path is the named-pipe path (\\.\pipe\...) or unix socket path.
	Path string `json:"path"`
}

// String returns a stable serialization "network:path" used for env vars
// and logging. Empty Addr yields "".
func (a Addr) String() string {
	if a.Network == "" && a.Path == "" {
		return ""
	}
	return a.Network + ":" + a.Path
}

// IsZero reports whether the address is unset.
func (a Addr) IsZero() bool {
	return a.Network == "" && a.Path == ""
}

// ParseAddr parses "network:path" produced by Addr.String.
// A bare path is also accepted and filled with the platform default network.
func ParseAddr(s string) (Addr, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Addr{}, fmt.Errorf("ipc: empty address")
	}
	network, path, ok := strings.Cut(s, ":")
	if !ok || network == "" || path == "" {
		// Bare path — assume platform default network.
		return Addr{Network: defaultNetwork(), Path: s}, nil
	}
	// Windows pipe paths start with \\ — after "winio:" the path may still
	// begin with "\\" so Cut is correct: "winio:\\.\pipe\x" -> path "\\.\pipe\x".
	// Unix absolute paths after "unix:" look like "/tmp/foo.sock".
	// Special case: drive-letter style "C:\..." must not be treated as network.
	if len(network) == 1 && (network[0] >= 'A' && network[0] <= 'Z' || network[0] >= 'a' && network[0] <= 'z') {
		return Addr{Network: defaultNetwork(), Path: s}, nil
	}
	switch network {
	case NetworkWindowsPipe, NetworkUnix:
		return Addr{Network: network, Path: path}, nil
	default:
		return Addr{}, fmt.Errorf("ipc: unknown network %q", network)
	}
}

// DefaultNetwork returns the transport network for the current GOOS.
func DefaultNetwork() string {
	return defaultNetwork()
}

func defaultNetwork() string {
	if runtime.GOOS == "windows" {
		return NetworkWindowsPipe
	}
	return NetworkUnix
}

// PipeName builds a Windows named-pipe path from a short name.
// name should be a simple identifier without path separators.
func PipeName(name string) string {
	name = sanitizeName(name)
	return `\\.\pipe\` + name
}

// SocketPath builds a unix-domain socket path under dir.
// If dir is empty, os.TempDir() is used.
func SocketPath(dir, name string) string {
	name = sanitizeName(name)
	if dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, name+".sock")
}

// NewTempPath returns a unique endpoint path for the current platform.
// prefix is incorporated into the name (e.g. "red-panda-agent").
func NewTempPath(prefix string) (string, error) {
	prefix = sanitizeName(prefix)
	if prefix == "" {
		prefix = "red-panda-ipc"
	}
	// Use a random suffix from crypto/rand via os.CreateTemp pattern.
	f, err := os.CreateTemp("", prefix+"-*")
	if err != nil {
		return "", fmt.Errorf("ipc: create temp name: %w", err)
	}
	base := filepath.Base(f.Name())
	_ = f.Close()
	_ = os.Remove(f.Name())

	if runtime.GOOS == "windows" {
		return PipeName(base), nil
	}
	return SocketPath(os.TempDir(), base), nil
}

// NewTempAddr returns a unique Addr for the current platform.
func NewTempAddr(prefix string) (Addr, error) {
	path, err := NewTempPath(prefix)
	if err != nil {
		return Addr{}, err
	}
	return Addr{Network: defaultNetwork(), Path: path}, nil
}

// ChildEnv returns environment assignments that tell a child process which
// IPC endpoint to dial. Callers typically append these to os.Environ().
func ChildEnv(addr Addr) []string {
	return []string{EnvAddr + "=" + addr.String()}
}

// AddrFromEnv reads EnvAddr from the process environment.
func AddrFromEnv() (Addr, error) {
	raw := strings.TrimSpace(os.Getenv(EnvAddr))
	if raw == "" {
		return Addr{}, fmt.Errorf("ipc: %s is not set", EnvAddr)
	}
	return ParseAddr(raw)
}

func sanitizeName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, `\`, "-")
	name = strings.ReplaceAll(name, `/`, "-")
	name = strings.ReplaceAll(name, ":", "-")
	name = strings.ReplaceAll(name, " ", "-")
	return name
}
