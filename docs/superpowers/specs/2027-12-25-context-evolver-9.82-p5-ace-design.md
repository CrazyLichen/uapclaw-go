# 9.82 Context Evolver P5 ACE 算法设计文档

## 概述

本文档描述 9.82 P5 ACE（Adaptive Context Evolution）算法的 Go 实现设计，包括 Playbook 核心数据结构、检索/总结 Op、prompt 模板、utils 工具函数以及 doc.go 回填。

ACE 是 Context Evolver 三条记忆算法管线之一，核心思想是 **Playbook 迭代**：维护一个结构化的"剧本"（Playbook），每个条目（Bullet）属于一个 section，带有 helpful/harmful/neutral 反馈计数。通过 反思→策展→增量更新 的循环，持续优化 playbook 内容。

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

### ACE 与 ReMe / ReasoningBank 的区别

| 维度 | ReMe (P3) | ReasoningBank (P4) | ACE (P5) |
|------|-----------|-------------------|----------|
| 核心思想 | 对比提取经验：成功 vs 失败轨迹 → 提炼记忆 | 推理链标签 + MaTTS 伸缩 | Playbook 迭代：反思→策展→增量更新 |
| 存储模型 | 扁平记忆条目 | 带标签记忆条目 | Bullet 为粒度的 Playbook，有 section 分组，有 helpful/harmful 计分 |
| 演化方式 | 提取→验证→去重→持久化 | 提取→标签判定→摘要→MaTTS→持久化 | 反思→策展(DeltaBatch)→应用增量(DeltaOperation)→持久化 |
| 检索方式 | 语义搜索+重排+改写 | 语义搜索 | 全量加载（不做语义搜索） |

### ACE 管线流程

```
检索流程（retrieve）:
  ACERecallMemoryOp → 全量加载 playbook bullets → context.retrieved_memories

总结流程（summarize）:
  LoadPlaybookOp → 加载已有 playbook
    >> (ReflectOp | ParallelReflectOp) → 反思生成 reflection
    >> (CurateOp | ParallelCurateOp) → 策展出 DeltaBatch
    >> ApplyDeltaOp → 应用增量到 playbook + 淘汰低分 bullet + 更新 vector store
    >> PersistMemoryOp → 持久化
```

---

## 1. 决策汇总

| # | 决策 | 选择 | 原因 |
|---|------|------|------|
| 1 | 数据结构文件组织 | 单独 `playbook.go` | 对齐 Python playbook.py，4 个类型内聚在一个文件 |
| 2 | nextID 机制 | Playbook 持有 nextID 字段 | 对齐 Python，简单直接，LoadPlaybookOp 时推算设置 |
| 3 | 淘汰机制 | 完全对齐 Python | ApplyDeltaOp 构造参数 maxBullets 默认 50，超限按 score=helpful-harmful 升序淘汰 |
| 4 | Op 组织方式 | 4 个独立 Op | 对齐 Python 和 P4 RB 模式：ReflectOp/ParallelReflectOp/CurateOp/ParallelCurateOp 各自判断 matts |
| 5 | OperationType | iota 枚举 + String() | 保持类型安全，对齐 Go 编码规范 |
| 6 | Prompt 模板 | 单独 prompt.go + ACEPrompt 结构体 | 对齐 P4 RB 的 ReasoningBankSummaryPrompt 模式，用 text/template |
| 7 | utils | ACE 包内独立实现 SafeJSONLoads | 和 ReMe/RB 保持一致的独立性 |
| 8 | 实现范围 | 严格 P5 边界 | 只实现 Op + 数据结构 + prompt，不回填 trajectory_generator/P6 |
| 9 | 时间字段 | time.Time | 对齐 Python datetime，RFC3339 序列化 |

---

## 2. 目录结构与文件清单

### 2.1 P5 新增

```
internal/agentcore/context_evolver/
├── retrieve/task/ace/           ← P5 新增
│   ├── doc.go                   # 包文档
│   ├── run.go                   # ACERecallMemoryOp
│   └── run_test.go              # ACERecallMemoryOp 测试
│
├── summary/task/ace/            ← P5 新增
│   ├── doc.go                   # 包文档
│   ├── playbook.go              # Playbook + DeltaBatch + Bullet + DeltaOperation + OperationType 枚举 + BulletTag
│   ├── update.go                # LoadPlaybookOp + ReflectOp + ParallelReflectOp + CurateOp + ParallelCurateOp + ApplyDeltaOp + PersistMemoryOp
│   ├── prompt.go                # 6 个 prompt 模板常量 + ACEPrompt 结构体
│   ├── utils.go                 # SafeJSONLoads
│   ├── playbook_test.go         # Playbook/DeltaBatch/Bullet/DeltaOperation 测试
│   ├── update_test.go           # 7 个 Op 测试
│   ├── prompt_test.go           # prompt 模板测试
│   └── utils_test.go            # SafeJSONLoads 测试
```

### 2.2 回填项

| 文件 | 回填内容 |
|------|---------|
| `internal/agentcore/context_evolver/doc.go` | 文件目录段加入 `retrieve/task/ace/` 和 `summary/task/ace/` 条目 |

---

## 3. 核心数据结构

### 3.1 OperationType 枚举

```go
// OperationType playbook 变更操作类型。
// 对齐 Python OperationType = Literal["ADD", "UPDATE", "TAG", "REMOVE"]。
type OperationType int

const (
    // OperationAdd 添加新 bullet
    OperationAdd OperationType = iota
    // OperationUpdate 更新已有 bullet 的内容和/或元数据
    OperationUpdate
    // OperationTag 对已有 bullet 的元数据进行增量标记
    OperationTag
    // OperationRemove 删除已有 bullet
    OperationRemove
)

// String 实现 Stringer 接口，返回 Python 对齐的操作类型字符串。
func (t OperationType) String() string { ... }

// ParseOperationType 从字符串解析操作类型。
func ParseOperationType(s string) (OperationType, error) { ... }
```

### 3.2 DeltaOperation

```go
// DeltaOperation 单条 playbook 变更操作。
// 对齐 Python DeltaOperation dataclass。
type DeltaOperation struct {
    // Type 操作类型
    Type OperationType `json:"type"`
    // Section 目标 section
    Section string `json:"section"`
    // Content 新内容（ADD/UPDATE 时使用）
    Content *string `json:"content,omitempty"`
    // BulletID 目标 bullet 标识（UPDATE/TAG/REMOVE 时必须）
    BulletID *string `json:"bullet_id,omitempty"`
    // Metadata 元数据变更（TAG/UPDATE 时使用）
    Metadata map[string]int `json:"metadata,omitempty"`
}
```

方法：
- `NewDeltaOperationFromJSON(payload map[string]any) *DeltaOperation` — 从 JSON 反序列化
- `ToJSON() map[string]any` — 序列化为 JSON

### 3.3 DeltaBatch

```go
// DeltaBatch 一组 curator 推理 + 操作。
// 对齐 Python DeltaBatch dataclass。
type DeltaBatch struct {
    // Reasoning 策展推理过程
    Reasoning string `json:"reasoning"`
    // Operations 待应用的操作列表
    Operations []DeltaOperation `json:"operations"`
}
```

方法：
- `NewDeltaBatchFromJSON(payload map[string]any) *DeltaBatch` — 从 JSON 反序列化
- `ToJSON() map[string]any` — 序列化为 JSON

### 3.4 Bullet

```go
// Bullet 单条 playbook 条目。
// 对齐 Python Bullet dataclass。
type Bullet struct {
    // ID 唯一标识，格式 {section_prefix}-{五位数}
    ID string `json:"id"`
    // Section 所属分类
    Section string `json:"section"`
    // Content 条目内容
    Content string `json:"content"`
    // Helpful 有帮助计数
    Helpful int `json:"helpful"`
    // Harmful 有害计数
    Harmful int `json:"harmful"`
    // Neutral 中性计数
    Neutral int `json:"neutral"`
    // CreatedAt 创建时间
    CreatedAt time.Time `json:"created_at"`
    // UpdatedAt 更新时间
    UpdatedAt time.Time `json:"updated_at"`
}
```

方法：
- `ApplyMetadata(metadata map[string]int)` — 应用元数据变更（helpful/harmful/neutral 字段增量更新）
- `Tag(tag string, increment int) error` — 对指定标签（helpful/harmful/neutral）增加计数，并更新 UpdatedAt

### 3.5 BulletTag

```go
// BulletTag bullet 标签。
// 对齐 Python BulletTag dataclass。
type BulletTag struct {
    // ID bullet 标识
    ID string `json:"id"`
    // Tag 标签名
    Tag string `json:"tag"`
}
```

### 3.6 Playbook

```go
// Playbook ACE 结构化上下文存储。
// 对齐 Python Playbook class。
//
// 内部维护 bullets map + sections 索引 + nextID 计数器。
// nextID 用于生成唯一 bullet ID，格式 {section_prefix}-{五位数}。
type Playbook struct {
    // bullets bullet 存储，key = bullet ID
    bullets map[string]*Bullet
    // sections section 索引，key = section name，value = bullet ID 列表（有序）
    sections map[string][]string
    // nextID 下一个 bullet ID 的数字部分
    nextID int
}
```

方法（对齐 Python Playbook 的所有方法）：

| 方法 | 签名 | 说明 |
|------|------|------|
| NewPlaybook | `func NewPlaybook() *Playbook` | 构造空 playbook |
| AddBullet | `func (p *Playbook) AddBullet(section, content string, bulletID *string, metadata map[string]int) *Bullet` | 添加 bullet |
| UpdateBullet | `func (p *Playbook) UpdateBullet(bulletID string, content *string, metadata map[string]int) *Bullet` | 更新 bullet，返回 nil 表示未找到 |
| TagBullet | `func (p *Playbook) TagBullet(bulletID, tag string, increment int) *Bullet` | 标记 bullet |
| RemoveBullet | `func (p *Playbook) RemoveBullet(bulletID string)` | 删除 bullet |
| GetBullet | `func (p *Playbook) GetBullet(bulletID string) *Bullet` | 获取 bullet |
| Bullets | `func (p *Playbook) Bullets() []*Bullet` | 返回所有 bullet |
| BulletIDs | `func (p *Playbook) BulletIDs() []string` | 返回所有 bullet ID |
| LoadBullet | `func (p *Playbook) LoadBullet(bullet *Bullet)` | 加载已有 bullet（反序列化用） |
| SetNextID | `func (p *Playbook) SetNextID(nextID int)` | 设置 nextID（反序列化用） |
| ToDict | `func (p *Playbook) ToDict() map[string]any` | 序列化为字典 |
| FromDict | `func PlaybookFromDict(payload map[string]any) *Playbook` | 从字典反序列化 |
| Dumps | `func (p *Playbook) Dumps() (string, error)` | 序列化为 JSON 字符串 |
| Loads | `func PlaybookLoads(data string) (*Playbook, error)` | 从 JSON 字符串反序列化 |
| ApplyDelta | `func (p *Playbook) ApplyDelta(delta *DeltaBatch)` | 应用 DeltaBatch |
| applyOperation | `func (p *Playbook) applyOperation(op *DeltaOperation)` | 应用单条操作（非导出） |
| AsPrompt | `func (p *Playbook) AsPrompt() string` | 生成 LLM 可读的 playbook 字符串 |
| MakePlaybookExcerpt | `func (p *Playbook) MakePlaybookExcerpt(bulletIDs []string) string` | 生成摘录 |
| Stats | `func (p *Playbook) Stats() map[string]any` | 返回统计信息 |
| generateID | `func (p *Playbook) generateID(section string) string` | 生成唯一 ID（非导出） |

---

## 4. Op 详细设计

### 4.1 retrieve/task/ace — ACERecallMemoryOp

```go
// ACERecallMemoryOp ACE 记忆检索操作。
// 从 vector store 全量加载 ACE 记忆（playbook bullets），不做语义搜索/排序。
// 对齐 Python RecallMemoryOp。
type ACERecallMemoryOp struct {
    op.OpBase
}
```

**Execute 逻辑**：
1. 从 RuntimeContext 获取 `user_id`（默认 "default"）
2. 检查 VectorStore 是否可用
3. 用 dummy embedding `[0.0]*2560` + `top_k=50` 搜索，metadata_filter = `{"workspace_id": user_id, "type": "ace_memory"}`
4. 将 VectorNode 转为 `ACEMemory`（使用已有的 `schema.NewACEMemoryFromVectorNode`），再转为 `ACERetrievedMemory`
5. 设置 `context.retrieved_memories`

**日志**：
- Debug: "Loading all ACE memories from vector store..."
- Info: "Retrieved %d ACE memories (playbook bullets)"
- Warn: "Failed to convert ACE memory from node %s: %v"

### 4.2 summary/task/ace — LoadPlaybookOp

```go
// LoadPlaybookOp 从 vector store 加载已有 playbook。
// 对齐 Python LoadPlaybookOp。
type LoadPlaybookOp struct {
    op.OpBase
}
```

**Execute 逻辑**：
1. 从 RuntimeContext 获取 `user_id`
2. 检查 VectorStore 是否可用
3. 用 dummy embedding + metadata_filter 搜索 ACE memories
4. 将 VectorNode metadata 转为 Bullet，调用 `playbook.LoadBullet(bullet)`
5. 遍历所有 bullet ID，从 ID 格式 `{section}-{五位数}` 中提取最大数字，设置 `playbook.SetNextID(maxID)`
6. 设置 `context.playbook`
7. 如果加载失败，使用空 Playbook 作为回退

**日志**：
- Info: "Loaded playbook with %d bullets, next_id=%d"
- Warn: "Failed to load playbook: %v. Starting with empty playbook."

### 4.3 summary/task/ace — ReflectOp

```go
// ReflectOp 单轨迹反思操作（matts="none"/"sequential"）。
// 对齐 Python ReflectOp。
type ReflectOp struct {
    op.OpBase
    // useGroundTruth 是否使用 ground truth
    useGroundTruth bool
}
```

**Execute 逻辑**：
1. 检查 matts 值，非 "none"/"sequential" 时跳过
2. 检查 LLM 是否可用
3. 从 context 获取 `query`、`trajectories`、`playbook`、`ground_truth`、`feedback`
4. 格式化 trajectory：取 `trajectories[0]`
5. 根据 `useGroundTruth` 选择 `aceReflectorPrompt` 或 `aceReflectorNoGTPrompt`
6. 使用 text/template 执行模板，传入 playbook.AsPrompt() 和 trajectory
7. 调用 LLM 生成 reflection
8. 使用 SafeJSONLoads 解析 JSON 响应
9. 设置 `context.reflection`

**日志**：
- Info: "Skipping ReflectOp for matts mode: %s"
- Warn: "No trajectories to reflect on"
- Debug: "Generating reflection from trajectory..."
- Info: "Generated reflection successfully"
- Error: "Failed to parse reflection: %v"

### 4.4 summary/task/ace — ParallelReflectOp

```go
// ParallelReflectOp 多轨迹反思操作（matts="parallel"/"combined"）。
// 对齐 Python ParallelReflectOp。
type ParallelReflectOp struct {
    op.OpBase
    // useGroundTruth 是否使用 ground truth
    useGroundTruth bool
}
```

**Execute 逻辑**：
1. 检查 matts 值，非 "parallel"/"combined" 时跳过
2. 检查 LLM 是否可用
3. 从 context 获取 `query`、`trajectories`、`playbook`、`ground_truth`、`feedback`
4. 检查 trajectories 数量 >= 2
5. 动态格式化多轨迹：每条用 `<TRAJECTORY i>` 标签包裹，附上对应的 feedback（如有）
6. 根据 `useGroundTruth` 选择 `aceReflectorScalingPrompt` 或 `aceReflectorScalingNoGTPrompt`
7. 调用 LLM + SafeJSONLoads 解析
8. 设置 `context.reflection`

### 4.5 summary/task/ace — CurateOp

```go
// CurateOp 单轨迹策展操作（matts="none"/"sequential"）。
// 对齐 Python CurateOp。
type CurateOp struct {
    op.OpBase
}
```

**Execute 逻辑**：
1. 检查 matts 值，非 "none"/"sequential" 时跳过
2. 检查 LLM 是否可用
3. 从 context 获取 `reflection`、`playbook`、`query`、`trajectories`
4. 如果 reflection 为空，设置空 DeltaBatch
5. 格式化 trajectory：取 `trajectories[0]`
6. 使用 `aceCuratorPrompt` 模板，传入 question_context=query, playbook.AsPrompt(), trajectory, reflection(JSON 序列化)
7. 调用 LLM + SafeJSONLoads 解析
8. 用 `NewDeltaBatchFromJSON` 构造 DeltaBatch
9. 设置 `context.delta`

### 4.6 summary/task/ace — ParallelCurateOp

```go
// ParallelCurateOp 多轨迹策展操作（matts="parallel"/"combined"）。
// 对齐 Python ParallelCurateOp。
type ParallelCurateOp struct {
    op.OpBase
}
```

**Execute 逻辑**：
1. 检查 matts 值，非 "parallel"/"combined" 时跳过
2. 检查 LLM 是否可用
3. 从 context 获取 `reflection`、`playbook`、`query`、`trajectories`
4. 检查 trajectories 数量 >= 2
5. 动态格式化多轨迹
6. 使用 `aceCuratorScalingPrompt` 模板
7. 调用 LLM + SafeJSONLoads + NewDeltaBatchFromJSON
8. 设置 `context.delta`

### 4.7 summary/task/ace — ApplyDeltaOp

```go
// ApplyDeltaOp 应用 playbook 变更并持久化到 vector store。
// 对齐 Python ApplyDeltaOp。
type ApplyDeltaOp struct {
    op.OpBase
    // maxBullets playbook 最大 bullet 数量，默认 50
    maxBullets int
}
```

**Execute 逻辑**：
1. 从 context 获取 `delta`、`playbook`、`user_id`
2. 如果 delta 为空或无 operations，设置 `context.memories = []` 返回
3. 检查 VectorStore 和 EmbeddingModel 是否可用
4. **淘汰逻辑**：计算 `addCount + currentCount - maxBullets`，如果 > 0，按 score=helpful-harmful 升序排列 bullets，删除最低分的
5. **逐条应用 DeltaOperation**：
   - ADD：`playbook.AddBullet()`，记录到 affectedBulletIDs
   - UPDATE：`playbook.UpdateBullet()`，如果 bullet 不存在且 content 非空则降级为 ADD，记录到 affectedBulletIDs
   - TAG：`playbook.TagBullet()`，记录到 affectedBulletIDs
   - REMOVE：`playbook.RemoveBullet()`，记录到 removedBulletIDs
6. **删除 removed bullets 从 vector store**：对每个 removedBulletID，构造 nodeID=`ace_{user_id}_{bulletID}`，调用 `vectorStore.Delete()`
7. **更新 affected bullets 到 vector store**：对每个 affectedBulletID，构造 ACEMemory → ToVectorNode → embed content → upsert
8. 设置 `context.memories`

**日志**：
- Info: "No delta operations to apply"
- Info: "Removed low-scoring bullet: %s"
- Info: "UPDATE operation converted to ADD: bullet %s not found, creating new bullet"
- Warn: "UPDATE operation failed: bullet %s not found and no content provided"
- Warn: "TAG operation failed: bullet %s not found"
- Debug: "Deleted bullet %s from vector store"
- Warn: "Failed to delete bullet %s: %v"
- Info: "Applied %d operations: %d bullets updated, %d bullets removed"

### 4.8 summary/task/ace — PersistMemoryOp

```go
// PersistMemoryOp 持久化 ACE 记忆到 JSON 或 Milvus。
// 对齐 Python PersistMemoryOp。
type PersistMemoryOp struct {
    op.OpBase
    // helper 持久化助手
    helper *cepersistence.MemoryPersistenceHelper
}

// algoName 固定为 "ace"
const aceAlgoName = "ace"
```

**构造参数**（对齐 Python）：
- `persistType`：默认 "auto"
- `persistPath`：默认 "./memories/{algo_name}/{user_id}.json"
- `milvusHost`：默认 "localhost"
- `milvusPort`：默认 19530
- `milvusCollection`：默认 "vector_nodes"

**Execute 逻辑**：
1. 从 context 获取 `user_id`
2. 检查 VectorStore 是否可用
3. 调用 `vectorStore.GetAll(metadata_filter={"workspace_id": user_id, "type": "ace_memory"})` 获取所有节点
4. 如果无节点，设置 `context.persistCount = 0` 返回
5. 将节点转为 `map[string]any`（每个 node.ToDict()）
6. 调用 `helper.Save(userID, aceAlgoName, nodesDict)`
7. 设置 `context.persistCount`

**日志**：
- Info: "PersistMemoryOp (ACE): no memories to persist for user=%s"
- Info: "PersistMemoryOp (ACE): persisted %d memories for user=%s via %s"

---

## 5. Prompt 模板

### 5.1 ACEPrompt 结构体

```go
// ACEPrompt ACE 算法提示词配置。
// 对齐 Python ACEPrompt dataclass。
type ACEPrompt struct {
    // ACEReflectorPrompt 带 ground truth 的反思提示词
    ACEReflectorPrompt *template.Template
    // ACEReflectorNoGTPrompt 无 ground truth 的反思提示词
    ACEReflectorNoGTPrompt *template.Template
    // ACECuratorPrompt 单轨迹策展提示词
    ACECuratorPrompt *template.Template
    // ACEReflectorScalingPrompt 多轨迹带 GT 的反思提示词
    ACEReflectorScalingPrompt *template.Template
    // ACEReflectorScalingNoGTPrompt 多轨迹无 GT 的反思提示词
    ACEReflectorScalingNoGTPrompt *template.Template
    // ACECuratorScalingPrompt 多轨迹策展提示词
    ACECuratorScalingPrompt *template.Template
}
```

### 5.2 模板占位符

| 模板 | 占位符 |
|------|--------|
| aceReflectorPrompt | `{{.GroundTruth}}` `{{.Feedback}}` `{{.Playbook}}` `{{.Trajectory}}` |
| aceReflectorNoGTPrompt | `{{.Playbook}}` `{{.Trajectory}}` |
| aceCuratorPrompt | `{{.QuestionContext}}` `{{.Playbook}}` `{{.Trajectory}}` `{{.Reflection}}` |
| aceReflectorScalingPrompt | `{{.GroundTruth}}` `{{.Playbook}}` `{{.Trajectories}}` |
| aceReflectorScalingNoGTPrompt | `{{.Playbook}}` `{{.Trajectories}}` |
| aceCuratorScalingPrompt | `{{.QuestionContext}}` `{{.Playbook}}` `{{.Trajectories}}` `{{.Reflection}}` |

### 5.3 模板数据结构

```go
// reflectorPromptData 反思提示词模板数据
type reflectorPromptData struct {
    GroundTruth string
    Feedback    string
    Playbook    string
    Trajectory  string
}

// reflectorScalingPromptData 多轨迹反思提示词模板数据
type reflectorScalingPromptData struct {
    GroundTruth  string
    Playbook     string
    Trajectories string
}

// curatorPromptData 策展提示词模板数据
type curatorPromptData struct {
    QuestionContext string
    Playbook        string
    Trajectory      string
    Reflection      string
}

// curatorScalingPromptData 多轨迹策展提示词模板数据
type curatorScalingPromptData struct {
    QuestionContext string
    Playbook        string
    Trajectories    string
    Reflection      string
}
```

### 5.4 Prompt 内容

6 个 prompt 模板的具体文本**一比一复刻** Python `summary/task/ace/prompt.py` 中的原文。详见 Python 源码：
- `openjiuwen/extensions/context_evolver/summary/task/ace/prompt.py`

**text/template 转义规则**：

Python 的 prompt 正文中大量使用 `{{` / `}}` 表示 JSON 示例（因为 Python `.format()` 需要双花括号转义）。Go `text/template` 中 `{{` 也是占位符语法，所以需要特殊处理：

- **Go 模板占位符**：直接写 `{{.GroundTruth}}`、`{{.Playbook}}` 等
- **JSON 示例中的字面 `{{`**：使用 `{{"{{"}}` 转义（输出字面 `{{`）

对齐 P4 RB 已有的 `text/template` 模式（P4 RB prompt 无 JSON 示例所以无此问题，但模式一致）。

---

## 6. Utils

### 6.1 SafeJSONLoads

```go
// SafeJSONLoads 安全解析 JSON 字符串。
// 对齐 Python _safe_json_loads。
// 支持三种回退策略：直接解析 → markdown code block 提取 → 任意 JSON 对象提取。
func SafeJSONLoads(text string) (map[string]any, error)
```

逻辑：
1. 尝试 `json.Unmarshal` 直接解析
2. 失败后，正则提取 markdown code block 中的 JSON：`` ```(?:json)?\s*(\{.*?\})\s*``` ``
3. 仍失败，正则提取任意 JSON 对象：`\{.*\}`
4. 全部失败，记录 Error 日志并返回 error

---

## 7. 依赖关系

### 7.1 上游依赖（已实现）

| 依赖 | 包 | 说明 |
|------|-----|------|
| BaseOp/OpBase | core/op | 操作基类和组合子 |
| ServiceContext | core/context | LLM/Embedding/VectorStore 服务访问 |
| RuntimeContext | core/context | 运行时上下文传递 |
| VectorNode | core/schema | 向量节点 |
| MemoryVectorStore | core/vector_store | 内存向量存储 |
| MemoryPersistenceHelper | core/persistence | 持久化助手 |
| ACEMemory | schema | ACE 记忆类型 + ToVectorNode/FromVectorNode |
| ACERetrievedMemory | schema | ACE 检索结果类型 |
| Trajectory/TrajectoryBatch | schema | 轨迹类型 |

### 7.2 下游依赖（P6/P7 实现）

| 下游 | 说明 |
|------|------|
| trajectory_generator.go | summarize_trajectories 的 ACE 分支 |
| TaskMemoryService | ACE 管线组装：`LoadPlaybookOp >> (ReflectOp | ParallelReflectOp) >> (CurateOp | ParallelCurateOp) >> ApplyDeltaOp >> PersistMemoryOp` |
| ContextEvolutionRail | 9.82 P7，⤴️9.24 P6 回填点 |

---

## 8. 不在 P5 范围内

- TaskMemoryService 的 ACE 管线组装（P6）
- trajectory_generator 的 ACE 分支（P6）
- ContextEvolutionRail（P7）
- MilvusConnector 实现（P7）
- 9.24 P6 回填（P7）
