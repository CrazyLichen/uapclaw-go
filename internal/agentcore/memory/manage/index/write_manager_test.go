package index

import (
	"context"
	"errors"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/index"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/mem_model"
)

// ──────────────────────────── 结构体 ────────────────────────────

// stubWriteManagerManager 用于测试 WriteManager 路由的 mock Manager
type stubWriteManagerManager struct {
	// memTypeStr 管理器类型
	memTypeStr string
	// addCalled AddMemories 调用次数
	addCalled int
	// updateCalled Update 调用次数
	updateCalled int
	// deleteByUserIDCalled DeleteByUserID 调用次数
	deleteByUserIDCalled int
	// addResult AddMemories 返回值
	addResult []mem_model.MemoryUnit
	// addErr AddMemories 返回错误
	addErr error
	// updateErr Update 返回错误
	updateErr error
	// deleteErr Delete 返回错误
	deleteErr error
	// deleteByUserIDErr DeleteByUserID 返回错误
	deleteByUserIDErr error
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewWriteManager 测试构造函数
func TestNewWriteManager(t *testing.T) {
	managers := map[string]BaseMemoryManager{
		"variable": &stubWriteManagerManager{memTypeStr: "variable"},
	}
	fakeIdx := newFakeMemoryIndex()
	wm := NewWriteManager(managers, fakeIdx)
	if wm == nil {
		t.Fatal("NewWriteManager 返回 nil")
	}
}

// TestWriteManager_AddMemories_去重 测试三种 Fragment 类型共享 Manager 只调用一次
func TestWriteManager_AddMemories_去重(t *testing.T) {
	// 创建共享的 stub Manager
	fragMgr := &stubWriteManagerManager{memTypeStr: "user_profile", addResult: []mem_model.MemoryUnit{}}

	managers := map[string]BaseMemoryManager{
		"user_profile":    fragMgr,
		"semantic_memory": fragMgr, // 共享同一实例
		"episodic_memory": fragMgr, // 共享同一实例
		"variable":        &stubWriteManagerManager{memTypeStr: "variable", addResult: []mem_model.MemoryUnit{}},
	}

	wm := NewWriteManager(managers, nil)
	memories := map[string][]mem_model.MemoryUnit{
		"user_profile": {&mem_model.FragmentMemoryUnit{}},
	}

	_, err := wm.AddMemories(context.Background(), "user1", "scope1", memories)
	if err != nil {
		t.Fatalf("AddMemories 返回错误: %v", err)
	}

	// 共享的 fragMgr 应该只被调用 1 次，variable 1 次 = 共 2 次
	totalCalled := fragMgr.addCalled
	for _, m := range managers {
		if stub, ok := m.(*stubWriteManagerManager); ok && stub != fragMgr {
			totalCalled += stub.addCalled
		}
	}
	if totalCalled != 2 {
		t.Errorf("期望 2 次 Manager 调用（frag 1 + variable 1），实际 %d 次", totalCalled)
	}
}

// TestWriteManager_AddMemories_空输入 测试空输入返回 nil
func TestWriteManager_AddMemories_空输入(t *testing.T) {
	wm := NewWriteManager(nil, nil)
	result, err := wm.AddMemories(context.Background(), "user1", "scope1", nil)
	if err != nil {
		t.Fatalf("AddMemories 返回错误: %v", err)
	}
	if result != nil {
		t.Errorf("期望 nil，实际 %v", result)
	}
}

// TestWriteManager_AddMemories_错误传播 测试 AddMemories 错误传播
func TestWriteManager_AddMemories_错误传播(t *testing.T) {
	expectedErr := errors.New("add failed")
	fragMgr := &stubWriteManagerManager{
		memTypeStr: "user_profile",
		addErr:     expectedErr,
	}

	managers := map[string]BaseMemoryManager{
		"user_profile": fragMgr,
	}

	wm := NewWriteManager(managers, nil)
	memories := map[string][]mem_model.MemoryUnit{
		"user_profile": {&mem_model.FragmentMemoryUnit{}},
	}

	_, err := wm.AddMemories(context.Background(), "user1", "scope1", memories)
	if err == nil {
		t.Fatal("期望返回错误，实际返回 nil")
	}
	if err.Error() != expectedErr.Error() {
		t.Errorf("错误消息不匹配: got %q, want %q", err.Error(), expectedErr.Error())
	}
}

// TestWriteManager_UpdateMemByID_路由 测试按 mem_type 路由到正确 Manager
func TestWriteManager_UpdateMemByID_路由(t *testing.T) {
	varMgr := &stubWriteManagerManager{memTypeStr: "variable"}
	fakeIdx := newFakeMemoryIndex()
	fakeIdx.AddMemories(context.Background(), "user1", "scope1", []*index.MemoryDoc{
		{ID: "mem1", Type: "variable", Text: "test"},
	})

	managers := map[string]BaseMemoryManager{
		"variable": varMgr,
	}

	wm := NewWriteManager(managers, fakeIdx)
	err := wm.UpdateMemByID(context.Background(), "user1", "scope1", "mem1", "new content")
	if err != nil {
		t.Fatalf("UpdateMemByID 返回错误: %v", err)
	}
	if varMgr.updateCalled != 1 {
		t.Errorf("期望 variable Manager Update 被调用 1 次，实际 %d 次", varMgr.updateCalled)
	}
}

// TestWriteManager_UpdateMemByID_类型不存在 测试记忆不存在时跳过
func TestWriteManager_UpdateMemByID_类型不存在(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()

	managers := map[string]BaseMemoryManager{
		"variable": &stubWriteManagerManager{memTypeStr: "variable"},
	}

	wm := NewWriteManager(managers, fakeIdx)
	err := wm.UpdateMemByID(context.Background(), "user1", "scope1", "nonexistent", "new content")
	if err != nil {
		t.Fatalf("期望返回 nil（跳过），实际返回错误: %v", err)
	}
}

// TestWriteManager_DeleteMemByID_路由 测试删除路由
func TestWriteManager_DeleteMemByID_路由(t *testing.T) {
	varMgr := &stubWriteManagerManager{memTypeStr: "variable"}
	fakeIdx := newFakeMemoryIndex()
	fakeIdx.AddMemories(context.Background(), "user1", "scope1", []*index.MemoryDoc{
		{ID: "mem1", Type: "variable", Text: "test"},
	})

	managers := map[string]BaseMemoryManager{
		"variable": varMgr,
	}

	wm := NewWriteManager(managers, fakeIdx)
	err := wm.DeleteMemByID(context.Background(), "user1", "scope1", "mem1")
	if err != nil {
		t.Fatalf("DeleteMemByID 返回错误: %v", err)
	}
}

// TestWriteManager_DeleteMemByUserID_遍历 测试遍历所有 Manager（去重）
func TestWriteManager_DeleteMemByUserID_遍历(t *testing.T) {
	fragMgr := &stubWriteManagerManager{memTypeStr: "user_profile"}
	varMgr := &stubWriteManagerManager{memTypeStr: "variable"}

	// 3 个 Fragment 类型共享 1 个 Manager + 1 个 Variable Manager
	managers := map[string]BaseMemoryManager{
		"user_profile":    fragMgr,
		"semantic_memory": fragMgr,
		"episodic_memory": fragMgr,
		"variable":        varMgr,
	}

	wm := NewWriteManager(managers, nil)
	err := wm.DeleteMemByUserID(context.Background(), "user1", "scope1")
	if err != nil {
		t.Fatalf("DeleteMemByUserID 返回错误: %v", err)
	}
	// 共享的 fragMgr 应该只被调用 1 次，variable 1 次
	if fragMgr.deleteByUserIDCalled != 1 {
		t.Errorf("期望 fragMgr DeleteByUserID 被调用 1 次，实际 %d 次", fragMgr.deleteByUserIDCalled)
	}
	if varMgr.deleteByUserIDCalled != 1 {
		t.Errorf("期望 varMgr DeleteByUserID 被调用 1 次，实际 %d 次", varMgr.deleteByUserIDCalled)
	}
}

// TestWriteManager_DeleteMemByUserID_错误传播 测试错误传播
func TestWriteManager_DeleteMemByUserID_错误传播(t *testing.T) {
	expectedErr := errors.New("delete failed")
	fragMgr := &stubWriteManagerManager{
		memTypeStr:         "user_profile",
		deleteByUserIDErr:  expectedErr,
	}

	managers := map[string]BaseMemoryManager{
		"user_profile": fragMgr,
	}

	wm := NewWriteManager(managers, nil)
	err := wm.DeleteMemByUserID(context.Background(), "user1", "scope1")
	if err == nil {
		t.Fatal("期望返回错误，实际返回 nil")
	}
}

// TestWriteManager_getMemTypeFromIndex_不支持类型 测试不支持的记忆类型
func TestWriteManager_getMemTypeFromIndex_不支持类型(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()
	fakeIdx.AddMemories(context.Background(), "user1", "scope1", []*index.MemoryDoc{
		{ID: "mem1", Type: "unknown_type", Text: "test"},
	})

	managers := map[string]BaseMemoryManager{
		"variable": &stubWriteManagerManager{memTypeStr: "variable"},
	}

	wm := NewWriteManager(managers, fakeIdx)
	memType, err := wm.getMemTypeFromIndex(context.Background(), "user1", "scope1", "mem1")
	if err != nil {
		t.Fatalf("期望返回 nil 错误，实际: %v", err)
	}
	if memType != "" {
		t.Errorf("期望空字符串（不支持类型），实际: %q", memType)
	}
}

// TestWriteManager_getMemTypeFromIndex_正常 测试正常路由查找
func TestWriteManager_getMemTypeFromIndex_正常(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()
	fakeIdx.AddMemories(context.Background(), "user1", "scope1", []*index.MemoryDoc{
		{ID: "mem1", Type: "variable", Text: "test"},
	})

	managers := map[string]BaseMemoryManager{
		"variable": &stubWriteManagerManager{memTypeStr: "variable"},
	}

	wm := NewWriteManager(managers, fakeIdx)
	memType, err := wm.getMemTypeFromIndex(context.Background(), "user1", "scope1", "mem1")
	if err != nil {
		t.Fatalf("期望返回 nil 错误，实际: %v", err)
	}
	if memType != "variable" {
		t.Errorf("期望 variable，实际: %q", memType)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// AddMemories 实现 BaseMemoryManager 接口
func (s *stubWriteManagerManager) AddMemories(ctx context.Context, userID string, scopeID string,
	memories map[string][]mem_model.MemoryUnit, llmModel ...*llm.Model) ([]mem_model.MemoryUnit, error) {
	s.addCalled++
	return s.addResult, s.addErr
}

// Update 实现 BaseMemoryManager 接口
func (s *stubWriteManagerManager) Update(ctx context.Context, userID string, scopeID string, memID string, newMemory string) (bool, error) {
	s.updateCalled++
	return true, s.updateErr
}

// Search 实现 BaseMemoryManager 接口
func (s *stubWriteManagerManager) Search(ctx context.Context, userID string, scopeID string, query string, topK int, memTypes []string) ([]*index.MemorySearchResult, error) {
	return nil, nil
}

// Get 实现 BaseMemoryManager 接口
func (s *stubWriteManagerManager) Get(ctx context.Context, userID string, scopeID string, memID string) (*index.MemoryDoc, error) {
	return nil, nil
}

// Delete 实现 BaseMemoryManager 接口
func (s *stubWriteManagerManager) Delete(ctx context.Context, userID string, scopeID string, memID string) (bool, error) {
	return true, s.deleteErr
}

// DeleteByUserID 实现 BaseMemoryManager 接口
func (s *stubWriteManagerManager) DeleteByUserID(ctx context.Context, userID string, scopeID string) (bool, error) {
	s.deleteByUserIDCalled++
	return true, s.deleteByUserIDErr
}

// getMemType 实现 getMemType 接口
func (s *stubWriteManagerManager) getMemType() string {
	return s.memTypeStr
}
