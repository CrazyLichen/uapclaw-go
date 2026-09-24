# 9.82 P6 服务层+Agent集成 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 9.82 Context Evolver P6 服务层+Agent集成，包括 TaskMemoryService 核心编排器、OpenAILLMWrapper/OpenAIEmbeddingWrapper 适配器、SummarizeTrajectories 补全、ContextEvolvingReActAgent Agent 集成层，以及 P4 状态回填。

**Architecture:** 混合方案 A+B——结构上严格对齐 Python（每个类独立文件），实现上复用 Go 已有的 client 构造基础设施。OpenAILLMWrapper 直接调 BaseModelClient.Invoke()（无回调，对齐 Python）。ContextEvolvingReActAgent 嵌入 *ReActAgent + 两层 Invoke 设计。配置通过 TaskMemoryServiceConfig 结构体 + common/config 全局配置获取，不新建 config 包。

**Tech Stack:** Go 1.22+，已有 context_evolver P1-P5 基础设施，common/config 全局配置，common/exception 错误码已定义（174000-174014）

**Design Spec:** `docs/superpowers/specs/2027-12-30-context-evolver-9.82-p6-service-layer-design.md`

**Python Source:**
- `openjiuwen/extensions/context_evolver/service/task_memory_service.py`
- `openjiuwen/extensions/context_evolver/service/trajectory_generator.py`
- `openjiuwen/extensions/context_evolver/context_evolving_react_agent.py`

---

## File Structure

| Operation | Path | Responsibility |
|-----------|------|----------------|
| Create | `internal/agentcore/context_evolver/service/llm_wrapper.go` | OpenAILLMWrapper 适配器 |
| Create | `internal/agentcore/context_evolver/service/llm_wrapper_test.go` | OpenAILLMWrapper 测试 |
| Create | `internal/agentcore/context_evolver/service/embedding_wrapper.go` | OpenAIEmbeddingWrapper 适配器 |
| Create | `internal/agentcore/context_evolver/service/embedding_wrapper_test.go` | OpenAIEmbeddingWrapper 测试 |
| Create | `internal/agentcore/context_evolver/service/task_memory_service.go` | TaskMemoryService + AddMemoryRequest + TaskMemoryServiceConfig |
| Create | `internal/agentcore/context_evolver/service/task_memory_service_test.go` | TaskMemoryService 测试 |
| Modify | `internal/agentcore/context_evolver/service/trajectory_generator.go` | 补全 SummarizeTrajectories stub |
| Modify | `internal/agentcore/context_evolver/service/trajectory_generator_test.go` | 补全 SummarizeTrajectories 测试 |
| Create | `internal/agentcore/context_evolver/context_evolving_react_agent.go` | ContextEvolvingReActAgent + MemoryAgentConfigInput |
| Create | `internal/agentcore/context_evolver/context_evolving_react_agent_test.go` | ContextEvolvingReActAgent 测试 |
| Modify | `internal/agentcore/context_evolver/service/doc.go` | 回填文件目录 |
| Modify | `internal/agentcore/context_evolver/doc.go` | 回填文件目录 |
| Modify | `IMPLEMENTATION_PLAN.md` | P4 状态 ☐→✅ |

---

### Task 1: OpenAILLMWrapper 适配器

**Files:**
- Create: `internal/agentcore/context_evolver/service/llm_wrapper.go`
- Create: `internal/agentcore/context_evolver/service/llm_wrapper_test.go`

- [ ] **Step 1: 编写 OpenAILLMWrapper 测试**

创建 `llm_wrapper_test.go`，mock `BaseModelClient` 测试：
- `TestNewOpenAILLMWrapper` — 构造成功
- `TestNewOpenAILLMWrapper_无APIKey返回错误` — API key 缺失
- `TestOpenAILLMWrapper_Generate` — 基本调用 + 验证 messages 构建
- `TestOpenAILLMWrapper_Generate_新模型不传MaxTokens` — newer model 逻辑
- `TestOpenAILLMWrapper_Generate_旧模型传MaxTokens` — 旧模型传 max_tokens
- `TestOpenAILLMWrapper_GenerateWithMessages` — 直接传 messages
- `TestOpenAILLMWrapper_Generate_调用失败返回错误` — 错误路径

mock 实现：创建 `fakeModelClient` 结构体实现 `model_clients.BaseModelClient`，记录收到的 messages 和 opts，返回预设响应。

- [ ] **Step 2: 运行测试确认失败**

Run: `go test -run TestNewOpenAILLMWrapper ./internal/agentcore/context_evolver/service/ -v`
Expected: 编译失败（llm_wrapper.go 不存在）

- [ ] **Step 3: 实现 OpenAILLMWrapper**

创建 `llm_wrapper.go`，包含：

```go
package service

// 导入：
// - model_clients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
// - llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
// - cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/context"
// - "github.com/uapclaw/uapclaw-go/internal/common/exception"
// - "github.com/uapclaw/uapclaw-go/internal/common/logger"

// OpenAILLMWrapper 结构体（对齐设计文档 §1.2）
// 字段：modelName, temperature, maxTokens, isNewerModel, client (BaseModelClient), modelConfig, clientConfig

// NewOpenAILLMWrapper 构造函数：
// 1. 校验 apiKey 非空（空则返回 exception.NewBaseError(StatusToolchainEvolvingMemoryConfigInvalid, ...)）
// 2. 设置 baseURL 默认值 "https://api.openai.com/v1"
// 3. 构建 ModelClientConfig{ClientProvider:"OpenAI", APIKey, APIBase, VerifySSL:false}
// 4. 构建 ModelRequestConfig{ModelName, Temperature}
// 5. 调 model_clients.NewOpenAIModelClient(modelConfig, clientConfig) 创建 client
// 6. 判断 isNewerModel（modelLower 包含 gpt-4/gpt-5/o1/o3）
// 7. 日志：logger.Info(logComponent).Str("model_name", modelName).Msg("Initialized OpenAI LLM")

// Generate(ctx, prompt, ...opts) 实现 LLMService 接口：
// 1. 从 GenerateOption 提取 SystemPrompt/Temperature/MaxTokens
// 2. 构建 MessagesParam：如果 SystemPrompt 非空加 system message，加 user message
// 3. 构建 InvokeOption：WithTemperature（优先 opts 传入），如果是旧模型 WithMaxTokens
// 4. 调 client.Invoke(ctx, messages, invokeOpts...)
// 5. 提取 response.Content
// 6. 日志：Debug "LLM generated" + char_count
// 7. 错误映射：exception.NewBaseError(StatusToolchainEvolvingMemoryLLMGenerationExecutionError, ...)

// GenerateWithMessages(ctx, messages, ...opts)：
// 1. 构建 []llmschema.MessageFromMap(messages)
// 2. 同 Generate 步骤 3-7
```

严格按项目编码规范：中文注释、结构体→常量→导出函数→非导出函数排列顺序、分隔注释。

- [ ] **Step 4: 运行测试确认通过**

Run: `go test -cover ./internal/agentcore/context_evolver/service/ -v -run TestOpenAILLMWrapper`
Expected: PASS，覆盖率 ≥ 85%

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/context_evolver/service/llm_wrapper.go internal/agentcore/context_evolver/service/llm_wrapper_test.go
git commit -m "feat(9.82-p6): 实现 OpenAILLMWrapper 适配器（直接调 BaseModelClient，无回调，对齐 Python）"
```

---

### Task 2: OpenAIEmbeddingWrapper 适配器

**Files:**
- Create: `internal/agentcore/context_evolver/service/embedding_wrapper.go`
- Create: `internal/agentcore/context_evolver/service/embedding_wrapper_test.go`

- [ ] **Step 1: 编写 OpenAIEmbeddingWrapper 测试**

创建 `embedding_wrapper_test.go`，mock `BaseEmbedding` 测试：
- `TestNewOpenAIEmbeddingWrapper` — 构造成功
- `TestNewOpenAIEmbeddingWrapper_无APIKey返回错误`
- `TestOpenAIEmbeddingWrapper_Embed` — 单文本 EmbedQuery→Embed 映射
- `TestOpenAIEmbeddingWrapper_EmbedBatch` — 批量 EmbedDocuments→EmbedBatch 映射
- `TestOpenAIEmbeddingWrapper_EmbedBatch_空列表返回空` — 空 texts
- `TestOpenAIEmbeddingWrapper_Embed_调用失败返回错误`

mock 实现：创建 `fakeEmbeddingClient` 实现 `embedding.BaseEmbedding`，记录调用参数，返回预设向量。

- [ ] **Step 2: 运行测试确认失败**

Run: `go test -run TestNewOpenAIEmbeddingWrapper ./internal/agentcore/context_evolver/service/ -v`
Expected: 编译失败

- [ ] **Step 3: 实现 OpenAIEmbeddingWrapper**

创建 `embedding_wrapper.go`，包含：

```go
package service

// 导入：
// - "github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
// - cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/context"
// - "github.com/uapclaw/uapclaw-go/internal/common/exception"
// - "github.com/uapclaw/uapclaw-go/internal/common/logger"

// OpenAIEmbeddingWrapper 结构体（对齐设计文档 §2.2）
// 字段：modelName, client (embedding.BaseEmbedding)

// NewOpenAIEmbeddingWrapper 构造函数：
// 1. 校验 apiKey 非空
// 2. 构建 EmbeddingConfig{ModelName, BaseURL, APIKey}
// 3. 调 embedding.NewOpenAIEmbedding(config) 创建 client
// 4. 日志：logger.Info(logComponent).Str("model_name", modelName).Msg("Initialized OpenAI Embedding")

// Embed(ctx, text) 实现 EmbeddingService 接口：
// 1. 调 client.EmbedQuery(ctx, text)
// 2. 日志：Debug "Generated embedding" + dimension
// 3. 错误映射：exception.NewBaseError(StatusToolchainEvolvingMemoryEmbeddingExecutionError, ...)

// EmbedBatch(ctx, texts) 实现 EmbeddingService 接口：
// 1. 如果 texts 为空返回空切片
// 2. 调 client.EmbedDocuments(ctx, texts)
// 3. 日志：Debug "Generated embeddings" + count
// 4. 错误映射同上
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test -cover ./internal/agentcore/context_evolver/service/ -v -run TestOpenAIEmbeddingWrapper`
Expected: PASS，覆盖率 ≥ 85%

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/context_evolver/service/embedding_wrapper.go internal/agentcore/context_evolver/service/embedding_wrapper_test.go
git commit -m "feat(9.82-p6): 实现 OpenAIEmbeddingWrapper 适配器（EmbedQuery→Embed, EmbedDocuments→EmbedBatch）"
```

---

### Task 3: TaskMemoryService 核心编排器

**Files:**
- Create: `internal/agentcore/context_evolver/service/task_memory_service.go`
- Create: `internal/agentcore/context_evolver/service/task_memory_service_test.go`

- [ ] **Step 1: 编写 TaskMemoryService 测试**

创建 `task_memory_service_test.go`，使用 `NewTaskMemoryServiceWithServices` 注入 mock：
- `TestNormalizeAlgoName` — 正常值 (ACE/RB/REME/REFCON/DIVCON) + 非法值
- `TestNewTaskMemoryServiceWithServices` — 辅助构造成功
- `TestNewTaskMemoryServiceWithServices_非法算法返回错误`
- `TestCreateRetrieveFlow` — 对 5 种算法验证管线组装（验证 flow 类型/包含的 Op 类型）
- `TestCreateSummaryFlow` — 对 5 种算法验证管线组装
- `TestCreateSummaryFlow_带持久化追加PersistOp` — persistType 非空时末尾追加 PersistMemoryOp
- `TestRetrieve` — 创建 RuntimeContext → 执行 flow → 格式化结果
- `TestSummarize` — 创建 RuntimeContext → 执行 flow
- `TestAddMemory_ReasoningBank` — RB memory 创建 + embed + upsert
- `TestAddMemory_ReMe` — ReMe memory 创建
- `TestAddMemory_ACE` — ACE memory 创建
- `TestLoadMemories_无持久化NoOp` — persistenceHelper 为 nil 直接返回
- `TestReconfigure` — 验证管线重建

mock 策略：mock LLMService（Generate 返回预设 JSON）、mock EmbeddingService（返回预设向量）、mock VectorStoreService（记录 upsert/search 调用）。

- [ ] **Step 2: 运行测试确认失败**

Run: `go test -run TestNormalizeAlgoName ./internal/agentcore/context_evolver/service/ -v`
Expected: 编译失败

- [ ] **Step 3: 实现 TaskMemoryService**

创建 `task_memory_service.go`，包含：

```go
package service

// 导入：
// - cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/context"
// - op "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/op"
// - cepersistence "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/persistence"
// - ceschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/schema"
// - ACERecallMemoryOp, ACELoadPlaybookOp 等 (各算法 Op)
// - "github.com/uapclaw/uapclaw-go/internal/common/exception"
// - "github.com/uapclaw/uapclaw-go/internal/common/logger"

// ──────────────────────────── 结构体 ────────────────────────────

// AddMemoryRequest 结构体（对齐设计文档 §3.1）
// 字段：Content, Query *string, WhenToUse *string, Title *string, Description *string, Section string, Label *string

// TaskMemoryServiceConfig 结构体（对齐设计文档 §零）
// 字段：LLMModel, EmbeddingModel, APIKey, APIBase, RetrievalAlgo, SummaryAlgo,
//       PersistType *string, PersistPath, MilvusHost, MilvusPort, MilvusCollection,
//       TopKRetrieval, TopKRerank, LLMRerank, LLMRewrite, TopKQuery,
//       UseGroundTruth, MaxPlaybookSize,
//       ExtractBestTraj, ExtractWorstTraj, ExtractComparativeTraj, MemoryValidation, MemoryDeduplication

// TaskMemoryService 结构体（对齐设计文档 §3.2）
// 字段：serviceContext, llm *OpenAILLMWrapper, embedding *OpenAIEmbeddingWrapper,
//       vectorStore, retrievalAlgorithm, summaryAlgorithm,
//       retrieveFlow op.Op, summaryFlow op.Op,
//       persistType *string, persistPath, milvusHost, milvusPort, milvusCollection,
//       persistenceHelper *cepersistence.MemoryPersistenceHelper

// ──────────────────────────── 常量 ────────────────────────────

// algoNameMap 对齐 Python normalize_algo_name 中的映射

// ──────────────────────────── 导出函数 ────────────────────────────

// defaultTaskMemoryServiceConfig 返回带默认值的配置

// NewTaskMemoryService 从配置创建（自建 Wrapper）
// 1. 创建 OpenAILLMWrapper + OpenAIEmbeddingWrapper
// 2. 创建 MemoryVectorStore
// 3. 注册到 ServiceContext
// 4. normalizeAlgoName
// 5. 构建 persistenceHelper
// 6. _createRetrieveFlow + _createSummaryFlow
// 7. 日志对齐 Python

// NewTaskMemoryServiceWithServices 辅助构造（注入已有服务）

// NormalizeAlgoName 导出的算法名规范化（对齐 Python @staticmethod）

// Retrieve(ctx, userID, query) — 对齐 Python retrieve()
// 1. 创建 RuntimeContext，设置 user_id, query
// 2. 执行 retrieveFlow
// 3. 从 context 取 retrieved_memories
// 4. 按 retrievalAlgorithm 格式化 memory_string
// 5. 构建 RetrieveResponse 返回

// Summarize(ctx, userID, matts, query, trajectories) — 对齐 Python summarize()
// 1. 创建 RuntimeContext，设置 user_id, matts, query, trajectories
// 2. 执行 summaryFlow
// 3. 从 context 取 memories
// 4. 构建 SummarizeResponse 返回

// AddMemory(ctx, userID, req) — 对齐 Python add_memory()
// 1. 按 summaryAlgorithm 分支创建 memory
//    - ReasoningBank: 校验 title/description/content，创建 ReasoningBankMemory
//    - ReMe/RefCon/DivCon: 校验 when_to_use，创建 ReMeMemory
//    - ACE: 校验 section/content，创建 ACEMemory
// 2. 调 memory.ToVectorNode()
// 3. 调 embedding.Embed(ctx, node.Content) 获取向量
// 4. 调 vectorStore.Upsert(ctx, node)
// 5. 如果 persistenceHelper 非空，调 helper.Save()
// 6. 返回 {status, memory_id, user_id, algorithm}

// LoadMemories(ctx, userID) — 对齐 Python load_memories()
// 1. 如果 persistenceHelper 为 nil 直接返回
// 2. 从 helper 加载数据
// 3. 逐条 VectorNode.FromDict → vectorStore.LoadNode
// 4. 日志对齐

// Reconfigure(algorithm) — 对齐 Python reconfigure()
// 1. normalizeAlgoName
// 2. 更新 retrievalAlgorithm + summaryAlgorithm
// 3. 重建 retrieveFlow + summaryFlow

// PersistType() / PersistPath() / MilvusHost() / MilvusPort() / MilvusCollection() / PersistenceHelper()
// 只读属性访问器

// ──────────────────────────── 非导出函数 ────────────────────────────

// createRetrieveFlow — 按 retrievalAlgorithm 组装管线（对齐设计文档 §3.6）
// ACE: NewACERecallMemoryOp(sc)
// ReasoningBank: NewRBRecallMemoryOp(sc, cfg.TopKQuery)
// ReMe: NewReMeRecallMemoryOp(sc, cfg.TopKRetrieval) >> NewReMeRerankMemoryOp(sc, cfg.LLMRerank, cfg.TopKRerank) >> NewReMeRewriteMemoryOp(sc, cfg.LLMRewrite)
// RefCon/DivCon: 同 ReMe 但硬编码 topk=10, rerank_topk=5

// createSummaryFlow — 按 summaryAlgorithm 组装管线（对齐设计文档 §3.7）
// 各算法管线组合见设计文档

// persistOp — 辅助方法：如果 persistType 非空返回对应算法的 PersistMemoryOp，否则返回 nil
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test -cover ./internal/agentcore/context_evolver/service/ -v -run TestTaskMemoryService -run TestNormalizeAlgoName`
Expected: PASS，覆盖率 ≥ 85%

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/context_evolver/service/task_memory_service.go internal/agentcore/context_evolver/service/task_memory_service_test.go
git commit -m "feat(9.82-p6): 实现 TaskMemoryService 核心编排器（双构造+五方法+管线组装）"
```

---

### Task 4: SummarizeTrajectories 补全

**Files:**
- Modify: `internal/agentcore/context_evolver/service/trajectory_generator.go`
- Modify: `internal/agentcore/context_evolver/service/trajectory_generator_test.go`

- [ ] **Step 1: 编写 SummarizeTrajectories 测试**

在 `trajectory_generator_test.go` 中新增：
- `TestSummarizeTrajectories_序列模式截断` — matts_mode="sequential" 时只保留最后一条轨迹
- `TestSummarizeTrajectories_ReMe传Score` — summary_algo=reme 时传 score
- `TestSummarizeTrajectories_RB推导Label` — use_goldlabel=true 时从 score 推导 label
- `TestSummarizeTrajectories_ACE传GroundTruth` — use_groundtruth=true 时传 feedback + ground_truth

使用 mock TaskMemoryService（或 NewTaskMemoryServiceWithServices 注入）。

- [ ] **Step 2: 运行测试确认失败**

Run: `go test -run TestSummarizeTrajectories ./internal/agentcore/context_evolver/service/ -v`
Expected: FAIL（当前 stub 返回 error）

- [ ] **Step 3: 补全 SummarizeTrajectories 实现**

修改 `trajectory_generator.go`：

1. **签名变更**：`memoryService any` → `memoryService *TaskMemoryService`
2. **实现**（对齐 Python `summarize_trajectories`）：
   - 序列模式截断：如果 mattsMode == "sequential"，trajectories/feedbacks/scores 只保留最后一个
   - 构建 extraKwargs map：
     - 从 `common/config` 读取 SUMMARY_ALGO 判断算法类型
     - ReMe 系列：传 `score` 到 RuntimeContext
     - RB + USE_GOLDLABEL：从 score 推导 `label`（`s == 1 → true`）
     - ACE + USE_GROUNDTRUTH：传 `feedback` + `ground_truth`
   - 委托 `memoryService.Summarize(ctx, userID, matts, query, trajectories, extraKwargs...)`
   - 错误时日志 + 返回 nil, err

- [ ] **Step 4: 运行测试确认通过**

Run: `go test -cover ./internal/agentcore/context_evolver/service/ -v -run TestSummarizeTrajectories`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/context_evolver/service/trajectory_generator.go internal/agentcore/context_evolver/service/trajectory_generator_test.go
git commit -m "feat(9.82-p6): 补全 SummarizeTrajectories（序列截断+算法特定kwargs+签名变更）"
```

---

### Task 5: ContextEvolvingReActAgent

**Files:**
- Create: `internal/agentcore/context_evolver/context_evolving_react_agent.go`
- Create: `internal/agentcore/context_evolver/context_evolving_react_agent_test.go`

- [ ] **Step 1: 编写 ContextEvolvingReActAgent 测试**

创建 `context_evolving_react_agent_test.go`：
- `TestNewContextEvolvingReActAgent` — 构造成功
- `TestNewContextEvolvingReActAgent_自建MemoryService` — memoryService 为 nil 时自动创建
- `TestInvokeWithMemory_普通模式` — 验证记忆检索 → 输入增强 → 父类调用
- `TestInvokeWithMemory_MaTTS模式路由` — inputs 含 matts_mode 时路由到 RunTrials
- `TestInvokeWithMemory_记忆缓存命中` — 同一 query 第二次调用使用缓存
- `TestInvokeWithMemory_注入方式切换` — injectMemoriesInContext=true/false 两种增强
- `TestExecute_AgentFlowService` — 实现 AgentFlowService 接口，走 invokeWithMemory

mock 策略：mock TaskMemoryService 的 Retrieve 方法返回预设 memory_string；mock ReActAgent 通过嵌入的 ReActAgent 构造。

- [ ] **Step 2: 运行测试确认失败**

Run: `go test -run TestNewContextEvolvingReActAgent ./internal/agentcore/context_evolver/ -v`
Expected: 编译失败

- [ ] **Step 3: 实现 ContextEvolvingReActAgent**

创建 `context_evolving_react_agent.go`，包含：

```go
package context_evolver

// 导入：
// - "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/agents"
// - agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
// - saconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/config"
// - cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/context"
// - "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/service"
// - "github.com/uapclaw/uapclaw-go/internal/common/logger"

// ──────────────────────────── 结构体 ────────────────────────────

// MemoryAgentConfigInput 结构体（对齐设计文档 §5.2）

// ContextEvolvingReActAgent 结构体（对齐设计文档 §5.3）
// 嵌入 *agents.ReActAgent
// 新增字段：memoryService, userID, injectMemoriesInContext, autoSummarize, autoSummarizeMattsMode, lastRetrievedQuery, lastRetrievalResult

// ──────────────────────────── 常量 ────────────────────────────

// logComponent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// NewContextEvolvingReActAgent 构造函数（对齐设计文档 §5.4）
// 1. 如果 memoryService 为 nil，用 persist 参数创建 TaskMemoryService
// 2. 调 memoryService.LoadMemories(ctx, userID)
// 3. autoConfigure（从 config 读取 API_KEY 等）
// 4. 日志对齐

// Invoke 重写（两层设计，对齐设计文档 §5.6）：
// 1. 提取 query
// 2. 如果 query 为空 → a.ReActAgent.Invoke(ctx, inputs, opts...)
// 3. 如果 inputs 含 matts_mode → 路由到 service.RunTrials(ctx, a, ...)
// 4. 否则 → a.invokeWithMemory(ctx, inputs, opts...)

// Execute 实现 AgentFlowService 接口（对齐设计文档 §5.8）：
// 构建 inputs map → 调 a.invokeWithMemory(ctx, inputs) → 提取结果构建 TrajectoryResult

// AddTool / AddTools（对齐设计文档 §5.9）

// ──────────────────────────── 非导出函数 ────────────────────────────

// invokeWithMemory 内层（对齐设计文档 §5.7）：
// 1. 提取 query 和 retrieval_query
// 2. 缓存检查
// 3. 调 memoryService.Retrieve(ctx, userID, retrievalQuery)
// 4. 增强输入（inject memory_string）
// 5. 调 a.ReActAgent.Invoke(ctx, augmentedInputs, opts...)
// 6. 在 result 中设置 memories_used

// autoConfigure（对齐设计文档 §5.10）：
// 从 common/config 读取 API_KEY/MODEL_PROVIDER/API_BASE/MODEL_NAME
// 构建 ReActAgentConfig → 调 a.Configure(ctx, config)

// copyInputs 辅助函数：深拷贝 map[string]any
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test -cover ./internal/agentcore/context_evolver/ -v -run TestContextEvolvingReActAgent`
Expected: PASS，覆盖率 ≥ 85%

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/context_evolver/context_evolving_react_agent.go internal/agentcore/context_evolver/context_evolving_react_agent_test.go
git commit -m "feat(9.82-p6): 实现 ContextEvolvingReActAgent（嵌入ReActAgent+两层Invoke+AgentFlowService）"
```

---

### Task 6: doc.go 回填 + IMPLEMENTATION_PLAN.md 更新

**Files:**
- Modify: `internal/agentcore/context_evolver/service/doc.go`
- Modify: `internal/agentcore/context_evolver/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 service/doc.go**

在文件目录段增加：
```
//	service/
//	├── doc.go                     # 包文档
//	├── llm_wrapper.go            # OpenAILLMWrapper 适配器
//	├── embedding_wrapper.go      # OpenAIEmbeddingWrapper 适配器
//	├── task_memory_service.go    # TaskMemoryService + AddMemoryRequest + TaskMemoryServiceConfig
//	└── trajectory_generator.go   # 轨迹生成和 MaTTS 试验函数
```

- [ ] **Step 2: 更新 context_evolver/doc.go**

在文件目录段增加 `context_evolving_react_agent.go` 条目。

- [ ] **Step 3: 更新 IMPLEMENTATION_PLAN.md**

在 9.82 行中：
- P4 状态从 `☐` 改为 `✅`（ReasoningBank算法 已完成，覆盖率 96.0%/89.1%）
- P6 状态从 `☐` 改为 `✅`

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/context_evolver/service/doc.go internal/agentcore/context_evolver/doc.go IMPLEMENTATION_PLAN.md
git commit -m "docs(9.82-p6): 回填 P4 状态为 ✅，更新 doc.go 文件目录，标记 P6 完成"
```

---

### Task 7: 全量编译 + 测试验证

- [ ] **Step 1: 全量编译**

Run: `pgrep -f 'go (build|test)' && pkill -f 'go (build|test)'; export GOPROXY=https://goproxy.cn,direct && go build ./...`
Expected: 编译成功

- [ ] **Step 2: 运行 context_evolver 全部测试**

Run: `export GOPROXY=https://goproxy.cn,direct && go test -cover ./internal/agentcore/context_evolver/... 2>&1`
Expected: 全部 PASS，各包覆盖率 ≥ 85%

- [ ] **Step 3: 检查是否有测试遗漏**

Run: `go test -cover ./internal/agentcore/context_evolver/service/ ./internal/agentcore/context_evolver/ -v 2>&1 | grep -E "coverage:|FAIL|PASS"`
Expected: 所有测试 PASS

- [ ] **Step 4: 最终提交**

如有任何修复：
```bash
git add -A && git commit -m "fix(9.82-p6): 修复全量编译/测试发现的问题"
```

---

## Self-Review

**1. Spec coverage:**

| Spec Section | Task |
|---|---|
| §零 配置获取策略 | Task 3 (TaskMemoryServiceConfig) |
| §一 OpenAILLMWrapper | Task 1 |
| §二 OpenAIEmbeddingWrapper | Task 2 |
| §三 TaskMemoryService | Task 3 |
| §四 SummarizeTrajectories | Task 4 |
| §五 ContextEvolvingReActAgent | Task 5 |
| §六 回填项 | Task 6 |
| §七 测试策略 | 覆盖在 Tasks 1-5 |

**2. Placeholder scan:** 无 TBD/TODO/占位符

**3. Type consistency:**
- `TaskMemoryServiceConfig` 在 Task 3 定义，在 Task 3/5 中使用 — 一致
- `AddMemoryRequest` 在 Task 3 定义 — 一致
- `*TaskMemoryService` 在 Task 3 定义，Task 4/5 使用 — 一致
- `cecontext.LLMService` / `cecontext.EmbeddingService` / `cecontext.AgentFlowService` — P1 已定义，Task 1/2/5 实现 — 一致
- `op.Op` 流水线接口 — P1 已定义，Task 3 使用 — 一致
