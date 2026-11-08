package runtime

import (
	"context"
	"testing"

	swruntime "github.com/uapclaw/uapclaw-go/internal/swarm/runtime"
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
	uc := &UapClawSwarm{sessionManager: swruntime.NewSessionManager()}
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
	manager := swruntime.NewSessionManager()
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
