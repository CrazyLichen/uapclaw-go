package mem_model

import (
	"context"
	"testing"

	kv "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/kv"
)

// ──────────────────────────── 测试用 24 字节 ID 常量 ────────────────────────────

// 测试用 ID 必须精确 24 字节（对齐 Python BYTE_NUM_PER_ID = 24）
const (
	testID1 = "123456789012345678901234" // 精确 24 字节
	testID2 = "aaaaaaaaaaaaaaaaaaaaaaaa" // 精确 24 字节
	testID3 = "bbbbbbbbbbbbbbbbbbbbbbbb" // 精确 24 字节
	testID4 = "cccccccccccccccccccccccc" // 精确 24 字节
)

// ──────────────────────────── 非导出函数测试 ────────────────────────────

// TestWriteID 测试 writeID
func TestWriteID(t *testing.T) {
	result := writeID("", testID1)
	if result != testID1 {
		t.Errorf("writeID() = %q, want %q", result, testID1)
	}
	result = writeID(testID1, testID2)
	if result != testID1+testID2 {
		t.Errorf("writeID() 追加失败")
	}
}

// TestDeleteIDByValue 测试 deleteIDByValue
func TestDeleteIDByValue(t *testing.T) {
	dataList := writeID(writeID("", testID1), testID2)
	result := deleteIDByValue(dataList, testID1)
	if result != testID2 {
		t.Errorf("deleteIDByValue() = %q, want %q", result, testID2)
	}
	// 删除不存在的 ID 应返回原列表
	result = deleteIDByValue(dataList, testID3)
	if result != dataList {
		t.Errorf("deleteIDByValue() 删除不存在的 ID 应返回原列表")
	}
}

// TestGetAllIDs 测试 getAllIDs
func TestGetAllIDs(t *testing.T) {
	dataList := writeID(writeID("", testID1), testID2)
	ids := getAllIDs(dataList)
	if len(ids) != 2 {
		t.Errorf("getAllIDs() 返回 %d 项, want 2", len(ids))
	}
	if ids[0] != testID1 || ids[1] != testID2 {
		t.Errorf("getAllIDs() = %v, want [%q, %q]", ids, testID1, testID2)
	}
}

// TestGetAllIDs_空列表 测试空列表
func TestGetAllIDs_空列表(t *testing.T) {
	ids := getAllIDs("")
	if len(ids) != 0 {
		t.Errorf("getAllIDs(空) 返回 %d 项, want 0", len(ids))
	}
}

// TestGetIDsInRange 测试 getIDsInRange
func TestGetIDsInRange(t *testing.T) {
	dataList := writeID(writeID(writeID("", testID1), testID2), testID3)
	ids := getIDsInRange(dataList, 1, 3)
	if len(ids) != 2 {
		t.Errorf("getIDsInRange(1,3) 返回 %d 项, want 2", len(ids))
	}
	if ids[0] != testID2 || ids[1] != testID3 {
		t.Errorf("getIDsInRange(1,3) = %v, want [%q, %q]", ids, testID2, testID3)
	}
}

// TestGetIDsInRange_越界 测试越界处理
func TestGetIDsInRange_越界(t *testing.T) {
	dataList := writeID("", testID1)
	// endIdx 超出范围
	ids := getIDsInRange(dataList, 0, 10)
	if len(ids) != 1 {
		t.Errorf("getIDsInRange(0,10) 返回 %d 项, want 1", len(ids))
	}
	// startIdx > endIdx
	ids = getIDsInRange(dataList, 5, 1)
	if len(ids) != 0 {
		t.Errorf("getIDsInRange(5,1) 返回 %d 项, want 0", len(ids))
	}
}

// ──────────────────────────── UserMemStore 测试 ────────────────────────────

// newTestUserMemStore 创建测试用 UserMemStore
func newTestUserMemStore(t *testing.T) *UserMemStore {
	t.Helper()
	store, err := NewUserMemStore(kv.NewInMemoryKVStore())
	if err != nil {
		t.Fatalf("NewUserMemStore() error = %v", err)
	}
	return store
}

// TestNewUserMemStore 测试创建 UserMemStore
func TestNewUserMemStore(t *testing.T) {
	store, err := NewUserMemStore(kv.NewInMemoryKVStore())
	if err != nil {
		t.Fatalf("NewUserMemStore() error = %v", err)
	}
	if store == nil {
		t.Fatal("NewUserMemStore() 返回 nil")
	}
}

// TestNewUserMemStore_nilKVStore 测试 nil kvStore 报错
func TestNewUserMemStore_nilKVStore(t *testing.T) {
	_, err := NewUserMemStore(nil)
	if err == nil {
		t.Error("nil kvStore 应返回错误")
	}
}

// TestUserMemStore_Write 测试写入
func TestUserMemStore_Write(t *testing.T) {
	store := newTestUserMemStore(t)
	ctx := context.Background()
	data := map[string]any{"mem_type": "user_profile", "content": "test"}
	ok, err := store.Write(ctx, "user1", "scope1", testID1, data)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if !ok {
		t.Error("Write() 应返回 true")
	}
}

// TestUserMemStore_Write_已存在 测试写入已存在的记忆返回 false
func TestUserMemStore_Write_已存在(t *testing.T) {
	store := newTestUserMemStore(t)
	ctx := context.Background()
	data := map[string]any{"mem_type": "user_profile", "content": "test"}
	_, _ = store.Write(ctx, "user1", "scope1", testID1, data)
	ok, err := store.Write(ctx, "user1", "scope1", testID1, data)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if ok {
		t.Error("Write() 已存在时应返回 false")
	}
}

// TestUserMemStore_Write_空数据 测试空数据返回 false
func TestUserMemStore_Write_空数据(t *testing.T) {
	store := newTestUserMemStore(t)
	ctx := context.Background()
	ok, err := store.Write(ctx, "user1", "scope1", testID1, map[string]any{})
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if ok {
		t.Error("Write() 空数据应返回 false")
	}
}

// TestUserMemStore_Write_索引更新 测试写入后索引更新
func TestUserMemStore_Write_索引更新(t *testing.T) {
	store := newTestUserMemStore(t)
	ctx := context.Background()
	data := map[string]any{"mem_type": "user_profile", "content": "test"}
	_, _ = store.Write(ctx, "user1", "scope1", testID1, data)

	// 验证 getAll 能读取
	result, err := store.GetAll(ctx, "user1", "scope1", "user_profile")
	if err != nil {
		t.Fatalf("GetAll() error = %v", err)
	}
	if result == nil || len(result) != 1 {
		t.Errorf("GetAll() 返回 %v, want 1 项", result)
	}
}

// TestUserMemStore_Update 测试更新
func TestUserMemStore_Update(t *testing.T) {
	store := newTestUserMemStore(t)
	ctx := context.Background()
	data := map[string]any{"mem_type": "user_profile", "content": "test"}
	_, _ = store.Write(ctx, "user1", "scope1", testID1, data)

	ok, err := store.Update(ctx, "user1", "scope1", testID1, map[string]any{"content": "updated"})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if !ok {
		t.Error("Update() 应返回 true")
	}

	result, _ := store.Get(ctx, "user1", "scope1", testID1)
	if result["content"] != "updated" {
		t.Errorf("Update 后 content = %v, want %q", result["content"], "updated")
	}
}

// TestUserMemStore_Update_不存在 测试更新不存在的记忆返回 false
func TestUserMemStore_Update_不存在(t *testing.T) {
	store := newTestUserMemStore(t)
	ctx := context.Background()
	ok, err := store.Update(ctx, "user1", "scope1", testID4, map[string]any{"content": "test"})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if ok {
		t.Error("Update() 不存在时应返回 false")
	}
}

// TestUserMemStore_Delete 测试删除
func TestUserMemStore_Delete(t *testing.T) {
	store := newTestUserMemStore(t)
	ctx := context.Background()
	data := map[string]any{"mem_type": "user_profile", "content": "test"}
	_, _ = store.Write(ctx, "user1", "scope1", testID1, data)

	err := store.Delete(ctx, "user1", "scope1", testID1)
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	result, _ := store.Get(ctx, "user1", "scope1", testID1)
	if result != nil {
		t.Error("Delete 后 Get 应返回 nil")
	}
}

// TestUserMemStore_Delete_不存在 测试删除不存在的记忆不报错
func TestUserMemStore_Delete_不存在(t *testing.T) {
	store := newTestUserMemStore(t)
	ctx := context.Background()
	err := store.Delete(ctx, "user1", "scope1", testID4)
	if err != nil {
		t.Fatalf("Delete() 不存在时不应报错, error = %v", err)
	}
}

// TestUserMemStore_BatchDelete 测试批量删除
func TestUserMemStore_BatchDelete(t *testing.T) {
	store := newTestUserMemStore(t)
	ctx := context.Background()
	_, _ = store.Write(ctx, "user1", "scope1", testID1, map[string]any{"mem_type": "user_profile", "content": "test1"})
	_, _ = store.Write(ctx, "user1", "scope1", testID2, map[string]any{"mem_type": "user_profile", "content": "test2"})

	err := store.BatchDelete(ctx, "user1", "scope1", []string{testID1, testID2})
	if err != nil {
		t.Fatalf("BatchDelete() error = %v", err)
	}
}

// TestUserMemStore_Get 测试获取
func TestUserMemStore_Get(t *testing.T) {
	store := newTestUserMemStore(t)
	ctx := context.Background()
	data := map[string]any{"mem_type": "user_profile", "content": "test"}
	_, _ = store.Write(ctx, "user1", "scope1", testID1, data)

	result, err := store.Get(ctx, "user1", "scope1", testID1)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if result == nil {
		t.Fatal("Get() 返回 nil")
	}
	if result["content"] != "test" {
		t.Errorf("Get() content = %v, want %q", result["content"], "test")
	}
}

// TestUserMemStore_Get_不存在 测试获取不存在的记忆返回 nil
func TestUserMemStore_Get_不存在(t *testing.T) {
	store := newTestUserMemStore(t)
	ctx := context.Background()
	result, err := store.Get(ctx, "user1", "scope1", testID4)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if result != nil {
		t.Error("Get() 不存在时应返回 nil")
	}
}

// TestUserMemStore_BatchGet 测试批量获取
func TestUserMemStore_BatchGet(t *testing.T) {
	store := newTestUserMemStore(t)
	ctx := context.Background()
	_, _ = store.Write(ctx, "user1", "scope1", testID1, map[string]any{"content": "test1"})
	_, _ = store.Write(ctx, "user1", "scope1", testID2, map[string]any{"content": "test2"})

	result, err := store.BatchGet(ctx, "user1", "scope1", []string{testID1, testID2})
	if err != nil {
		t.Fatalf("BatchGet() error = %v", err)
	}
	if len(result) != 2 {
		t.Errorf("BatchGet() 返回 %d 项, want 2", len(result))
	}
}

// TestUserMemStore_GetAll 测试获取全部
func TestUserMemStore_GetAll(t *testing.T) {
	store := newTestUserMemStore(t)
	ctx := context.Background()
	_, _ = store.Write(ctx, "user1", "scope1", testID1, map[string]any{"mem_type": "user_profile", "content": "test1"})
	_, _ = store.Write(ctx, "user1", "scope1", testID2, map[string]any{"mem_type": "summary", "content": "test2"})

	// 获取全部
	result, err := store.GetAll(ctx, "user1", "scope1", "")
	if err != nil {
		t.Fatalf("GetAll() error = %v", err)
	}
	if result == nil || len(result) != 2 {
		t.Errorf("GetAll(全部) 返回 %d 项, want 2", len(result))
	}

	// 按类型获取
	result, err = store.GetAll(ctx, "user1", "scope1", "user_profile")
	if err != nil {
		t.Fatalf("GetAll(user_profile) error = %v", err)
	}
	if result == nil || len(result) != 1 {
		t.Errorf("GetAll(user_profile) 返回 %d 项, want 1", len(result))
	}
}

// TestUserMemStore_GetAll_空结果 测试空结果返回 nil
func TestUserMemStore_GetAll_空结果(t *testing.T) {
	store := newTestUserMemStore(t)
	ctx := context.Background()
	result, err := store.GetAll(ctx, "nonexist", "nonexist", "")
	if err != nil {
		t.Fatalf("GetAll() error = %v", err)
	}
	if result != nil {
		t.Error("GetAll() 无数据时应返回 nil")
	}
}

// TestUserMemStore_GetInRange 测试范围获取
func TestUserMemStore_GetInRange(t *testing.T) {
	store := newTestUserMemStore(t)
	ctx := context.Background()
	_, _ = store.Write(ctx, "user1", "scope1", testID1, map[string]any{"content": "test1"})
	_, _ = store.Write(ctx, "user1", "scope1", testID2, map[string]any{"content": "test2"})
	_, _ = store.Write(ctx, "user1", "scope1", testID3, map[string]any{"content": "test3"})

	result, err := store.GetInRange(ctx, "user1", "scope1", 1, 3, "")
	if err != nil {
		t.Fatalf("GetInRange() error = %v", err)
	}
	if len(result) != 2 {
		t.Errorf("GetInRange(1,3) 返回 %d 项, want 2", len(result))
	}
}

// TestUserMemStore_GetInRange_不存在 测试不存在的键返回 nil
func TestUserMemStore_GetInRange_不存在(t *testing.T) {
	store := newTestUserMemStore(t)
	ctx := context.Background()
	result, err := store.GetInRange(ctx, "nonexist", "nonexist", 0, 10, "")
	if err != nil {
		t.Fatalf("GetInRange() error = %v", err)
	}
	if result != nil {
		t.Error("GetInRange() 不存在时应返回 nil")
	}
}
