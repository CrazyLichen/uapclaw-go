# RedisStore 设计文档

> 日期：2025-07-30
> 对应 Python：`openjiuwen/extensions/store/kv/redis_store.py`
> 实现位置：`internal/agentcore/store/kv/redis.go`

## 1. 概述

RedisStore 是基于 go-redis/redis (v9) 的 Redis KV 存储实现，支持 Standalone 和 Cluster 两种模式。
它实现了 `BaseKVStore` 接口，与已有的 InMemoryKVStore、FileKVStore、DbBasedKVStore 并列，
为需要分布式 KV 存储的场景提供统一能力。

Python 中 RedisStore 位于 `extensions/store/kv/`（扩展包），而非 `core/foundation/store/kv/`，
表示它是可选扩展而非核心依赖。Go 版本同样作为可选依赖，通过 go-redis 间接引入。

## 2. 设计决策

| 决策项 | 选择 | 理由 |
|--------|------|------|
| Redis 客户端 | go-redis/redis v9 | 最主流，支持 Standalone + Cluster + Sentinel，API 与 Python redis.asyncio 对应清晰 |
| 内部客户端字段 | `redis.Cmdable` | 最小依赖，Client 和 ClusterClient 均满足此接口 |
| Cluster 检测 | 类型断言自动检测 | `_, ok := client.(*redis.ClusterClient)` 零配置自动检测 |
| Cluster 全节点 SCAN | 保存 `*redis.ClusterClient` 引用 | ForEachMaster 需要 ClusterClient 类型，Cmdable 上无此方法 |
| 方法风格 | 同步，对齐 BaseKVStore 接口 | 与已有三种实现一致，Go 惯用同步 + goroutine |
| 构造函数 | `NewRedisStore(client, opts...)` | 函数式选项，扩展性好 |
| MGet Cluster 容错 | 先 MGET，CROSSSLOT 失败回退 Pipeline+逐个 GET | 对齐 Python 回退策略 |
| Pipeline | 包装 `redis.Pipeliner` 为 `KVPipeline` | 利用 go-redis 原生 Pipeline 能力，减少自研复杂度 |
| RefreshTTL | RedisStore 独有导出方法 | 对齐 Python，不放入 BaseKVStore 接口 |
| 单元测试 | miniredis 内嵌模拟 | 覆盖率计入基线，不依赖外部服务 |

## 3. 核心结构体

```go
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
    // ops 操作元数据（key 和操作类型），用于 Execute 时组装 PipelineResult
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
```

### Cluster 模式判断

```go
// isCluster 返回是否为 Cluster 模式。
// 等价于 Python 的 self._is_cluster，但通过 clusterClient != nil 推断，无需额外字段。
func (s *RedisStore) isCluster() bool {
    return s.clusterClient != nil
}
```

## 4. 构造函数

```go
// NewRedisStore 创建基于 Redis 的 KV 存储实例。
//
// client: Redis 客户端，支持 *redis.Client（Standalone）和 *redis.ClusterClient（Cluster）。
// 构造函数自动通过类型断言检测 Cluster 模式，无需手动配置。
//
// 对齐 Python: RedisStore(redis: Redis | RedisCluster)
func NewRedisStore(client redis.Cmdable, opts ...Option) *RedisStore
```

### 类型断言自动检测

```go
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
```

### Option 函数式选项

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

**注意**：移除了之前讨论的 `WithIsCluster` 选项。Cluster 模式完全由 `clusterClient != nil` 判断，
自动检测已覆盖绝大多数场景，`WithClusterClient` 仅用于代理/包装器等特殊情况。

### 调用方示例

```go
// Standalone 模式
client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
store := kv.NewRedisStore(client)

// Cluster 模式（自动检测）
clusterClient := redis.NewClusterClient(&redis.ClusterOptions{Addrs: []string{...}})
store := kv.NewRedisStore(clusterClient)
```

## 5. 方法实现策略

### 5.1 单键操作（Standalone/Cluster 通用）

| 方法 | go-redis 命令 | 说明 |
|------|-------------|------|
| `Set` | `client.Set(ctx, key, value, 0)` | ttl=0 表示不过期 |
| `ExclusiveSet` | `client.SetNX(ctx, key, value, time.Duration(expiry)*time.Second)` | expiry=0 走旧版 SETNX，>0 走 SET NX EX |
| `Get` | `client.Get(ctx, key)` | 返回 []byte，nil 表示不存在 |
| `Exists` | `client.Exists(ctx, key)` | 返回 bool |
| `Delete` | `client.Del(ctx, key)` | 删除不存在 key 不报错 |

### 5.2 GetByPrefix（Cluster 差异化）

**Standalone 模式**：

```go
func (s *RedisStore) GetByPrefix(ctx context.Context, prefix string) (map[string][]byte, error) {
    result := make(map[string][]byte)
    pattern := prefix + "*"
    var cursor uint64
    for {
        var keys []string
        keys, cursor = s.client.Scan(ctx, cursor, pattern, 100).Val()
        for _, key := range keys {
            val, err := s.client.Get(ctx, key).Bytes()
            if err == redis.Nil {
                continue  // key 在 SCAN 和 GET 之间被删除
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
    return result, nil
}
```

**Cluster 模式**（使用 ForEachMaster）：

```go
func (s *RedisStore) GetByPrefixCluster(ctx context.Context, prefix string) (map[string][]byte, error) {
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
    return result, err
}
```

### 5.3 DeleteByPrefix

对齐 GetByPrefix 策略：
- Standalone：SCAN 收集 keys + 批量 Del
- Cluster：ForEachMaster + SCAN 收集 keys + 批量 Del
- batchSize=0 一次性删除，>0 分批删除
- 对齐 Python: batch_size 未指定时默认不分批，收集所有 key 后一次性删除

**Standalone 模式**：

```go
func (s *RedisStore) DeleteByPrefix(ctx context.Context, prefix string, batchSize int) error {
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
    return nil
}
```

**Cluster 模式**：使用 `ForEachMaster` 遍历每个 master 节点，在每个节点上 SCAN 收集 key 后删除。
逻辑与 GetByPrefix 类似，只是收集 key 后执行 Del 而非 Get。

### 5.4 MGet（Cluster 容错回退）

对齐 Python：先尝试 MGET，失败时回退到 Pipeline+逐个 GET（不分 Standalone/Cluster）。

```go
func (s *RedisStore) MGet(ctx context.Context, keys []string) ([][]byte, error) {
    if len(keys) == 0 {
        return [][]byte{}, nil
    }

    // 尝试 MGET
    vals, err := s.client.MGet(ctx, keys...).Result()
    if err != nil {
        // MGET 失败（可能是 Cluster CROSSSLOT 或其他原因），回退到 Pipeline+逐个 GET
        // 对齐 Python: try MGET → except → fallback to individual GETs
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
    return result, nil
}

// mGetFallback 使用 Pipeline 逐个 GET 的回退方案
func (s *RedisStore) mGetFallback(ctx context.Context, keys []string) ([][]byte, error) {
    pipe := s.client.Pipeline()
    cmds := make([]*redis.StringCmd, len(keys))
    for i, key := range keys {
        cmds[i] = pipe.Get(ctx, key)
    }
    _, _ = pipe.Exec(ctx)  // 忽略整体错误，逐个读取结果

    result := make([][]byte, len(keys))
    for i, cmd := range cmds {
        val, err := cmd.Bytes()
        if err == redis.Nil {
            continue
        }
        if err != nil {
            continue  // 对齐 Python：单个 key 失败时对应位置为 nil
        }
        result[i] = val
    }
    return result, nil
}
```

### 5.5 BatchDelete

与 Standalone 逻辑一致，go-redis Pipeline 自动按 slot 分组。

```go
func (s *RedisStore) BatchDelete(ctx context.Context, keys []string, batchSize int) (int, error) {
    if len(keys) == 0 {
        return 0, nil
    }
    if batchSize <= 0 {
        // 一次性删除
        return int(s.client.Del(ctx, keys...).Val()), nil
    }
    // 分批删除
    totalDeleted := 0
    for i := 0; i < len(keys); i += batchSize {
        end := i + batchSize
        if end > len(keys) {
            end = len(keys)
        }
        totalDeleted += int(s.client.Del(ctx, keys[i:end]...).Val())
    }
    return totalDeleted, nil
}
```

### 5.6 Pipeline（包装 redis.Pipeliner）

```go
func (s *RedisStore) Pipeline(ctx context.Context) KVPipeline {
    return &redisPipeline{
        pipe: s.client.Pipeline(),
        ops:  make([]pipelineOp, 0),
    }
}

func (p *redisPipeline) Set(ctx context.Context, key string, value []byte, expiry int) error {
    var ttl time.Duration
    if expiry > 0 {
        ttl = time.Duration(expiry) * time.Second
    }
    cmd := p.pipe.Set(ctx, key, value, ttl)
    p.ops = append(p.ops, pipelineOp{op: "set", key: key, setCmd: cmd})
    return nil
}

func (p *redisPipeline) Get(ctx context.Context, key string) error {
    cmd := p.pipe.Get(ctx, key)
    p.ops = append(p.ops, pipelineOp{op: "get", key: key, getCmd: cmd})
    return nil
}

func (p *redisPipeline) Exists(ctx context.Context, key string) error {
    cmd := p.pipe.Exists(ctx, key)
    p.ops = append(p.ops, pipelineOp{op: "exists", key: key, existsCmd: cmd})
    return nil
}

func (p *redisPipeline) Execute(ctx context.Context) ([]PipelineResult, error) {
    // 执行所有管道命令
    _, _ = p.pipe.Exec(ctx)

    // 组装结果
    results := make([]PipelineResult, len(p.ops))
    for i, op := range p.ops {
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

    // 清空操作，允许 Pipeline 复用
    p.ops = p.ops[:0]

    return results, nil
}
```

### 5.7 RefreshTTL（Redis 独有方法）

```go
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
        return nil  // 对齐 Python：静默忽略
    }
    return nil
}
```

## 6. 方法对照表

| Go 方法 | Python 方法 | 差异说明 |
|---------|------------|---------|
| `Set(ctx, key, value)` | `set(key, value)` | value 统一 []byte，Redis 原生存储 |
| `ExclusiveSet(ctx, key, value, expiry)` | `exclusive_set(key, value, expiry)` | expiry=0 走 SETNX，>0 走 SET NX EX |
| `Get(ctx, key)` | `get(key)` | 统一返回 []byte，nil 表示不存在 |
| `Exists(ctx, key)` | `exists(key)` | Python 无 try/except，Go 返回 error |
| `Delete(ctx, key)` | `delete(key)` | 无差异 |
| `GetByPrefix(ctx, prefix)` | `get_by_prefix(prefix)` | Cluster 用 ForEachMaster 全节点 SCAN |
| `DeleteByPrefix(ctx, prefix, batchSize)` | `delete_by_prefix(prefix, batch_size)` | Cluster 用 ForEachMaster 全节点 SCAN |
| `MGet(ctx, keys)` | `mget(keys)` | Cluster 下 CROSSSLOT 回退 Pipeline+GET |
| `BatchDelete(ctx, keys, batchSize)` | `batch_delete(keys, batch_size)` | 无差异 |
| `Pipeline(ctx)` | `pipeline()` | 包装 redis.Pipeliner 为 KVPipeline |
| `RefreshTTL(ctx, keys, ttlSeconds)` | `refresh_ttl(keys, ttl_seconds)` | 独有方法，不在 BaseKVStore 接口中 |

## 7. 过期语义对比

Redis 原生支持 TTL，与其他实现的过期语义不同：

| 行为 | InMemoryKVStore | FileKVStore | DbBasedKVStore | **RedisStore** |
|------|-----------------|-------------|----------------|----------------|
| **Set 过期** | 不支持（expiryTs=0） | 不支持（ExpiryAt=0） | 不支持（原始 BLOB） | **不支持**（ttl=0） |
| **ExclusiveSet 过期** | 应用层管理 | 应用层管理 | JSON 包装 | **Redis 原生 EX** |
| **Get 过期 key** | 返回 nil | 返回值 | 返回值 | **返回 nil**（Redis 自动删除） |
| **Exists 过期 key** | 返回 false | 返回 true | 返回 true | **返回 false**（Redis 自动删除） |
| **ExclusiveSet 过期 key** | 允许覆盖 | 允许覆盖 | 允许覆盖 | **允许覆盖**（NX 检测不存在） |

RedisStore 利用 Redis 原生 TTL 机制，过期由 Redis 服务端自动管理，
无需应用层惰性检查或 JSON 包装。这是最正确的过期实现方式。

## 8. 文件结构

```
kv/
├── doc.go              # 包文档（需更新文件目录和 RedisStore 说明）
├── base.go             # BaseKVStore 接口 + KVPipeline 接口 + PipelineResult 结构体
├── in_memory.go        # InMemoryKVStore 内存实现 + inMemoryPipeline
├── file.go             # FileKVStore 文件持久化实现 + filePipeline
├── db_based.go         # DbBasedKVStore 数据库实现 + dbBasedPipeline
├── redis.go            # RedisStore Redis 实现 + redisPipeline + Option（新增）
├── base_test.go
├── in_memory_test.go
├── file_test.go
├── db_based_test.go
└── redis_test.go       # RedisStore 测试（miniredis）（新增）
```

## 9. 测试策略

### 9.1 单元测试（miniredis）

使用 `github.com/alicebob/miniredis/v2` 作为内嵌 Redis 模拟器：

- 所有 BaseKVStore 接口方法的正常路径测试
- ExclusiveSet 的过期语义测试
- MGet 的部分不存在测试
- BatchDelete 的分批删除测试
- Pipeline 的混合操作和结果顺序测试
- 并发安全测试（多 goroutine 并发读写）
- RefreshTTL 独有方法测试
- NewRedisStore 构造函数测试（自动检测 Cluster）

覆盖率计入基线，不依赖外部服务。

### 9.2 集成测试（//go:build integration）

使用真实 Redis 服务：
- Standalone 模式的完整功能验证
- Cluster 模式的 ForEachMaster 全节点 SCAN 验证
- Cluster 模式下 MGet CROSSSLOT 回退验证
- 大数据量下的 SCAN 性能验证

### 9.3 miniredis 限制

miniredis 不支持 Cluster 模式，因此：
- Cluster 特有逻辑（ForEachMaster、CROSSSLOT 回退）通过 integration 测试覆盖
- 单元测试中 Cluster 相关方法走 Standalone 路径的基本逻辑
- 构造函数 `WithClusterClient` 选项测试用 mock 验证

## 10. 依赖变更

go.mod 新增：
```
github.com/redis/go-redis/v9
github.com/alicebob/miniredis/v2  // 测试依赖
```

## 11. 日志对齐

按照项目日志同步规则，逐条对照 Python `logger.` 调用：

| Python 日志点 | Go 日志 | 级别 |
|--------------|---------|------|
| `logger.debug(f"Successfully set key: {key}")` | `logger.Debug(logComponent).Str("key", key).Msg("成功设置 key")` | Debug |
| `logger.error(f"Failed to set key: {key}, error: {e}")` | `logger.Error(logComponent).Str("key", key).Err(err).Msg("设置 key 失败")` | Error |
| `logger.debug(f"Exclusive set key: {key}, result: {bool(result)}")` | `logger.Debug(logComponent).Str("key", key).Bool("result", ok).Msg("排他设置 key")` | Debug |
| `logger.warning(f"MGET failed, falling back...")` | `logger.Warn(logComponent).Err(err).Msg("MGET 失败，回退到逐个 GET")` | Warn |
| `logger.warning(f"Failed to refresh TTL...")` | `logger.Warn(logComponent).Int("key_count", len(keys)).Err(err).Msg("刷新 TTL 失败")` | Warn |
| 其他方法类似 | 对齐 Python 逐条映射 | — |

日志组件使用 `ComponentAgentCore`（agentcore 下的包统一使用此组件）。
