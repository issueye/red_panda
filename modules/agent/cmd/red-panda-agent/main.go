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

// openTransport 在设置 RED_PANDA_IPC_ADDR 时优先使用 IPC（正常 Gateway 启动路径）。
// 手动或本地调试未启动父监听器时回退为 stdio。诊断日志始终输出到 stderr。
func openTransport() (in io.Reader, out io.Writer, closer io.Closer, err error) {
	if strings.TrimSpace(os.Getenv(ipc.EnvAddr)) == "" {
		return os.Stdin, os.Stdout, nil, nil
	}
	conn, err := ipc.DialFromEnv(context.Background())
	if err != nil {
		return nil, nil, nil, fmt.Errorf("ipc dial: %w", err)
	}
	// net.Conn 同时实现 Reader 和 Writer，一个连接即可替代一对管道。
	return conn, conn, conn, nil
}
