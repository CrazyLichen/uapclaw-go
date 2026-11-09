package security

import (
	"testing"
)

// ──────────────────────────── 引擎访问器（9.90d-1 Task 13 基础设施） ────────────────────────────

// TestPermissionEngine_FileGuard往返 验证 FileGuard / SetFileGuard 读写。
func TestPermissionEngine_FileGuard往返(t *testing.T) {
	engine := NewPermissionEngine(map[string]any{}, nil, "", "")
	original := engine.FileGuard()

	replacement := &stubFileGuard{enabled: true, mode: "native"}
	engine.SetFileGuard(replacement)
	if engine.FileGuard() != replacement {
		t.Error("SetFileGuard should replace the evaluator")
	}

	engine.SetFileGuard(original)
	if engine.FileGuard() != original {
		t.Error("should be able to restore original")
	}
}

// TestPermissionEngine_WorkspaceRoot往返 验证 WorkspaceRoot / SetWorkspaceRoot 读写。
func TestPermissionEngine_WorkspaceRoot往返(t *testing.T) {
	engine := NewPermissionEngine(map[string]any{}, nil, "", "")
	if got := engine.WorkspaceRoot(); got != "" {
		t.Errorf("initial WorkspaceRoot = %q, want empty", got)
	}

	engine.SetWorkspaceRoot("/tmp/ws")
	if got := engine.WorkspaceRoot(); got != "/tmp/ws" {
		t.Errorf("WorkspaceRoot = %q", got)
	}
}

// TestPermissionEngine_RebuildFileGuard 验证重建走已注册的工厂。
func TestPermissionEngine_RebuildFileGuard(t *testing.T) {
	// 注册一个返回固定实例的工厂，让重建有可观测结果
	sentinel := &stubFileGuard{enabled: true, mode: "rebuilt"}
	RegisterFileGuardFactory(func(map[string]any, string, []string) FileGuardEvaluator {
		return sentinel
	})
	t.Cleanup(func() { RegisterFileGuardFactory(nil) })

	engine := NewPermissionEngine(map[string]any{}, nil, "", "/tmp/ws")
	engine.RebuildFileGuard()
	if engine.FileGuard() != sentinel {
		t.Error("RebuildFileGuard should install the factory-produced evaluator")
	}
}

// TestPermissionEngine_RebuildFileGuard_无工厂 验证未注册工厂时置 nil 且不 panic。
func TestPermissionEngine_RebuildFileGuard_无工厂(t *testing.T) {
	RegisterFileGuardFactory(nil)
	engine := NewPermissionEngine(map[string]any{}, nil, "", "/tmp/ws")
	engine.RebuildFileGuard()
	if engine.FileGuard() != nil {
		t.Error("without factory should be nil")
	}
}

// stubFileGuard 测试用路径判定器。
type stubFileGuard struct {
	enabled bool
	mode    string
}

// Enabled 实现 FileGuardEvaluator
func (s *stubFileGuard) Enabled() bool { return s.enabled }

// Mode 实现 FileGuardEvaluator
func (s *stubFileGuard) Mode() string { return s.mode }

// Evaluate 实现 FileGuardEvaluator
func (s *stubFileGuard) Evaluate(string, map[string]any) *PermissionResult { return nil }

// CollectAskAccesses 实现 FileGuardEvaluator
func (s *stubFileGuard) CollectAskAccesses(string, map[string]any) []PathAction { return nil }
