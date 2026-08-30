package search

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	storeindex "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/index"
	manageindex "github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/index"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/mem_model"
)

// ──────────────────────────── 结构体 ────────────────────────────

// stubSearchManager 用于测试 SearchManager 的 mock Manager
type stubSearchManager struct {
	// memTypeStr 管理器类型
	memTypeStr string
	// searchResult Search 返回值
	searchResult []*storeindex.MemorySearchResult
	// searchErr Search 返回错误
	searchErr error
	// searchCalled Search 调用次数
	searchCalled int
	// lastSearchTypes 最近一次 Search 传入的 memTypes
	lastSearchTypes []string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewSearchManager 测试构造函数
func TestNewSearchManager(t *testing.T) {
	managers := map[string]manageindex.BaseMemoryManager{
		"variable": &stubSearchManager{memTypeStr: "variable"},
	}
	sm := NewSearchManager(managers, nil, nil)
	if sm == nil {
		t.Fatal("NewSearchManager 返回 nil")
	}
}

// TestNewSearchParams 测试默认搜索参数
func TestNewSearchParams(t *testing.T) {
	params := NewSearchParams("user1", "scope1", "test query")
	if params.UserID != "user1" {
		t.Errorf("UserID = %q, want %q", params.UserID, "user1")
	}
	if params.TopK != DefaultTopK {
		t.Errorf("TopK = %d, want %d", params.TopK, DefaultTopK)
	}
	if params.Threshold != DefaultThreshold {
		t.Errorf("Threshold = %f, want %f", params.Threshold, DefaultThreshold)
	}
	if params.SearchType != nil {
		t.Errorf("SearchType = %v, want nil", params.SearchType)
	}
}

// TestSearchManager_Search_全部类型 测试无 search_type 时遍历所有 Manager
func TestSearchManager_Search_全部类型(t *testing.T) {
	fragMgr := &stubSearchManager{
		memTypeStr: "user_profile",
		searchResult: []*storeindex.MemorySearchResult{
			{Doc: &storeindex.MemoryDoc{ID: "f1"}, Score: 0.8},
		},
	}
	varMgr := &stubSearchManager{
		memTypeStr: "variable",
		searchResult: []*storeindex.MemorySearchResult{
			{Doc: &storeindex.MemoryDoc{ID: "v1"}, Score: 0.5},
		},
	}

	// 3 个 Fragment 类型共享 1 个 Manager + 1 个 Variable Manager
	managers := map[string]manageindex.BaseMemoryManager{
		"user_profile":    fragMgr,
		"semantic_memory": fragMgr,
		"episodic_memory": fragMgr,
		"variable":        varMgr,
	}

	sm := NewSearchManager(managers, nil, nil)
	params := NewSearchParams("user1", "scope1", "test")
	results, err := sm.Search(context.Background(), params)
	if err != nil {
		t.Fatalf("Search 返回错误: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("期望 2 个结果（frag 1 + variable 1），实际 %d 个", len(results))
	}
	// 共享的 fragMgr 应该只被调用 1 次
	if fragMgr.searchCalled != 1 {
		t.Errorf("期望 fragMgr Search 被调用 1 次，实际 %d 次", fragMgr.searchCalled)
	}
}

// TestSearchManager_Search_指定类型 测试按 search_type 路由
func TestSearchManager_Search_指定类型(t *testing.T) {
	fragMgr := &stubSearchManager{
		memTypeStr: "user_profile",
		searchResult: []*storeindex.MemorySearchResult{
			{Doc: &storeindex.MemoryDoc{ID: "f1"}, Score: 0.9},
		},
	}

	managers := map[string]manageindex.BaseMemoryManager{
		"user_profile":    fragMgr,
		"semantic_memory": fragMgr, // 共享
		"variable":        &stubSearchManager{memTypeStr: "variable"},
	}

	sm := NewSearchManager(managers, nil, nil)
	params := NewSearchParams("user1", "scope1", "test")
	params.SearchType = []string{"user_profile", "semantic_memory"}
	results, err := sm.Search(context.Background(), params)
	if err != nil {
		t.Fatalf("Search 返回错误: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("期望 1 个结果，实际 %d 个", len(results))
	}
	// 共享 fragMgr 应该只被调用 1 次（去重），但 types 应包含 user_profile + semantic_memory
	if fragMgr.searchCalled != 1 {
		t.Errorf("期望 fragMgr Search 被调用 1 次，实际 %d 次", fragMgr.searchCalled)
	}
	if len(fragMgr.lastSearchTypes) != 2 {
		t.Errorf("期望 lastSearchTypes 包含 2 个类型，实际 %d 个", len(fragMgr.lastSearchTypes))
	}
}

// TestSearchManager_Search_排序截断 测试 score 降序 + top_k + threshold
func TestSearchManager_Search_排序截断(t *testing.T) {
	fragMgr := &stubSearchManager{
		memTypeStr: "user_profile",
		searchResult: []*storeindex.MemorySearchResult{
			{Doc: &storeindex.MemoryDoc{ID: "low"}, Score: 0.4},
			{Doc: &storeindex.MemoryDoc{ID: "high"}, Score: 0.9},
			{Doc: &storeindex.MemoryDoc{ID: "mid"}, Score: 0.6},
			{Doc: &storeindex.MemoryDoc{ID: "below"}, Score: 0.1},
		},
	}

	managers := map[string]manageindex.BaseMemoryManager{
		"user_profile": fragMgr,
	}

	sm := NewSearchManager(managers, nil, nil)
	params := NewSearchParams("user1", "scope1", "test")
	params.TopK = 2
	params.Threshold = 0.3
	results, err := sm.Search(context.Background(), params)
	if err != nil {
		t.Fatalf("Search 返回错误: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("期望 2 个结果（top_k=2），实际 %d 个", len(results))
	}
	// 结果按 score 降序排列
	if results[0].Score < results[1].Score {
		t.Errorf("结果未按 score 降序排列: %f, %f", results[0].Score, results[1].Score)
	}
	// threshold 0.3 过滤掉了 score=0.1 和 score=0.4 的结果
	// 但 score=0.4 >= 0.3，所以应该包含
	// 实际上 0.4 >= 0.3 通过 threshold，0.1 < 0.3 被过滤
	// 剩余: 0.9, 0.6, 0.4 → top_k=2 → 0.9, 0.6
	if results[0].Doc.ID != "high" {
		t.Errorf("第一个结果 ID = %q, want %q", results[0].Doc.ID, "high")
	}
	if results[1].Doc.ID != "mid" {
		t.Errorf("第二个结果 ID = %q, want %q", results[1].Doc.ID, "mid")
	}
}

// TestSearchManager_Search_非法类型 测试不合法的 search_type 返回错误
func TestSearchManager_Search_非法类型(t *testing.T) {
	managers := map[string]manageindex.BaseMemoryManager{
		"variable": &stubSearchManager{memTypeStr: "variable"},
	}

	sm := NewSearchManager(managers, nil, nil)
	params := NewSearchParams("user1", "scope1", "test")
	params.SearchType = []string{"invalid_type"}
	_, err := sm.Search(context.Background(), params)
	if err == nil {
		t.Fatal("期望返回错误，实际返回 nil")
	}
}

// TestSearchManager_Search_未初始化 测试 search_type 对应 Manager 未初始化
func TestSearchManager_Search_未初始化(t *testing.T) {
	managers := map[string]manageindex.BaseMemoryManager{}

	sm := NewSearchManager(managers, nil, nil)
	params := NewSearchParams("user1", "scope1", "test")
	params.SearchType = []string{"variable"}
	_, err := sm.Search(context.Background(), params)
	if err == nil {
		t.Fatal("期望返回错误（manager 未初始化），实际返回 nil")
	}
}

// TestSearchManager_ListUserMem_无索引 测试 memory_index 未初始化
func TestSearchManager_ListUserMem_无索引(t *testing.T) {
	sm := NewSearchManager(nil, nil, nil)
	_, err := sm.ListUserMem(context.Background(), "user1", "scope1", 10, 1, "")
	if err == nil {
		t.Fatal("期望返回错误，实际返回 nil")
	}
}

// TestContainsString 测试辅助函数
func TestContainsString(t *testing.T) {
	slice := []string{"a", "b", "c"}
	if !containsString(slice, "a") {
		t.Error("期望 containsString 返回 true")
	}
	if containsString(slice, "d") {
		t.Error("期望 containsString 返回 false")
	}
	if containsString(nil, "a") {
		t.Error("期望 nil 切片返回 false")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// AddMemories 实现 BaseMemoryManager 接口
func (s *stubSearchManager) AddMemories(ctx context.Context, userID string, scopeID string,
	memories map[string][]mem_model.MemoryUnit, llmModel ...*llm.Model) ([]mem_model.MemoryUnit, error) {
	return nil, nil
}

// Update 实现 BaseMemoryManager 接口
func (s *stubSearchManager) Update(ctx context.Context, userID string, scopeID string, memID string, newMemory string) (bool, error) {
	return true, nil
}

// Search 实现 BaseMemoryManager 接口
func (s *stubSearchManager) Search(ctx context.Context, userID string, scopeID string, query string, topK int, memTypes []string) ([]*storeindex.MemorySearchResult, error) {
	s.searchCalled++
	s.lastSearchTypes = memTypes
	return s.searchResult, s.searchErr
}

// Get 实现 BaseMemoryManager 接口
func (s *stubSearchManager) Get(ctx context.Context, userID string, scopeID string, memID string) (*storeindex.MemoryDoc, error) {
	return nil, nil
}

// Delete 实现 BaseMemoryManager 接口
func (s *stubSearchManager) Delete(ctx context.Context, userID string, scopeID string, memID string) (bool, error) {
	return true, nil
}

// DeleteByUserID 实现 BaseMemoryManager 接口
func (s *stubSearchManager) DeleteByUserID(ctx context.Context, userID string, scopeID string) (bool, error) {
	return true, nil
}
