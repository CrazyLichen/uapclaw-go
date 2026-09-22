# 9.82 Context Evolver P3 ReMe 算法设计

## 概述

P3 实现 ReMe（Reflective Memory）算法——三条记忆管线中最先实现的一条。ReMe 从任务执行轨迹中提取蒸馏后的经验知识，以 `ReMeMemory` 形式写入向量库，并在后续任务中检索注入 Agent 上下文。

P3 同时对 `core/` 层做必要扩展：定义 `LLMService`/`EmbeddingService` 本地接口、新增 `OpBase` 结构体、`VectorStoreService` 补充 `GetAll` 方法。这些改动是 P4/P5 共享的基础设施。

### 在 Agent 会话中的流程位置

```
9.82 Context Evolver 整体架构

P1 ✅ 核心框架（RuntimeContext/ServiceContext/BaseOp/SequentialOp/ParallelOp + VectorNode + MemoryVectorStore + JSONFileConnector + MemoryPersistenceHelper）
P2 ✅ IO Schema（Trajectory/Memory + ACE/RB/ReMe 三系 Memory+Request+Response + 泛型 Response）
P3 ☐ ReMe 算法 ← 本次设计
     ├── core 层扩展：LLMService + EmbeddingService + OpBase + GetAll
     ├── retrieve/reme/：RecallMemoryOp → RerankMemoryOp → RewriteMemoryOp
     └── summary/reme/：TrajectoryPreprocessOp → (Success|Failure|Comparative|ComparativeAll)ExtractionOp → MemoryValidationOp → MemoryDeduplicationOp → UpdateVectorStoreOp → PersistMemoryOp
P4 ☐ ReasoningBank 算法
P5 ☐ ACE 算法
P6 ☐ 服务层 + Agent 集成（OpenAILLMWrapper/OpenAIEmbeddingWrapper/TaskMemoryService）
P7 ☐ ContextEvolutionRail + MilvusConnector + 工具层（→ 9.24 P6 回填）
```

### 作用

**Retrieve 流程**（任务开始时）：query → embedding 检索 → LLM 重排序 → LLM 改写 → 注入 Agent 上下文

**Summarize 流程**（任务完成后）：轨迹 → 按 score 分组 → LLM 提取经验 → LLM 校验质量 → embedding 去重 → 写入向量库 → 持久化

---

## 设计决策

以下 14 项决策在 brainstorming 阶段与用户逐个确认：

### D1: LLM/Embedding 接口在 P3 定义

在 `core/context/` 包定义 `LLMService` + `EmbeddingService` 本地接口，替换 `ServiceContext.LLM()`/`EmbeddingModel()` 返回的 `any`。P6 的 `OpenAILLMWrapper`/`OpenAIEmbeddingWrapper` 实现时自然满足。

**Why:** P3 有 8 个 Op 依赖 LLM、3 个依赖 Embedding，继续用 `any` 每个 Op 都要重复类型断言+错误处理。

### D2: 补上 ComparativeAllExtractionOp

与 Python 一比一对齐，IMPLEMENTATION_PLAN 原描述遗漏了 `ComparativeAllExtractionOp`。它与 `ComparativeExtractionOp` 是两种对比策略：

| Op | 对比范围 | 提示词 |
|---|---|---|
| `ComparativeExtractionOp` | 最高分 vs 最低分两条轨迹 | `comparative_memory_prompt`（higher_steps/lower_steps） |
| `ComparativeAllExtractionOp` | 全部轨迹（自对比） | `comparative_all_memory_prompt`（全部 trajectory 文本） |

### D3: 目录结构对齐 Python

```
context_evolver/
├── core/                        # P1 ✅（P3 扩展）
│   ├── context/                 # 新增 LLMService + EmbeddingService
│   ├── op/                      # 新增 OpBase
│   ├── schema/
│   ├── vector_store/            # MemoryVectorStore 已有 GetAll，接口补上
│   ├── file_connector/
│   └── persistence/
├── schema/                      # P2 ✅
├── retrieve/                    # P3 新增
│   └── reme/
│       ├── doc.go
│       ├── run.go               # RecallMemoryOp + RerankMemoryOp + RewriteMemoryOp
│       ├── prompt.go            # rerank + rewrite 提示词
│       ├── utils.go             # parseJSONListResponse + parseJSONField
│       ├── run_test.go
│       ├── prompt_test.go
│       └── utils_test.go
└── summary/                     # P3 新增
    └── reme/
        ├── doc.go
        ├── update.go            # 9 个 Summarize Op
        ├── prompt.go            # 5 个提示词
        ├── utils.go             # parseJSONExperienceResponse + CalculateCosineSimilarity
        ├── update_test.go
        ├── prompt_test.go
        └── utils_test.go
```

### D4: VectorStoreService 加 GetAll

`GetAll(metadataFilter map[string]any) []*schema.VectorNode` 加入 `VectorStoreService` 接口。`MemoryDeduplicationOp` 和 `PersistMemoryOp` 都依赖此方法。`MemoryVectorStore` 已有实现，零改动。

### D5: LLMService.Generate 对齐 Python async_generate

```go
type LLMService interface {
    Generate(ctx context.Context, prompt string, opts ...GenerateOption) (string, error)
}
```

可选参数通过选项函数：`WithSystemPrompt(s)` / `WithTemperature(f)` / `WithMaxTokens(n)`。

**Python 调用链路**：P3 Op 只用 `async_generate(prompt)` 纯文本接口 → P6 的 `OpenAILLMWrapper` 内部包装为 `messages` → 调底层 `BaseModelClient.Invoke()`。Go 对齐此分层。

### D6: EmbeddingService 双方法

```go
type EmbeddingService interface {
    Embed(ctx context.Context, text string) ([]float64, error)
    EmbedBatch(ctx context.Context, texts []string) ([][]float64, error)
}
```

### D7: Op 构造时注入 *ServiceContext

对齐 Python `BaseOp.__init__` 持有 `_service_context` 的模式。每个具体 Op 的构造函数接受 `*ServiceContext` 参数：

```go
func NewRecallMemoryOp(sc *cecontext.ServiceContext, topK int) *RecallMemoryOp
```

### D8: 新增 OpBase 结构体

```go
// OpBase 操作基类，提供对 ServiceContext 中服务的统一访问。
type OpBase struct {
    sc *cecontext.ServiceContext
}

func (b *OpBase) LLM() cecontext.LLMService       // 可能返回 nil
func (b *OpBase) EmbeddingModel() cecontext.EmbeddingService  // 可能返回 nil
func (b *OpBase) VectorStore() cecontext.VectorStoreService   // 可能返回 nil
```

具体 Op 嵌入 `OpBase`：`type RecallMemoryOp struct { OpBase; topK int }`。

### D9: OpBase 放在 core/op 包

与 `BaseOp` 接口同包，三条管线共享。

### D10: Pipeline 组装由调用方统一传入 sc

P6 的 `TaskMemoryService` 持有 `*ServiceContext`，构造管线时统一传给每个 Op。对齐 Python `TaskMemoryService._create_retrieve_flow()` 的模式。

### D11: GenerateOption 选项函数

```go
type GenerateOption func(*GenerateConfig)

func WithSystemPrompt(s string) GenerateOption
func WithTemperature(f float64) GenerateOption
func WithMaxTokens(n int) GenerateOption
```

与项目已有的 `PersistenceOption` 风格一致。

### D12: reme/utils.go 内独立实现 CalculateCosineSimilarity

对齐 Python `summary/task/reme/utils.py` 和 `vector_store` 各自独立实现余弦相似度。两个包用途不同，不为一个小函数引入公共包依赖。

### D13: P3 内一并修改 core 层

`ServiceContext.LLM()` 从返回 `any` 改为返回 `LLMService`，`EmbeddingModel()` 改为返回 `EmbeddingService`。同步更新 P1 测试。变更范围可控。

### D14: IMPLEMENTATION_PLAN P3 描述已更新

补上 `ComparativeAllExtractionOp` 和 core 层改动说明。

---

## 详细设计

### 1. core/context 层扩展

#### 1.1 LLMService 接口

```go
// LLMService LLM 服务本地接口。
// 对齐 Python OpenAILLMWrapper.async_generate(prompt, system_prompt?, temperature?, max_tokens?)。
// P6 的 OpenAILLMWrapper 实现此接口。
type LLMService interface {
    // Generate 调用 LLM 生成文本响应。
    // 对齐 Python async_generate(prompt) → str。
    Generate(ctx context.Context, prompt string, opts ...GenerateOption) (string, error)
}

// GenerateConfig LLM 调用的可选配置。
type GenerateConfig struct {
    SystemPrompt string
    Temperature  float64  // 0 表示未设置，使用模型默认值
    MaxTokens    int      // 0 表示未设置，使用模型默认值
}

// GenerateOption LLM 调用选项函数。
type GenerateOption func(*GenerateConfig)

func WithSystemPrompt(s string) GenerateOption {
    return func(c *GenerateConfig) { c.SystemPrompt = s }
}
func WithTemperature(f float64) GenerateOption {
    return func(c *GenerateConfig) { c.Temperature = f }
}
func WithMaxTokens(n int) GenerateOption {
    return func(c *GenerateConfig) { c.MaxTokens = n }
}
```

#### 1.2 EmbeddingService 接口

```go
// EmbeddingService Embedding 模型本地接口。
// 对齐 Python OpenAIEmbeddingWrapper.async_embed(text)/async_embed_batch(texts)。
// P6 的 OpenAIEmbeddingWrapper 实现此接口。
type EmbeddingService interface {
    // Embed 生成单文本的向量嵌入。
    // 对齐 Python async_embed(text) → List[float]。
    Embed(ctx context.Context, text string) ([]float64, error)
    // EmbedBatch 批量生成文本的向量嵌入。
    // 对齐 Python async_embed_batch(texts) → List[List[float]]。
    EmbedBatch(ctx context.Context, texts []string) ([][]float64, error)
}
```

#### 1.3 VectorStoreService 补充 GetAll

```go
type VectorStoreService interface {
    // ... 现有方法不变 ...
    Upsert(ctx context.Context, node *schema.VectorNode) error
    Search(ctx context.Context, embedding []float64, topK int, metadataFilter map[string]any) ([]*schema.VectorNode, error)
    Delete(ctx context.Context, nodeID string) (bool, error)
    Clear()
    Count() int
    // GetAll 获取所有向量节点，可选按 metadata 过滤。
    // 对齐 Python MemoryVectorStore.get_all(metadata_filter)。
    // MemoryDeduplicationOp 和 PersistMemoryOp 依赖此方法。
    GetAll(metadataFilter map[string]any) []*schema.VectorNode
}
```

#### 1.4 ServiceContext 方法返回值变更

```go
// 修改前：
func (sc *ServiceContext) LLM() any
func (sc *ServiceContext) EmbeddingModel() any

// 修改后：
func (sc *ServiceContext) LLM() LLMService
func (sc *ServiceContext) EmbeddingModel() EmbeddingService
```

内部实现参考已有的 `VectorStore()` 方法的类型断言模式。

### 2. core/op 层扩展

#### 2.1 OpBase 结构体

```go
// OpBase 操作基类，提供对 ServiceContext 中服务的统一访问。
//
// 对齐 Python BaseOp._service_context 属性及其 llm/embedding_model/vector_store 属性。
// 具体 Op 嵌入此结构体以复用服务访问逻辑，避免每个 Op 重复写字段和方法。
//
// Python: openjiuwen/extensions/context_evolver/core/op/base_op.py
type OpBase struct {
    // sc 服务上下文引用，构造时注入
    sc *cecontext.ServiceContext
}

// NewOpBase 创建操作基类。
func NewOpBase(sc *cecontext.ServiceContext) *OpBase {
    return &OpBase{sc: sc}
}

// LLM 返回 LLM 服务。未注册时返回 nil。
// 对齐 Python BaseOp.llm 属性。
func (b *OpBase) LLM() cecontext.LLMService {
    if b.sc == nil {
        return nil
    }
    return b.sc.LLM()
}

// EmbeddingModel 返回 Embedding 模型服务。未注册时返回 nil。
// 对齐 Python BaseOp.embedding_model 属性。
func (b *OpBase) EmbeddingModel() cecontext.EmbeddingService {
    if b.sc == nil {
        return nil
    }
    return b.sc.EmbeddingModel()
}

// VectorStore 返回向量存储服务。未注册时返回 nil。
// 对齐 Python BaseOp.vector_store 属性。
func (b *OpBase) VectorStore() cecontext.VectorStoreService {
    if b.sc == nil {
        return nil
    }
    return b.sc.VectorStore()
}
```

### 3. retrieve/reme 包

包名：`package reme`（`github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/retrieve/reme`）

#### 3.1 Retrieve Ops

**RecallMemoryOp**：按 query embedding 检索 top-K ReMe 记忆

```go
type RecallMemoryOp struct {
    op.OpBase
    topK int
}

func NewRecallMemoryOp(sc *cecontext.ServiceContext, topK int) *RecallMemoryOp
func (op *RecallMemoryOp) Execute(ctx context.Context, rc *cecontext.RuntimeContext) error
```

- 从 `rc` 获取 `query` 和 `user_id`
- 调 `EmbeddingModel().Embed(ctx, query)` 生成 query embedding
- 调 `VectorStore().Search(ctx, embedding, topK, {"workspace_id": userID, "type": "reme_memory"})` 检索
- 结果转 `[]ReMeRetrievedMemory` 写入 `rc.Set("retrieved_memories", ...)`

**RerankMemoryOp**：LLM 对召回结果重新排序

```go
type RerankMemoryOp struct {
    op.OpBase
    llmRerank bool
    topKRerank int
}

func NewRerankMemoryOp(sc *cecontext.ServiceContext, llmRerank bool, topKRerank int) *RerankMemoryOp
func (op *RerankMemoryOp) Execute(ctx context.Context, rc *cecontext.RuntimeContext) error
```

- 从 `rc` 获取 `query` 和 `retrieved_memories`
- `llmRerank=false` 时跳过
- 格式化候选项，调 `LLM().Generate(ctx, prompt)` 获取排序
- 用 `parseJSONListResponse` 解析 `ranked_indices`
- 按 indices 重排，取 `topKRerank` 条

**RewriteMemoryOp**：LLM 将记忆改写为连贯上下文

```go
type RewriteMemoryOp struct {
    op.OpBase
    llmRewrite bool
}

func NewRewriteMemoryOp(sc *cecontext.ServiceContext, llmRewrite bool) *RewriteMemoryOp
func (op *RewriteMemoryOp) Execute(ctx context.Context, rc *cecontext.RuntimeContext) error
```

- 从 `rc` 获取 `query` 和 `retrieved_memories`
- `llmRewrite=false` 时用格式化原文
- 调 `LLM().Generate(ctx, prompt)` 获取改写结果
- 用 `parseJSONField` 解析 `rewritten_context`
- 写入 `rc.Set("memory_string", ...)`

#### 3.2 Retrieve 提示词

一比一复刻 Python `retrieve/task/reme/prompt.py` 中的 2 个提示词常量：

- `memoryRerankPrompt` — 对齐 `MEMORY_RERANK_PROMPT`
- `memoryRewritePrompt` — 对齐 `MEMORY_REWRITE_PROMPT`

以及 `ReMeRetrievePrompts` 结构体（对齐 Python `ReMePrompt` dataclass）。

#### 3.3 Retrieve 工具函数

对齐 Python `retrieve/task/reme/utils.py`：

- `ParseJSONListResponse(response, key string) []int` — 对齐 `parse_json_list_response`
- `ParseJSONField(response, key string) string` — 对齐 `parse_json_field`（返回空字符串表示未找到，Go 不用 Optional）

### 4. summary/reme 包

包名：`package reme`（`github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/summary/reme`）

#### 4.1 Summarize Ops

**TrajectoryPreprocessOp**：按 score 阈值分组为 success/failure

```go
type TrajectoryPreprocessOp struct {
    op.OpBase
}
```

- 从 `rc` 获取 `trajectories`、`score`、`threshold`（默认 1）
- 按 score ≥ threshold 分为 success/failure
- 写入 `rc.Set("success_trajectories", ...)` / `rc.Set("failure_trajectories", ...)` / `rc.Set("all_trajectories", ...)`

**SuccessExtractionOp**：LLM 从成功轨迹提取经验

```go
type SuccessExtractionOp struct {
    op.OpBase
    useExtraction bool
}
```

- `useExtraction=false` 时跳过
- 对每条 success_trajectory 调 `LLM().Generate(ctx, prompt)`
- 用 `parseJSONExperienceResponse` 解析
- 转为 `[]ReMeMemory` 写入 `rc.Set("success_memories", ...)`

**FailureExtractionOp**：LLM 从失败轨迹提取教训

结构与 `SuccessExtractionOp` 对称，使用 `failure_memory_prompt`。

**ComparativeExtractionOp**：LLM 对比最高/最低分轨迹

```go
type ComparativeExtractionOp struct {
    op.OpBase
    useExtraction bool
}
```

- 需要 ≥2 条轨迹且 score 有差异
- 取 max/min score 对应的轨迹
- 写入 `rc.Set("comparative_memories", ...)`

**ComparativeAllExtractionOp**：LLM 对比所有轨迹（自对比）

```go
type ComparativeAllExtractionOp struct {
    op.OpBase
    useExtraction bool
}
```

- 需要 ≥2 条轨迹
- 使用 `comparative_all_memory_prompt`
- 同样写入 `rc.Set("comparative_memories", ...)`

**MemoryValidationOp**：LLM 校验记忆质量

```go
type MemoryValidationOp struct {
    op.OpBase
    useValidation bool
}
```

- 合并 success/failure/comparative 三组 memories
- 对每条 memory 调 `LLM().Generate(ctx, prompt)` 校验
- 解析 `is_valid` + `score`，阈值 0.5
- 写入 `rc.Set("validated_memories", ...)`

**MemoryDeduplicationOp**：基于 embedding 余弦相似度去重

```go
type MemoryDeduplicationOp struct {
    op.OpBase
    useDeduplication    bool
    similarityThreshold float64
}
```

- 从 `VectorStore().GetAll({"workspace_id": userID, "type": "reme_memory"})` 获取已有记忆 embedding
- 对每条新记忆调 `EmbeddingModel().Embed(ctx, text)` 生成 embedding
- 与已有记忆和当前批次做余弦相似度比对，超阈值去重
- 写入 `rc.Set("deduplicated_memories", ...)` / `rc.Set("duplicate_count", ...)`

**UpdateVectorStoreOp**：生成 embedding + 写入向量库

```go
type UpdateVectorStoreOp struct {
    op.OpBase
}
```

- 对每条 `deduplicated_memories` 调 `ReMeMemory.ToVectorNode()` 转换
- 批量调 `EmbeddingModel().EmbedBatch(ctx, contents)` 生成 embedding
- 调 `VectorStore().Upsert(ctx, node)` 逐条写入
- 写入 `rc.Set("stored_count", ...)` / `rc.Set("memory_ids", ...)` / `rc.Set("memories", ...)`

**PersistMemoryOp**：持久化到 JSON/Milvus

```go
type PersistMemoryOp struct {
    op.OpBase
    helper *persistence.MemoryPersistenceHelper
}
```

- 从 `VectorStore().GetAll({"workspace_id": userID, "type": "reme_memory"})` 获取所有节点
- 调 `helper.Save(userID, "reme", nodesDict)` 持久化
- 写入 `rc.Set("persist_count", ...)`

#### 4.2 Summary 提示词

一比一复刻 Python `summary/task/reme/prompt.py` 中的 5 个提示词常量：

- `comparativeMemoryPrompt` — 对齐 `COMPARATIVE_MEMORY_PROMPT`
- `successMemoryPrompt` — 对齐 `SUCCESS_MEMORY_PROMPT`
- `failureMemoryPrompt` — 对齐 `FAILURE_MEMORY_PROMPT`
- `comparativeAllMemoryPrompt` — 对齐 `COMPARATIVE_ALL_MEMORY_PROMPT`
- `memoryValidationPrompt` — 对齐 `MEMORY_VALIDATION_PROMPT`

以及 `ReMeSummaryPrompts` 结构体（对齐 Python `ReMePrompt` dataclass）。

#### 4.3 Summary 工具函数

对齐 Python `summary/task/reme/utils.py`：

- `ParseJSONExperienceResponse(response string) []map[string]any` — 对齐 `parse_json_experience_response`
- `CalculateCosineSimilarity(a, b []float64) float64` — 对齐 `calculate_cosine_similarity`
- `isValidExperience(data map[string]any) bool` — 对齐 `_is_valid_experience`（非导出）

### 5. 错误处理

对齐 Python 的错误处理模式：

- **LLM/Embedding/VectorStore 未注册**：Op 内部检查 `b.LLM() == nil` 等，返回 `fmt.Errorf("LLM not configured in ServiceContext")`（对齐 Python `ValueError`）
- **LLM 调用失败**：`Generate` 返回 error，Op 直接向上传播（对齐 Python `raise ToolchainError`）
- **JSON 解析失败**：工具函数返回空结果 + 日志警告（对齐 Python `logger.warning`），不中断流程
- **轨迹/记忆为空**：跳过处理，写入空列表到 context（对齐 Python 各 Op 的空输入检查）

### 6. 日志对齐

对齐 Python `context_engine_logger` 的所有调用点：

| Python 日志 | Go 日志 | 组件 |
|---|---|---|
| `logger.warning("No trajectories to process")` | `logger.Warn(ComponentAgentCore).Msg("无轨迹可处理")` | ComponentAgentCore |
| `logger.info("Preprocessed %s trajectories: %s success, %s failure", ...)` | `logger.Info(ComponentAgentCore).Int("total", ...).Int("success", ...).Int("failure", ...).Msg(...)` | ComponentAgentCore |
| `logger.debug("Extracting insights from %s successful trajectories...", ...)` | `logger.Debug(ComponentAgentCore).Int("count", ...).Msg(...)` | ComponentAgentCore |
| `logger.warning("Failed to create ReMeMemory from parsed data: %s", e)` | `logger.Warn(ComponentAgentCore).Err(err).Msg(...)` | ComponentAgentCore |
| `logger.warning("Memory validation failed: %s", reason)` | `logger.Warn(ComponentAgentCore).Str("reason", ...).Msg(...)` | ComponentAgentCore |
| `logger.debug("Removed duplicate (similar to existing): %s...", ...)` | `logger.Debug(ComponentAgentCore).Str("when_to_use", ...).Msg(...)` | ComponentAgentCore |
| `logger.info("Deduplicated %s memories to %s (removed %s duplicates)", ...)` | `logger.Info(ComponentAgentCore).Int("total", ...).Int("unique", ...).Int("removed", ...).Msg(...)` | ComponentAgentCore |
| `logger.info("Storing %s ReMe memories to vector store...", ...)` | `logger.Info(ComponentAgentCore).Int("count", ...).Msg(...)` | ComponentAgentCore |
| `logger.info("Successfully stored %s ReMe memories in vector store", ...)` | `logger.Info(ComponentAgentCore).Int("count", ...).Msg(...)` | ComponentAgentCore |
| `logger.info("PersistMemoryOp (ReMe): persisted %d memories for user=%s via %s", ...)` | `logger.Info(ComponentAgentCore).Int("count", ...).Str("user_id", ...).Str("backend", ...).Msg(...)` | ComponentAgentCore |

### 7. 测试策略

#### 7.1 Mock 策略

P3 不依赖真实 LLM/Embedding API，通过 mock 接口测试：

- `fakeLLMService`：实现 `LLMService`，返回预设文本
- `fakeEmbeddingService`：实现 `EmbeddingService`，返回预设向量
- `VectorStoreService`：直接使用 `MemoryVectorStore`（纯内存，无需 mock）

#### 7.2 覆盖率要求

- 每个 Op 的 `Execute` 方法必须覆盖：正常路径 + 空输入 + 服务未注册 + LLM 返回非法 JSON
- 工具函数覆盖：正常 JSON / 无 JSON block / 畸形 JSON
- 提示词测试：确认格式化占位符替换正确
- 目标：≥ 85%

#### 7.3 测试函数命名

遵循项目规范：`TestXxx` / `TestXxx_场景描述`

---

## 变更影响

### P1 代码变更

| 文件 | 变更内容 |
|---|---|
| `core/context/service_context.go` | 新增 `LLMService`/`EmbeddingService` 接口定义 + `GenerateOption`/`GenerateConfig` + `LLM()`/`EmbeddingModel()` 改返回接口类型 |
| `core/context/service_context_test.go` | 同步更新测试 |
| `core/context/doc.go` | 更新文件目录 |
| `core/op/base_op.go` | 新增 `OpBase` 结构体 + `NewOpBase` + 服务访问方法 |
| `core/op/base_op_test.go` | 新增 OpBase 测试 |
| `core/op/doc.go` | 更新文件目录 |
| `core/vector_store/memory_vector_store.go` | 无代码改动，`GetAll` 已在 P1 实现 |

### P2 代码变更

无。P2 的 `ReMeMemory`/`ReMeRetrievedMemory`/`ReMeSummarizeRequest`/`ReMeRetrieveRequest`/`ReMeSummarizeResponse`/`ReMeRetrieveResponse` 已在 P2 定义完毕。

### 新增文件

| 文件 | 内容 |
|---|---|
| `retrieve/reme/doc.go` | 包文档 |
| `retrieve/reme/run.go` | 3 个 Retrieve Op |
| `retrieve/reme/prompt.go` | 2 个提示词 + ReMeRetrievePrompts |
| `retrieve/reme/utils.go` | 2 个工具函数 |
| `retrieve/reme/run_test.go` | Retrieve Op 测试 |
| `retrieve/reme/prompt_test.go` | 提示词测试 |
| `retrieve/reme/utils_test.go` | 工具函数测试 |
| `summary/reme/doc.go` | 包文档 |
| `summary/reme/update.go` | 9 个 Summarize Op |
| `summary/reme/prompt.go` | 5 个提示词 + ReMeSummaryPrompts |
| `summary/reme/utils.go` | 3 个工具函数 |
| `summary/reme/update_test.go` | Summarize Op 测试 |
| `summary/reme/prompt_test.go` | 提示词测试 |
| `summary/reme/utils_test.go` | 工具函数测试 |

---

## 不在 P3 范围内

- `OpenAILLMWrapper` / `OpenAIEmbeddingWrapper`（P6）
- `TaskMemoryService`（P6）
- `ContextEvolutionRail` + 工具层（P7）
- `MilvusConnector`（P7）
- ReasoningBank 算法（P4）
- ACE 算法（P5）
- 集成测试（需真实 LLM API，用 `//go:build llm` 标签）
