# 9.82 Context Evolver P6 服务层+Agent集成 设计文档

## 概述

本文档描述 9.82 Context Evolver P6 的 Go 实现设计，包括 TaskMemoryService 核心编排器、
OpenAILLMWrapper/OpenAIEmbeddingWrapper 适配器、SummarizeTrajectories 补全、
ContextEvolvingReActAgent Agent 集成层，以及 P4 状态回填。

P6 是 P1-P5 算法层的"黏合层"——将各算法 Op 组装成完整的检索/总结管线，
对外暴露 TaskMemoryService 统一入口，并通过 ContextEvolvingReActAgent 将记忆检索
集成到 ReActAgent 的 invoke 循环中。

## 依赖关系

```
P1 核心框架 (✅) → P2 IO Schema (✅) → P3 ReMe (✅) → P4 RB (✅) → P5 ACE (✅)
                                                                       ↓
                                                                   P6 服务层 (本文档)
                                                                       ↓
                                                                   P7 Rail+工具
```

P6 依赖 P1-P5 全部完成。P4 代码+测试已全部完成（retrieve/rb 覆盖率 96.0%，
summary/rb 覆盖率 89.1%），但 IMPLEMENTATION_PLAN.md 中状态仍为 ☐，需回填为 ✅。

## 对应 Python 代码

| Python 文件 | Go 目标路径 |
|-------------|------------|
| `openjiuwen/extensions/context_evolver/core/config.py` | `internal/agentcore/context_evolver/core/config/config.go`（P6 新建） |
| `openjiuwen/extensions/context_evolver/service/task_memory_service.py` | `internal/agentcore/context_evolver/service/task_memory_service.go` |
| `openjiuwen/extensions/context_evolver/service/trajectory_generator.py` | `internal/agentcore/context_evolver/service/trajectory_generator.go`（补全 stub） |
| `openjiuwen/extensions/context_evolver/context_evolving_react_agent.py` | `internal/agentcore/context_evolver/context_evolving_react_agent.go` |

## 文件结构

```
context_evolver/
├── core/
│   └── config/
│       ├── doc.go                # 包文档
│       └── config.go             # 配置加载（P6 新建，对齐 Python config.py）
├── service/
│   ├── doc.go                       # 包文档（回填更新）
│   ├── trajectory_generator.go      # 补全 SummarizeTrajectories stub
│   ├── task_memory_service.go       # TaskMemoryService + AddMemoryRequest
│   ├── llm_wrapper.go              # OpenAILLMWrapper
│   └── embedding_wrapper.go        # OpenAIEmbeddingWrapper
├── context_evolving_react_agent.go  # ContextEvolvingReActAgent + MemoryAgentConfigInput
├── doc.go                           # 包文档（回填更新）
```

## 零、config 包（core/config/config.go）

Python 的 `context_evolver/core/config.py` 提供全局配置加载（.env + config.yaml）和 `get(key, default)` / `set_value(key, value)` / `snapshot()` / `restore(snap)` 接口。

Go 侧 P1-P5 未移植此模块，P6 需要新建。

### 设计

- 包路径：`internal/agentcore/context_evolver/core/config`
- 对齐 Python 的 `load()` / `get()` / `set_value()` / `delete()` / `snapshot()` / `restore()` / `reload()`
- Go 实现差异：
  - 不使用 `dotenv` 库（Go 项目已有 `internal/agentcore/config` 全局配置体系），context_evolver 的 config 包作为**局部配置覆盖层**
  - `Load(configPath)` 从 YAML 文件加载（使用 `gopkg.in/yaml.v3`）
  - `Get(key, default)` 查找顺序：内部配置 → 环境变量 → default
  - 环境变量类型转换：对齐 Python `_convert_value`（bool/int/float/string）
  - 全局变量 `var _config map[string]any` + `var _configLoaded bool`（对齐 Python `_config` / `_config_loaded`）
  - 提供 `SetValue(key, value)` / `Delete(key)` / `Snapshot() map[string]any` / `Restore(snap map[string]any)` / `Reload()`
- **不加载 .env 文件**：Go 项目的 API key 通过构造函数参数传入，不从 .env 读取

### 日志

- config 包有循环依赖风险（logger → config），对齐项目规则 3.4，使用 `log.Printf("[config] ...")` 代替 logger

### 测试

- config_test.go：测试 Load/Get/SetValue/Delete/Snapshot/Restore/Reload
- 覆盖率 ≥ 85%

## 一、OpenAILLMWrapper（llm_wrapper.go）

### 1.1 设计决策

| 决策 | 选择 | 原因 |
|------|------|------|
| 调用路径 | 直接调 `BaseModelClient.Invoke()` | 对齐 Python：Python 的 OpenAILLMWrapper 绕过 Model 门面直接调 `client.invoke()`，不经过回调装饰器 |
| 是否走 Model 门面 | 否 | Model.Invoke() 含 emit_before/transform/emit_after 回调，Python Wrapper 无此行为 |
| Client 创建 | 内部用 `model_clients.NewOpenAIModelClient()` | 对齐 Python 自建 client，但复用 Go 已有的构造函数，不重复造 HTTP 轮子 |
| newer model 判断 | 保留 | Python 的 `_is_newer_model` 逻辑（gpt-4/gpt-5/o1/o3 不传 max_tokens） |

### 1.2 结构体

```go
// OpenAILLMWrapper 包装 BaseModelClient 提供 LLMService 接口。
// 对齐 Python OpenAILLMWrapper。
type OpenAILLMWrapper struct {
    // modelName 模型名称
    modelName string
    // temperature 采样温度
    temperature float64
    // maxTokens 最大生成 token 数
    maxTokens int
    // isNewerModel 是否为较新模型（不传 max_tokens）
    isNewerModel bool
    // client 底层模型客户端（直接调用，不走 Model 门面回调）
    client model_clients.BaseModelClient
    // modelConfig 模型请求配置
    modelConfig *llmschema.ModelRequestConfig
    // clientConfig 模型客户端配置
    clientConfig *llmschema.ModelClientConfig
}
```

### 1.3 构造函数

```go
// NewOpenAILLMWrapper 创建 OpenAI LLM 适配器。
// 对齐 Python OpenAILLMWrapper.__init__(model_name, api_key, base_url, temperature, max_tokens)。
// 内部用 Go 已有的 model_clients.NewOpenAIModelClient() 创建 client，直接调 client.Invoke()，
// 不走 Model 门面（无回调装饰，与 Python 行为一致）。
func NewOpenAILLMWrapper(modelName string, apiKey string, baseURL string, temperature float64, maxTokens int) (*OpenAILLMWrapper, error)
```

### 1.4 实现的方法

| 方法 | 对齐 Python | 说明 |
|------|------------|------|
| `Generate(ctx, prompt, ...opts)` | `async_generate(prompt, system_prompt?, temperature?, max_tokens?)` | 构建 messages → `client.Invoke()` → 提取 `content` |
| `GenerateWithMessages(ctx, messages, ...opts)` | `async_generate_with_messages(messages, temperature?, max_tokens?)` | 直接传 messages 列表 |

### 1.5 newer model 逻辑

```go
// isNewerModel 判断：model_name 包含 gpt-4/gpt-5/o1/o3 时为 true。
// 对齐 Python OpenAILLMWrapper._is_newer_model。
// 较新模型使用 max_completion_tokens 而非 max_tokens，Go SDK 已内置此判断，
// Wrapper 层只需不在 InvokeOption 中传 MaxTokens 即可。
modelLower := strings.ToLower(modelName)
isNewerModel = strings.Contains(modelLower, "gpt-4") ||
    strings.Contains(modelLower, "gpt-5") ||
    strings.Contains(modelLower, "o1") ||
    strings.Contains(modelLower, "o3")
```

### 1.6 错误处理

对齐 Python 的 `raise_error(StatusCode.TOOLCHAIN_EVOLVING_MEMORY_*, ...)`：
- 配置无效（API key 缺失）→ `exception.NewBaseError(StatusToolchainEvolvingMemoryConfigInvalid, ...)`
- LLM 调用失败 → `exception.NewBaseError(StatusToolchainEvolvingMemoryLLMGenerationExecutionError, ...)`

### 1.7 日志

对齐 Python：
- 初始化：`logger.Info("Initialized OpenAI LLM with model: %s", model_name)` → Go: `logger.Info(logComponent).Str("model_name", modelName).Msg("Initialized OpenAI LLM")`
- 调用完成：`logger.debug("LLM generated %s characters", len(content))` → Go: `logger.Debug(logComponent).Int("char_count", len(content)).Msg("LLM generated")`
- 调用失败：`logger.error("LLM generation failed: %s", e)` → Go: `logger.Error(logComponent).Err(err).Msg("LLM generation failed")`

## 二、OpenAIEmbeddingWrapper（embedding_wrapper.go）

### 2.1 设计决策

| 决策 | 选择 | 原因 |
|------|------|------|
| Client 创建 | 内部用 Go 已有的 `OpenAIEmbedding` | 对齐 Python 自建 `CoreOpenAIEmbedding`，复用 Go 已有构造函数 |
| 接口映射 | `EmbedQuery → Embed`，`EmbedDocuments → EmbedBatch` | 方法名不同但语义一致 |

### 2.2 结构体

```go
// OpenAIEmbeddingWrapper 包装 OpenAIEmbedding 提供 EmbeddingService 接口。
// 对齐 Python OpenAIEmbeddingWrapper。
type OpenAIEmbeddingWrapper struct {
    // modelName 模型名称
    modelName string
    // client 底层 embedding 客户端
    client embedding.BaseEmbedding
}
```

### 2.3 构造函数

```go
// NewOpenAIEmbeddingWrapper 创建 OpenAI Embedding 适配器。
// 对齐 Python OpenAIEmbeddingWrapper.__init__(model_name, api_key, base_url)。
// 内部用 Go 已有的 OpenAIEmbedding 构造，复用 HTTP 客户端和 API 调用逻辑。
func NewOpenAIEmbeddingWrapper(modelName string, apiKey string, baseURL string) (*OpenAIEmbeddingWrapper, error)
```

### 2.4 实现的方法

| 方法 | 对齐 Python | 说明 |
|------|------------|------|
| `Embed(ctx, text)` | `async_embed(text)` | 调 `client.EmbedQuery(ctx, text)` |
| `EmbedBatch(ctx, texts)` | `async_embed_batch(texts)` | 调 `client.EmbedDocuments(ctx, texts)` |

### 2.5 错误处理

- 配置无效（API key 缺失）→ `exception.NewBaseError(StatusToolchainEvolvingMemoryConfigInvalid, ...)`
- Embedding 调用失败 → `exception.NewBaseError(StatusToolchainEvolvingMemoryEmbeddingExecutionError, ...)`

### 2.6 日志

对齐 Python：
- 初始化：`logger.Info("Initialized OpenAI Embedding with model: %s", model_name)`
- 单文本：`logger.debug("Generated embedding of dimension %s", len(embedding))`
- 批量：`logger.debug("Generated %s embeddings", len(embeddings))`
- 失败：`logger.error("Embedding generation failed: %s", e)` / `logger.error("Batch embedding generation failed: %s", e)`

## 三、TaskMemoryService（task_memory_service.go）

### 3.1 AddMemoryRequest

```go
// AddMemoryRequest 手动添加记忆请求。对齐 Python AddMemoryRequest。
type AddMemoryRequest struct {
    // Content 记忆内容（必填）
    Content string
    // Query 用于 embedding 的查询（仅 ReasoningBank）
    Query *string
    // WhenToUse 何时使用此记忆（仅 ReMe/RefCon/DivCon）
    WhenToUse *string
    // Title 记忆标题（仅 ReasoningBank）
    Title *string
    // Description 记忆描述（仅 ReasoningBank）
    Description *string
    // Section 记忆分类（仅 ACE），默认 "general"
    Section string
    // Label 记忆标签（仅 ReasoningBank）
    Label *string
}
```

### 3.2 结构体

```go
// TaskMemoryService 任务记忆服务，提供记忆检索/总结/管理的统一入口。
// 对齐 Python TaskMemoryService。
type TaskMemoryService struct {
    // serviceContext 共享服务上下文
    serviceContext *cecontext.ServiceContext
    // llm LLM 服务
    llm *OpenAILLMWrapper
    // embedding Embedding 服务
    embedding *OpenAIEmbeddingWrapper
    // vectorStore 向量存储
    vectorStore cecontext.VectorStoreService
    // retrievalAlgorithm 检索算法名称（ACE/ReasoningBank/ReMe/RefCon/DivCon）
    retrievalAlgorithm string
    // summaryAlgorithm 总结算法名称
    summaryAlgorithm string
    // retrieveFlow 检索管线
    retrieveFlow op.Op
    // summaryFlow 总结管线
    summaryFlow op.Op
    // persistType 持久化类型（nil=关闭，"json"/"milvus"/"auto"）
    persistType *string
    // persistPath JSON 持久化路径模板
    persistPath string
    // milvusHost Milvus 主机
    milvusHost string
    // milvusPort Milvus 端口
    milvusPort int
    // milvusCollection Milvus 集合名
    milvusCollection string
    // persistenceHelper 持久化助手
    persistenceHelper *persistence.MemoryPersistenceHelper
}
```

### 3.3 构造函数

**主构造**（对齐 Python，自建 Wrapper）：

```go
// NewTaskMemoryService 创建任务记忆服务。
// 对齐 Python TaskMemoryService.__init__(llm_model, embedding_model, api_key,
//     retrieval_algo, summary_algo, config_path, persist_type, persist_path, ...)。
// 内部创建 OpenAILLMWrapper + OpenAIEmbeddingWrapper，注册到 ServiceContext。
func NewTaskMemoryService(
    llmModel string, embeddingModel string, apiKey string, apiBase string,
    retrievalAlgo string, summaryAlgo string,
    persistType *string, persistPath string,
    milvusHost string, milvusPort int, milvusCollection string,
) (*TaskMemoryService, error)
```

**辅助构造**（方便测试注入）：

```go
// NewTaskMemoryServiceWithServices 使用已构造好的 LLM/Embedding 服务创建任务记忆服务。
// 方便测试时注入 mock，也方便复用已有的服务实例。
func NewTaskMemoryServiceWithServices(
    llm cecontext.LLMService, emb cecontext.EmbeddingService,
    vectorStore cecontext.VectorStoreService,
    retrievalAlgo string, summaryAlgo string,
    persistType *string, persistPath string,
    milvusHost string, milvusPort int, milvusCollection string,
) (*TaskMemoryService, error)
```

### 3.4 核心方法

| 方法 | 对齐 Python | 说明 |
|------|------------|------|
| `Retrieve(ctx, userID, query)` | `async retrieve(user_id, query, **kwargs)` | 创建 RuntimeContext → 执行 retrieveFlow → 按算法格式化结果 → 返回 `RetrieveResponse` |
| `Summarize(ctx, userID, matts, query, trajectories)` | `async summarize(user_id, matts, query, trajectories, **kwargs)` | 创建 RuntimeContext → 执行 summaryFlow → 返回 `SummarizeResponse` |
| `AddMemory(ctx, userID, req)` | `async add_memory(user_id, request)` | 根据 summaryAlgorithm 创建对应类型 memory → embed → upsert → 持久化 |
| `LoadMemories(ctx, userID)` | `load_memories(user_id)` | 从持久化后端加载 → 插入 vectorStore |
| `Reconfigure(algorithm)` | `reconfigure(algorithm)` | 切换算法 → 重建两条管线 |

### 3.5 只读属性

| 方法 | 对齐 Python | 说明 |
|------|------------|------|
| `PersistType()` | `@property persist_type` | 返回 `*string`，nil 表示关闭 |
| `PersistPath()` | `@property persist_path` | 返回路径模板 |
| `MilvusHost()` | `@property milvus_host` | 返回主机名 |
| `MilvusPort()` | `@property milvus_port` | 返回端口 |
| `MilvusCollection()` | `@property milvus_collection` | 返回集合名 |
| `PersistenceHelper()` | `@property persistence_helper` | 返回 helper，nil 表示关闭 |

### 3.6 _createRetrieveFlow

对齐 Python `_create_retrieve_flow`，按 retrievalAlgorithm 组装管线：

| 算法 | 管线 |
|------|------|
| ACE | `ACERecallMemoryOp()` |
| ReasoningBank | `RBRecallMemoryOp(topK)` |
| ReMe | `ReMeRecallMemoryOp(topk) >> ReMeRerankMemoryOp(llm, topk) >> ReMeRewriteMemoryOp(llm)` |
| RefCon/DivCon | 同 ReMe 但用硬编码参数（topk=10, rerank_topk=5） |

### 3.7 _createSummaryFlow

对齐 Python `_create_summary_flow`，按 summaryAlgorithm 组装管线：

| 算法 | 管线 |
|------|------|
| ACE | `ACELoadPlaybookOp() >> (ACEReflectOp() | ACEParallelReflectOp()) >> (ACECurateOp() | ACEParallelCurateOp()) >> ACEApplyDeltaOp(maxBullets) >> [ACEPersistMemoryOp]` |
| ReasoningBank | `(RBSummarizeMemoryOp() | RBSummarizeMemoryParallelOp()) >> RBUpdateVectorStoreOp() >> [RBPersistMemoryOp]` |
| ReMe | `ReMeTrajectoryPreprocessOp() >> (ReMeSuccessExtractionOp() | ReMeFailureExtractionOp() | ReMeComparativeExtractionOp()) >> ReMeMemoryValidationOp() >> ReMeMemoryDeduplicationOp() >> ReMeUpdateVectorStoreOp() >> [ReMePersistMemoryOp]` |
| RefCon/DivCon | `ReMeTrajectoryPreprocessOp() >> ReMeComparativeAllExtractionOp() >> ReMeMemoryValidationOp(use=false) >> ReMeMemoryDeduplicationOp() >> ReMeUpdateVectorStoreOp() >> [ReMePersistMemoryOp]` |

`[XxxPersistMemoryOp]` 表示当 `persistType` 非空时追加，否则省略。

### 3.8 normalizeAlgoName

对齐 Python `normalize_algo_name`：

```go
var algoNameMap = map[string]string{
    "RB":            "ReasoningBank",
    "REASONINGBANK": "ReasoningBank",
    "REME":          "ReMe",
    "ACE":           "ACE",
    "REFCON":        "RefCon",
    "DIVCON":        "DivCon",
}
```

非法算法名返回 error（对齐 Python 的 `raise_error(StatusCode.TOOLCHAIN_EVOLVING_MEMORY_CONFIG_INVALID, ...)`）。

### 3.9 日志

对齐 Python TaskMemoryService 的所有日志调用：
- 初始化：`logger.Info("Initializing TaskMemoryService...")` + `logger.Info("Configuration: llm_model=%s, embedding_model=%s", ...)`
- 算法选择：`logger.Info("Selected algorithms - Retrieval: %s, Summary: %s", ...)`
- 持久化：`logger.Info("Memory persistence enabled: type=%s, path=%s", ...)`
- 初始化完成：`logger.Info("TaskMemoryService initialized successfully with retrieval=%s, summary=%s", ...)`
- 初始化失败：`logger.Error("TaskMemoryService initialization failed: %s", e)`
- reconfigure：`logger.Info("TaskMemoryService reconfigured: retrieval=%s, summary=%s", ...)`
- retrieve：`logger.Info("Retrieving task memory for user=%s, query='%s...'", ...)` + `logger.Info("Retrieved memories:\n%s\nUsing %s memories", ...)`
- summarize：`logger.Info("Summarizing %s trajectories for user=%s", ...)`
- add_memory：`logger.Info("Adding manual %s memory for user=%s", ...)` + `logger.Info("Added %s memory: %s", ...)`
- load_memories：`logger.Info("Loaded %d memories into vector store (algo=%s)", ...)` + `logger.Warning("Failed to load node %s: %s", ...)`

## 四、SummarizeTrajectories 补全（trajectory_generator.go）

### 4.1 当前状态

当前为 stub：`return nil, fmt.Errorf("not implemented: depends on TaskMemoryService (P6)")`

### 4.2 补全内容

对齐 Python `summarize_trajectories(memory_service, user_id, params)`：

1. **序列模式截断**：`matts_mode == "sequential"` 时只保留最后一条轨迹
2. **算法特定 kwargs 过滤**：
   - ReMe 系列：传 `score`
   - RB + USE_GOLDLABEL：从 score 推导 `label`（`s == 1.0 → true`）
   - ACE + USE_GROUNDTRUTH：传 `feedback` + `ground_truth`
3. **委托**：调用 `memoryService.Summarize(ctx, userID, matts, query, trajectories, ...extraKwargs)`
4. **签名变更**：`memoryService any` → `*TaskMemoryService`

### 4.3 config 读取

对齐 Python 中的 `memory_config.get(...)` 调用。Go 侧需要新建 `core/config/config.go`（见§零），从配置中读取：
- `SUMMARY_ALGO` — 判断当前算法
- `USE_GOLDLABEL` — RB 是否推导 label
- `USE_GROUNDTRUTH` — ACE 是否传 ground_truth
- `TOPK_QUERY` / `TOPK_RETRIEVAL` / `TOPK_RERANK` / `LLM_RERANK` / `LLM_REWRITE` — ReMe 检索参数
- `USE_GROUNDTRUTH` / `MAX_PLAYBOOK_SIZE` — ACE 总结参数
- `EXTRACT_BEST_TRAJ` / `EXTRACT_WORST_TRAJ` / `EXTRACT_COMPARATIVE_TRAJ` / `MEMORY_VALIDATION` / `MEMORY_DEDUPLICATION` — ReMe 总结参数
- `MATTS_DEFAULT_K` / `MATTS_DEFAULT_MODE` — MaTTS 参数

### 4.4 日志

对齐 Python：
- 失败：`logger.Error("Failed to summarize trajectories: %s", exc)`

## 五、ContextEvolvingReActAgent（context_evolving_react_agent.go）

### 5.1 设计决策

| 决策 | 选择 | 原因 |
|------|------|------|
| 与 ReActAgent 的关系 | 嵌入 `*ReActAgent` + 重写 Invoke/Stream | Go 最接近 Python 继承的方式；所有未导出字段都有导出 getter，零影响 |
| 两层 Invoke | 外层路由 + 内层 `invokeWithMemory` | 对齐 Python `invoke` / `_invoke_with_memory` 的分层设计 |
| AgentFlowService | 实现 Execute() 接口 | runTrialsInner 通过此路径调用内层，避免递归；对齐 Python `getattr(agent, "_invoke_with_memory", agent.invoke)` |
| 外部持有类型 | `*ContextEvolvingReActAgent` 或 `BaseAgent`/`AgentFlowService` 接口 | 确保 Go 动态分发到重写版 Invoke |

### 5.2 MemoryAgentConfigInput

```go
// MemoryAgentConfigInput 记忆 Agent 配置输入。对齐 Python MemoryAgentConfigInput。
type MemoryAgentConfigInput struct {
    // ModelProvider 模型提供商
    ModelProvider string
    // APIKey API 密钥
    APIKey string
    // APIBase API 基地址
    APIBase string
    // ModelName 模型名称
    ModelName string
    // SystemPrompt 系统提示词
    SystemPrompt *string
    // MaxIterations 最大迭代次数
    MaxIterations int
}
```

### 5.3 结构体

```go
// ContextEvolvingReActAgent 带记忆检索能力的 ReActAgent。
// 嵌入 *ReActAgent 获得所有父类方法，重写 Invoke/Stream 添加记忆检索。
// 对齐 Python ContextEvolvingReActAgent(ReActAgent)。
type ContextEvolvingReActAgent struct {
    *ReActAgent  // 嵌入指针，对齐 Python 继承

    // memoryService 记忆服务
    memoryService *TaskMemoryService
    // userID 用户标识
    userID string
    // injectMemoriesInContext 是否将记忆注入上下文
    injectMemoriesInContext bool
    // autoSummarize 是否自动总结
    autoSummarize bool
    // autoSummarizeMattsMode 自动总结的 MaTTS 模式
    autoSummarizeMattsMode string
    // lastRetrievedQuery 缓存上次检索的 query
    lastRetrievedQuery string
    // lastRetrievalResult 缓存上次检索结果
    lastRetrievalResult map[string]any
}
```

### 5.4 构造函数

```go
// NewContextEvolvingReActAgent 创建带记忆检索能力的 ReActAgent。
// 对齐 Python ContextEvolvingReActAgent.__init__(card, user_id, memory_service, ...)。
func NewContextEvolvingReActAgent(
    card *agentschema.AgentCard,
    config *saconfig.ReActAgentConfig,
    userID string,
    memoryService *TaskMemoryService,
    injectMemoriesInContext bool,
    persistType *string, persistPath string,
    milvusHost string, milvusPort int, milvusCollection string,
    autoSummarize bool, autoSummarizeMattsMode string,
) *ContextEvolvingReActAgent
```

如果 `memoryService` 为 nil，内部自动创建（传入 persist 参数）。

初始化后调用 `memoryService.LoadMemories(ctx, userID)` 加载已有记忆。

### 5.5 两层 Invoke 设计

```
ContextEvolvingReActAgent
├── Invoke(ctx, inputs, opts...)          ← 外层路由（重写）
│   ├── inputs 含 matts_mode?
│   │   └── RunTrials(agent.Execute, ...)  ← 委托 MaTTS
│   └── 普通模式
│       └── invokeWithMemory(ctx, inputs, opts...)
├── invokeWithMemory(ctx, inputs, opts...) ← 内层（非导出）
│   ├── memoryService.Retrieve(ctx, userID, query)  ← 检索记忆
│   ├── 缓存检查（lastRetrievedQuery）
│   ├── 增强输入（inject memory_string）
│   └── a.ReActAgent.Invoke(ctx, augmentedInputs, opts...)  ← 调父类纯 ReAct 循环
└── Execute(ctx, query, sessionID)         ← 实现 AgentFlowService 接口
    └── invokeWithMemory(ctx, {query, sessionID})
```

### 5.6 Invoke 外层路由逻辑

对齐 Python `ContextEvolvingReActAgent.invoke`：

```go
func (a *ContextEvolvingReActAgent) Invoke(ctx context.Context, inputs map[string]any, opts ...interfaces.AgentOption) (map[string]any, error) {
    query, _ := inputs["query"].(string)
    if query == "" {
        return a.ReActAgent.Invoke(ctx, inputs, opts...)  // 无 query，走父类
    }

    mattsMode, hasMattMode := inputs["matts_mode"].(string)
    if hasMattMode {
        // MaTTS 模式：委托 run_trials
        groundTruth, _ := inputs["ground_truth"].(string)
        mattsK, _ := inputs["matts_k"].(int)
        return RunTrials(ctx, a, RunTrialsInput{
            Agent:        a,  // self，实现 AgentFlowService
            UserID:       a.userID,
            Question:     query,
            GroundTruth:  groundTruth,
            MattsK:       mattsK,
            MattsMode:    mattsMode,
        })
    }

    // 普通模式：检索记忆 + 调父类
    return a.invokeWithMemory(ctx, inputs, opts...)
}
```

### 5.7 invokeWithMemory 内层逻辑

对齐 Python `_invoke_with_memory`：

```go
func (a *ContextEvolvingReActAgent) invokeWithMemory(ctx context.Context, inputs map[string]any, opts ...interfaces.AgentOption) (map[string]any, error) {
    query, _ := inputs["query"].(string)
    retrievalQuery := query
    if rq, ok := inputs["retrieval_query"].(string); ok {
        retrievalQuery = rq
    }

    // 1. 检索记忆（带缓存）
    var memoryString string
    var memoriesUsed int
    if a.lastRetrievedQuery == retrievalQuery && a.lastRetrievalResult != nil {
        memoryString, _ = a.lastRetrievalResult["memory_string"].(string)
        memoriesUsed = len(a.lastRetrievalResult["retrieved_memory"].([]any))
    } else {
        result, err := a.memoryService.Retrieve(ctx, a.userID, retrievalQuery)
        if err != nil {
            logger.Error(logComponent).Err(err).Msg("Failed to retrieve memories")
        } else {
            a.lastRetrievedQuery = retrievalQuery
            a.lastRetrievalResult = result
            memoryString, _ = result["memory_string"].(string)
            // memoriesUsed = ...
        }
    }

    // 2. 增强输入
    augmentedInputs := copyMap(inputs)
    if memoriesUsed > 0 && memoryString != "" {
        if a.injectMemoriesInContext {
            memoryContext := fmt.Sprintf("Some Related Experience to help you complete the task:\n%s\n", memoryString)
            augmentedInputs["query"] = fmt.Sprintf("%s\n\n%s", memoryContext, query)
        } else {
            augmentedInputs["memory_context"] = memoryString
            augmentedInputs["memories_used"] = memoriesUsed
        }
    }

    // 3. 调父类原始 ReAct 循环（对齐 Python super().invoke()）
    result, err := a.ReActAgent.Invoke(ctx, augmentedInputs, opts...)
    if result != nil {
        result["memories_used"] = memoriesUsed
    }
    return result, err
}
```

### 5.8 Execute（AgentFlowService 接口）

```go
// Execute 实现 AgentFlowService 接口。
// runTrialsInner 通过此路径调用，走内层 invokeWithMemory 避免递归。
// 对齐 Python _run_trials_inner 中 getattr(agent, "_invoke_with_memory", agent.invoke)。
func (a *ContextEvolvingReActAgent) Execute(ctx context.Context, query string, sessionID string) (*cecontext.TrajectoryResult, error) {
    inputs := map[string]any{
        "query":         query,
        "retrieval_query": query,  // 自纠正模式传入原始问题
    }
    result, err := a.invokeWithMemory(ctx, inputs)
    if err != nil {
        return nil, err
    }
    answer, _ := result["output"].(string)
    return &cecontext.TrajectoryResult{
        Answer:     answer,
        Success:    true,
        Trajectory: formatTrajectoryFromResult(result),
    }, nil
}
```

### 5.9 AddTool / AddTools

对齐 Python：
```go
func (a *ContextEvolvingReActAgent) AddTool(tool ToolInterface) { ... }
func (a *ContextEvolvingReActAgent) AddTools(tools []ToolInterface) { ... }
```

通过 `a.AbilityManager()` (导出 getter) 访问。

### 5.10 autoConfigure

对齐 Python `_auto_configure`：从 config 读取 API_KEY/API_BASE/MODEL_NAME/MODEL_PROVIDER，
构建 `ReActAgentConfig`，调用 `a.Configure(ctx, config)`。

### 5.11 日志

对齐 Python ContextEvolvingReActAgent：
- 初始化：`logger.Info("ContextEvolvingReActAgent initialized for user=%s, inject_in_context=%s, auto_summarize=%s, persist_type=%s", ...)`
- 记忆缓存命中：`logger.Info("Reusing cached memory retrieval result")`
- 检索完成：`logger.Info("Retrieved %s memories for query", ...)`
- 检索失败：`logger.Error("Failed to retrieve memories: %s", e)`
- 无 query 警告：`logger.Warning("No query provided in inputs")`

## 六、回填项

### 6.1 P4 状态回填

IMPLEMENTATION_PLAN.md 9.82 行中 P4 状态从 `☐` 改为 `✅`。

### 6.2 doc.go 更新

- `service/doc.go`：文件目录段增加 `task_memory_service.go`、`llm_wrapper.go`、`embedding_wrapper.go`
- `context_evolver/doc.go`：文件目录段增加 `context_evolving_react_agent.go`

### 6.3 trajectory_generator.go 签名变更

`SummarizeTrajectories` 的 `memoryService any` 参数改为 `*TaskMemoryService`。

## 七、测试策略

### 7.1 OpenAILLMWrapper 测试（llm_wrapper_test.go）

- mock `BaseModelClient`：验证 messages 构建、newer model 判断、max_tokens 传递逻辑
- 测试 `Generate` 和 `GenerateWithMessages`
- 测试错误路径（API key 缺失、LLM 调用失败）

### 7.2 OpenAIEmbeddingWrapper 测试（embedding_wrapper_test.go）

- mock `BaseEmbedding`：验证 `EmbedQuery` → `Embed`、`EmbedDocuments` → `EmbedBatch` 的正确映射
- 测试空批量输入
- 测试错误路径

### 7.3 TaskMemoryService 测试（task_memory_service_test.go）

- 使用 `NewTaskMemoryServiceWithServices` 注入 mock LLM/Embedding/VectorStore
- 测试 `_createRetrieveFlow` / `_createSummaryFlow` 对各算法的管线组装
- 测试 `Retrieve`：验证 RuntimeContext 设置、管线执行、结果格式化
- 测试 `Summarize`：验证 RuntimeContext 设置、管线执行
- 测试 `AddMemory`：对 ACE/RB/ReMe 三种算法分别测试 memory 创建逻辑
- 测试 `LoadMemories`：验证 persistenceHelper 为 nil 时的 no-op 行为
- 测试 `Reconfigure`：验证管线重建
- 测试 `normalizeAlgoName`：正常值 + 非法值

### 7.4 SummarizeTrajectories 测试（trajectory_generator_test.go 更新）

- 测试序列模式截断
- 测试 ReMe 的 score 传递
- 测试 RB + USE_GOLDLABEL 的 label 推导
- 测试 ACE + USE_GROUNDTRUTH 的 ground_truth/feedback 传递

### 7.5 ContextEvolvingReActAgent 测试（context_evolving_react_agent_test.go）

- 测试普通模式 invoke：验证记忆检索 → 输入增强 → 父类调用的完整链路
- 测试 MaTTS 模式 invoke：验证路由到 RunTrials
- 测试 Execute：验证走内层 invokeWithMemory
- 测试记忆缓存命中
- 测试 injectMemoriesInContext = true/false 两种注入方式
- 测试 auto_summarize

### 7.6 覆盖率目标

所有新增文件测试覆盖率 ≥ 85%。
