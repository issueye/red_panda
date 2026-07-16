package mcpkit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRedactSecrets(t *testing.T) {
	got := RedactSecrets("token=abc123 and abc123", map[string]string{"KEY": "abc123"})
	if strings.Contains(got, "abc123") || !strings.Contains(got, "****") {
		t.Fatalf("redact failed: %q", got)
	}
}

func TestResolveDir(t *testing.T) {
	if got := resolveDir("rel", "E:\\ws"); !strings.Contains(got, "rel") {
		t.Fatalf("resolveDir relative: %q", got)
	}
	if got := resolveDir("C:\\abs", "E:\\ws"); got != "C:\\abs" && got != `C:\abs` {
		// On non-Windows this may differ; only assert absolute path is preserved when IsAbs.
		if filepath.IsAbs("C:\\abs") && got != "C:\\abs" {
			t.Fatalf("resolveDir abs: %q", got)
		}
	}
	if got := resolveDir("", "/ws"); got != "/ws" {
		t.Fatalf("resolveDir empty dir: %q", got)
	}
}

func TestDiscoverDisabledCommand(t *testing.T) {
	result := Discover(context.Background(), SessionConfig{}, DiscoverOptions{StartMS: 100})
	if result.Status != "failed" || !strings.Contains(result.Error, "command is required") {
		t.Fatalf("expected command required, got %#v", result)
	}
}

func TestOpenMissingCommand(t *testing.T) {
	_, err := Open(context.Background(), SessionConfig{Command: ""})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFormatPhaseErrorTimeout(t *testing.T) {
	err := context.DeadlineExceeded
	if got := formatPhaseError(err); got != "timeout" {
		t.Fatalf("formatPhaseError = %q", got)
	}
}

func TestBoundedBufferCaps(t *testing.T) {
	b := &boundedBuffer{max: 8}
	_, _ = b.Write([]byte("abcdefghijklmnop"))
	if got := b.String(); got != "abcdefgh" {
		t.Fatalf("bounded = %q", got)
	}
}

func TestCallRequiresToolName(t *testing.T) {
	_, err := Call(context.Background(), SessionConfig{Command: os.Args[0]}, "", nil, CallOptions{})
	if err == nil || !strings.Contains(err.Error(), "tool name") {
		t.Fatalf("expected tool name error, got %v", err)
	}
}

// Smoke: Open against a non-executable path fails quickly.
func TestOpenBadCommand(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := Open(ctx, SessionConfig{
		Command: filepath.Join(t.TempDir(), "no-such-mcp-binary"),
	})
	if err == nil {
		t.Fatal("expected start failure")
	}
}
