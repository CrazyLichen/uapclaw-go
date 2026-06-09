package kv

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// ──── 构造函数测试 ────

// TestNewInMemoryKVStore 验证构造函数创建非 nil 实例。
func TestNewInMemoryKVStore(t *testing.T) {
	store := NewInMemoryKVStore()
	if store == nil {
		t.Fatal("NewInMemoryKVStore 返回 nil")
	}
	if store.store == nil {
		t.Fatal("内部 store 映射未初始化")
	}
}

// ──── Set / Get 测试 ────

// TestInMemoryKVStore_SetGet 基本 Set + Get 往返。
func TestInMemoryKVStore_SetGet(t *testing.T) {
	store := NewInMemoryKVStore()
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

// TestInMemoryKVStore_Get_不存在 验证获取不存在的 key 返回 nil。
func TestInMemoryKVStore_Get_不存在(t *testing.T) {
	store := NewInMemoryKVStore()
	ctx := context.Background()

	val, err := store.Get(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("Get 返回错误: %v", err)
	}
	if val != nil {
		t.Errorf("Get 返回 %v, 期望 nil", val)
	}
}

// TestInMemoryKVStore_Set_覆盖 验证 Set 覆盖已有值。
func TestInMemoryKVStore_Set_覆盖(t *testing.T) {
	store := NewInMemoryKVStore()
	ctx := context.Background()

	_ = store.Set(ctx, "key1", []byte("old"))
	_ = store.Set(ctx, "key1", []byte("new"))

	val, _ := store.Get(ctx, "key1")
	if string(val) != "new" {
		t.Errorf("覆盖后 Get 返回 %q, 期望 %q", string(val), "new")
	}
}

// ──── Exists 测试 ────

// TestInMemoryKVStore_Exists_存在 验证 key 存在时 Exists 返回 true。
func TestInMemoryKVStore_Exists_存在(t *testing.T) {
	store := NewInMemoryKVStore()
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

// TestInMemoryKVStore_Exists_不存在 验证 key 不存在时 Exists 返回 false。
func TestInMemoryKVStore_Exists_不存在(t *testing.T) {
	store := NewInMemoryKVStore()
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

// TestInMemoryKVStore_Delete 验证删除后 Get 返回 nil。
func TestInMemoryKVStore_Delete(t *testing.T) {
	store := NewInMemoryKVStore()
	ctx := context.Background()

	_ = store.Set(ctx, "key1", []byte("value1"))
	_ = store.Delete(ctx, "key1")

	val, _ := store.Get(ctx, "key1")
	if val != nil {
		t.Errorf("删除后 Get 返回 %v, 期望 nil", val)
	}
}

// TestInMemoryKVStore_Delete_不存在 验证删除不存在的 key 不报错。
func TestInMemoryKVStore_Delete_不存在(t *testing.T) {
	store := NewInMemoryKVStore()
	ctx := context.Background()

	err := store.Delete(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("删除不存在的 key 返回错误: %v", err)
	}
}

// ──── ExclusiveSet 测试 ────

// TestInMemoryKVStore_ExclusiveSet_新key 验证新 key 设置成功。
func TestInMemoryKVStore_ExclusiveSet_新key(t *testing.T) {
	store := NewInMemoryKVStore()
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

// TestInMemoryKVStore_ExclusiveSet_已存在拒绝 验证 key 已存在且未过期时返回 false。
func TestInMemoryKVStore_ExclusiveSet_已存在拒绝(t *testing.T) {
	store := NewInMemoryKVStore()
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

// TestInMemoryKVStore_ExclusiveSet_已过期允许覆盖 验证 key 已过期时允许覆盖。
func TestInMemoryKVStore_ExclusiveSet_已过期允许覆盖(t *testing.T) {
	store := NewInMemoryKVStore()
	ctx := context.Background()

	// 设置 1 秒后过期的 key
	ok, _ := store.ExclusiveSet(ctx, "key1", []byte("value1"), 1)
	if !ok {
		t.Fatal("首次 ExclusiveSet 应返回 true")
	}

	// 等待过期（Unix 秒级精度，需要等待超过 1 秒才能保证 now > expiryTs）
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

// TestInMemoryKVStore_ExclusiveSet_负数expiry 验证 expiry < 0 时等价于 0（永不过期）。
func TestInMemoryKVStore_ExclusiveSet_负数expiry(t *testing.T) {
	store := NewInMemoryKVStore()
	ctx := context.Background()

	ok, err := store.ExclusiveSet(ctx, "key1", []byte("value1"), -5)
	if err != nil {
		t.Fatalf("ExclusiveSet 返回错误: %v", err)
	}
	if !ok {
		t.Error("ExclusiveSet 返回 false, 期望 true")
	}

	// 验证 key 存在
	val, _ := store.Get(ctx, "key1")
	if string(val) != "value1" {
		t.Errorf("Get 返回 %q, 期望 %q", string(val), "value1")
	}

	// 验证 expiryTs 为 0（永不过期）
	store.mu.RLock()
	e, found := store.store["key1"]
	store.mu.RUnlock()
	if !found {
		t.Fatal("store 中未找到 key1")
	}
	if e.expiryTs != 0 {
		t.Errorf("expiryTs = %d, 期望 0（负数 expiry 应等价于 0）", e.expiryTs)
	}
}

// TestInMemoryKVStore_ExclusiveSet_expiry为零 验证 expiry=0 时永不过期。
func TestInMemoryKVStore_ExclusiveSet_expiry为零(t *testing.T) {
	store := NewInMemoryKVStore()
	ctx := context.Background()

	ok, err := store.ExclusiveSet(ctx, "key1", []byte("value1"), 0)
	if err != nil {
		t.Fatalf("ExclusiveSet 返回错误: %v", err)
	}
	if !ok {
		t.Error("ExclusiveSet 返回 false, 期望 true")
	}

	// 验证 key 存在
	val, _ := store.Get(ctx, "key1")
	if string(val) != "value1" {
		t.Errorf("Get 返回 %q, 期望 %q", string(val), "value1")
	}

	// 验证 expiryTs 为 0（永不过期）
	store.mu.RLock()
	e, found := store.store["key1"]
	store.mu.RUnlock()
	if !found {
		t.Fatal("store 中未找到 key1")
	}
	if e.expiryTs != 0 {
		t.Errorf("expiryTs = %d, 期望 0（expiry=0 表示永不过期）", e.expiryTs)
	}

	// 验证再次 ExclusiveSet 同一 key 会被拒绝（未过期）
	ok2, _ := store.ExclusiveSet(ctx, "key1", []byte("value2"), 0)
	if ok2 {
		t.Error("expiry=0 的 key 应永不过期，第二次 ExclusiveSet 应返回 false")
	}
}

// TestInMemoryKVStore_ExclusiveSet_带expiry 验证带过期时间的设置。
func TestInMemoryKVStore_ExclusiveSet_带expiry(t *testing.T) {
	store := NewInMemoryKVStore()
	ctx := context.Background()

	ok, _ := store.ExclusiveSet(ctx, "key1", []byte("value1"), 10)
	if !ok {
		t.Fatal("ExclusiveSet 应返回 true")
	}

	// 验证 key 存在
	exists, _ := store.Exists(ctx, "key1")
	if !exists {
		t.Error("设置后 Exists 返回 false, 期望 true")
	}

	// 验证内部过期时间戳已设置（通过读取 store 内部状态）
	store.mu.RLock()
	e, ok := store.store["key1"]
	store.mu.RUnlock()
	if !ok {
		t.Fatal("store 中未找到 key1")
	}
	if e.expiryTs == 0 {
		t.Error("expiryTs 为 0, 期望非零")
	}
}

// TestInMemoryKVStore_Exists_已过期 验证已过期的 key Exists 返回 false。
func TestInMemoryKVStore_Exists_已过期(t *testing.T) {
	store := NewInMemoryKVStore()
	ctx := context.Background()

	ok, _ := store.ExclusiveSet(ctx, "key1", []byte("value1"), 1)
	if !ok {
		t.Fatal("ExclusiveSet 应返回 true")
	}

	time.Sleep(2 * time.Second)

	exists, _ := store.Exists(ctx, "key1")
	if exists {
		t.Error("过期后 Exists 返回 true, 期望 false")
	}
}

// TestInMemoryKVStore_Get_已过期 验证已过期的 key Get 返回 nil。
func TestInMemoryKVStore_Get_已过期(t *testing.T) {
	store := NewInMemoryKVStore()
	ctx := context.Background()

	ok, _ := store.ExclusiveSet(ctx, "key1", []byte("value1"), 1)
	if !ok {
		t.Fatal("ExclusiveSet 应返回 true")
	}

	time.Sleep(2 * time.Second)

	val, _ := store.Get(ctx, "key1")
	if val != nil {
		t.Errorf("过期后 Get 返回 %v, 期望 nil", val)
	}
}

// ──── GetByPrefix 测试 ────

// TestInMemoryKVStore_GetByPrefix_正常 验证按前缀获取键值对。
func TestInMemoryKVStore_GetByPrefix_正常(t *testing.T) {
	store := NewInMemoryKVStore()
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

// TestInMemoryKVStore_GetByPrefix_空前缀 验证空前缀匹配所有 key。
func TestInMemoryKVStore_GetByPrefix_空前缀(t *testing.T) {
	store := NewInMemoryKVStore()
	ctx := context.Background()

	_ = store.Set(ctx, "user:1", []byte("alice"))
	_ = store.Set(ctx, "item:1", []byte("book"))

	result, err := store.GetByPrefix(ctx, "")
	if err != nil {
		t.Fatalf("GetByPrefix 返回错误: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("GetByPrefix 返回 %d 条, 期望 2 条（空前缀匹配所有 key）", len(result))
	}
	if string(result["user:1"]) != "alice" {
		t.Errorf("user:1 = %q, 期望 %q", string(result["user:1"]), "alice")
	}
	if string(result["item:1"]) != "book" {
		t.Errorf("item:1 = %q, 期望 %q", string(result["item:1"]), "book")
	}
}

// TestInMemoryKVStore_GetByPrefix_无匹配 验证无匹配前缀返回空 map。
func TestInMemoryKVStore_GetByPrefix_无匹配(t *testing.T) {
	store := NewInMemoryKVStore()
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

// TestInMemoryKVStore_GetByPrefix_含过期key 验证过期的 key 不包含在结果中。
func TestInMemoryKVStore_GetByPrefix_含过期key(t *testing.T) {
	store := NewInMemoryKVStore()
	ctx := context.Background()

	_ = store.Set(ctx, "user:1", []byte("alice"))
	ok, _ := store.ExclusiveSet(ctx, "user:2", []byte("bob"), 1)
	if !ok {
		t.Fatal("ExclusiveSet 应返回 true")
	}

	time.Sleep(2 * time.Second)

	result, _ := store.GetByPrefix(ctx, "user:")
	if len(result) != 1 {
		t.Errorf("GetByPrefix 返回 %d 条, 期望 1 条（过期 key 被过滤）", len(result))
	}
	if string(result["user:1"]) != "alice" {
		t.Errorf("user:1 = %q, 期望 %q", string(result["user:1"]), "alice")
	}
}

// ──── DeleteByPrefix 测试 ────

// TestInMemoryKVStore_DeleteByPrefix_无匹配 验证无匹配前缀不报错。
func TestInMemoryKVStore_DeleteByPrefix_无匹配(t *testing.T) {
	store := NewInMemoryKVStore()
	ctx := context.Background()

	_ = store.Set(ctx, "user:1", []byte("alice"))

	err := store.DeleteByPrefix(ctx, "item:", 0)
	if err != nil {
		t.Fatalf("DeleteByPrefix 返回错误: %v", err)
	}

	// 验证原有 key 未受影响
	exists, _ := store.Exists(ctx, "user:1")
	if !exists {
		t.Error("user:1 不应被删除")
	}
}

// TestInMemoryKVStore_DeleteByPrefix_一次性 验证 batchSize=0 时一次性删除。
func TestInMemoryKVStore_DeleteByPrefix_一次性(t *testing.T) {
	store := NewInMemoryKVStore()
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

// TestInMemoryKVStore_DeleteByPrefix_分批 验证 batchSize>0 时分批删除。
func TestInMemoryKVStore_DeleteByPrefix_分批(t *testing.T) {
	store := NewInMemoryKVStore()
	ctx := context.Background()

	_ = store.Set(ctx, "user:1", []byte("alice"))
	_ = store.Set(ctx, "user:2", []byte("bob"))
	_ = store.Set(ctx, "user:3", []byte("charlie"))

	err := store.DeleteByPrefix(ctx, "user:", 2)
	if err != nil {
		t.Fatalf("DeleteByPrefix 返回错误: %v", err)
	}

	// 所有 user: 前缀的 key 都应被删除
	result, _ := store.GetByPrefix(ctx, "user:")
	if len(result) != 0 {
		t.Errorf("分批删除后仍有 %d 条 user: 前缀的 key", len(result))
	}
}

// ──── MGet 测试 ────

// TestInMemoryKVStore_MGet_正常 验证批量获取多个 key。
func TestInMemoryKVStore_MGet_正常(t *testing.T) {
	store := NewInMemoryKVStore()
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

// TestInMemoryKVStore_MGet_空列表 验证传入空 key 列表时返回空切片。
func TestInMemoryKVStore_MGet_空列表(t *testing.T) {
	store := NewInMemoryKVStore()
	ctx := context.Background()

	values, err := store.MGet(ctx, []string{})
	if err != nil {
		t.Fatalf("MGet 返回错误: %v", err)
	}
	if len(values) != 0 {
		t.Errorf("MGet 返回 %d 条, 期望 0 条", len(values))
	}
}

// TestInMemoryKVStore_MGet_部分不存在 验证部分 key 不存在时对应位置为 nil。
func TestInMemoryKVStore_MGet_部分不存在(t *testing.T) {
	store := NewInMemoryKVStore()
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

// TestInMemoryKVStore_MGet_含过期 验证过期 key 对应位置为 nil。
func TestInMemoryKVStore_MGet_含过期(t *testing.T) {
	store := NewInMemoryKVStore()
	ctx := context.Background()

	_ = store.Set(ctx, "k1", []byte("v1"))
	ok, _ := store.ExclusiveSet(ctx, "k2", []byte("v2"), 1)
	if !ok {
		t.Fatal("ExclusiveSet 应返回 true")
	}

	time.Sleep(2 * time.Second)

	values, _ := store.MGet(ctx, []string{"k1", "k2"})
	if string(values[0]) != "v1" {
		t.Errorf("values[0] = %q, 期望 %q", string(values[0]), "v1")
	}
	if values[1] != nil {
		t.Errorf("过期 key values[1] = %v, 期望 nil", values[1])
	}
}

// ──── BatchDelete 测试 ────

// TestInMemoryKVStore_BatchDelete_正常 验证批量删除并返回删除数量。
func TestInMemoryKVStore_BatchDelete_正常(t *testing.T) {
	store := NewInMemoryKVStore()
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

// TestInMemoryKVStore_BatchDelete_空列表 验证空列表返回 0。
func TestInMemoryKVStore_BatchDelete_空列表(t *testing.T) {
	store := NewInMemoryKVStore()
	ctx := context.Background()

	deleted, err := store.BatchDelete(ctx, []string{}, 0)
	if err != nil {
		t.Fatalf("BatchDelete 返回错误: %v", err)
	}
	if deleted != 0 {
		t.Errorf("BatchDelete 返回 %d, 期望 0", deleted)
	}
}

// TestInMemoryKVStore_BatchDelete_分批 验证分批删除。
func TestInMemoryKVStore_BatchDelete_分批(t *testing.T) {
	store := NewInMemoryKVStore()
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

// TestInMemoryKVStore_BatchDelete_部分不存在 验证只删除存在的 key。
func TestInMemoryKVStore_BatchDelete_部分不存在(t *testing.T) {
	store := NewInMemoryKVStore()
	ctx := context.Background()

	_ = store.Set(ctx, "k1", []byte("v1"))

	deleted, _ := store.BatchDelete(ctx, []string{"k1", "k2"}, 0)
	if deleted != 1 {
		t.Errorf("BatchDelete 返回 %d, 期望 1", deleted)
	}
}

// ──── Pipeline 测试 ────

// TestInMemoryKVStore_Pipeline_混合操作 验证 Pipeline 混合 set+get+exists 操作。
func TestInMemoryKVStore_Pipeline_混合操作(t *testing.T) {
	store := NewInMemoryKVStore()
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

// TestInMemoryKVStore_Pipeline_复用 验证 Execute 后 Pipeline 可复用。
func TestInMemoryKVStore_Pipeline_复用(t *testing.T) {
	store := NewInMemoryKVStore()
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

// TestInMemoryKVStore_Pipeline_空操作 验证空 Pipeline Execute 返回空切片。
func TestInMemoryKVStore_Pipeline_空操作(t *testing.T) {
	store := NewInMemoryKVStore()
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

// TestInMemoryKVStore_并发安全 验证多 goroutine 并发读写无 race。
func TestInMemoryKVStore_并发安全(t *testing.T) {
	store := NewInMemoryKVStore()
	ctx := context.Background()

	const goroutines = 50
	const opsPerGoroutine = 100

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

// ──── 接口满足验证 ────

// TestInMemoryKVStore_接口满足 验证 InMemoryKVStore 满足 BaseKVStore 接口。
func TestInMemoryKVStore_接口满足(t *testing.T) {
	var _ BaseKVStore = (*InMemoryKVStore)(nil)
}

// ──── Pipeline 带 expiry Set 测试 ────

// TestInMemoryKVStore_Pipeline_Set带过期 验证 Pipeline Set 带 expiry 写入。
func TestInMemoryKVStore_Pipeline_Set带过期(t *testing.T) {
	store := NewInMemoryKVStore()
	ctx := context.Background()

	pipe := store.Pipeline(ctx)
	_ = pipe.Set(ctx, "expire_key", []byte("expire_value"), 60)
	_ = pipe.Set(ctx, "no_expire_key", []byte("no_expire_value"), 0)

	results, err := pipe.Execute(ctx)
	if err != nil {
		t.Fatalf("Pipeline Execute 返回错误: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("期望 2 个结果，实际 %d", len(results))
	}

	// 验证数据实际写入
	val, _ := store.Get(ctx, "expire_key")
	if string(val) != "expire_value" {
		t.Errorf("expire_key = %q, 期望 %q", string(val), "expire_value")
	}
	val, _ = store.Get(ctx, "no_expire_key")
	if string(val) != "no_expire_value" {
		t.Errorf("no_expire_key = %q, 期望 %q", string(val), "no_expire_value")
	}

	// 验证带过期的 key 内部有过期时间戳
	store.mu.RLock()
	e, ok := store.store["expire_key"]
	store.mu.RUnlock()
	if !ok {
		t.Fatal("store 中未找到 expire_key")
	}
	if e.expiryTs == 0 {
		t.Error("带过期的 key expiryTs 不应为 0")
	}

	store.mu.RLock()
	e2, ok := store.store["no_expire_key"]
	store.mu.RUnlock()
	if !ok {
		t.Fatal("store 中未找到 no_expire_key")
	}
	if e2.expiryTs != 0 {
		t.Errorf("不带过期的 key expiryTs 应为 0, 实际 %d", e2.expiryTs)
	}
}

// TestInMemoryKVStore_Pipeline_Get不存在 验证 Pipeline Get 不存在的 key 返回 nil。
func TestInMemoryKVStore_Pipeline_Get不存在(t *testing.T) {
	store := NewInMemoryKVStore()
	ctx := context.Background()

	pipe := store.Pipeline(ctx)
	_ = pipe.Get(ctx, "nonexistent_key")

	results, err := pipe.Execute(ctx)
	if err != nil {
		t.Fatalf("Pipeline Execute 返回错误: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("期望 1 个结果，实际 %d", len(results))
	}
	if results[0].Value != nil {
		t.Errorf("不存在的 key Value 应为 nil, 实际 %v", results[0].Value)
	}
}

// TestInMemoryKVStore_DeleteByPrefix_负数batchSize 验证 batchSize<0 时一次性删除。
func TestInMemoryKVStore_DeleteByPrefix_负数batchSize(t *testing.T) {
	store := NewInMemoryKVStore()
	ctx := context.Background()

	_ = store.Set(ctx, "user:1", []byte("alice"))
	_ = store.Set(ctx, "user:2", []byte("bob"))

	err := store.DeleteByPrefix(ctx, "user:", -1)
	if err != nil {
		t.Fatalf("DeleteByPrefix 返回错误: %v", err)
	}

	exists, _ := store.Exists(ctx, "user:1")
	if exists {
		t.Error("负数 batchSize 删除后 user:1 仍存在")
	}
}

// TestInMemoryKVStore_BatchDelete_负数batchSize 验证 batchSize<0 时一次性删除。
func TestInMemoryKVStore_BatchDelete_负数batchSize(t *testing.T) {
	store := NewInMemoryKVStore()
	ctx := context.Background()

	_ = store.Set(ctx, "k1", []byte("v1"))
	_ = store.Set(ctx, "k2", []byte("v2"))

	deleted, err := store.BatchDelete(ctx, []string{"k1", "k2"}, -1)
	if err != nil {
		t.Fatalf("BatchDelete 返回错误: %v", err)
	}
	if deleted != 2 {
		t.Errorf("BatchDelete 返回 %d, 期望 2", deleted)
	}
}
