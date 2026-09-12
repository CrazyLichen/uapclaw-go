# Memory 包 any 类型收紧设计

> 日期：2025-07-15
> 状态：已确认

## 背景

对 `internal/agentcore/memory/` 包下所有 `map[string]any` / `any` 使用点进行审查，识别出可收紧的类型不安全场景。经 brainstorming 逐项讨论后，确认 2 项修复、3 项不修。

## 审查结论

### 修复项

| # | 问题 | 位置 | 决策 |
|---|------|------|------|
| 1 | MemorySettings 5 个子配置 `map[string]any` | `lite/config.go` | 替换为 typed struct |
| 2 | UserMemStore CRUD 返回 `map[string]any` | `manage/mem_model/user_mem_store.go` | 定义 UserMemoryRecord struct |

### 不修项

| # | 问题 | 位置 | 决策 | 原因 |
|---|------|------|------|------|
| 3 | CodingMemoryWriteWithContext 返回 `map[string]any` | `lite/coding_memory_tool_ops.go` | 留后续 | Tool.Invoke 接口返回 `map[string]any`，改 struct 仍需 ToDict 桥接 |
| 4 | SqlDbQuerier / SqlDbStore `map[string]any` | `migration/migrator/` + `manage/mem_model/sql_db_store.go` | 不修 | GORM 通用 CRUD + 多表操作，map 是自然选择。`migrator` → `mem_model` 无循环依赖（单向依赖通过接口注入解耦） |
| 5 | `parseCheckItems(parsed any)` 等 | `manage/update/update_checker.go` 等 | 不修 | LLM 输出无结构保证 / migration 脚本对齐 Python Callable / stdlib 签名 |

## 修复项 1：MemorySettings typed struct

### 新增结构体定义

```go
// ChunkingConfig 分块配置。对齐 Python MemorySettings.chunking
type ChunkingConfig struct {
    // Tokens 分块 token 数，默认 256
    Tokens float64
    // Overlap 重叠 token 数，默认 32
    Overlap float64
}

// HybridConfig 混合搜索配置。对齐 Python MemorySettings.query.hybrid
type HybridConfig struct {
    // Enabled 是否启用混合搜索，默认 true
    Enabled bool
    // VectorWeight 向量搜索权重，默认 0.7
    VectorWeight float64
    // TextWeight 文本搜索权重，默认 0.3
    TextWeight float64
    // CandidateMultiplier 候选倍数，默认 2.0
    CandidateMultiplier float64
}

// QueryConfig 查询配置。对齐 Python MemorySettings.query
type QueryConfig struct {
    // MaxResults 最大结果数，默认 10
    MaxResults float64
    // MinScore 最小相关度分数，默认 0.3
    MinScore float64
    // Hybrid 混合搜索配置
    Hybrid HybridConfig
}

// VectorStoreConfig 向量存储配置。对齐 Python MemorySettings.store.vector
type VectorStoreConfig struct {
    // Enabled 是否启用向量存储，默认 true
    Enabled bool
}

// FtsConfig 全文搜索配置。对齐 Python MemorySettings.store.fts
type FtsConfig struct {
    // Enabled 是否启用全文搜索，默认 true
    Enabled bool
}

// StoreConfig 存储配置。对齐 Python MemorySettings.store
type StoreConfig struct {
    // Path 数据库文件路径，默认 "memory.db"
    Path string
    // Vector 向量存储配置
    Vector VectorStoreConfig
    // Fts 全文搜索配置
    Fts FtsConfig
}

// SyncConfig 同步配置。对齐 Python MemorySettings.sync
type SyncConfig struct {
    // Watch 是否监听文件变更，默认 true
    Watch bool
    // WatchDebounceMs 防抖毫秒数，默认 2000
    WatchDebounceMs int
    // OnSearch 搜索前同步，默认 true
    OnSearch bool
    // OnSessionStart 会话开始时同步，默认 true
    OnSessionStart bool
    // IntervalMinutes 定时同步间隔分钟数，默认 0（禁用）
    IntervalMinutes int
}

// CacheConfig 缓存配置。对齐 Python MemorySettings.cache
type CacheConfig struct {
    // Enabled 是否启用缓存，默认 true
    Enabled bool
    // MaxEntries 最大缓存条目数，默认 10000
    MaxEntries float64
}
```

### MemorySettings 变更

```go
// 修改前
type MemorySettings struct {
    Provider   string
    Model      string
    Fallback   string
    Sources    []string
    ExtraPaths []string
    Chunking   map[string]any
    Query      map[string]any
    Store      map[string]any
    Sync       map[string]any
    Cache      map[string]any
}

// 修改后
type MemorySettings struct {
    Provider   string
    Model      string
    Fallback   string
    Sources    []string
    ExtraPaths []string
    Chunking   ChunkingConfig
    Query      QueryConfig
    Store      StoreConfig
    Sync       SyncConfig
    Cache      CacheConfig
}
```

### 数值字段统一 float64

**决策原因**：`CreateMemorySettings` 中默认值写入 `int`（如 `"tokens": 256`），但 JSON/YAML 反序列化时 `int` 会被解析为 `float64`。Python 不区分 int/float，统一用 float64 避免类型断言风险。

### CreateMemorySettings 变更

**默认值构造**改为直接赋值 struct 字段：

```go
s := &MemorySettings{
    Provider:   "openai_compatible",
    Model:      "text-embedding-v3",
    Fallback:   "mock",
    Sources:    []string{"memory", "sessions"},
    Chunking: ChunkingConfig{
        Tokens:  256,
        Overlap: 32,
    },
    Query: QueryConfig{
        MaxResults: 10,
        MinScore:   0.3,
        Hybrid: HybridConfig{
            Enabled:             true,
            VectorWeight:        0.7,
            TextWeight:          0.3,
            CandidateMultiplier: 2.0,
        },
    },
    Store: StoreConfig{
        Path: "memory.db",
        Vector: VectorStoreConfig{Enabled: true},
        Fts:    FtsConfig{Enabled: true},
    },
    Sync: SyncConfig{
        Watch:           true,
        WatchDebounceMs: 2000,
        OnSearch:        true,
        OnSessionStart:  true,
    },
    Cache: CacheConfig{
        Enabled:    true,
        MaxEntries: 10000,
    },
}
```

**overrides 合并逻辑**保持 `map[string]any` 外部接口不变，内部按 key 分支解析到对应 struct 字段：

```go
case "chunking":
    if v, ok := value.(map[string]any); ok {
        if v2, ok := v["tokens"].(float64); ok { s.Chunking.Tokens = v2 }
        if v2, ok := v["tokens"].(int); ok { s.Chunking.Tokens = float64(v2) }
        if v2, ok := v["overlap"].(float64); ok { s.Chunking.Overlap = v2 }
        if v2, ok := v["overlap"].(int); ok { s.Chunking.Overlap = float64(v2) }
    }
case "query":
    if v, ok := value.(map[string]any); ok {
        if v2, ok := v["max_results"].(float64); ok { s.Query.MaxResults = v2 }
        if v2, ok := v["min_score"].(float64); ok { s.Query.MinScore = v2 }
        if hybrid, ok := v["hybrid"].(map[string]any); ok {
            if v2, ok := hybrid["enabled"].(bool); ok { s.Query.Hybrid.Enabled = v2 }
            if v2, ok := hybrid["vectorWeight"].(float64); ok { s.Query.Hybrid.VectorWeight = v2 }
            if v2, ok := hybrid["textWeight"].(float64); ok { s.Query.Hybrid.TextWeight = v2 }
            if v2, ok := hybrid["candidateMultiplier"].(float64); ok { s.Query.Hybrid.CandidateMultiplier = v2 }
        }
    }
// ... 同理 store/sync/cache
```

### manager_impl.go 消费方变更（约 20 处）

| 改前 | 改后 |
|------|------|
| `m.settings.Chunking["tokens"].(int)` | `int(m.settings.Chunking.Tokens)` |
| `m.settings.Chunking["overlap"].(int)` | `int(m.settings.Chunking.Overlap)` |
| `m.settings.Query["min_score"].(float64)` | `m.settings.Query.MinScore` |
| `m.settings.Query["max_results"].(float64)` | `m.settings.Query.MaxResults` |
| `m.settings.Query["hybrid"].(map[string]any)` | `m.settings.Query.Hybrid` |
| `hybrid["candidateMultiplier"].(float64)` | `m.settings.Query.Hybrid.CandidateMultiplier` |
| `hybrid["enabled"].(bool)` | `m.settings.Query.Hybrid.Enabled` |
| `m.settings.Store["fts"].(map[string]any); v["enabled"].(bool)` | `m.settings.Store.Fts.Enabled` |
| `m.settings.Store["vector"].(map[string]any); v["enabled"].(bool)` | `m.settings.Store.Vector.Enabled` |
| `m.settings.Store["path"].(string)` | `m.settings.Store.Path` |
| `m.settings.Sync["watch"].(bool)` | `m.settings.Sync.Watch` |
| `m.settings.Sync["onSearch"].(bool)` | `m.settings.Sync.OnSearch` |
| `m.settings.Sync["watchDebounceMs"].(int)` | `m.settings.Sync.WatchDebounceMs` |
| `m.settings.Sync["intervalMinutes"].(int)` | `m.settings.Sync.IntervalMinutes` |
| `m.settings.Cache["enabled"].(bool)` | `m.settings.Cache.Enabled` |

### writeMeta 消费方变更

`writeMeta` 接受 `map[string]any`（写入 DB 的元数据），构造 meta 时：

```go
// 修改前
meta := map[string]any{
    "chunkTokens":  m.settings.Chunking["tokens"],
    "chunkOverlap": m.settings.Chunking["overlap"],
}

// 修改后
meta := map[string]any{
    "chunkTokens":  m.settings.Chunking.Tokens,
    "chunkOverlap": m.settings.Chunking.Overlap,
}
```

`shouldFullReindex` 中 `meta["chunkTokens"] != m.settings.Chunking["tokens"]` 改为 `meta["chunkTokens"] != m.settings.Chunking.Tokens`。

### config_test.go 变更

```go
// 修改前
tokens, _ := s.Chunking["tokens"].(int)
hybrid, _ := s.Query["hybrid"].(map[string]any)

// 修改后
if s.Chunking.Tokens != 256 { ... }
if s.Query.Hybrid.Enabled != true { ... }
```

## 修复项 2：UserMemStore typed struct

### 新增结构体

```go
// UserMemoryRecord 用户记忆数据记录。
// 替代 map[string]any，提供类型安全的记忆数据访问。
// 对齐 Python UserMemStore 中 dict 格式的记忆数据
type UserMemoryRecord struct {
    // MemType 记忆类型（如 "user_profile"、"semantic_memory"、"summary"）
    MemType string
    // Content 记忆内容
    Content string
    // Topic 主题（可选，用于用户画像主题索引）
    Topic string
    // Timestamp 时间戳（可选，ISO 格式字符串）
    Timestamp string
}
```

**字段说明**：基于代码中实际使用的字段：
- `memTypeFieldKey = "mem_type"` → `MemType`
- `Write`/`Update` 中 `data["content"]` → `Content`
- `GetByTopic` 中按 `topic` 查询 → `Topic`
- 可能存在的 `timestamp` → `Timestamp`

### 上游调用方分析

**重要发现**：`UserMemStore` 当前没有生产代码调用方——`NewUserMemStore` 仅在测试中被调用。改动风险极低，不会破坏任何上游代码。

但 `UserMemoryRecord` 的字段定义需对照 Python 端 `UserMemStore.write` 实际传入的 dict 键确认完整性。实现时应搜索 Python 源码中调用 `self._user_mem_store.write(...)` 的位置，确认实际写入字段。

### 方法签名变更

| 方法 | 改前 | 改后 |
|------|------|------|
| `Write` | `(data map[string]any) (bool, error)` | `(data *UserMemoryRecord) (bool, error)` |
| `Update` | `(data map[string]any) (bool, error)` | **保持** `(data map[string]any) (bool, error)` |
| `Get` | `(map[string]any, error)` | `(*UserMemoryRecord, error)` |
| `BatchGet` | `([]map[string]any, error)` | `([]*UserMemoryRecord, error)` |
| `GetAll` | `([]map[string]any, error)` | `([]*UserMemoryRecord, error)` |
| `GetByTopic` | `([]map[string]any, error)` | `([]*UserMemoryRecord, error)` |
| `GetInRange` | `([]map[string]any, error)` | `([]*UserMemoryRecord, error)` |
| `get` (内部) | `(map[string]any, error)` | `(*UserMemoryRecord, error)` |

### Write 方法变更

```go
// 修改前
func (s *UserMemStore) Write(ctx context.Context, userID, scopeID, memID string, data map[string]any) (bool, error) {
    ...
    jsonData, err := json.Marshal(data)
    ...
    if memType, ok := data[memTypeFieldKey]; ok {
        memTypeStr, ok := memType.(string)
        if !ok { return false, fmt.Errorf(...) }
        ...
    }
}

// 修改后
func (s *UserMemStore) Write(ctx context.Context, userID, scopeID, memID string, data *UserMemoryRecord) (bool, error) {
    ...
    jsonData, err := json.Marshal(data)
    ...
    if data.MemType != "" {
        memTypeStr := data.MemType  // 直接使用，无需类型断言
        ...
    }
}
```

### Get / BatchGet 方法变更

```go
// 修改前（get 内部方法）
func (s *UserMemStore) get(ctx context.Context, memKey string) (map[string]any, error) {
    ...
    var result map[string]any
    if err := json.Unmarshal(memValue, &result); err != nil { return nil, err }
    return result, nil
}

// 修改后
func (s *UserMemStore) get(ctx context.Context, memKey string) (*UserMemoryRecord, error) {
    ...
    var result UserMemoryRecord
    if err := json.Unmarshal(memValue, &result); err != nil { return nil, err }
    return &result, nil
}
```

BatchGet 中 `[]map[string]any` → `[]*UserMemoryRecord`，`json.Unmarshal` 反序列化目标从 `var m map[string]any` 改为 `var m UserMemoryRecord`。

### Update 方法（保持 map）

Update 需要做字段级合并（只覆盖传入的字段），保持 `map[string]any` 参数：

```go
// 保持不变
func (s *UserMemStore) Update(ctx context.Context, userID, scopeID, memID string, data map[string]any) (bool, error) {
    ...
    var dictValue map[string]any
    if err := json.Unmarshal(oldData, &dictValue); err != nil { return false, err }
    for newKey, newValue := range data {
        dictValue[newKey] = newValue  // 字段级合并
    }
    ...
}
```

### innerDelete 方法变更

内部读取数据仍需解析 `mem_type`，从 `map[string]any` 改为 `UserMemoryRecord`：

```go
// 修改前
var dictValue map[string]any
if err := json.Unmarshal(data, &dictValue); err == nil {
    if memType, ok := dictValue[memTypeFieldKey]; ok {
        memTypeStr, ok := memType.(string)
        ...
    }
}

// 修改后
var record UserMemoryRecord
if err := json.Unmarshal(data, &record); err == nil {
    if record.MemType != "" {
        memTypeStr := record.MemType
        ...
    }
}
```

### 测试文件变更

- `Write` 调用从 `map[string]any{"mem_type": "user_profile", "content": "test"}` 改为 `&UserMemoryRecord{MemType: "user_profile", Content: "test"}`
- `Update` 调用保持 `map[string]any{"content": "updated"}`
- `Get` 返回值从 `result["content"]` 改为 `result.Content`
- `BatchGet`/`GetAll` 等返回值从 `len(result)` / `result[i]["content"]` 改为 struct 字段访问

## 不修项补充说明

### SqlDbQuerier / SqlDbStore

经审查确认：
- `migrator` 包只 import `context`，不依赖 `mem_model`
- 依赖方向为单向：`mem_model` → `migrator`（通过接口注入 `SqlDbStore`）
- `SqlDbQuerier` 的 `map[string]any` 不是因为循环依赖被迫使用 any
- 真实原因是 GORM `db.Table(table).Create(data)` 天然接受 `map[string]any`，且 conditions 值类型多样（string/[]string/[]any 用于 IN 查询），无法用单一 struct 表达
