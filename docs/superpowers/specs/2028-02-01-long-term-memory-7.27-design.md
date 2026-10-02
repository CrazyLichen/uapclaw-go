# 7.27 LongTermMemory 设计文档

## 概述

LongTermMemory 是记忆系统的**顶层编排器**（Singleton），不实现任何存储逻辑，而是组装和协调已有的各子模块（7.1-7.10 + 7.18-7.26），对外提供统一的记忆写入、检索、删除、更新 API。

对应 Python 代码：`openjiuwen/core/memory/long_term_memory.py`

## 在 Agent 会话中的流程位置

```
用户消息 → AgentSession → Controller/ReActAgent → 思考→调用工具→观察
                                    │
                                    ├── 会话内上下文（Session State、Interaction）
                                    ├── LLM 调用（通过 Model 客户端）
                                    └── LongTermMemory ←── 7.27
                                          │
                    ┌─────────────────────┼─────────────────────┐
                    │                     │                     │
              AddMessages          SearchUserMem         GetVariables
            （每轮对话后写入）     （每轮对话前检索）      （按名获取变量）
                    │                     │                     │
                    ▼                     ▼                     ▼
              MessageManager        SearchManager          VariableManager
              FragmentMemoryManager  MemoryIndex            KVStore
              VariableManager
              SummaryManager
              WriteManager
```

- **写入时机**：Agent 每轮对话结束时，调用 `AddMessages()` 存消息并通过 LLM 提取记忆碎片
- **检索时机**：Agent 下一轮对话开始前，调用 `SearchUserMem()` 检索相关记忆注入 Prompt
- **变量读取**：通过 `GetVariables()` 读取用户变量注入 Prompt

## 前置依赖状态

| 依赖模块 | 状态 | Go 文件 |
|---------|------|--------|
| MemoryAnalyzer + Generator (7.19) | ✅ | `process/extract/analyzer.go` + `generator.go` |
| Migration Operations + Migrators (7.22-7.23) | ✅ | `migration/operation/` + `migration/migrator/` + `run_migrations.go` |
| MemoryEngineConfig | ✅ | `config/engine_config.go` |
| AgentMemoryConfig | ✅ | `config/agent_config.go` |
| MemoryScopeConfig (含 model/embedding 字段) | ✅ | `config/scope_config.go` |
| AesStorageCodec (7.24) | ✅ | `codec/aes_storage_codec.go` |
| DistributedLock (7.25) | ✅ | `common/distributed_lock.go` |
| Memory Prompts (7.26) | ✅ | `prompts/prompt_applier.go` |
| MemoryEvents | ✅ | `runner/callback/events.go` |
| CallbackFramework (Memory 域) | ✅ | `runner/callback/framework.go` |
| BaseMemoryIndex + SimpleMemoryIndex | ✅ | `foundation/store/index/` |

## 核心设计决策

| 决策 | 选择 | 理由 |
|------|------|------|
| 单例模式 | `sync.Once` 全局变量 | 用户选择，简单直接 |
| 回调集成 | ReActAgent 式包装：公开方法触发回调 → 私有 Impl 方法做逻辑 | 与项目已有模式一致（Invoke → emit_before → invokeImpl → emit_after） |
| MemoryEventData | 直接加 UserID/ScopeID/Query/MemoryType/MemoryID 字段 | 类型安全，对齐 Python kwargs |
| BaseMemoryIndex | 加 `SetEmbeddingModel` 到接口 | 替代 Python hasattr 动态检查 |
| 包位置 | `internal/agentcore/memory/ltm/` | LongTermMemory 是顶层编排器，独立包避免同层冲突 |

## 文件组织

```
internal/agentcore/memory/ltm/
├── doc.go                    # 包文档
├── models.go                 # MemInfo / MemResult / AddMemResult
├── long_term_memory.go       # LongTermMemory 结构体 + sync.Once 单例 + 公开方法签名
├── register.go               # RegisterStore / RegisterPlugin / MigrateBetweenIndices
├── config_ops.go             # SetConfig / SetScopeConfig / GetScopeConfig / DeleteScopeConfig
├── add_messages.go           # AddMessages (核心写入流程，含回调包装)
├── search_ops.go             # SearchUserMem / SearchUserHistorySummary / GetVariables
├── delete_ops.go             # DeleteMemByID / DeleteMemByUserID / DeleteMemByScope / DeleteMessages / DeleteVariables
├── update_ops.go             # UpdateMemByID / UpdateVariables
├── query_ops.go              # GetRecentMessages / GetMessageByID / GetUserMemByPage / UserMemTotalNum
├── scope_helpers.go          # getScopeLLM / getScopeConfig / applyScopeEmbedding / getScopeEmbeddingModel
├── helpers.go                # checkMessages / getHistoryMessages / validateID / runMigration
├── long_term_memory_test.go  # 单元测试
├── models_test.go            # 模型测试
├── add_messages_test.go      # AddMessages 测试
├── search_ops_test.go        # 搜索操作测试
├── delete_ops_test.go        # 删除操作测试
├── update_ops_test.go        # 更新操作测试
├── query_ops_test.go         # 查询操作测试
├── scope_helpers_test.go     # scope 辅助方法测试
└── helpers_test.go           # 辅助方法测试
```

## 数据模型

### MemInfo

```go
// MemInfo 记忆信息。
// Python: MemInfo
type MemInfo struct {
    // MemID 记忆唯一标识
    MemID string
    // Content 记忆内容
    Content string
    // Type 记忆类型
    Type mem_model.MemoryType
    // Timestamp 记忆时间戳
    Timestamp *time.Time
}
```

### MemResult

```go
// MemResult 记忆搜索结果。
// Python: MemResult
type MemResult struct {
    // MemInfo 记忆信息
    MemInfo *MemInfo
    // Score 相关度分数
    Score float64
}
```

### AddMemResult

```go
// AddMemResult 添加记忆操作结果。
// Python: AddMemResult
type AddMemResult struct {
    // Variables 变量记忆结果
    Variables []*mem_model.VariableUnit
    // UserProfile 用户画像记忆结果
    UserProfile []*mem_model.FragmentMemoryUnit
    // SemanticMemory 语义记忆结果
    SemanticMemory []*mem_model.FragmentMemoryUnit
    // EpisodicMemory 情景记忆结果
    EpisodicMemory []*mem_model.FragmentMemoryUnit
    // Summary 摘要记忆结果
    Summary []*mem_model.SummaryUnit
}
```

### AddMessagesOption / AddMessagesParams

Python 的 `add_messages` 使用关键字参数 + `DEFAULT_VALUE` 占位符模式。Go 用 Option 模式：

```go
// AddMessagesOption 添加消息的可选参数。
type AddMessagesOption func(*addMessagesParams)

type addMessagesParams struct {
    UserID                   string
    ScopeID                  string
    SessionID                string
    Timestamp                *time.Time
    GenMem                   bool
    GenMemWithHistoryMsgNum  int
}

func WithUserID(uid string) AddMessagesOption { ... }
func WithScopeID(sid string) AddMessagesOption { ... }
func WithSessionID(sid string) AddMessagesOption { ... }
func WithTimestamp(t time.Time) AddMessagesOption { ... }
func WithGenMem(gen bool) AddMessagesOption { ... }
func WithGenMemWithHistoryMsgNum(n int) AddMessagesOption { ... }
```

## LongTermMemory 结构体

```go
// LongTermMemory 长期记忆编排器（Singleton）。
// Python: LongTermMemory(metaclass=Singleton)
type LongTermMemory struct {
    // ── 配置 ──
    sysMemConfig    *config.MemoryEngineConfig
    scopeConfig     map[string]*config.MemoryScopeConfig

    // ── 存储 ──
    kvStore         kv.BaseKVStore
    vectorStore     vector.BaseVectorStore
    dbStore         db.BaseDbStore
    messageStore    db.BaseMessageStore
    memoryIndex     storeindex.BaseMemoryIndex
    storageCodec    codec.StorageCodec  // 对齐 Python AesStorageCodec

    // ── Manager ──
    scopeUserMappingMgr  *mem_model.ScopeUserMappingManager
    messageMgr           *mem_model.MessageManager
    fragmentMemoryMgr    *manage_index.FragmentMemoryManager
    variableMgr          *manage_index.VariableManager
    writeMgr             *manage_index.WriteManager
    summaryMgr           *manage_index.SummaryManager
    searchMgr            *search.SearchManager
    generator            *extract.Generator
    fragmentType         []string  // [user_profile, episodic_memory, semantic_memory]

    // ── LLM / Embedding ──
    baseLLM          *llm.Model
    baseEmbed        embedding.BaseEmbedding
    scopeEmbedding   map[string]embedding.BaseEmbedding  // scope 级 embedding 缓存
}
```

## 方法清单与回调装饰

| 公开方法 | 回调触发 | Impl 方法 | Python 对应 |
|---------|---------|----------|------------|
| `RegisterStore` | — | — | `register_store` |
| `RegisterPlugin` | — | — | `register_plugin` |
| `MigrateBetweenIndices` | — | — | `migrate_between_indices` |
| `SetConfig` | — | — | `set_config` |
| `SetScopeConfig` | — | — | `set_scope_config` |
| `GetScopeConfig` | — | — | `get_scope_config` |
| `DeleteScopeConfig` | — | — | `delete_scope_config` |
| **`AddMessages`** | `TriggerMemory(MEMORY_ADDED)` 前 | `addMessagesImpl` | `add_messages` |
| `GetRecentMessages` | — | — | `get_recent_messages` |
| `GetMessageByID` | — | — | `get_message_by_id` |
| `DeleteMessagesByUserAndScope` | — | — | `delete_messages_by_user_and_scope` |
| **`DeleteMemByID`** | `TriggerMemory(MEMORY_DELETED)` 前 | `deleteMemByIDImpl` | `delete_mem_by_id` |
| **`DeleteMemByUserID`** | `TriggerMemory(MEMORY_DELETED)` 前 | `deleteMemByUserIDImpl` | `delete_mem_by_user_id` |
| `DeleteMemByScope` | — | — | `delete_mem_by_scope` |
| **`UpdateMemByID`** | `TriggerMemory(MEMORY_UPDATED)` 前 | `updateMemByIDImpl` | `update_mem_by_id` |
| `GetVariables` | — | — | `get_variables` |
| **`SearchUserMem`** | `TriggerMemory(SEARCH_STARTED)` 前 + `trigger(SEARCH_FINISHED)` 后 | `searchUserMemImpl` | `search_user_mem` |
| **`SearchUserHistorySummary`** | `TriggerMemory(SEARCH_STARTED)` 前 + `trigger(SEARCH_FINISHED)` 后 | `searchUserHistorySummaryImpl` | `search_user_history_summary` |
| `UpdateVariables` | — | — | `update_variables` |
| `DeleteVariables` | — | — | `delete_variables` |
| `UserMemTotalNum` | — | — | `user_mem_total_num` |
| `GetUserMemByPage` | — | — | `get_user_mem_by_page` |

## 回调包装模式（对齐 ReActAgent.Invoke）

以 `AddMessages` 为例：

```go
// AddMessages 写入消息并生成长期记忆。
// 执行顺序：① emit_before(MEMORY_ADDED) → ② addMessagesImpl → ③ 返回
//
// Python: @_fw.emit_before(MemoryEvents.MEMORY_ADDED) → LongTermMemory.add_messages
func (m *LongTermMemory) AddMessages(
    ctx context.Context,
    messages []llmschema.BaseMessage,
    agentConfig *config.AgentMemoryConfig,
    opts ...AddMessagesOption,
) (*AddMemResult, error) {
    params := newAddMessagesParams(messages, agentConfig, opts...)

    // ① emit_before: 触发 MEMORY_ADDED 事件
    fw := callback.GetCallbackFramework()
    fw.TriggerMemory(ctx, &callback.MemoryEventData{
        Event:      callback.MemoryAdded,
        UserID:     params.UserID,
        ScopeID:    params.ScopeID,
        MemoryType: "all",
        Value:      messages,
    })

    // ② 真实逻辑
    return m.addMessagesImpl(ctx, params)
}
```

以 `SearchUserMem` 为例（含 after trigger）：

```go
// SearchUserMem 语义搜索用户记忆。
// 执行顺序：① emit_before(SEARCH_STARTED) → ② searchUserMemImpl → ③ trigger(SEARCH_FINISHED) → ④ 返回
//
// Python: @_fw.emit_before(MemoryEvents.MEMORY_SEARCH_STARTED) → search_user_mem
func (m *LongTermMemory) SearchUserMem(
    ctx context.Context,
    query string,
    num int,
    opts ...SearchOption,
) ([]*MemResult, error) {
    params := newSearchParams(query, num, opts...)

    // ① emit_before
    fw := callback.GetCallbackFramework()
    fw.TriggerMemory(ctx, &callback.MemoryEventData{
        Event:      callback.MemorySearchStarted,
        UserID:     params.UserID,
        ScopeID:    params.ScopeID,
        Query:      query,
        MemoryType: "user_mem",
    })

    // ② 真实逻辑
    results, err := m.searchUserMemImpl(ctx, params)

    // ③ trigger after
    if err == nil {
        callback.Trigger(ctx, callback.MemorySearchFinished,
            "scope_id", params.ScopeID,
            "user_id", params.UserID,
            "query", query,
            "result_count", len(results),
            "search_type", "user_mem",
        )
    }

    // ④ 返回
    return results, err
}
```

## AddMessages 核心流程详解

这是 LongTermMemory 最核心的方法，完整流程对齐 Python：

```
AddMessages(ctx, messages, agentConfig, opts...)
  ├── ① validateID(scopeID) — 校验 scope_id 格式
  ├── ② getScopeLLM(scopeID) — 获取 scope 级 LLM
  ├── ③ getScopeConfig(scopeID) — 获取 scope 级配置
  ├── ④ applyScopeEmbedding(scopeID) — 应用 scope 级 embedding 到 memoryIndex
  ├── ⑤ DistributedLock(kvStore, "user/{userID}") — 获取用户级分布式锁
  │     ├── ⑥ LLM 为空时返回错误
  │     ├── ⑦ getHistoryMessages(userID, scopeID, sessionID, historyWindowSize) — 获取历史消息
  │     ├── ⑧ scopeUserMappingManager.Add(userID, scopeID) — 添加 scope-user 映射
  │     ├── ⑨ timestamp 为 nil 时使用 time.Now()
  │     ├── ⑩ 遍历 messages，逐条调用 messageManager.Add() 写入消息
  │     │     └── msgID = 最后一条消息的 ID
  │     ├── ⑪ genMem=false → 返回空 AddMemResult
  │     ├── ⑫ checkMessages(messages) — 检查消息有效性 + 截断
  │     │     └── 无 human 消息 → 返回空 AddMemResult
  │     ├── ⑬ generator.GenAllMemory(ctx, params) — 编排全部记忆生成
  │     │     ├── Analyze() → VariableResult + Summary
  │     │     ├── ExtractLongTermMemory() → FragmentMemoryUnit
  │     │     └── 返回 map[string][]MemoryUnit
  │     ├── ⑭ writeManager.AddMemories(ctx, userID, scopeID, allMemory, llm) — 写入各 Manager
  │     └── ⑮ 按 MemoryType 分类填充 AddMemResult
  └── 释放分布式锁
```

## 前置改动

### 1. BaseMemoryIndex 接口加 SetEmbeddingModel

修改 `internal/agentcore/foundation/store/index/base.go`：

```go
type BaseMemoryIndex interface {
    // SetStorageCodec 设置存储编解码器。
    SetStorageCodec(codec StorageCodec)

    // SetEmbeddingModel 设置或替换嵌入模型。
    // Python: hasattr(memory_index, 'set_embedding_model') → memory_index.set_embedding_model(emb)
    SetEmbeddingModel(model embedding.BaseEmbedding)

    // ... 其余方法不变
}
```

`SimpleMemoryIndex` 已有此方法，无需改动。其他实现（如 Milvus 适配器）需加 no-op 实现。

### 2. MemoryEventData 加字段

修改 `internal/agentcore/runner/callback/events.go`：

```go
type MemoryEventData struct {
    // Event 事件类型
    Event MemoryEventType
    // UserID 用户标识
    UserID string
    // ScopeID 作用域标识
    ScopeID string
    // Query 搜索查询
    Query string
    // MemoryType 记忆类型
    MemoryType string
    // MemoryID 记忆标识
    MemoryID string
    // Score 相关度分数
    Score float64
    // Timestamp 时间戳
    Timestamp *time.Time
    // Key 记忆键（保留向后兼容）
    Key string
    // Value 记忆值
    Value any
    // Extra 额外数据
    Extra map[string]any
}
```

### 3. MemoryScopeConfig 补充序列化

在 `config/scope_config.go` 加 `ToJSON()` / `FromJSON()` 方法，供 KV 存取：

```go
func (c *MemoryScopeConfig) ToJSON() (string, error) { ... }
func MemoryScopeConfigFromJSON(data string) (*MemoryScopeConfig, error) { ... }
```

## IMPLEMENTATION_PLAN.md 状态更新

7.19、7.22、7.23 当前标记为 ☐，实际已实现，需更新为 ✅。

## 测试策略

- 所有公开方法通过 mock Store 和 mock Model 进行单元测试
- `addMessagesImpl` 测试：验证消息写入 → Generator 调用 → WriteManager 写入 → AddMemResult 构造
- `searchUserMemImpl` 测试：验证 SearchManager 调用 → MemResult 构造 → SEARCH_FINISHED 触发
- 回调包装测试：验证 TriggerMemory 在正确时机被调用
- scope 配置加解密测试：验证 API Key 写入加密、读取解密
- 分布式锁测试：验证写操作在锁保护下执行
- 目标覆盖率 ≥ 85%
