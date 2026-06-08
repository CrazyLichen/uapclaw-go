# InMemoryKVStore 设计

## 概述

领域四（存储层）4.2 小节的实现设计：Go 版 InMemoryKVStore 内存键值存储，对照 Python 源码 `openjiuwen/core/foundation/store/kv/in_memory_kv_store.py` 进行迁移。基于 4.1 已完成的 `BaseKVStore` + `KVPipeline` 接口提供内存实现。

## 设计决策

| 决策项 | 选择 | 理由 |
|--------|------|------|
| 并发模型 | `sync.RWMutex` | 读多写少场景性能优于 Mutex，Go 惯用方案 |
| 过期时间戳类型 | `int64`（Unix 秒） | 与 Python 原版对齐，0 表示不过期，简单直接 |
| Pipeline 实现 | 回调函数式（闭包） | 与 Python 的 `BasedKVStorePipeline(execute_func)` 对齐 |
| 过期清理策略 | 纯惰性（同 Python） | 只在读取时判断过期，不主动删除，允许 ExclusiveSet 覆盖过期 key |
| Pipeline 复用 | Execute() 后清空操作列表 | 与 Python 原版 `self._operations = []` 对齐 |
| 文件组织 | `in_memory.go`（同目录 `kv/`） | 与 Python 文件名对应，同包可直接访问内部类型 |

## 文件结构

```
internal/agentcore/store/kv/
├── doc.go              # 包文档
├── base.go             # BaseKVStore + KVPipeline + PipelineResult
├── base_test.go        # 接口编译验证测试
├── in_memory.go        # InMemoryKVStore 实现
└── in_memory_test.go   # InMemoryKVStore 单元测试
```

对应 Python 代码：`openjiuwen/core/foundation/store/kv/in_memory_kv_store.py`

## 数据结构

### entry — 存储条目

```go
// entry 内存存储条目，保存值和过期时间戳
type entry struct {
    // value 存储的值
    value []byte
    // expiryTs 过期时间戳（Unix 秒），0 表示不过期
    expiryTs int64
}
```

### operation — Pipeline 操作记录

```go
// operation Pipeline 操作记录
type operation struct {
    // op 操作类型："set"、"get"、"exists"
    op string
    // key 操作的键
    key string
    // value Set 操作的值，仅 op 为 "set" 时有效
    value []byte
}
```

### InMemoryKVStore — 内存键值存储

```go
// InMemoryKVStore 基于 内存的键值存储实现
type InMemoryKVStore struct {
    // mu 读写锁，保证并发安全
    mu sync.RWMutex
    // store 内部存储映射
    store map[string]entry
}
```

### inMemoryPipeline — 内存 Pipeline 实现

```go
// inMemoryPipeline 内存 Pipeline 实现
type inMemoryPipeline struct {
    // ops 待执行的操作列表
    ops []operation
    // exec 执行函数闭包，在 Pipeline() 方法中创建
    exec func(ops []operation) ([]PipelineResult, error)
}
```

## 方法实现

### 构造函数

```go
// NewInMemoryKVStore 创建新的内存键值存储实例
func NewInMemoryKVStore() *InMemoryKVStore
```

### 核心 CRUD 方法

| 方法 | 锁类型 | 说明 |
|------|--------|------|
| `Set(ctx, key, value) error` | 写锁 | 存储 key=value，过期时间 0 |
| `Get(ctx, key) ([]byte, error)` | 读锁 | 获取值，惰性检查过期，委托 `getWithoutLock` |
| `Exists(ctx, key) (bool, error)` | 读锁 | 检查 key 是否存在且未过期 |
| `Delete(ctx, key) error` | 写锁 | 删除 key，不存在时无操作 |
| `ExclusiveSet(ctx, key, value, expiry) (bool, error)` | 写锁 | 原子性条件设置：key 不存在或已过期时设置，否则拒绝 |

### 内部辅助方法

```go
// getWithoutLock 在无锁状态下读取值并检查过期
// 与 Python 的 _get_without_lock 对齐
// key 不存在返回 nil，已过期返回 nil（但不删除）
func (s *InMemoryKVStore) getWithoutLock(key string) []byte
```

### 批量操作方法

| 方法 | 锁类型 | 说明 |
|------|--------|------|
| `GetByPrefix(ctx, prefix) (map[string][]byte, error)` | 读锁 | 遍历匹配前缀的 key，调用 getWithoutLock 过滤过期 |
| `DeleteByPrefix(ctx, prefix, batchSize) error` | 写锁 | batchSize≤0 一次性删除，否则分批删除 |
| `MGet(ctx, keys) ([][]byte, error)` | 读锁 | 批量获取，逐个调用 getWithoutLock |
| `BatchDelete(ctx, keys, batchSize) (int, error)` | 写锁 | 返回实际删除数量，空列表返回 0 |

### Pipeline 方法

```go
// Pipeline 创建批量操作管道
// 闭包捕获 store 引用，Execute() 时加写锁批量执行
func (s *InMemoryKVStore) Pipeline(_ context.Context) KVPipeline
```

Pipeline 的 Set/Get/Exists 只追加操作列表（返回 nil error），Execute() 调用闭包批量执行并返回 `[]PipelineResult`，执行后清空操作列表允许复用。

## Python → Go 实现映射

| Python 实现 | Go 实现 | 差异说明 |
|-------------|---------|---------|
| `dict[str, tuple[str\|bytes, Optional[int]]]` | `map[string]entry` | entry 结构体替代元组，类型安全 |
| `asyncio.Lock()` | `sync.RWMutex` | 读写分离，读多写少场景更优 |
| `('set', key, value)` 元组 | `operation{op, key, value}` 结构体 | 结构体替代元组，字段语义清晰 |
| `BasedKVStorePipeline(func)` | `inMemoryPipeline{exec: func}` | 闭包模式对齐 |
| `_get_without_lock()` | `getWithoutLock()` | 方法名降级，逻辑完全一致 |
| `self._operations = []` | `p.ops = nil` | Execute 后清空，允许 Pipeline 复用 |
| `time.time()` | `time.Now().Unix()` | Unix 秒级时间戳 |
| `expiry: int\|None` | `expiry: int`（0=不过期） | 零值语义替代 Optional |
| `batch_size: Optional[int]` | `batchSize: int`（0=不分批） | 零值语义替代 Optional |

## ExclusiveSet 过期覆盖逻辑

与 Python 完全对齐：

```
ExclusiveSet(key, value, expiry):
    加写锁
    if key 存在于 store:
        if key 已过期 (expiryTs != 0 && now > expiryTs):
            允许覆盖 → 继续设置
        else:
            key 未过期 → 返回 false
    设置 key = (value, now + expiry if expiry > 0 else 0)
    返回 true
```

## 测试策略

文件：`in_memory_test.go`，覆盖率目标 ≥ 85%，无需 build tag（纯内存无外部依赖）。

| 测试场景 | 覆盖方法 |
|---------|---------|
| 基本增删改查 | Set / Get / Delete |
| 获取不存在的 key | Get 返回 nil |
| 删除不存在的 key | Delete 无报错 |
| ExclusiveSet 正常设置 | 新 key 设置成功返回 true |
| ExclusiveSet key 已存在拒绝 | 返回 false |
| ExclusiveSet key 已过期允许覆盖 | 返回 true |
| ExclusiveSet 带 expiry | 验证过期时间戳正确 |
| Exists 存在/不存在/已过期 | 三种场景 |
| GetByPrefix 正常/无匹配/含过期 key | 三种场景 |
| DeleteByPrefix 一次性/分批删除 | batchSize=0 和 batchSize>0 |
| MGet 正常/部分不存在/含过期 | 三种场景 |
| BatchDelete 正常/空列表/分批/返回数量 | 四种场景 |
| Pipeline 混合 set+get+exists | 验证 []PipelineResult 正确性 |
| Pipeline 复用 | Execute() 后再次操作可正常执行 |
| 并发安全 | 多 goroutine 并发读写 + -race 检测 |

## 日志

Python 原版 `in_memory_kv_store.py` 无任何 logger 调用，Go 实现也不添加日志。

## 范围说明

**4.2 范围内**：
- InMemoryKVStore 结构体和全部 BaseKVStore 接口方法实现
- inMemoryPipeline 结构体和全部 KVPipeline 接口方法实现
- 内部辅助类型 entry、operation
- getWithoutLock 辅助方法
- doc.go 包文档更新（添加 in_memory.go 条目）
- in_memory_test.go 单元测试

**不在 4.2 范围内**（后续步骤）：
- 4.3 ShelveStore 等价实现
- 4.4 DbBasedKVStore 实现
- 4.5 RedisStore 实现
