package main

import (
	"context"
	"fmt"
	"os"

	"redpanda/agent/internal/runtime"
)

var version = "dev"

func main() {
	rt := runtime.New(os.Stdin, os.Stdout, os.Stderr, version)
	if err := rt.Serve(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "red-panda-agent: %v\n", err)
		os.Exit(1)
	}
}
