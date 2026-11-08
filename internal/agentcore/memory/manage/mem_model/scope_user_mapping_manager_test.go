package mem_model

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/kv"
)

// TestKvScopeUserMappingManager_增删查 覆盖 KV 后端映射管理器的三条基本路径。
//
// 为什么需要这个后端：LongTermMemory 原先强依赖 db_store，没有 db 就整体不可用。
// 该实现让 db_store 缺失时仍能记录 scope→user 映射，从而正常清理数据。
// 对齐 Python KvScopeUserMappingManager（scope_user_mapping_manager.py:50）。
func TestKvScopeUserMappingManager_增删查(t *testing.T) {
	ctx := context.Background()
	kvStore := kv.NewInMemoryKVStore()

	mgr, err := NewKvScopeUserMappingManager(kvStore)
	if err != nil {
		t.Fatalf("创建 KvScopeUserMappingManager 失败: %v", err)
	}

	// 初始无映射
	got, err := mgr.GetByScopeID(ctx, "scope-1")
	if err != nil {
		t.Fatalf("GetByScopeID 失败: %v", err)
	}
	if got != nil {
		t.Errorf("初始应无映射，实际 %v", got)
	}

	// 添加两条
	if err := mgr.Add(ctx, "user-a", "scope-1"); err != nil {
		t.Fatalf("Add 失败: %v", err)
	}
	if err := mgr.Add(ctx, "user-b", "scope-1"); err != nil {
		t.Fatalf("Add 失败: %v", err)
	}
	// 幂等：重复添加不报错也不重复
	if err := mgr.Add(ctx, "user-a", "scope-1"); err != nil {
		t.Fatalf("重复 Add 应幂等，实际失败: %v", err)
	}

	got, err = mgr.GetByScopeID(ctx, "scope-1")
	if err != nil {
		t.Fatalf("GetByScopeID 失败: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("应查到 2 条映射，实际 %d 条: %v", len(got), got)
	}

	// 另一个 scope 不应被串到
	other, err := mgr.GetByScopeID(ctx, "scope-2")
	if err != nil {
		t.Fatalf("GetByScopeID 失败: %v", err)
	}
	if other != nil {
		t.Errorf("scope-2 应无映射，实际 %v", other)
	}

	// 按 scope 删除
	if err := mgr.DeleteByScopeID(ctx, "scope-1"); err != nil {
		t.Fatalf("DeleteByScopeID 失败: %v", err)
	}
	got, err = mgr.GetByScopeID(ctx, "scope-1")
	if err != nil {
		t.Fatalf("GetByScopeID 失败: %v", err)
	}
	if got != nil {
		t.Errorf("删除后应为空，实际 %v", got)
	}
}

// TestKvScopeUserMappingManager_映射内容含用户与作用域 落盘的每条记录必须带
// user_id 与 scope_id 两个字段——上层 DeleteMemByScope 依赖 user_id 逐用户清理。
func TestKvScopeUserMappingManager_映射内容含用户与作用域(t *testing.T) {
	ctx := context.Background()
	kvStore := kv.NewInMemoryKVStore()

	mgr, err := NewKvScopeUserMappingManager(kvStore)
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if err := mgr.Add(ctx, "user-x", "scope-y"); err != nil {
		t.Fatalf("Add 失败: %v", err)
	}

	got, err := mgr.GetByScopeID(ctx, "scope-y")
	if err != nil {
		t.Fatalf("GetByScopeID 失败: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("应有 1 条，实际 %d", len(got))
	}
	if got[0]["user_id"] != "user-x" {
		t.Errorf("user_id = %v, want user-x", got[0]["user_id"])
	}
	if got[0]["scope_id"] != "scope-y" {
		t.Errorf("scope_id = %v, want scope-y", got[0]["scope_id"])
	}
}
