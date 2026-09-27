# 7.25 Memory Common — DistributedLock 设计文档

> 对应 Python 源码：`openjiuwen/core/memory/common/distributed_lock.py`

## 1. 背景与定位

### 1.1 在 Agent 会话中的流程位置

```
用户输入 → Gateway → AgentServer → AgentLoop (ReAct)
                                  ├─ 1. ContextEngine 组装上下文
                                  │   ├─ FragmentMemory（片段记忆）← 7.6 ✅
                                  │   ├─ SummaryMemory（摘要记忆）← 7.7 ✅
                                  │   ├─ VariableMemory（变量记忆）← 7.7 ✅
                                  │   ├─ GraphMemory（图记忆）← 7.11 ✅
                                  │   └─ LongTermMemory（长期记忆）← 7.27 ☐
                                  ├─ 2. LLM 推理
                                  ├─ 3. Tool 执行
                                  └─ 4. 记忆写入
                                      └─ memory/common 提供底层工具：
                                          ├─ DistributedLock ← LongTermMemory 的用户级写锁
                                          ├─ GenerateIdxName/ParseMemtypeFromIdxName ← SemanticStore 索引名
                                          ├─ ParseMemoryHitInfos ← SemanticStore 命中解析
                                          └─ KvPrefixRegistry ← VariableManager + UserMemStore + KVMigrator
```

### 1.2 核心作用

`memory/common` 是记忆系统的**跨模块共享基础设施层**，不直接参与 Agent 会话流程，而是被上层记忆组件依赖：

| 组件 | 提供能力 | 被谁使用 |
|------|---------|---------|
| `DistributedLock` | 基于 KV Store 的分布式锁（acquire/release + UUID 防误删 + TTL 防死锁） | **仅** `LongTermMemory`（7.27），5 处调用 |
| `GenerateIdxName` / `ParseMemtypeFromIdxName` | 向量索引名生成/解析 | `SemanticStore`（7.9 已回填 ✅） |
| `ParseMemoryHitInfos` | 解析命中结果 (ids + scores) | `SemanticStore`（7.9 已回填 ✅） |
| `KvPrefixRegistry` | KV 前缀注册表（current + legacy 管理） | `VariableManager` + `UserMemStore` + `KVMigrator` |

### 1.3 已完成与待实现

| Python 文件 | 内容 | Go 对应 | 状态 |
|------------|------|---------|------|
| `base.py` | `generate_idx_name` / `parse_memtype_from_idx_name` / `parse_memory_hit_infos` | `base.go` | ✅ 已实现（7.9 回填） |
| `kv_prefix_registry.py` | `KvPrefixRegistry` + 全局实例 | `kv_prefix_registry.go` | ✅ 已实现（7.7 完成） |
| `distributed_lock.py` | `DistributedLock`（async + KV store） | `distributed_lock.go` | ❌ **待实现** |

## 2. 实现方案：1:1 复刻 Python

基于 `kv.BaseKVStore` 接口的分布式锁，1:1 对齐 Python `DistributedLock`：
- 自旋获取（固定 10ms 重试间隔）
- UUID 防误删
- TTL 防死锁
- Go 特有：ctx 取消支持

### 2.1 结构体与构造函数

```go
// DistributedLock 基于 KV Store 的分布式锁。
// 通过 ExclusiveSet 原子操作实现跨进程互斥，
// UUID 防误删，TTL 防死锁。
//
// Python: openjiuwen/core/memory/common/distributed_lock.py (DistributedLock)
type DistributedLock struct {
    // store KV 存储后端（Redis/InMemory 等）
    store kv.BaseKVStore
    // lockKey 锁键名，格式 "_lock/{lockName}"
    lockKey string
    // ttl 锁过期秒数，防止死锁
    ttl int
    // retryDelay 获取失败后的重试间隔
    retryDelay time.Duration
    // lockValue 当前锁持有者的唯一标识（UUID），防止误删
    lockValue string
}

// NewDistributedLock 创建分布式锁实例。
//
// Python: DistributedLock(store, lock_name)
func NewDistributedLock(store kv.BaseKVStore, lockName string) *DistributedLock
```

**对齐要点：**
- Python `__init__(self, store, lock_name)` → Go `NewDistributedLock(store, lockName)`
- Python `self.lock_key = "_lock/" + lock_name` → Go 同样拼接
- Python `self.ttl = 10` → Go `ttl = 10`
- Python `self.retry_delay = 0.01`（10ms）→ Go `retryDelay = 10 * time.Millisecond`
- Python `self.lock_value = None` → Go `lockValue = ""`（Acquire 时赋值）

### 2.2 Acquire — 自旋获取锁

```go
// Acquire 获取分布式锁。自旋调用 ExclusiveSet 直到成功或 ctx 被取消。
//
// 每次尝试生成新的 UUID 作为 lockValue，通过 ExclusiveSet 原子写入。
// 成功时 lockValue 被保存，用于 Release 时校验身份。
// 失败时等待 retryDelay 后重试。
//
// Python: DistributedLock.acquire()
func (l *DistributedLock) Acquire(ctx context.Context) error {
    l.lockValue = uuid.New().String()
    for {
        ok, err := l.store.ExclusiveSet(ctx, l.lockKey, []byte(l.lockValue), l.ttl)
        if err != nil {
            return err
        }
        if ok {
            return nil
        }
        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-time.After(l.retryDelay):
        }
    }
}
```

**对齐要点：**
- Python `self.lock_value = str(uuid.uuid4())` → Go `l.lockValue = uuid.New().String()`
- Python `while True: success = await self.store.exclusive_set(...)` → Go `for { ok, err := l.store.ExclusiveSet(...) }`
- Python `await asyncio.sleep(self.retry_delay)` → Go `time.After(l.retryDelay)`
- **Go 特有**：Python 的 `while True` 没有 cancellation 机制，Go 加 `ctx.Done()` 检查，避免 ctx 取消后无限自旋

### 2.3 Release — 释放锁

```go
// Release 释放分布式锁。校验 lockValue 一致后才删除，防止误删他人持有的锁。
// 释放失败时记录 Error 日志但不返回 error（对齐 Python 的 try/except 静默处理）。
//
// Python: DistributedLock.release()
func (l *DistributedLock) Release(ctx context.Context) error {
    val, err := l.store.Get(ctx, l.lockKey)
    if err != nil {
        return nil  // 对齐 Python：静默处理
    }
    if string(val) == l.lockValue {
        _ = l.store.Delete(ctx, l.lockKey)
    }
    return nil
}
```

**对齐要点：**
- Python `lock_key = await self.store.get(self.lock_key)` → Go `val, err := l.store.Get(ctx, l.lockKey)`
- Python `if lock_key == self.lock_value: await self.store.delete(self.lock_key)` → Go 同样先 get 比较再 delete
- Python `except Exception as e: memory_logger.error(...)` → Go 日志记录 + 返回 nil

### 2.4 WithLock — 便捷方法（对齐 Python context manager）

```go
// WithLock 获取锁后执行 fn，执行完毕自动释放锁。
// 对齐 Python 的 `async with DistributedLock(store, name):` 用法。
func (l *DistributedLock) WithLock(ctx context.Context, fn func(ctx context.Context) error) error {
    if err := l.Acquire(ctx); err != nil {
        return err
    }
    defer l.Release(ctx)
    return fn(ctx)
}
```

**Python 对比：**
```python
# Python 用法
lock = DistributedLock(self.kv_store, f"user/{user_id}")
async with lock:
    await self.write_manager.delete_mem_by_user_id(...)
```

```go
// Go 用法 A：手动 Acquire/Release + defer
lock := common.NewDistributedLock(kvStore, fmt.Sprintf("user/%s", userID))
if err := lock.Acquire(ctx); err != nil {
    return err
}
defer lock.Release(ctx)
// 临界区操作...

// Go 用法 B：WithLock 回调
lock := common.NewDistributedLock(kvStore, fmt.Sprintf("user/%s", userID))
return lock.WithLock(ctx, func(ctx context.Context) error {
    // 临界区操作...
    return nil
})
```

## 3. 日志对齐

Python `distributed_lock.py` 只有一处日志：

```python
memory_logger.error("Error releasing lock", exception=str(e), event_type=LogEventType.MEMORY_STORE)
```

Go 对应：

```go
logger.Error(logComponent).Err(err).Str("event_type", string(logger.LogEventMemoryStore)).Msg("释放锁失败")
```

- 组件常量使用 `logger.ComponentCommon`（common 属于基础设施层）
- `LogEventMemoryStore` 对齐 Python `LogEventType.MEMORY_STORE`

## 4. 文件组织

```
common/
├── doc.go                    # 包文档（更新）
├── base.go                   # 记忆索引名称生成/解析 + 命中结果解析（已有 ✅）
├── base_test.go              # base.go 测试（已有 ✅）
├── kv_prefix_registry.go     # KV 前缀注册表（已有 ✅）
├── kv_prefix_registry_test.go # kv_prefix_registry 测试（已有 ✅）
├── distributed_lock.go       # 分布式锁（新增 🆕）
└── distributed_lock_test.go  # 分布式锁测试（新增 🆕）
```

## 5. 测试策略

使用 `kv.BaseKVStore` 的 `InMemoryKVStore` 实现（已有），不依赖外部 Redis，无需 build tag。

| 测试用例 | 说明 |
|---------|------|
| `TestNewDistributedLock` | 构造函数参数正确 |
| `TestDistributedLock_Acquire` | 无竞争时一次获取成功 |
| `TestDistributedLock_Acquire_竞争自旋` | 两个锁争同一 key，第二个阻塞直到第一个释放 |
| `TestDistributedLock_Acquire_上下文取消` | ctx 取消后 Acquire 立即返回 error |
| `TestDistributedLock_Release` | 正常释放后其他锁可获取 |
| `TestDistributedLock_Release_非持有者不删` | lockValue 不匹配时不删除 |
| `TestDistributedLock_Release_锁不存在` | key 已过期/不存在时 Release 不报错 |
| `TestDistributedLock_WithLock` | WithLock 正常获取/释放 |
| `TestDistributedLock_WithLock_fn出错也释放` | fn 返回 error 时锁仍被释放 |

## 6. IMPLEMENTATION_PLAN.md 更新

7.25 状态从 `☐` 改为 `✅`，描述更新为：

```
| 7.25 | ✅ | Memory Common | 记忆公共工具（base.go + KvPrefixRegistry 已在 7.7/7.9 回填 ✅；DistributedLock ✅） | `openjiuwen/core/memory/common/` |
```

## 7. 回填关系

- 7.25 完成后，`DistributedLock` 可直接被 7.27 LongTermMemory 使用
- 无需回填到其他已完成章节（base.go 和 KvPrefixRegistry 已在 7.7/7.9 中完成）
- 7.9 的 `⤴️ 回填 memory/common/base.go` 标记已在此前完成，不受影响
