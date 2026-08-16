//go:build test

package index

import (
	"context"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/index"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/mem_model"
)

func TestNewFragmentMemoryManager(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()
	mgr := NewFragmentMemoryManager(fakeIdx, nil)
	if mgr == nil {
		t.Fatal("NewFragmentMemoryManager 返回 nil")
	}
	if mgr.memType != "fragment" {
		t.Errorf("memType = %q, want %q", mgr.memType, "fragment")
	}
}

func TestFragmentMemoryManager_AddMemories_SingleAdd(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()
	mgr := NewFragmentMemoryManager(fakeIdx, nil)

	memories := map[string][]*mem_model.FragmentMemoryUnit{
		"user_profile": {
			{
				BaseMemoryUnit: mem_model.BaseMemoryUnit{
					MemType: mem_model.MemoryTypeUserProfile,
					MemID:   "mem-001",
				},
				Content:       "用户喜欢阅读",
				OperationType: mem_model.OperationTypeAdd,
			},
		},
	}
	result, err := mgr.AddMemories(context.Background(), "user-1", "scope-1", memories)
	if err != nil {
		t.Fatalf("AddMemories 返回 error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("期望返回 1 个结果，得到 %d", len(result))
	}
	// 验证写入到 fakeIndex
	doc, _ := fakeIdx.GetByID(context.Background(), "user-1", "scope-1", "mem-001")
	if doc == nil {
		t.Fatal("期望记忆已写入，但 GetByID 返回 nil")
	}
	if doc.Text != "用户喜欢阅读" {
		t.Errorf("doc.Text = %q, want %q", doc.Text, "用户喜欢阅读")
	}
}

func TestFragmentMemoryManager_Search(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()
	_ = fakeIdx.AddMemories(context.Background(), "user-1", "scope-1", []*index.MemoryDoc{
		{ID: "mem-001", Text: "用户喜欢阅读", Type: "user_profile", Timestamp: time.Now()},
	})

	mgr := NewFragmentMemoryManager(fakeIdx, nil)
	result, err := mgr.Search(context.Background(), "user-1", "scope-1", "阅读", 5, nil)
	if err != nil {
		t.Fatalf("Search 返回 error: %v", err)
	}
	if len(result) == 0 {
		t.Error("期望返回搜索结果，但得到空列表")
	}
}

func TestFragmentMemoryManager_Get(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()
	_ = fakeIdx.AddMemories(context.Background(), "user-1", "scope-1", []*index.MemoryDoc{
		{ID: "mem-001", Text: "用户喜欢阅读", Type: "user_profile", Timestamp: time.Now()},
	})

	mgr := NewFragmentMemoryManager(fakeIdx, nil)
	doc, err := mgr.Get(context.Background(), "user-1", "scope-1", "mem-001")
	if err != nil {
		t.Fatalf("Get 返回 error: %v", err)
	}
	if doc == nil {
		t.Fatal("期望返回 MemoryDoc，但得到 nil")
	}
	if doc.ID != "mem-001" {
		t.Errorf("doc.ID = %q, want %q", doc.ID, "mem-001")
	}
}

func TestFragmentMemoryManager_Get_NotFound(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()
	mgr := NewFragmentMemoryManager(fakeIdx, nil)
	doc, err := mgr.Get(context.Background(), "user-1", "scope-1", "nonexistent")
	if err != nil {
		t.Fatalf("Get 返回 error: %v", err)
	}
	if doc != nil {
		t.Errorf("期望返回 nil，但得到 %+v", doc)
	}
}

func TestFragmentMemoryManager_Update(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()
	_ = fakeIdx.AddMemories(context.Background(), "user-1", "scope-1", []*index.MemoryDoc{
		{ID: "mem-001", Text: "旧内容", Type: "user_profile", Timestamp: time.Now()},
	})

	mgr := NewFragmentMemoryManager(fakeIdx, nil)
	ok, err := mgr.Update(context.Background(), "user-1", "scope-1", "mem-001", "新内容")
	if err != nil {
		t.Fatalf("Update 返回 error: %v", err)
	}
	if !ok {
		t.Error("期望返回 true，但得到 false")
	}
	// 验证更新后的内容
	doc, _ := fakeIdx.GetByID(context.Background(), "user-1", "scope-1", "mem-001")
	if doc == nil || doc.Text != "新内容" {
		t.Errorf("更新后 doc.Text = %q, want %q", doc.Text, "新内容")
	}
}

func TestFragmentMemoryManager_Update_NotFound(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()
	mgr := NewFragmentMemoryManager(fakeIdx, nil)
	ok, err := mgr.Update(context.Background(), "user-1", "scope-1", "nonexistent", "新内容")
	if err != nil {
		t.Fatalf("Update 返回 error: %v", err)
	}
	if ok {
		t.Error("期望返回 false，但得到 true")
	}
}

func TestFragmentMemoryManager_Delete(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()
	_ = fakeIdx.AddMemories(context.Background(), "user-1", "scope-1", []*index.MemoryDoc{
		{ID: "mem-001", Text: "用户喜欢阅读", Type: "user_profile", Timestamp: time.Now()},
	})

	mgr := NewFragmentMemoryManager(fakeIdx, nil)
	ok, err := mgr.Delete(context.Background(), "user-1", "scope-1", "mem-001")
	if err != nil {
		t.Fatalf("Delete 返回 error: %v", err)
	}
	if !ok {
		t.Error("期望返回 true，但得到 false")
	}
	// 验证已删除
	doc, _ := fakeIdx.GetByID(context.Background(), "user-1", "scope-1", "mem-001")
	if doc != nil {
		t.Error("期望记忆已删除，但 GetByID 返回非 nil")
	}
}

func TestFragmentMemoryManager_Delete_NotFound(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()
	mgr := NewFragmentMemoryManager(fakeIdx, nil)
	ok, err := mgr.Delete(context.Background(), "user-1", "scope-1", "nonexistent")
	if err != nil {
		t.Fatalf("Delete 返回 error: %v", err)
	}
	if ok {
		t.Error("期望返回 false，但得到 true")
	}
}

func TestFragmentMemoryManager_DeleteByUserID(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()
	_ = fakeIdx.AddMemories(context.Background(), "user-1", "scope-1", []*index.MemoryDoc{
		{ID: "mem-001", Text: "用户喜欢阅读", Type: "user_profile", Timestamp: time.Now()},
	})

	mgr := NewFragmentMemoryManager(fakeIdx, nil)
	ok, err := mgr.DeleteByUserID(context.Background(), "user-1", "scope-1")
	if err != nil {
		t.Fatalf("DeleteByUserID 返回 error: %v", err)
	}
	if !ok {
		t.Error("期望返回 true，但得到 false")
	}
}

func TestFragmentMemoryManager_ValidateParams_UserIDEmpty(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()
	mgr := NewFragmentMemoryManager(fakeIdx, nil)
	_, err := mgr.AddMemories(context.Background(), "", "scope-1", nil)
	if err == nil {
		t.Fatal("期望返回 error，但得到 nil")
	}
}

func TestFragmentMemoryManager_ValidateParams_ScopeIDEmpty(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()
	mgr := NewFragmentMemoryManager(fakeIdx, nil)
	_, err := mgr.AddMemories(context.Background(), "user-1", "", nil)
	if err == nil {
		t.Fatal("期望返回 error，但得到 nil")
	}
}

func TestFragmentMemoryManager_AddMemories_WithDelete(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()
	_ = fakeIdx.AddMemories(context.Background(), "user-1", "scope-1", []*index.MemoryDoc{
		{ID: "old-001", Text: "旧记忆", Type: "user_profile", Timestamp: time.Now()},
	})

	mgr := NewFragmentMemoryManager(fakeIdx, nil)
	memories := map[string][]*mem_model.FragmentMemoryUnit{
		"user_profile": {
			{
				BaseMemoryUnit: mem_model.BaseMemoryUnit{
					MemType: mem_model.MemoryTypeUserProfile,
					MemID:   "old-001",
				},
				OperationType: mem_model.OperationTypeDelete,
			},
		},
	}
	result, err := mgr.AddMemories(context.Background(), "user-1", "scope-1", memories)
	if err != nil {
		t.Fatalf("AddMemories 返回 error: %v", err)
	}
	// 验证旧记忆已删除
	doc, _ := fakeIdx.GetByID(context.Background(), "user-1", "scope-1", "old-001")
	if doc != nil {
		t.Error("期望旧记忆已删除，但 GetByID 返回非 nil")
	}
	_ = result
}

func TestFragmentMemoryManager_AddMemories_WithUpdate(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()
	_ = fakeIdx.AddMemories(context.Background(), "user-1", "scope-1", []*index.MemoryDoc{
		{ID: "mem-001", Text: "旧内容", Type: "user_profile", Timestamp: time.Now()},
	})

	mgr := NewFragmentMemoryManager(fakeIdx, nil)
	memories := map[string][]*mem_model.FragmentMemoryUnit{
		"user_profile": {
			{
				BaseMemoryUnit: mem_model.BaseMemoryUnit{
					MemType: mem_model.MemoryTypeUserProfile,
					MemID:   "mem-001",
				},
				Content:       "新内容",
				OperationType: mem_model.OperationTypeUpdate,
			},
		},
	}
	result, err := mgr.AddMemories(context.Background(), "user-1", "scope-1", memories)
	if err != nil {
		t.Fatalf("AddMemories 返回 error: %v", err)
	}
	// 验证更新后的内容
	doc, _ := fakeIdx.GetByID(context.Background(), "user-1", "scope-1", "mem-001")
	if doc == nil || doc.Text != "新内容" {
		t.Errorf("更新后 doc.Text = %q, want %q", doc.Text, "新内容")
	}
	_ = result
}

func TestFragmentMemoryManager_ListFragmentMemories(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()
	_ = fakeIdx.AddMemories(context.Background(), "user-1", "scope-1", []*index.MemoryDoc{
		{ID: "mem-001", Text: "用户喜欢阅读", Type: "user_profile", Timestamp: time.Now()},
		{ID: "mem-002", Text: "用户是工程师", Type: "semantic_memory", Timestamp: time.Now()},
	})

	mgr := NewFragmentMemoryManager(fakeIdx, nil)
	docs, err := mgr.ListFragmentMemories(context.Background(), "user-1", "scope-1", 0, 100, "")
	if err != nil {
		t.Fatalf("ListFragmentMemories 返回 error: %v", err)
	}
	if len(docs) != 2 {
		t.Errorf("期望返回 2 条记忆，得到 %d", len(docs))
	}
}

func TestRemoveUpdateEntriesFromProcessResult(t *testing.T) {
	processResult := map[string]*mem_model.FragmentMemoryUnit{
		"mem-001": {OperationType: mem_model.OperationTypeUpdate},
		"mem-002": {OperationType: mem_model.OperationTypeAdd},
	}
	deleteSet := map[string]bool{"mem-001": true}
	removeUpdateEntriesFromProcessResult(deleteSet, processResult)
	if _, ok := processResult["mem-001"]; ok {
		t.Error("mem-001 应被移除")
	}
	if _, ok := processResult["mem-002"]; !ok {
		t.Error("mem-002 应保留")
	}
}

func TestAppendMemUnitListToDict(t *testing.T) {
	dict := map[string]*mem_model.FragmentMemoryUnit{
		"mem-001": {BaseMemoryUnit: mem_model.BaseMemoryUnit{MemID: "mem-001"}, Content: "旧内容"},
	}
	list := []*mem_model.FragmentMemoryUnit{
		{BaseMemoryUnit: mem_model.BaseMemoryUnit{MemID: "mem-001"}, Content: "新内容"},
		{BaseMemoryUnit: mem_model.BaseMemoryUnit{MemID: "mem-002"}, Content: "新增内容"},
	}
	appendMemUnitListToDict(dict, list)
	if dict["mem-001"].Content != "新内容" {
		t.Errorf("mem-001 应被覆盖，得到 %q", dict["mem-001"].Content)
	}
	if dict["mem-002"].Content != "新增内容" {
		t.Errorf("mem-002 应被添加，得到 %q", dict["mem-002"].Content)
	}
}

func TestParseTimestamp(t *testing.T) {
	tests := []struct {
		input string
		valid bool
	}{
		{"", false}, // 空字符串返回当前时间
		{"2027-04-01 12:00:00", true},
		{"2027-04-01 12-00-00", true},
		{"2027-04-01T12:00:00Z", true},
		{"invalid", false}, // 无效格式返回当前时间
	}
	for _, tt := range tests {
		result := parseTimestamp(tt.input)
		if tt.valid {
			if result.IsZero() {
				t.Errorf("parseTimestamp(%q) 返回零值", tt.input)
			}
		}
		// 不验证空字符串和无效格式的具体值，只验证不 panic
	}
}

func TestIsFragmentMemoryType(t *testing.T) {
	if !isFragmentMemoryType("user_profile") {
		t.Error("user_profile 应为碎片记忆类型")
	}
	if !isFragmentMemoryType("semantic_memory") {
		t.Error("semantic_memory 应为碎片记忆类型")
	}
	if !isFragmentMemoryType("episodic_memory") {
		t.Error("episodic_memory 应为碎片记忆类型")
	}
	if isFragmentMemoryType("variable") {
		t.Error("variable 不应为碎片记忆类型")
	}
	if isFragmentMemoryType("summary") {
		t.Error("summary 不应为碎片记忆类型")
	}
}
