//go:build !windows

package processcli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCaptureDiagnostics_debug透传 debug 模式不重定向，LogPath 为空。
func TestCaptureDiagnostics_debug透传(t *testing.T) {
	streams, restore, err := CaptureDiagnostics(true, true)
	if err != nil {
		t.Fatalf("CaptureDiagnostics: %v", err)
	}
	defer restore()
	if streams.LogPath != "" {
		t.Errorf("debug 模式 LogPath = %q, 期望空", streams.LogPath)
	}
	fmt.Fprintln(streams.Stdout, "hello")
}

// TestCaptureDiagnostics_写入日志文件 stderr 被重定向到日志文件。
func TestCaptureDiagnostics_写入日志文件(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", dir)

	streams, restore, err := CaptureDiagnostics(false, false)
	if err != nil {
		t.Fatalf("CaptureDiagnostics: %v", err)
	}
	if streams.LogPath == "" {
		restore()
		t.Fatal("非 debug 模式应产生日志路径")
	}
	_, _ = os.Stderr.WriteString("diagnostic-line\n")
	restore()

	raw, readErr := os.ReadFile(streams.LogPath)
	if readErr != nil {
		t.Fatalf("读日志: %v", readErr)
	}
	if !strings.Contains(string(raw), "diagnostic-line") {
		t.Errorf("日志未包含写入内容：%q", string(raw))
	}
	if !strings.Contains(streams.LogPath, filepath.Join("agent", "process_cli_logs")) {
		t.Errorf("日志路径 = %q, 期望含 agent/process_cli_logs", streams.LogPath)
	}
}

// TestCaptureDiagnostics_恢复后stdout可用。
func TestCaptureDiagnostics_恢复后stdout可用(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", dir)

	_, restore, err := CaptureDiagnostics(true, false)
	if err != nil {
		t.Fatalf("CaptureDiagnostics: %v", err)
	}
	restore()
	if _, writeErr := os.Stdout.WriteString(""); writeErr != nil {
		t.Errorf("恢复后 stdout 不可写: %v", writeErr)
	}
}

// TestCaptureDiagnostics_环境变量开启透传 UAPCLAW_PROCESS_DEBUG=1 等同 debug。
func TestCaptureDiagnostics_环境变量开启透传(t *testing.T) {
	t.Setenv(processDebugEnv, "1")
	streams, restore, err := CaptureDiagnostics(true, false)
	if err != nil {
		t.Fatalf("CaptureDiagnostics: %v", err)
	}
	defer restore()
	if streams.LogPath != "" {
		t.Errorf("环境变量开启时不应产生日志路径，得 %q", streams.LogPath)
	}
}
