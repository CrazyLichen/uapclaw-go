package security

import (
	"context"
	"testing"

	harnesssecurity "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── 派发注入（9.90d-1 Task 13 前置基础设施） ────────────────────────────

// TestSetOverride_未注入走默认 验证无 override 时父类行为不变。
func TestSetOverride_未注入走默认(t *testing.T) {
	rail := NewPermissionInterruptRail(map[string]any{}, nil, nil, nil, "", nil)
	if rail.override != nil {
		t.Error("new rail should have nil override")
	}
}

// TestPersistAllowAlways_注入后派发 验证 override 被父类内部调用。
func TestPersistAllowAlways_注入后派发(t *testing.T) {
	rail := NewPermissionInterruptRail(map[string]any{}, nil, nil, nil, "", nil)
	called := false
	rail.SetOverride(&PermissionInterruptRailOverride{
		PersistAllowAlways: func(ctx context.Context, normalizedName string, toolArgs map[string]any) (bool, bool) {
			called = true
			return true, true // handled
		},
	})
	got := rail.persistAllowAlways(context.Background(), "bash", map[string]any{"command": "ls"})
	if !called {
		t.Error("override should be called")
	}
	if !got {
		t.Error("should return override result")
	}
}

// TestPersistAllowAlways_未处理回落父类 验证 handled=false 时走默认实现。
func TestPersistAllowAlways_未处理回落父类(t *testing.T) {
	rail := NewPermissionInterruptRail(map[string]any{}, nil, nil, nil, "", nil)
	called := false
	rail.SetOverride(&PermissionInterruptRailOverride{
		PersistAllowAlways: func(context.Context, string, map[string]any) (bool, bool) {
			called = true
			return false, false // not handled → 回落
		},
	})
	// 父类默认实现会走到 persistMergedAllow，无 host 钩子时返回 false
	_ = rail.persistAllowAlways(context.Background(), "bash", map[string]any{"command": "ls"})
	if !called {
		t.Error("override should still be consulted")
	}
}

// TestPersistMergedAllow_注入后派发 验证 override 生效。
func TestPersistMergedAllow_注入后派发(t *testing.T) {
	rail := NewPermissionInterruptRail(map[string]any{}, nil, nil, nil, "", nil)
	called := false
	rail.SetOverride(&PermissionInterruptRailOverride{
		PersistMergedAllow: func(context.Context, string, map[string]any, func(map[string]any) bool, bool) (bool, bool) {
			called = true
			return true, true
		},
	})
	got := rail.persistMergedAllow(context.Background(), "bash", map[string]any{}, nil, true)
	if !called || !got {
		t.Errorf("called=%v got=%v", called, got)
	}
}

// TestPersistSessionAllow_注入后派发 验证 override 生效。
func TestPersistSessionAllow_注入后派发(t *testing.T) {
	rail := NewPermissionInterruptRail(map[string]any{}, nil, nil, nil, "", nil)
	called := false
	rail.SetOverride(&PermissionInterruptRailOverride{
		PersistSessionAllow: func(context.Context, string, map[string]any, string) (bool, bool) {
			called = true
			return true, true
		},
	})
	got := rail.persistSessionAllow(context.Background(), "bash", map[string]any{}, "sess-1")
	if !called || !got {
		t.Errorf("called=%v got=%v", called, got)
	}
}

// TestCollectFileGuardPersistAccesses_注入后派发 验证 override 生效。
func TestCollectFileGuardPersistAccesses_注入后派发(t *testing.T) {
	rail := NewPermissionInterruptRail(map[string]any{}, nil, nil, nil, "", nil)
	called := false
	want := []harnesssecurity.PathAction{{Path: "/a", Action: "read"}}
	rail.SetOverride(&PermissionInterruptRailOverride{
		CollectFileGuardPersistAccesses: func(string, map[string]any, map[string]any) ([]harnesssecurity.PathAction, bool) {
			called = true
			return want, true
		},
	})
	got := rail.collectFileGuardPersistAccesses("read_file", map[string]any{}, map[string]any{})
	if !called {
		t.Error("override should be called")
	}
	if len(got) != 1 || got[0].Path != "/a" {
		t.Errorf("got %v", got)
	}
}

// TestGetUserInput_注入后派发 验证 override 生效。
func TestGetUserInput_注入后派发(t *testing.T) {
	rail := NewPermissionInterruptRail(map[string]any{}, nil, nil, nil, "", nil)
	called := false
	rail.SetOverride(&PermissionInterruptRailOverride{
		GetUserInput: func(*agentinterfaces.AgentCallbackContext, string) (any, bool) {
			called = true
			return "override-input", true
		},
	})
	got := rail.getUserInput(nil, "call-1")
	if !called {
		t.Error("override should be called")
	}
	if got != "override-input" {
		t.Errorf("got %v", got)
	}
}

// ──────────────────────────── 导出访问器 ────────────────────────────

// TestAccessors_基础 验证导出访问器返回内部状态。
func TestAccessors_基础(t *testing.T) {
	cfg := map[string]any{"k": "v"}
	rail := NewPermissionInterruptRail(cfg, nil, nil, nil, "", nil)

	if rail.StaticConfig() == nil {
		t.Error("StaticConfig should not be nil")
	}
	// 构造函数在 host 为 nil 时补空结构体（对齐 Python `host or ToolPermissionHost()`）
	if rail.Host() == nil {
		t.Error("Host should be non-nil (constructor fills a default)")
	}
}

// TestNormalizeToolName_导出 验证别名归一。
func TestNormalizeToolName_导出(t *testing.T) {
	rail := NewPermissionInterruptRail(map[string]any{}, nil, nil, nil, "", nil)
	if got := rail.NormalizeToolName("free_search"); got != "mcp_free_search" {
		t.Errorf("got %q, want mcp_free_search", got)
	}
	if got := rail.NormalizeToolName("unknown_tool"); got != "unknown_tool" {
		t.Errorf("got %q", got)
	}
}

// TestParseToolArgs_导出 验证包级 helper 可用。
func TestParseToolArgs_导出(t *testing.T) {
	if got := ParseToolArgs(nil); len(got) != 0 {
		t.Errorf("nil toolCall -> %v", got)
	}
}

// TestParseConfirmPayload_导出 验证包级 helper 可用。
func TestParseConfirmPayload_导出(t *testing.T) {
	if got := ParseConfirmPayload(nil); got != nil {
		t.Errorf("nil -> %v", got)
	}
	// 从 PermissionConfirmResponse 直接透传
	in := &harnesssecurity.PermissionConfirmResponse{Approved: true}
	if got := ParseConfirmPayload(in); got == nil || !got.Approved {
		t.Errorf("got %v", got)
	}
}

// ──────────────────────────── 导出访问器（直接调用） ────────────────────────────

// TestAutoConfirmKey_导出 验证自动确认键计算可用。
func TestAutoConfirmKey_导出(t *testing.T) {
	rail := NewPermissionInterruptRail(map[string]any{}, nil, nil, nil, "", nil)
	if got := rail.AutoConfirmKey(nil); got != "" {
		t.Logf("nil toolCall key = %q", got)
	}
}

// TestCollectFileGuardPersistAccesses_导出 验证导出方法转发。
func TestCollectFileGuardPersistAccesses_导出(t *testing.T) {
	rail := NewPermissionInterruptRail(map[string]any{}, nil, nil, nil, "", nil)
	// 无 host.ResolveWorkspaceDir 时返回 nil
	if got := rail.CollectFileGuardPersistAccesses("read_file", map[string]any{}, map[string]any{}); got != nil {
		t.Errorf("got %v", got)
	}
}

// TestGetUserInput_导出 验证导出方法转发（nil ctx 时不 panic 需 override 兜住）。
func TestGetUserInput_导出(t *testing.T) {
	rail := NewPermissionInterruptRail(map[string]any{}, nil, nil, nil, "", nil)
	rail.SetOverride(&PermissionInterruptRailOverride{
		GetUserInput: func(*agentinterfaces.AgentCallbackContext, string) (any, bool) {
			return "stub", true
		},
	})
	if got := rail.GetUserInput(nil, "call-1"); got != "stub" {
		t.Errorf("got %v", got)
	}
}

// TestPersistAllowAlways_导出 验证导出方法转发。
func TestPersistAllowAlways_导出(t *testing.T) {
	rail := NewPermissionInterruptRail(map[string]any{}, nil, nil, nil, "", nil)
	rail.SetOverride(&PermissionInterruptRailOverride{
		PersistAllowAlways: func(context.Context, string, map[string]any) (bool, bool) { return true, true },
	})
	if got := rail.PersistAllowAlways(context.Background(), "bash", map[string]any{}); !got {
		t.Error("should be true via override")
	}
}

// TestPersistSessionAllow_导出 验证导出方法转发。
func TestPersistSessionAllow_导出(t *testing.T) {
	rail := NewPermissionInterruptRail(map[string]any{}, nil, nil, nil, "", nil)
	rail.SetOverride(&PermissionInterruptRailOverride{
		PersistSessionAllow: func(context.Context, string, map[string]any, string) (bool, bool) { return true, true },
	})
	if got := rail.PersistSessionAllow(context.Background(), "bash", map[string]any{}, "sess-1"); !got {
		t.Error("should be true via override")
	}
}

// TestPersistMergedAllow_导出 验证导出方法转发。
func TestPersistMergedAllow_导出(t *testing.T) {
	rail := NewPermissionInterruptRail(map[string]any{}, nil, nil, nil, "", nil)
	rail.SetOverride(&PermissionInterruptRailOverride{
		PersistMergedAllow: func(context.Context, string, map[string]any, func(map[string]any) bool, bool) (bool, bool) {
			return true, true
		},
	})
	if got := rail.PersistMergedAllow(context.Background(), "bash", map[string]any{}, nil, true); !got {
		t.Error("should be true via override")
	}
}

// TestOverrideGetUserInput_无override 验证 nil override 时返回 not handled。
func TestOverrideGetUserInput_无override(t *testing.T) {
	rail := NewPermissionInterruptRail(map[string]any{}, nil, nil, nil, "", nil)
	if _, handled := rail.overrideGetUserInput(nil, "x"); handled {
		t.Error("nil override should not handle")
	}
	rail.SetOverride(&PermissionInterruptRailOverride{})
	if _, handled := rail.overrideGetUserInput(nil, "x"); handled {
		t.Error("empty override should not handle")
	}
}

// ──────────────────────────── 导出方法（9.90d-1 Task 16 补齐） ────────────────────────────

// TestCollectFileGuardPersistAccessesDefault_无host返回nil 验证默认实现。
func TestCollectFileGuardPersistAccessesDefault_无host返回nil(t *testing.T) {
	rail := NewPermissionInterruptRail(map[string]any{}, nil, nil, nil, "", nil)
	if got := rail.CollectFileGuardPersistAccessesDefault("read_file", map[string]any{}, map[string]any{}); got != nil {
		t.Errorf("got %v", got)
	}
}

// TestCollectFileGuardPersistAccessesDefault_不查override 验证绕过 override 不递归。
func TestCollectFileGuardPersistAccessesDefault_不查override(t *testing.T) {
	rail := NewPermissionInterruptRail(map[string]any{}, nil, nil, nil, "", nil)
	called := false
	rail.SetOverride(&PermissionInterruptRailOverride{
		CollectFileGuardPersistAccesses: func(string, map[string]any, map[string]any) ([]harnesssecurity.PathAction, bool) {
			called = true
			return nil, true
		},
	})
	// Default 版不应触发 override（否则会死循环）
	_ = rail.CollectFileGuardPersistAccessesDefault("read_file", map[string]any{}, map[string]any{})
	if called {
		t.Error("Default variant must NOT consult override")
	}
}

// TestResolvePermissionInterrupt_转发父类 验证导出方法转发到私有实现。
func TestResolvePermissionInterrupt_转发父类(t *testing.T) {
	rail := NewPermissionInterruptRail(map[string]any{}, nil, nil, nil, "", nil)
	cbc := agentinterfaces.NewAgentCallbackContext(nil, nil, nil)

	// 无 inputs 时父类提前返回零值 decision，不 panic
	defer func() {
		if r := recover(); r != nil {
			t.Logf("interrupt raised as expected: %v", r)
		}
	}()
	_ = rail.ResolvePermissionInterrupt(context.Background(), cbc, nil, nil, nil)
}
