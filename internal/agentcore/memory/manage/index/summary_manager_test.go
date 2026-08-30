//go:build test

package index

import (
	"context"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/index"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/mem_model"
)

func TestNewSummaryManager(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()
	mgr := NewSummaryManager(fakeIdx, nil)
	if mgr == nil {
		t.Fatal("NewSummaryManager 返回 nil")
	}
	if mgr.memType != "summary" {
		t.Errorf("memType = %q, want %q", mgr.memType, "summary")
	}
}

func TestSummaryManager_AddMemories(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()
	mgr := NewSummaryManager(fakeIdx, nil)

	memories := map[string][]mem_model.MemoryUnit{
		"summary": {
			&mem_model.SummaryUnit{
				BaseMemoryUnit: mem_model.BaseMemoryUnit{
					MemType: mem_model.MemoryTypeSummary,
					MemID:   "sum-001",
				},
				Summary:       "用户讨论了项目架构设计",
				MessageMemID:  "msg-001",
				Timestamp:     "2027-04-15 10:00:00",
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
	doc, _ := fakeIdx.GetByID(context.Background(), "user-1", "scope-1", "sum-001")
	if doc == nil {
		t.Fatal("期望记忆已写入，但 GetByID 返回 nil")
	}
	if doc.Text != "用户讨论了项目架构设计" {
		t.Errorf("doc.Text = %q, want %q", doc.Text, "用户讨论了项目架构设计")
	}
	if doc.Type != "summary" {
		t.Errorf("doc.Type = %q, want %q", doc.Type, "summary")
	}
	// 验证 fields
	if doc.Fields["source_id"] != "msg-001" {
		t.Errorf("doc.Fields[source_id] = %v, want %q", doc.Fields["source_id"], "msg-001")
	}
}

func TestSummaryManager_AddMemories_Multiple(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()
	mgr := NewSummaryManager(fakeIdx, nil)

	memories := map[string][]mem_model.MemoryUnit{
		"summary": {
			&mem_model.SummaryUnit{
				BaseMemoryUnit: mem_model.BaseMemoryUnit{MemType: mem_model.MemoryTypeSummary, MemID: "sum-001"},
				Summary: "摘要1", Timestamp: "2027-04-15 10:00:00",
			},
			&mem_model.SummaryUnit{
				BaseMemoryUnit: mem_model.BaseMemoryUnit{MemType: mem_model.MemoryTypeSummary, MemID: "sum-002"},
				Summary: "摘要2", Timestamp: "2027-04-15 11:00:00",
			},
		},
	}
	result, err := mgr.AddMemories(context.Background(), "user-1", "scope-1", memories)
	if err != nil {
		t.Fatalf("AddMemories 返回 error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("期望返回 2 个结果，得到 %d", len(result))
	}
}

func TestSummaryManager_AddMemories_NonSummaryTypeIgnored(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()
	mgr := NewSummaryManager(fakeIdx, nil)

	memories := map[string][]mem_model.MemoryUnit{
		"user_profile": {
			&mem_model.FragmentMemoryUnit{
				BaseMemoryUnit: mem_model.BaseMemoryUnit{MemType: mem_model.MemoryTypeUserProfile, MemID: "prof-001"},
				Content: "用户喜欢阅读",
			},
		},
	}
	result, err := mgr.AddMemories(context.Background(), "user-1", "scope-1", memories)
	if err != nil {
		t.Fatalf("AddMemories 返回 error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("期望返回 0 个结果（非 summary 类型被忽略），得到 %d", len(result))
	}
}

func TestSummaryManager_AddMemories_EmptyResult(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()
	mgr := NewSummaryManager(fakeIdx, nil)

	memories := map[string][]mem_model.MemoryUnit{}
	result, err := mgr.AddMemories(context.Background(), "user-1", "scope-1", memories)
	if err != nil {
		t.Fatalf("AddMemories 返回 error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("期望返回 0 个结果，得到 %d", len(result))
	}
}

func TestSummaryManager_AddMemories_ValidateParams(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()
	mgr := NewSummaryManager(fakeIdx, nil)
	_, err := mgr.AddMemories(context.Background(), "", "scope-1", nil)
	if err == nil {
		t.Fatal("期望返回 error，但得到 nil")
	}
}

func TestSummaryManager_Update(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()
	_ = fakeIdx.AddMemories(context.Background(), "user-1", "scope-1", []*index.MemoryDoc{
		{ID: "sum-001", Text: "旧摘要", Type: "summary", Timestamp: time.Now()},
	})

	mgr := NewSummaryManager(fakeIdx, nil)
	ok, err := mgr.Update(context.Background(), "user-1", "scope-1", "sum-001", "新摘要")
	if err != nil {
		t.Fatalf("Update 返回 error: %v", err)
	}
	if !ok {
		t.Error("期望返回 true")
	}
	doc, _ := fakeIdx.GetByID(context.Background(), "user-1", "scope-1", "sum-001")
	if doc == nil || doc.Text != "新摘要" {
		t.Errorf("更新后 doc.Text = %q, want %q", doc.Text, "新摘要")
	}
}

func TestSummaryManager_Update_NotFound(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()
	mgr := NewSummaryManager(fakeIdx, nil)
	ok, err := mgr.Update(context.Background(), "user-1", "scope-1", "nonexistent", "新摘要")
	if err != nil {
		t.Fatalf("Update 返回 error: %v", err)
	}
	if ok {
		t.Error("期望返回 false")
	}
}

func TestSummaryManager_Delete(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()
	_ = fakeIdx.AddMemories(context.Background(), "user-1", "scope-1", []*index.MemoryDoc{
		{ID: "sum-001", Text: "摘要", Type: "summary", Timestamp: time.Now()},
	})

	mgr := NewSummaryManager(fakeIdx, nil)
	ok, err := mgr.Delete(context.Background(), "user-1", "scope-1", "sum-001")
	if err != nil {
		t.Fatalf("Delete 返回 error: %v", err)
	}
	if !ok {
		t.Error("期望返回 true")
	}
	doc, _ := fakeIdx.GetByID(context.Background(), "user-1", "scope-1", "sum-001")
	if doc != nil {
		t.Error("期望记忆已删除")
	}
}

func TestSummaryManager_DeleteByUserID(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()
	_ = fakeIdx.AddMemories(context.Background(), "user-1", "scope-1", []*index.MemoryDoc{
		{ID: "sum-001", Text: "摘要", Type: "summary", Timestamp: time.Now()},
	})

	mgr := NewSummaryManager(fakeIdx, nil)
	ok, err := mgr.DeleteByUserID(context.Background(), "user-1", "scope-1")
	if err != nil {
		t.Fatalf("DeleteByUserID 返回 error: %v", err)
	}
	if !ok {
		t.Error("期望返回 true")
	}
}

func TestSummaryManager_Get(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()
	_ = fakeIdx.AddMemories(context.Background(), "user-1", "scope-1", []*index.MemoryDoc{
		{ID: "sum-001", Text: "摘要内容", Type: "summary", Timestamp: time.Now()},
	})

	mgr := NewSummaryManager(fakeIdx, nil)
	doc, err := mgr.Get(context.Background(), "user-1", "scope-1", "sum-001")
	if err != nil {
		t.Fatalf("Get 返回 error: %v", err)
	}
	if doc == nil {
		t.Fatal("期望返回 MemoryDoc")
	}
	if doc.ID != "sum-001" {
		t.Errorf("doc.ID = %q, want %q", doc.ID, "sum-001")
	}
}

func TestSummaryManager_Get_NotFound(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()
	mgr := NewSummaryManager(fakeIdx, nil)
	doc, err := mgr.Get(context.Background(), "user-1", "scope-1", "nonexistent")
	if err != nil {
		t.Fatalf("Get 返回 error: %v", err)
	}
	if doc != nil {
		t.Errorf("期望返回 nil，得到 %+v", doc)
	}
}

func TestSummaryManager_Search(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()
	_ = fakeIdx.AddMemories(context.Background(), "user-1", "scope-1", []*index.MemoryDoc{
		{ID: "sum-001", Text: "用户讨论了架构设计", Type: "summary", Timestamp: time.Now()},
	})

	mgr := NewSummaryManager(fakeIdx, nil)
	result, err := mgr.Search(context.Background(), "user-1", "scope-1", "架构", 5, nil)
	if err != nil {
		t.Fatalf("Search 返回 error: %v", err)
	}
	if len(result) == 0 {
		t.Error("期望返回搜索结果")
	}
}

func TestSummaryManager_Search_WithMemTypes(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()
	_ = fakeIdx.AddMemories(context.Background(), "user-1", "scope-1", []*index.MemoryDoc{
		{ID: "sum-001", Text: "摘要", Type: "summary", Timestamp: time.Now()},
	})

	mgr := NewSummaryManager(fakeIdx, nil)
	// memTypes 参数被忽略，硬编码为 ["summary"]
	result, err := mgr.Search(context.Background(), "user-1", "scope-1", "摘要", 5, []string{"summary"})
	if err != nil {
		t.Fatalf("Search 返回 error: %v", err)
	}
	if len(result) == 0 {
		t.Error("期望返回搜索结果")
	}
}

func TestSummaryManager_ListUserSummary(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()
	baseTime := time.Now()
	_ = fakeIdx.AddMemories(context.Background(), "user-1", "scope-1", []*index.MemoryDoc{
		{ID: "sum-001", Text: "较早的摘要", Type: "summary", Timestamp: baseTime.Add(-1 * time.Hour)},
		{ID: "sum-002", Text: "较晚的摘要", Type: "summary", Timestamp: baseTime},
	})

	mgr := NewSummaryManager(fakeIdx, nil)
	docs, err := mgr.ListUserSummary(context.Background(), "user-1", "scope-1", 0, 100)
	if err != nil {
		t.Fatalf("ListUserSummary 返回 error: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("期望返回 2 条记忆，得到 %d", len(docs))
	}
	// 验证按 timestamp 降序排列
	if !docs[0].Timestamp.After(docs[1].Timestamp) {
		t.Error("期望按 timestamp 降序排列")
	}
}

func TestSummaryManager_ListUserSummary_Empty(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()
	mgr := NewSummaryManager(fakeIdx, nil)
	docs, err := mgr.ListUserSummary(context.Background(), "user-1", "scope-1", 0, 100)
	if err != nil {
		t.Fatalf("ListUserSummary 返回 error: %v", err)
	}
	if len(docs) != 0 {
		t.Errorf("期望返回 0 条记忆，得到 %d", len(docs))
	}
}

func TestSummaryManager_Delete_ValidateParams(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()
	mgr := NewSummaryManager(fakeIdx, nil)
	_, err := mgr.Delete(context.Background(), "", "scope-1", "sum-001")
	if err == nil {
		t.Fatal("期望返回 error")
	}
}

func TestSummaryManager_Search_ValidateParams(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()
	mgr := NewSummaryManager(fakeIdx, nil)
	_, err := mgr.Search(context.Background(), "", "scope-1", "query", 5, nil)
	if err == nil {
		t.Fatal("期望返回 error")
	}
}

func TestSummaryManager_Update_ValidateParams(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()
	mgr := NewSummaryManager(fakeIdx, nil)
	_, err := mgr.Update(context.Background(), "", "scope-1", "sum-001", "new")
	if err == nil {
		t.Fatal("期望返回 error")
	}
}

func TestSummaryManager_DeleteByUserID_ValidateParams(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()
	mgr := NewSummaryManager(fakeIdx, nil)
	_, err := mgr.DeleteByUserID(context.Background(), "", "scope-1")
	if err == nil {
		t.Fatal("期望返回 error")
	}
}

func TestSummaryManager_ListUserSummary_ValidateParams(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()
	mgr := NewSummaryManager(fakeIdx, nil)
	_, err := mgr.ListUserSummary(context.Background(), "", "scope-1", 0, 100)
	if err == nil {
		t.Fatal("期望返回 error")
	}
}

func TestSummaryManager_AddMemories_WithCryptoKey(t *testing.T) {
	fakeIdx := newFakeMemoryIndex()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	mgr := NewSummaryManager(fakeIdx, key)

	memories := map[string][]mem_model.MemoryUnit{
		"summary": {
			&mem_model.SummaryUnit{
				BaseMemoryUnit: mem_model.BaseMemoryUnit{MemType: mem_model.MemoryTypeSummary, MemID: "sum-001"},
				Summary: "加密摘要", Timestamp: "2027-04-15 10:00:00",
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
}

func TestSummaryManager_AddMemories_NonSummaryUnitTypeIgnored(t *testing.T) {
	// 测试 summary key 下传入非 SummaryUnit 类型的情况
	fakeIdx := newFakeMemoryIndex()
	mgr := NewSummaryManager(fakeIdx, nil)

	memories := map[string][]mem_model.MemoryUnit{
		"summary": {
			&mem_model.FragmentMemoryUnit{
				BaseMemoryUnit: mem_model.BaseMemoryUnit{MemType: mem_model.MemoryTypeSummary, MemID: "sum-001"},
				Content: "这不是摘要",
			},
		},
	}
	result, err := mgr.AddMemories(context.Background(), "user-1", "scope-1", memories)
	if err != nil {
		t.Fatalf("AddMemories 返回 error: %v", err)
	}
	// 非 SummaryUnit 类型被忽略，结果为空
	if len(result) != 0 {
		t.Errorf("期望返回 0 个结果（非 SummaryUnit 被忽略），得到 %d", len(result))
	}
}
