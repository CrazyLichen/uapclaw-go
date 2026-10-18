package lite

import "testing"

// ──────────────────────────── 导出函数 ────────────────────────────

// TestIsClosed_Manager为nil 检查 Manager 为 nil 时 IsClosed 返回 true
func TestIsClosed_Manager为nil(t *testing.T) {
	base := &LiteMemoryToolContextBase{Manager: nil}
	if !base.IsClosed() {
		t.Error("Manager 为 nil 时 IsClosed 应返回 true")
	}
}

// TestIsClosed_Manager未关闭 检查 Manager 的 IsClosed 返回 false 时结果
func TestIsClosed_Manager未关闭(t *testing.T) {
	// 使用 memoryIndexManager 实例，默认 closed=false
	mgr := &memoryIndexManager{}
	base := &LiteMemoryToolContextBase{Manager: mgr}
	if base.IsClosed() {
		t.Error("Manager 未关闭时 IsClosed 应返回 false")
	}
}

// TestIsClosed_Manager已关闭 检查 Manager 的 IsClosed 返回 true 时结果
func TestIsClosed_Manager已关闭(t *testing.T) {
	mgr := &memoryIndexManager{closed: true}
	base := &LiteMemoryToolContextBase{Manager: mgr}
	if !base.IsClosed() {
		t.Error("Manager 已关闭时 IsClosed 应返回 true")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
