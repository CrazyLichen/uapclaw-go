package resources_manager

import (
	"testing"

	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
)

// ──────────────────────────── 结构体 ────────────────────────────

// mockSysOperation 测试用 SysOperation 桩实现
type mockSysOperation struct {
	sysop.BaseSysOperation
	isolationKeyTemplate string
}

// IsolationKeyTemplate 返回隔离键模板
func (m *mockSysOperation) IsolationKeyTemplate() string {
	return m.isolationKeyTemplate
}

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestSysOperationMgr_创建不返回nil 测试 NewSysOperationMgr 不返回 nil
func TestSysOperationMgr_创建不返回nil(t *testing.T) {
	mgr := NewSysOperationMgr()
	if mgr == nil {
		t.Error("NewSysOperationMgr 不应返回 nil")
	}
}

// TestSysOperationMgr_AddSysOperation_正常 测试正常添加系统操作
func TestSysOperationMgr_AddSysOperation_正常(t *testing.T) {
	mgr := NewSysOperationMgr()
	instance := &mockSysOperation{}
	err := mgr.AddSysOperation("test-op", instance)
	if err != nil {
		t.Errorf("AddSysOperation 不应返回错误，实际: %v", err)
	}
}

// TestSysOperationMgr_AddSysOperation_空ID 测试空 ID 返回错误
func TestSysOperationMgr_AddSysOperation_空ID(t *testing.T) {
	mgr := NewSysOperationMgr()
	instance := &mockSysOperation{}
	err := mgr.AddSysOperation("", instance)
	if err == nil {
		t.Error("空 ID 应返回错误")
	}
}

// TestSysOperationMgr_AddSysOperation_空实例 测试空实例返回错误
func TestSysOperationMgr_AddSysOperation_空实例(t *testing.T) {
	mgr := NewSysOperationMgr()
	err := mgr.AddSysOperation("test-op", nil)
	if err == nil {
		t.Error("空实例应返回错误")
	}
}

// TestSysOperationMgr_AddSysOperation_重复ID 测试重复 ID 返回错误
func TestSysOperationMgr_AddSysOperation_重复ID(t *testing.T) {
	mgr := NewSysOperationMgr()
	instance := &mockSysOperation{}
	_ = mgr.AddSysOperation("test-op", instance)
	err := mgr.AddSysOperation("test-op", instance)
	if err == nil {
		t.Error("重复 ID 应返回错误")
	}
}

// TestSysOperationMgr_AddSysOperation_隔离键冲突 测试隔离键模板冲突返回错误
func TestSysOperationMgr_AddSysOperation_隔离键冲突(t *testing.T) {
	mgr := NewSysOperationMgr()
	inst1 := &mockSysOperation{isolationKeyTemplate: "tpl-1"}
	inst2 := &mockSysOperation{isolationKeyTemplate: "tpl-1"}
	_ = mgr.AddSysOperation("op-1", inst1)
	err := mgr.AddSysOperation("op-2", inst2)
	if err == nil {
		t.Error("隔离键模板冲突应返回错误")
	}
}

// TestSysOperationMgr_GetSysOperation_正常 测试正常获取系统操作
func TestSysOperationMgr_GetSysOperation_正常(t *testing.T) {
	mgr := NewSysOperationMgr()
	instance := &mockSysOperation{}
	_ = mgr.AddSysOperation("test-op", instance)
	result, err := mgr.GetSysOperation("test-op")
	if err != nil {
		t.Errorf("GetSysOperation 不应返回错误，实际: %v", err)
	}
	if result != instance {
		t.Error("GetSysOperation 应返回相同实例")
	}
}

// TestSysOperationMgr_GetSysOperation_不存在 测试获取不存在的系统操作
func TestSysOperationMgr_GetSysOperation_不存在(t *testing.T) {
	mgr := NewSysOperationMgr()
	_, err := mgr.GetSysOperation("nonexistent")
	if err == nil {
		t.Error("获取不存在的系统操作应返回错误")
	}
}

// TestSysOperationMgr_RemoveSysOperation_正常 测试正常移除系统操作
func TestSysOperationMgr_RemoveSysOperation_正常(t *testing.T) {
	mgr := NewSysOperationMgr()
	instance := &mockSysOperation{isolationKeyTemplate: "tpl-1"}
	_ = mgr.AddSysOperation("test-op", instance)
	removed, err := mgr.RemoveSysOperation("test-op")
	if err != nil {
		t.Errorf("RemoveSysOperation 不应返回错误，实际: %v", err)
	}
	if removed != instance {
		t.Error("RemoveSysOperation 应返回被移除的实例")
	}
}

// TestSysOperationMgr_RemoveSysOperation_不存在 测试移除不存在的系统操作
func TestSysOperationMgr_RemoveSysOperation_不存在(t *testing.T) {
	mgr := NewSysOperationMgr()
	_, err := mgr.RemoveSysOperation("nonexistent")
	if err == nil {
		t.Error("移除不存在的系统操作应返回错误")
	}
}

// ──────────────────────────── 非导出函数测试 ────────────────────────────
