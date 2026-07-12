package ipc

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestListenDialRoundTrip(t *testing.T) {
	session, err := ListenTemp("red-panda-ipc-test")
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	errCh := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		conn, err := DialAddr(ctx, session.Addr)
		if err != nil {
			errCh <- err
			return
		}
		defer conn.Close()
		stream := NewStream(conn, 0)
		line, err := stream.ReadLine()
		if err != nil {
			errCh <- err
			return
		}
		if string(line) != "ping" {
			errCh <- errString("unexpected line: " + string(line))
			return
		}
		errCh <- stream.WriteStringLine("pong")
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := session.Accept(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	stream := NewStream(conn, 0)
	if err := stream.WriteStringLine("ping"); err != nil {
		t.Fatal(err)
	}
	line, err := stream.ReadLine()
	if err != nil {
		t.Fatal(err)
	}
	if string(line) != "pong" {
		t.Fatalf("got %q want pong", line)
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
}

func TestStartProcessEcho(t *testing.T) {
	// Build a tiny helper that dials RED_PANDA_IPC_ADDR and echoes one line.
	dir := t.TempDir()
	src := filepath.Join(dir, "echo_child.go")
	bin := filepath.Join(dir, "echo_child")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	helper := `package main
import (
  "context"
  "os"
  "time"
  "redpanda/ipc"
)
func main() {
  ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
  defer cancel()
  conn, err := ipc.DialFromEnv(ctx)
  if err != nil { os.Exit(2) }
  defer conn.Close()
  s := ipc.NewStream(conn, 0)
  line, err := s.ReadLine()
  if err != nil { os.Exit(3) }
  if err := s.WriteLine(line); err != nil { os.Exit(4) }
}
`
	if err := os.WriteFile(src, []byte(helper), 0o600); err != nil {
		t.Fatal(err)
	}

	modRoot, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	// Isolate helper module; replace points at the local ipc package.
	helperMod := filepath.Join(dir, "go.mod")
	modContent := "module echo_child\n\ngo 1.25.0\n\nrequire redpanda/ipc v0.0.0\n\nreplace redpanda/ipc => " +
		filepath.ToSlash(modRoot) + "\n"
	if err := os.WriteFile(helperMod, []byte(modContent), 0o600); err != nil {
		t.Fatal(err)
	}
	mainSrc := filepath.Join(dir, "main.go")
	if err := os.WriteFile(mainSrc, []byte(helper), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(src)

	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = dir
	tidy.Env = append(os.Environ(), "GOWORK=off")
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy helper: %v\n%s", err, out)
	}
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build helper: %v\n%s", err, out)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var stderr bytesBuffer
	proc, err := StartProcess(ctx, ProcessConfig{
		Command: bin,
		Prefix:  "red-panda-echo",
		Configure: func(c *exec.Cmd) {
			c.Stderr = &stderr
		},
	})
	if err != nil {
		t.Fatalf("StartProcess: %v\nstderr: %s", err, stderr.String())
	}
	defer proc.Close()

	if err := proc.Stream.WriteStringLine(`{"jsonrpc":"2.0","id":"1","method":"core.ping"}`); err != nil {
		t.Fatal(err)
	}
	line, err := proc.Stream.ReadLine()
	if err != nil {
		t.Fatalf("ReadLine: %v\nstderr: %s", err, stderr.String())
	}
	want := `{"jsonrpc":"2.0","id":"1","method":"core.ping"}`
	if string(line) != want {
		t.Fatalf("got %q want %q", line, want)
	}
}

type errString string

func (e errString) Error() string { return string(e) }

// bytesBuffer is a tiny concurrency-safe stderr sink for tests.
type bytesBuffer struct {
	b []byte
}

func (b *bytesBuffer) Write(p []byte) (int, error) {
	b.b = append(b.b, p...)
	return len(p), nil
}

func (b *bytesBuffer) String() string {
	return string(b.b)
}

// Ensure io.Writer assignment compiles.
var _ io.Writer = (*bytesBuffer)(nil)
