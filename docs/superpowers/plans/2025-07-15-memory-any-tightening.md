# Memory any 类型收紧 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 收紧 memory 包中 2 处 `map[string]any` 为 typed struct，消除类型断言风险

**Architecture:** 分两个独立修复项实施：(1) MemorySettings 子配置 struct 化 + (2) UserMemStore CRUD 返回 struct 化。每个修复项先改类型定义，再适配消费方，最后更新测试。

**Tech Stack:** Go 1.x, zerolog 结构化日志, GORM, KV 存储抽象

**设计文档:** `docs/superpowers/specs/2025-07-15-memory-any-tightening-design.md`

---

## 文件变更映射

### 修复项 1：MemorySettings typed struct

| 文件 | 操作 | 职责 |
|------|------|------|
| `lite/config.go` | 修改 | 新增 8 个子配置 struct + 修改 MemorySettings 字段类型 + 重写 CreateMemorySettings |
| `lite/config_test.go` | 修改 | 适配 struct 字段访问 |
| `lite/manager_impl.go` | 修改 | 约 18 处 map 访问改为 struct 字段 |
| `lite/doc.go` | 修改 | 更新包文档 |

### 修复项 2：UserMemStore typed struct

| 文件 | 操作 | 职责 |
|------|------|------|
| `manage/mem_model/user_mem_store.go` | 修改 | 新增 UserMemoryRecord struct + 修改方法签名 |
| `manage/mem_model/user_mem_store_test.go` | 修改 | 适配 Write/Get/BatchGet 等调用方式 |
| `manage/mem_model/doc.go` | 修改 | 更新包文档 |

---

### Task 1: 新增 MemorySettings 子配置 struct

**Files:**
- Modify: `internal/agentcore/memory/lite/config.go`

- [ ] **Step 1: 在 config.go 结构体区块新增 8 个子配置 struct**

在 `MemorySettings` 结构体定义之前插入：

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

- [ ] **Step 2: 修改 MemorySettings 字段类型**

将 `MemorySettings` 中 5 个 `map[string]any` 字段替换：

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

- [ ] **Step 3: 重写 CreateMemorySettings 默认值构造**

将默认值从 map 构造改为 struct 字段赋值：

```go
s := &MemorySettings{
	Provider:   "openai_compatible",
	Model:      "text-embedding-v3",
	Fallback:   "mock",
	Sources:    []string{"memory", "sessions"},
	ExtraPaths: nil,
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
		Path:   "memory.db",
		Vector: VectorStoreConfig{Enabled: true},
		Fts:    FtsConfig{Enabled: true},
	},
	Sync: SyncConfig{
		Watch:           true,
		WatchDebounceMs: 2000,
		OnSearch:        true,
		OnSessionStart:  true,
		IntervalMinutes: 0,
	},
	Cache: CacheConfig{
		Enabled:    true,
		MaxEntries: 10000,
	},
}
```

- [ ] **Step 4: 重写 CreateMemorySettings overrides 合并逻辑**

将 switch 中 5 个子配置的 `map[string]any` 整体替换改为逐字段解析：

```go
	case "chunking":
		if v, ok := value.(map[string]any); ok {
			if v2, ok := v["tokens"].(float64); ok {
				s.Chunking.Tokens = v2
			} else if v2, ok := v["tokens"].(int); ok {
				s.Chunking.Tokens = float64(v2)
			}
			if v2, ok := v["overlap"].(float64); ok {
				s.Chunking.Overlap = v2
			} else if v2, ok := v["overlap"].(int); ok {
				s.Chunking.Overlap = float64(v2)
			}
		}
	case "query":
		if v, ok := value.(map[string]any); ok {
			if v2, ok := v["max_results"].(float64); ok {
				s.Query.MaxResults = v2
			} else if v2, ok := v["max_results"].(int); ok {
				s.Query.MaxResults = float64(v2)
			}
			if v2, ok := v["min_score"].(float64); ok {
				s.Query.MinScore = v2
			}
			if hybrid, ok := v["hybrid"].(map[string]any); ok {
				if v2, ok := hybrid["enabled"].(bool); ok {
					s.Query.Hybrid.Enabled = v2
				}
				if v2, ok := hybrid["vectorWeight"].(float64); ok {
					s.Query.Hybrid.VectorWeight = v2
				}
				if v2, ok := hybrid["textWeight"].(float64); ok {
					s.Query.Hybrid.TextWeight = v2
				}
				if v2, ok := hybrid["candidateMultiplier"].(float64); ok {
					s.Query.Hybrid.CandidateMultiplier = v2
				}
			}
		}
	case "store":
		if v, ok := value.(map[string]any); ok {
			if v2, ok := v["path"].(string); ok {
				s.Store.Path = v2
			}
			if vec, ok := v["vector"].(map[string]any); ok {
				if v2, ok := vec["enabled"].(bool); ok {
					s.Store.Vector.Enabled = v2
				}
			}
			if fts, ok := v["fts"].(map[string]any); ok {
				if v2, ok := fts["enabled"].(bool); ok {
					s.Store.Fts.Enabled = v2
				}
			}
		}
	case "sync":
		if v, ok := value.(map[string]any); ok {
			if v2, ok := v["watch"].(bool); ok {
				s.Sync.Watch = v2
			}
			if v2, ok := v["watchDebounceMs"].(int); ok {
				s.Sync.WatchDebounceMs = v2
			}
			if v2, ok := v["onSearch"].(bool); ok {
				s.Sync.OnSearch = v2
			}
			if v2, ok := v["onSessionStart"].(bool); ok {
				s.Sync.OnSessionStart = v2
			}
			if v2, ok := v["intervalMinutes"].(int); ok {
				s.Sync.IntervalMinutes = v2
			}
		}
	case "cache":
		if v, ok := value.(map[string]any); ok {
			if v2, ok := v["enabled"].(bool); ok {
				s.Cache.Enabled = v2
			}
			if v2, ok := v["maxEntries"].(float64); ok {
				s.Cache.MaxEntries = v2
			} else if v2, ok := v["maxEntries"].(int); ok {
				s.Cache.MaxEntries = float64(v2)
			}
		}
```

- [ ] **Step 5: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)'; go build ./internal/agentcore/memory/lite/... 2>&1 | head -30`
Expected: 编译错误，manager_impl.go 中对 map 的索引访问报错（类型已改为 struct）

---

### Task 2: 适配 manager_impl.go 消费方

**Files:**
- Modify: `internal/agentcore/memory/lite/manager_impl.go`

- [ ] **Step 1: 替换 Sync 字段访问（3 处）**

第 158 行：
```go
// 修改前
if syncWatch, _ := m.settings.Sync["watch"].(bool); syncWatch {
// 修改后
if m.settings.Sync.Watch {
```

第 211 行：
```go
// 修改前
if v, ok := m.settings.Sync["onSearch"].(bool); ok {
	onSearch = v
}
// 修改后
onSearch = m.settings.Sync.OnSearch
```

第 1458 行：
```go
// 修改前
if v, ok := m.settings.Sync["watchDebounceMs"].(int); ok {
	debounceMs = v
}
// 修改后
debounceMs = m.settings.Sync.WatchDebounceMs
```

第 1478 行：
```go
// 修改前
if v, ok := m.settings.Sync["intervalMinutes"].(int); ok {
	minutes = v
}
// 修改后
minutes = m.settings.Sync.IntervalMinutes
```

- [ ] **Step 2: 替换 Query 字段访问（3 处）**

第 228 行：
```go
// 修改前
} else if v, ok := m.settings.Query["min_score"].(float64); ok {
	minScore = v
}
// 修改后
} else {
	minScore = m.settings.Query.MinScore
}
```

第 235 行：
```go
// 修改前
} else if v, ok := m.settings.Query["max_results"].(float64); ok {
	maxResults = int(v)
}
// 修改后
} else {
	maxResults = int(m.settings.Query.MaxResults)
}
```

第 240 行及后续：
```go
// 修改前
hybrid := make(map[string]any)
if v, ok := m.settings.Query["hybrid"].(map[string]any); ok {
	hybrid = v
}
candidateMultiplier := 2.0
if v, ok := hybrid["candidateMultiplier"].(float64); ok {
	candidateMultiplier = v
}
...
if v, ok := hybrid["enabled"].(bool); ok {
	hybridEnabled = v
}

// 修改后
candidateMultiplier := m.settings.Query.Hybrid.CandidateMultiplier
if candidateMultiplier == 0 {
	candidateMultiplier = 2.0
}
...
hybridEnabled := m.settings.Query.Hybrid.Enabled
```

注意：删除 `hybrid` 局部变量，所有引用改为 `m.settings.Query.Hybrid.xxx`。搜索逻辑中 opt 级别的 hybrid 覆盖需保留：

```go
// 搜索方法中 opt 级别的 hybrid 覆盖（opts["hybrid"] 来自调用方）
if v, ok := opts["hybrid"].(map[string]any); ok {
	if v2, ok := v["candidateMultiplier"].(float64); ok {
		candidateMultiplier = v2
	}
	if v2, ok := v["enabled"].(bool); ok {
		hybridEnabled = v2
	}
}
```

- [ ] **Step 3: 替换 Store 字段访问（5 处）**

第 429 行（Status 方法）：
```go
// 修改前
if v, ok := m.settings.Store["fts"].(map[string]any); ok {
	if v2, ok := v["enabled"].(bool); ok {
		ftsEnabled = v2
	}
}
// 修改后
ftsEnabled = m.settings.Store.Fts.Enabled
```

第 435 行：
```go
// 修改前
if v, ok := m.settings.Store["vector"].(map[string]any); ok {
	if v2, ok := v["enabled"].(bool); ok {
		vecEnabled = v2
	}
}
// 修改后
vecEnabled = m.settings.Store.Vector.Enabled
```

第 625 行（resolveDBPath）：
```go
// 修改前
if v, ok := m.settings.Store["path"].(string); ok && v != "" {
	storePath = v
}
// 修改后
if m.settings.Store.Path != "" {
	storePath = m.settings.Store.Path
}
```

第 665 行（ensureSchema）：
```go
// 修改前
if v, ok := m.settings.Store["fts"].(map[string]any); ok {
	if v2, ok := v["enabled"].(bool); ok {
		ftsEnabled = v2
	}
}
// 修改后
ftsEnabled = m.settings.Store.Fts.Enabled
```

第 707 行（loadVectorExtension）：
```go
// 修改前
if v, ok := m.settings.Store["vector"].(map[string]any); ok {
	if v2, ok := v["enabled"].(bool); ok {
		vecEnabled = v2
	}
}
// 修改后
vecEnabled = m.settings.Store.Vector.Enabled
```

- [ ] **Step 4: 替换 Chunking 字段访问（4 处）**

第 761 行（shouldFullReindex）：
```go
// 修改前
if meta["chunkTokens"] != m.settings.Chunking["tokens"] {
// 修改后
if meta["chunkTokens"] != m.settings.Chunking.Tokens {
```

第 784-785 行（runReindex）：
```go
// 修改前
"chunkTokens":  m.settings.Chunking["tokens"],
"chunkOverlap": m.settings.Chunking["overlap"],
// 修改后
"chunkTokens":  m.settings.Chunking.Tokens,
"chunkOverlap": m.settings.Chunking.Overlap,
```

第 924-925 行（indexMemoryEntry）：
```go
// 修改前
chunkTokens, _ := m.settings.Chunking["tokens"].(int)
chunkOverlap, _ := m.settings.Chunking["overlap"].(int)
// 修改后
chunkTokens := int(m.settings.Chunking.Tokens)
chunkOverlap := int(m.settings.Chunking.Overlap)
```

- [ ] **Step 5: 替换 Cache 字段访问（2 处）**

第 441 行（Status 方法）：
```go
// 修改前
if v, ok := m.settings.Cache["enabled"].(bool); ok {
	cacheEnabled = v
}
// 修改后
cacheEnabled = m.settings.Cache.Enabled
```

第 1075 行（embedText）：
```go
// 修改前
if v, ok := m.settings.Cache["enabled"].(bool); ok {
	cacheEnabled = v
}
// 修改后
cacheEnabled = m.settings.Cache.Enabled
```

- [ ] **Step 6: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/memory/lite/...`
Expected: 编译通过

---

### Task 3: 适配 config_test.go

**Files:**
- Modify: `internal/agentcore/memory/lite/config_test.go`

- [ ] **Step 1: 替换 TestCreateMemorySettings_分块配置 中的 map 访问**

```go
// 修改前
func TestCreateMemorySettings_分块配置(t *testing.T) {
	s := CreateMemorySettings("/tmp", nil)
	tokens, _ := s.Chunking["tokens"].(int)
	if tokens != 256 {
		t.Errorf("Chunking.tokens 应为 256，实际为 %v", s.Chunking["tokens"])
	}
	overlap, _ := s.Chunking["overlap"].(int)
	if overlap != 32 {
		t.Errorf("Chunking.overlap 应为 32，实际为 %v", s.Chunking["overlap"])
	}
}

// 修改后
func TestCreateMemorySettings_分块配置(t *testing.T) {
	s := CreateMemorySettings("/tmp", nil)
	if s.Chunking.Tokens != 256 {
		t.Errorf("Chunking.Tokens 应为 256，实际为 %v", s.Chunking.Tokens)
	}
	if s.Chunking.Overlap != 32 {
		t.Errorf("Chunking.Overlap 应为 32，实际为 %v", s.Chunking.Overlap)
	}
}
```

- [ ] **Step 2: 替换 TestCreateMemorySettings_混合搜索配置 中的 map 访问**

```go
// 修改前
func TestCreateMemorySettings_混合搜索配置(t *testing.T) {
	s := CreateMemorySettings("/tmp", nil)
	hybrid, _ := s.Query["hybrid"].(map[string]any)
	if hybrid == nil {
		t.Fatal("Query.hybrid 应为非 nil")
	}
	if hybrid["enabled"] != true {
		t.Error("hybrid.enabled 应为 true")
	}
	if hybrid["vectorWeight"] != 0.7 {
		t.Errorf("hybrid.vectorWeight 应为 0.7，实际为 %v", hybrid["vectorWeight"])
	}
	if hybrid["textWeight"] != 0.3 {
		t.Errorf("hybrid.textWeight 应为 0.3，实际为 %v", hybrid["textWeight"])
	}
}

// 修改后
func TestCreateMemorySettings_混合搜索配置(t *testing.T) {
	s := CreateMemorySettings("/tmp", nil)
	if !s.Query.Hybrid.Enabled {
		t.Error("Query.Hybrid.Enabled 应为 true")
	}
	if s.Query.Hybrid.VectorWeight != 0.7 {
		t.Errorf("Query.Hybrid.VectorWeight 应为 0.7，实际为 %v", s.Query.Hybrid.VectorWeight)
	}
	if s.Query.Hybrid.TextWeight != 0.3 {
		t.Errorf("Query.Hybrid.TextWeight 应为 0.3，实际为 %v", s.Query.Hybrid.TextWeight)
	}
}
```

- [ ] **Step 3: 运行测试验证**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/memory/lite/... -count=1 2>&1 | tail -20`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/memory/lite/
git commit -m "refactor(memory): MemorySettings 子配置 map[string]any 替换为 typed struct

- 新增 ChunkingConfig/QueryConfig/HybridConfig/StoreConfig/VectorStoreConfig/FtsConfig/SyncConfig/CacheConfig
- MemorySettings 5 个 map[string]any 字段改为 typed struct
- 数值字段统一 float64，消除 int/float64 类型断言风险
- CreateMemorySettings overrides 保持 map[string]any 外部接口，内部逐字段解析
- manager_impl.go 约 18 处 map 索引访问改为 struct 字段访问
- 消除所有 .(bool)/.(float64)/.(string)/.(map[string]any) 类型断言"
```

---

### Task 4: 新增 UserMemoryRecord struct + 修改 Write 方法

**Files:**
- Modify: `internal/agentcore/memory/manage/mem_model/user_mem_store.go`

- [ ] **Step 1: 在 user_mem_store.go 结构体区块新增 UserMemoryRecord**

在 `UserMemStore` 结构体之前插入：

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

- [ ] **Step 2: 修改 Write 方法签名和实现**

```go
// 修改前
func (s *UserMemStore) Write(ctx context.Context, userID, scopeID, memID string, data map[string]any) (bool, error) {
	if len(data) == 0 {
		...
		return false, nil
	}
	...
	jsonData, err := json.Marshal(data)
	...
	if memType, ok := data[memTypeFieldKey]; ok {
		memTypeStr, ok := memType.(string)
		if !ok {
			...
			return false, fmt.Errorf("mem_type 字段不是字符串类型，实际类型: %T", memType)
		}
		...
	}
}

// 修改后
func (s *UserMemStore) Write(ctx context.Context, userID, scopeID, memID string, data *UserMemoryRecord) (bool, error) {
	if data == nil {
		logger.Error(logComponent).
			Str("memory_id", memID).
			Str("event_type", "MEMORY_STORE").
			Str("user_id", userID).
			Str("scope_id", scopeID).
			Msg("Write failed, because data is nil")
		return false, nil
	}
	...
	jsonData, err := json.Marshal(data)
	...
	if data.MemType != "" {
		memTypeStr := data.MemType
		...
	}
}
```

注意：`len(data) == 0` 改为 `data == nil`（struct 无法用 len 判空）。

- [ ] **Step 3: 修改 get 内部方法返回类型**

```go
// 修改前
func (s *UserMemStore) get(ctx context.Context, memKey string) (map[string]any, error) {
	...
	var result map[string]any
	if err := json.Unmarshal(memValue, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// 修改后
func (s *UserMemStore) get(ctx context.Context, memKey string) (*UserMemoryRecord, error) {
	...
	var result UserMemoryRecord
	if err := json.Unmarshal(memValue, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
```

- [ ] **Step 4: 修改 Get/BatchGet/GetAll/GetByTopic/GetInRange 方法签名和实现**

```go
// Get
func (s *UserMemStore) Get(ctx context.Context, userID, scopeID, memID string) (*UserMemoryRecord, error) {

// BatchGet
func (s *UserMemStore) BatchGet(ctx context.Context, userID, scopeID string, memIDs []string) ([]*UserMemoryRecord, error) {
	...
	result := make([]*UserMemoryRecord, 0, len(valueList))
	for _, value := range valueList {
		if value == nil {
			continue
		}
		var m UserMemoryRecord
		if err := json.Unmarshal(value, &m); err != nil {
			continue
		}
		result = append(result, &m)
	}
	return result, nil
}

// GetAll
func (s *UserMemStore) GetAll(ctx context.Context, userID, scopeID, memType string) ([]*UserMemoryRecord, error) {

// GetByTopic
func (s *UserMemStore) GetByTopic(ctx context.Context, userID, scopeID, topic string) ([]*UserMemoryRecord, error) {

// GetInRange
func (s *UserMemStore) GetInRange(ctx context.Context, userID, scopeID string, startIdx, endIdx int, memType string) ([]*UserMemoryRecord, error) {
```

- [ ] **Step 5: 修改 innerDelete 中的 map 反序列化**

```go
// 修改前
var dictValue map[string]any
if err := json.Unmarshal(data, &dictValue); err == nil {
	if memType, ok := dictValue[memTypeFieldKey]; ok {
		memTypeStr, ok := memType.(string)
		if !ok {
			...
		}
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

- [ ] **Step 6: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/memory/manage/mem_model/...`
Expected: 编译错误，测试文件中 map 调用不匹配

---

### Task 5: 适配 user_mem_store_test.go

**Files:**
- Modify: `internal/agentcore/memory/manage/mem_model/user_mem_store_test.go`

- [ ] **Step 1: 替换所有 Write 调用中的 map 为 UserMemoryRecord**

```go
// 修改前
data := map[string]any{"mem_type": "user_profile", "content": "test"}
ok, err := store.Write(ctx, "user1", "scope1", testID1, data)

// 修改后
data := &UserMemoryRecord{MemType: "user_profile", Content: "test"}
ok, err := store.Write(ctx, "user1", "scope1", testID1, data)
```

逐个替换所有 Write 调用（约 15 处），模式统一。

无 mem_type 的 Write 调用：
```go
// 修改前
_, _ = store.Write(ctx, "user1", "scope1", testID1, map[string]any{"content": "test1"})
// 修改后
_, _ = store.Write(ctx, "user1", "scope1", testID1, &UserMemoryRecord{Content: "test1"})
```

- [ ] **Step 2: 替换所有 Get 返回值断言**

```go
// 修改前
result, _ := store.Get(ctx, "user1", "scope1", testID1)
if result["mem_type"] != "user_profile" { ... }
if result["content"] != "test" { ... }

// 修改后
result, _ := store.Get(ctx, "user1", "scope1", testID1)
if result.MemType != "user_profile" { ... }
if result.Content != "test" { ... }
```

- [ ] **Step 3: 替换 GetAll/GetInRange 返回值断言**

```go
// 修改前
result, err := store.GetAll(ctx, "user1", "scope1", "user_profile")
if len(result) != 1 { ... }

// 修改后 — 长度断言不变，内容断言改为 struct 字段
result, err := store.GetAll(ctx, "user1", "scope1", "user_profile")
if len(result) != 1 { ... }
```

- [ ] **Step 4: 替换 BatchGet 返回值断言**

```go
// 修改前
result, err := store.BatchGet(ctx, "user1", "scope1", []string{testID1, testID2})
if len(result) != 2 { ... }

// 修改后 — 同上，长度断言不变
```

- [ ] **Step 5: 替换空数据 Write 测试**

```go
// 修改前
ok, err := store.Write(ctx, "user1", "scope1", testID1, map[string]any{})

// 修改后
ok, err := store.Write(ctx, "user1", "scope1", testID1, &UserMemoryRecord{})
```

注意：`&UserMemoryRecord{}` 是零值 struct（MemType=""、Content=""），与之前的 `map[string]any{}` 空字典语义不同——空 dict 触发 `len(data) == 0` 返回 false，零值 struct 不触发 `data == nil`。需确认：是否应该保留"空内容返回 false"的逻辑？

选项 A：保留业务语义，增加 Content 非空检查：
```go
if data == nil || data.Content == "" {
	// ... 空数据日志
	return false, nil
}
```

选项 B：仅检查 nil（与设计文档中 Update 保持 map 的决策一致，Write 允许零值 struct）。

**推荐选项 A**，对齐 Python `if not data: return False` 语义。

- [ ] **Step 6: 运行测试验证**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/memory/manage/mem_model/... -count=1 -v 2>&1 | tail -30`
Expected: PASS

- [ ] **Step 7: 提交**

```bash
git add internal/agentcore/memory/manage/mem_model/
git commit -m "refactor(memory): UserMemStore CRUD 返回 typed struct 替代 map[string]any

- 新增 UserMemoryRecord struct（MemType/Content/Topic/Timestamp）
- Write/Get/BatchGet/GetAll/GetByTopic/GetInRange 改为 typed struct
- Update 保持 map[string]any 参数（字段级合并语义）
- innerDelete 反序列化改为 UserMemoryRecord
- 消除所有 .(string) 类型断言"
```

---

### Task 6: 更新 doc.go 包文档

**Files:**
- Modify: `internal/agentcore/memory/lite/doc.go`
- Modify: `internal/agentcore/memory/manage/mem_model/doc.go`

- [ ] **Step 1: 更新 lite/doc.go**

在包文档中补充新增的子配置 struct 说明，更新核心类型索引。

- [ ] **Step 2: 更新 mem_model/doc.go**

在包文档中补充 UserMemoryRecord 说明，更新核心类型索引。

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/memory/lite/doc.go internal/agentcore/memory/manage/mem_model/doc.go
git commit -m "docs(memory): 更新包文档反映 typed struct 变更"
```

---

### Task 7: 全量编译 + 测试验证

**Files:** 无代码变更

- [ ] **Step 1: 检查残留 go 进程**

Run: `pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' || echo "无残留进程"`

- [ ] **Step 2: 全量编译**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./...`
Expected: 编译通过

- [ ] **Step 3: 运行 memory 包测试**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/memory/... -count=1 2>&1 | tail -30`
Expected: PASS

- [ ] **Step 4: 检查是否有其他包引用 MemorySettings 子配置 map 字段**

Run: `cd /home/opensource/uapclaw-gateway && grep -rn '\.Chunking\[' internal/ --include='*.go' && grep -rn '\.Query\[' internal/ --include='*.go' && grep -rn '\.Store\[' internal/ --include='*.go' && grep -rn '\.Sync\[' internal/ --include='*.go' && grep -rn '\.Cache\[' internal/ --include='*.go'`
Expected: 无结果（所有 map 索引访问已替换为 struct 字段）

- [ ] **Step 5: 推送**

```bash
git push
```
