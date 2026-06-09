package kv

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// newTestDbBasedKVStore 创建用于测试的 DbBasedKVStore 实例，使用 SQLite :memory: 数据库。
func newTestDbBasedKVStore(t *testing.T) *DbBasedKVStore {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开 SQLite 内存数据库失败: %v", err)
	}
	return NewDbBasedKVStore(db)
}

// ──── 构造函数测试 ────

// TestNewDbBasedKVStore 验证构造函数创建非 nil 实例。
func TestNewDbBasedKVStore(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	if store == nil {
		t.Fatal("NewDbBasedKVStore 返回 nil")
	}
	if store.db == nil {
		t.Fatal("内部 db 未初始化")
	}
}

// ──── 接口满足验证 ────

// TestDbBasedKVStore_接口满足 验证 DbBasedKVStore 满足 BaseKVStore 接口。
func TestDbBasedKVStore_接口满足(t *testing.T) {
	var _ BaseKVStore = (*DbBasedKVStore)(nil)
}

// ──── 惰性建表测试 ────

// TestDbBasedKVStore_惰性建表 验证首次操作时自动建表。
func TestDbBasedKVStore_惰性建表(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	ctx := context.Background()

	// 初始状态：未建表
	if store.tableCreated.Load() {
		t.Error("初始状态 tableCreated 应为 false")
	}

	// 执行 Set 触发建表
	err := store.Set(ctx, "key1", []byte("value1"))
	if err != nil {
		t.Fatalf("Set 返回错误: %v", err)
	}

	// 建表后标志应为 true
	if !store.tableCreated.Load() {
		t.Error("Set 后 tableCreated 应为 true")
	}

	// 验证表存在：查询应成功
	var count int64
	store.db.Model(&KVStoreRow{}).Count(&count)
	if count != 1 {
		t.Errorf("建表后记录数 = %d, 期望 1", count)
	}
}

// ──── Set / Get 测试 ────

// TestDbBasedKVStore_SetGet 基本 Set + Get 往返。
func TestDbBasedKVStore_SetGet(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	ctx := context.Background()

	err := store.Set(ctx, "key1", []byte("value1"))
	if err != nil {
		t.Fatalf("Set 返回错误: %v", err)
	}

	val, err := store.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Get 返回错误: %v", err)
	}
	if string(val) != "value1" {
		t.Errorf("Get 返回 %q, 期望 %q", string(val), "value1")
	}
}

// TestDbBasedKVStore_Get_不存在 验证获取不存在的 key 返回 nil。
func TestDbBasedKVStore_Get_不存在(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	ctx := context.Background()

	val, err := store.Get(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("Get 返回错误: %v", err)
	}
	if val != nil {
		t.Errorf("Get 返回 %v, 期望 nil", val)
	}
}

// TestDbBasedKVStore_Set_覆盖 验证 Set 覆盖已有值。
func TestDbBasedKVStore_Set_覆盖(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	ctx := context.Background()

	_ = store.Set(ctx, "key1", []byte("old"))
	_ = store.Set(ctx, "key1", []byte("new"))

	val, _ := store.Get(ctx, "key1")
	if string(val) != "new" {
		t.Errorf("覆盖后 Get 返回 %q, 期望 %q", string(val), "new")
	}
}

// TestDbBasedKVStore_Set_二进制值 验证 Set/Get 二进制数据往返。
func TestDbBasedKVStore_Set_二进制值(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	ctx := context.Background()

	// 包含 NULL 字节和其他非文本字节的值
	original := []byte{0x00, 0x01, 0x02, 0xFF, 0xFE, 'h', 'e', 'l', 'l', 'o'}
	err := store.Set(ctx, "bin_key", original)
	if err != nil {
		t.Fatalf("Set 返回错误: %v", err)
	}

	val, err := store.Get(ctx, "bin_key")
	if err != nil {
		t.Fatalf("Get 返回错误: %v", err)
	}
	if len(val) != len(original) {
		t.Fatalf("Get 返回长度 %d, 期望 %d", len(val), len(original))
	}
	for i, b := range val {
		if b != original[i] {
			t.Errorf("Get[%d] = 0x%02X, 期望 0x%02X", i, b, original[i])
		}
	}
}

// ──── Exists 测试 ────

// TestDbBasedKVStore_Exists_存在 验证 key 存在时 Exists 返回 true。
func TestDbBasedKVStore_Exists_存在(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	ctx := context.Background()

	_ = store.Set(ctx, "key1", []byte("value1"))

	exists, err := store.Exists(ctx, "key1")
	if err != nil {
		t.Fatalf("Exists 返回错误: %v", err)
	}
	if !exists {
		t.Error("Exists 返回 false, 期望 true")
	}
}

// TestDbBasedKVStore_Exists_不存在 验证 key 不存在时 Exists 返回 false。
func TestDbBasedKVStore_Exists_不存在(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	ctx := context.Background()

	exists, err := store.Exists(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("Exists 返回错误: %v", err)
	}
	if exists {
		t.Error("Exists 返回 true, 期望 false")
	}
}

// ──── Delete 测试 ────

// TestDbBasedKVStore_Delete 验证删除后 Get 返回 nil。
func TestDbBasedKVStore_Delete(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	ctx := context.Background()

	_ = store.Set(ctx, "key1", []byte("value1"))
	_ = store.Delete(ctx, "key1")

	val, _ := store.Get(ctx, "key1")
	if val != nil {
		t.Errorf("删除后 Get 返回 %v, 期望 nil", val)
	}
}

// TestDbBasedKVStore_Delete_不存在 验证删除不存在的 key 不报错。
func TestDbBasedKVStore_Delete_不存在(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	ctx := context.Background()

	err := store.Delete(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("删除不存在的 key 返回错误: %v", err)
	}
}

// ──── ExclusiveSet 测试 ────

// TestDbBasedKVStore_ExclusiveSet_新key 验证新 key 设置成功。
func TestDbBasedKVStore_ExclusiveSet_新key(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	ctx := context.Background()

	ok, err := store.ExclusiveSet(ctx, "key1", []byte("value1"), 0)
	if err != nil {
		t.Fatalf("ExclusiveSet 返回错误: %v", err)
	}
	if !ok {
		t.Error("ExclusiveSet 返回 false, 期望 true")
	}

	val, _ := store.Get(ctx, "key1")
	if string(val) != "value1" {
		t.Errorf("ExclusiveSet 后 Get 返回 %q, 期望 %q", string(val), "value1")
	}
}

// TestDbBasedKVStore_ExclusiveSet_已存在拒绝 验证 key 已存在且未过期时返回 false。
func TestDbBasedKVStore_ExclusiveSet_已存在拒绝(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	ctx := context.Background()

	_ = store.Set(ctx, "key1", []byte("value1"))
	ok, err := store.ExclusiveSet(ctx, "key1", []byte("value2"), 0)
	if err != nil {
		t.Fatalf("ExclusiveSet 返回错误: %v", err)
	}
	if ok {
		t.Error("ExclusiveSet 返回 true, 期望 false")
	}

	// 原值不变
	val, _ := store.Get(ctx, "key1")
	if string(val) != "value1" {
		t.Errorf("拒绝后 Get 返回 %q, 期望 %q", string(val), "value1")
	}
}

// TestDbBasedKVStore_ExclusiveSet_已过期允许覆盖 验证 key 已过期时允许覆盖。
func TestDbBasedKVStore_ExclusiveSet_已过期允许覆盖(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	ctx := context.Background()

	// 设置 1 秒后过期的 key
	ok, _ := store.ExclusiveSet(ctx, "key1", []byte("value1"), 1)
	if !ok {
		t.Fatal("首次 ExclusiveSet 应返回 true")
	}

	// 等待过期
	time.Sleep(2 * time.Second)

	// 已过期的 key 允许覆盖
	ok, err := store.ExclusiveSet(ctx, "key1", []byte("value2"), 0)
	if err != nil {
		t.Fatalf("ExclusiveSet 返回错误: %v", err)
	}
	if !ok {
		t.Error("已过期 key 的 ExclusiveSet 返回 false, 期望 true")
	}

	val, _ := store.Get(ctx, "key1")
	if string(val) != "value2" {
		t.Errorf("覆盖后 Get 返回 %q, 期望 %q", string(val), "value2")
	}
}

// TestDbBasedKVStore_ExclusiveSet_expiry为零 验证 expiry=0 时永不过期。
func TestDbBasedKVStore_ExclusiveSet_expiry为零(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	ctx := context.Background()

	ok, err := store.ExclusiveSet(ctx, "key1", []byte("value1"), 0)
	if err != nil {
		t.Fatalf("ExclusiveSet 返回错误: %v", err)
	}
	if !ok {
		t.Error("ExclusiveSet 返回 false, 期望 true")
	}

	// 验证再次 ExclusiveSet 同一 key 会被拒绝（永不过期）
	ok2, _ := store.ExclusiveSet(ctx, "key1", []byte("value2"), 0)
	if ok2 {
		t.Error("expiry=0 的 key 应永不过期，第二次 ExclusiveSet 应返回 false")
	}
}

// TestDbBasedKVStore_ExclusiveSet_带expiry 验证带过期时间的设置。
func TestDbBasedKVStore_ExclusiveSet_带expiry(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	ctx := context.Background()

	ok, _ := store.ExclusiveSet(ctx, "key1", []byte("value1"), 60)
	if !ok {
		t.Fatal("ExclusiveSet 应返回 true")
	}

	// 验证 key 存在
	exists, _ := store.Exists(ctx, "key1")
	if !exists {
		t.Error("设置后 Exists 返回 false, 期望 true")
	}

	// 验证内部值是 exclusive JSON 格式
	var row KVStoreRow
	store.db.Where("key = ?", "key1").First(&row)
	var ev exclusiveValue
	if err := json.Unmarshal(row.Value, &ev); err != nil {
		t.Fatalf("内部值不是有效 JSON: %v", err)
	}
	if ev.ExclusiveExpiry == 0 {
		t.Error("ExclusiveExpiry 为 0, 期望非零")
	}
}

// TestDbBasedKVStore_ExclusiveSet_手动修改过期时间 验证手动将过期时间设为过去后可覆盖。
func TestDbBasedKVStore_ExclusiveSet_手动修改过期时间(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	ctx := context.Background()

	// 设置带过期的 key
	ok, _ := store.ExclusiveSet(ctx, "key1", []byte("value1"), 60)
	if !ok {
		t.Fatal("ExclusiveSet 应返回 true")
	}

	// 手动修改过期时间为过去
	var row KVStoreRow
	store.db.Where("key = ?", "key1").First(&row)
	var ev exclusiveValue
	json.Unmarshal(row.Value, &ev)
	ev.ExclusiveExpiry = 1 // 1970-01-01，已过期
	newValue, _ := json.Marshal(ev)
	store.db.Model(&KVStoreRow{}).Where("key = ?", "key1").Update("value", newValue)

	// 已过期，允许覆盖
	ok, err := store.ExclusiveSet(ctx, "key1", []byte("value2"), 0)
	if err != nil {
		t.Fatalf("ExclusiveSet 返回错误: %v", err)
	}
	if !ok {
		t.Error("已过期 key 的 ExclusiveSet 返回 false, 期望 true")
	}
}

// TestDbBasedKVStore_Get_解包Exclusive值 验证 Get 解包 exclusive-set 的值返回实际 []byte。
func TestDbBasedKVStore_Get_解包Exclusive值(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	ctx := context.Background()

	ok, err := store.ExclusiveSet(ctx, "key1", []byte("exclusive_value"), 0)
	if err != nil {
		t.Fatalf("ExclusiveSet 返回错误: %v", err)
	}
	if !ok {
		t.Fatal("ExclusiveSet 返回 false, 期望 true")
	}

	// Get 应解包，返回实际值而非 JSON 包装
	val, err := store.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Get 返回错误: %v", err)
	}
	if string(val) != "exclusive_value" {
		t.Errorf("Get 返回 %q, 期望 %q", string(val), "exclusive_value")
	}

	// 验证返回的不是 JSON 字节
	var ev exclusiveValue
	if err := json.Unmarshal(val, &ev); err == nil && ev.ExclusiveValue != "" {
		t.Error("Get 返回了 JSON 包装结构，应返回解包后的实际值")
	}
}

// ──── GetByPrefix 测试 ────

// TestDbBasedKVStore_GetByPrefix_正常 验证按前缀获取键值对。
func TestDbBasedKVStore_GetByPrefix_正常(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	ctx := context.Background()

	_ = store.Set(ctx, "user:1", []byte("alice"))
	_ = store.Set(ctx, "user:2", []byte("bob"))
	_ = store.Set(ctx, "item:1", []byte("book"))

	result, err := store.GetByPrefix(ctx, "user:")
	if err != nil {
		t.Fatalf("GetByPrefix 返回错误: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("GetByPrefix 返回 %d 条, 期望 2 条", len(result))
	}
	if string(result["user:1"]) != "alice" {
		t.Errorf("user:1 = %q, 期望 %q", string(result["user:1"]), "alice")
	}
	if string(result["user:2"]) != "bob" {
		t.Errorf("user:2 = %q, 期望 %q", string(result["user:2"]), "bob")
	}
}

// TestDbBasedKVStore_GetByPrefix_无匹配 验证无匹配前缀返回空 map。
func TestDbBasedKVStore_GetByPrefix_无匹配(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	ctx := context.Background()

	_ = store.Set(ctx, "user:1", []byte("alice"))

	result, err := store.GetByPrefix(ctx, "item:")
	if err != nil {
		t.Fatalf("GetByPrefix 返回错误: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("GetByPrefix 返回 %d 条, 期望 0 条", len(result))
	}
}

// ──── DeleteByPrefix 测试 ────

// TestDbBasedKVStore_DeleteByPrefix_一次性 验证 batchSize=0 时一次性删除。
func TestDbBasedKVStore_DeleteByPrefix_一次性(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	ctx := context.Background()

	_ = store.Set(ctx, "user:1", []byte("alice"))
	_ = store.Set(ctx, "user:2", []byte("bob"))
	_ = store.Set(ctx, "item:1", []byte("book"))

	err := store.DeleteByPrefix(ctx, "user:", 0)
	if err != nil {
		t.Fatalf("DeleteByPrefix 返回错误: %v", err)
	}

	exists, _ := store.Exists(ctx, "user:1")
	if exists {
		t.Error("删除后 user:1 仍存在")
	}
	exists, _ = store.Exists(ctx, "item:1")
	if !exists {
		t.Error("item:1 不应被删除")
	}
}

// TestDbBasedKVStore_DeleteByPrefix_分批 验证 batchSize>0 时分批删除。
func TestDbBasedKVStore_DeleteByPrefix_分批(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	ctx := context.Background()

	_ = store.Set(ctx, "user:1", []byte("alice"))
	_ = store.Set(ctx, "user:2", []byte("bob"))
	_ = store.Set(ctx, "user:3", []byte("charlie"))

	err := store.DeleteByPrefix(ctx, "user:", 2)
	if err != nil {
		t.Fatalf("DeleteByPrefix 返回错误: %v", err)
	}

	result, _ := store.GetByPrefix(ctx, "user:")
	if len(result) != 0 {
		t.Errorf("分批删除后仍有 %d 条 user: 前缀的 key", len(result))
	}
}

// TestDbBasedKVStore_DeleteByPrefix_无匹配 验证无匹配前缀不报错。
func TestDbBasedKVStore_DeleteByPrefix_无匹配(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	ctx := context.Background()

	_ = store.Set(ctx, "user:1", []byte("alice"))

	err := store.DeleteByPrefix(ctx, "item:", 0)
	if err != nil {
		t.Fatalf("DeleteByPrefix 返回错误: %v", err)
	}

	exists, _ := store.Exists(ctx, "user:1")
	if !exists {
		t.Error("user:1 不应被删除")
	}
}

// ──── MGet 测试 ────

// TestDbBasedKVStore_MGet_正常 验证批量获取多个 key。
func TestDbBasedKVStore_MGet_正常(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	ctx := context.Background()

	_ = store.Set(ctx, "k1", []byte("v1"))
	_ = store.Set(ctx, "k2", []byte("v2"))

	values, err := store.MGet(ctx, []string{"k1", "k2"})
	if err != nil {
		t.Fatalf("MGet 返回错误: %v", err)
	}
	if len(values) != 2 {
		t.Fatalf("MGet 返回 %d 条, 期望 2 条", len(values))
	}
	if string(values[0]) != "v1" {
		t.Errorf("values[0] = %q, 期望 %q", string(values[0]), "v1")
	}
	if string(values[1]) != "v2" {
		t.Errorf("values[1] = %q, 期望 %q", string(values[1]), "v2")
	}
}

// TestDbBasedKVStore_MGet_空列表 验证传入空 key 列表时返回空切片。
func TestDbBasedKVStore_MGet_空列表(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	ctx := context.Background()

	values, err := store.MGet(ctx, []string{})
	if err != nil {
		t.Fatalf("MGet 返回错误: %v", err)
	}
	if len(values) != 0 {
		t.Errorf("MGet 返回 %d 条, 期望 0 条", len(values))
	}
}

// TestDbBasedKVStore_MGet_部分不存在 验证部分 key 不存在时对应位置为 nil。
func TestDbBasedKVStore_MGet_部分不存在(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	ctx := context.Background()

	_ = store.Set(ctx, "k1", []byte("v1"))

	values, _ := store.MGet(ctx, []string{"k1", "k2"})
	if len(values) != 2 {
		t.Fatalf("MGet 返回 %d 条, 期望 2 条", len(values))
	}
	if string(values[0]) != "v1" {
		t.Errorf("values[0] = %q, 期望 %q", string(values[0]), "v1")
	}
	if values[1] != nil {
		t.Errorf("values[1] = %v, 期望 nil", values[1])
	}
}

// ──── BatchDelete 测试 ────

// TestDbBasedKVStore_BatchDelete_正常 验证批量删除并返回删除数量。
func TestDbBasedKVStore_BatchDelete_正常(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	ctx := context.Background()

	_ = store.Set(ctx, "k1", []byte("v1"))
	_ = store.Set(ctx, "k2", []byte("v2"))
	_ = store.Set(ctx, "k3", []byte("v3"))

	deleted, err := store.BatchDelete(ctx, []string{"k1", "k3"}, 0)
	if err != nil {
		t.Fatalf("BatchDelete 返回错误: %v", err)
	}
	if deleted != 2 {
		t.Errorf("BatchDelete 返回 %d, 期望 2", deleted)
	}

	exists, _ := store.Exists(ctx, "k1")
	if exists {
		t.Error("k1 删除后仍存在")
	}
	exists, _ = store.Exists(ctx, "k2")
	if !exists {
		t.Error("k2 不应被删除")
	}
}

// TestDbBasedKVStore_BatchDelete_空列表 验证空列表返回 0。
func TestDbBasedKVStore_BatchDelete_空列表(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	ctx := context.Background()

	deleted, err := store.BatchDelete(ctx, []string{}, 0)
	if err != nil {
		t.Fatalf("BatchDelete 返回错误: %v", err)
	}
	if deleted != 0 {
		t.Errorf("BatchDelete 返回 %d, 期望 0", deleted)
	}
}

// TestDbBasedKVStore_BatchDelete_分批 验证分批删除。
func TestDbBasedKVStore_BatchDelete_分批(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	ctx := context.Background()

	_ = store.Set(ctx, "k1", []byte("v1"))
	_ = store.Set(ctx, "k2", []byte("v2"))
	_ = store.Set(ctx, "k3", []byte("v3"))

	deleted, _ := store.BatchDelete(ctx, []string{"k1", "k2", "k3"}, 2)
	if deleted != 3 {
		t.Errorf("BatchDelete 返回 %d, 期望 3", deleted)
	}

	result, _ := store.GetByPrefix(ctx, "k")
	if len(result) != 0 {
		t.Errorf("分批删除后仍有 %d 条 key", len(result))
	}
}

// TestDbBasedKVStore_BatchDelete_部分不存在 验证只删除存在的 key。
func TestDbBasedKVStore_BatchDelete_部分不存在(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	ctx := context.Background()

	_ = store.Set(ctx, "k1", []byte("v1"))

	deleted, _ := store.BatchDelete(ctx, []string{"k1", "k2"}, 0)
	if deleted != 1 {
		t.Errorf("BatchDelete 返回 %d, 期望 1", deleted)
	}
}

// ──── Pipeline 测试 ────

// TestDbBasedKVStore_Pipeline_混合操作 验证 Pipeline 混合 set+get+exists 操作。
func TestDbBasedKVStore_Pipeline_混合操作(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	ctx := context.Background()

	// 先设置一个 key
	_ = store.Set(ctx, "existing", []byte("old_value"))

	// 创建 Pipeline 并执行混合操作
	pipe := store.Pipeline(ctx)
	_ = pipe.Set(ctx, "new_key", []byte("new_value"), 0)
	_ = pipe.Get(ctx, "existing")
	_ = pipe.Exists(ctx, "nonexistent")

	results, err := pipe.Execute(ctx)
	if err != nil {
		t.Fatalf("Pipeline Execute 返回错误: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("Pipeline 返回 %d 条结果, 期望 3 条", len(results))
	}

	// 验证 Set 结果
	if results[0].Op != "set" || results[0].Key != "new_key" {
		t.Errorf("results[0] = {Op: %q, Key: %q}, 期望 {Op: %q, Key: %q}", results[0].Op, results[0].Key, "set", "new_key")
	}

	// 验证 Get 结果
	if results[1].Op != "get" || string(results[1].Value) != "old_value" {
		t.Errorf("results[1] = {Op: %q, Value: %q}, 期望 {Op: %q, Value: %q}", results[1].Op, string(results[1].Value), "get", "old_value")
	}

	// 验证 Exists 结果
	if results[2].Op != "exists" || results[2].Exists {
		t.Errorf("results[2] = {Op: %q, Exists: %v}, 期望 {Op: %q, Exists: false}", results[2].Op, results[2].Exists, "exists")
	}

	// 验证 Pipeline Set 实际写入了 store
	val, _ := store.Get(ctx, "new_key")
	if string(val) != "new_value" {
		t.Errorf("Pipeline Set 后 Get 返回 %q, 期望 %q", string(val), "new_value")
	}
}

// TestDbBasedKVStore_Pipeline_复用 验证 Execute 后 Pipeline 可复用。
func TestDbBasedKVStore_Pipeline_复用(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	ctx := context.Background()

	pipe := store.Pipeline(ctx)

	// 第一次使用
	_ = pipe.Set(ctx, "k1", []byte("v1"), 0)
	results1, err := pipe.Execute(ctx)
	if err != nil {
		t.Fatalf("第一次 Execute 返回错误: %v", err)
	}
	if len(results1) != 1 {
		t.Errorf("第一次 Execute 返回 %d 条结果, 期望 1 条", len(results1))
	}

	// 第二次使用（复用同一个 Pipeline）
	_ = pipe.Set(ctx, "k2", []byte("v2"), 0)
	_ = pipe.Get(ctx, "k1")
	results2, err := pipe.Execute(ctx)
	if err != nil {
		t.Fatalf("第二次 Execute 返回错误: %v", err)
	}
	if len(results2) != 2 {
		t.Errorf("第二次 Execute 返回 %d 条结果, 期望 2 条", len(results2))
	}

	// 验证第二次的 Get 结果
	if results2[1].Op != "get" || string(results2[1].Value) != "v1" {
		t.Errorf("复用 Pipeline Get 结果 = {Op: %q, Value: %q}, 期望 {Op: %q, Value: %q}", results2[1].Op, string(results2[1].Value), "get", "v1")
	}
}

// TestDbBasedKVStore_Pipeline_空操作 验证空 Pipeline Execute 返回空切片。
func TestDbBasedKVStore_Pipeline_空操作(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	ctx := context.Background()

	pipe := store.Pipeline(ctx)
	results, err := pipe.Execute(ctx)
	if err != nil {
		t.Fatalf("空 Pipeline Execute 返回错误: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("空 Pipeline Execute 返回 %d 条结果, 期望 0 条", len(results))
	}
}

// ──── 并发安全测试 ────

// TestDbBasedKVStore_并发安全 验证多 goroutine 并发读写无 race。
func TestDbBasedKVStore_并发安全(t *testing.T) {
	store := newTestDbBasedKVStore(t)
	ctx := context.Background()

	const goroutines = 20
	const opsPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(goroutines * 4) // 4 组 goroutine: Set, Get, Delete, ExclusiveSet

	// 并发 Set
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				key := fmt.Sprintf("set:%d:%d", id, j)
				_ = store.Set(ctx, key, []byte("value"))
			}
		}(i)
	}

	// 并发 Get
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				key := fmt.Sprintf("set:%d:%d", id, j)
				_, _ = store.Get(ctx, key)
			}
		}(i)
	}

	// 并发 Delete
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				key := fmt.Sprintf("set:%d:%d", id, j)
				_ = store.Delete(ctx, key)
			}
		}(i)
	}

	// 并发 ExclusiveSet
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				key := fmt.Sprintf("exclusive:%d:%d", id, j)
				_, _ = store.ExclusiveSet(ctx, key, []byte("value"), 0)
			}
		}(i)
	}

	wg.Wait()
}
