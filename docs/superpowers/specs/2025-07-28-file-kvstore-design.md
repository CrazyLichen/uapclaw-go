# FileKVStore (ShelveStore 等价) 设计

## 概述

领域四（存储层）4.3 小节的实现设计：Go 版 FileKVStore，基于 bbolt 的本地文件持久化 KV 存储，对照 Python 源码 `openjiuwen/core/foundation/store/kv/shelve_store.py` 进行迁移。基于 4.1 已完成的 `BaseKVStore` + `KVPipeline` 接口提供文件存储实现。

## 设计决策

| 决策项 | 选择 | 理由 |
|--------|------|------|
| 底层引擎 | bbolt (`go.etcd.io/bbolt`) | 纯 Go 嵌入式 KV 数据库，单文件 ACID 事务，零外部依赖，etcd 底层存储，生态成熟 |
| 行为对齐 | 严格复刻 Python ShelveStore | 保持跨语言可预测性，包括已知的值解包不一致和过期语义不一致 |
| 序列化格式 | 统一 JSON 结构体 | 所有值统一编码为 `fileEntry` JSON，ExpiryAt=0 表示不过期；简化判断逻辑，与 Python dict 包装器对应 |
| 并发模型 | 依赖 bbolt 自身事务控制 | bbolt 读写事务模型已保证 ACID：多只读事务可并发，写事务串行且互斥读；ExclusiveSet 的 read-then-write 在同一写事务中天然原子性 |
| 连接管理 | 单 DB 实例复用 + Close() | bbolt 推荐用法，性能更好；Python 每次打开/关闭，Go 改进 |
| 过期语义 | 对齐 Python ShelveStore | Get 返回已过期值、Exists 对过期 key 返回 true、仅 ExclusiveSet 检查过期 |
| 值解包 | 对齐 Python 不一致 | Get 解包，MGet/GetByPrefix/Pipeline Get 不解包 |

## 文件结构

```
internal/agentcore/store/kv/
├── doc.go              # 包文档（更新，添加 file.go 条目）
├── base.go             # BaseKVStore + KVPipeline + PipelineResult
├── in_memory.go        # InMemoryKVStore 实现
├── file.go             # FileKVStore 实现（新增）
└── file_test.go        # FileKVStore 单元测试（新增）
```

对应 Python 代码：`openjiuwen/core/foundation/store/kv/shelve_store.py`

## 数据结构

### fileEntry — 统一存储条目

```go
// fileEntry 文件存储条目，统一 JSON 序列化格式
// 对齐 Python：Set 存原始值，ExclusiveSet 存 {EXCLUSIVE_VALUE_KEY, EXCLUSIVE_EXPIRY_KEY} dict
// Go 统一用 JSON 结构体，ExpiryAt=0 表示不过期（等同于 Python 的普通 Set）
type fileEntry struct {
    // Value 实际存储的值（Base64 编码）
    Value string `json:"exclusive_value"`
    // ExpiryAt 过期时间戳（Unix 秒），0 表示不过期
    ExpiryAt int64 `json:"exclusive_expiry"`
}
```

存储格式示例：

| 操作 | 存储格式 | 说明 |
|------|---------|------|
| `Set(key, value)` | `{"exclusive_value":"AQID","exclusive_expiry":0}` | ExpiryAt=0，不过期 |
| `ExclusiveSet(key, value, 60)` | `{"exclusive_value":"AQID","exclusive_expiry":1753920000}` | ExpiryAt=绝对时间戳 |

### FileKVStore — 文件键值存储

```go
// FileKVStore 基于 bbolt 的文件持久化键值存储
// 对应 Python ShelveStore，严格复刻其语义（包括已知的值解包不一致）
type FileKVStore struct {
    // db bbolt 数据库实例，构造时打开，Close() 关闭
    db *bbolt.DB
    // bucketName 默认 bucket 名称
    bucketName string
}
```

### filePipeline — 文件 Pipeline 实现

```go
// filePipeline 文件存储 Pipeline 实现
type filePipeline struct {
    // ops 待执行的操作列表
    ops []operation
    // store 关联的 FileKVStore 实例
    store *FileKVStore
}
```

## 方法实现

### 构造函数

```go
// NewFileKVStore 创建基于 bbolt 的文件 KV 存储
// dbPath: 数据库文件路径（自动创建父目录）
// 对齐 Python: Path(db_path).parent.mkdir(parents=True, exist_ok=True)
func NewFileKVStore(dbPath string) (*FileKVStore, error)
```

构造时：
1. `os.MkdirAll(filepath.Dir(dbPath), 0755)` 创建父目录
2. `bbolt.Open(dbPath, 0600, nil)` 打开数据库
3. 创建默认 bucket（名称 `"default"`）

### Close 方法

```go
// Close 关闭数据库连接
func (s *FileKVStore) Close() error
```

Python ShelveStore 没有显式 Close，但 Go 的 bbolt 需要优雅关闭。

### Set

```
Set(ctx, key, value):
  1. 编码 fileEntry{Value: base64(value), ExpiryAt: 0}
  2. 开启 bbolt 写事务
  3. bucket.Put(key, jsonBytes)
  4. 提交事务
```

对齐 Python：`db[key] = value` 直接覆盖，`sync()` 刷盘。bbolt 写事务提交即刷盘。

### ExclusiveSet

```
ExclusiveSet(ctx, key, value, expiry):
  1. now = time.Now().Unix()
  2. 开启 bbolt 写事务
  3. 读取已有值 existing = bucket.Get(key)
  4. 如果 key 存在：
     a. 反序列化为 fileEntry
     b. 如果 existing.ExpiryAt == 0 或 existing.ExpiryAt > now：
        → 未过期，返回 false（拒绝）
     c. 如果 existing.ExpiryAt > 0 且 existing.ExpiryAt <= now：
        → 已过期，允许覆盖
  5. 计算 expireAt = now + expiry（如果 expiry > 0），否则 0
  6. 编码 fileEntry{Value: base64(value), ExpiryAt: expireAt}
  7. bucket.Put(key, jsonBytes)
  8. 提交事务
  9. 返回 true
```

对齐 Python 的 `exclusive_set` 逻辑：
- 普通 `Set` 写入的值 `ExpiryAt=0`，`ExclusiveSet` 检查 `ExpiryAt == 0` 视为"未过期"，返回 false — 与 Python 中普通 Set 存原始值导致 exclusive_set 返回 false 行为一致
- 已过期的 exclusive key 可以被覆盖

### Get

```
Get(ctx, key):
  1. 开启 bbolt 只读事务
  2. raw = bucket.Get(key)
  3. 如果 raw == nil：返回 nil, nil
  4. 反序列化为 fileEntry
  5. 返回 base64.Decode(entry.Value)  ← 解包！
```

对齐 Python：`get()` 解包 exclusive dict，返回实际值。**不过期检查**（与 InMemoryKVStore 不同，对齐 Python ShelveStore）。

### Exists

```
Exists(ctx, key):
  1. 开启 bbolt 只读事务
  2. raw = bucket.Get(key)
  3. 返回 raw != nil  ← 不过期检查！
```

对齐 Python：`exists()` 不检查过期，expired key 仍返回 true。

### Delete

```
Delete(ctx, key):
  1. 开启 bbolt 写事务
  2. 如果 key 存在：bucket.Delete(key)
  3. 提交事务
```

### GetByPrefix

```
GetByPrefix(ctx, prefix):
  1. 开启 bbolt 只读事务
  2. 遍历 bucket.Cursor()，筛选 key.HasPrefix(prefix)
  3. 收集结果 result[key] = raw（原始 JSON 字节） ← 不解包！
  4. 返回 result
```

对齐 Python：`get_by_prefix()` 返回原始值（dict 包装器），不解包。

### DeleteByPrefix

```
DeleteByPrefix(ctx, prefix, batchSize):
  1. 收集所有匹配前缀的 key 列表
  2. 如果 batchSize <= 0：一次性删除
  3. 否则：分批删除（每批 batchSize 个）
  4. 提交事务
```

对齐 Python：batch_size 参数存在但单次事务内删除，为 API 一致性保留参数。

### MGet

```
MGet(ctx, keys):
  1. 开启 bbolt 只读事务
  2. 对每个 key：raw = bucket.Get(key)
  3. 返回 [][]byte（原始 JSON 字节） ← 不解包！
```

对齐 Python：`mget()` 返回原始值，不解包。

### BatchDelete

```
BatchDelete(ctx, keys, batchSize):
  1. 开启 bbolt 写事务
  2. 遍历 keys，删除存在的 key，计数
  3. 提交事务
  4. 返回实际删除数量
```

### Pipeline

```go
// Pipeline 创建批量操作管道
func (s *FileKVStore) Pipeline(_ context.Context) KVPipeline
```

Pipeline 的 `Set`/`Get`/`Exists` 只追加操作列表。`Execute()` 开启单个写事务，依次执行所有操作：
- `set`：编码 `fileEntry{Value: base64(op.value), ExpiryAt: 0}`，直接 `bucket.Put`
- `get`：`bucket.Get(key)` 返回原始 JSON 字节 ← 不解包！
- `exists`：`bucket.Get(key) != nil`

对齐 Python：pipeline `set` 是普通 set（非 exclusive），pipeline `get` 返回原始值。

## 值解包不一致对照表

| 方法 | Python 行为 | Go FileKVStore 行为 | 解包？ |
|------|------------|---------------------|--------|
| `Get` | 解包 exclusive dict，返回实际值 | base64 解码 `fileEntry.Value` | ✅ 解包 |
| `MGet` | 返回原始 dict，不解包 | 返回原始 JSON 字节 | ❌ 不解包 |
| `GetByPrefix` | 返回原始 dict，不解包 | 返回原始 JSON 字节 | ❌ 不解包 |
| `Pipeline Get` | 返回原始 dict，不解包 | 返回原始 JSON 字节 | ❌ 不解包 |
| `Pipeline Set` | 普通覆盖，非 exclusive | 编码 `fileEntry{ExpiryAt:0}` | — |

## 过期语义对照表

| 方法 | Python ShelveStore | Go InMemoryKVStore | Go FileKVStore |
|------|-------------------|--------------------|-----------------|
| `Get` 过期 key | 返回值（不检查过期） | 返回 nil | 返回值（不检查过期）✅ |
| `Exists` 过期 key | true | false | true ✅ |
| `ExclusiveSet` 过期 key | 允许覆盖 | 允许覆盖 | 允许覆盖 ✅ |
| `ExclusiveSet` 未过期 key | 拒绝（false） | 拒绝（false） | 拒绝（false）✅ |

## Python → Go 实现映射

| Python | Go | 差异说明 |
|--------|-----|---------|
| `shelve.open(path, 'c', writeback=True)` | `bbolt.Open(path, 0600, nil)` | 单实例复用 vs 每次打开 |
| `db[key] = value` | `bucket.Put([]byte(key), jsonBytes)` | 统一 JSON 编码 |
| `db.sync()` | 写事务提交自动刷盘 | bbolt ACID 保证 |
| `db.get(key)` | `bucket.Get([]byte(key))` | bbolt 只读事务 |
| `key in db` | `bucket.Get([]byte(key)) != nil` | bbolt 无 `Exists` 方法 |
| `db.keys()` | `bucket.Cursor()` 遍历 | 游标迭代 |
| `isinstance(existing, dict) and EXCLUSIVE_EXPIRY_KEY in existing` | `entry.ExpiryAt > 0` | 统一 JSON 后判断简化 |
| `asyncio.run_in_executor` | 同步方法 + `context.Context` | Go 不需要异步包装 |
| `BasedKVStorePipeline(func)` | `filePipeline{store: s}` | 引用 store 而非闭包 |
| `Path(db_path).parent.mkdir(...)` | `os.MkdirAll(filepath.Dir(dbPath), 0755)` | 对齐 |

## 关键差异清单（Go 相对 Python 的改进/差异）

| # | 差异 | 原因 |
|---|------|------|
| 1 | 统一 JSON 序列化（Python 中 Set 存原始值，ExclusiveSet 存 dict） | Go 中统一格式，简化判断逻辑；行为等价（ExpiryAt=0 等同"不过期"） |
| 2 | 单 DB 实例复用 + Close() | bbolt 推荐用法，Python 每次打开/关闭 |
| 3 | 无 `_run_in_thread` | Go 是同步模型，bbolt 操作本身很快，无需异步包装 |
| 4 | 并发安全由 bbolt 事务保证 | Python 无锁不安全，Go 依赖 bbolt ACID |
| 5 | Base64 编码二进制值 | JSON 无法直接存 `[]byte`，需编码 |
| 6 | Pipeline 通过 store 引用执行 | Python 通过闭包传递 execute 函数 |

## 测试策略

文件：`file_test.go`，覆盖率目标 ≥ 85%，无需 build tag（bbolt 是纯 Go 库，`t.TempDir()` 提供临时目录）。

| 测试场景 | 覆盖方法 |
|---------|---------|
| 构造函数：正常创建 | `NewFileKVStore` |
| 构造函数：自动创建父目录 | `NewFileKVStore` |
| 构造函数：路径无效 | 返回 error |
| 基本 Set/Get | Set → Get 验证值一致 |
| Get 不存在的 key | 返回 nil |
| Delete 存在/不存在的 key | 正常删除 / 无报错 |
| Exists 存在/不存在 | true / false |
| ExclusiveSet 正常设置 | 新 key 返回 true |
| ExclusiveSet key 已存在拒绝 | 返回 false |
| ExclusiveSet key 已过期允许覆盖 | 返回 true |
| ExclusiveSet 带 expiry | 验证 ExpiryAt 时间戳正确 |
| Get 不过期检查（对齐 Python） | 过期 key 仍返回值 |
| Exists 不过期检查（对齐 Python） | 过期 key 返回 true |
| Get 解包 exclusive 值 | 返回实际 []byte，非 JSON 包装 |
| MGet 不解包 | 返回原始 JSON 字节 |
| GetByPrefix 不解包 | 返回原始 JSON 字节 |
| GetByPrefix 正常/无匹配 | 有结果 / 空 map |
| DeleteByPrefix 正常/无匹配/batchSize | 三种场景 |
| MGet 正常/部分不存在 | 有值 / nil |
| BatchDelete 正常/空列表/返回数量 | 三种场景 |
| Pipeline 混合 set+get+exists | 验证 PipelineResult |
| Pipeline 复用 | Execute 后再次操作正常 |
| Close 后操作 | 合理的 panic 或 error |
| 并发安全 | 多 goroutine 并发读写 |
| 数据持久化 | 写入后关闭再打开，数据仍在 |

## 日志

Python 原版 `shelve_store.py` 无任何 logger 调用，Go 实现也不添加日志。

## 范围说明

**4.3 范围内**：
- FileKVStore 结构体和全部 BaseKVStore 接口方法实现
- filePipeline 结构体和全部 KVPipeline 接口方法实现
- 内部辅助类型 fileEntry
- NewFileKVStore 构造函数和 Close 方法
- doc.go 包文档更新（添加 file.go 条目）
- file_test.go 单元测试

**不在 4.3 范围内**（后续步骤）：
- 4.4 DbBasedKVStore 实现
- 4.5 RedisStore 实现
