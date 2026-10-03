package dreaming

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/dreaming"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestGetDreamingOrchestrator_空映射 验证空映射时返回 nil
func TestGetDreamingOrchestrator_空映射(t *testing.T) {
	resetOrchestrators()
	result := GetDreamingOrchestrator("agent")
	if result != nil {
		t.Error("空映射时应返回 nil")
	}
}

// TestStartDreaming_配置未启用 验证 enabled=false 时返回 nil
func TestStartDreaming_配置未启用(t *testing.T) {
	resetOrchestrators()
	ctx := context.Background()
	// 默认配置 enabled=false
	orch, err := StartDreaming(ctx, "/nonexistent/sessions", "/nonexistent/output", "agent", "zh", nil)
	if err != nil {
		t.Errorf("未启用时应返回 nil, nil，实际 err=%v", err)
	}
	if orch != nil {
		t.Error("未启用时应返回 nil")
	}
}

// TestStartDreaming_幂等性 验证同一 mode 重复调用返回已有实例
func TestStartDreaming_幂等性(t *testing.T) {
	resetOrchestrators()

	// 准备临时目录
	tmpDir := t.TempDir()
	sessionsDir := filepath.Join(tmpDir, "sessions")
	outputDir := filepath.Join(tmpDir, "memory")
	os.MkdirAll(sessionsDir, 0o755)
	os.MkdirAll(outputDir, 0o755)

	// 设置环境变量启用
	envKey := "DREAMING_AGENT_ENABLED"
	os.Setenv(envKey, "true")
	defer os.Unsetenv(envKey)

	// 使用超大间隔避免实际 sweep
	os.Setenv("DREAMING_INTERVAL", "999999")
	defer os.Unsetenv("DREAMING_INTERVAL")

	ctx := context.Background()
	orch1, err := StartDreaming(ctx, sessionsDir, outputDir, "agent", "zh", nil)
	if err != nil {
		t.Fatalf("首次启动失败: %v", err)
	}
	if orch1 == nil {
		t.Fatal("首次启动应返回非 nil")
	}

	// 重复调用应返回同一实例
	orch2, err := StartDreaming(ctx, sessionsDir, outputDir, "agent", "zh", nil)
	if err != nil {
		t.Fatalf("重复调用失败: %v", err)
	}
	if orch2 != orch1 {
		t.Error("幂等调用应返回同一实例")
	}

	// 清理
	StopDreaming(ctx, "agent")
}

// TestStopDreaming_指定模式 验证停止指定模式
func TestStopDreaming_指定模式(t *testing.T) {
	resetOrchestrators()

	tmpDir := t.TempDir()
	sessionsDir := filepath.Join(tmpDir, "sessions")
	outputDir := filepath.Join(tmpDir, "memory")
	os.MkdirAll(sessionsDir, 0o755)
	os.MkdirAll(outputDir, 0o755)

	envKey := "DREAMING_AGENT_ENABLED"
	os.Setenv(envKey, "true")
	defer os.Unsetenv(envKey)
	os.Setenv("DREAMING_INTERVAL", "999999")
	defer os.Unsetenv("DREAMING_INTERVAL")

	ctx := context.Background()
	orch, _ := StartDreaming(ctx, sessionsDir, outputDir, "agent", "zh", nil)
	if orch == nil {
		t.Fatal("启动失败")
	}

	StopDreaming(ctx, "agent")

	result := GetDreamingOrchestrator("agent")
	if result != nil {
		t.Error("停止后应返回 nil")
	}
}

// TestStopDreaming_空模式停止全部 验证 mode="" 时停止所有
func TestStopDreaming_空模式停止全部(t *testing.T) {
	resetOrchestrators()

	tmpDir := t.TempDir()
	os.MkdirAll(filepath.Join(tmpDir, "sessions"), 0o755)
	os.MkdirAll(filepath.Join(tmpDir, "memory"), 0o755)

	ctx := context.Background()

	// 手动注入两个 orchestrator（跳过 enabled 检查）
	var sweepCalls atomic.Int32
	orch1 := dreaming.NewDreamingOrchestrator(
		func(ctx context.Context) error {
			sweepCalls.Add(1)
			return nil
		},
		999999*time.Second,
		dreaming.WithName("dreaming-agent"),
	)
	orch2 := dreaming.NewDreamingOrchestrator(
		func(ctx context.Context) error {
			sweepCalls.Add(1)
			return nil
		},
		999999*time.Second,
		dreaming.WithName("dreaming-code"),
	)

	orch1.Start(ctx)
	orch2.Start(ctx)
	orchestratorsMu.Lock()
	orchestrators["agent"] = orch1
	orchestrators["code"] = orch2
	orchestratorsMu.Unlock()

	// 停止全部
	StopDreaming(ctx, "")

	if GetDreamingOrchestrator("agent") != nil {
		t.Error("agent 应已停止")
	}
	if GetDreamingOrchestrator("code") != nil {
		t.Error("code 应已停止")
	}
}

// TestStopDreaming_幂等性 验证重复停止不做任何操作
func TestStopDreaming_幂等性(t *testing.T) {
	resetOrchestrators()
	ctx := context.Background()
	// 未启动时停止不应 panic
	StopDreaming(ctx, "agent")
	StopDreaming(ctx, "")
}

// TestStartDreaming_SweeperInit失败 验证 Sweeper init 失败时返回错误
func TestStartDreaming_SweeperInit失败(t *testing.T) {
	resetOrchestrators()

	envKey := "DREAMING_AGENT_ENABLED"
	os.Setenv(envKey, "true")
	defer os.Unsetenv(envKey)
	os.Setenv("DREAMING_INTERVAL", "999999")
	defer os.Unsetenv("DREAMING_INTERVAL")

	ctx := context.Background()
	// 使用不可能的路径
	_, err := StartDreaming(ctx, "/proc/impossible/path", "/proc/impossible/path", "agent", "zh", nil)
	if err == nil {
		t.Error("Sweeper init 失败时应返回错误")
	}
}

// TestStartDreaming_WithBusyChecker 验证 busyChecker 传递到 Orchestrator
func TestStartDreaming_WithBusyChecker(t *testing.T) {
	resetOrchestrators()

	tmpDir := t.TempDir()
	sessionsDir := filepath.Join(tmpDir, "sessions")
	outputDir := filepath.Join(tmpDir, "memory")
	os.MkdirAll(sessionsDir, 0o755)
	os.MkdirAll(outputDir, 0o755)

	envKey := "DREAMING_AGENT_ENABLED"
	os.Setenv(envKey, "true")
	defer os.Unsetenv(envKey)
	os.Setenv("DREAMING_INTERVAL", "999999")
	defer os.Unsetenv("DREAMING_INTERVAL")

	var busyCalled atomic.Int32
	busyChecker := func() bool {
		busyCalled.Add(1)
		return false
	}

	ctx := context.Background()
	orch, err := StartDreaming(ctx, sessionsDir, outputDir, "agent", "zh", busyChecker)
	if err != nil {
		t.Fatalf("启动失败: %v", err)
	}
	if orch == nil {
		t.Fatal("应返回非 nil")
	}

	// Health 应该报告 running=true
	h := orch.Health()
	if !h.Running {
		t.Error("Orchestrator 应在运行")
	}

	StopDreaming(ctx, "agent")
}

// TestStartDreaming_CodeMode 验证 code 模式启动
func TestStartDreaming_CodeMode(t *testing.T) {
	resetOrchestrators()

	tmpDir := t.TempDir()
	sessionsDir := filepath.Join(tmpDir, "sessions")
	outputDir := filepath.Join(tmpDir, "coding_memory")
	os.MkdirAll(sessionsDir, 0o755)
	os.MkdirAll(outputDir, 0o755)

	envKey := "DREAMING_CODE_ENABLED"
	os.Setenv(envKey, "true")
	defer os.Unsetenv(envKey)
	os.Setenv("DREAMING_INTERVAL", "999999")
	defer os.Unsetenv("DREAMING_INTERVAL")

	ctx := context.Background()
	orch, err := StartDreaming(ctx, sessionsDir, outputDir, "code", "zh", nil)
	if err != nil {
		t.Fatalf("code 模式启动失败: %v", err)
	}
	if orch == nil {
		t.Fatal("code 模式应返回非 nil")
	}

	StopDreaming(ctx, "code")
}

// TestResetOrchestrators 验证 resetOrchestrators 清空全局映射
func TestResetOrchestrators(t *testing.T) {
	resetOrchestrators()

	// 手动存入
	orch := dreaming.NewDreamingOrchestrator(
		func(ctx context.Context) error { return nil },
		999999*time.Second,
		dreaming.WithName("dreaming-test"),
	)
	orchestratorsMu.Lock()
	orchestrators["test"] = orch
	orchestratorsMu.Unlock()

	if GetDreamingOrchestrator("test") == nil {
		t.Error("存入后应可获取")
	}

	resetOrchestrators()

	if GetDreamingOrchestrator("test") != nil {
		t.Error("重置后应返回 nil")
	}
}

// TestStartDreaming_并发安全 验证并发启动/停止不会 panic
func TestStartDreaming_并发安全(t *testing.T) {
	resetOrchestrators()

	tmpDir := t.TempDir()
	sessionsDir := filepath.Join(tmpDir, "sessions")
	outputDir := filepath.Join(tmpDir, "memory")
	os.MkdirAll(sessionsDir, 0o755)
	os.MkdirAll(outputDir, 0o755)

	envKey := "DREAMING_AGENT_ENABLED"
	os.Setenv(envKey, "true")
	defer os.Unsetenv(envKey)
	os.Setenv("DREAMING_INTERVAL", "999999")
	defer os.Unsetenv("DREAMING_INTERVAL")

	done := make(chan struct{})

	// 并发启动
	go func() {
		defer func() { done <- struct{}{} }()
		ctx := context.Background()
		for i := 0; i < 5; i++ {
			StartDreaming(ctx, sessionsDir, outputDir, "agent", "zh", nil)
			time.Sleep(10 * time.Millisecond)
		}
	}()

	// 并发停止
	go func() {
		defer func() { done <- struct{}{} }()
		ctx := context.Background()
		for i := 0; i < 5; i++ {
			StopDreaming(ctx, "agent")
			time.Sleep(10 * time.Millisecond)
		}
	}()

	<-done
	<-done

	// 不应 panic
	ctx := context.Background()
	StopDreaming(ctx, "agent")
}
