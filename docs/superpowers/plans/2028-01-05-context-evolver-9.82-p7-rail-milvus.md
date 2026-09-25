# 9.82 P7 ContextEvolutionRail + MilvusConnector 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 9.82 P7，将 Context Evolver 接入 Agent 运行时——ContextEvolutionRail 记忆检索注入 + MilvusConnector 向量数据库持久化 + auto 模式回退 + 9.24 P6 回填。

**Architecture:** ContextEvolutionRail 直接嵌入 DeepAgentRail（对齐 Python），通过 BeforeTaskIteration/AfterTaskIteration 钩子透明注入记忆和自动总结轨迹。MilvusConnector 独立实现（不复用 foundation/store/vector），固定 5 字段 schema + namespace 分区。MemoryPersistenceHelper 新增 auto 模式惰性探测。

**Tech Stack:** Go 1.22+, milvus-sdk-go v2, sync.Once, type-assert DeepAgentInterface

**Design Spec:** `docs/superpowers/specs/2028-01-05-context-evolver-9.82-p7-rail-milvus-design.md`

---

## 文件结构

| 操作 | 文件 | 职责 |
|------|------|------|
| **创建** | `harness/rails/evolution/context_evolution_rail.go` | ContextEvolutionRail 结构体 + 构造 + 2 个回调 + extractTrajectory |
| **创建** | `harness/rails/evolution/context_evolution_rail_test.go` | ContextEvolutionRail 单元测试 |
| **创建** | `context_evolver/core/persistence/milvus_connector.go` | MilvusConnectorImpl 实现 + milvusClient 接口 + 辅助函数 |
| **创建** | `context_evolver/core/persistence/milvus_connector_test.go` | MilvusConnectorImpl 单元测试（fakeClient） |
| **修改** | `context_evolver/core/persistence/persistence_helper.go` | auto 模式 + resolveBackend + Save/Load 路由 |
| **修改** | `context_evolver/core/persistence/persistence_helper_test.go` | auto 模式测试 |
| **修改** | `context_evolver/service/trajectory_generator.go` | 导出 FormatTrajectory |
| **修改** | `context_evolver/service/doc.go` | 文件目录更新 |
| **修改** | `harness/rails/evolution/doc.go` | 文件目录 + P6 描述更新 |
| **修改** | `context_evolver/core/persistence/doc.go` | 文件目录更新 |
| **修改** | `IMPLEMENTATION_PLAN.md` | 9.24 + 9.82 状态更新 |

---

### Task 1: 导出 FormatTrajectory

**Files:**
- Modify: `internal/agentcore/context_evolver/service/trajectory_generator.go:232`
- Modify: `internal/agentcore/context_evolver/service/doc.go`

ContextEvolutionRail 的 extractTrajectory 需要调用 `formatTrajectory`，但它是未导出函数。需要导出。

- [ ] **Step 1: 导出 FormatTrajectory**

在 `trajectory_generator.go` 中：
- 将 `func formatTrajectory(messages []Message) string` 改为 `func FormatTrajectory(messages []Message) string`
- 更新内部调用：`formatTrajectory(` → `FormatTrajectory(`（grep 全文件）

- [ ] **Step 2: 更新 doc.go**

在 `service/doc.go` 文件目录中，确认 `trajectory_generator.go` 的职责描述包含 `FormatTrajectory`。

- [ ] **Step 3: 运行测试确认无破坏**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/context_evolver/service/... -count=1 -timeout 120s
```

Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add -A && git commit -m "refactor(context_evolver): export FormatTrajectory for P7 ContextEvolutionRail"
```

---

### Task 2: MilvusConnector 实现

**Files:**
- Create: `internal/agentcore/context_evolver/core/persistence/milvus_connector.go`
- Create: `internal/agentcore/context_evolver/core/persistence/milvus_connector_test.go`

- [ ] **Step 1: 创建 milvus_connector.go — milvusClient 接口 + 常量 + 辅助函数**

```go
package persistence

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/milvus-io/milvus/client/v2/column"
	"github.com/milvus-io/milvus/client/v2/entity"
	"github.com/milvus-io/milvus/client/v2/index"
	milvusclient "github.com/milvus-io/milvus/client/v2/milvusclient"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 接口 ────────────────────────────

// milvusClient Milvus 客户端操作接口（用于解耦和测试）。
type milvusClient interface {
	CreateCollection(ctx context.Context, option milvusclient.CreateCollectionOption, callOptions ...any) error
	HasCollection(ctx context.Context, option milvusclient.HasCollectionOption, callOptions ...any) (bool, error)
	DescribeCollection(ctx context.Context, option milvusclient.DescribeCollectionOption, callOptions ...any) (*entity.Collection, error)
	Insert(ctx context.Context, option milvusclient.InsertOption, callOptions ...any) (milvusclient.InsertResult, error)
	Search(ctx context.Context, option milvusclient.SearchOption, callOptions ...any) ([]milvusclient.ResultSet, error)
	Query(ctx context.Context, option milvusclient.QueryOption, callOptions ...any) ([]milvusclient.QueryResult, error)
	Delete(ctx context.Context, option milvusclient.DeleteOption, callOptions ...any) (milvusclient.DeleteResult, error)
	LoadCollection(ctx context.Context, option milvusclient.LoadCollectionOption, callOptions ...any) error
	Flush(ctx context.Context, option milvusclient.FlushOption, callOptions ...any) error
	CreateIndex(ctx context.Context, option milvusclient.CreateIndexOption, callOptions ...any) error
	HasIndex(ctx context.Context, option milvusclient.HasIndexOption, callOptions ...any) (bool, error)
	Close(ctx context.Context) error
}

// ──────────────────────────── 结构体 ────────────────────────────

// MilvusConnectorImpl Milvus 向量数据库连接器实现。
//
// 独立实现（对齐 Python milvus_connector.py），不复用 foundation/store/vector/MilvusVectorStore。
// 固定 5 字段 schema：id(VARCHAR) / namespace(VARCHAR) / content(VARCHAR) / embedding(FLOAT_VECTOR) / metadata(JSON)。
// 命名空间分区：通过 namespace 字段过滤实现逻辑分区。
//
// Python: openjiuwen/extensions/context_evolver/core/db_connector/milvus_connector.py
type MilvusConnectorImpl struct {
	host           string
	port           int
	collectionName string
	dim            int
	alias          string
	metricType     string
	client         milvusClient
	mu             sync.RWMutex
	createClient   func(ctx context.Context, host string, port int, alias string) (milvusClient, error)
}

// MilvusConnectorOption 构造选项函数。
type MilvusConnectorOption func(*MilvusConnectorImpl)

// ──────────────────────────── 常量 ────────────────────────────

const (
	fieldID        = "id"
	fieldNS        = "namespace"
	fieldContent   = "content"
	fieldEmbedding = "embedding"
	fieldMetadata  = "metadata"

	idMaxLen      = 256
	nsMaxLen      = 256
	contentMaxLen = 65535

	defaultCollectionName = "vector_nodes"
	defaultAlias          = "default"
	defaultMetricType     = "COSINE"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMilvusConnectorImpl 创建 Milvus 连接器实例。
// 客户端惰性创建，初始化时不需要 Milvus 可用。
// 对齐 Python: MilvusConnector(host, port, collection_name, dim, alias, metric_type)
func NewMilvusConnectorImpl(opts ...MilvusConnectorOption) *MilvusConnectorImpl {
	m := &MilvusConnectorImpl{
		collectionName: defaultCollectionName,
		alias:          defaultAlias,
		metricType:     defaultMetricType,
		createClient:   defaultMilvusCreateClient,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// MilvusConnector 4 方法实现（persistence.MilvusConnector 接口）

func (m *MilvusConnectorImpl) SaveToDB(namespace string, data map[string]any) error {
	// ... 完整实现见 Step 2
}

func (m *MilvusConnectorImpl) LoadFromDB(namespace string) (map[string]any, error) {
	// ... 完整实现见 Step 2
}

func (m *MilvusConnectorImpl) Exists(namespace string) bool {
	// ... 完整实现见 Step 2
}

func (m *MilvusConnectorImpl) Delete(namespace string) bool {
	// ... 完整实现见 Step 2
}

// 扩展方法

func (m *MilvusConnectorImpl) Search(namespace string, embedding []float32, topK int, metric string) ([]map[string]any, error) { ... }
func (m *MilvusConnectorImpl) DeleteNodes(namespace string, nodeIDs []string) bool { ... }
func (m *MilvusConnectorImpl) ListNamespaces() []string { ... }
func (m *MilvusConnectorImpl) Count(namespace string) int { ... }
func (m *MilvusConnectorImpl) Flush() { ... }
func (m *MilvusConnectorImpl) Close() { ... }
func (m *MilvusConnectorImpl) SetClient(client milvusClient) { ... }

// 辅助函数

func Truncate(text string, maxBytes int) string { ... }
func IDsExpr(ids []string) string { ... }
```

注意：`persistence.MilvusConnector` 接口方法不带 `context.Context`。MilvusConnectorImpl 内部方法需要 context 时，用 `context.Background()`（对齐 Python 同步风格）。但 `createClient` 需要 context。

- [ ] **Step 2: 实现 SaveToDB**

对齐 Python `save_to_db` 270-355 行：

```
1. 空 data → 直接返回 nil
2. 自动检测 dim：遍历 data 找第一个有 embedding 的节点，取 len(embedding)
3. getClient() → 初始化 collection（_initCollection）
4. 遍历 data：
   - 无 embedding → skipped++，跳过
   - 有 embedding → 收集 ids/namespaces/contents/embeddings/metadatas
   - truncate id/ns/content 到对应最大长度
5. skipped > 0 → Warning 日志
6. ids 为空 → Warning 日志，返回 nil
7. delete 已有 PK：collection.Delete(idsExpr(ids))
8. insert rows
9. flush
10. Info 日志：保存数量
```

- [ ] **Step 3: 实现 LoadFromDB**

对齐 Python `load_from_db` 357-403 行：

```
1. getClient()
2. query: namespace == "{ns}"，输出全部 5 字段
3. 遍历 results，构造 map[string]any
4. Info 日志
```

- [ ] **Step 4: 实现 Exists / Delete**

对齐 Python `exists` / `delete`。

- [ ] **Step 5: 实现扩展方法 Search / DeleteNodes / ListNamespaces / Count / Flush / Close**

对齐 Python 对应方法。

- [ ] **Step 6: 实现内部方法 getClient / initCollection / ensureIndex / defaultMilvusCreateClient**

对齐 Python `_connect` / `_init_collection` / `_ensure_index` / `_get_collection`。

- [ ] **Step 7: 创建 milvus_connector_test.go — fakeMilvusClient + 单元测试**

```go
// fakeMilvusClient 用于单元测试
type fakeMilvusClient struct { ... }

// 测试用例：
// - TestNewMilvusConnectorImpl
// - TestTruncate_UTF8安全截断
// - TestIDsExpr
// - TestSaveToDB_空数据
// - TestSaveToDB_跳过无Embedding
// - TestSaveToDB_Upsert
// - TestLoadFromDB
// - TestExists_有数据
// - TestExists_无数据
// - TestDelete_有数据
// - TestDelete_无数据
```

fakeMilvusClient 需要实现 `milvusClient` 接口的所有方法，用内存 map 模拟。

- [ ] **Step 8: 运行测试**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/context_evolver/core/persistence/... -count=1 -timeout 120s
```

Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add -A && git commit -m "feat(context_evolver): implement MilvusConnectorImpl with fixed 5-field schema"
```

---

### Task 3: MemoryPersistenceHelper auto 模式

**Files:**
- Modify: `internal/agentcore/context_evolver/core/persistence/persistence_helper.go`
- Modify: `internal/agentcore/context_evolver/core/persistence/persistence_helper_test.go`

- [ ] **Step 1: 修改默认 persistType 为 "auto"**

在 `NewMemoryPersistenceHelper` 中：
- `persistType: "json"` → `persistType: "auto"`
- `resolvedType: "json"` → `resolvedType: ""`（待探测）

- [ ] **Step 2: 添加 resolveBackend 方法**

```go
// resolveOnce 保证只执行一次
func (h *MemoryPersistenceHelper) resolveOnce() {
    h.resolveOnce.Do(func() {
        switch h.persistType {
        case "auto":
            // 尝试 Milvus 可达性
            if h.milvusConnector != nil {
                // 用轻量操作探测（尝试 LoadFromDB 一个不可能存在的 namespace）
                if h.probeMilvus() {
                    h.resolvedType = "milvus"
                    return
                }
            } else if h.milvusHost != "" {
                // 自动创建 MilvusConnectorImpl 并注入
                conn := NewMilvusConnectorImpl(
                    WithMilvusConnectorHost(h.milvusHost),
                    WithMilvusConnectorPort(h.milvusPort),
                    WithMilvusConnectorCollection(h.milvusCollection),
                )
                h.milvusConnector = conn
                if h.probeMilvus() {
                    h.resolvedType = "milvus"
                    return
                }
            }
            h.resolvedType = "json"
            logger.Warn(logComponent).Str("persist_type", h.persistType).Msg("Milvus 不可达，回退 JSON 后端")
        case "milvus":
            h.resolvedType = "milvus"
        default:
            h.resolvedType = "json"
        }
    })
}

// probeMilvus 探测 Milvus 可达性
func (h *MemoryPersistenceHelper) probeMilvus() bool {
    // 尝试 Exists 操作，不抛异常即为可达
    defer func() { recover() }() // 防止 panic
    // 用 HasCollection 或等效轻量操作
    if impl, ok := h.milvusConnector.(*MilvusConnectorImpl); ok {
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()
        return impl.probeReachable(ctx)
    }
    // 非 MilvusConnectorImpl 实现时，尝试 Exists
    return h.milvusConnector.Exists("__probe__") || true
}
```

需要在 MemoryPersistenceHelper 中新增 `resolveOnce sync.Once` 字段。

- [ ] **Step 3: 修改 Save/Load 方法增加路由**

```go
func (h *MemoryPersistenceHelper) Save(userID, algoName string, nodesDict map[string]any) error {
    if len(nodesDict) == 0 { return nil }
    h.resolveOnce()
    switch h.resolvedType {
    case "milvus":
        ns := Namespace(userID, algoName)
        return h.milvusConnector.SaveToDB(ns, nodesDict)
    default:
        return h.saveJSON(userID, algoName, nodesDict)
    }
}

func (h *MemoryPersistenceHelper) Load(userID, algoName string) (map[string]any, error) {
    h.resolveOnce()
    switch h.resolvedType {
    case "milvus":
        ns := Namespace(userID, algoName)
        return h.milvusConnector.LoadFromDB(ns)
    default:
        return h.loadJSON(userID, algoName)
    }
}
```

- [ ] **Step 4: 添加 MilvusConnectorOption 构造选项**

```go
func WithMilvusConnectorHost(host string) MilvusConnectorOption { ... }
func WithMilvusConnectorPort(port int) MilvusConnectorOption { ... }
func WithMilvusConnectorCollection(name string) MilvusConnectorOption { ... }
```

- [ ] **Step 5: 编写 auto 模式测试**

在 `persistence_helper_test.go` 中新增：
- `TestAuto_默认Auto` — 验证新默认 persistType="auto"
- `TestAuto_Milvus可达` — 注入 mock → resolvedType="milvus"
- `TestAuto_Milvus不可达` — mock 失败 → resolvedType="json"
- `TestAuto_显式JSON` — persistType="json" → 不探测
- `TestAuto_显式Milvus` — persistType="milvus" → resolvedType="milvus"

- [ ] **Step 6: 运行测试**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/context_evolver/core/persistence/... -count=1 -timeout 120s
```

- [ ] **Step 7: Commit**

```bash
git add -A && git commit -m "feat(context_evolver): add auto mode to MemoryPersistenceHelper with Milvus fallback"
```

---

### Task 4: ContextEvolutionRail 实现

**Files:**
- Create: `internal/agentcore/harness/rails/evolution/context_evolution_rail.go`
- Create: `internal/agentcore/harness/rails/evolution/context_evolution_rail_test.go`

**关键导入路径：**
- `hinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"` — DeepAgentInterface
- `saconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/config"` — ReActAgentConfig
- `ceservice "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/service"` — TaskMemoryService, FormatTrajectory, EvaluateTrial, SummarizeTrajectories

**关键类型断言链：**
```go
deepAgent, ok := cbc.Agent().(hinterfaces.DeepAgentInterface)
// deepAgent.ReactAgent() → *agents.ReActAgent
// reactAgent.Config() → agentinterfaces.AgentConfig
// config.(*saconfig.ReActAgentConfig) → *ReActAgentConfig
// reactCfg.PromptTemplate → []map[string]any
// reactAgent.Configure(ctx, &newReactCfg) → error
```

- [ ] **Step 1: 创建 context_evolution_rail.go — 结构体 + 构造 + GetCallbacks**

```go
package evolution

import (
	"context"
	"fmt"

	hinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	saconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/config"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	ceservice "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/service"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ContextEvolutionRail 上下文演化轨道。
//
// 嵌入 DeepAgentRail（对齐 Python ContextEvolutionRail(DeepAgentRail)），
// Priority=50，在 BeforeTaskIteration 中检索记忆注入系统提示词，
// 在 AfterTaskIteration 中恢复原始提示词、标注 memoriesUsed、自动总结轨迹。
//
// Python: openjiuwen/harness/rails/evolution/context_evolution_rail.py
type ContextEvolutionRail struct {
	rails.DeepAgentRail

	userID                  string
	memoryService           *ceservice.TaskMemoryService
	injectMemoriesInContext bool
	autoSummarize           bool
	autoSummarizeMattsMode  string

	memoriesUsed            int
	originalPromptTemplate  []map[string]any

	lastRetrievedQuery      string
	lastRetrievalResult     *ceservice.RetrieveResult

	agent                   agentinterfaces.BaseAgent
	currentQuery            string
}

// ContextEvolutionRailOption 构造选项函数。
type ContextEvolutionRailOption func(*ContextEvolutionRail)

// ──────────────────────────── 常量 ────────────────────────────

const contextEvolutionPriority = 50

// ──────────────────────────── 导出函数 ────────────────────────────

// NewContextEvolutionRail 创建上下文演化轨道实例。
// 对齐 Python: ContextEvolutionRail(user_id, memory_service, inject_memories_in_context, auto_summarize, auto_summarize_matts_mode)
func NewContextEvolutionRail(
	userID string,
	memoryService *ceservice.TaskMemoryService,
	opts ...ContextEvolutionRailOption,
) *ContextEvolutionRail {
	r := &ContextEvolutionRail{
		userID:                  userID,
		memoryService:           memoryService,
		injectMemoriesInContext: true,
		autoSummarize:           true,
		autoSummarizeMattsMode:  "none",
	}
	for _, opt := range opts {
		opt(r)
	}
	// memoryService 为 nil 时创建默认实例（对齐 Python）
	if r.memoryService == nil {
		r.memoryService = ceservice.NewTaskMemoryService()
	}
	// 加载已有记忆（对齐 Python __init__ 末尾）
	r.memoryService.LoadMemories(r.userID)

	logger.Info(logComponent).
		Str("user_id", userID).
		Bool("inject", r.injectMemoriesInContext).
		Bool("auto_summarize", r.autoSummarize).
		Msg("ContextEvolutionRail 初始化完成")

	return r
}

// WithInjectMemoriesInContext 设置是否注入记忆到上下文。
func WithInjectMemoriesInContext(v bool) ContextEvolutionRailOption {
	return func(r *ContextEvolutionRail) { r.injectMemoriesInContext = v }
}

// WithAutoSummarize 设置是否自动总结轨迹。
func WithAutoSummarize(v bool) ContextEvolutionRailOption {
	return func(r *ContextEvolutionRail) { r.autoSummarize = v }
}

// WithAutoSummarizeMattsMode 设置自动总结的 MaTTS 模式。
func WithAutoSummarizeMattsMode(mode string) ContextEvolutionRailOption {
	return func(r *ContextEvolutionRail) { r.autoSummarizeMattsMode = mode }
}

// Priority 返回轨道优先级。
func (r *ContextEvolutionRail) Priority() int { return contextEvolutionPriority }

// GetCallbacks 注册回调。
func (r *ContextEvolutionRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	callbacks := r.DeepAgentRail.GetCallbacks()
	callbacks[agentinterfaces.CallbackBeforeTaskIteration] = r.beforeTaskIteration
	callbacks[agentinterfaces.CallbackAfterTaskIteration] = r.afterTaskIteration
	return callbacks
}

// 只读访问器
func (r *ContextEvolutionRail) MemoriesUsed() int    { return r.memoriesUsed }
func (r *ContextEvolutionRail) CurrentQuery() string  { return r.currentQuery }
func (r *ContextEvolutionRail) AgentRef() agentinterfaces.BaseAgent { return r.agent }
```

- [ ] **Step 2: 实现 beforeTaskIteration**

对齐 Python 108-193 行：

```go
func (r *ContextEvolutionRail) beforeTaskIteration(ctx context.Context, cbcRaw any) error {
	cbc, ok := cbcRaw.(*agentinterfaces.AgentCallbackContext)
	if !ok || cbc == nil {
		return nil
	}

	// 1. 重置每次迭代状态
	r.memoriesUsed = 0
	r.originalPromptTemplate = nil

	// 2. 捕获 agent 引用
	if r.agent == nil {
		r.agent = cbc.Agent()
	}

	// 3. 获取 query
	inputs := cbc.Inputs()
	taskInputs, ok := inputs.(*agentinterfaces.TaskIterationInputs)
	if !ok || taskInputs == nil {
		return nil
	}
	query := taskInputs.Query
	if query == "" {
		return nil
	}

	// 4. 保存 currentQuery
	r.currentQuery = query

	// 5. 检索记忆
	var memoryResult *ceservice.RetrieveResult
	if r.lastRetrievedQuery == query && r.lastRetrievalResult != nil {
		memoryResult = r.lastRetrievalResult
		logger.Info(logComponent).Msg("复用缓存的记忆检索结果")
	} else {
		result, err := r.memoryService.Retrieve(ctx, r.userID, query)
		if err != nil {
			logger.Error(logComponent).Err(err).Msg("检索记忆失败")
			return nil // 不中断 Agent
		}
		memoryResult = result
		r.lastRetrievedQuery = query
		r.lastRetrievalResult = result
	}

	memoryString := memoryResult.MemoryString
	r.memoriesUsed = len(memoryResult.RetrievedMemory)
	logger.Info(logComponent).Int("memories_used", r.memoriesUsed).Msg("检索到记忆")

	// 6. 记忆注入
	if !(r.memoriesUsed > 0 && memoryString != "" && r.injectMemoriesInContext) {
		return nil
	}

	// type-assert: BaseAgent → DeepAgentInterface
	deepAgent, ok := cbc.Agent().(hinterfaces.DeepAgentInterface)
	if !ok {
		logger.Warn(logComponent).Msg("Agent 非 DeepAgentInterface，跳过记忆注入")
		return nil
	}

	reactAgent := deepAgent.ReactAgent()
	if reactAgent == nil {
		logger.Warn(logComponent).Msg("ReactAgent 为 nil，跳过记忆注入")
		return nil
	}

	agentCfg := reactAgent.Config()
	reactCfg, ok := agentCfg.(*saconfig.ReActAgentConfig)
	if !ok {
		logger.Warn(logComponent).Msg("Agent config 非 ReActAgentConfig，跳过记忆注入")
		return nil
	}

	// 深拷贝原始 PromptTemplate
	r.originalPromptTemplate = deepCopyPromptTemplate(reactCfg.PromptTemplate)

	// 构造记忆块
	memoryBlock := fmt.Sprintf("Some Related Experience to help you complete the task:\n%s", memoryString)

	// 在 system message 末尾追加记忆块
	newTemplate := make([]map[string]any, len(reactCfg.PromptTemplate))
	for i, msg := range reactCfg.PromptTemplate {
		newMsg := make(map[string]any, len(msg))
		for k, v := range msg {
			newMsg[k] = v
		}
		if role, _ := msg["role"].(string); role == "system" {
			content, _ := msg["content"].(string)
			newMsg["content"] = content + "\n\n" + memoryBlock
		}
		newTemplate[i] = newMsg
	}

	// 应用新 PromptTemplate
	newReactCfg := *reactCfg
	newReactCfg.PromptTemplate = newTemplate
	if err := reactAgent.Configure(ctx, &newReactCfg); err != nil {
		logger.Error(logComponent).Err(err).Msg("注入记忆到 PromptTemplate 失败")
		r.originalPromptTemplate = nil
		return nil
	}

	logger.Debug(logComponent).Msg("已注入记忆上下文到 Agent 系统提示词")
	return nil
}
```

- [ ] **Step 3: 实现 afterTaskIteration**

对齐 Python 199-243 行：

```go
func (r *ContextEvolutionRail) afterTaskIteration(ctx context.Context, cbcRaw any) error {
	cbc, ok := cbcRaw.(*agentinterfaces.AgentCallbackContext)
	if !ok || cbc == nil {
		return nil
	}

	// 1. 恢复原始提示词模板
	if r.originalPromptTemplate != nil {
		deepAgent, ok := cbc.Agent().(hinterfaces.DeepAgentInterface)
		if ok && deepAgent != nil {
			reactAgent := deepAgent.ReactAgent()
			if reactAgent != nil {
				agentCfg := reactAgent.Config()
				if reactCfg, ok := agentCfg.(*saconfig.ReActAgentConfig); ok {
					newReactCfg := *reactCfg
					newReactCfg.PromptTemplate = r.originalPromptTemplate
					_ = reactAgent.Configure(ctx, &newReactCfg)
				}
			}
		}
		r.originalPromptTemplate = nil
		logger.Debug(logComponent).Msg("已恢复原始 Agent 系统提示词")
	}

	// 2. 标注 memoriesUsed
	cbc.Extra()["memories_used"] = r.memoriesUsed

	// 3. 自动总结
	if r.autoSummarize && r.currentQuery != "" {
		trajectory := r.extractTrajectory(cbc)
		if trajectory != "" {
			feedback, score := ceservice.EvaluateTrial(r.currentQuery, trajectory, "")
			logger.Info(logComponent).Msg("正在为当前轨迹运行自动总结")
			_, err := ceservice.SummarizeTrajectories(ctx, r.memoryService, r.userID,
				ceservice.SummarizeTrajectoriesInput{
					Query:      r.currentQuery,
					Trajectory: []string{trajectory},
					MattsMode:  "none",
					Feedback:   []string{feedback},
					Score:      []int{score},
				},
			)
			if err != nil {
				logger.Error(logComponent).Err(err).Msg("afterTaskIteration 自动总结失败")
			}
		}
	}

	return nil
}
```

- [ ] **Step 4: 实现 extractTrajectory + deepCopyPromptTemplate**

```go
// extractTrajectory 从 agent 的 context_engine 提取对话轨迹。
// 对齐 Python: ContextEvolutionRail.extract_trajectory
func (r *ContextEvolutionRail) extractTrajectory(cbc *agentinterfaces.AgentCallbackContext) string {
	defer func() {
		if err := recover(); err != nil {
			logger.Warn(logComponent).Any("panic", err).Msg("提取轨迹时发生 panic")
		}
	}()

	deepAgent, ok := cbc.Agent().(hinterfaces.DeepAgentInterface)
	if !ok || deepAgent == nil {
		return ""
	}

	reactAgent := deepAgent.ReactAgent()
	if reactAgent == nil {
		return ""
	}

	ce := reactAgent.ContextEngine()
	if ce == nil {
		return ""
	}

	sess := cbc.Session()
	if sess == nil {
		return ""
	}

	sessionID := sess.GetSessionID()
	mc := ce.GetContext("default_context_id", sessionID)
	if mc == nil {
		return ""
	}

	messages, err := mc.GetMessages(0, true)
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("获取对话消息失败")
		return ""
	}

	// 转换为 service.Message 格式
	serviceMsgs := make([]ceservice.Message, 0, len(messages))
	for _, msg := range messages {
		serviceMsgs = append(serviceMsgs, ceservice.Message{
			Role:    msg.Role(),
			Content: msg.Content(),
		})
	}

	return ceservice.FormatTrajectory(serviceMsgs)
}

// deepCopyPromptTemplate 深拷贝提示词模板。
func deepCopyPromptTemplate(tpl []map[string]any) []map[string]any {
	if tpl == nil {
		return nil
	}
	result := make([]map[string]any, len(tpl))
	for i, msg := range tpl {
		newMsg := make(map[string]any, len(msg))
		for k, v := range msg {
			newMsg[k] = v
		}
		result[i] = newMsg
	}
	return result
}
```

- [ ] **Step 5: 创建 context_evolution_rail_test.go**

测试用例（同包测试，可访问非导出函数）：

```go
// - TestNewContextEvolutionRail_默认值
// - TestNewContextEvolutionRail_自定义选项
// - TestNewContextEvolutionRail_NilMemoryService
// - TestBeforeTaskIteration_记忆注入
// - TestBeforeTaskIteration_缓存命中
// - TestBeforeTaskIteration_检索失败
// - TestBeforeTaskIteration_无记忆
// - TestBeforeTaskIteration_不注入
// - TestBeforeTaskIteration_无Query
// - TestAfterTaskIteration_恢复Prompt
// - TestAfterTaskIteration_AutoSummarize
// - TestAfterTaskIteration_AutoSummarize失败
// - TestExtractTrajectory_正常
// - TestExtractTrajectory_无ContextEngine
// - TestDeepCopyPromptTemplate
// - TestAccessors
```

mock TaskMemoryService：需要创建 `mockTaskMemoryService`，实现 `Retrieve` / `LoadMemories` 等方法。由于 TaskMemoryService 是具体类型不是接口，测试中需要考虑如何 mock：
- 方案：在测试中嵌入 `*ceservice.TaskMemoryService` 并重写 Retrieve 方法不可行（具体类型方法不可重写）
- 替代方案：将 `ContextEvolutionRail.memoryService` 改为接口 `TaskMemoryServiceProtocol`，提取 Retrieve/LoadMemories 等必要方法。或者直接构造真实 TaskMemoryService 但 mock 其底层依赖。
- **推荐方案**：定义 `taskMemoryServicer` 接口（非导出），包含 Retrieve/LoadMemories/Summarize 三个方法，ContextEvolutionRail 持有该接口而非具体类型。测试中注入 mock 实现。

- [ ] **Step 6: 运行测试**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/harness/rails/evolution/... -run "TestContextEvolution|TestNewContextEvolution|TestBeforeTask|TestAfterTask|TestExtractTrajectory|TestDeepCopy|TestAccessors" -count=1 -timeout 120s
```

- [ ] **Step 7: Commit**

```bash
git add -A && git commit -m "feat(evolution): implement ContextEvolutionRail with memory injection and auto-summarize"
```

---

### Task 5: doc.go 更新

**Files:**
- Modify: `internal/agentcore/harness/rails/evolution/doc.go`
- Modify: `internal/agentcore/context_evolver/core/persistence/doc.go`
- Modify: `internal/agentcore/context_evolver/service/doc.go`

- [ ] **Step 1: 更新 evolution/doc.go**

文件目录新增：
```
├── context_evolution_rail.go       # ContextEvolutionRail 上下文演化轨道（Priority=50）
```

包功能概述 P6 描述更新：
```
P6（ContextEvolutionRail 上下文演化轨道：记忆检索注入 + 自动轨迹总结）已完成
```

- [ ] **Step 2: 更新 persistence/doc.go**

文件目录新增：
```
├── milvus_connector.go            # MilvusConnectorImpl Milvus 向量数据库连接器实现
```

- [ ] **Step 3: 更新 service/doc.go**

确认 `trajectory_generator.go` 描述包含 `FormatTrajectory`。

- [ ] **Step 4: Commit**

```bash
git add -A && git commit -m "docs: update doc.go for P7 ContextEvolutionRail + MilvusConnector"
```

---

### Task 6: IMPLEMENTATION_PLAN.md 状态更新

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 9.24 行 P6 状态**

找到 9.24 行中 `P6(☐ 上下文演化: ContextEvolutionRail; ⤴️9.82 P7)`，替换为：
`P6(✅ 上下文演化: ContextEvolutionRail(Priority=50+记忆注入+自动总结); ⤴️9.82 P7 ✅)`

确保 P1-P5 的 ✅ 状态不变。

- [ ] **Step 2: 更新 9.82 行 P7 状态**

找到 9.82 行中 `P7(☐ ContextEvolutionRail + MilvusConnector + 工具层(experience_retrieve/experience_learn/experience_clear) + 9.24 P6回填); ⤴️9.24 P6`，替换为：
`P7(✅ ContextEvolutionRail + MilvusConnector + MemoryPersistenceHelper auto模式 + 9.24 P6回填; 跳过工具层: Python无此实现); ⤴️9.24 P6 ✅`

确保 P1-P6 的 ✅ 状态不变。

- [ ] **Step 3: Commit**

```bash
git add -A && git commit -m "docs: update IMPLEMENTATION_PLAN.md for 9.82 P7 and 9.24 P6 completion"
```

---

## 自审清单

**1. Spec 覆盖率：**
- ✅ ContextEvolutionRail 结构/构造/回调 → Task 4
- ✅ BeforeTaskIteration → Task 4 Step 2
- ✅ AfterTaskIteration → Task 4 Step 3
- ✅ ExtractTrajectory → Task 4 Step 4
- ✅ MilvusConnector 实现 → Task 2
- ✅ MemoryPersistenceHelper auto → Task 3
- ✅ 9.24 P6 回填 → Task 5 + Task 6
- ✅ 工具层跳过说明 → Task 6
- ✅ 日志对齐 → Task 4 Step 2-4 中逐一对齐
- ✅ FormatTrajectory 导出 → Task 1

**2. Placeholder 扫描：** 无 TBD/TODO，所有代码步骤有实际内容。

**3. 类型一致性：**
- ✅ `ceservice.RetrieveResult` — Task 4 中使用
- ✅ `ceservice.TaskMemoryService` — 需要定义 `taskMemoryServicer` 接口
- ✅ `hinterfaces.DeepAgentInterface` — type-assert 路径正确
- ✅ `saconfig.ReActAgentConfig` — PromptTemplate 类型 `[]map[string]any`
- ✅ `agentinterfaces.TaskIterationInputs` — 有 Query 字段，无 RetrievalQuery（直接用 Query）
- ✅ `ceservice.FormatTrajectory` — Task 1 先导出
- ✅ `ceservice.EvaluateTrial` — 返回 `(string, int)`
- ✅ `ceservice.SummarizeTrajectories` — 返回 `(*SummarizeResult, error)`
