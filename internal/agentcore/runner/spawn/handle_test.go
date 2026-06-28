package spawn

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewSpawnedProcessHandle 测试创建句柄及初始属性
func TestNewSpawnedProcessHandle(t *testing.T) {
	cmd := exec.Command("echo", "test")
	cfg := DefaultSpawnConfig()

	handle := NewSpawnedProcessHandle(
		"proc-1",
		cmd,
		nil,
		nil,
		cfg,
		nil,
		0, // 使用默认 maxHealthFailures
	)

	if handle.ProcessID() != "proc-1" {
		t.Errorf("ProcessID() = %q, want \"proc-1\"", handle.ProcessID())
	}
	if handle.PID() != -1 {
		t.Errorf("PID() = %d, want -1（未启动）", handle.PID())
	}
	if handle.ExitCode() != -1 {
		t.Errorf("ExitCode() = %d, want -1（未退出）", handle.ExitCode())
	}
}

// TestSpawnedProcessHandle_ProcessID 测试 ProcessID 属性
func TestSpawnedProcessHandle_ProcessID(t *testing.T) {
	cmd := exec.Command("echo")
	cfg := DefaultSpawnConfig()
	handle := NewSpawnedProcessHandle("test-proc-id", cmd, nil, nil, cfg, nil, 2)

	if got := handle.ProcessID(); got != "test-proc-id" {
		t.Errorf("ProcessID() = %q, want \"test-proc-id\"", got)
	}
}

// TestSpawnedProcessHandle_IsAlive 测试 IsAlive 判断
func TestSpawnedProcessHandle_IsAlive(t *testing.T) {
	cfg := DefaultSpawnConfig()

	// cmd 为 nil
	handle := NewSpawnedProcessHandle("p1", nil, nil, nil, cfg, nil, 2)
	if handle.IsAlive() {
		t.Error("cmd 为 nil 时 IsAlive() 应为 false")
	}

	// cmd 存在但 Process 为 nil（未启动）
	cmd := exec.Command("echo")
	handle = NewSpawnedProcessHandle("p2", cmd, nil, nil, cfg, nil, 2)
	if handle.IsAlive() {
		t.Error("cmd.Process 为 nil 时 IsAlive() 应为 false")
	}
}

// TestSpawnedProcessHandle_IsHealthy 测试 IsHealthy 判断
func TestSpawnedProcessHandle_IsHealthy(t *testing.T) {
	cfg := DefaultSpawnConfig()
	cmd := exec.Command("echo")
	handle := NewSpawnedProcessHandle("p1", cmd, nil, nil, cfg, nil, 2)

	// 初始 isHealthy=true，但 cmd.Process 为 nil → IsAlive()=false
	if handle.IsHealthy() {
		t.Error("进程未启动时 IsHealthy() 应为 false")
	}

	// 手动设置 isHealthy=false，验证不健康
	handle.mu.Lock()
	handle.isHealthy = false
	handle.mu.Unlock()
	if handle.IsHealthy() {
		t.Error("isHealthy=false 且进程未启动时 IsHealthy() 应为 false")
	}
}

// TestSpawnedProcessHandle_ExitCode 测试 ExitCode
func TestSpawnedProcessHandle_ExitCode(t *testing.T) {
	cfg := DefaultSpawnConfig()

	// cmd 为 nil
	handle := NewSpawnedProcessHandle("p1", nil, nil, nil, cfg, nil, 2)
	if handle.ExitCode() != -1 {
		t.Errorf("cmd 为 nil 时 ExitCode() = %d, want -1", handle.ExitCode())
	}

	// cmd 存在但未退出
	cmd := exec.Command("echo")
	handle = NewSpawnedProcessHandle("p2", cmd, nil, nil, cfg, nil, 2)
	if handle.ExitCode() != -1 {
		t.Errorf("进程未退出时 ExitCode() = %d, want -1", handle.ExitCode())
	}
}

// TestSpawnedProcessHandle_PID 测试 PID
func TestSpawnedProcessHandle_PID(t *testing.T) {
	cfg := DefaultSpawnConfig()

	// cmd 为 nil
	handle := NewSpawnedProcessHandle("p1", nil, nil, nil, cfg, nil, 2)
	if handle.PID() != -1 {
		t.Errorf("cmd 为 nil 时 PID() = %d, want -1", handle.PID())
	}

	// cmd 存在但 Process 为 nil
	cmd := exec.Command("echo")
	handle = NewSpawnedProcessHandle("p2", cmd, nil, nil, cfg, nil, 2)
	if handle.PID() != -1 {
		t.Errorf("cmd.Process 为 nil 时 PID() = %d, want -1", handle.PID())
	}
}

// TestSpawnedProcessHandle_SendMessage_进程未运行 测试 SendMessage 在进程未运行时返回错误
func TestSpawnedProcessHandle_SendMessage_进程未运行(t *testing.T) {
	cfg := DefaultSpawnConfig()
	cmd := exec.Command("echo")
	handle := NewSpawnedProcessHandle("p1", cmd, nil, nil, cfg, nil, 2)

	msg := NewMessage(MessageTypeInput, nil)
	err := handle.SendMessage(context.Background(), msg)
	if err == nil {
		t.Error("进程未运行时 SendMessage 应返回错误")
	}
	if !strings.Contains(err.Error(), "未运行") {
		t.Errorf("错误信息应包含 \"未运行\"，实际: %v", err)
	}
}

// TestSpawnedProcessHandle_SendMessage_写入 测试 SendMessage 通过管道写入消息
func TestSpawnedProcessHandle_SendMessage_写入(t *testing.T) {
	cfg := DefaultSpawnConfig()
	var buf bytes.Buffer
	cmd := exec.Command("echo")

	// 使用 nopWriteCloser 包装 buf 作为 stdin
	handle := NewSpawnedProcessHandle(
		"p1", cmd,
		nopWriteCloser{Writer: &buf},
		nil, cfg, nil, 2,
	)

	// 手动模拟进程已启动（设置 cmd.Process）
	cmd.Process = &os.Process{Pid: 12345}
	cmd.ProcessState = nil // 未退出

	msg := NewMessage(MessageTypeInput, map[string]any{"key": "val"})
	err := handle.SendMessage(context.Background(), msg)
	if err != nil {
		t.Fatalf("SendMessage 失败: %v", err)
	}

	// 验证 buf 中写入了 JSON + 换行
	if buf.Len() == 0 {
		t.Error("SendMessage 应向 stdin 写入数据")
	}
	if !strings.HasSuffix(buf.String(), "\n") {
		t.Error("SendMessage 写入的数据应以换行符结尾")
	}
}

// TestSpawnedProcessHandle_ReceiveMessage 测试 ReceiveMessage 从管道读取消息
func TestSpawnedProcessHandle_ReceiveMessage(t *testing.T) {
	cfg := DefaultSpawnConfig()
	cmd := exec.Command("echo")

	// 构造包含一条消息的 stdout
	msg := NewMessage(MessageTypeOutput, map[string]any{"result": "ok"})
	var buf bytes.Buffer
	if err := WriteMessage(&buf, msg); err != nil {
		t.Fatalf("WriteMessage 失败: %v", err)
	}

	handle := NewSpawnedProcessHandle(
		"p1", cmd, nil, &buf, cfg, nil, 2,
	)

	received, err := handle.ReceiveMessage(context.Background())
	if err != nil {
		t.Fatalf("ReceiveMessage 失败: %v", err)
	}
	if received.Type != MessageTypeOutput {
		t.Errorf("Type = %d, want %d", received.Type, MessageTypeOutput)
	}
}

// TestSpawnedProcessHandle_StartHealthCheck_重复启动 测试重复启动健康检查返回错误
func TestSpawnedProcessHandle_StartHealthCheck_重复启动(t *testing.T) {
	cfg := DefaultSpawnConfig()
	cmd := exec.Command("echo")
	handle := NewSpawnedProcessHandle("p1", cmd, nil, nil, cfg, nil, 2)

	// 第一次启动
	err := handle.StartHealthCheck(context.Background(), 10*time.Second)
	if err != nil {
		t.Fatalf("首次 StartHealthCheck 失败: %v", err)
	}
	defer handle.StopHealthCheck()

	// 重复启动应返回错误
	err = handle.StartHealthCheck(context.Background(), 10*time.Second)
	if err == nil {
		t.Error("重复启动健康检查应返回错误")
	}
}

// TestSpawnedProcessHandle_StopHealthCheck_未启动 测试未启动时 StopHealthCheck 不报错
func TestSpawnedProcessHandle_StopHealthCheck_未启动(t *testing.T) {
	cfg := DefaultSpawnConfig()
	cmd := exec.Command("echo")
	handle := NewSpawnedProcessHandle("p1", cmd, nil, nil, cfg, nil, 2)

	err := handle.StopHealthCheck()
	if err != nil {
		t.Errorf("未启动时 StopHealthCheck 不应返回错误，实际: %v", err)
	}
}

// TestSpawnedProcessHandle_ForceKill_进程未运行 测试 ForceKill 在进程未运行时
func TestSpawnedProcessHandle_ForceKill_进程未运行(t *testing.T) {
	cfg := DefaultSpawnConfig()
	cmd := exec.Command("echo")
	handle := NewSpawnedProcessHandle("p1", cmd, nil, nil, cfg, nil, 2)

	err := handle.ForceKill()
	if err != nil {
		t.Errorf("进程未运行时 ForceKill 不应返回错误，实际: %v", err)
	}
}

// TestSpawnedProcessHandle_Shutdown_进程未运行 测试 Shutdown 在进程未运行时
func TestSpawnedProcessHandle_Shutdown_进程未运行(t *testing.T) {
	cfg := DefaultSpawnConfig()
	cmd := exec.Command("echo")
	handle := NewSpawnedProcessHandle("p1", cmd, nil, nil, cfg, nil, 2)

	killed, err := handle.Shutdown(context.Background())
	if err != nil {
		t.Fatalf("进程未运行时 Shutdown 不应返回错误，实际: %v", err)
	}
	if !killed {
		t.Error("进程未运行时 Shutdown 应返回 killed=true")
	}
}

// TestSpawnedProcessHandle_Shutdown_重复关闭 测试重复关闭返回错误
func TestSpawnedProcessHandle_Shutdown_重复关闭(t *testing.T) {
	cfg := DefaultSpawnConfig()
	cmd := exec.Command("echo")
	handle := NewSpawnedProcessHandle("p1", cmd, nil, nil, cfg, nil, 2)

	// 第一次关闭（进程未运行，直接返回）
	_, _ = handle.Shutdown(context.Background())

	// 重复关闭
	_, err := handle.Shutdown(context.Background())
	if err == nil {
		t.Error("重复关闭应返回错误")
	}
}

// TestSpawnedProcessHandle_recordHealthFailure 测试健康检查失败记录
func TestSpawnedProcessHandle_recordHealthFailure(t *testing.T) {
	cfg := DefaultSpawnConfig()
	cmd := exec.Command("echo")

	unhealthyCalled := false
	onUnhealthy := func() { unhealthyCalled = true }

	handle := NewSpawnedProcessHandle("p1", cmd, nil, nil, cfg, onUnhealthy, 2)

	// 第一次失败
	handle.recordHealthFailure()
	handle.mu.Lock()
	fails1 := handle.consecutiveFails
	healthy1 := handle.isHealthy
	handle.mu.Unlock()
	if fails1 != 1 {
		t.Errorf("consecutiveFails = %d, want 1", fails1)
	}
	if healthy1 {
		t.Error("isHealthy 应为 false")
	}

	// 第二次失败，达到 maxHealthFailures=2，应触发回调
	handle.recordHealthFailure()
	if !unhealthyCalled {
		t.Error("达到 maxHealthFailures 时应触发 onUnhealthy 回调")
	}

	// 第三次失败，回调不应重复触发
	unhealthyCalled = false
	handle.recordHealthFailure()
	if unhealthyCalled {
		t.Error("回调已触发后不应重复触发")
	}
}

// TestSpawnedProcessHandle_MaxHealthFailures_默认值 测试默认 maxHealthFailures
func TestSpawnedProcessHandle_MaxHealthFailures_默认值(t *testing.T) {
	cfg := DefaultSpawnConfig()
	cmd := exec.Command("echo")
	handle := NewSpawnedProcessHandle("p1", cmd, nil, nil, cfg, nil, 0)

	handle.mu.Lock()
	maxFails := handle.maxHealthFailures
	handle.mu.Unlock()
	if maxFails != DefaultMaxHealthFailures {
		t.Errorf("maxHealthFailures = %d, want %d", maxFails, DefaultMaxHealthFailures)
	}
}

// TestSpawnedProcessHandle_WaitForCompletion_进程未启动 测试 WaitForCompletion 在进程未启动时
func TestSpawnedProcessHandle_WaitForCompletion_进程未启动(t *testing.T) {
	cfg := DefaultSpawnConfig()
	cmd := exec.Command("echo")
	handle := NewSpawnedProcessHandle("p1", cmd, nil, nil, cfg, nil, 2)

	exitCode, err := handle.WaitForCompletion()
	if err == nil {
		t.Error("进程未启动时 WaitForCompletion 应返回错误")
	}
	if exitCode != -1 {
		t.Errorf("ExitCode = %d, want -1", exitCode)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// nopWriteCloser 将 io.Writer 包装为 io.WriteCloser（Close 为空操作）。
type nopWriteCloser struct {
	io.Writer
}

func (nopWriteCloser) Close() error { return nil }

// nopMutexWriter 用于测试的线程安全写入器。
type nopMutexWriter struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (w *nopMutexWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

// TestSpawnedProcessHandle_isWindows 测试平台判断
func TestSpawnedProcessHandle_isWindows(t *testing.T) {
	cfg := DefaultSpawnConfig()
	cmd := exec.Command("echo")
	handle := NewSpawnedProcessHandle("p1", cmd, nil, nil, cfg, nil, 2)

	// 在 Linux 上应返回 false
	if handle.isWindows() {
		t.Error("Linux 上 isWindows() 应为 false")
	}
}

// TestSpawnedProcessHandle_forceTerminate_进程未运行 测试 forceTerminate 在进程未运行时
func TestSpawnedProcessHandle_forceTerminate_进程未运行(t *testing.T) {
	cfg := DefaultSpawnConfig()
	cmd := exec.Command("echo")
	handle := NewSpawnedProcessHandle("p1", cmd, nil, nil, cfg, nil, 2)

	killed, err := handle.forceTerminate()
	if err != nil {
		t.Errorf("进程未运行时 forceTerminate 不应返回错误，实际: %v", err)
	}
	if !killed {
		t.Error("进程未运行时 forceTerminate 应返回 killed=true")
	}
}

// TestSpawnedProcessHandle_IsAlive_已退出 测试 IsAlive 在进程已退出时
func TestSpawnedProcessHandle_IsAlive_已退出(t *testing.T) {
	cfg := DefaultSpawnConfig()
	cmd := exec.Command("echo")
	// 模拟进程已退出：设置 ProcessState
	cmd.Process = &os.Process{Pid: 12345}
	cmd.ProcessState = &os.ProcessState{}

	handle := NewSpawnedProcessHandle("p1", cmd, nil, nil, cfg, nil, 2)
	if handle.IsAlive() {
		t.Error("进程已退出时 IsAlive() 应为 false")
	}
}

// TestSpawnedProcessHandle_PID_已启动 测试 PID 在进程已启动时
func TestSpawnedProcessHandle_PID_已启动(t *testing.T) {
	cfg := DefaultSpawnConfig()
	cmd := exec.Command("echo")
	cmd.Process = &os.Process{Pid: 12345}

	handle := NewSpawnedProcessHandle("p1", cmd, nil, nil, cfg, nil, 2)
	if handle.PID() != 12345 {
		t.Errorf("PID() = %d, want 12345", handle.PID())
	}
}

// TestSpawnedProcessHandle_ExitCode_已退出 测试 ExitCode 在进程已退出时
func TestSpawnedProcessHandle_ExitCode_已退出(t *testing.T) {
	cfg := DefaultSpawnConfig()
	cmd := exec.Command("echo")
	cmd.Process = &os.Process{Pid: 12345}
	// 运行一个快速命令以获取真实的 ProcessState
	realCmd := exec.Command("true")
	_ = realCmd.Run()
	cmd.ProcessState = realCmd.ProcessState

	handle := NewSpawnedProcessHandle("p1", cmd, nil, nil, cfg, nil, 2)
	if handle.ExitCode() != 0 {
		t.Errorf("ExitCode() = %d, want 0", handle.ExitCode())
	}
}

// TestSpawnedProcessHandle_ReceiveMessage_读取错误 测试 ReceiveMessage 读取错误
func TestSpawnedProcessHandle_ReceiveMessage_读取错误(t *testing.T) {
	cfg := DefaultSpawnConfig()
	cmd := exec.Command("echo")
	handle := NewSpawnedProcessHandle("p1", cmd, nil, &errorReader{}, cfg, nil, 2)

	_, err := handle.ReceiveMessage(context.Background())
	if err == nil {
		t.Error("读取错误时应返回错误")
	}
}

// TestSpawnedProcessHandle_WaitForCompletion_关闭stdin 测试 WaitForCompletion 关闭 stdin
func TestSpawnedProcessHandle_WaitForCompletion_关闭stdin(t *testing.T) {
	cfg := DefaultSpawnConfig()
	cmd := exec.Command("echo")

	var buf bytes.Buffer
	handle := NewSpawnedProcessHandle(
		"p1", cmd,
		nopWriteCloser{Writer: &buf},
		nil, cfg, nil, 2,
	)

	// 进程未启动，应返回错误但先关闭 stdin
	_, err := handle.WaitForCompletion()
	if err == nil {
		t.Error("进程未启动时 WaitForCompletion 应返回错误")
	}
}

// TestSpawnedProcessHandle_ForceKill_已退出 测试 ForceKill 在进程已退出时
func TestSpawnedProcessHandle_ForceKill_已退出(t *testing.T) {
	cfg := DefaultSpawnConfig()
	cmd := exec.Command("echo")
	cmd.Process = &os.Process{Pid: 12345}
	cmd.ProcessState = &os.ProcessState{}

	handle := NewSpawnedProcessHandle("p1", cmd, nil, nil, cfg, nil, 2)
	err := handle.ForceKill()
	if err != nil {
		t.Errorf("进程已退出时 ForceKill 不应返回错误，实际: %v", err)
	}
}

// ──────────────────────────── 非导出辅助 ────────────────────────────

// errorReader 总是返回错误的 io.Reader
type errorReader struct{}

func (r *errorReader) Read(p []byte) (int, error) {
	return 0, fmt.Errorf("读取错误")
}
