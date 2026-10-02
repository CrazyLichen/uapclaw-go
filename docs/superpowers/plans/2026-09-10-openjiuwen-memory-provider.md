# 7.16 OpenJiuwenMemoryProvider 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 OpenJiuwenMemoryProvider — MemoryProvider 接口的本地 LTM 适配，直接使用全局 LongTermMemory 单例

**Architecture:** OpenJiuwenProvider 实现 MemoryProvider 接口，嵌入 BaseMemoryProvider，支持双模式构造（config dict 驱动创建 store/embedding + 直接注入）。Initialize 中获取 LTM 全局单例并 RegisterStore。对外暴露 ltm_search 和 ltm_search_summary 两个工具，Prefetch 搜索用户记忆和历史摘要注入上下文，SyncTurn 将对话消息写入 LTM。

**Tech Stack:** Go 1.22+，项目已有的 ltm/kv/vector/db/embedding/config 包

---

### Task 1: 创建 OpenJiuwenProvider 结构体 + 常量 + 全局变量 + 构造函数

**Files:**
- Create: `internal/agentcore/memory/external/openjiuwen_provider.go`

- [ ] **Step 1: 创建文件骨架**

创建 `openjiuwen_provider.go`，包含 package、import、结构体定义、常量、全局变量和构造函数。

```go
package external

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	db "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/db"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	kv "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/kv"
	vector "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/vector"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/ltm"
	apiembedding "github.com/uapclaw-uapclaw-go/internal/agentcore/retrieval/embedding"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// ──────────────────────────── 结构体 ────────────────────────────

// OpenJiuwenProvider 基于 openjiuwen LongTermMemory 的外部记忆提供者。
// 对齐 Python: OpenJiuwenMemoryProvider (openjiuwen/core/memory/external/openjiuwen_memory_provider.py)
//
// 不依赖外部 REST API，直接使用项目自身 LTM 引擎（全局单例）。
// 支持双模式构造：config dict 驱动创建 store/embedding + 直接注入。
type OpenJiuwenProvider struct {
	BaseMemoryProvider
	// config 配置字典（对齐 Python self._config）
	config map[string]any
	// kvStore KV 存储（直接注入或 Initialize 中根据 config 创建）
	kvStore kv.BaseKVStore
	// vectorStore 向量存储（直接注入或 Initialize 中根据 config 创建）
	vectorStore vector.BaseVectorStore
	// dbStore 数据库存储（直接注入或 Initialize 中根据 config 创建）
	dbStore db.BaseDbStore
	// embeddingModel 嵌入模型（直接注入或 Initialize 中根据 config 创建）
	embeddingModel embedding.BaseEmbedding
	// engineConfig 记忆引擎配置（直接注入）
	engineConfig *config.MemoryEngineConfig
	// scopeConfig 记忆作用域配置（直接注入或从 config 解析）
	scopeConfig *config.MemoryScopeConfig
	// agentMemoryConfig Agent 记忆配置（直接注入）
	agentMemoryConfig *config.AgentMemoryConfig
	// userID 用户标识
	userID string
	// scopeID 作用域标识
	scopeID string
	// sessionID 会话标识
	sessionID string
	// initialized 是否已初始化
	initialized bool
}

// OpenJiuwenProviderOption OpenJiuwenProvider 的可选参数。
type OpenJiuwenProviderOption func(*OpenJiuwenProvider)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultRecallUserMemNum 默认用户记忆召回数量
	// 对齐 Python: DEFAULT_RECALL_USER_MEM_NUM = 5
	defaultRecallUserMemNum = 5
	// defaultRecallHistoryMemNum 默认历史摘要召回数量
	// 对齐 Python: DEFAULT_RECALL_HISTORY_MEM_NUM = 3
	defaultRecallHistoryMemNum = 3
	// defaultKVBackend 默认 KV 后端
	// 对齐 Python: _DEFAULT_KV_BACKEND = "memory"
	defaultKVBackend = "memory"
	// defaultVectorBackend 默认向量后端
	// 对齐 Python: _DEFAULT_VECTOR_BACKEND = "chroma"
	defaultVectorBackend = "chroma"
	// defaultDBBackend 默认数据库后端
	// 对齐 Python: _DEFAULT_DB_BACKEND = "sqlite"
	defaultDBBackend = "sqlite"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// ojLogComponent 日志组件标识
	ojLogComponent = logger.ComponentAgentCore

	// ltmSearchSchema ltm_search 工具 Schema
	// 对齐 Python: LTM_SEARCH_SCHEMA
	ltmSearchSchema = ToolSchema{
		Name:        "ltm_search",
		Description: "在长期记忆中搜索相关信息。搜索范围包括用户用户画像、情景记忆和语义记忆。",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query":     map[string]any{"type": "string", "description": "搜索查询内容"},
				"num":       map[string]any{"type": "integer", "description": "最大返回结果数量", "default": defaultRecallUserMemNum},
				"threshold": map[string]any{"type": "number", "description": "最小相关性阈值 (0-1)", "default": 0.3},
			},
			"required": []string{"query"},
		},
	}

	// ltmSearchSummarySchema ltm_search_summary 工具 Schema
	// 对齐 Python: LTM_SEARCH_SUMMARY_SCHEMA
	ltmSearchSummarySchema = ToolSchema{
		Name:        "ltm_search_summary",
		Description: "在长期记忆中搜索历史会话摘要。用于回忆之前讨论的话题和达成的结论。",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string", "description": "搜索查询内容"},
				"num":   map[string]any{"type": "integer", "description": "最大返回结果数量", "default": defaultRecallHistoryMemNum},
			},
			"required": []string{"query"},
		},
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewOpenJiuwenProvider 创建 OpenJiuwenProvider 实例。
// 对齐 Python: OpenJiuwenMemoryProvider.__init__(config, kv_store, vector_store, db_store, embedding_model, engine_config, scope_config, agent_memory_config)
//
// 双模式构造：config dict 驱动创建 store/embedding + 直接注入。
// config 为 nil 时默认空 map。注入的 store/embedding 优先，缺失的由 Initialize 中根据 config 创建。
func NewOpenJiuwenProvider(config map[string]any, opts ...OpenJiuwenProviderOption) *OpenJiuwenProvider {
	if config == nil {
		config = make(map[string]any)
	}
	p := &OpenJiuwenProvider{
		config:            config,
		userID:            ltm.DefaultValue,
		scopeID:           ltm.DefaultValue,
		sessionID:         ltm.DefaultValue,
		agentMemoryConfig: config.DefaultAgentMemoryConfig(),
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// WithKVStore 设置 KV 存储。
func WithKVStore(store kv.BaseKVStore) OpenJiuwenProviderOption {
	return func(p *OpenJiuwenProvider) { p.kvStore = store }
}

// WithVectorStore 设置向量存储。
func WithVectorStore(store vector.BaseVectorStore) OpenJiuwenProviderOption {
	return func(p *OpenJiuwenProvider) { p.vectorStore = store }
}

// WithDbStore 设置数据库存储。
func WithDbStore(store db.BaseDbStore) OpenJiuwenProviderOption {
	return func(p *OpenJiuwenProvider) { p.dbStore = store }
}

// WithEmbeddingModel 设置嵌入模型。
func WithEmbeddingModel(model embedding.BaseEmbedding) OpenJiuwenProviderOption {
	return func(p *OpenJiuwenProvider) { p.embeddingModel = model }
}

// WithEngineConfig 设置记忆引擎配置。
func WithEngineConfig(cfg *config.MemoryEngineConfig) OpenJiuwenProviderOption {
	return func(p *OpenJiuwenProvider) { p.engineConfig = cfg }
}

// WithScopeConfig 设置记忆作用域配置。
func WithScopeConfig(cfg *config.MemoryScopeConfig) OpenJiuwenProviderOption {
	return func(p *OpenJiuwenProvider) { p.scopeConfig = cfg }
}

// WithAgentMemoryConfig 设置 Agent 记忆配置。
func WithAgentMemoryConfig(cfg *config.AgentMemoryConfig) OpenJiuwenProviderOption {
	return func(p *OpenJiuwenProvider) { p.agentMemoryConfig = cfg }
}
```

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/memory/external/...`
Expected: 编译通过（部分方法尚未实现，后续步骤补充）

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/memory/external/openjiuwen_provider.go
git commit -m "feat(7.16): 添加 OpenJiuwenProvider 结构体+常量+构造函数"
```

---

### Task 2: 实现 MemoryProvider 接口的简单方法（Name/IsAvailable/IsInitialized/GetToolSchemas/SystemPromptBlock/Shutdown）

**Files:**
- Modify: `internal/agentcore/memory/external/openjiuwen_provider.go`

- [ ] **Step 1: 在导出函数区域添加以下方法**

```go
// Name 返回 Provider 唯一名称。
// Python: @property name -> "openjiuwen"
func (p *OpenJiuwenProvider) Name() string {
	return "openjiuwen"
}

// IsAvailable 检查 Provider 是否已配置且就绪。
// Python: is_available() -> bool
// 三 store 都有 OR config.embedding.model_name 存在
func (p *OpenJiuwenProvider) IsAvailable() bool {
	if p.kvStore != nil && p.vectorStore != nil && p.dbStore != nil {
		return true
	}
	embedCfg, _ := p.config["embedding"].(map[string]any)
	if embedCfg == nil {
		return false
	}
	modelName, _ := embedCfg["model_name"].(string)
	return modelName != ""
}

// IsInitialized 返回 Provider 是否已初始化。
// Python: @property is_initialized -> self._is_initialized
func (p *OpenJiuwenProvider) IsInitialized() bool {
	return p.initialized
}

// GetToolSchemas 返回 Provider 提供的工具 Schema 列表。
// Python: get_tool_schemas() -> [LTM_SEARCH_SCHEMA, LTM_SEARCH_SUMMARY_SCHEMA]
func (p *OpenJiuwenProvider) GetToolSchemas() []ToolSchema {
	return []ToolSchema{ltmSearchSchema, ltmSearchSummarySchema}
}

// SystemPromptBlock 返回 Provider 的系统提示词引导块。
// Python: system_prompt_block() (L133-148)
// 提示词内容从 Python 源码直接复制，禁止自行翻译或改写。
func (p *OpenJiuwenProvider) SystemPromptBlock() string {
	return "# Long-Term Memory System\n\n" +
		"You have long-term memory capabilities and can remember user information across sessions. " +
		"The system automatically extracts valuable information from conversations and stores it in memory.\n\n" +
		"## Memory Search\n\n" +
		"When you need to recall previous information, use the `ltm_search` tool to search long-term memory.\n" +
		"- Search queries should contain key information (names, dates, event keywords)\n" +
		"- If results are insufficient, try searching again with different keywords\n\n" +
		"## Automatic Memory\n\n" +
		"The system automatically extracts from each conversation:\n" +
		"- User profile (identity, preferences, habits)\n" +
		"- Episodic memory (specific events, decisions)\n" +
		"- Semantic memory (background knowledge, technical details)\n" +
		"- Conversation summaries (key conclusions, main points)"
}

// Shutdown 关闭 Provider 释放资源。
// Python: async def shutdown() -> None: self._is_initialized = False
func (p *OpenJiuwenProvider) Shutdown(_ context.Context) error {
	p.initialized = false
	return nil
}
```

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/memory/external/...`
Expected: 编译通过

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/memory/external/openjiuwen_provider.go
git commit -m "feat(7.16): 实现 Name/IsAvailable/IsInitialized/GetToolSchemas/SystemPromptBlock/Shutdown"
```

---

### Task 3: 实现 4 个 create* 工厂方法 + parseScopeConfig

**Files:**
- Modify: `internal/agentcore/memory/external/openjiuwen_provider.go`

- [ ] **Step 1: 在非导出函数区域添加以下方法**

```go
// ──────────────────────────── 非导出函数 ────────────────────────────

// createKVStore 根据 config 创建 KV 存储。
// 对齐 Python: _create_kv_store (L240-258)
func (p *OpenJiuwenProvider) createKVStore() kv.BaseKVStore {
	kvCfg, _ := p.config["kv"].(map[string]any)
	if kvCfg == nil {
		kvCfg = make(map[string]any)
	}
	backend, _ := kvCfg["backend"].(string)
	if backend == "" {
		backend = defaultKVBackend
	}
	switch backend {
	case "memory":
		return kv.NewInMemoryKVStore()
	case "shelve":
		path, _ := kvCfg["path"].(string)
		if path == "" {
			path = resolveLTMDir() + "/kv"
		}
		store, err := kv.NewFileKVStore(path)
		if err != nil {
			// 对齐 Python: logger.error("[OpenJiuwenMemoryProvider] KV store creation failed ({backend}): {e}")
			logger.Error(ojLogComponent).Str("backend", backend).Err(err).Msg("KV store 创建失败")
			return nil
		}
		return store
	case "sqlite":
		// 对齐 Python: DbBasedKVStore(engine)
		path, _ := kvCfg["path"].(string)
		if path == "" {
			path = resolveLTMDir() + "/memory_kv.db"
		}
		dsn := fmt.Sprintf("file:%s?cache=shared&_journal_mode=WAL", path)
		gormDB, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
		if err != nil {
			logger.Error(ojLogComponent).Str("backend", backend).Err(err).Msg("KV store 创建失败")
			return nil
		}
		return kv.NewDbBasedKVStore(gormDB)
	default:
		logger.Error(ojLogComponent).Str("backend", backend).Msg("不支持的 KV 后端类型")
		return nil
	}
}

// createVectorStore 根据 config 创建向量存储。
// 对齐 Python: _create_vector_store (L260-270)
func (p *OpenJiuwenProvider) createVectorStore() vector.BaseVectorStore {
	vecCfg, _ := p.config["vector"].(map[string]any)
	if vecCfg == nil {
		vecCfg = make(map[string]any)
	}
	backend, _ := vecCfg["backend"].(string)
	if backend == "" {
		backend = defaultVectorBackend
	}
	switch backend {
	case "chroma":
		persistDir, _ := vecCfg["persist_directory"].(string)
		if persistDir == "" {
			persistDir = resolveLTMDir() + "/chroma"
		}
		return vector.NewChromaVectorStore(persistDir)
	default:
		logger.Error(ojLogComponent).Str("backend", backend).Msg("不支持的 vector 后端类型")
		return nil
	}
}

// createDBStore 根据 config 创建数据库存储。
// 对齐 Python: _create_db_store (L272-284)
func (p *OpenJiuwenProvider) createDBStore(ctx context.Context) db.BaseDbStore {
	dbCfg, _ := p.config["db"].(map[string]any)
	if dbCfg == nil {
		dbCfg = make(map[string]any)
	}
	backend, _ := dbCfg["backend"].(string)
	if backend == "" {
		backend = defaultDBBackend
	}
	switch backend {
	case "sqlite":
		path, _ := dbCfg["path"].(string)
		if path == "" {
			path = resolveLTMDir() + "/ltm.db"
		}
		dsn := fmt.Sprintf("file:%s?cache=shared&_journal_mode=WAL", path)
		gormDB, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
		if err != nil {
			logger.Error(ojLogComponent).Str("backend", backend).Err(err).Msg("DB store 创建失败")
			return nil
		}
		return db.NewDefaultDbStore(gormDB)
	default:
		logger.Error(ojLogComponent).Str("backend", backend).Msg("不支持的 DB 后端类型")
		return nil
	}
}

// createEmbedding 根据 config 创建嵌入模型。
// 对齐 Python: _create_embedding (L286-300)
func (p *OpenJiuwenProvider) createEmbedding() embedding.BaseEmbedding {
	embedCfg, _ := p.config["embedding"].(map[string]any)
	if embedCfg == nil {
		return nil
	}
	modelName, _ := embedCfg["model_name"].(string)
	if modelName == "" {
		return nil
	}
	baseURL, _ := embedCfg["base_url"].(string)
	apiKey, _ := embedCfg["api_key"].(string)
	cfg := apiembedding.EmbeddingConfig{
		ModelName: modelName,
		BaseURL:   baseURL,
		APIKey:    apiKey,
	}
	emb := apiembedding.NewAPIEmbedding(cfg)
	if emb == nil {
		logger.Error(ojLogComponent).Msg("Embedding 创建失败")
		return nil
	}
	return emb
}

// parseScopeConfig 从 config 解析 MemoryScopeConfig。
// 对齐 Python: _parse_scope_config (L230-238)
func (p *OpenJiuwenProvider) parseScopeConfig() *config.MemoryScopeConfig {
	scopeCfg, _ := p.config["scope_config"].(map[string]any)
	if len(scopeCfg) == 0 {
		return nil
	}
	data, err := json.Marshal(scopeCfg)
	if err != nil {
		// 对齐 Python: logger.warning("[OpenJiuwenMemoryProvider] Failed to parse scope_config: {e}")
		logger.Warn(ojLogComponent).Err(err).Msg("解析 scope_config 失败")
		return nil
	}
	cfg := &config.MemoryScopeConfig{}
	if err := json.Unmarshal(data, cfg); err != nil {
		logger.Warn(ojLogComponent).Err(err).Msg("解析 scope_config 失败")
		return nil
	}
	return cfg
}

// resolveLTMDir 解析 LTM 数据目录。
// 对齐 Python: _resolve_ltm_dir() → {workspace_dir}/memory/ltm
func resolveLTMDir() string {
	return filepath.Join(workspace.WorkspaceDir(), "memory", "ltm")
}
```

需要在 import 中添加 `"gorm.io/gorm"` 和 `"gorm.io/driver/sqlite"` 和 `"encoding/json"`。

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/memory/external/...`
Expected: 编译通过

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/memory/external/openjiuwen_provider.go
git commit -m "feat(7.16): 实现 4 个 create* 工厂方法 + parseScopeConfig + resolveLTMDir"
```

---

### Task 4: 实现 Initialize 方法

**Files:**
- Modify: `internal/agentcore/memory/external/openjiuwen_provider.go`

- [ ] **Step 1: 在导出函数区域添加 Initialize**

```go
// Initialize 初始化 Provider。
// 对齐 Python: async def initialize(self, **kwargs) -> None (L100-131)
//
// 完整流程：
// 1. 从 opts 解析 user_id / scope_id / session_id
// 2. 缺失 store → 根据 config 创建
// 3. 获取 LTM 全局单例
// 4. LTM.KVStore 为 nil → RegisterStore
// 5. scopeConfig 不为 nil → LTM.SetScopeConfig
// 6. p.initialized = true
func (p *OpenJiuwenProvider) Initialize(ctx context.Context, opts ...ProviderOption) error {
	po := applyOptions(opts...)

	// 对齐 Python: self._user_id = kwargs.get("user_id", self._user_id)
	if po.UserID != "" {
		p.userID = po.UserID
	}
	if po.ScopeID != "" {
		p.scopeID = po.ScopeID
	}
	if po.SessionID != "" {
		p.sessionID = po.SessionID
	}

	// 对齐 Python: if self._kv_store is None: self._kv_store = self._create_kv_store()
	if p.kvStore == nil {
		p.kvStore = p.createKVStore()
	}
	if p.vectorStore == nil {
		p.vectorStore = p.createVectorStore()
	}
	if p.dbStore == nil {
		p.dbStore = p.createDBStore(ctx)
	}
	if p.embeddingModel == nil {
		p.embeddingModel = p.createEmbedding()
	}

	// 对齐 Python: if not self._kv_store or not self._vector_store or not self._db_store: logger.error(...); return
	if p.kvStore == nil || p.vectorStore == nil || p.dbStore == nil {
		logger.Error(ojLogComponent).Msg("Store 创建失败")
		return nil
	}

	// 对齐 Python: self._ltm = LongTermMemory()
	ltmInstance := ltm.GetLongTermMemory()

	// 对齐 Python: if self._ltm.kv_store is None: await self._ltm.register_store(...)
	if ltmInstance.KVStore() == nil {
		registerOpts := []ltm.RegisterStoreOption{
			ltm.WithVectorStore(p.vectorStore),
			ltm.WithDbStore(p.dbStore),
			ltm.WithEmbeddingModel(p.embeddingModel),
		}
		if err := ltmInstance.RegisterStore(ctx, p.kvStore, registerOpts...); err != nil {
			logger.Error(ojLogComponent).Err(err).Msg("LTM RegisterStore 失败")
			return err
		}
	}

	// 对齐 Python: if self._scope_config: await self._ltm.set_scope_config(self._scope_id, self._scope_config)
	if p.scopeConfig == nil {
		p.scopeConfig = p.parseScopeConfig()
	}
	if p.scopeConfig != nil {
		if err := ltmInstance.SetScopeConfig(ctx, p.scopeID, p.scopeConfig); err != nil {
			logger.Warn(ojLogComponent).Err(err).Str("scope_id", p.scopeID).Msg("SetScopeConfig 失败")
		}
	}

	p.initialized = true
	return nil
}
```

注意：需要检查 `ltm.LongTermMemory` 是否有 `KVStore()` 导出方法暴露内部 kvStore 字段。如果没有，需要在 `ltm` 包中添加该方法。

- [ ] **Step 2: 检查/添加 ltm.KVStore() 访问方法**

如果 `ltm.LongTermMemory` 没有 `KVStore()` 方法，在 `long_term_memory.go` 的导出函数区域添加：

```go
// KVStore 返回已注册的 KV 存储（nil 表示未注册）。
// 供 OpenJiuwenProvider 判断是否需要 RegisterStore。
func (m *LongTermMemory) KVStore() kv.BaseKVStore {
	return m.kvStore
}
```

- [ ] **Step 3: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/memory/...`
Expected: 编译通过

- [ ] **Step 4: Commit**

```bash
git add internal/agentcore/memory/external/openjiuwen_provider.go internal/agentcore/memory/ltm/long_term_memory.go
git commit -m "feat(7.16): 实现 Initialize + ltm.KVStore() 访问方法"
```

---

### Task 5: 实现 HandleToolCall + handleSearch + handleSearchSummary

**Files:**
- Modify: `internal/agentcore/memory/external/openjiuwen_provider.go`

- [ ] **Step 1: 在导出函数区域添加 HandleToolCall**

```go
// HandleToolCall 处理工具调用并返回结果字符串。
// 对齐 Python: async def handle_tool_call(self, tool_name, args) -> str (L153-164)
func (p *OpenJiuwenProvider) HandleToolCall(ctx context.Context, toolName string, args map[string]any) (string, error) {
	if !p.initialized {
		b, _ := json.Marshal(map[string]string{"error": "Memory provider not initialized"})
		return string(b), nil
	}
	ltmInstance := ltm.GetLongTermMemory()
	if !ltmInstance.IsInitialized() {
		b, _ := json.Marshal(map[string]string{"error": "Memory provider not initialized"})
		return string(b), nil
	}
	try := func() (any, error) {
		switch toolName {
		case "ltm_search":
			return p.handleSearch(ctx, args)
		case "ltm_search_summary":
			return p.handleSearchSummary(ctx, args)
		default:
			return nil, fmt.Errorf("unknown tool: %s", toolName)
		}
	}
	result, err := try()
	if err != nil {
		b, _ := json.Marshal(map[string]any{"error": err.Error(), "results": []any{}})
		return string(b), nil
	}
	b, _ := json.Marshal(result)
	return string(b), nil
}
```

注意：需要检查 `ltm.LongTermMemory` 是否有 `IsInitialized()` 导出方法。如果没有，需要在 `ltm` 包中添加。

- [ ] **Step 2: 在非导出函数区域添加 handleSearch 和 handleSearchSummary**

```go
// handleSearch 处理 ltm_search 工具调用。
// 对齐 Python: _handle_search (L302-324)
func (p *OpenJiuwenProvider) handleSearch(ctx context.Context, args map[string]any) (any, error) {
	query, _ := args["query"].(string)
	num := defaultRecallUserMemNum
	if n, ok := args["num"]; ok {
		switch v := n.(type) {
		case float64:
			num = int(v)
		case int:
			num = v
		}
	}
	threshold := 0.3
	if t, ok := args["threshold"]; ok {
		switch v := t.(type) {
		case float64:
			threshold = v
		}
	}

	ltmInstance := ltm.GetLongTermMemory()
	results, err := ltmInstance.SearchUserMem(ctx, query, num,
		ltm.SearchWithUserID(p.userID),
		ltm.SearchWithScopeID(p.scopeID),
		ltm.SearchWithThreshold(threshold),
	)
	if err != nil {
		return nil, err
	}

	type searchResult struct {
		ID      string `json:"id"`
		Content string `json:"content"`
		Type    string `json:"type"`
		Score   float64 `json:"score"`
	}
	out := make([]searchResult, 0, len(results))
	for _, r := range results {
		typeStr := "unknown"
		if r.MemInfo != nil {
			typeStr = r.MemInfo.Type.String()
		}
		memID := ""
		content := ""
		score := 0.0
		if r.MemInfo != nil {
			memID = r.MemInfo.MemID
			content = r.MemInfo.Content
		}
		score = r.Score
		out = append(out, searchResult{
			ID: memID, Content: content, Type: typeStr, Score: score,
		})
	}
	return map[string]any{"results": out, "count": len(out)}, nil
}

// handleSearchSummary 处理 ltm_search_summary 工具调用。
// 对齐 Python: _handle_search_summary (L326-347)
func (p *OpenJiuwenProvider) handleSearchSummary(ctx context.Context, args map[string]any) (any, error) {
	query, _ := args["query"].(string)
	num := defaultRecallHistoryMemNum
	if n, ok := args["num"]; ok {
		switch v := n.(type) {
		case float64:
			num = int(v)
		case int:
			num = v
		}
	}

	ltmInstance := ltm.GetLongTermMemory()
	results, err := ltmInstance.SearchUserHistorySummary(ctx, query, num,
		ltm.SearchWithUserID(p.userID),
		ltm.SearchWithScopeID(p.scopeID),
	)
	if err != nil {
		return nil, err
	}

	type summaryResult struct {
		ID      string  `json:"id"`
		Content string  `json:"content"`
		Score   float64 `json:"score"`
	}
	out := make([]summaryResult, 0, len(results))
	for _, r := range results {
		memID := ""
		content := ""
		score := 0.0
		if r.MemInfo != nil {
			memID = r.MemInfo.MemID
			content = r.MemInfo.Content
		}
		score = r.Score
		out = append(out, summaryResult{ID: memID, Content: content, Score: score})
	}
	return map[string]any{"results": out, "count": len(out)}, nil
}
```

- [ ] **Step 3: 检查/添加 ltm.IsInitialized() 方法**

如果 `ltm.LongTermMemory` 没有 `IsInitialized()` 方法，在 `long_term_memory.go` 的导出函数区域添加：

```go
// IsInitialized 返回 LTM 是否已初始化（kvStore/dbStore 已注册且 SetConfig 已调用）。
func (m *LongTermMemory) IsInitialized() bool {
	return m.kvStore != nil && m.dbStore != nil && m.searchManager != nil
}
```

- [ ] **Step 4: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/memory/...`
Expected: 编译通过

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/memory/external/openjiuwen_provider.go internal/agentcore/memory/ltm/long_term_memory.go
git commit -m "feat(7.16): 实现 HandleToolCall + handleSearch + handleSearchSummary + ltm.IsInitialized()"
```

---

### Task 6: 实现 Prefetch + SyncTurn

**Files:**
- Modify: `internal/agentcore/memory/external/openjiuwen_provider.go`

- [ ] **Step 1: 在导出函数区域添加 Prefetch 和 SyncTurn**

```go
// Prefetch 根据查询预取记忆上下文。
// 对齐 Python: async def prefetch(self, query, **kwargs) -> str (L166-201)
func (p *OpenJiuwenProvider) Prefetch(ctx context.Context, query string, opts ...ProviderOption) (string, error) {
	ltmInstance := ltm.GetLongTermMemory()
	if !p.initialized || !ltmInstance.IsInitialized() {
		return "", nil
	}
	po := applyOptions(opts...)
	userID := p.userID
	if po.UserID != "" {
		userID = po.UserID
	}
	scopeID := p.scopeID
	if po.ScopeID != "" {
		scopeID = po.ScopeID
	}

	parts := make([]string, 0)

	// 对齐 Python: mem_results = await self._ltm.search_user_mem(query=query, num=5, user_id=user_id, scope_id=scope_id, threshold=0.3)
	func() {
		defer func() {
			if r := recover(); r != nil {
				// 对齐 Python: except Exception as e: logger.warning(f"prefetch search_user_mem failed: {e}")
				logger.Warn(ojLogComponent).Any("panic", r).Msg("prefetch search_user_mem 失败")
			}
		}()
		results, err := ltmInstance.SearchUserMem(ctx, query, defaultRecallUserMemNum,
			ltm.SearchWithUserID(userID),
			ltm.SearchWithScopeID(scopeID),
			ltm.SearchWithThreshold(0.3),
		)
		if err != nil {
			// 对齐 Python: logger.warning(f"prefetch search_user_mem failed: {e}")
			logger.Warn(ojLogComponent).Err(err).Msg("prefetch search_user_mem 失败")
			return
		}
		if len(results) > 0 {
			parts = append(parts, "## Related Memories")
			for _, r := range results {
				typeLabel := "unknown"
				if r.MemInfo != nil {
					typeLabel = r.MemInfo.Type.String()
				}
				content := ""
				if r.MemInfo != nil {
					content = r.MemInfo.Content
				}
				parts = append(parts, fmt.Sprintf("- [%s] %s (score: %.2f)", typeLabel, content, r.Score))
			}
		}
	}()

	// 对齐 Python: summary_results = await self._ltm.search_user_history_summary(query=query, num=3, user_id=user_id, scope_id=scope_id, threshold=0.3)
	func() {
		defer func() {
			if r := recover(); r != nil {
				// 对齐 Python: except Exception as e: logger.warning(f"prefetch search_user_history_summary failed: {e}")
				logger.Warn(ojLogComponent).Any("panic", r).Msg("prefetch search_user_history_summary 失败")
			}
		}()
		results, err := ltmInstance.SearchUserHistorySummary(ctx, query, defaultRecallHistoryMemNum,
			ltm.SearchWithUserID(userID),
			ltm.SearchWithScopeID(scopeID),
		)
		if err != nil {
			logger.Warn(ojLogComponent).Err(err).Msg("prefetch search_user_history_summary 失败")
			return
		}
		if len(results) > 0 {
			parts = append(parts, "\n## Related History Summaries")
			for _, r := range results {
				content := ""
				if r.MemInfo != nil {
					content = r.MemInfo.Content
				}
				parts = append(parts, fmt.Sprintf("- %s (score: %.2f)", content, r.Score))
			}
		}
	}()

	// 对齐 Python: if not parts: return ""
	if len(parts) == 0 {
		return "", nil
	}
	// 对齐 Python: return "\n".join(parts)
	result := ""
	for i, part := range parts {
		if i > 0 {
			result += "\n"
		}
		result += part
	}
	return result, nil
}

// SyncTurn 同步一轮对话到外部记忆。
// 对齐 Python: async def sync_turn(self, user_msg, assistant_msg, **kwargs) -> None (L203-225)
func (p *OpenJiuwenProvider) SyncTurn(ctx context.Context, userMsg, assistantMsg string, opts ...ProviderOption) error {
	ltmInstance := ltm.GetLongTermMemory()
	if !p.initialized || !ltmInstance.IsInitialized() {
		return nil
	}
	po := applyOptions(opts...)
	userID := p.userID
	if po.UserID != "" {
		userID = po.UserID
	}
	scopeID := p.scopeID
	if po.ScopeID != "" {
		scopeID = po.ScopeID
	}
	sessionID := p.sessionID
	if po.SessionID != "" {
		sessionID = po.SessionID
	}

	// 对齐 Python: messages: list[BaseMessage] = []
	var messages []llmschema.BaseMessage
	// 对齐 Python: if user_msg: messages.append(UserMessage(content=user_msg))
	if userMsg != "" {
		messages = append(messages, llmschema.NewUserMessage(userMsg))
	}
	// 对齐 Python: if assistant_msg: messages.append(AssistantMessage(content=assistant_msg))
	if assistantMsg != "" {
		messages = append(messages, llmschema.NewAssistantMessage(assistantMsg))
	}
	// 对齐 Python: if not messages: return
	if len(messages) == 0 {
		return nil
	}

	// 对齐 Python: await self._ltm.add_messages(messages, self._agent_memory_config, user_id=user_id, scope_id=scope_id, session_id=session_id)
	func() {
		defer func() {
			if r := recover(); r != nil {
				// 对齐 Python: except Exception as e: logger.warning(f"sync_turn add_messages failed: {e}")
				logger.Warn(ojLogComponent).Any("panic", r).Msg("sync_turn add_messages 失败")
			}
		}()
		_, err := ltmInstance.AddMessages(ctx, messages, p.agentMemoryConfig,
			ltm.WithUserID(userID),
			ltm.WithScopeID(scopeID),
			ltm.WithSessionID(sessionID),
		)
		if err != nil {
			// 对齐 Python: except Exception as e: logger.warning(f"sync_turn add_messages failed: {e}")
			logger.Warn(ojLogComponent).Err(err).Msg("sync_turn add_messages 失败")
		}
	}()

	return nil
}
```

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/memory/external/...`
Expected: 编译通过

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/memory/external/openjiuwen_provider.go
git commit -m "feat(7.16): 实现 Prefetch + SyncTurn"
```

---

### Task 7: 编写单元测试

**Files:**
- Create: `internal/agentcore/memory/external/openjiuwen_provider_test.go`

- [ ] **Step 1: 创建测试文件**

```go
package external

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

func TestNewOpenJiuwenProvider_空配置(t *testing.T) {
	p := NewOpenJiuwenProvider(nil)
	assert.Equal(t, "openjiuwen", p.Name())
	assert.False(t, p.IsAvailable())
	assert.False(t, p.IsInitialized())
}

func TestNewOpenJiuwenProvider_嵌入配置可用(t *testing.T) {
	p := NewOpenJiuwenProvider(map[string]any{
		"embedding": map[string]any{
			"model_name": "text-embedding-v3",
		},
	})
	assert.True(t, p.IsAvailable())
}

func TestOpenJiuwenProvider_GetToolSchemas(t *testing.T) {
	p := NewOpenJiuwenProvider(nil)
	schemas := p.GetToolSchemas()
	assert.Len(t, schemas, 2)
	assert.Equal(t, "ltm_search", schemas[0].Name)
	assert.Equal(t, "ltm_search_summary", schemas[1].Name)
}

func TestOpenJiuwenProvider_SystemPromptBlock(t *testing.T) {
	p := NewOpenJiuwenProvider(nil)
	block := p.SystemPromptBlock()
	assert.Contains(t, block, "Long-Term Memory System")
	assert.Contains(t, block, "ltm_search")
	assert.Contains(t, block, "User profile")
}

func TestOpenJiuwenProvider_Shutdown(t *testing.T) {
	p := NewOpenJiuwenProvider(nil)
	p.initialized = true
	err := p.Shutdown(context.Background())
	assert.NoError(t, err)
	assert.False(t, p.IsInitialized())
}

func TestOpenJiuwenProvider_HandleToolCall_未初始化(t *testing.T) {
	p := NewOpenJiuwenProvider(nil)
	result, err := p.HandleToolCall(context.Background(), "ltm_search", map[string]any{"query": "test"})
	assert.NoError(t, err)
	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))
	assert.Equal(t, "Memory provider not initialized", parsed["error"])
}

func TestOpenJiuwenProvider_HandleToolCall_未知工具(t *testing.T) {
	p := NewOpenJiuwenProvider(nil)
	p.initialized = true
	// 直接调用 handleSearch 测试未知工具路径
	result, err := p.HandleToolCall(context.Background(), "unknown_tool", map[string]any{})
	assert.NoError(t, err)
	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))
	assert.Contains(t, parsed["error"], "unknown tool")
}

func TestOpenJiuwenProvider_Prefetch_未初始化(t *testing.T) {
	p := NewOpenJiuwenProvider(nil)
	result, err := p.Prefetch(context.Background(), "test query")
	assert.NoError(t, err)
	assert.Equal(t, "", result)
}

func TestOpenJiuwenProvider_SyncTurn_未初始化(t *testing.T) {
	p := NewOpenJiuwenProvider(nil)
	err := p.SyncTurn(context.Background(), "hello", "world")
	assert.NoError(t, err)
}

func TestOpenJiuwenProvider_SyncTurn_空消息(t *testing.T) {
	p := NewOpenJiuwenProvider(nil)
	p.initialized = true
	err := p.SyncTurn(context.Background(), "", "")
	assert.NoError(t, err)
}

func TestOpenJiuwenProvider_parseScopeConfig_空配置(t *testing.T) {
	p := NewOpenJiuwenProvider(nil)
	cfg := p.parseScopeConfig()
	assert.Nil(t, cfg)
}

func TestOpenJiuwenProvider_parseScopeConfig_有效配置(t *testing.T) {
	p := NewOpenJiuwenProvider(map[string]any{
		"scope_config": map[string]any{
			"user_profile_definition":    "测试画像",
			"semantic_memory_definition": "测试语义",
			"episodic_memory_definition": "测试情景",
		},
	})
	cfg := p.parseScopeConfig()
	require.NotNil(t, cfg)
	assert.Equal(t, "测试画像", cfg.UserProfileDefinition)
	assert.Equal(t, "测试语义", cfg.SemanticMemoryDefinition)
	assert.Equal(t, "测试情景", cfg.EpisodicMemoryDefinition)
}

func TestOpenJiuwenProvider_createKVStore_memory后端(t *testing.T) {
	p := NewOpenJiuwenProvider(map[string]any{
		"kv": map[string]any{"backend": "memory"},
	})
	store := p.createKVStore()
	assert.NotNil(t, store)
}

func TestOpenJiuwenProvider_createEmbedding_无配置(t *testing.T) {
	p := NewOpenJiuwenProvider(nil)
	emb := p.createEmbedding()
	assert.Nil(t, emb)
}

func TestOpenJiuwenProvider_createEmbedding_有配置(t *testing.T) {
	p := NewOpenJiuwenProvider(map[string]any{
		"embedding": map[string]any{
			"model_name": "text-embedding-v3",
			"base_url":   "https://api.example.com",
			"api_key":    "test-key",
		},
	})
	emb := p.createEmbedding()
	assert.NotNil(t, emb)
}
```

- [ ] **Step 2: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/external/... -run TestOpenJiuwenProvider -v`
Expected: 所有测试通过

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/memory/external/openjiuwen_provider_test.go
git commit -m "test(7.16): 添加 OpenJiuwenProvider 单元测试"
```

---

### Task 8: 回填 deep_adapter_rails.go — 添加 openjiuwen 分支 + buildOpenJiuwenProviderConfig

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter_rails.go`

- [ ] **Step 1: 在 buildExternalMemoryProvider 的 switch 中添加 openjiuwen 分支**

在 `case "openviking":` 块之后、`default:` 之前，添加：

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

- [ ] **Step 2: 添加 buildOpenJiuwenProviderConfig 函数**

在文件非导出函数区域添加：

```go
// buildOpenJiuwenProviderConfig 将 jiuwenswarm 配置映射为 OpenJiuwenProvider 预期的 config dict。
// 对齐 Python: build_openjiuwen_provider_config(ext_cfg) (external_memory_config.py L87-126)
func buildOpenJiuwenProviderConfig(cfg map[string]any, configCache map[string]any) map[string]any {
	ojCfg, _ := cfg["openjiuwen"].(map[string]any)
	if ojCfg == nil {
		ojCfg = make(map[string]any)
	}
	ltmDir := filepath.Join(workspace.WorkspaceDir(), "memory", "ltm")

	// KV 配置
	kvBackend := strOr(strVal(ojCfg["kv_type"]), "shelve")
	kvBackend = strings.ToLower(strings.TrimSpace(kvBackend))
	kvPath := strOr(strVal(ojCfg["kv_path"]), filepath.Join(ltmDir, "kv"))

	// Vector 配置
	vectorBackend := strOr(strVal(ojCfg["vector_type"]), "chroma")
	vectorBackend = strings.ToLower(strings.TrimSpace(vectorBackend))
	vectorDir := strOr(strVal(ojCfg["vector_persist_dir"]), filepath.Join(ltmDir, "chroma"))

	// DB 配置
	dbBackend := strOr(strVal(ojCfg["db_type"]), "sqlite")
	dbBackend = strings.ToLower(strings.TrimSpace(dbBackend))
	dbPath := strOr(strVal(ojCfg["db_path"]), filepath.Join(ltmDir, "ltm.db"))

	// Embedding 配置 — 从顶层 embed 配置获取
	embedCfg, _ := configCache["embed"].(map[string]any)
	embeddingConfig := map[string]any{}
	if embedCfg != nil {
		modelName := strOr(strVal(embedCfg["embed_model"]), os.Getenv("EMBED_MODEL"))
		baseURL := strOr(strVal(embedCfg["embed_api_base"]), os.Getenv("EMBED_BASE_URL"))
		apiKey := strOr(strVal(embedCfg["embed_api_key"]), os.Getenv("EMBED_API_KEY"))
		embeddingConfig["model_name"] = modelName
		embeddingConfig["base_url"] = baseURL
		embeddingConfig["api_key"] = apiKey
		if modelName == "" {
			logger.Warn(logComponent).Msg("buildOpenJiuwenProviderConfig: Embedding 未配置 — LTM 将跳过向量搜索")
		}
	}

	return map[string]any{
		"kv":        map[string]any{"backend": kvBackend, "path": kvPath},
		"vector":    map[string]any{"backend": vectorBackend, "persist_directory": vectorDir},
		"db":        map[string]any{"backend": dbBackend, "path": dbPath},
		"embedding": embeddingConfig,
	}
}
```

需要在 import 中添加 `"path/filepath"` 和 `"os"`（如果还没有）。

- [ ] **Step 3: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/server/adapter/...`
Expected: 编译通过

- [ ] **Step 4: Commit**

```bash
git add internal/swarm/server/adapter/deep_adapter_rails.go
git commit -m "feat(7.16): 回填 deep_adapter_rails.go — openjiuwen 分支 + buildOpenJiuwenProviderConfig"
```

---

### Task 9: 更新 doc.go + IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `internal/agentcore/memory/external/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 doc.go 文件目录**

在 `doc.go` 的文件目录树中添加 `openjiuwen_provider.go`：

```
//	external/
//	├── doc.go                    # 包文档
//	├── provider.go               # MemoryProvider 接口 + BaseMemoryProvider + ToolSchema + ProviderOption
//	├── mem0_client.go            # Mem0 HTTP 客户端（REST API 封装：search/getAll/add）
//	├── mem0_provider.go          # Mem0Provider — MemoryProvider 的 Mem0 实现（熔断器+工具调用）
//	├── openjiuwen_provider.go    # OpenJiuwenProvider — MemoryProvider 的 openjiuwen LTM 实现（全局单例+双模式构造）
//	├── viking_client.go          # OpenViking HTTP 客户端（REST API 封装：health/post/get/close + 身份 Header）
//	└── viking_provider.go        # OpenVikingProvider — MemoryProvider 的 OpenViking 实现（5 工具+会话管理）
```

- [ ] **Step 2: 更新 IMPLEMENTATION_PLAN.md**

将 7.16 行的状态从 `☐` 改为 `✅`，描述更新为：

```
| 7.16 | ✅ | OpenJiuwenMemoryProvider | ✅ OpenJiuwenProvider（双模式构造+config 驱动工厂+LTM 全局单例+ltm_search/ltm_search_summary 2 工具+Prefetch+SyncTurn+Shutdown+deep_adapter_rails 回填） | `openjiuwen/core/memory/external/openjiuwen_memory_provider.py` |
```

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/memory/external/doc.go IMPLEMENTATION_PLAN.md
git commit -m "docs(7.16): 更新 doc.go 文件目录 + IMPLEMENTATION_PLAN.md 状态"
```

---

### Task 10: 运行全量编译 + 测试验证

- [ ] **Step 1: 检查残留 go 进程**

Run: `pgrep -f 'go (build|test)'`
Expected: 无残留进程（如有则 kill）

- [ ] **Step 2: 设置代理 + 全量编译**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./...`
Expected: 编译通过

- [ ] **Step 3: 运行 external 包测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/external/... -v -cover`
Expected: 所有测试通过，覆盖率 ≥ 85%

- [ ] **Step 4: 运行 ltm 包测试确认未破坏**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/ltm/... -v -cover`
Expected: 测试通过

- [ ] **Step 5: 运行 adapter 包测试确认回填未破坏**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/adapter/... -v -cover`
Expected: 测试通过
