package mem_model

import (
	"context"
	"encoding/json"
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

// ──────────────────────────── UserMemoryRecord 测试 ────────────────────────────

// TestUserMemoryRecord_JSON序列化 测试 UserMemoryRecord JSON 序列化/反序列化
func TestUserMemoryRecord_JSON序列化(t *testing.T) {
	record := &UserMemoryRecord{
		ID:        testID1,
		Mem:       "测试内容",
		MemType:   "user_profile",
		Timestamp: "2025-07-15",
		Score:     0.95,
		SourceID:  "src-001",
		Metadata:  "meta-info",
	}

	// 序列化
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	// 反序列化
	var restored UserMemoryRecord
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if restored.ID != record.ID {
		t.Errorf("ID = %q, want %q", restored.ID, record.ID)
	}
	if restored.Mem != record.Mem {
		t.Errorf("Mem = %q, want %q", restored.Mem, record.Mem)
	}
	if restored.MemType != record.MemType {
		t.Errorf("MemType = %q, want %q", restored.MemType, record.MemType)
	}
	if restored.Timestamp != record.Timestamp {
		t.Errorf("Timestamp = %q, want %q", restored.Timestamp, record.Timestamp)
	}
	if restored.Score != record.Score {
		t.Errorf("Score = %v, want %v", restored.Score, record.Score)
	}
	if restored.SourceID != record.SourceID {
		t.Errorf("SourceID = %q, want %q", restored.SourceID, record.SourceID)
	}
	if restored.Metadata != record.Metadata {
		t.Errorf("Metadata = %q, want %q", restored.Metadata, record.Metadata)
	}
}

// TestUserMemoryRecord_omitempty 测试 omitempty 字段
func TestUserMemoryRecord_omitempty(t *testing.T) {
	record := &UserMemoryRecord{
		ID:      testID1,
		Mem:     "测试",
		MemType: "user_profile",
	}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	// omitempty 字段不应出现在 JSON 中
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if _, ok := m["timestamp"]; ok {
		t.Error("空 Timestamp 不应出现在 JSON 中")
	}
	if _, ok := m["score"]; ok {
		t.Error("零值 Score 不应出现在 JSON 中")
	}
	if _, ok := m["source_id"]; ok {
		t.Error("空 SourceID 不应出现在 JSON 中")
	}
	if _, ok := m["metadata"]; ok {
		t.Error("空 Metadata 不应出现在 JSON 中")
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
	record := &UserMemoryRecord{MemType: "user_profile", Mem: "test"}
	ok, err := store.Write(ctx, "user1", "scope1", testID1, record)
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
	record := &UserMemoryRecord{MemType: "user_profile", Mem: "test"}
	_, _ = store.Write(ctx, "user1", "scope1", testID1, record)
	ok, err := store.Write(ctx, "user1", "scope1", testID1, record)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if ok {
		t.Error("Write() 已存在时应返回 false")
	}
}

// TestUserMemStore_Write_nilRecord 测试 nil record 返回 false
func TestUserMemStore_Write_nilRecord(t *testing.T) {
	store := newTestUserMemStore(t)
	ctx := context.Background()
	ok, err := store.Write(ctx, "user1", "scope1", testID1, nil)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if ok {
		t.Error("Write() nil record 应返回 false")
	}
}

// TestUserMemStore_Write_索引更新 测试写入后索引更新
func TestUserMemStore_Write_索引更新(t *testing.T) {
	store := newTestUserMemStore(t)
	ctx := context.Background()
	record := &UserMemoryRecord{MemType: "user_profile", Mem: "test"}
	_, _ = store.Write(ctx, "user1", "scope1", testID1, record)

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
	record := &UserMemoryRecord{MemType: "user_profile", Mem: "test"}
	_, _ = store.Write(ctx, "user1", "scope1", testID1, record)

	ok, err := store.Update(ctx, "user1", "scope1", testID1, map[string]any{"mem": "updated"})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if !ok {
		t.Error("Update() 应返回 true")
	}

	result, _ := store.Get(ctx, "user1", "scope1", testID1)
	if result == nil {
		t.Fatal("Update 后 Get 返回 nil")
	}
	if result.Mem != "updated" {
		t.Errorf("Update 后 Mem = %q, want %q", result.Mem, "updated")
	}
}

// TestUserMemStore_Update_不存在 测试更新不存在的记忆返回 false
func TestUserMemStore_Update_不存在(t *testing.T) {
	store := newTestUserMemStore(t)
	ctx := context.Background()
	ok, err := store.Update(ctx, "user1", "scope1", testID4, map[string]any{"mem": "test"})
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
	record := &UserMemoryRecord{MemType: "user_profile", Mem: "test"}
	_, _ = store.Write(ctx, "user1", "scope1", testID1, record)

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
	_, _ = store.Write(ctx, "user1", "scope1", testID1, &UserMemoryRecord{MemType: "user_profile", Mem: "test1"})
	_, _ = store.Write(ctx, "user1", "scope1", testID2, &UserMemoryRecord{MemType: "user_profile", Mem: "test2"})

	err := store.BatchDelete(ctx, "user1", "scope1", []string{testID1, testID2})
	if err != nil {
		t.Fatalf("BatchDelete() error = %v", err)
	}
}

// TestUserMemStore_Get 测试获取
func TestUserMemStore_Get(t *testing.T) {
	store := newTestUserMemStore(t)
	ctx := context.Background()
	record := &UserMemoryRecord{MemType: "user_profile", Mem: "test"}
	_, _ = store.Write(ctx, "user1", "scope1", testID1, record)

	result, err := store.Get(ctx, "user1", "scope1", testID1)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if result == nil {
		t.Fatal("Get() 返回 nil")
	}
	if result.Mem != "test" {
		t.Errorf("Get() Mem = %q, want %q", result.Mem, "test")
	}
	if result.MemType != "user_profile" {
		t.Errorf("Get() MemType = %q, want %q", result.MemType, "user_profile")
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
	_, _ = store.Write(ctx, "user1", "scope1", testID1, &UserMemoryRecord{Mem: "test1"})
	_, _ = store.Write(ctx, "user1", "scope1", testID2, &UserMemoryRecord{Mem: "test2"})

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
	_, _ = store.Write(ctx, "user1", "scope1", testID1, &UserMemoryRecord{MemType: "user_profile", Mem: "test1"})
	_, _ = store.Write(ctx, "user1", "scope1", testID2, &UserMemoryRecord{MemType: "summary", Mem: "test2"})

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
	_, _ = store.Write(ctx, "user1", "scope1", testID1, &UserMemoryRecord{Mem: "test1"})
	_, _ = store.Write(ctx, "user1", "scope1", testID2, &UserMemoryRecord{Mem: "test2"})
	_, _ = store.Write(ctx, "user1", "scope1", testID3, &UserMemoryRecord{Mem: "test3"})

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
