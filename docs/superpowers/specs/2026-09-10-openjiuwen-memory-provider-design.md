# 7.16 OpenJiuwenMemoryProvider 设计文档

## 概述

OpenJiuwenMemoryProvider 是 MemoryProvider 接口的本地 LTM 实现，直接使用项目自身 LongTermMemory 引擎（全局单例），不依赖外部 REST API（如 Mem0、OpenViking）。将 LTM 的 SearchUserMem / SearchUserHistorySummary / AddMessages 能力包装为 MemoryProvider 协议。

对应 Python 源码：`openjiuwen/core/memory/external/openjiuwen_memory_provider.py`（348行）

## 在 Agent 会话中的流程位置

```
Agent 会话生命周期
┌───────────────────────────────────────────────────────────┐
│ 1. 启动 → DeepAdapter.buildExternalMemoryRail()          │
│    └─ provider = NewOpenJiuwenProvider(config) ← 7.16     │
│ 2. ExternalMemoryRail.Init()                              │
│    └─ Provider.Initialize()                               │
│       └─ ltm.GetLongTermMemory().RegisterStore(...)       │
│ 3. 每轮对话前 → Rail.BeforeModelCall()                    │
│    └─ Provider.Prefetch()  ← 搜索相关记忆                │
│ 4. LLM 生成 → Provider.HandleToolCall()                   │
│    └─ ltm_search / ltm_search_summary                     │
│ 5. 每轮对话后 → Rail.AfterInvoke()                        │
│    └─ Provider.SyncTurn()  ← LTM.AddMessages()           │
│ 6. 会话结束 → Provider.Shutdown()                         │
└───────────────────────────────────────────────────────────┘
```

**作用**：OpenJiuwenMemoryProvider 是唯一不依赖外部 REST API 的 MemoryProvider 实现，适合本地/单进程部署场景。

## 依赖现状（全部已就绪）

| 依赖 | Go 实现状态 | 包路径 |
|------|------------|--------|
| MemoryProvider 接口 | ✅ | `memory/external/provider.go` |
| BaseKVStore + 实现 | ✅ | `foundation/store/kv/` |
| BaseVectorStore + 实现 | ✅ | `foundation/store/vector/` |
| BaseDbStore + 实现 | ✅ | `foundation/store/db/` |
| BaseEmbedding + 实现 | ✅ | `retrieval/embedding/` |
| MemoryEngineConfig | ✅ | `memory/config/engine_config.go` |
| MemoryScopeConfig | ✅ | `memory/config/scope_config.go` |
| AgentMemoryConfig | ✅ | `memory/config/agent_config.go` |
| LongTermMemory | ✅ | `memory/ltm/` |
| Generator (extract) | ✅ | `memory/process/extract/` |
| ExternalMemoryRail | ✅ | `harness/rails/memory/` |

## 实现文件清单

| 文件 | 说明 |
|------|------|
| `external/openjiuwen_provider.go` | Provider 完整实现 |
| `external/openjiuwen_provider_test.go` | 单元测试 |
| `external/doc.go` | 更新文件目录 |
| `deep_adapter_rails.go` | 回填 `"openjiuwen"` 分支 + `buildOpenJiuwenProviderConfig` |

## 设计决策

### 1. 构造模式：对齐 Python 双模式

- **config dict 模式**：`NewOpenJiuwenProvider(config)` 传入 `map[string]any`，Initialize 中根据 config 自动创建缺失的 store/embedding
- **直接注入模式**：`NewOpenJiuwenProvider(config, WithKVStore(kv), WithVectorStore(vec), ...)` 直接传入已构造好的实例
- config dict 和直接注入可以混合使用：注入优先，缺失的由 config 创建

### 2. LTM 实例策略：直接使用全局单例

- Provider 内部调 `ltm.GetLongTermMemory()` 获取全局单例
- Initialize 中 RegisterStore 到全局单例
- 对齐 Python `LongTermMemory()` Singleton metaclass 行为
- 测试时用 `ltm.ResetLongTermMemory()` 重置

### 3. config["kv"] backend 名称映射

| Python backend | Go 实现 |
|---------------|---------|
| `"memory"` | `kv.NewInMemoryKVStore()` |
| `"sqlite"` | `kv.NewDbBasedKVStore(engine)` |
| `"shelve"` | `kv.NewFileKVStore(path)` |

## 结构体设计

```go
type OpenJiuwenProvider struct {
    BaseMemoryProvider
    config            map[string]any
    kvStore           kv.BaseKVStore
    vectorStore       vector.BaseVectorStore
    dbStore           db.BaseDbStore
    embeddingModel    embedding.BaseEmbedding
    engineConfig      *config.MemoryEngineConfig
    scopeConfig       *config.MemoryScopeConfig
    agentMemoryConfig *config.AgentMemoryConfig
    userID            string   // 默认 "__default__"
    scopeID           string   // 默认 "__default__"
    sessionID         string   // 默认 "__default__"
    initialized       bool
}
```

### 常量

```go
const (
    defaultRecallUserMemNum    = 5
    defaultRecallHistoryMemNum = 3
    defaultKVBackend     = "memory"
    defaultVectorBackend = "chroma"
    defaultDBBackend     = "sqlite"
)
```

### 全局变量

```go
var (
    ltmSearchSchema        ToolSchema  // 对齐 Python LTM_SEARCH_SCHEMA
    ltmSearchSummarySchema ToolSchema  // 对齐 Python LTM_SEARCH_SUMMARY_SCHEMA
    ojLogComponent         = logger.ComponentAgentCore
)
```

## 方法实现详细设计

### NewOpenJiuwenProvider

```go
func NewOpenJiuwenProvider(config map[string]any, opts ...OpenJiuwenProviderOption) *OpenJiuwenProvider
```

- config 为 nil 时默认空 map
- 应用 functional options（WithKVStore / WithVectorStore / WithDbStore / WithEmbeddingModel / WithEngineConfig / WithScopeConfig / WithAgentMemoryConfig）
- userID/scopeID/sessionID 默认 `"__default__"`

对齐 Python `__init__`（L61-85）。

### Name / IsAvailable / IsInitialized

- `Name()` → `"openjiuwen"`
- `IsAvailable()` → 三个 store 都不为 nil OR `config["embedding"]["model_name"]` 存在
- `IsInitialized()` → `p.initialized`

对齐 Python L87-98。

### Initialize

```go
func (p *OpenJiuwenProvider) Initialize(ctx context.Context, opts ...ProviderOption) error
```

完整流程对齐 Python L100-131：

1. 从 opts 解析 user_id / scope_id / session_id
2. kvStore 为 nil → `createKVStore()`
3. vectorStore 为 nil → `createVectorStore()`
4. dbStore 为 nil → `createDBStore(ctx)`
5. embeddingModel 为 nil → `createEmbedding()`
6. 任一 store 为 nil → error 日志 + return（对齐 Python L114-116）
7. 获取 LTM 全局单例 `ltm.GetLongTermMemory()`
8. LTM.KVStore 为 nil → `RegisterStore(ctx, kvStore, WithVectorStore, WithDbStore, WithEmbeddingModel)`
9. scopeConfig 不为 nil → `LTM.SetScopeConfig(ctx, scopeID, scopeConfig)`
10. `p.initialized = true`

### SystemPromptBlock

**提示词直接从 Python 源码复制**，对齐 Python L133-148：

```
# Long-Term Memory System

You have long-term memory capabilities and can remember user information across sessions. The system automatically extracts valuable information from conversations and stores it in memory.

## Memory Search

When you need to recall previous information, use the `ltm_search` tool to search long-term memory.
- Search queries should contain key information (names, dates, event keywords)
- If results are insufficient, try searching again with different keywords

## Automatic Memory

The system automatically extracts from each conversation:
- User profile (identity, preferences, habits)
- Episodic memory (specific events, decisions)
- Semantic memory (background knowledge, technical details)
- Conversation summaries (key conclusions, main points)
```

### GetToolSchemas

返回两个 ToolSchema：

1. `ltm_search`：query(必填) + num(默认5) + threshold(默认0.3)
2. `ltm_search_summary`：query(必填) + num(默认3)

对齐 Python L26-51。

### HandleToolCall

```go
func (p *OpenJiuwenProvider) HandleToolCall(ctx context.Context, toolName string, args map[string]any) (string, error)
```

分发逻辑对齐 Python L153-164：
- 未初始化 → `{"error": "Memory provider not initialized"}`
- `"ltm_search"` → `handleSearch`
- `"ltm_search_summary"` → `handleSearchSummary`
- 其他 → `{"error": "Unknown tool: xxx"}`
- 异常 → `{"error": "xxx", "results": []}`

### handleSearch

对齐 Python L302-324：

1. 从 args 提取 query / num / threshold
2. 调 `ltm.SearchUserMem(ctx, query, num, SearchWithUserID, SearchWithScopeID, SearchWithThreshold)`
3. 将 `[]*ltm.MemResult` 序列化为 `{"results": [...], "count": N}`

### handleSearchSummary

对齐 Python L326-347：

1. 从 args 提取 query / num
2. 调 `ltm.SearchUserHistorySummary(ctx, query, num, SearchWithUserID, SearchWithScopeID)`
3. 将结果序列化为 `{"results": [...], "count": N}`

### Prefetch

对齐 Python L166-201：

1. 未初始化或 ltm 为 nil → 返回 ""
2. search_user_mem（num=5, threshold=0.3）→ 格式化为 `"## Related Memories\n- [type] content (score: 0.xx)"`
3. search_user_history_summary（num=3, threshold=0.3）→ 格式化为 `"## Related History Summaries\n- content (score: 0.xx)"`
4. 两段各自 try-catch，失败 warn 日志但不阻断
5. 无结果返回 ""

### SyncTurn

对齐 Python L203-225：

1. 未初始化或 ltm 为 nil → return nil
2. 构造 `[]llmschema.BaseMessage`（userMsg → UserMessage, assistantMsg → AssistantMessage）
3. 空消息列表 → return nil
4. 调 `ltm.AddMessages(ctx, messages, p.agentMemoryConfig, ltm.WithUserID, ltm.WithScopeID, ltm.WithSessionID)`
5. 异常 → warn 日志，不返回 error（对齐 Python 只 warn 不抛）

### Shutdown

对齐 Python L227-228：设 `p.initialized = false`

### 4 个 create* 工厂方法

#### createKVStore

对齐 Python L240-258：

| config backend | Go 实现 |
|---------------|---------|
| `"memory"` (默认) | `kv.NewInMemoryKVStore()` |
| `"sqlite"` | 需要 GORM engine → `kv.NewDbBasedKVStore(engine)` |
| `"shelve"` | `kv.NewFileKVStore(path)` — Go 用 FileKVStore 对应 Python ShelveStore |

- 失败 → error 日志 + return nil

#### createVectorStore

对齐 Python L260-270：

- 使用 `vector.NewChromaVectorStore(...)` 或其他已注册后端
- Python 使用 `create_vector_store(backend, **kwargs)` 工厂函数
- Go 侧目前无统一工厂函数，直接按 backend 名分发创建

#### createDBStore

对齐 Python L272-284：

- backend `"sqlite"` (默认) → `db.NewDefaultDbStore(path)`
- 失败 → error 日志 + return nil

#### createEmbedding

对齐 Python L286-300：

- config["embedding"]["model_name"] 为空 → return nil
- 构造 `EmbeddingConfig{ModelName, BaseURL, APIKey}` → `embedding.NewAPIEmbedding(config)`
- 失败 → error 日志 + return nil

### parseScopeConfig

对齐 Python L230-238：

- 从 `config["scope_config"]` 解析为 `*config.MemoryScopeConfig`
- 失败 → warn 日志 + return nil

## deep_adapter_rails.go 回填

### 1. buildExternalMemoryProvider 添加 openjiuwen 分支

在 switch 中添加：

```go
case "openjiuwen":
    providerConfig := buildOpenJiuwenProviderConfig(cfg, d.configCache)
    provider := ext.NewOpenJiuwenProvider(providerConfig)
    if !provider.IsAvailable() {
        logger.Warn(logComponent).Msg("buildExternalMemoryProvider: OpenJiuwenProvider 不可用")
        return nil
    }
    return provider
```

### 2. 新增 buildOpenJiuwenProviderConfig

对齐 Python `build_openjiuwen_provider_config`（`external_memory_config.py` L87-126）：

- 从 `cfg["openjiuwen"]` 读取子配置
- 默认 LTM 目录：`{workspace_dir}/memory/ltm`
- KV：`kv_type`（默认 `"shelve"`）→ `"shelve"` 对应 Go FileKVStore；`kv_path`（默认 `{ltm_dir}/kv`）
- Vector：`vector_type`（默认 `"chroma"`）；`vector_persist_dir`（默认 `{ltm_dir}/chroma`）
- DB：`db_type`（默认 `"sqlite"`）；`db_path`（默认 `{ltm_dir}/ltm.db`）
- Embedding：从顶层 `embed` 配置获取 `embed_model` / `embed_api_base` / `embed_api_key`，环境变量 fallback

## 日志同步要点

对照 Python 源码中的 logger 调用：

| Python 行 | 级别 | 内容 | Go 对应 |
|-----------|------|------|---------|
| L115 | error | Store creation failed | `logger.Error(ojLogComponent).Msg(...)` |
| L186 | warning | prefetch search_user_mem failed | `logger.Warn(ojLogComponent).Err(err).Msg(...)` |
| L200 | warning | prefetch search_user_history_summary failed | `logger.Warn(ojLogComponent).Err(err).Msg(...)` |
| L225 | warning | sync_turn add_messages failed | `logger.Warn(ojLogComponent).Err(err).Msg(...)` |
| L237 | warning | Failed to parse scope_config | `logger.Warn(ojLogComponent).Err(err).Msg(...)` |
| L257 | error | KV store creation failed | `logger.Error(ojLogComponent).Str("backend", backend).Err(err).Msg(...)` |
| L269 | error | Vector store creation failed | `logger.Error(ojLogComponent).Str("backend", backend).Err(err).Msg(...)` |
| L283 | error | DB store creation failed | `logger.Error(ojLogComponent).Str("backend", backend).Err(err).Msg(...)` |
| L299 | error | Embedding creation failed | `logger.Error(ojLogComponent).Err(err).Msg(...)` |

## 测试策略

- 使用 mock LTM（通过 `ltm.ResetLongTermMemory()` + 测试用 LTM 实例）
- 构造函数双模式测试
- Initialize 正常/失败路径
- HandleToolCall 两个工具 + 未知工具 + 未初始化
- Prefetch 正常/空结果/搜索失败
- SyncTurn 正常/空消息/LTM 失败
- SystemPromptBlock 内容验证
- GetToolSchemas 返回验证
- 工厂方法创建各 backend 类型

覆盖率目标 ≥ 85%。
