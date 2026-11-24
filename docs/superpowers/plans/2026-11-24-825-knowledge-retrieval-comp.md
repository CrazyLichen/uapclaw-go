# 8.25 批 KnowledgeRetrievalComp + Memory 两件 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 对齐 Python `workflow/components/resource/` 三组件——8.25 KnowledgeRetrievalComp（第一轮）、8.25a MemoryRetrievalComp / 8.25b MemoryWriteComp（第二轮），含 workflow 端到端多检索模式真链集成验证。

**Architecture:** 新建 `internal/openuapclaw/core/workflow/components/resource/` 子包，三文件一比一复刻 Python；Executable 嵌 `components.WorkflowComponent` 基座（InvokeFn/TraceMetadataFn 槽注入，8.17/8.24 惯例）；KB 构造消费 13.26-13.28 既有 knowledgebase 包；集成测试真实创建 workflow 并以 glm-4.5-flash 免费档跑 vector/hybrid/graph/agentic 四模式。

**Tech Stack:** Go 1.x、既有 internal/openuapclaw/core/{retrieval,foundation/llm,session,workflow,common/{exception,security,logging,pythonsem}}、chroma/milvus 按 13.x store 选项、`//go:build integration && llm` 集成测试。

**Spec:** `docs/superpowers/specs/2026-11-24-825-knowledge-retrieval-comp-design.md`

**已核对的事实基线（实施者必读）：**
- 错误码常量已存在：`exception.StatusComponentKnowledgeRetrievalInvokeCallFailed`(101100)/`...EmbedModelInitError`(101101)/`...InputParamError`(101102)/`...LLMModelInitError`(101103)，在 `internal/common/exception/codes_workflow.go:220-236`
- `security.IsSensitive()` 已存在（`internal/common/security/user_config.go:91`）——spec §2.5 缺口核查关闭，无需讨论
- `knowledgebase.RetrieveMultiKBWithSource(ctx, kbs, query, *common.RetrievalConfig, topK *int)` 在 `retrieval/knowledgebase/multi_kb.go:82`
- KB 构造为 Go 侧不对称直译（13.27 拍板）：`NewSimpleKnowledgeBase(config, retriever Retriever, opts...)` / `NewGraphKnowledgeBase(config, chunkRetriever, tripleRetriever, opts...)`；`WithVectorStore/WithEmbedModel/WithLLMClient(BaseModelClient)` 选项在 `knowledgebase/base.go:143-177`
- Retriever 按索引类型构造：`retriever.NewSparseRetriever(store)` / `NewVectorRetriever(store, embed)` / `NewHybridRetriever(store, embed, opts...)`；agentic 包装 `retriever.NewAgenticRetriever(base, llm, opts...)`（`agentic_retriever.go:150`）
- `vector_store.CreateVectorStore(config common.VectorStoreConfig, opts ...StoreCreateOption)`（`vector_store/store.go:131`）；`embedding.NewOpenAIEmbedding(config, opts...)`（`openai.go:124`，**返回无 error**——Python 构造抛异常路径在 Go 是方法级调用失败，见 D-25-3）
- ResourceMgr 取模型先例：`llm_comp.go:977-991 getModel`——`runner.GetResourceMgr()` → `mgr.GetModel(ctx, []string{modelID})` 取 `clients[0]`，直接复用该模式
- Executable 模式：嵌 `components.WorkflowComponent`，构造期 `e.InvokeFn = e.invokeEntry`、`e.TraceMetadataFn = e.traceMetadata`（8.24 QuestionerComp 同款；勿 override 基座两方法——8.20 教训）
- 基座槽签名：`InvokeFn func(ctx, inputs map[string]any, sess *session.NodeSessionFacade, modelContext iface.ModelContext) (map[string]any, error)`（`components/component.go:54`）
- NodeSessionFacade 有 `GetExecutableID()/GetSessionID()/GetComponentID()`（8.22/8.24 已消费）
- memory 域：`ltm.LongTermMemory`（7.27）已有 `SearchUserMem/SearchUserHistorySummary/AddMessages`；`memory.AgentMemoryConfig` 在 `core/memory/config/agent_config.go`

---

## 文件结构（两轮全量）

```
internal/openuapclaw/core/workflow/components/resource/
├── doc.go                        # 子包文档（Task 1 创建，第二轮补条目）
├── knowledge_retrieval_comp.go   # 8.25 一比一 ← knowledge_retrieval_comp.py (281行)
├── memory_retrieval_comp.go      # 8.25a ← memory_retrieval_comp.py (160行)
└── memory_write_comp.go          # 8.25b ← memory_write_comp.py (155行)

tests/integration/agentcore/workflow_knowledge_retrieval/
└── knowledge_retrieval_e2e_test.go   # 集成：workflow 端到端多模式真链
```

修改：`components/doc.go`（文件树加 resource/ 条目）、`IMPLEMENTATION_PLAN.md`（8.25 行 + 新增 8.25a/8.25b 行）。

**已知偏差（spec §五 预设 + 本计划新增，实施中如遇新偏差先登记再实施）：**
- D-25-1：Python `get_model(id=...)` 单实例 vs Go `GetModel(ctx, []string)` 取 [0]（8.18 先例）
- D-25-2：Python 组件向 KB 传 `vector_store/embed_model/llm_client` 三件、KB 内部组装检索器；Go 侧 13.27 拍板 KB 签名不对称直译收显式 `retriever`——组件按 `kb_config.index_type` 构建对应 Retriever（sparse/vector/hybrid），agentic 时 `NewAgenticRetriever` 包装后再传入 KB。检索行为等价，构造归属差异登记
- D-25-3：Python `OpenAIEmbedding(config, ...)` 构造可抛异常（wrap 101101）；Go `NewOpenAIEmbedding` 无 error 返回——101101 的 embed 构造失败分支在 Go 收窄为「embed_config 为 nil 或 index_type 校验失败」两分支，运行期 embed 调用失败经 `RetrieveMultiKBWithSource` 错误传播仍 wrap 101100
- D-25-4：Python `validate_inputs` 用 pydantic ValidationError；Go 用 `pythonsem.ValidateAndConvertType` 校验 + 错误信息对齐 `ExceptionUtils.format_validation_error` 风格（字段名+原因），逐字程度以 8.23/8.24 先例为准

---

# 第一轮：8.25 KnowledgeRetrievalComp

## Task 0: 建包 + 计划表登记

**Files:**
- Create: `internal/openuapclaw/core/workflow/components/resource/doc.go`
- Modify: `internal/openuapclaw/core/workflow/components/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 0.1: 创建 resource/doc.go**

```go
// Package resource 提供 workflow 资源类组件（知识检索/长记忆读写）。
//
// 一比一对齐 Python openjiuwen/core/workflow/components/resource/ 三组件：
// 知识库检索（KnowledgeRetrievalComp）、长记忆检索（MemoryRetrievalComp）、
// 长记忆写入（MemoryWriteComp）。均为纯 INVOKE 叶子执行节点，不涉流式路径。
//
// 文件目录：
//
//	resource/
//	├── doc.go                        # 包文档
//	├── knowledge_retrieval_comp.go  # 知识库检索组件（Python knowledge_retrieval_comp.py）
//	├── memory_retrieval_comp.go     # 长记忆检索组件（Python memory_retrieval_comp.py）
//	└── memory_write_comp.go         # 长记忆写入组件（Python memory_write_comp.py）
//
// 对应 Python 代码：openjiuwen/core/workflow/components/resource/
package resource
```

- [ ] **Step 0.2: components/doc.go 文件树加 `resource/` 条目**（一行：`│	└── resource/           # 资源类组件（知识检索/长记忆读写）`，按现有树形缩进风格）

- [ ] **Step 0.3: IMPLEMENTATION_PLAN.md 登记**：8.25 行改 `🔄`；紧随其后插入两行（内容为占位、完成轮次回填）：

```markdown
| 8.25a | ☐ | MemoryRetrievalComp | 长记忆检索组件 | `openjiuwen/core/workflow/components/resource/` |
| 8.25b | ☐ | MemoryWriteComp | 长记忆写入组件 | `openjiuwen/core/workflow/components/resource/` |
```

- [ ] **Step 0.4: 编译验证** `go build ./internal/openuapclaw/core/workflow/...`，期望无错
- [ ] **Step 0.5: Commit**

```bash
git add internal/openuapclaw/core/workflow/components/resource/doc.go internal/openuapclaw/core/workflow/components/doc.go IMPLEMENTATION_PLAN.md
git commit internal/openuapclaw/core/workflow/components/resource/doc.go internal/openuapclaw/core/workflow/components/doc.go IMPLEMENTATION_PLAN.md -m "feat(8.25): resource 子包建包+计划表登记 8.25a/8.25b（方案B拆两轮）"
```

> ⚠️ 共享 checkout 纪律：commit 前必须 `git status --porcelain` 核对第一列无他流 staged 改动；一律 path-limited 提交；commit 后 `git show --stat HEAD` 核对文件数。

## Task 1: 配置/输入输出类型 + validateInputs/formatOutput

**Files:**
- Create: `internal/openuapclaw/core/workflow/components/resource/knowledge_retrieval_comp.go`
- Test: `internal/openuapclaw/core/workflow/components/resource/knowledge_retrieval_comp_test.go`

- [ ] **Step 1.1: 写失败测试**

```go
package resource

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/<module>/internal/common/exception"
	retrievalcommon "github.com/<module>/internal/openuapclaw/core/retrieval/common"
)

// 测试命名禁①序号字符（11.12 教训）
func TestValidateInputs_query缺失时返回101102(t *testing.T) {
	_, err := validateKnowledgeRetrievalInputs(map[string]any{})
	require.Error(t, err)
	assert.True(t, exception.Is(err, exception.StatusComponentKnowledgeRetrievalInputParamError))
}

func TestValidateInputs_query非字符串时返回101102(t *testing.T) {
	_, err := validateKnowledgeRetrievalInputs(map[string]any{"query": 123})
	require.Error(t, err)
	assert.True(t, exception.Is(err, exception.StatusComponentKnowledgeRetrievalInputParamError))
}

func TestValidateInputs_extra键宽容只取query(t *testing.T) {
	in, err := validateKnowledgeRetrievalInputs(map[string]any{"query": "q", "other": 1})
	require.NoError(t, err)
	assert.Equal(t, "q", in.Query)
}

func TestFormatOutput_results拼接context(t *testing.T) {
	out := formatKnowledgeRetrievalOutput([]retrievalcommon.MultiKBRetrievalResult{
		{Text: "a"}, {Text: "b"},
	})
	assert.Equal(t, []string{"a", "b"}, out["results"])
	assert.Equal(t, "a\n\nb", out["context"])
}

func TestFormatOutput_空results(t *testing.T) {
	out := formatKnowledgeRetrievalOutput(nil)
	assert.Equal(t, []string{}, out["results"]) // pydantic default_factory=list 语义：非nil空切片
	assert.Equal(t, "", out["context"])
}
```

- [ ] **Step 1.2: 跑测试确认 FAIL**（编译错：未定义）`go test ./internal/openuapclaw/core/workflow/components/resource/ -run TestValidateInputs -v`
- [ ] **Step 1.3: 写类型与两函数**

```go
// Package doc 见 doc.go（此处仅示例核心片段，文件其余部分按 Go 规范排列）
// 知识检索组件（Python knowledge_retrieval_comp.py 一比一）

// ComponentKBConfig 单知识库配置（Python ComponentKBConfig :42-46）
type ComponentKBConfig struct {
	// KBConfig 知识库配置（Python kb_config）
	KBConfig retrievalcommon.KnowledgeBaseConfig
	// VectorStoreConfig 向量库配置
	VectorStoreConfig retrievalcommon.VectorStoreConfig
	// EmbedConfig 嵌入配置，nil 表示未提供（Python embed_config Optional）
	EmbedConfig *embedding.EmbeddingConfig
	// EmbedAdditionalConfig 嵌入额外配置（Python embed_additional_config）
	EmbedAdditionalConfig map[string]any
}

// KnowledgeRetrievalCompConfig 组件配置（Python :49-56 dataclass kw_only）
type KnowledgeRetrievalCompConfig struct {
	ComponentKBConfigs           []ComponentKBConfig
	VectorStoreConnectionConfig  map[string]any
	RetrievalConfig              retrievalcommon.RetrievalConfig
	ModelID                      string // 空串 ≙ Python None
	ModelClientConfig            *llm.ModelClientConfig
	ModelConfig                  *llm.ModelRequestConfig
}

// KnowledgeRetrievalInput 输入（Python :59-62；extra=allow → 只取 query 键）
type KnowledgeRetrievalInput struct {
	Query string
}

// validateKnowledgeRetrievalInputs 校验并提取 query（Python validate_inputs :249-258）
func validateKnowledgeRetrievalInputs(inputs map[string]any) (KnowledgeRetrievalInput, error) {
	raw, ok := inputs["query"]
	if !ok {
		return KnowledgeRetrievalInput{}, exception.BuildError(
			exception.StatusComponentKnowledgeRetrievalInputParamError,
			exception.WithMsg("query\n  Field required"))
	}
	query, err := pythonsem.ValidateAndConvertType(raw, "string")
	if err != nil {
		return KnowledgeRetrievalInput{}, exception.BuildError(
			exception.StatusComponentKnowledgeRetrievalInputParamError,
			exception.WithMsg("query\n  "+err.Error()))
	}
	return KnowledgeRetrievalInput{Query: query.(string)}, nil
}

// formatKnowledgeRetrievalOutput 组装输出（Python _format_output :260-269；
// KnowledgeRetrievalOutput model_dump 等价 map）
func formatKnowledgeRetrievalOutput(results []retrievalcommon.MultiKBRetrievalResult) map[string]any {
	texts := make([]string, 0, len(results))
	for _, r := range results {
		texts = append(texts, r.Text)
	}
	return map[string]any{
		"results": texts,
		"context": strings.Join(texts, "\n\n"),
	}
}
```

（import 路径占位 `<module>` 以 go.mod module 名为准；日志组件常量 `const logComponent = logger.ComponentAgentCore`——落地时对照 8.22/8.24 实际取值）

- [ ] **Step 1.4: 跑测试 PASS** 同命令，期望 PASS
- [ ] **Step 1.5: Commit** `git commit <两文件> -m "feat(8.25): 知识检索组件配置/输入输出/校验/格式化（Python :42-69,:249-269）"`

## Task 2: createLLMInstance（agentic 模型获取）

**Files:** 同 Task 1 两文件

- [ ] **Step 2.1: 失败测试**

```go
func TestCreateLLMInstance_agentic无模型配置返回101103(t *testing.T) {
	e := newTestExecutable(&KnowledgeRetrievalCompConfig{RetrievalConfig: retrievalcommon.RetrievalConfig{Agentic: true}})
	_, err := e.createLLMInstance(context.Background())
	require.Error(t, err)
	assert.True(t, exception.Is(err, exception.StatusComponentKnowledgeRetrievalLLMModelInitError))
}

func TestCreateLLMInstance_直构Model路径(t *testing.T) {
	e := newTestExecutable(&KnowledgeRetrievalCompConfig{
		RetrievalConfig:   retrievalcommon.RetrievalConfig{Agentic: true},
		ModelClientConfig: &llm.ModelClientConfig{ /* 最小合法值，对照 llm 包字段 */ },
		ModelConfig:       &llm.ModelRequestConfig{},
	})
	m, err := e.createLLMInstance(context.Background())
	require.NoError(t, err)
	assert.NotNil(t, m)
}

func TestCreateLLMInstance_modelID走ResourceMgr(t *testing.T) {
	// 前置：注册测试模型进 ResourceMgr（对照 llm_comp_test.go 既有 getModel 测试装配方式）
	// 断言：返回 clients[0]；未注册模型 → error（对齐 getModel "model %s not found"）
}
```

- [ ] **Step 2.2: FAIL 确认 → Step 2.3: 实现**

```go
// createLLMInstance 创建 agentic 检索模型实例（Python _create_llm_instance :235-246）
func (e *KnowledgeRetrievalExecutable) createLLMInstance(ctx context.Context) (model_clients.BaseModelClient, error) {
	if e.cfg.ModelID == "" {
		if e.cfg.ModelClientConfig == nil || e.cfg.ModelConfig == nil {
			return nil, exception.BuildError(
				exception.StatusComponentKnowledgeRetrievalLLMModelInitError,
				exception.WithMsg("LLM model config is required for agentic retrieval"))
		}
		return llm.NewModel(e.cfg.ModelClientConfig, e.cfg.ModelConfig), nil
	}
	// Python: Runner.resource_mgr.get_model(id=...)；D-25-1 单元素收窄（8.18 getModel 先例直调）
	mgr := runner.GetResourceMgr()
	if mgr == nil {
		return nil, fmt.Errorf("resource mgr is not initialized")
	}
	clients, err := mgr.GetModel(ctx, []string{e.cfg.ModelID})
	if err != nil {
		return nil, err
	}
	if len(clients) == 0 {
		return nil, fmt.Errorf("model %s not found", e.cfg.ModelID)
	}
	return clients[0], nil
}
```

（`NewModel` 返回签名以 `foundation/llm/model.go:73` 为准；LLMInvoker/BaseModelClient 类型对齐 8.18 LLMInvoker 收口惯例）

- [ ] **Step 2.4: PASS → Step 2.5: Commit** `feat(8.25): createLLMInstance（agentic 模型直构/ResourceMgr 双路径）`

## Task 3: createKnowledgeBases（store/embed/retriever/KB 分叉）

**Files:** 同 Task 1 两文件

- [ ] **Step 3.1: 失败测试**

```go
func TestCreateKnowledgeBases_vector无embedConfig返回101101(t *testing.T) {
	e := newTestExecutable(&KnowledgeRetrievalCompConfig{
		ComponentKBConfigs: []ComponentKBConfig{{
			KBConfig:      retrievalcommon.KnowledgeBaseConfig{IndexType: "vector"}, // 字段名对照 retrievalcommon
			VectorStoreConfig: testVectorStoreConfig(), // InMemory/Chroma 本地可构造配置
		}},
	})
	_, err := e.createKnowledgeBases(context.Background())
	require.Error(t, err)
	assert.True(t, exception.Is(err, exception.StatusComponentKnowledgeRetrievalEmbedModelInitError))
}

func TestCreateKnowledgeBases_hybrid同样要求embed(t *testing.T) { /* IndexType: "hybrid"，同断言 101101 */ }

func TestCreateKnowledgeBases_useGraph构造GraphKB(t *testing.T) {
	// embed_config 提供 → 构造成功；断言返回实例类型 *knowledgebase.GraphKnowledgeBase
}

func TestCreateKnowledgeBases_agentic包装AgenticRetriever(t *testing.T) {
	// retrieval_config.agentic=true + 已注入 llm → simple 路径 KB 内 retriever 为 *retriever.AgenticRetriever
	// （经 KB 暴露的访问器或检索行为断言；访问器不存在则用接口断言）
}

func TestCreateKnowledgeBases_sparse索引不需要embed(t *testing.T) { /* IndexType: "bm25" 或 sparse 值对照 Python：仅 vector/hybrid 校验 */ }
```

- [ ] **Step 3.2: FAIL → Step 3.3: 实现**

```go
// createKnowledgeBases 构建知识库列表（Python _create_knowledge_bases :185-233）。
// D-25-2：Go KB 收显式 retriever（13.27 不对称直译），组件按 index_type 组装检索器；
// D-25-3：Go NewOpenAIEmbedding 无 error，embed 构造失败分支收窄为配置校验。
func (e *KnowledgeRetrievalExecutable) createKnowledgeBases(ctx context.Context) ([]knowledgebase.KnowledgeBase, error) {
	useGraph := e.cfg.RetrievalConfig.UseGraph != nil && *e.cfg.RetrievalConfig.UseGraph // Python use_graph bool；Go *bool nil→false 语义对照 retrievalcommon 注释
	kbInstances := make([]knowledgebase.KnowledgeBase, 0, len(e.cfg.ComponentKBConfigs))

	for i := range e.cfg.ComponentKBConfigs {
		kbCfg := &e.cfg.ComponentKBConfigs[i]

		vectorStore, err := vector_store.CreateVectorStore(kbCfg.VectorStoreConfig,
			vector_store.WithConnectionConfig(e.cfg.VectorStoreConnectionConfig)) // StoreCreateOption 具体形态落地时对照 store.go
		if err != nil {
			return nil, err
		}

		// embed model needed for vector/hybrid search（Python :194-212）
		var embedModel embedding.BaseEmbedding
		if kbCfg.EmbedConfig == nil {
			if kbCfg.KBConfig.IndexType == "vector" || kbCfg.KBConfig.IndexType == "hybrid" {
				return nil, exception.BuildError(
					exception.StatusComponentKnowledgeRetrievalEmbedModelInitError,
					exception.WithMsg("Embedding config is required for vector or hybrid index type"))
			}
		} else {
			embedModel = embedding.NewOpenAIEmbedding(*kbCfg.EmbedConfig) // D-25-3
		}

		retriever, err := e.buildRetriever(ctx, kbCfg, vectorStore, embedModel)
		if err != nil {
			return nil, err
		}

		var kb knowledgebase.KnowledgeBase
		if useGraph {
			// Python GraphKnowledgeBase(config, vector_store, embed_model, llm_client)；
			// Go 图库双检索器形态（13.28）：chunk/triple 同源，落地时对照 graph_kb.go 内部
			// 若双检索器同型则同参传入（D-25-2 延伸，登记偏差）
			kb, err = knowledgebase.NewGraphKnowledgeBase(kbCfg.KBConfig, retriever, retriever,
				knowledgebase.WithVectorStore(vectorStore),
				knowledgebase.WithEmbedModel(embedModel),
				knowledgebase.WithLLMClient(e.llm))
		} else {
			kb, err = knowledgebase.NewSimpleKnowledgeBase(kbCfg.KBConfig, retriever,
				knowledgebase.WithVectorStore(vectorStore),
				knowledgebase.WithEmbedModel(embedModel),
				knowledgebase.WithLLMClient(e.llm))
		}
		if err != nil {
			return nil, err
		}
		kbInstances = append(kbInstances, kb)
	}
	return kbInstances, nil
}

// buildRetriever 按索引类型组装检索器（D-25-2；Python 无此函数——KB 内部组装的 Go 显式化）
func (e *KnowledgeRetrievalExecutable) buildRetriever(ctx context.Context,
	kbCfg *ComponentKBConfig, vs vector_store.VectorStore, em embedding.BaseEmbedding,
) (retriever.Retriever, error) {
	var base retriever.Retriever
	switch kbCfg.KBConfig.IndexType {
	case "bm25", "sparse":
		base = retriever.NewSparseRetriever(vs)
	case "hybrid":
		base = retriever.NewHybridRetriever(vs, em)
	default: // vector
		base = retriever.NewVectorRetriever(vs, em)
	}
	if e.cfg.RetrievalConfig.Agentic && e.llm != nil {
		return retriever.NewAgenticRetriever(base, e.llm) // 错误传播（retriever 必填 155208 防御已在库内）
	}
	return base, nil
}
```

- [ ] **Step 3.4: PASS → Step 3.5: Commit** `feat(8.25): createKnowledgeBases——store/embed/index_type 分叉+agentic 检索器包装（D-25-2/3）`

## Task 4: initializeIfNeeded 双检锁

**Files:** 同 Task 1 两文件

- [ ] **Step 4.1: 失败测试**（含 `-race`）

```go
func TestInitializeIfNeeded_并发只初始化一次(t *testing.T) {
	e := newTestExecutable(minSimpleConfig()) // 非 agentic + sparse KB，构造可成功
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = e.initializeIfNeeded(context.Background()) }()
	}
	wg.Wait()
	assert.True(t, e.initialized)
	assert.Len(t, e.kbs, 1)
}

func TestInitializeIfNeeded_失败后保持未初始化可重试(t *testing.T) {
	e := newTestExecutable(badConfig()) // vector 无 embed → 101100 wrap
	err := e.initializeIfNeeded(context.Background())
	require.Error(t, err)
	assert.True(t, exception.Is(err, exception.StatusComponentKnowledgeRetrievalInvokeCallFailed))
	assert.False(t, e.initialized)
}
```

- [ ] **Step 4.2: FAIL → Step 4.3: 实现**

```go
// initializeIfNeeded 惰性初始化（Python _initialize_if_needed :167-183；
// asyncio.Lock 双检 → sync.Once 语义等价：失败保持未初始化可重试）
func (e *KnowledgeRetrievalExecutable) initializeIfNeeded(ctx context.Context) error {
	if e.initialized {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.initialized {
		return nil
	}
	if e.cfg.RetrievalConfig.Agentic {
		llm, err := e.createLLMInstance(ctx)
		if err != nil {
			return exception.BuildError(
				exception.StatusComponentKnowledgeRetrievalInvokeCallFailed,
				exception.WithMsg("Failed to initialise knowledge retrieval component"), exception.WithCause(err))
		}
		e.llm = llm
	}
	kbs, err := e.createKnowledgeBases(ctx)
	if err != nil {
		return exception.BuildError(
			exception.StatusComponentKnowledgeRetrievalInvokeCallFailed,
			exception.WithMsg("Failed to initialise knowledge retrieval component"), exception.WithCause(err))
	}
	e.kbs = kbs
	e.initialized = true
	return nil
}
```

（`exception.WithCause` 具体选项名对照 `internal/common/exception/errors.go` 既有用法）

- [ ] **Step 4.4: PASS（`-race` 同跑）→ Step 4.5: Commit** `feat(8.25): 惰性初始化双检锁（asyncio.Lock 语义等价）`

## Task 5: invokeEntry 主链 + 三处日志 + TraceMetadata + 组件外壳

**Files:** 同 Task 1 两文件

- [ ] **Step 5.1: 失败测试**

```go
func TestInvokeEntry_空query返回101102(t *testing.T) { /* inputs {"query": "  "} → 101102，且起始日志不出现 */ }

func TestInvokeEntry_端到端fake底(t *testing.T) {
	// 真实链纪律：KB 用 InMemory store + 真 retriever；不 mock 被测组件
	// 写入文档 → invoke → 断言 results/context 非空
}

func TestInvokeEntry_检索失败包装101100(t *testing.T) {
	// store 构造成功但检索层必败（如无效 collection 配置）→ 101100 cause 传播
}

func TestTraceMetadata_非agentic无requestModel(t *testing.T) {
	md := e.traceMetadata()
	assert.Equal(t, 5, md["top_k"])
	assert.Equal(t, []string{"kb-1"}, md["data_source_ids"])
	_, ok := md["request_model"]
	assert.False(t, ok)
}

func TestTraceMetadata_agentic且有modelId带requestModel(t *testing.T) { /* md["request_model"] == "glm-4.5-flash" */ }
```

- [ ] **Step 5.2: FAIL → Step 5.3: 实现**

```go
// invokeEntry 执行入口（Python invoke :80-143 十步一比一）
func (e *KnowledgeRetrievalExecutable) invokeEntry(ctx context.Context,
	inputs map[string]any, sess *session.NodeSessionFacade, modelContext iface.ModelContext,
) (map[string]any, error) {
	e.setSession(sess) // Python self._set_session(:145-146)

	if err := e.initializeIfNeeded(ctx); err != nil {
		return nil, err
	}

	// Validate & extract query（Python :86-92）
	in, err := validateKnowledgeRetrievalInputs(inputs)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.Query) == "" {
		return nil, exception.BuildError(
			exception.StatusComponentKnowledgeRetrievalInputParamError,
			exception.WithMsg("Query must be a non-empty string"))
	}

	// 起始日志（Python :94-106；字段逐项：query_length/top_k/sensitive_mode）
	logger.Info(logComponent).
		Str("event_type", "WORKFLOW_COMPONENT_START").
		Str("component_id", sess.GetExecutableID()).
		Str("component_type_str", "KnowledgeRetrievalComponent").
		Str("session_id", sess.GetSessionID()).
		Int("query_length", len([]rune(in.Query))). // Python len(str)=字符数
		Int("top_k", e.cfg.RetrievalConfig.TopK).
		Bool("sensitive_mode", security.IsSensitive()).
		Msg("Knowledge retrieval started")

	// 检索（Python :108-127；注意：错误日志的 component_id 用入参 session 而非 self._session——Python :118 原样保留）
	results, err := knowledgebase.RetrieveMultiKBWithSource(ctx, e.kbs, in.Query, &e.cfg.RetrievalConfig, nil)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "WORKFLOW_COMPONENT_ERROR").
			Str("component_id", sess.GetComponentID()).
			Str("component_type_str", "KnowledgeRetrievalComponent").
			Str("session_id", sess.GetSessionID()).
			Err(err).
			Msg("Knowledge retrieval retrieve call failed")
		return nil, exception.BuildError(
			exception.StatusComponentKnowledgeRetrievalInvokeCallFailed,
			exception.WithMsg("Knowledge retrieval retrieve call failed"), exception.WithCause(err))
	}

	output := formatKnowledgeRetrievalOutput(results)

	// 结束日志（Python :131-141）
	logger.Info(logComponent).
		Str("event_type", "WORKFLOW_COMPONENT_END").
		Str("component_id", sess.GetExecutableID()).
		Str("component_type_str", "KnowledgeRetrievalComponent").
		Str("session_id", sess.GetSessionID()).
		Int("num_results", len(results)).
		Bool("sensitive_mode", security.IsSensitive()).
		Msg("Knowledge retrieval completed")

	return output, nil
}

// traceMetadata 追踪元数据（Python trace_metadata :148-165；TraceMetadataFn 槽绑定）
func (e *KnowledgeRetrievalExecutable) traceMetadata() map[string]any {
	ids := make([]string, 0, len(e.cfg.ComponentKBConfigs))
	for i := range e.cfg.ComponentKBConfigs {
		ids = append(ids, e.cfg.ComponentKBConfigs[i].KBConfig.KBID) // 字段名对照 retrievalcommon.KnowledgeBaseConfig
	}
	metadata := map[string]any{
		"top_k":           e.cfg.RetrievalConfig.TopK,
		"data_source_ids": ids,
	}
	if e.cfg.RetrievalConfig.Agentic && e.cfg.ModelID != "" {
		metadata["request_model"] = e.cfg.ModelID
	}
	return metadata
}
```

组件外壳（同文件，8.24 QuestionerComponent 同款）：

```go
// KnowledgeRetrievalComponent 组件外壳（Python :272-281）
type KnowledgeRetrievalComponent struct {
	components.WorkflowComponent
}

// NewKnowledgeRetrievalComponent 构造：槽注入 self（基座槽优先，勿 override 两方法）
func NewKnowledgeRetrievalComponent(cfg *KnowledgeRetrievalCompConfig) (*KnowledgeRetrievalComponent, error) {
	exec, err := NewKnowledgeRetrievalExecutable(cfg)
	if err != nil {
		return nil, err
	}
	c := &KnowledgeRetrievalComponent{}
	c.ToExecutableFn = func() graphexe.GeneralExecutor { return exec }
	c.TraceMetadataFn = exec.traceMetadata
	return c, nil
}
```

Executable 构造：

```go
// KnowledgeRetrievalExecutable 执行体（Python :70-78 字段一比一；_lock→mu sync.Mutex）
type KnowledgeRetrievalExecutable struct {
	components.WorkflowComponent
	cfg         *KnowledgeRetrievalCompConfig
	kbs         []knowledgebase.KnowledgeBase
	llm         model_clients.BaseModelClient
	initialized bool
	mu          sync.Mutex
	sess        *session.NodeSessionFacade
}

// NewKnowledgeRetrievalExecutable 构造
func NewKnowledgeRetrievalExecutable(cfg *KnowledgeRetrievalCompConfig) (*KnowledgeRetrievalExecutable, error) {
	if cfg == nil {
		return nil, fmt.Errorf("component config is required") // Python __init__ 默认 None 由外壳兜住；直调 nil 防御对照 8.3 nil fn panic 先例
	}
	e := &KnowledgeRetrievalExecutable{cfg: cfg}
	e.InvokeFn = e.invokeEntry
	e.TraceMetadataFn = e.traceMetadata
	return e, nil
}
```

（AddComponent 由基座 `AddComponentToGraph` 提供——Python `add_component` :277-278 直调 `graph.add_node`；对照 8.24 外壳是否 override，缺则按基座约定补一转发方法）

- [ ] **Step 5.4: PASS → Step 5.5: 全包测试 + race**：`CGO_ENABLED=1 go test -race -cover ./internal/openuapclaw/core/workflow/components/resource/...`，期望 PASS 且包覆盖 ≥85%
- [ ] **Step 5.6: Commit** `feat(8.25): invoke 主链+三处日志+traceMetadata+组件外壳（Python invoke :80-281 收口）`

## Task 6: 集成测试——真实 workflow × 多检索模式真链（用户硬要求）

**Files:**
- Create: `tests/integration/agentcore/workflow_knowledge_retrieval/knowledge_retrieval_e2e_test.go`

**前置事实**：免费 LLM `glm-4.5-flash`（8.24 真链冒烟同款，key/env 装配对照 8.24 集成用例 `tests/integration/agentcore/workflow_questioner` 既有装配）；集成门禁规则 7：`//go:build integration && llm` + `suite.Run(` + 真跨 3 子系统（workflow 引擎 + retrieval knowledgebase + foundation llm）+ env 缺 key `s.T().Skip`（跳过仅限 LLM 依赖用例，纯检索模式用例不 Skip）。

- [ ] **Step 6.1: 写集成用例**（骨架，四模式各一子测试）

```go
//go:build integration && llm

package workflow_knowledge_retrieval

import (
	"testing"

	"github.com/stretchrtest/suite" // 对照既有集成套件实际 import
)

// 端到端：真实创建 workflow（Start→kb1→End）→ inputs_schema 映射 →
// vector/hybrid/graph/agentic 四模式真链检索
type KnowledgeRetrievalE2ESuite struct{ suite.Suite }

func (s *KnowledgeRetrievalE2ESuite) TestVector模式_存储提取检索全链() {
	// 1. 真 embedding（glm 免费档 embed 端点或 env 配置）+ InMemory/Chroma store
	// 2. KB 组件配置 index_type=vector；经 KB 原生 API（13.26/13.27）写入文档并断言可读回（存储侧验证）
	// 3. workflow.SetStartComp("start", ...) / AddWorkflowComp("kb1", kbComp,
	//    inputs_schema=map{"query": "start.question"}) / SetEndComp / AddEdge 三连
	// 4. RunWorkflow(inputs={"question": <检索问句>})
	// 5. 断言：end 输出引用 kb1.context 非空且含写入文档关键词；results 非空
}

func (s *KnowledgeRetrievalE2ESuite) TestHybrid模式() { /* 同上，index_type=hybrid */ }
func (s *KnowledgeRetrievalE2ESuite) TestGraph模式() {
	// use_graph=true + GraphKnowledgeBase：写三元组→图检索命中断言（13.28 管线）
}
func (s *KnowledgeRetrievalE2ESuite) TestAgentic模式_真LLM() {
	// retrieval_config.agentic=true + model_id=glm-4.5-flash（ResourceMgr 注册）
	// 验证组件装配层把 Model 正确注入 AgenticRetriever 并完成多跳检索
	// env 缺 LLM key → s.T().Skip("需要免费档 LLM key")
}
func TestKnowledgeRetrievalE2E(t *testing.T) { suite.Run(t, new(KnowledgeRetrievalE2ESuite)) }
```

（具名子测试禁①序号字符；具体 SDK/store 装配细节对照 13.27/13.28 集成用例先例逐字复用其 helper）

- [ ] **Step 6.2: 本地跑** `CGO_ENABLED=1 go test -tags "integration llm" ./tests/integration/agentcore/workflow_knowledge_retrieval/ -v`——期望四模式 PASS（缺 key 仅 agentic Skip）
- [ ] **Step 6.3: 审计自检** `go run ./cmd/integration-audit -verify` 通过
- [ ] **Step 6.4: Commit** `test(8.25): 集成——真实 workflow×四检索模式真链端到端（glm-4.5-flash）`

## Task 7: 第一轮收口——全量回归 + 计划表 + 错误码消费登记

- [ ] **Step 7.1:** `go build ./... && go vet ./...` 无错
- [ ] **Step 7.2:** 全量回归 `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...` 零 FAIL（既有失败对照基线，勿代修）
- [ ] **Step 7.3:** `IMPLEMENTATION_PLAN.md`：8.25 行 `🔄`→`✅`（产出列写 D-25-1..4 + 集成四模式兑现 + 101100-101103 转已消费）；跨域联动注记「8.25 ⤴️ 依赖 13.26-13.28」改 ✅ 兑现；7.36 行 ⤵️ 核销
- [ ] **Step 7.4:** Commit（path-limited）`chore(8.25): 第一轮收口——计划表回填+错误码消费登记`
- [ ] **Step 7.5:** 更新记忆 `rsi-8-25-design.md` → 第一轮完成状态

---

# 第二轮：8.25a MemoryRetrievalComp + 8.25b MemoryWriteComp

## Task 8: MemoryRetrievalComp

**Files:**
- Create: `internal/openuapclaw/core/workflow/components/resource/memory_retrieval_comp.go`
- Test: `memory_retrieval_comp_test.go`
- Modify: `resource/doc.go`（补条目）

- [ ] **Step 8.1: 失败测试**

```go
func TestMemoryRetrieval_validateInputs_空query报错(t *testing.T) { /* COMPONENT_MEMORY_RETRIEVAL_INPUT_PARAM_ERROR；错误码未预置则先按 Python 数值登记（13.2 已拍板随批登记） */ }
func TestMemoryRetrieval_validateInputs_topK默认5(t *testing.T) { /* inputs 无 top_k → 5 */ }
func TestMemoryRetrieval_invoke_双路检索映射(t *testing.T) {
	// 真 ltm.LongTermMemory（7.27 InMemory 构造先例）+ 写入测试记忆
	// 断言 fragment_memory_results/summary_results 序列化形态（model_dump 等价键名）
}
func TestMemoryRetrieval_invoke_阈值与scope透传(t *testing.T) { /* threshold=0.5/user_id/scope_id 传到 SearchUserMem 参数 */ }
func TestMemoryRetrieval_invoke_检索失败包装(t *testing.T) { /* COMPONENT_MEMORY_RETRIEVAL_INVOKE_CALL_FAILED + ERROR 日志字段 */ }
func TestMemoryRetrieval_日志字段完整(t *testing.T) { /* start: query_length/top_k/threshold/user_id/scope_id/sensitive_mode; end: num_results/num_summary_results/sensitive_mode */ }
```

- [ ] **Step 8.2: FAIL → Step 8.3: 实现**（结构同 8.25：Executable 嵌基座 + InvokeFn 槽；配置持 `*ltm.LongTermMemory` 具体指针；`ScopeID/UserID` 默认 `"__default__"`；invoke 十步逐条对齐 Python memory_retrieval_comp.py:75-147；`_format_output` 返回 map 键 `fragment_memory_results/summary_results`）

- [ ] **Step 8.4: PASS → Step 8.5: Commit** `feat(8.25a): 长记忆检索组件（Python memory_retrieval_comp.py 一比一）`

## Task 9: MemoryWriteComp

**Files:**
- Create: `memory_write_comp.go` + `memory_write_comp_test.go`；Modify `resource/doc.go`

- [ ] **Step 9.1: 失败测试**

```go
func TestMemoryWrite_validateInputs_空messages报错(t *testing.T) { /* "Messages list cannot be empty" */ }
func TestMemoryWrite_invoke_写消息含genMem透传(t *testing.T) {
	// 真 LTM + fake LLM 边界（协议边界允许）；gen_mem=True + gen_mem_with_history_msg_num=2 透传 AddMessages
	// timestamp nil→Python 内部 now() 语义对照 add_messages.go
}
func TestMemoryWrite_invoke_成功返回success(t *testing.T) { /* {"success": true} */ }
func TestMemoryWrite_invoke_失败包装(t *testing.T) { /* COMPONENT_MEMORY_WRITE_INVOKE_CALL_FAILED */ }
```

- [ ] **Step 9.2: FAIL → Step 9.3: 实现**（同款结构；invoke 逐条对齐 memory_write_comp.py:99-176；`BaseMessage` 用 llmschema 既有类型）

- [ ] **Step 9.4: PASS → Step 9.5: Commit** `feat(8.25b): 长记忆写入组件（Python memory_write_comp.py 一比一）`

## Task 10: 第二轮收口

- [ ] **Step 10.1:** `go build ./... && go vet ./...` + 全量回归 `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...` 零 FAIL
- [ ] **Step 10.2:** `IMPLEMENTATION_PLAN.md`：8.25a/8.25b → ✅；spec §五 补第二轮偏差（如有）
- [ ] **Step 10.3:** Commit `chore(8.25): 第二轮收口——memory 两件完成登记`；推送远端
- [ ] **Step 10.4:** 更新记忆（8.25 批全完成 + 下一节 8.26）

---

## Self-Review 结论

- **Spec 覆盖**：spec §2.1-2.6 → Task 0-7；§3.1-3.3 → Task 8-10；§四回填 → Task 0.3/7.3/10.2；§五偏差 → D-25-1..4 分挂 Task 2/3；集成硬要求 → Task 6 四模式逐条对应 spec §2.6 四项
- **占位符扫描**：无 TBD/TODO；import 路径 `<module>` 与个别 SDK 形参（StoreCreateOption/KBID 字段名）标注了「落地时对照既有文件」的核实指令，属防漂移核对点而非待实现项
- **类型一致性**：Executable 槽签名与 `components/component.go:54` 一致；`RetrieveMultiKBWithSource` 五参、KB 构造/选项与 base.go 一致；`getModel` 复刻 llm_comp.go:977 先例
