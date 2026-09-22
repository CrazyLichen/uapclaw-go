# 9.82 Context Evolver P1 核心框架设计

## 概述

9.82 Context Evolver 是上下文记忆演化系统，让 Agent 具备"上下文记忆"能力——从任务执行轨迹中提取经验，以结构化记忆形式持久化，并在后续任务中检索注入。包含 3 条独立的记忆算法管线（ACE / ReasoningBank / ReMe），P1 实现所有管线的共享运行时基础设施。

### 在 Agent 会话中的流程位置

```
DeepAgent 会话循环
  ├── 任务循环 (TaskLoopController/LoopCoordinator)
  │     └── 每轮迭代
  │           ├── Rails 前置处理
  │           │     ├── SecurityRail
  │           │     ├── SkillUseRail
  │           │     ├── MemoryRail
  │           │     ├── EvolutionRail (9.24)
  │           │     │     └── P6: ContextEvolutionRail ← ⤴️9.82 P7 回填点
  │           │     └── ...
  │           ├── LLM 调用
  │           ├── 工具执行
  │           └── Rails 后置处理
  └── 会话结束 → 轨迹收集 → 演化系统
        ├── EvolutionRail (9.24) → 技能/经验演化
        └── ContextEvolutionRail (9.82 P7) → 上下文记忆演化 ← 依赖 9.82 P1-P6
```

### 和 SkillEvolutionRail 的区别

| 维度 | SkillEvolutionRail (9.24) | ContextEvolutionRail (9.82 P7) |
|------|---------------------------|-------------------------------|
| 演化对象 | 工具描述、指令文本、技能包 | 任务执行经验知识 |
| 存储形式 | YAML 技能文件 (EvolutionStore) | 向量化记忆节点 (VectorStore → JSON/Milvus) |
| 检索方式 | 按技能名精确匹配 | 向量相似度检索 |
| 执行模式 | 异步后台（BackgroundTask） | 串行 await（after_task_iteration） |
| 需要审批 | 是 | 否，直接写入 |
| 类比 | "改进你的工具箱说明书" | "积累你的工作日记" |

### 对话历史 vs Context Evolver 记忆

| 维度 | 对话历史上下文 | Context Evolver 记忆 |
|------|--------------|---------------------|
| 生命周期 | 随会话结束而消失 | 跨会话持久化（JSON/Milvus） |
| 内容 | 原始消息序列 | 蒸馏后的经验知识 |
| 大小 | 线性增长，可能很长 | 精炼，每条记忆几百字 |
| 检索方式 | 全量放入 LLM 上下文窗口 | 向量相似度检索，只注入相关的几条 |

### 检索 query

检索 query 就是当前轮次用户输入的任务问题。`ContextEvolutionRail.before_task_iteration` 中：
- `query = ctx.inputs.query`
- `retrieval_query = ctx.inputs.retrieval_query or query`（MaTTS sequential 模式下用原始问题而非 refine 后的长 prompt 去检索）

### auto_summarize 执行模式

对齐 Python，串行 await。`after_task_iteration` 中直接 await `summarize_trajectories`，下一轮迭代必须等它跑完。默认关闭（`auto_summarize=False`）。这样设计是因为下一轮检索需要用到刚写入的记忆，异步写入会导致记忆注入不一致。

---

## 设计决策

| 决策点 | 结论 | 原因 |
|--------|------|------|
| 目录位置 | `internal/agentcore/context_evolver/` | 和 sys_operation 保持一致 |
| config 模块 | 不实现，复用 Go 项目已有 config 包 | Python 的 `core/config.py` 也是自建的，Go 项目有成熟 config 包 |
| Message/Role | 不实现，复用 `agentcore/foundation/llm/schema` 的 `BaseMessage`/`RoleType` | Python 的 `core/schema/message.py` 定义了自己的 Message/Role 但业务代码未使用，实际用 `openjiuwen.core.foundation.llm` 的消息类型 |
| 操作组合方式 | 方法链 `Then`/`With` | 用户选择方案 B |
| ServiceContext | 非单例，依赖注入 | Go 惯用法，Python 用 `__new__` 单例不适配 |
| RuntimeContext | `map[string]any` + 泛型 `GetTyped[T]` | Python 用 `__getattr__`/`__setattr__` 动态属性，Go 无此特性 |
| MemoryVectorStore | 手写余弦相似度 | 纯 `math` 包，无外部依赖 |
| MilvusConnector | P1 只定义接口，实现在 P7 | Milvus 依赖外部服务，和 ContextEvolutionRail 一起实现 |
| auto_summarize | 对齐 Python，串行 await，默认关闭 | 下一轮检索需要新记忆，异步会不一致 |

---

## 包结构

```
internal/agentcore/context_evolver/
├── doc.go                                # 包文档
├── core/
│   ├── doc.go
│   ├── context/
│   │   ├── doc.go
│   │   ├── runtime_context.go           # RuntimeContext
│   │   └── service_context.go           # ServiceContext
│   ├── op/
│   │   ├── doc.go
│   │   ├── base_op.go                   # BaseOp 接口 + Seq/Par 工厂 + Then/With 方法链
│   │   ├── sequential_op.go             # SequentialOp
│   │   └── parallel_op.go              # ParallelOp
│   ├── schema/
│   │   ├── doc.go
│   │   └── vector_node.go              # VectorNode
│   ├── vector_store/
│   │   ├── doc.go
│   │   └── memory_vector_store.go      # MemoryVectorStore
│   ├── file_connector/
│   │   ├── doc.go
│   │   └── json_file_connector.go      # JSONFileConnector
│   └── persistence/
│       ├── doc.go
│       └── persistence_helper.go        # MemoryPersistenceHelper
```

对应 Python 源码路径：`openjiuwen/extensions/context_evolver/`

---

## 核心类型设计

### 1. RuntimeContext

对齐 Python `core/context/runtime_context.py`。

```go
// RuntimeContext 操作间传递中间结果的上下文。
//
// Python 用 __getattr__/__setattr__ 动态属性访问，Go 改为 map + 方法对。
// Python: context.user_id = "alice" → Go: context.Set("user_id", "alice")
//
// Python: openjiuwen/extensions/context_evolver/core/context/runtime_context.py
type RuntimeContext struct {
    mu   sync.RWMutex
    data map[string]any
}

func NewRuntimeContext() *RuntimeContext

// Set 设置键值
func (rc *RuntimeContext) Set(key string, value any)

// Get 获取值
func (rc *RuntimeContext) Get(key string) any

// GetDefault 获取值，不存在时返回默认值
func (rc *RuntimeContext) GetDefault(key string, defaultVal any) any

// GetTyped 泛型获取，带类型断言
func GetTyped[T any](rc *RuntimeContext, key string) (T, bool)

// ToDict 返回快照副本
func (rc *RuntimeContext) ToDict() map[string]any
```

### 2. ServiceContext

对齐 Python `core/context/service_context.py`。Python 用 `__new__` 单例 → Go 用依赖注入。

```go
// ServiceContext 管理共享服务（LLM/Embedding/VectorStore）的上下文。
//
// Python 用 __new__ 单例模式，Go 改为依赖注入——由调用方构造并传入。
//
// Python: openjiuwen/extensions/context_evolver/core/context/service_context.py
type ServiceContext struct {
    services map[string]any
}

func NewServiceContext() *ServiceContext

// RegisterService 注册服务
func (sc *ServiceContext) RegisterService(name string, svc any)

// GetService 获取服务
func (sc *ServiceContext) GetService(name string) any

// LLM 获取 LLM 服务
func (sc *ServiceContext) LLM() any

// EmbeddingModel 获取 Embedding 模型服务
func (sc *ServiceContext) EmbeddingModel() any

// VectorStore 获取 VectorStore 服务
func (sc *ServiceContext) VectorStore() *MemoryVectorStore

// Clear 清除所有服务
func (sc *ServiceContext) Clear()
```

### 3. BaseOp + SequentialOp + ParallelOp

对齐 Python `core/op/base_op.py`、`sequential_op.py`、`parallel_op.py`。
Python 用 `>>` 和 `|` 运算符重载 → Go 用 `Then`/`With` 方法链。

```go
// BaseOp 操作接口。
//
// 操作是可组合的计算原子，通过 Then（顺序）和 With（并行）组合成流水线。
// Python 用 __rshift__ (>>) 和 __or__ (|) 运算符，Go 用方法链。
//
// Python: openjiuwen/extensions/context_evolver/core/op/base_op.py
type BaseOp interface {
    // Execute 执行操作，读写 RuntimeContext
    Execute(ctx context.Context, rc *RuntimeContext) error
}

// Seq 单操作包装为 SequentialOp（链式起点）
func Seq(op BaseOp) *SequentialOp

// Par 单操作包装为 ParallelOp（链式起点）
func Par(op BaseOp) *ParallelOp

// SequentialOp 顺序组合。
//
// 按 ops 顺序依次执行，任一失败立即返回 error。
// Then 支持扁平化嵌套的 SequentialOp（对齐 Python SequentialOp.__rshift__）。
//
// Python: openjiuwen/extensions/context_evolver/core/op/sequential_op.py
type SequentialOp struct {
    ops []BaseOp
}

// Then 追加操作，返回扩展后的 SequentialOp
func (s *SequentialOp) Then(other BaseOp) *SequentialOp

// Execute 顺序执行所有操作
func (s *SequentialOp) Execute(ctx context.Context, rc *RuntimeContext) error

// ParallelOp 并行组合。
//
// 使用 errgroup.Group 并行执行，任一失败 cancel 其余。
// 共享 RuntimeContext（需调用方保证并发安全）。
// With 支持扁平化嵌套的 ParallelOp（对齐 Python ParallelOp.__or__）。
//
// Python: openjiuwen/extensions/context_evolver/core/op/parallel_op.py
type ParallelOp struct {
    ops []BaseOp
}

// With 追加并行操作，返回扩展后的 ParallelOp
func (p *ParallelOp) With(other BaseOp) *ParallelOp

// Execute 并行执行所有操作
func (p *ParallelOp) Execute(ctx context.Context, rc *RuntimeContext) error
```

使用示例（对齐 Python ACE summary flow）：

```go
// Python:
// flow = LoadPlaybookOp() >> (ReflectOp() | ParallelReflectOp()) >> ApplyDeltaOp()

// Go:
flow := Seq(loadPlaybookOp).
    Then(Par(reflectOp).With(parallelReflectOp)).
    Then(applyDeltaOp)

err := flow.Execute(ctx, runtimeCtx)
```

### 4. VectorNode

对齐 Python `core/schema/vector_node.py`。

```go
// VectorNode 向量存储标准序列化格式。
//
// 所有记忆类型（ACE/ReasoningBank/ReMe）通过此格式统一序列化。
// embedding 使用 float64（Go 无 float32 向量库生态约束）。
//
// Python: openjiuwen/extensions/context_evolver/core/schema/vector_node.py
type VectorNode struct {
    // ID 唯一标识
    ID string `json:"id"`
    // Content 文本内容（用于 embedding）
    Content string `json:"content"`
    // Embedding 向量嵌入
    Embedding []float64 `json:"embedding,omitempty"`
    // Metadata 附加元数据
    Metadata map[string]any `json:"metadata"`
}

func NewVectorNode(id, content string, embedding []float64, metadata map[string]any) *VectorNode

// ToDict 转换为字典
func (n *VectorNode) ToDict() map[string]any

// VectorNodeFromDict 从字典创建 VectorNode
func VectorNodeFromDict(data map[string]any) (*VectorNode, error)
```

### 5. MemoryVectorStore

对齐 Python `core/vector_store/memory_vector_store.py`。Python 用 numpy 算余弦 → Go 手写。

```go
// MemoryVectorStore 内存向量库，使用余弦相似度搜索。
//
// Python 用 numpy 计算余弦相似度，Go 手写（点积 / 范数乘积），纯 math 包。
//
// Python: openjiuwen/extensions/context_evolver/core/vector_store/memory_vector_store.py
type MemoryVectorStore struct {
    mu      sync.RWMutex
    vectors map[string]*VectorNode
}

func NewMemoryVectorStore() *MemoryVectorStore

// Upsert 插入或更新向量节点
func (s *MemoryVectorStore) Upsert(ctx context.Context, node *VectorNode) error

// Search 向量相似度搜索（余弦相似度）
func (s *MemoryVectorStore) Search(ctx context.Context, embedding []float64, topK int, metadataFilter map[string]any) ([]*VectorNode, error)

// Delete 删除向量节点
func (s *MemoryVectorStore) Delete(ctx context.Context, nodeID string) (bool, error)

// Clear 清除所有向量
func (s *MemoryVectorStore) Clear()

// Count 获取向量数量
func (s *MemoryVectorStore) Count() int

// GetAll 获取所有向量，可选元数据过滤
func (s *MemoryVectorStore) GetAll(metadataFilter map[string]any) []*VectorNode

// LoadNode 直接加载单个向量节点（用于反序列化）
func (s *MemoryVectorStore) LoadNode(nodeID string, node *VectorNode)

// LoadFromDict 从字典加载多个向量节点
func (s *MemoryVectorStore) LoadFromDict(data map[string]map[string]any) error
```

### 6. JSONFileConnector

对齐 Python `core/file_connector/json_file_connector.py`。
Go 无需 `safe_model_dump`（Python 那个是 Pydantic 版本兼容函数），Go 结构体直接 `json.Marshal`。

```go
// JSONFileConnector 通用 JSON 文件 I/O 连接器。
//
// 处理文件读写操作，对数据结构无关——调用方负责序列化/反序列化。
// 自动创建父目录，UTF-8 编码。
//
// Python: openjiuwen/extensions/context_evolver/core/file_connector/json_file_connector.py
type JSONFileConnector struct {
    indent      int
    ensureASCII bool
}

// JSONFileConnectorOption JSON 文件连接器配置选项
type JSONFileConnectorOption func(*JSONFileConnector)

func NewJSONFileConnector(opts ...JSONFileConnectorOption) *JSONFileConnector

// SaveToFile 保存字典数据到 JSON 文件（自动创建父目录）
func (c *JSONFileConnector) SaveToFile(filePath string, data map[string]any) error

// LoadFromFile 从 JSON 文件加载字典数据
func (c *JSONFileConnector) LoadFromFile(filePath string) (map[string]any, error)

// Exists 检查文件是否存在
func (c *JSONFileConnector) Exists(filePath string) bool

// Delete 删除文件
func (c *JSONFileConnector) Delete(filePath string) (bool, error)
```

### 7. MemoryPersistenceHelper

对齐 Python `core/persistence.py`。
P1 阶段 `persistType` 只支持 `"json"`，`"auto"` 和 `"milvus"` 在 P7 实现 `MilvusConnector` 后启用。

```go
// MilvusConnector Milvus 后端接口（P7 实现）。
//
// P1 只定义接口，实现在 P7 和 ContextEvolutionRail 一起完成。
//
// Python: openjiuwen/extensions/context_evolver/core/db_connector/milvus_connector.py
type MilvusConnector interface {
    SaveToDB(namespace string, data map[string]any) error
    LoadFromDB(namespace string) (map[string]any, error
    Exists(namespace string) bool
    Delete(namespace string) bool
}

// MemoryPersistenceHelper 记忆持久化助手。
//
// 支持 JSON 文件和 Milvus 双后端。"auto" 模式探测 Milvus 可达性后回退 JSON。
// P1 阶段只实现 JSON 后端，Milvus 在 P7 启用。
//
// Python: openjiuwen/extensions/context_evolver/core/persistence.py
type MemoryPersistenceHelper struct {
    persistType      string   // "auto" / "json" / "milvus"
    persistPath      string   // "./memories/{algo_name}/{user_id}.json"
    milvusHost       string
    milvusPort       int
    milvusCollection string

    jsonConnector    *JSONFileConnector
    milvusConnector  MilvusConnector   // P7 注入
    resolvedType     string            // auto 探测后缓存
}

// PersistenceOption 持久化助手配置选项
type PersistenceOption func(*MemoryPersistenceHelper)

func NewMemoryPersistenceHelper(opts ...PersistenceOption) *MemoryPersistenceHelper

// Save 持久化节点到后端
func (h *MemoryPersistenceHelper) Save(userID, algoName string, nodesDict map[string]any) error

// Load 从后端加载节点
func (h *MemoryPersistenceHelper) Load(userID, algoName string) (map[string]any, error)

// SetMilvusConnector 注入 Milvus 连接器（P7 使用）
func (h *MemoryPersistenceHelper) SetMilvusConnector(conn MilvusConnector)

// ResolvedType 获取解析后的后端类型
func (h *MemoryPersistenceHelper) ResolvedType() string
```

---

## P1 不实现的内容

| 模块 | Python 路径 | 原因 |
|------|------------|------|
| `core/config.py` | `extensions/context_evolver/core/config.py` | 复用 Go 项目已有 config 包 |
| `core/schema/message.py` | `extensions/context_evolver/core/schema/message.py` | 复用 `agentcore/foundation/llm/schema` 的 `BaseMessage`/`RoleType` |
| `core/db_connector/milvus_connector.py` | `extensions/context_evolver/core/db_connector/milvus_connector.py` | P7 实现，P1 只定义接口 |
| `__init__.py` 中的 Message/Role 导出 | `extensions/context_evolver/core/schema/__init__.py` | 不需要，直接用 llm/schema 包 |

---

## 回填关系

P1 不直接回填任何占位。P1 是 P2-P7 的基础：

- P2（IO Schema）依赖 P1 的 `VectorNode`、`RuntimeContext`
- P3-P5（三系算法）依赖 P1 的 `BaseOp`/`SequentialOp`/`ParallelOp`、`ServiceContext`
- P6（服务层）依赖 P1 的全部模块
- P7（ContextEvolutionRail）回填 **9.24 P6**（`EvolutionRail` 中的 `ContextEvolutionRail` 占位）

9.24 当前状态：`P6(☐ 上下文演化: ContextEvolutionRail; ⤴️9.82 P7)`

---

## 测试策略

每个模块配备 `_test.go` 文件，覆盖率目标 ≥ 85%：

| 模块 | 测试要点 |
|------|---------|
| RuntimeContext | Set/Get/GetTyped/并发安全/ToDict 快照隔离 |
| ServiceContext | RegisterService/便捷访问器/Clear |
| BaseOp + SequentialOp | Then 链式/扁平化嵌套/中间失败短路 |
| BaseOp + ParallelOp | With 链式/扁平化嵌套/errgroup 并行/单失败 cancel |
| VectorNode | ToDict/FromDict 往返/JSON 序列化 |
| MemoryVectorStore | Upsert/Search 余弦排序/Delete/metadataFilter/并发安全/LoadFromDict |
| JSONFileConnector | SaveToFile 自动建目录/LoadFromFile/Exists/Delete/中文编码 |
| MemoryPersistenceHelper | Save+Load JSON 往返/auto 探测回退/路径模板替换/MilvusConnector mock |
