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

// defaultShellTimeout 限制 shell.exec 的执行时长。许多命令行工具（如 Office 自动化）
// 输出成功后仍会在子进程或 COM 进程上挂起，因此必须及时返回。
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
		// -NonInteractive 避免任务完成后出现交互提示而挂起会话。
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
	// 避免继承活动 stdin，部分命令行工具会永久等待该输入。
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
		// 终止整个进程树：PowerShell 退出后 officecli 或 COM 子进程可能残留，
		// 子进程也可能在输出成功后挂起（OfficeCLI 的典型情况）。
		killShellProcessTree(cmd)
		// 留出短暂时间供 Wait 感知终止操作。
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
		output := TruncateToolOutput(strings.TrimSpace(stdout.String() + "\n" + stderr.String()))
		if output != "" {
			// 工作可能已完成（例如“已在 /slide[4] 添加幻灯片”），但进程尚未退出；
			// 应明确呈现部分成功，而非只返回超时。
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

// killShellProcessTree 终止 shell 及其所有后代进程。
// 在 Windows 上，Process.Kill 只会终止 powershell.exe，不会终止 officecli.exe 子进程。
func killShellProcessTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	if goruntime.GOOS == "windows" && pid > 0 {
		// /T 表示进程树，/F 表示强制终止。
		killer := exec.Command("taskkill", "/F", "/T", "/PID", fmt.Sprintf("%d", pid))
		_ = killer.Run()
	}
	_ = cmd.Process.Kill()
}
