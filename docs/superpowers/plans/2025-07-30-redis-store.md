# RedisStore 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 RedisStore，基于 go-redis/redis v9 的 Redis KV 存储，支持 Standalone 和 Cluster 两种模式。

**Architecture:** RedisStore 实现 BaseKVStore 接口，持有 `redis.Cmdable` 命令接口 + 可选 `*redis.ClusterClient` 引用。构造函数自动通过类型断言检测 Cluster 模式。Cluster 模式下 GetByPrefix/DeleteByPrefix 使用 ForEachMaster 全节点 SCAN，MGet 失败时回退 Pipeline+逐个 GET。Pipeline 包装 redis.Pipeliner 为 KVPipeline 接口。

**Tech Stack:** go-redis/redis v9, miniredis v2 (测试), zerolog (日志)

**设计文档：** `docs/superpowers/specs/2025-07-30-redis-store-design.md`

---

## 文件结构

| 操作 | 文件 | 职责 |
|------|------|------|
| 修改 | `go.mod` / `go.sum` | 添加 go-redis/v9 + miniredis/v2 依赖 |
| 创建 | `internal/agentcore/store/kv/redis.go` | RedisStore + redisPipeline + Option + RefreshTTL |
| 创建 | `internal/agentcore/store/kv/redis_test.go` | RedisStore 单元测试（miniredis） |
| 修改 | `internal/agentcore/store/kv/doc.go` | 更新文件目录和 RedisStore 说明 |

---

### Task 1: 添加依赖

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] **Step 1: 添加 go-redis 和 miniredis 依赖**

```bash
cd /home/opensource/uap-claw-go && go get github.com/redis/go-redis/v9@latest && go get github.com/alicebob/miniredis/v2@latest
```

- [ ] **Step 2: 验证依赖添加成功**

```bash
cd /home/opensource/uap-claw-go && go mod tidy && grep -E "go-redis|miniredis" go.mod
```

Expected: 输出包含 `github.com/redis/go-redis/v9` 和 `github.com/alicebob/miniredis/v2`

- [ ] **Step 3: 提交**

```bash
git add go.mod go.sum && git commit -m "chore: add go-redis/v9 and miniredis/v2 dependencies for RedisStore"
```

---

### Task 2: 实现 RedisStore 核心结构体和单键操作

**Files:**
- Create: `internal/agentcore/store/kv/redis.go`

- [ ] **Step 1: 编写 RedisStore 结构体、构造函数、Option 和单键操作（Set/Get/Exists/Delete/ExclusiveSet）**

```go
package kv

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RedisStore 基于 go-redis 的 Redis KV 存储实现。
//
// 支持 Standalone 和 Cluster 两种模式，通过构造函数传入的客户端类型自动检测。
// 当传入 *redis.ClusterClient 时，自动启用 Cluster 模式，
// GetByPrefix/DeleteByPrefix 会使用 ForEachMaster 遍历所有 master 节点。
//
// 对应 Python: openjiuwen/extensions/store/kv/redis_store.py
type RedisStore struct {
	// client Redis 命令接口（Standalone 和 Cluster 均满足）
	client redis.Cmdable
	// clusterClient Cluster 模式下保存的 *redis.ClusterClient 引用
	// 非 nil 时表示 Cluster 模式，用于 ForEachMaster 全节点 SCAN
	clusterClient *redis.ClusterClient
}

// redisPipeline Redis Pipeline 实现，包装 go-redis Pipeliner 为 KVPipeline 接口。
type redisPipeline struct {
	// pipe go-redis 原生 Pipeliner
	pipe redis.Pipeliner
	// ops 操作元数据，用于 Execute 时组装 PipelineResult
	ops []pipelineOp
}

// pipelineOp Pipeline 操作元数据
type pipelineOp struct {
	// op 操作类型："set"、"get"、"exists"
	op string
	// key 操作的键
	key string
	// setCmd Set 操作的命令引用（op 为 "set" 时有效）
	setCmd *redis.StatusCmd
	// getCmd Get 操作的命令引用（op 为 "get" 时有效）
	getCmd *redis.StringCmd
	// existsCmd Exists 操作的命令引用（op 为 "exists" 时有效）
	existsCmd *redis.IntCmd
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件，agentcore 下统一使用 ComponentAgentCore
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewRedisStore 创建基于 Redis 的 KV 存储实例。
//
// client: Redis 客户端，支持 *redis.Client（Standalone）和 *redis.ClusterClient（Cluster）。
// 构造函数自动通过类型断言检测 Cluster 模式，无需手动配置。
//
// 对齐 Python: RedisStore(redis: Redis | RedisCluster)
func NewRedisStore(client redis.Cmdable, opts ...Option) *RedisStore {
	s := &RedisStore{client: client}

	// 自动检测 Cluster 模式
	if cc, ok := client.(*redis.ClusterClient); ok {
		s.clusterClient = cc
	}

	// 应用函数式选项
	for _, opt := range opts {
		opt(s)
	}

	return s
}

// Set 存储或覆盖一个键值对。
// 对齐 Python: RedisStore.set(key, value)
func (s *RedisStore) Set(ctx context.Context, key string, value []byte) error {
	err := s.client.Set(ctx, key, value, 0).Err()
	if err != nil {
		logger.Error(logComponent).
			Str("key", key).
			Err(err).
			Msg("设置 key 失败")
		return err
	}
	logger.Debug(logComponent).
		Str("key", key).
		Msg("成功设置 key")
	return nil
}

// ExclusiveSet 原子性地设置键值对，仅当 key 不存在时成功。
// expiry 为过期秒数，0 表示不过期。
// 返回 true 表示设置成功，false 表示 key 已存在。
// 对齐 Python: RedisStore.exclusive_set(key, value, expiry)
func (s *RedisStore) ExclusiveSet(ctx context.Context, key string, value []byte, expiry int) (bool, error) {
	var ttl time.Duration
	if expiry > 0 {
		ttl = time.Duration(expiry) * time.Second
	}
	ok, err := s.client.SetNX(ctx, key, value, ttl).Result()
	if err != nil {
		logger.Error(logComponent).
			Str("key", key).
			Err(err).
			Msg("排他设置 key 失败")
		return false, err
	}
	logger.Debug(logComponent).
		Str("key", key).
		Bool("result", ok).
		Int("expiry", expiry).
		Msg("排他设置 key")
	return ok, nil
}

// Get 根据 key 获取值，key 不存在时返回 nil, nil。
// 对齐 Python: RedisStore.get(key)
func (s *RedisStore) Get(ctx context.Context, key string) ([]byte, error) {
	val, err := s.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		logger.Debug(logComponent).
			Str("key", key).
			Msg("key 不存在")
		return nil, nil
	}
	if err != nil {
		logger.Error(logComponent).
			Str("key", key).
			Err(err).
			Msg("获取 key 失败")
		return nil, err
	}
	logger.Debug(logComponent).
		Str("key", key).
		Msg("成功获取 key")
	return val, nil
}

// Exists 检查 key 是否存在。
// 对齐 Python: RedisStore.exists(key)
func (s *RedisStore) Exists(ctx context.Context, key string) (bool, error) {
	n, err := s.client.Exists(ctx, key).Result()
	if err != nil {
		logger.Error(logComponent).
			Str("key", key).
			Err(err).
			Msg("检查 key 存在失败")
		return false, err
	}
	return n > 0, nil
}

// Delete 删除指定 key。key 不存在时返回 nil（不报错），与 Python 行为一致。
// 对齐 Python: RedisStore.delete(key)
func (s *RedisStore) Delete(ctx context.Context, key string) error {
	n, err := s.client.Del(ctx, key).Result()
	if err != nil {
		logger.Error(logComponent).
			Str("key", key).
			Err(err).
			Msg("删除 key 失败")
		return err
	}
	logger.Debug(logComponent).
		Str("key", key).
		Int64("deleted", n).
		Msg("删除 key")
	return nil
}

// RefreshTTL 刷新指定 keys 的 TTL。
// 这是 RedisStore 独有的方法，不在 BaseKVStore 接口中。
// ttlSeconds <= 0 或 keys 为空时直接返回 nil。
// 失败时静默忽略（仅记录 Warn 日志），对齐 Python 行为。
//
// 对齐 Python: RedisStore.refresh_ttl(keys, ttl_seconds)
func (s *RedisStore) RefreshTTL(ctx context.Context, keys []string, ttlSeconds int) error {
	if len(keys) == 0 || ttlSeconds <= 0 {
		return nil
	}
	pipe := s.client.Pipeline()
	for _, key := range keys {
		pipe.Expire(ctx, key, time.Duration(ttlSeconds)*time.Second)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		logger.Warn(logComponent).
			Str("event_type", "REDIS_REFRESH_TTL_ERROR").
			Int("key_count", len(keys)).
			Int("ttl_seconds", ttlSeconds).
			Err(err).
			Msg("刷新 TTL 失败，静默忽略")
		return nil
	}
	logger.Debug(logComponent).
		Int("key_count", len(keys)).
		Int("ttl_seconds", ttlSeconds).
		Msg("成功刷新 TTL")
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isCluster 返回是否为 Cluster 模式。
// 等价于 Python 的 self._is_cluster，但通过 clusterClient != nil 推断，无需额外字段。
func (s *RedisStore) isCluster() bool {
	return s.clusterClient != nil
}
```

- [ ] **Step 2: 编写 Option 函数式选项**

在同一个文件 `redis.go` 的导出函数区块中，在 `NewRedisStore` 之后添加：

```go
// Option RedisStore 的函数式选项
type Option func(*RedisStore)

// WithClusterClient 手动设置 Cluster 客户端引用。
// 当 Cmdable 接口的底层类型无法被断言为 *redis.ClusterClient 时使用
// （例如通过包装器或代理传入的 ClusterClient）。
func WithClusterClient(cc *redis.ClusterClient) Option {
	return func(s *RedisStore) {
		s.clusterClient = cc
	}
}
```

注意：`Option` 类型定义应放在结构体区块之后、常量区块之前，按照项目代码规范（类型别名归类到枚举区块，但 Option 是 `func` 类型，归到导出函数区块更合适）。实际放在导出函数区块 `NewRedisStore` 之后即可。

- [ ] **Step 3: 编译验证**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/store/kv/...
```

Expected: 编译失败（缺少 BaseKVStore 接口方法），这是预期行为，后续 Task 补充

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/store/kv/redis.go && git commit -m "feat(kv): add RedisStore struct, constructor, Option, and single-key operations"
```

---

### Task 3: 实现 GetByPrefix / DeleteByPrefix（含 Cluster 支持）

**Files:**
- Modify: `internal/agentcore/store/kv/redis.go`

- [ ] **Step 1: 在 `redis.go` 的导出函数区块（Delete 之后）添加 GetByPrefix**

```go
// GetByPrefix 获取所有以 prefix 开头的键值对。
// Standalone 模式使用 SCAN 迭代；Cluster 模式使用 ForEachMaster 遍历所有 master 节点。
// 对齐 Python: RedisStore.get_by_prefix(prefix)
func (s *RedisStore) GetByPrefix(ctx context.Context, prefix string) (map[string][]byte, error) {
	if s.isCluster() {
		return s.getByPrefixCluster(ctx, prefix)
	}
	return s.getByPrefixStandalone(ctx, prefix)
}

// DeleteByPrefix 删除所有以 prefix 开头的键值对。
// batchSize 为每批删除的数量，0 或负数表示一次性删除。
// Standalone 模式使用 SCAN 迭代；Cluster 模式使用 ForEachMaster 遍历所有 master 节点。
// 对齐 Python: RedisStore.delete_by_prefix(prefix, batch_size)
func (s *RedisStore) DeleteByPrefix(ctx context.Context, prefix string, batchSize int) error {
	if s.isCluster() {
		return s.deleteByPrefixCluster(ctx, prefix, batchSize)
	}
	return s.deleteByPrefixStandalone(ctx, prefix, batchSize)
}
```

- [ ] **Step 2: 在非导出函数区块添加 Standalone/Cluster 的具体实现**

```go
// getByPrefixStandalone Standalone 模式下按前缀获取键值对。
func (s *RedisStore) getByPrefixStandalone(ctx context.Context, prefix string) (map[string][]byte, error) {
	result := make(map[string][]byte)
	pattern := prefix + "*"
	var cursor uint64
	for {
		var keys []string
		keys, cursor = s.client.Scan(ctx, cursor, pattern, 100).Val()
		for _, key := range keys {
			val, err := s.client.Get(ctx, key).Bytes()
			if err == redis.Nil {
				continue // key 在 SCAN 和 GET 之间被删除
			}
			if err != nil {
				return nil, err
			}
			result[key] = val
		}
		if cursor == 0 {
			break
		}
	}
	logger.Debug(logComponent).
		Str("prefix", prefix).
		Int("count", len(result)).
		Msg("按前缀获取键值对")
	return result, nil
}

// getByPrefixCluster Cluster 模式下按前缀获取键值对，使用 ForEachMaster 遍历所有 master 节点。
func (s *RedisStore) getByPrefixCluster(ctx context.Context, prefix string) (map[string][]byte, error) {
	result := make(map[string][]byte)
	pattern := prefix + "*"
	err := s.clusterClient.ForEachMaster(ctx, func(ctx context.Context, master *redis.Client) error {
		var cursor uint64
		for {
			keys, cursor, err := master.Scan(ctx, cursor, pattern, 100).Result()
			if err != nil {
				return err
			}
			for _, key := range keys {
				val, err := s.client.Get(ctx, key).Bytes()
				if err == redis.Nil {
					continue
				}
				if err != nil {
					return err
				}
				result[key] = val
			}
			if cursor == 0 {
				break
			}
		}
		return nil
	})
	if err != nil {
		logger.Error(logComponent).
			Str("prefix", prefix).
			Err(err).
			Msg("Cluster 模式按前缀获取键值对失败")
		return nil, err
	}
	logger.Debug(logComponent).
		Str("prefix", prefix).
		Int("count", len(result)).
		Msg("Cluster 模式按前缀获取键值对")
	return result, nil
}

// deleteByPrefixStandalone Standalone 模式下按前缀删除键值对。
func (s *RedisStore) deleteByPrefixStandalone(ctx context.Context, prefix string, batchSize int) error {
	pattern := prefix + "*"
	var keys []string
	var cursor uint64
	for {
		var batch []string
		batch, cursor = s.client.Scan(ctx, cursor, pattern, 100).Val()
		keys = append(keys, batch...)
		if cursor == 0 {
			break
		}
	}
	if len(keys) == 0 {
		return nil
	}
	if batchSize <= 0 {
		return s.client.Del(ctx, keys...).Err()
	}
	for i := 0; i < len(keys); i += batchSize {
		end := i + batchSize
		if end > len(keys) {
			end = len(keys)
		}
		if err := s.client.Del(ctx, keys[i:end]...).Err(); err != nil {
			return err
		}
	}
	logger.Debug(logComponent).
		Str("prefix", prefix).
		Int("count", len(keys)).
		Msg("按前缀删除键值对")
	return nil
}

// deleteByPrefixCluster Cluster 模式下按前缀删除键值对，使用 ForEachMaster 遍历所有 master 节点。
func (s *RedisStore) deleteByPrefixCluster(ctx context.Context, prefix string, batchSize int) error {
	pattern := prefix + "*"
	err := s.clusterClient.ForEachMaster(ctx, func(ctx context.Context, master *redis.Client) error {
		var keys []string
		var cursor uint64
		for {
			batch, cursor, err := master.Scan(ctx, cursor, pattern, 100).Result()
			if err != nil {
				return err
			}
			keys = append(keys, batch...)
			if cursor == 0 {
				break
			}
		}
		if len(keys) == 0 {
			return nil
		}
		if batchSize <= 0 {
			return s.client.Del(ctx, keys...).Err()
		}
		for i := 0; i < len(keys); i += batchSize {
			end := i + batchSize
			if end > len(keys) {
				end = len(keys)
			}
			if err := s.client.Del(ctx, keys[i:end]...).Err(); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		logger.Error(logComponent).
			Str("prefix", prefix).
			Err(err).
			Msg("Cluster 模式按前缀删除失败")
		return err
	}
	logger.Debug(logComponent).
		Str("prefix", prefix).
		Msg("Cluster 模式按前缀删除键值对")
	return nil
}
```

- [ ] **Step 3: 编译验证**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/store/kv/...
```

Expected: 仍然缺少 MGet/BatchDelete/Pipeline，继续下一个 Task

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/store/kv/redis.go && git commit -m "feat(kv): add GetByPrefix/DeleteByPrefix with Cluster ForEachMaster support"
```

---

### Task 4: 实现 MGet / BatchDelete（含 Cluster 容错回退）

**Files:**
- Modify: `internal/agentcore/store/kv/redis.go`

- [ ] **Step 1: 在导出函数区块添加 MGet 和 BatchDelete**

```go
// MGet 批量获取多个 key 的值。
// 返回值与输入 keys 顺序对应，不存在的 key 对应位置为 nil。
// MGET 失败时（如 Cluster CROSSSLOT），回退到 Pipeline+逐个 GET。
// 对齐 Python: RedisStore.mget(keys)
func (s *RedisStore) MGet(ctx context.Context, keys []string) ([][]byte, error) {
	if len(keys) == 0 {
		return [][]byte{}, nil
	}

	// 尝试 MGET
	vals, err := s.client.MGet(ctx, keys...).Result()
	if err != nil {
		// MGET 失败（可能是 Cluster CROSSSLOT 或其他原因），回退到 Pipeline+逐个 GET
		// 对齐 Python: try MGET → except → fallback to individual GETs
		logger.Warn(logComponent).
			Err(err).
			Int("key_count", len(keys)).
			Msg("MGET 失败，回退到逐个 GET")
		return s.mGetFallback(ctx, keys)
	}

	// 转换结果为 [][]byte
	result := make([][]byte, len(keys))
	for i, val := range vals {
		if val == nil {
			continue
		}
		switch v := val.(type) {
		case string:
			result[i] = []byte(v)
		case []byte:
			result[i] = v
		}
	}
	logger.Debug(logComponent).
		Int("key_count", len(keys)).
		Msg("批量获取 keys")
	return result, nil
}

// BatchDelete 批量删除多个 key，返回成功删除的数量。
// batchSize 为每批删除的数量，0 或负数表示一次性删除。
// 对齐 Python: RedisStore.batch_delete(keys, batch_size)
func (s *RedisStore) BatchDelete(ctx context.Context, keys []string, batchSize int) (int, error) {
	if len(keys) == 0 {
		return 0, nil
	}
	if batchSize <= 0 {
		n, err := s.client.Del(ctx, keys...).Result()
		if err != nil {
			logger.Error(logComponent).
				Int("key_count", len(keys)).
				Err(err).
				Msg("批量删除失败")
			return 0, err
		}
		logger.Debug(logComponent).
			Int("key_count", len(keys)).
			Int64("deleted", n).
			Msg("批量删除 keys")
		return int(n), nil
	}
	totalDeleted := 0
	for i := 0; i < len(keys); i += batchSize {
		end := i + batchSize
		if end > len(keys) {
			end = len(keys)
		}
		n, err := s.client.Del(ctx, keys[i:end]...).Result()
		if err != nil {
			logger.Error(logComponent).
				Int("batch", i/batchSize+1).
				Err(err).
				Msg("分批删除失败")
			return totalDeleted, err
		}
		totalDeleted += int(n)
	}
	logger.Debug(logComponent).
		Int("key_count", len(keys)).
		Int("deleted", totalDeleted).
		Msg("分批删除 keys")
	return totalDeleted, nil
}
```

- [ ] **Step 2: 在非导出函数区块添加 mGetFallback**

```go
// mGetFallback 使用 Pipeline 逐个 GET 的回退方案。
// 对齐 Python: MGET failed, falling back to individual GETs
func (s *RedisStore) mGetFallback(ctx context.Context, keys []string) ([][]byte, error) {
	pipe := s.client.Pipeline()
	cmds := make([]*redis.StringCmd, len(keys))
	for i, key := range keys {
		cmds[i] = pipe.Get(ctx, key)
	}
	_, _ = pipe.Exec(ctx) // 忽略整体错误，逐个读取结果

	result := make([][]byte, len(keys))
	for i, cmd := range cmds {
		val, err := cmd.Bytes()
		if err == redis.Nil {
			continue
		}
		if err != nil {
			continue // 对齐 Python：单个 key 失败时对应位置为 nil
		}
		result[i] = val
	}
	return result, nil
}
```

- [ ] **Step 3: 编译验证**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/store/kv/...
```

Expected: 仍然缺少 Pipeline 方法

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/store/kv/redis.go && git commit -m "feat(kv): add MGet with fallback and BatchDelete for RedisStore"
```

---

### Task 5: 实现 Pipeline（包装 redis.Pipeliner 为 KVPipeline）

**Files:**
- Modify: `internal/agentcore/store/kv/redis.go`

- [ ] **Step 1: 在导出函数区块添加 Pipeline 方法**

```go
// Pipeline 创建批量操作管道，用于减少网络往返。
// 包装 go-redis 原生 Pipeliner 为 KVPipeline 接口。
// 对齐 Python: RedisStore.pipeline()
func (s *RedisStore) Pipeline(_ context.Context) KVPipeline {
	return &redisPipeline{
		pipe: s.client.Pipeline(),
		ops:  make([]pipelineOp, 0),
	}
}
```

- [ ] **Step 2: 添加 redisPipeline 的 KVPipeline 接口方法**

```go
// Set 向管道中添加一个 Set 操作（仅记录，不立即执行）。
// expiry 为过期秒数，0 表示不过期。
func (p *redisPipeline) Set(ctx context.Context, key string, value []byte, expiry int) error {
	var ttl time.Duration
	if expiry > 0 {
		ttl = time.Duration(expiry) * time.Second
	}
	cmd := p.pipe.Set(ctx, key, value, ttl)
	p.ops = append(p.ops, pipelineOp{op: "set", key: key, setCmd: cmd})
	return nil
}

// Get 向管道中添加一个 Get 操作（仅记录，不立即执行）。
func (p *redisPipeline) Get(ctx context.Context, key string) error {
	cmd := p.pipe.Get(ctx, key)
	p.ops = append(p.ops, pipelineOp{op: "get", key: key, getCmd: cmd})
	return nil
}

// Exists 向管道中添加一个 Exists 操作（仅记录，不立即执行）。
func (p *redisPipeline) Exists(ctx context.Context, key string) error {
	cmd := p.pipe.Exists(ctx, key)
	p.ops = append(p.ops, pipelineOp{op: "exists", key: key, existsCmd: cmd})
	return nil
}

// Execute 提交并执行管道中的所有操作，返回各操作的结果。
// 执行后管道被清空，可复用。
func (p *redisPipeline) Execute(ctx context.Context) ([]PipelineResult, error) {
	// 拷贝操作列表
	ops := p.ops
	p.ops = p.ops[:0] // 清空允许 Pipeline 复用

	if len(ops) == 0 {
		return []PipelineResult{}, nil
	}

	// 执行所有管道命令
	_, _ = p.pipe.Exec(ctx)

	// 组装结果
	results := make([]PipelineResult, len(ops))
	for i, op := range ops {
		results[i] = PipelineResult{Op: op.op, Key: op.key}
		switch op.op {
		case "set":
			results[i].Err = op.setCmd.Err()
		case "get":
			val, err := op.getCmd.Bytes()
			if err == redis.Nil {
				results[i].Value = nil
				results[i].Err = nil
			} else if err != nil {
				results[i].Err = err
			} else {
				results[i].Value = val
			}
		case "exists":
			results[i].Exists = op.existsCmd.Val() > 0
			results[i].Err = op.existsCmd.Err()
		}
	}

	return results, nil
}
```

- [ ] **Step 3: 添加接口编译验证（在 redis.go 末尾添加）**

```go
// 编译时校验 RedisStore 满足 BaseKVStore 接口
var _ BaseKVStore = (*RedisStore)(nil)
```

- [ ] **Step 4: 编译验证**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/store/kv/...
```

Expected: 编译成功，无错误

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/store/kv/redis.go && git commit -m "feat(kv): add Pipeline wrapper for RedisStore implementing KVPipeline interface"
```

---

### Task 6: 编写单元测试（miniredis）

**Files:**
- Create: `internal/agentcore/store/kv/redis_test.go`

- [ ] **Step 1: 编写测试辅助函数和构造函数测试**

```go
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
	// 注意：miniredis 不支持 Cluster，这里只验证类型断言逻辑
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

	// 创建 Standalone 客户端，但通过 Option 手动设置 ClusterClient
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
```

- [ ] **Step 2: 编写 Set/Get/Exists/Delete/ExclusiveSet 测试**

```go
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
```

- [ ] **Step 3: 编写 GetByPrefix / DeleteByPrefix / MGet / BatchDelete 测试**

```go
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
```

- [ ] **Step 4: 编写 Pipeline 和 RefreshTTL 测试**

```go
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
```

- [ ] **Step 5: 编写并发安全测试**

```go
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
```

- [ ] **Step 6: 运行测试验证**

```bash
cd /home/opensource/uap-claw-go && go test -v -count=1 ./internal/agentcore/store/kv/ -run TestRedisStore
```

Expected: 所有 RedisStore 测试通过

- [ ] **Step 7: 提交**

```bash
git add internal/agentcore/store/kv/redis_test.go && git commit -m "test(kv): add RedisStore unit tests using miniredis"
```

---

### Task 7: 更新 doc.go

**Files:**
- Modify: `internal/agentcore/store/kv/doc.go`

- [ ] **Step 1: 更新 doc.go 的文件目录和说明**

读取当前 `doc.go` 内容，更新为：

```go
// Package kv 提供键值存储的抽象接口定义和多种后端实现。
//
// 本包定义了所有 KV 存储后端必须满足的 BaseKVStore 接口，
// 以及用于批量操作的 KVPipeline 接口和 PipelineResult 结果类型。
// InMemoryKVStore 提供基于内存的并发安全实现，支持惰性过期检查。
// FileKVStore 提供基于 bbolt 的文件持久化实现，对应 Python ShelveStore，
// 严格复刻其语义（包括已知的值解包不一致和过期语义不一致）。
// DbBasedKVStore 提供基于 GORM 的数据库持久化实现，支持 SQLite/MySQL/PostgreSQL，
// 对应 Python DbBasedKVStore。
// RedisStore 提供基于 go-redis 的 Redis 实现，支持 Standalone 和 Cluster 两种模式，
// 对应 Python RedisStore。
//
// 文件目录：
//
//	kv/
//	├── doc.go           # 包文档
//	├── base.go          # BaseKVStore 接口 + KVPipeline 接口 + PipelineResult 结构体
//	├── in_memory.go     # InMemoryKVStore 内存实现 + inMemoryPipeline
//	├── file.go          # FileKVStore 文件持久化实现 + filePipeline
//	├── db_based.go      # DbBasedKVStore 数据库实现（GORM）+ dbBasedPipeline
//	└── redis.go         # RedisStore Redis 实现 + redisPipeline + Option
//
// 对应 Python 代码：openjiuwen/core/foundation/store/base_kv_store.py
//
//	InMemoryKVStore 对应: openjiuwen/core/foundation/store/kv/in_memory_kv_store.py
//	FileKVStore 对应:     openjiuwen/core/foundation/store/kv/shelve_store.py
//	DbBasedKVStore 对应:  openjiuwen/core/foundation/store/kv/db_based_kv_store.py
//	RedisStore 对应:      openjiuwen/extensions/store/kv/redis_store.py
//
// 核心类型/接口索引：
//
//	BaseKVStore      — KV 存储基础接口，定义 Get/Set/Delete 等单键操作
//	KVPipeline       — 批量操作接口，支持 Set/Get/Exists 管道和 Execute 提交
//	PipelineResult   — 管道操作结果，包含 Op/Key/Value/Exists/Err 字段
//	InMemoryKVStore  — 内存实现，并发安全，支持惰性过期检查
//	FileKVStore      — 文件持久化实现（bbolt），对应 Python ShelveStore
//	DbBasedKVStore   — 数据库持久化实现（GORM），支持 SQLite/MySQL/PostgreSQL
//	RedisStore       — Redis 实现（go-redis），支持 Standalone 和 Cluster 模式
package kv
```

- [ ] **Step 2: 提交**

```bash
git add internal/agentcore/store/kv/doc.go && git commit -m "docs(kv): update doc.go with RedisStore description and file entry"
```

---

### Task 8: 更新 IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 将步骤 4.5 的状态从 ☐ 更新为 ✅**

在 `IMPLEMENTATION_PLAN.md` 中找到：

```
| 4.5 | ☐ | RedisStore | Redis 实现 | `openjiuwen/extensions/store/redis_store.py` |
```

替换为：

```
| 4.5 | ✅ | RedisStore | Redis 实现 | `openjiuwen/extensions/store/redis_store.py` |
```

- [ ] **Step 2: 提交**

```bash
git add IMPLEMENTATION_PLAN.md && git commit -m "docs: mark step 4.5 RedisStore as completed"
```

---

### Task 9: 最终验证

**Files:** 无变更

- [ ] **Step 1: 运行 kv 包全部测试**

```bash
cd /home/opensource/uap-claw-go && go test -v -count=1 ./internal/agentcore/store/kv/...
```

Expected: 所有测试通过

- [ ] **Step 2: 检查测试覆盖率**

```bash
cd /home/opensource/uap-claw-go && go test -cover ./internal/agentcore/store/kv/...
```

Expected: 覆盖率 ≥ 85%

- [ ] **Step 3: 运行全项目编译检查**

```bash
cd /home/opensource/uap-claw-go && go build ./...
```

Expected: 编译成功
