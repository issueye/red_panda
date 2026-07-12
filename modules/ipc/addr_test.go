package ipc

import (
	"runtime"
	"strings"
	"testing"
)

func TestParseAddrRoundTrip(t *testing.T) {
	addr, err := NewTempAddr("red-panda-test")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseAddr(addr.String())
	if err != nil {
		t.Fatal(err)
	}
	if parsed != addr {
		t.Fatalf("round-trip mismatch: got %+v want %+v", parsed, addr)
	}
}

func TestParseAddrBarePath(t *testing.T) {
	var path string
	if runtime.GOOS == "windows" {
		path = `\\.\pipe\red-panda-bare`
	} else {
		path = "/tmp/red-panda-bare.sock"
	}
	addr, err := ParseAddr(path)
	if err != nil {
		t.Fatal(err)
	}
	if addr.Path != path {
		t.Fatalf("path: got %q want %q", addr.Path, path)
	}
	if addr.Network != DefaultNetwork() {
		t.Fatalf("network: got %q want %q", addr.Network, DefaultNetwork())
	}
}

func TestParseAddrRejectsUnknownNetwork(t *testing.T) {
	_, err := ParseAddr("tcp:127.0.0.1:1")
	if err == nil {
		t.Fatal("expected error for unknown network")
	}
}

func TestChildEnvAndAddrFromEnv(t *testing.T) {
	addr := Addr{Network: DefaultNetwork(), Path: "test-endpoint"}
	env := ChildEnv(addr)
	if len(env) != 1 || !strings.HasPrefix(env[0], EnvAddr+"=") {
		t.Fatalf("unexpected env: %v", env)
	}
	t.Setenv(EnvAddr, addr.String())
	got, err := AddrFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != addr.String() {
		t.Fatalf("got %q want %q", got.String(), addr.String())
	}
}

func TestSanitizeName(t *testing.T) {
	got := sanitizeName(`a/b\c: d`)
	if strings.ContainsAny(got, `/\ :`) {
		t.Fatalf("sanitize left separators: %q", got)
	}
}
