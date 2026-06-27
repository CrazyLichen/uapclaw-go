package resources_manager

import (
	"testing"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestAgentTeamMgr_创建不返回nil 测试 NewAgentTeamMgr 不返回 nil
func TestAgentTeamMgr_创建不返回nil(t *testing.T) {
	mgr := NewAgentTeamMgr()
	if mgr == nil {
		t.Error("NewAgentTeamMgr 不应返回 nil")
	}
}

// ──────────────────────────── 非导出函数测试 ────────────────────────────
