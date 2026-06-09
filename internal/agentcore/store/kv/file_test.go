package kv

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	bolt "go.etcd.io/bbolt"
)

// newTestFileKVStore 创建用于测试的 FileKVStore 实例，使用 t.TempDir() 提供临时目录。
func newTestFileKVStore(t *testing.T) *FileKVStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := NewFileKVStore(dbPath)
	if err != nil {
		t.Fatalf("NewFileKVStore 返回错误: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// ──── 构造函数测试 ────

// TestNewFileKVStore 验证构造函数创建非 nil 实例。
func TestNewFileKVStore(t *testing.T) {
	store := newTestFileKVStore(t)
	if store == nil {
		t.Fatal("NewFileKVStore 返回 nil")
	}
	if store.db == nil {
		t.Fatal("内部 db 未初始化")
	}
}

// TestNewFileKVStore_自动创建父目录 验证构造函数自动创建父目录。
func TestNewFileKVStore_自动创建父目录(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sub", "dir", "test.db")
	store, err := NewFileKVStore(dbPath)
	if err != nil {
		t.Fatalf("NewFileKVStore 返回错误: %v", err)
	}
	defer store.Close()

	// 验证数据库文件存在
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("数据库文件未创建")
	}
}

// TestNewFileKVStore_路径无效 验证路径无效时返回错误。
func TestNewFileKVStore_路径无效(t *testing.T) {
	// 使用一个空字符串作为路径（无法创建目录）
	_, err := NewFileKVStore("")
	if err == nil {
		t.Error("空路径应返回错误")
	}
}

// ──── Set / Get 测试 ────

// TestFileKVStore_SetGet 基本 Set + Get 往返。
func TestFileKVStore_SetGet(t *testing.T) {
	store := newTestFileKVStore(t)
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

// TestFileKVStore_Get_不存在 验证获取不存在的 key 返回 nil。
func TestFileKVStore_Get_不存在(t *testing.T) {
	store := newTestFileKVStore(t)
	ctx := context.Background()

	val, err := store.Get(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("Get 返回错误: %v", err)
	}
	if val != nil {
		t.Errorf("Get 返回 %v, 期望 nil", val)
	}
}

// TestFileKVStore_Set_覆盖 验证 Set 覆盖已有值。
func TestFileKVStore_Set_覆盖(t *testing.T) {
	store := newTestFileKVStore(t)
	ctx := context.Background()

	_ = store.Set(ctx, "key1", []byte("old"))
	_ = store.Set(ctx, "key1", []byte("new"))

	val, _ := store.Get(ctx, "key1")
	if string(val) != "new" {
		t.Errorf("覆盖后 Get 返回 %q, 期望 %q", string(val), "new")
	}
}

// ──── Exists 测试 ────

// TestFileKVStore_Exists_存在 验证 key 存在时 Exists 返回 true。
func TestFileKVStore_Exists_存在(t *testing.T) {
	store := newTestFileKVStore(t)
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

// TestFileKVStore_Exists_不存在 验证 key 不存在时 Exists 返回 false。
func TestFileKVStore_Exists_不存在(t *testing.T) {
	store := newTestFileKVStore(t)
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

// TestFileKVStore_Delete 验证删除后 Get 返回 nil。
func TestFileKVStore_Delete(t *testing.T) {
	store := newTestFileKVStore(t)
	ctx := context.Background()

	_ = store.Set(ctx, "key1", []byte("value1"))
	_ = store.Delete(ctx, "key1")

	val, _ := store.Get(ctx, "key1")
	if val != nil {
		t.Errorf("删除后 Get 返回 %v, 期望 nil", val)
	}
}

// TestFileKVStore_Delete_不存在 验证删除不存在的 key 不报错。
func TestFileKVStore_Delete_不存在(t *testing.T) {
	store := newTestFileKVStore(t)
	ctx := context.Background()

	err := store.Delete(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("删除不存在的 key 返回错误: %v", err)
	}
}

// ──── Get 解包 exclusive 值测试 ────

// TestFileKVStore_Get_解包Exclusive值 验证 Get 解包 exclusive-set 的值返回实际 []byte。
func TestFileKVStore_Get_解包Exclusive值(t *testing.T) {
	store := newTestFileKVStore(t)
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
	var entry fileEntry
	if err := json.Unmarshal(val, &entry); err == nil && entry.Value != "" {
		t.Error("Get 返回了 JSON 包装结构，应返回解包后的实际值")
	}
}

// ──── ExclusiveSet 测试 ────

// TestFileKVStore_ExclusiveSet_新key 验证新 key 设置成功。
func TestFileKVStore_ExclusiveSet_新key(t *testing.T) {
	store := newTestFileKVStore(t)
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

// TestFileKVStore_ExclusiveSet_已存在拒绝 验证 key 已存在且未过期时返回 false。
func TestFileKVStore_ExclusiveSet_已存在拒绝(t *testing.T) {
	store := newTestFileKVStore(t)
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

// TestFileKVStore_ExclusiveSet_已过期允许覆盖 验证 key 已过期时允许覆盖。
func TestFileKVStore_ExclusiveSet_已过期允许覆盖(t *testing.T) {
	store := newTestFileKVStore(t)
	ctx := context.Background()

	// 设置 1 秒后过期的 key
	ok, _ := store.ExclusiveSet(ctx, "key1", []byte("value1"), 1)
	if !ok {
		t.Fatal("首次 ExclusiveSet 应返回 true")
	}

	// 手动修改过期时间为过去，避免 sleep
	err := store.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(store.bucketName))
		raw := b.Get([]byte("key1"))
		var entry fileEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			return err
		}
		entry.ExpiryAt = 1 // 1970-01-01，已过期
		data, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		return b.Put([]byte("key1"), data)
	})
	if err != nil {
		t.Fatalf("修改过期时间失败: %v", err)
	}

	// 已过期的 key 允许覆盖
	ok, err = store.ExclusiveSet(ctx, "key1", []byte("value2"), 0)
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

// TestFileKVStore_ExclusiveSet_带expiry 验证带过期时间的设置。
func TestFileKVStore_ExclusiveSet_带expiry(t *testing.T) {
	store := newTestFileKVStore(t)
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

	// 验证内部过期时间戳已设置（通过直接读取 bbolt）
	err := store.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(store.bucketName))
		raw := b.Get([]byte("key1"))
		var entry fileEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			return err
		}
		if entry.ExpiryAt == 0 {
			t.Error("ExpiryAt 为 0, 期望非零")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("读取内部数据失败: %v", err)
	}
}

// ──── 过期语义测试（对齐 Python ShelveStore） ────

// TestFileKVStore_Get_过期key仍返回值 验证 Get 不过期检查（对齐 Python ShelveStore）。
func TestFileKVStore_Get_过期key仍返回值(t *testing.T) {
	store := newTestFileKVStore(t)
	ctx := context.Background()

	// 设置带过期的 key，然后手动修改为已过期
	ok, _ := store.ExclusiveSet(ctx, "key1", []byte("value1"), 10)
	if !ok {
		t.Fatal("ExclusiveSet 应返回 true")
	}

	err := store.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(store.bucketName))
		raw := b.Get([]byte("key1"))
		var entry fileEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			return err
		}
		entry.ExpiryAt = 1 // 1970-01-01，已过期
		data, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		return b.Put([]byte("key1"), data)
	})
	if err != nil {
		t.Fatalf("修改过期时间失败: %v", err)
	}

	// Get 应返回值，不过期检查（对齐 Python）
	val, err := store.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Get 返回错误: %v", err)
	}
	if string(val) != "value1" {
		t.Errorf("过期 key Get 返回 %q, 期望 %q（对齐 Python 不过期检查）", string(val), "value1")
	}
}

// TestFileKVStore_Exists_过期key仍返回true 验证 Exists 不过期检查（对齐 Python ShelveStore）。
func TestFileKVStore_Exists_过期key仍返回true(t *testing.T) {
	store := newTestFileKVStore(t)
	ctx := context.Background()

	// 设置带过期的 key，然后手动修改为已过期
	ok, _ := store.ExclusiveSet(ctx, "key1", []byte("value1"), 10)
	if !ok {
		t.Fatal("ExclusiveSet 应返回 true")
	}

	err := store.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(store.bucketName))
		raw := b.Get([]byte("key1"))
		var entry fileEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			return err
		}
		entry.ExpiryAt = 1 // 1970-01-01，已过期
		data, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		return b.Put([]byte("key1"), data)
	})
	if err != nil {
		t.Fatalf("修改过期时间失败: %v", err)
	}

	// Exists 应返回 true，不过期检查（对齐 Python）
	exists, err := store.Exists(ctx, "key1")
	if err != nil {
		t.Fatalf("Exists 返回错误: %v", err)
	}
	if !exists {
		t.Error("过期 key Exists 返回 false, 期望 true（对齐 Python 不过期检查）")
	}
}

// ──── GetByPrefix 测试 ────

// TestFileKVStore_GetByPrefix_正常 验证按前缀获取键值对。
func TestFileKVStore_GetByPrefix_正常(t *testing.T) {
	store := newTestFileKVStore(t)
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
	// 注意：GetByPrefix 不解包，返回原始 JSON 字节
	// 验证 key 存在即可
	if _, ok := result["user:1"]; !ok {
		t.Error("结果中缺少 user:1")
	}
	if _, ok := result["user:2"]; !ok {
		t.Error("结果中缺少 user:2")
	}
}

// TestFileKVStore_GetByPrefix_无匹配 验证无匹配前缀返回空 map。
func TestFileKVStore_GetByPrefix_无匹配(t *testing.T) {
	store := newTestFileKVStore(t)
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

// TestFileKVStore_GetByPrefix_不解包 验证 GetByPrefix 返回原始 JSON 字节（不解包）。
func TestFileKVStore_GetByPrefix_不解包(t *testing.T) {
	store := newTestFileKVStore(t)
	ctx := context.Background()

	_ = store.Set(ctx, "user:1", []byte("alice"))

	result, _ := store.GetByPrefix(ctx, "user:")
	raw := result["user:1"]

	// 应为有效 JSON（fileEntry 格式）
	var entry fileEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		t.Fatalf("GetByPrefix 返回值不是有效 JSON: %v", err)
	}
	if entry.Value == "" {
		t.Error("fileEntry.Value 为空")
	}
	// 不应等于原始值 "alice"（应是 base64 编码的 JSON 包装）
	if entry.Value == "alice" {
		t.Error("GetByPrefix 应返回原始 JSON 字节（不解包），但看起来解包了")
	}
}

// ──── DeleteByPrefix 测试 ────

// TestFileKVStore_DeleteByPrefix_一次性 验证 batchSize=0 时一次性删除。
func TestFileKVStore_DeleteByPrefix_一次性(t *testing.T) {
	store := newTestFileKVStore(t)
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

// TestFileKVStore_DeleteByPrefix_分批 验证 batchSize>0 时分批删除。
func TestFileKVStore_DeleteByPrefix_分批(t *testing.T) {
	store := newTestFileKVStore(t)
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

// TestFileKVStore_MGet_正常 验证批量获取多个 key。
func TestFileKVStore_MGet_正常(t *testing.T) {
	store := newTestFileKVStore(t)
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
	// MGet 不解包，返回原始 JSON 字节
	if values[0] == nil {
		t.Error("values[0] 为 nil")
	}
	if values[1] == nil {
		t.Error("values[1] 为 nil")
	}
}

// TestFileKVStore_MGet_部分不存在 验证部分 key 不存在时对应位置为 nil。
func TestFileKVStore_MGet_部分不存在(t *testing.T) {
	store := newTestFileKVStore(t)
	ctx := context.Background()

	_ = store.Set(ctx, "k1", []byte("v1"))

	values, _ := store.MGet(ctx, []string{"k1", "k2"})
	if len(values) != 2 {
		t.Fatalf("MGet 返回 %d 条, 期望 2 条", len(values))
	}
	if values[0] == nil {
		t.Error("k1 应存在")
	}
	if values[1] != nil {
		t.Errorf("k2 不存在，应为 nil, 实际为 %v", values[1])
	}
}

// TestFileKVStore_MGet_不解包 验证 MGet 返回原始 JSON 字节（不解包）。
func TestFileKVStore_MGet_不解包(t *testing.T) {
	store := newTestFileKVStore(t)
	ctx := context.Background()

	_ = store.Set(ctx, "k1", []byte("v1"))

	values, _ := store.MGet(ctx, []string{"k1"})
	raw := values[0]

	// 应为有效 JSON（fileEntry 格式）
	var entry fileEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		t.Fatalf("MGet 返回值不是有效 JSON: %v", err)
	}
	if entry.Value == "" {
		t.Error("fileEntry.Value 为空")
	}
}

// ──── BatchDelete 测试 ────

// TestFileKVStore_BatchDelete_正常 验证批量删除并返回删除数量。
func TestFileKVStore_BatchDelete_正常(t *testing.T) {
	store := newTestFileKVStore(t)
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

// TestFileKVStore_BatchDelete_空列表 验证空列表返回 0。
func TestFileKVStore_BatchDelete_空列表(t *testing.T) {
	store := newTestFileKVStore(t)
	ctx := context.Background()

	deleted, err := store.BatchDelete(ctx, []string{}, 0)
	if err != nil {
		t.Fatalf("BatchDelete 返回错误: %v", err)
	}
	if deleted != 0 {
		t.Errorf("BatchDelete 返回 %d, 期望 0", deleted)
	}
}

// TestFileKVStore_BatchDelete_分批 验证分批删除。
func TestFileKVStore_BatchDelete_分批(t *testing.T) {
	store := newTestFileKVStore(t)
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

// TestFileKVStore_BatchDelete_部分不存在 验证只删除存在的 key。
func TestFileKVStore_BatchDelete_部分不存在(t *testing.T) {
	store := newTestFileKVStore(t)
	ctx := context.Background()

	_ = store.Set(ctx, "k1", []byte("v1"))

	deleted, _ := store.BatchDelete(ctx, []string{"k1", "k2"}, 0)
	if deleted != 1 {
		t.Errorf("BatchDelete 返回 %d, 期望 1", deleted)
	}
}

// ──── Pipeline 测试 ────

// TestFileKVStore_Pipeline_混合操作 验证 Pipeline 混合 set+get+exists 操作。
func TestFileKVStore_Pipeline_混合操作(t *testing.T) {
	store := newTestFileKVStore(t)
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

	// 验证 Get 结果（Pipeline Get 不解包，返回原始 JSON 字节）
	if results[1].Op != "get" {
		t.Errorf("results[1].Op = %q, 期望 %q", results[1].Op, "get")
	}
	if results[1].Value == nil {
		t.Error("Get existing key 返回 nil")
	} else {
		// 验证是 JSON 格式（不解包）
		var entry fileEntry
		if err := json.Unmarshal(results[1].Value, &entry); err != nil {
			t.Errorf("Pipeline Get 返回值不是有效 JSON: %v", err)
		}
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

// TestFileKVStore_Pipeline_复用 验证 Execute 后 Pipeline 可复用。
func TestFileKVStore_Pipeline_复用(t *testing.T) {
	store := newTestFileKVStore(t)
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

	// 验证第二次的 Get 结果（不解包）
	if results2[1].Op != "get" {
		t.Errorf("复用 Pipeline Get Op = %q, 期望 %q", results2[1].Op, "get")
	}
	if results2[1].Value == nil {
		t.Error("Get k1 返回 nil")
	}
}

// TestFileKVStore_Pipeline_空操作 验证空 Pipeline Execute 返回空切片。
func TestFileKVStore_Pipeline_空操作(t *testing.T) {
	store := newTestFileKVStore(t)
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

// TestFileKVStore_并发安全 验证多 goroutine 并发读写无 race。
func TestFileKVStore_并发安全(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "concurrent.db")
	store, err := NewFileKVStore(dbPath)
	if err != nil {
		t.Fatalf("NewFileKVStore 返回错误: %v", err)
	}
	defer store.Close()

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

// ──── 持久化测试 ────

// TestFileKVStore_数据持久化 验证写入后关闭再打开，数据仍在。
func TestFileKVStore_数据持久化(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "persist.db")
	ctx := context.Background()

	// 第一次打开，写入数据
	store1, err := NewFileKVStore(dbPath)
	if err != nil {
		t.Fatalf("第一次 NewFileKVStore 返回错误: %v", err)
	}
	_ = store1.Set(ctx, "key1", []byte("value1"))
	_ = store1.Set(ctx, "key2", []byte("value2"))
	if err := store1.Close(); err != nil {
		t.Fatalf("Close 返回错误: %v", err)
	}

	// 第二次打开，验证数据仍在
	store2, err := NewFileKVStore(dbPath)
	if err != nil {
		t.Fatalf("第二次 NewFileKVStore 返回错误: %v", err)
	}
	defer store2.Close()

	val, err := store2.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Get 返回错误: %v", err)
	}
	if string(val) != "value1" {
		t.Errorf("持久化后 Get 返回 %q, 期望 %q", string(val), "value1")
	}

	val, err = store2.Get(ctx, "key2")
	if err != nil {
		t.Fatalf("Get 返回错误: %v", err)
	}
	if string(val) != "value2" {
		t.Errorf("持久化后 Get 返回 %q, 期望 %q", string(val), "value2")
	}
}

// ──── Close 后操作测试 ────

// TestFileKVStore_Close后操作 验证 Close 后操作返回 error。
func TestFileKVStore_Close后操作(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "closed.db")
	store, err := NewFileKVStore(dbPath)
	if err != nil {
		t.Fatalf("NewFileKVStore 返回错误: %v", err)
	}

	// 写入数据后关闭
	_ = store.Set(context.Background(), "key1", []byte("value1"))
	if err := store.Close(); err != nil {
		t.Fatalf("Close 返回错误: %v", err)
	}

	// Close 后 Get 应返回 error
	_, err = store.Get(context.Background(), "key1")
	if err == nil {
		t.Error("Close 后 Get 应返回 error")
	}
}

// ──── 接口满足验证 ────

// TestFileKVStore_接口满足 验证 FileKVStore 满足 BaseKVStore 接口。
func TestFileKVStore_接口满足(t *testing.T) {
	var _ BaseKVStore = (*FileKVStore)(nil)
}

// ──── NewFileKVStore 错误路径测试 ────

// TestNewFileKVStore_创建目录失败 验证无法创建目录时返回错误。
func TestNewFileKVStore_创建目录失败(t *testing.T) {
	// 使用 /proc 下无法创建子目录的路径
	_, err := NewFileKVStore("/proc/impossible/path/test.db")
	if err == nil {
		t.Error("无法创建目录时应返回错误")
	}
}

// ──── Set 序列化错误测试 ────

// TestFileKVStore_Set_正常覆盖 验证 Set 正常覆盖已有值后可读取。
func TestFileKVStore_Set_正常覆盖(t *testing.T) {
	store := newTestFileKVStore(t)
	ctx := context.Background()

	_ = store.Set(ctx, "key1", []byte("old"))
	_ = store.Set(ctx, "key1", []byte("new"))

	val, _ := store.Get(ctx, "key1")
	if string(val) != "new" {
		t.Errorf("覆盖后 Get 返回 %q, 期望 %q", string(val), "new")
	}
}

// ──── ExclusiveSet 反序列化错误测试 ────

// TestFileKVStore_ExclusiveSet_损坏数据允许覆盖 验证已有值反序列化失败时返回错误。
func TestFileKVStore_ExclusiveSet_损坏数据允许覆盖(t *testing.T) {
	store := newTestFileKVStore(t)
	ctx := context.Background()

	// 手动写入无效 JSON 数据
	err := store.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(store.bucketName))
		return b.Put([]byte("bad_key"), []byte("not valid json"))
	})
	if err != nil {
		t.Fatalf("写入测试数据失败: %v", err)
	}

	// ExclusiveSet 应返回错误（无法反序列化已有值）
	_, err = store.ExclusiveSet(ctx, "bad_key", []byte("new_value"), 0)
	if err == nil {
		t.Error("损坏数据时 ExclusiveSet 应返回反序列化错误")
	}
}

// ──── Get 反序列化/解码错误测试 ────

// TestFileKVStore_Get_损坏数据 验证 Get 遇到损坏数据时返回错误。
func TestFileKVStore_Get_损坏数据(t *testing.T) {
	store := newTestFileKVStore(t)
	ctx := context.Background()

	// 手动写入无效 JSON 数据
	err := store.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(store.bucketName))
		return b.Put([]byte("bad_key"), []byte("not valid json"))
	})
	if err != nil {
		t.Fatalf("写入测试数据失败: %v", err)
	}

	_, err = store.Get(ctx, "bad_key")
	if err == nil {
		t.Error("Get 损坏数据应返回反序列化错误")
	}
}

// ──── Pipeline 带 expiry 的 Set 测试 ────

// TestFileKVStore_Pipeline_Set带过期 验证 Pipeline Set 带 expiry 写入。
func TestFileKVStore_Pipeline_Set带过期(t *testing.T) {
	store := newTestFileKVStore(t)
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
	if results[0].Err != nil {
		t.Errorf("Set expire_key 出错: %v", results[0].Err)
	}
	if results[1].Err != nil {
		t.Errorf("Set no_expire_key 出错: %v", results[1].Err)
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
	err = store.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(store.bucketName))
		raw := b.Get([]byte("expire_key"))
		var entry fileEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			return err
		}
		if entry.ExpiryAt == 0 {
			t.Error("带过期的 key ExpiryAt 不应为 0")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("读取内部数据失败: %v", err)
	}
}

// TestFileKVStore_Pipeline_Get不存在 验证 Pipeline Get 不存在的 key 返回 nil。
func TestFileKVStore_Pipeline_Get不存在(t *testing.T) {
	store := newTestFileKVStore(t)
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

// TestFileKVStore_Pipeline_Exists存在 验证 Pipeline Exists 对存在的 key 返回 true。
func TestFileKVStore_Pipeline_Exists存在(t *testing.T) {
	store := newTestFileKVStore(t)
	ctx := context.Background()

	_ = store.Set(ctx, "existing_key", []byte("value"))

	pipe := store.Pipeline(ctx)
	_ = pipe.Exists(ctx, "existing_key")

	results, err := pipe.Execute(ctx)
	if err != nil {
		t.Fatalf("Pipeline Execute 返回错误: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("期望 1 个结果，实际 %d", len(results))
	}
	if !results[0].Exists {
		t.Error("存在的 key Exists 应为 true")
	}
}

// ──── DeleteByPrefix 负数 batchSize 测试 ────

// TestFileKVStore_DeleteByPrefix_负数batchSize 验证 batchSize<0 时一次性删除。
func TestFileKVStore_DeleteByPrefix_负数batchSize(t *testing.T) {
	store := newTestFileKVStore(t)
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

// ──── BatchDelete 负数 batchSize 测试 ────

// TestFileKVStore_BatchDelete_负数batchSize 验证 batchSize<0 时一次性删除。
func TestFileKVStore_BatchDelete_负数batchSize(t *testing.T) {
	store := newTestFileKVStore(t)
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
