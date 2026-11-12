package runtime

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// TestAgentManager_CleanupSessionRuntime_空会话ID 返回 false。
func TestAgentManager_CleanupSessionRuntime_空会话ID(t *testing.T) {
	manager := NewAgentManager()
	cleaned, err := manager.CleanupSessionRuntime(context.Background(), "ch-1", "")
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if cleaned {
		t.Error("空会话 ID 应返回 false")
	}
}

// TestAgentManager_CleanupSessionRuntime_空白会话ID 去空白后为空也返回 false。
func TestAgentManager_CleanupSessionRuntime_空白会话ID(t *testing.T) {
	manager := NewAgentManager()
	cleaned, err := manager.CleanupSessionRuntime(context.Background(), "ch-1", "   ")
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if cleaned {
		t.Error("空白会话 ID 应返回 false")
	}
}

// TestAgentManager_CleanupSessionRuntime_无缓存agent 返回 false。
func TestAgentManager_CleanupSessionRuntime_无缓存agent(t *testing.T) {
	manager := NewAgentManager()
	cleaned, err := manager.CleanupSessionRuntime(context.Background(), "ch-1", "s1")
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if cleaned {
		t.Error("无缓存 agent 应返回 false")
	}
}

// TestFacadeHasSessionRuntime_无会话运行时 门面初始为 false。
func TestFacadeHasSessionRuntime_无会话运行时(t *testing.T) {
	uc := &UapClawSwarm{}
	if uc.HasSessionRuntime("s1") {
		t.Error("门面初始不应持有会话运行时")
	}
}

// TestFacadeHasSessionRuntime_队列有运行时 会话队列存在即为真。
func TestFacadeHasSessionRuntime_队列有运行时(t *testing.T) {
	uc := &UapClawSwarm{sessionManager: session.NewSessionManager()}
	sid := "s1"
	_, _ = uc.sessionManager.SubmitAndWait(context.Background(), sid,
		func(context.Context) (any, error) { return nil, nil })
	if !uc.HasSessionRuntime(sid) {
		t.Error("门面队列保留运行时应为真")
	}
	uc.sessionManager.CloseAllSessions(context.Background())
}

// TestFacadeCleanupSessionRuntime_关门面队列 清理后队列状态消失。
func TestFacadeCleanupSessionRuntime_关门面队列(t *testing.T) {
	manager := session.NewSessionManager()
	uc := &UapClawSwarm{sessionManager: manager}
	sid := "s1"
	_, _ = manager.SubmitAndWait(context.Background(), sid,
		func(context.Context) (any, error) { return nil, nil })

	if err := uc.CleanupSessionRuntime(context.Background(), sid); err != nil {
		t.Fatalf("清理不应报错: %v", err)
	}
	if manager.HasSessionRuntime(&sid) {
		t.Error("清理后门面队列不应再有该会话")
	}
}

// TestFacadeCleanupSessionRuntime_无adapter不panic adapter 为 nil 时安全。
func TestFacadeCleanupSessionRuntime_无adapter不panic(t *testing.T) {
	uc := &UapClawSwarm{}
	if err := uc.CleanupSessionRuntime(context.Background(), "s1"); err != nil {
		t.Errorf("无 adapter 时不应报错: %v", err)
	}
}

// TestAgentManager_CleanupSessionRuntime_有缓存agent且清理成功 返回 true。
func TestAgentManager_CleanupSessionRuntime_有缓存agent且清理成功(t *testing.T) {
	manager := NewAgentManager()
	sid := "s1"
	uc := &UapClawSwarm{sessionManager: session.NewSessionManager()}
	// 先在门面队列里留下该会话的运行时，使 HasSessionRuntime 为真
	_, _ = uc.sessionManager.SubmitAndWait(context.Background(), sid,
		func(context.Context) (any, error) { return nil, nil })
	manager.PreSetAgent("ch-1", "cache-1", uc, "agent", "normal", "")

	cleaned, err := manager.CleanupSessionRuntime(context.Background(), "ch-1", sid)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if !cleaned {
		t.Error("有缓存 agent 且清理成功应返回 true")
	}
	if uc.sessionManager.HasSessionRuntime(&sid) {
		t.Error("清理后门面队列不应再有该会话")
	}
}

// TestAgentManager_CleanupSessionRuntime_无运行时跳过 有 agent 但会话无运行时。
func TestAgentManager_CleanupSessionRuntime_无运行时跳过(t *testing.T) {
	manager := NewAgentManager()
	uc := &UapClawSwarm{}
	manager.PreSetAgent("ch-1", "cache-1", uc, "agent", "normal", "")

	cleaned, err := manager.CleanupSessionRuntime(context.Background(), "ch-1", "s-none")
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if cleaned {
		t.Error("会话无运行时时应返回 false")
	}
}

// TestFacadeCleanupSessionRuntime_清adapter侧 adapter 缓存被清理。
func TestFacadeCleanupSessionRuntime_清adapter侧(t *testing.T) {
	fa := &fakeAdapter{}
	uc := &UapClawSwarm{adapter: fa}
	if err := uc.CleanupSessionRuntime(context.Background(), "s1"); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	// fakeAdapter 的 CleanupSessionAdapter 恒返回 false，走到 Debug 分支即可
	if !fa.cleanupCalled {
		t.Error("应调用 adapter 侧清理")
	}
}

// TestFacadeHasSessionRuntime_adapter侧为真 adapter 报告有运行时。
func TestFacadeHasSessionRuntime_adapter侧为真(t *testing.T) {
	fa := &fakeAdapter{hasRuntime: true}
	uc := &UapClawSwarm{adapter: fa}
	if !uc.HasSessionRuntime("s1") {
		t.Error("adapter 侧有运行时应返回真")
	}
}
