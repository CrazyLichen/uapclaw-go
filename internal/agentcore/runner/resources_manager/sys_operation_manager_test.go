package resources_manager

import (
	"testing"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestSysOperationMgr_创建不返回nil 测试 NewSysOperationMgr 不返回 nil
func TestSysOperationMgr_创建不返回nil(t *testing.T) {
	mgr := NewSysOperationMgr()
	if mgr == nil {
		t.Error("NewSysOperationMgr 不应返回 nil")
	}
}

// ──────────────────────────── 非导出函数测试 ────────────────────────────
