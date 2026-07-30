package internal

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"time"
)

const defaultShellTimeout = 60 * time.Second

// RunShell is the handler for shell.exec.
// Function body is identical to tools/shell.go; only the wrapper return type differs.
func RunShell(ctx context.Context, root string, command string) (string, error) {
	if strings.TrimSpace(command) == "" {
		return "", fmt.Errorf("empty shell command")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	toolCtx, cancel := context.WithTimeout(ctx, defaultShellTimeout)
	defer cancel()

	var cmd *exec.Cmd
	if goruntime.GOOS == "windows" {
		cmd = exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", command)
	} else {
		cmd = exec.Command("sh", "-c", command)
	}
	if root != "" {
		if resolved, err := filepath.Abs(root); err == nil {
			cmd.Dir = resolved
		}
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Stdin = bytes.NewReader(nil)

	if err := cmd.Start(); err != nil {
		return "", err
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-toolCtx.Done():
		killShellProcessTree(cmd)
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
		output := TruncateToolOutput(strings.TrimSpace(stdout.String() + "\n" + stderr.String()))
		if output != "" {
			return output, fmt.Errorf(
				"shell command timed out after %s (process did not exit; partial output was captured — the command may have already succeeded)",
				defaultShellTimeout,
			)
		}
		return "", fmt.Errorf("shell command timed out after %s", defaultShellTimeout)
	case err := <-done:
		output := TruncateToolOutput(strings.TrimSpace(stdout.String() + "\n" + stderr.String()))
		if err != nil {
			return output, err
		}
		return output, nil
	}
}

// killShellProcessTree terminates the shell and all child processes.
func killShellProcessTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	if goruntime.GOOS == "windows" && pid > 0 {
		killer := exec.Command("taskkill", "/F", "/T", "/PID", fmt.Sprintf("%d", pid))
		_ = killer.Run()
	}
	_ = cmd.Process.Kill()
}
