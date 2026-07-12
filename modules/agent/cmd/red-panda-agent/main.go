package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"redpanda/agent/internal/runtime"
	"redpanda/ipc"
)

var version = "dev"

func main() {
	in, out, closer, err := openTransport()
	if err != nil {
		fmt.Fprintf(os.Stderr, "red-panda-agent: %v\n", err)
		os.Exit(1)
	}
	if closer != nil {
		defer closer.Close()
	}

	rt := runtime.New(in, out, os.Stderr, version)
	if err := rt.Serve(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "red-panda-agent: %v\n", err)
		os.Exit(1)
	}
}

// openTransport prefers IPC when RED_PANDA_IPC_ADDR is set (normal gateway
// launch path). Falls back to stdio for manual/local debugging without a
// parent listener. Diagnostic logs always stay on stderr.
func openTransport() (in io.Reader, out io.Writer, closer io.Closer, err error) {
	if strings.TrimSpace(os.Getenv(ipc.EnvAddr)) == "" {
		return os.Stdin, os.Stdout, nil, nil
	}
	conn, err := ipc.DialFromEnv(context.Background())
	if err != nil {
		return nil, nil, nil, fmt.Errorf("ipc dial: %w", err)
	}
	// net.Conn is both Reader and Writer; one connection replaces the pair of pipes.
	return conn, conn, conn, nil
}
