package ipc

import (
	"bytes"
	"io"
	"net"
	"strings"
	"testing"
)

func TestStreamLineRoundTrip(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	server := NewStream(c1, 0)
	client := NewStream(c2, 0)

	msg := `{"jsonrpc":"2.0","id":"1","method":"core.ping"}`
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.WriteStringLine(msg)
	}()

	line, err := client.ReadLine()
	if err != nil {
		t.Fatal(err)
	}
	if string(line) != msg {
		t.Fatalf("got %q want %q", line, msg)
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
}

func TestStreamLargeLine(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	server := NewStream(c1, 256*1024)
	client := NewStream(c2, 256*1024)

	payload := strings.Repeat("x", 100*1024)
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.WriteStringLine(payload)
	}()

	line, err := client.ReadLine()
	if err != nil {
		t.Fatal(err)
	}
	if string(line) != payload {
		t.Fatalf("length mismatch: got %d want %d", len(line), len(payload))
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
}

func TestStreamRejectsOversizeWrite(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	s := NewStream(c1, 16)
	if err := s.WriteStringLine(strings.Repeat("a", 17)); err == nil {
		t.Fatal("expected oversize write error")
	}
}

func TestStreamEOF(t *testing.T) {
	c1, c2 := net.Pipe()
	s := NewStream(c1, 0)
	_ = c2.Close()
	_, err := s.ReadLine()
	if err != io.EOF {
		t.Fatalf("got %v want EOF", err)
	}
}

func TestStreamCRLF(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	client := NewStream(c2, 0)
	go func() {
		_, _ = c1.Write([]byte("hello\r\n"))
	}()
	line, err := client.ReadLine()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(line, []byte("hello")) {
		t.Fatalf("got %q", line)
	}
}
