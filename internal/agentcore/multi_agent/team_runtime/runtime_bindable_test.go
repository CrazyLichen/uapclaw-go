package team_runtime

import (
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestRuntimeBindable_接口满足 测试 RuntimeBindable 接口
func TestRuntimeBindable_接口满足(t *testing.T) {
	// CommunicableAgent 应实现 RuntimeBindable
	var _ RuntimeBindable = (*CommunicableAgent)(nil)

	// mockRuntimeBindable 也应实现 RuntimeBindable
	var _ RuntimeBindable = (*mockRuntimeBindable)(nil)
}

// TestRuntimeBindable_绑定 测试 BindRuntime 调用
func TestRuntimeBindable_绑定(t *testing.T) {
	bindable := &mockRuntimeBindable{}
	runtime := &TeamRuntime{teamID: "test-team"}

	bindable.BindRuntime(runtime, "agent-1")

	if !bindable.bound {
		t.Error("BindRuntime 后 bound 应为 true")
	}
	if bindable.runtime != runtime {
		t.Error("runtime 未正确设置")
	}
	if bindable.agentID != "agent-1" {
		t.Errorf("agentID = %q, want %q", bindable.agentID, "agent-1")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
