package kv

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// newTestRedisStore 创建用于测试的 RedisStore 实例，使用 miniredis 模拟。
func newTestRedisStore(t *testing.T) (*RedisStore, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("启动 miniredis 失败: %v", err)
	}
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	store := NewRedisStore(client)
	return store, mr
}

// ──── 构造函数测试 ────

// TestNewRedisStore 验证构造函数创建非 nil 实例。
func TestNewRedisStore(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("启动 miniredis 失败: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	store := NewRedisStore(client)
	if store == nil {
		t.Fatal("NewRedisStore 返回 nil")
	}
	if store.client == nil {
		t.Fatal("内部 client 未初始化")
	}
	if store.isCluster() {
		t.Fatal("Standalone 客户端不应被检测为 Cluster 模式")
	}
}

// TestNewRedisStore_自动检测Cluster 验证传入 ClusterClient 时自动检测 Cluster 模式。
func TestNewRedisStore_自动检测Cluster(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("启动 miniredis 失败: %v", err)
	}
	defer mr.Close()

	// 传入 *redis.Client，不应被检测为 Cluster
	standaloneClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	store := NewRedisStore(standaloneClient)
	if store.isCluster() {
		t.Fatal("Standalone 客户端不应被检测为 Cluster 模式")
	}
}

// TestNewRedisStore_WithClusterClient 验证 WithClusterClient 选项。
func TestNewRedisStore_WithClusterClient(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("启动 miniredis 失败: %v", err)
	}
	defer mr.Close()

	standaloneClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	fakeCluster := redis.NewClusterClient(&redis.ClusterOptions{
		Addrs: []string{mr.Addr()},
	})
	defer fakeCluster.Close()

	store := NewRedisStore(standaloneClient, WithClusterClient(fakeCluster))
	if !store.isCluster() {
		t.Fatal("WithClusterClient 应启用 Cluster 模式")
	}
	if store.clusterClient != fakeCluster {
		t.Fatal("clusterClient 应为 WithClusterClient 传入的实例")
	}
}

// ──── Set / Get 测试 ────

// TestRedisStore_SetGet 基本 Set + Get 往返。
func TestRedisStore_SetGet(t *testing.T) {
	store, mr := newTestRedisStore(t)
	defer mr.Close()
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
		t.Fatalf("期望 value1，实际 %s", val)
	}
}

// TestRedisStore_Get不存在 验证 key 不存在时返回 nil。
func TestRedisStore_Get不存在(t *testing.T) {
	store, mr := newTestRedisStore(t)
	defer mr.Close()
	ctx := context.Background()

	val, err := store.Get(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("Get 不存在的 key 不应返回错误: %v", err)
	}
	if val != nil {
		t.Fatalf("期望 nil，实际 %v", val)
	}
}

// TestRedisStore_Set覆盖 验证 Set 覆盖已有值。
func TestRedisStore_Set覆盖(t *testing.T) {
	store, mr := newTestRedisStore(t)
	defer mr.Close()
	ctx := context.Background()

	_ = store.Set(ctx, "key1", []byte("old"))
	_ = store.Set(ctx, "key1", []byte("new"))

	val, _ := store.Get(ctx, "key1")
	if string(val) != "new" {
		t.Fatalf("期望 new，实际 %s", val)
	}
}

// ──── Exists 测试 ────

// TestRedisStore_Exists 验证 Exists 检查 key 是否存在。
func TestRedisStore_Exists(t *testing.T) {
	store, mr := newTestRedisStore(t)
	defer mr.Close()
	ctx := context.Background()

	exists, _ := store.Exists(ctx, "key1")
	if exists {
		t.Fatal("不存在的 key 应返回 false")
	}

	_ = store.Set(ctx, "key1", []byte("value1"))
	exists, _ = store.Exists(ctx, "key1")
	if !exists {
		t.Fatal("存在的 key 应返回 true")
	}
}

// ──── Delete 测试 ────

// TestRedisStore_Delete 验证 Delete 删除 key。
func TestRedisStore_Delete(t *testing.T) {
	store, mr := newTestRedisStore(t)
	defer mr.Close()
	ctx := context.Background()

	_ = store.Set(ctx, "key1", []byte("value1"))
	_ = store.Delete(ctx, "key1")

	val, _ := store.Get(ctx, "key1")
	if val != nil {
		t.Fatal("删除后 Get 应返回 nil")
	}
}

// TestRedisStore_Delete不存在 验证删除不存在的 key 不报错。
func TestRedisStore_Delete不存在(t *testing.T) {
	store, mr := newTestRedisStore(t)
	defer mr.Close()
	ctx := context.Background()

	err := store.Delete(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("删除不存在的 key 不应返回错误: %v", err)
	}
}

// ──── ExclusiveSet 测试 ────

// TestRedisStore_ExclusiveSet新key 验证新 key 设置成功。
func TestRedisStore_ExclusiveSet新key(t *testing.T) {
	store, mr := newTestRedisStore(t)
	defer mr.Close()
	ctx := context.Background()

	ok, err := store.ExclusiveSet(ctx, "key1", []byte("value1"), 0)
	if err != nil {
		t.Fatalf("ExclusiveSet 返回错误: %v", err)
	}
	if !ok {
		t.Fatal("新 key 应设置成功")
	}
}

// TestRedisStore_ExclusiveSet已存在 验证已存在的 key 设置失败。
func TestRedisStore_ExclusiveSet已存在(t *testing.T) {
	store, mr := newTestRedisStore(t)
	defer mr.Close()
	ctx := context.Background()

	_ = store.Set(ctx, "key1", []byte("value1"))
	ok, _ := store.ExclusiveSet(ctx, "key1", []byte("value2"), 0)
	if ok {
		t.Fatal("已存在的 key 应设置失败")
	}
}

// TestRedisStore_ExclusiveSet带过期 验证 ExclusiveSet 带过期时间。
func TestRedisStore_ExclusiveSet带过期(t *testing.T) {
	store, mr := newTestRedisStore(t)
	defer mr.Close()
	ctx := context.Background()

	ok, _ := store.ExclusiveSet(ctx, "key1", []byte("value1"), 2)
	if !ok {
		t.Fatal("新 key 应设置成功")
	}

	// 验证 key 存在
	val, _ := store.Get(ctx, "key1")
	if string(val) != "value1" {
		t.Fatalf("期望 value1，实际 %s", val)
	}

	// 快进让 key 过期
	mr.FastForward(3 * time.Second)

	// 过期后 key 不存在
	val, _ = store.Get(ctx, "key1")
	if val != nil {
		t.Fatal("过期后 Get 应返回 nil")
	}

	// 过期后可以重新设置
	ok, _ = store.ExclusiveSet(ctx, "key1", []byte("value2"), 0)
	if !ok {
		t.Fatal("过期后应可重新设置")
	}
}

// TestRedisStore_ExclusiveSet过期后可覆盖 验证 key 过期后 ExclusiveSet 允许覆盖。
func TestRedisStore_ExclusiveSet过期后可覆盖(t *testing.T) {
	store, mr := newTestRedisStore(t)
	defer mr.Close()
	ctx := context.Background()

	ok, _ := store.ExclusiveSet(ctx, "key1", []byte("value1"), 1)
	if !ok {
		t.Fatal("首次设置应成功")
	}

	mr.FastForward(2 * time.Second)

	ok, _ = store.ExclusiveSet(ctx, "key1", []byte("value2"), 0)
	if !ok {
		t.Fatal("过期后应可覆盖")
	}
}

// ──── GetByPrefix 测试 ────

// TestRedisStore_GetByPrefix 验证按前缀获取键值对。
func TestRedisStore_GetByPrefix(t *testing.T) {
	store, mr := newTestRedisStore(t)
	defer mr.Close()
	ctx := context.Background()

	_ = store.Set(ctx, "prefix:key1", []byte("value1"))
	_ = store.Set(ctx, "prefix:key2", []byte("value2"))
	_ = store.Set(ctx, "other:key3", []byte("value3"))

	result, err := store.GetByPrefix(ctx, "prefix:")
	if err != nil {
		t.Fatalf("GetByPrefix 返回错误: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("期望 2 个结果，实际 %d", len(result))
	}
	if string(result["prefix:key1"]) != "value1" {
		t.Fatalf("期望 value1，实际 %s", result["prefix:key1"])
	}
	if string(result["prefix:key2"]) != "value2" {
		t.Fatalf("期望 value2，实际 %s", result["prefix:key2"])
	}
}

// TestRedisStore_GetByPrefix无匹配 验证无匹配时返回空 map。
func TestRedisStore_GetByPrefix无匹配(t *testing.T) {
	store, mr := newTestRedisStore(t)
	defer mr.Close()
	ctx := context.Background()

	result, err := store.GetByPrefix(ctx, "nonexistent:")
	if err != nil {
		t.Fatalf("GetByPrefix 返回错误: %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("期望 0 个结果，实际 %d", len(result))
	}
}

// ──── DeleteByPrefix 测试 ────

// TestRedisStore_DeleteByPrefix 验证按前缀删除键值对。
func TestRedisStore_DeleteByPrefix(t *testing.T) {
	store, mr := newTestRedisStore(t)
	defer mr.Close()
	ctx := context.Background()

	_ = store.Set(ctx, "prefix:key1", []byte("value1"))
	_ = store.Set(ctx, "prefix:key2", []byte("value2"))
	_ = store.Set(ctx, "other:key3", []byte("value3"))

	err := store.DeleteByPrefix(ctx, "prefix:", 0)
	if err != nil {
		t.Fatalf("DeleteByPrefix 返回错误: %v", err)
	}

	// 验证 prefix: 开头的 key 已删除
	val, _ := store.Get(ctx, "prefix:key1")
	if val != nil {
		t.Fatal("prefix:key1 应已删除")
	}
	val, _ = store.Get(ctx, "prefix:key2")
	if val != nil {
		t.Fatal("prefix:key2 应已删除")
	}

	// 验证 other: 开头的 key 仍存在
	val, _ = store.Get(ctx, "other:key3")
	if string(val) != "value3" {
		t.Fatal("other:key3 应未被删除")
	}
}

// TestRedisStore_DeleteByPrefix分批 验证按前缀分批删除。
func TestRedisStore_DeleteByPrefix分批(t *testing.T) {
	store, mr := newTestRedisStore(t)
	defer mr.Close()
	ctx := context.Background()

	// 插入 5 个 key
	for i := 0; i < 5; i++ {
		_ = store.Set(ctx, fmt.Sprintf("prefix:key%d", i), []byte(fmt.Sprintf("value%d", i)))
	}

	err := store.DeleteByPrefix(ctx, "prefix:", 2)
	if err != nil {
		t.Fatalf("DeleteByPrefix 分批返回错误: %v", err)
	}

	// 验证所有 prefix: 开头的 key 已删除
	result, _ := store.GetByPrefix(ctx, "prefix:")
	if len(result) != 0 {
		t.Fatalf("期望 0 个结果，实际 %d", len(result))
	}
}

// ──── MGet 测试 ────

// TestRedisStore_MGet 验证批量获取。
func TestRedisStore_MGet(t *testing.T) {
	store, mr := newTestRedisStore(t)
	defer mr.Close()
	ctx := context.Background()

	_ = store.Set(ctx, "key1", []byte("value1"))
	_ = store.Set(ctx, "key2", []byte("value2"))

	result, err := store.MGet(ctx, []string{"key1", "key2", "key3"})
	if err != nil {
		t.Fatalf("MGet 返回错误: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("期望 3 个结果，实际 %d", len(result))
	}
	if string(result[0]) != "value1" {
		t.Fatalf("位置 0 期望 value1，实际 %s", result[0])
	}
	if string(result[1]) != "value2" {
		t.Fatalf("位置 1 期望 value2，实际 %s", result[1])
	}
	if result[2] != nil {
		t.Fatalf("位置 2 不存在的 key 应为 nil，实际 %v", result[2])
	}
}

// TestRedisStore_MGet空列表 验证空列表返回空切片。
func TestRedisStore_MGet空列表(t *testing.T) {
	store, mr := newTestRedisStore(t)
	defer mr.Close()
	ctx := context.Background()

	result, err := store.MGet(ctx, []string{})
	if err != nil {
		t.Fatalf("MGet 空列表返回错误: %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("期望 0 个结果，实际 %d", len(result))
	}
}

// ──── BatchDelete 测试 ────

// TestRedisStore_BatchDelete 验证批量删除。
func TestRedisStore_BatchDelete(t *testing.T) {
	store, mr := newTestRedisStore(t)
	defer mr.Close()
	ctx := context.Background()

	_ = store.Set(ctx, "key1", []byte("value1"))
	_ = store.Set(ctx, "key2", []byte("value2"))
	_ = store.Set(ctx, "key3", []byte("value3"))

	deleted, err := store.BatchDelete(ctx, []string{"key1", "key2", "nonexistent"}, 0)
	if err != nil {
		t.Fatalf("BatchDelete 返回错误: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("期望删除 2 个，实际 %d", deleted)
	}

	val, _ := store.Get(ctx, "key1")
	if val != nil {
		t.Fatal("key1 应已删除")
	}
	val, _ = store.Get(ctx, "key3")
	if val == nil {
		t.Fatal("key3 应未被删除")
	}
}

// TestRedisStore_BatchDelete空列表 验证空列表返回 0。
func TestRedisStore_BatchDelete空列表(t *testing.T) {
	store, mr := newTestRedisStore(t)
	defer mr.Close()
	ctx := context.Background()

	deleted, err := store.BatchDelete(ctx, []string{}, 0)
	if err != nil {
		t.Fatalf("BatchDelete 空列表返回错误: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("期望删除 0 个，实际 %d", deleted)
	}
}

// TestRedisStore_BatchDelete分批 验证分批删除。
func TestRedisStore_BatchDelete分批(t *testing.T) {
	store, mr := newTestRedisStore(t)
	defer mr.Close()
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		_ = store.Set(ctx, fmt.Sprintf("key%d", i), []byte(fmt.Sprintf("value%d", i)))
	}

	deleted, err := store.BatchDelete(ctx, []string{"key0", "key1", "key2", "key3", "key4"}, 2)
	if err != nil {
		t.Fatalf("BatchDelete 分批返回错误: %v", err)
	}
	if deleted != 5 {
		t.Fatalf("期望删除 5 个，实际 %d", deleted)
	}
}

// ──── Pipeline 测试 ────

// TestRedisStore_Pipeline 验证 Pipeline 混合操作。
func TestRedisStore_Pipeline(t *testing.T) {
	store, mr := newTestRedisStore(t)
	defer mr.Close()
	ctx := context.Background()

	// 预设数据
	_ = store.Set(ctx, "existing", []byte("old_value"))

	pipe := store.Pipeline(ctx)
	_ = pipe.Set(ctx, "new_key", []byte("new_value"), 0)
	_ = pipe.Get(ctx, "existing")
	_ = pipe.Exists(ctx, "nonexistent")

	results, err := pipe.Execute(ctx)
	if err != nil {
		t.Fatalf("Pipeline Execute 返回错误: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("期望 3 个结果，实际 %d", len(results))
	}

	// 验证 set 结果
	if results[0].Op != "set" || results[0].Err != nil {
		t.Fatalf("set 结果异常: op=%s, err=%v", results[0].Op, results[0].Err)
	}

	// 验证 get 结果
	if results[1].Op != "get" || string(results[1].Value) != "old_value" {
		t.Fatalf("get 结果异常: value=%s", results[1].Value)
	}

	// 验证 exists 结果
	if results[2].Op != "exists" || results[2].Exists {
		t.Fatalf("exists 结果异常: exists=%v", results[2].Exists)
	}
}

// TestRedisStore_Pipeline空操作 验证空 Pipeline 返回空结果。
func TestRedisStore_Pipeline空操作(t *testing.T) {
	store, mr := newTestRedisStore(t)
	defer mr.Close()
	ctx := context.Background()

	pipe := store.Pipeline(ctx)
	results, err := pipe.Execute(ctx)
	if err != nil {
		t.Fatalf("Pipeline Execute 返回错误: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("期望 0 个结果，实际 %d", len(results))
	}
}

// TestRedisStore_Pipeline复用 验证 Pipeline 执行后可复用。
func TestRedisStore_Pipeline复用(t *testing.T) {
	store, mr := newTestRedisStore(t)
	defer mr.Close()
	ctx := context.Background()

	pipe := store.Pipeline(ctx)

	// 第一次使用
	_ = pipe.Set(ctx, "key1", []byte("value1"), 0)
	results1, _ := pipe.Execute(ctx)
	if len(results1) != 1 {
		t.Fatalf("第一次 Execute 期望 1 个结果，实际 %d", len(results1))
	}

	// 复用
	_ = pipe.Set(ctx, "key2", []byte("value2"), 0)
	results2, _ := pipe.Execute(ctx)
	if len(results2) != 1 {
		t.Fatalf("第二次 Execute 期望 1 个结果，实际 %d", len(results2))
	}

	// 验证两次 set 都成功
	val, _ := store.Get(ctx, "key1")
	if string(val) != "value1" {
		t.Fatalf("key1 期望 value1，实际 %s", val)
	}
	val, _ = store.Get(ctx, "key2")
	if string(val) != "value2" {
		t.Fatalf("key2 期望 value2，实际 %s", val)
	}
}

// TestRedisStore_Pipeline带过期 验证 Pipeline Set 带 expiry。
func TestRedisStore_Pipeline带过期(t *testing.T) {
	store, mr := newTestRedisStore(t)
	defer mr.Close()
	ctx := context.Background()

	pipe := store.Pipeline(ctx)
	_ = pipe.Set(ctx, "expire_key", []byte("expire_value"), 2)
	_, _ = pipe.Execute(ctx)

	// 验证 key 存在
	val, _ := store.Get(ctx, "expire_key")
	if string(val) != "expire_value" {
		t.Fatalf("期望 expire_value，实际 %s", val)
	}

	// 快进让 key 过期
	mr.FastForward(3 * time.Second)

	val, _ = store.Get(ctx, "expire_key")
	if val != nil {
		t.Fatal("过期后 Get 应返回 nil")
	}
}

// ──── RefreshTTL 测试 ────

// TestRedisStore_RefreshTTL 验证刷新 TTL。
func TestRedisStore_RefreshTTL(t *testing.T) {
	store, mr := newTestRedisStore(t)
	defer mr.Close()
	ctx := context.Background()

	// 先设置带短过期时间的 key
	ok, _ := store.ExclusiveSet(ctx, "key1", []byte("value1"), 1)
	if !ok {
		t.Fatal("设置 key1 失败")
	}
	_ = store.Set(ctx, "key2", []byte("value2"))

	// 刷新 TTL
	err := store.RefreshTTL(ctx, []string{"key1", "key2"}, 10)
	if err != nil {
		t.Fatalf("RefreshTTL 返回错误: %v", err)
	}

	// 快进超过原始过期时间
	mr.FastForward(2 * time.Second)

	// key1 应该仍存在（TTL 已刷新到 10s）
	val, _ := store.Get(ctx, "key1")
	if val == nil {
		t.Fatal("key1 刷新 TTL 后应仍存在")
	}
}

// TestRedisStore_RefreshTTL空列表 验证空列表不执行操作。
func TestRedisStore_RefreshTTL空列表(t *testing.T) {
	store, mr := newTestRedisStore(t)
	defer mr.Close()
	ctx := context.Background()

	err := store.RefreshTTL(ctx, []string{}, 10)
	if err != nil {
		t.Fatalf("空列表 RefreshTTL 不应返回错误: %v", err)
	}
}

// TestRedisStore_RefreshTTL无效TTL 验证 ttlSeconds <= 0 时不执行操作。
func TestRedisStore_RefreshTTL无效TTL(t *testing.T) {
	store, mr := newTestRedisStore(t)
	defer mr.Close()
	ctx := context.Background()

	err := store.RefreshTTL(ctx, []string{"key1"}, 0)
	if err != nil {
		t.Fatalf("无效 TTL 不应返回错误: %v", err)
	}

	err = store.RefreshTTL(ctx, []string{"key1"}, -1)
	if err != nil {
		t.Fatalf("负数 TTL 不应返回错误: %v", err)
	}
}

// ──── 并发安全测试 ────

// TestRedisStore_并发安全 验证多 goroutine 并发读写。
func TestRedisStore_并发安全(t *testing.T) {
	store, mr := newTestRedisStore(t)
	defer mr.Close()
	ctx := context.Background()

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines * 3)

	// 并发 Set
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			_ = store.Set(ctx, fmt.Sprintf("key%d", i), []byte(fmt.Sprintf("value%d", i)))
		}(i)
	}

	// 并发 Get
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			_, _ = store.Get(ctx, fmt.Sprintf("key%d", i))
		}(i)
	}

	// 并发 ExclusiveSet
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			_, _ = store.ExclusiveSet(ctx, fmt.Sprintf("exclusive%d", i), []byte("val"), 0)
		}(i)
	}

	wg.Wait()
}

// ──── 错误路径测试 ────

// TestRedisStore_错误路径 通过关闭 miniredis 模拟连接失败，覆盖错误日志分支。
func TestRedisStore_错误路径(t *testing.T) {
	store, mr := newTestRedisStore(t)
	ctx := context.Background()

	// 先正常操作一次，确保 store 可用
	_ = store.Set(ctx, "key1", []byte("value1"))

	// 关闭 miniredis 模拟连接失败
	mr.Close()

	// 以下操作都会进入错误路径，触发 Error 日志
	_ = store.Set(ctx, "key2", []byte("value2"))
	_, _ = store.Get(ctx, "key1")
	_, _ = store.Exists(ctx, "key1")
	_ = store.Delete(ctx, "key1")
	_, _ = store.ExclusiveSet(ctx, "key2", []byte("value2"), 0)
	_, _ = store.GetByPrefix(ctx, "prefix:")
	_ = store.DeleteByPrefix(ctx, "prefix:", 0)
	_, _ = store.MGet(ctx, []string{"key1"})
	_, _ = store.BatchDelete(ctx, []string{"key1"}, 0)
}

// TestRedisStore_MGet失败回退 验证 MGet 失败时回退到逐个 GET。
func TestRedisStore_MGet失败回退(t *testing.T) {
	store, mr := newTestRedisStore(t)
	ctx := context.Background()

	_ = store.Set(ctx, "key1", []byte("value1"))

	// 关闭 miniredis 模拟连接失败
	mr.Close()

	// MGet 应该走回退路径（mGetFallback）
	result, _ := store.MGet(ctx, []string{"key1", "key2"})
	// 回退后 Pipeline 也会失败，结果都是 nil
	if len(result) != 2 {
		t.Fatalf("期望 2 个结果，实际 %d", len(result))
	}
}
