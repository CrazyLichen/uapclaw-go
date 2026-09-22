# 9.82 Context Evolver P2 IO Schema 层设计

## 概述

P2 实现 Context Evolver 的输入/输出数据模型，为 P3（ReMe）、P4（ReasoningBank）、P5（ACE）三条算法管线提供统一的 IO 类型定义。所有算法共享 Trajectory 输入和 Summarize/Retrieve 输出，但各自有独立的 Memory + Request + Response 体系。

### 在 Agent 会话中的流程位置

```
DeepAgent 会话循环
  ├── 任务循环 (TaskLoopController/LoopCoordinator)
  │     └── 每轮迭代
  │           ├── Rails 前置处理 (before_task_iteration)
  │           │     └── ContextEvolutionRail (9.82 P7)
  │           │           └── TaskMemoryService.retrieve(query)
  │           │                 └── 返回 RetrieveResponse[T] ← P2 定义
  │           │                 └── 将 memory_string 注入系统提示词
  │           ├── LLM 调用（带着历史经验推理）
  │           ├── 工具执行
  │           └── Rails 后置处理 (after_task_iteration)
  │                 └── ContextEvolutionRail (9.82 P7)
  │                       └── TaskMemoryService.summarize(trajectories)
  │                             └── 输入 TrajectoryBatch ← P2 定义
  │                             └── 返回 SummarizeResponse[T] ← P2 定义
  └── 会话结束
```

### 依赖链

```
9.82 P1 ✅ 核心框架 → P2 ☐ IO Schema → P3 ☐ ReMe → P4 ☐ RB → P5 ☐ ACE → P6 ☐ 服务层 → P7 ☐ Rail+工具
```

P2 是 P3-P5 所有算法实现的数据模型基础，必须先行。

---

## 设计决策

### 决策 1：跳过 Python memory.py 死代码

Python 中存在两套 BaseMemory 定义：
- `schema/memory.py`：早期简化实现，包含 TaskMemory/PersonalMemory/ReasoningBankMemory + vector_node_to_memory 工厂函数
- `schema/io_schema.py`：按论文原始设计重新定义的类型

经分析，`memory.py` 中的 TaskMemory、PersonalMemory、vector_node_to_memory 无任何业务代码调用，是遗留死代码。`io_schema.py` 才是当前唯一活跃的类型定义源。

**结论**：Go 只实现 `io_schema.py` + `trajectory.py` 中的类型，跳过 `memory.py`。

### 决策 2：不实现 Ours 类型

Python 中 Ours 系列全部是 ReMe 的空继承（`class OursMemory(ReMeMemory): pass`），6 个空子类无任何行为差异。Ours 是论文对比实验用的变体，实际业务中不需要区分。

**结论**：Go 中不实现 Ours 系列，需要时直接使用 ReMe 类型。

### 决策 3：SummarizeResponse/RetrieveResponse 使用泛型

Python 的泛型 Response 用 `Union[List[ACEMemory], ...]` 表示多算法支持。Go 没有 Union 类型。

**结论**：使用 Go 泛型 `SummarizeResponse[T any]` / `RetrieveResponse[T any]`，使用时 `SummarizeResponse[ACEMemory]` 等。类型安全且符合 Go 1.18+ 惯用法。

### 决策 4：BaseMemory 结构体嵌入

Python 中各 Memory 类型继承 BaseMemory。Go 中选择结构体嵌入对齐 Python 继承模式，同时各类型可独立定义自己的字段。

```go
type BaseMemory struct {
    WorkspaceID string
}
```

### 决策 5：MemoryInterface 接口不含 FromVectorNode

`from_vector_node` 在 Python 中是 classmethod（构造函数语义），Go 中接口方法必须有接收者实例才能调用，不适合作为接口方法。

**结论**：MemoryInterface 只含 `GetWorkspaceID()` + `ToVectorNode()`，FromVectorNode 改为包级构造函数。

```go
type MemoryInterface interface {
    GetWorkspaceID() string
    ToVectorNode() *core_schema.VectorNode
}
```

包级构造函数：
- `NewACEMemoryFromVectorNode(node *VectorNode) *ACEMemory`
- `NewReasoningBankMemoryFromVectorNode(node *VectorNode) *ReasoningBankMemory`
- `NewReMeMemoryFromVectorNode(node *VectorNode) *ReMeMemory`
- `VectorNodeToMemory(node *VectorNode) (MemoryInterface, error)` — 按 metadata.type 分发

### 决策 6：新增 schema/ 包（与 core/ 平级）

对齐 Python 的 `schema/` 目录结构，与 `core/` 包平级。避免 `core/schema/` 过度膨胀（P1 已有 VectorNode）。

---

## 包结构

```
internal/agentcore/context_evolver/
├── core/                        # P1 已完成
│   ├── context/                 # RuntimeContext/ServiceContext
│   ├── op/                      # BaseOp/SequentialOp/ParallelOp
│   ├── schema/                  # VectorNode（P1 已有）
│   ├── vector_store/            # MemoryVectorStore
│   ├── file_connector/          # JSONFileConnector
│   └── persistence/             # MemoryPersistenceHelper
└── schema/                      # P2 新增
    ├── doc.go                   # 包文档
    ├── trajectory.go            # FeedbackType + Trajectory + TrajectoryBatch
    ├── memory.go                # BaseMemory + MemoryInterface + 各 Memory 类型 + 包级构造函数
    ├── io_schema.go             # ACE/RB/ReMe 的 Request + Response + 泛型 Response
    ├── trajectory_test.go       # Trajectory 系列测试
    ├── memory_test.go           # Memory 系列测试
    └── io_schema_test.go        # IO Schema 系列测试
```

---

## 类型定义

### trajectory.go — 轨迹系列

#### FeedbackType 枚举

```go
type FeedbackType int

const (
    FeedbackHelpful  FeedbackType = iota  // 有帮助
    FeedbackHarmful                        // 有害
    FeedbackNeutral                        // 中性
)
```

对齐 Python `FeedbackType(str, Enum)`，Go 用标准 iota 枚举。实现 `String()` 和 `MarshalJSON`/`UnmarshalJSON` 以保持 JSON 序列化输出 `"helpful"`/`"harmful"`/`"neutral"`。

#### Trajectory 结构体

```go
type Trajectory struct {
    Query    string         // 执行的查询或任务
    Response string         // 生成的响应或输出
    Feedback FeedbackType   // 结果反馈
    Context  map[string]any // 执行的附加上下文
}
```

方法：
- `IsSuccess() bool` — Feedback == FeedbackHelpful
- `IsFailure() bool` — Feedback == FeedbackHarmful
- `ToDict() map[string]any` — 转为字典
- `String() string` — query 超过 50 字符截断

#### TrajectoryBatch 结构体

```go
type TrajectoryBatch struct {
    Trajectories []Trajectory  // 轨迹列表
    UserID       string        // 用户标识
    Metadata     map[string]any // 批次元数据
}
```

方法：
- `GetSuccessTrajectories() []Trajectory`
- `GetFailureTrajectories() []Trajectory`
- `CountByFeedback() map[FeedbackType]int`

---

### memory.go — Memory 系列

#### BaseMemory 结构体（嵌入用）

```go
type BaseMemory struct {
    WorkspaceID string // 工作空间/用户标识
}
```

方法：`GetWorkspaceID() string` — 返回 WorkspaceID

#### MemoryInterface 接口

```go
type MemoryInterface interface {
    GetWorkspaceID() string
    ToVectorNode() *core_schema.VectorNode
}
```

#### ACEMemory

```go
type ACEMemory struct {
    BaseMemory           // 嵌入
    ID         string    // 记忆标识
    Section    string    // 记忆分类
    Content    string    // 记忆内容
    Helpful    int       // 有帮助计数
    Harmful    int       // 有害计数
    Neutral    int       // 中性计数
    CreatedAt  time.Time // 创建时间
    UpdatedAt  time.Time // 更新时间
}
```

`ToVectorNode()` — metadata.type = `"ace_memory"`，embedding content = Content

#### ReasoningBankMemoryItem

```go
type ReasoningBankMemoryItem struct {
    Title       string // 核心策略标识
    Description string // 一句话摘要
    Content     string // 详细内容
}
```

非 MemoryInterface，是 ReasoningBankMemory 的子结构。

#### ReasoningBankMemory

```go
type ReasoningBankMemory struct {
    BaseMemory                    // 嵌入
    Query       string            // 用作 embedding 索引的查询
    Memory      []ReasoningBankMemoryItem // 记忆条目列表
    Label       *bool             // 记忆标签
}
```

`ToVectorNode()` — metadata.type = `"reasoning_bank_memory"`，embedding content = Query

注意：RBMemory **没有顶层 content 字段**，content 嵌套在 Memory 列表项中。这是与 Python 一致的设计。

#### ReMeMemoryMetadata

```go
type ReMeMemoryMetadata struct {
    Tags       []string  // 记忆标签
    StepType   string    // 步骤类型
    ToolsUsed  []string  // 使用的工具
    Confidence float64   // 置信度
    Freq       int       // 使用频率
    Utility    float64   // 效用分数
}
```

#### ReMeMemory

```go
type ReMeMemory struct {
    BaseMemory                   // 嵌入
    WhenToUse string             // 使用条件
    Content   string             // 记忆内容
    Score     float64            // 记忆分数 (0-1)
    CreatedAt time.Time          // 创建时间
    UpdatedAt time.Time          // 更新时间
    Metadata  ReMeMemoryMetadata // 元数据
}
```

`ToVectorNode()` — metadata.type = `"reme_memory"`，embedding content = WhenToUse

#### 包级构造函数

```go
// 从 VectorNode 反序列化各 Memory 类型
func NewACEMemoryFromVectorNode(node *core_schema.VectorNode) *ACEMemory
func NewReasoningBankMemoryFromVectorNode(node *core_schema.VectorNode) *ReasoningBankMemory
func NewReMeMemoryFromVectorNode(node *core_schema.VectorNode) *ReMeMemory

// 按 metadata.type 分发的工厂函数
func VectorNodeToMemory(node *core_schema.VectorNode) (MemoryInterface, error)
```

`VectorNodeToMemory` 根据 `node.Metadata["type"]` 分发：
- `"ace_memory"` → `NewACEMemoryFromVectorNode`
- `"reasoning_bank_memory"` → `NewReasoningBankMemoryFromVectorNode`
- `"reme_memory"` → `NewReMeMemoryFromVectorNode`
- 其他 → 返回 error

---

### io_schema.go — Request/Response 系列

#### ACE 系列

| 类型 | 字段 |
|------|------|
| `ACESummarizeRequest` | Matts string, Query string, Trajectories []string, GroundTruth *string, Feedback []string |
| `ACESummarizeResponse` | Status string, Memory []ACEMemory |
| `ACERetrieveRequest` | UserID *string |
| `ACERetrieveResponse` | Status string, MemoryString string, RetrievedMemory []ACERetrievedMemory |
| `ACERetrievedMemory` | ID string, Section string, Content string, Helpful int, Harmful int, Neutral int |

#### ReasoningBank 系列

| 类型 | 字段 |
|------|------|
| `ReasoningBankSummarizeRequest` | Matts string, Query string, Trajectories []string, Label []*bool |
| `ReasoningBankSummarizeResponse` | Status string, Memory []ReasoningBankMemory |
| `ReasoningBankRetrieveRequest` | Query string, TopK int |
| `ReasoningBankRetrieveResponse` | Status string, MemoryString string, RetrievedMemory []ReasoningBankRetrievedMemory |
| `ReasoningBankRetrievedMemory` | Title string, Description string, Content string |

#### ReMe 系列

| 类型 | 字段 |
|------|------|
| `ReMeSummarizeRequest` | Matts string, Trajectories []string, Score []float64 |
| `ReMeSummarizeResponse` | Status string, Memory []ReMeMemory |
| `ReMeRetrieveRequest` | Query string, TopKRetrieval int, TopKRerank int |
| `ReMeRetrieveResponse` | Status string, MemoryString string, RetrievedMemory []ReMeRetrievedMemory |
| `ReMeRetrievedMemory` | WhenToUse string, Content string |

#### 泛型 Response

```go
// 通用摘要响应，支持任意算法的 Memory 类型
type SummarizeResponse[T any] struct {
    Status string
    Memory []T
}

// 通用检索响应，支持任意算法的 RetrievedMemory 类型
type RetrieveResponse[T any] struct {
    Status          string
    MemoryString    string
    RetrievedMemory []T
}
```

---

## 类型总览

| 分类 | 类型 | 数量 |
|------|------|------|
| Trajectory 系列 | FeedbackType, Trajectory, TrajectoryBatch | 3 |
| Memory 基础 | BaseMemory, MemoryInterface | 2 |
| Memory 具体类型 | ACEMemory, ReasoningBankMemory, ReasoningBankMemoryItem, ReMeMemory, ReMeMemoryMetadata | 5 |
| ACE Request/Response | ACESummarizeRequest, ACESummarizeResponse, ACERetrieveRequest, ACERetrieveResponse, ACERetrievedMemory | 5 |
| RB Request/Response | ReasoningBankSummarizeRequest, ReasoningBankSummarizeResponse, ReasoningBankRetrieveRequest, ReasoningBankRetrieveResponse, ReasoningBankRetrievedMemory | 5 |
| ReMe Request/Response | ReMeSummarizeRequest, ReMeSummarizeResponse, ReMeRetrieveRequest, ReMeRetrieveResponse, ReMeRetrievedMemory | 5 |
| 泛型 Response | SummarizeResponse[T], RetrieveResponse[T] | 2 |
| 包级函数 | NewACEMemoryFromVectorNode, NewReasoningBankMemoryFromVectorNode, NewReMeMemoryFromVectorNode, VectorNodeToMemory | 4 |
| **总计** | | **31** |

---

## 测试覆盖

### trajectory_test.go
- FeedbackType 枚举 String() 输出
- FeedbackType JSON 序列化/反序列化往返
- Trajectory 创建、IsSuccess/IsFailure
- Trajectory ToDict 往返
- Trajectory String() 截断
- TrajectoryBatch 过滤方法（GetSuccessTrajectories/GetFailureTrajectories）
- TrajectoryBatch CountByFeedback
- TrajectoryBatch String()

### memory_test.go
- BaseMemory GetWorkspaceID
- ACEMemory 创建、ToVectorNode 往返、NewACEMemoryFromVectorNode 往返
- ReasoningBankMemory 创建、ToVectorNode 往返、NewReasoningBankMemoryFromVectorNode 往返
- ReMeMemory 创建、ToVectorNode 往返、NewReMeMemoryFromVectorNode 往返
- VectorNodeToMemory 按 type 分发（三种类型 + 未知类型返回 error）
- Memory 空字段默认值

### io_schema_test.go
- 各 Request 类型 JSON 序列化/反序列化往返
- 各 Response 类型 JSON 序列化/反序列化往返
- SummarizeResponse[ACEMemory] / SummarizeResponse[ReasoningBankMemory] / SummarizeResponse[ReMeMemory] 泛型实例化
- RetrieveResponse[T] 同上
- 各类型默认值验证

---

## 对应 Python 代码

| Python 文件 | Go 文件 | 说明 |
|------------|--------|------|
| `openjiuwen/extensions/context_evolver/schema/trajectory.py` | `schema/trajectory.go` | FeedbackType + Trajectory + TrajectoryBatch |
| `openjiuwen/extensions/context_evolver/schema/io_schema.py` | `schema/memory.go` + `schema/io_schema.go` | Memory 类型拆到 memory.go，Request/Response 拆到 io_schema.go |
| `openjiuwen/extensions/context_evolver/schema/memory.py` | — | 跳过（死代码） |

---

## 回填检查

P2 无回填需求。P2 产出的类型将被 P3-P7 消费：
- P3（ReMe）：消费 ReMeMemory + ReMeMemoryMetadata + ReMeSummarizeRequest/Response + ReMeRetrieveRequest/Response
- P4（RB）：消费 ReasoningBankMemory + ReasoningBankMemoryItem + RB Summarize/Retrieve Request/Response
- P5（ACE）：消费 ACEMemory + ACE Summarize/Retrieve Request/Response
- P6（服务层）：消费 Trajectory + TrajectoryBatch + SummarizeResponse[T] + RetrieveResponse[T] + MemoryInterface
- P7（Rail+工具）：消费 MemoryInterface + VectorNodeToMemory
