package tools

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

// defaultShellTimeout bounds shell.exec. Many CLI tools (e.g. Office automation)
// print success then hang on child/COM processes; we must still return.
const defaultShellTimeout = 60 * time.Second

func runShell(ctx context.Context, root string, command string) (string, error) {
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
		// -NonInteractive avoids prompts that hang the session after work is done.
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
	// Avoid inheriting a live stdin that some CLIs wait on forever.
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
		// Kill the whole tree: PowerShell may exit while officecli/COM children linger,
		// or the child may hang after printing success (exactly the OfficeCLI case).
		killShellProcessTree(cmd)
		// Give Wait a moment to observe the kill.
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
		output := TruncateToolOutput(strings.TrimSpace(stdout.String() + "\n" + stderr.String()))
		if output != "" {
			// Work often already finished (e.g. "Added slide at /slide[4]") but process
			// did not exit 鈥?surface partial success clearly instead of a bare timeout.
			return output, fmt.Errorf(
				"shell command timed out after %s (process did not exit; partial output was captured 鈥?the command may have already succeeded)",
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

// killShellProcessTree terminates the shell and its descendants.
// On Windows, Process.Kill only kills powershell.exe, not officecli.exe children.
func killShellProcessTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	if goruntime.GOOS == "windows" && pid > 0 {
		// /T = tree, /F = force
		killer := exec.Command("taskkill", "/F", "/T", "/PID", fmt.Sprintf("%d", pid))
		_ = killer.Run()
	}
	_ = cmd.Process.Kill()
}
