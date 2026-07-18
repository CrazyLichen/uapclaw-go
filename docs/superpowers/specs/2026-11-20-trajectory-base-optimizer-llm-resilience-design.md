# 9.77 基础 Trajectory 类型 + 9.72e BaseOptimizer & LLMResilience 设计

## 概述

本文档描述自演化系统中两个紧密关联组件的 Go 实现：

1. **9.77 基础 Trajectory 类型** — 执行轨迹的数据模型，是 BaseOptimizer 的核心依赖
2. **9.72e BaseOptimizer + LLMResilience** — 优化器基类（backward/step 骨架）+ LLM 调用重试策略

实现顺序：先做 9.77 基础类型（仅 types.py 对应的结构体和工具函数），再做 9.72e，
最后回填已有代码中的 `any` 占位和类型断言。

## 流程位置

在 Agent 离线演化流水线中的位置：

```
Trainer 编排:
  1. bind(operators, targets)     — 绑定可优化的 Operator
  2. forward(train_cases)         — 前向推理，产生 Trajectory     ← 9.77
  3. evaluate(trajectories)       — 评估推理结果                  ← 9.71 ✅
  4. signal(evaluated_cases)      — 从评估结果生成演化信号         ← 9.73
  5. update(signals)              — 信号驱动更新 ← 9.70c ✅
     └─ SingleDimUpdater/MultiDimUpdater
        └─ BaseOptimizer          ← 📍 9.72e 就在这里
           ├─ add_trajectory()    — 缓存轨迹
           ├─ backward(signals)   — 计算梯度（信号→梯度映射）
           └─ step()              — 生成更新映射（梯度→更新）
  6. apply_updates(updates)       — 应用更新到 Operator           ← Trainer ✅
```

---

## 第一步：9.77 基础 Trajectory 类型

### 包路径

`internal/evolving/trajectory/`

### 文件结构

```
trajectory/
├── doc.go           # 包文档
└── types.go         # 核心类型定义 + JSONSafe + MessageToDict
```

### 类型定义

#### StepKind

```go
// StepKind 执行步骤类型。
//
// 对应 Python: StepKind = Literal["llm", "tool"]
type StepKind string

const (
    // StepKindLLM LLM 调用步骤
    StepKindLLM StepKind = "llm"
    // StepKindTool 工具调用步骤
    StepKindTool StepKind = "tool"
)
```

#### CostInfo

```go
// CostInfo 聚合成本指标。
//
// 对应 Python: CostInfo = Dict[str, int]  # {"input_tokens": N, "output_tokens": M}
type CostInfo map[string]int
```

#### StepDetail 接口

```go
// StepDetail 执行步骤的详细数据接口。
//
// LLM 步骤由 LLMCallDetail 实现，工具步骤由 ToolCallDetail 实现。
// StepKind() 方法提供类型判别，也可通过 Go 类型断言 switch d.(type) 判别。
//
// 对应 Python: StepDetail = Union[LLMCallDetail, ToolCallDetail]
type StepDetail interface {
    // StepKind 返回步骤类型（llm 或 tool）。
    StepKind() StepKind
}
```

#### LLMCallDetail

```go
// LLMCallDetail LLM 调用完整执行数据。
//
// 对应 Python: LLMCallDetail dataclass
type LLMCallDetail struct {
    // Model 模型名称
    Model string
    // Messages 消息列表（原始消息对象，经 JSONSafe 处理后为可序列化形式）
    Messages []any
    // Response 模型响应（可选）
    Response any
    // Tools 工具定义列表（可选）
    Tools []map[string]any
    // Usage 使用量信息（可选）
    Usage map[string]any
    // Meta 扩展元数据
    Meta map[string]any
}

// StepKind 返回 StepKindLLM，实现 StepDetail 接口。
func (d *LLMCallDetail) StepKind() StepKind { return StepKindLLM }
```

#### ToolCallDetail

```go
// ToolCallDetail 工具调用完整执行数据。
//
// 对应 Python: ToolCallDetail dataclass
type ToolCallDetail struct {
    // ToolName 工具名称
    ToolName string
    // CallArgs 调用参数（可选）
    CallArgs any
    // CallResult 调用结果（可选）
    CallResult any
    // ToolDescription 工具描述（可选）
    ToolDescription string
    // ToolSchema 工具 JSON Schema（可选）
    ToolSchema map[string]any
    // ToolCallID 工具调用 ID，用于脚本产物跟踪（可选）
    ToolCallID string
}

// StepKind 返回 StepKindTool，实现 StepDetail 接口。
func (d *ToolCallDetail) StepKind() StepKind { return StepKindTool }
```

#### TrajectoryStep

```go
// TrajectoryStep 执行轨迹中的单个步骤。
//
// 字段分类：
//   - 核心执行事实：Kind, Error, StartTimeMs, EndTimeMs
//   - 结构化详情：Detail (LLMCallDetail | ToolCallDetail | nil)
//   - 后注入字段：Reward, PromptTokenIDs, CompletionTokenIDs, Logprobs
//   - 扩展元数据：Meta
//
// 对应 Python: TrajectoryStep dataclass
type TrajectoryStep struct {
    // Kind 步骤类型（llm/tool）
    Kind StepKind
    // Error 错误信息（可选）
    Error map[string]any
    // StartTimeMs 步骤开始时间（毫秒时间戳，可选）
    StartTimeMs int
    // EndTimeMs 步骤结束时间（毫秒时间戳，可选）
    EndTimeMs int
    // Detail 结构化步骤数据（LLMCallDetail 或 ToolCallDetail，可选）
    Detail StepDetail
    // Reward 标量奖励，来自 PRM 或 SignalDetector（可选）
    Reward float64
    // PromptTokenIDs 提示词 token ID 列表，仅 kind=llm（可选）
    PromptTokenIDs []int
    // CompletionTokenIDs 补全 token ID 列表，仅 kind=llm（可选）
    CompletionTokenIDs []int
    // Logprobs token 对数概率，仅 kind=llm（可选）
    Logprobs any
    // Meta 扩展元数据，包含 operator_id、agent_id、invoke 关系等
    Meta map[string]any
}
```

#### Trajectory

```go
// Trajectory 完整执行轨迹。
//
// 对应 Python: Trajectory dataclass
type Trajectory struct {
    // ExecutionID 唯一执行标识符
    ExecutionID string
    // Steps 有序执行步骤列表
    Steps []*TrajectoryStep
    // Source 执行来源："online"（deepagents）或 "offline"（trainer）
    Source string
    // CaseID 离线模式下的数据集用例标识（可选）
    CaseID string
    // SessionID 在线模式下的会话 ID（可选）
    SessionID string
    // Cost 聚合成本指标（可选）
    Cost CostInfo
    // Meta 扩展元数据，包含 member_id、member_count 等
    Meta map[string]any
}

// ToMessages 返回 LLM 步骤中记录的消息类字典列表。
//
// 遍历所有 kind=llm 且 detail 为 LLMCallDetail 的步骤，
// 提取 messages 和 response，通过 MessageToDict 标准化为字典。
//
// 对应 Python: Trajectory.to_messages()
func (t *Trajectory) ToMessages() []map[string]any
```

### 工具函数

#### JSONSafe

```go
// JSONSafe 递归转换常见消息/工具调用对象为可 JSON 序列化的值。
//
// 处理规则：
//   - nil → nil
//   - string/int/float64/bool → 原值
//   - []any → 递归每个元素
//   - map[string]any → key 转 string，递归 value
//   - 其他类型 → json.Marshal→json.Unmarshal 到 any（兜底，利用 Go JSON 序列化链）
//   - Marshal 失败 → fmt.Sprint(value) 转字符串
//
// 对应 Python: _json_safe(value)
func JSONSafe(value any) any
```

**与 Python 的对齐说明**：
- Python `isinstance(value, (str, int, float, bool))` → Go 类型 switch `string/int/float64/bool`
- Python `isinstance(value, list/tuple)` → Go `[]any` 递归
- Python `isinstance(value, dict)` → Go `map[string]any` 递归
- Python `getattr(value, "model_dump", None)` callable → Go `json.Marshal→json.Unmarshal`，
  利用 Go 的 `json.Marshaler` 接口等价于 Python 的 `model_dump()`
- Python 兜底 `str(value)` → Go 兜底 `fmt.Sprint(value)`

**细微差异**：`json.Marshal→Unmarshal` 路径会将 int 变为 float64（JSON number 规范），
不影响功能正确性（_json_safe 目标就是"可 JSON 序列化"）。

#### MessageToDict

```go
// MessageToDict 将运行时消息对象标准化为消息类字典。
//
// 处理规则：
//   1. 已经是 map[string]any → 直接 JSONSafe
//   2. 尝试 json.Marshal→json.Unmarshal 到 map[string]any → JSONSafe
//   3. 兜底 → {"role": "unknown", "content": fmt.Sprint(msg)}
//
// 对应 Python: Trajectory._message_to_dict(message)
func MessageToDict(msg any) map[string]any
```

**与 Python 的对齐说明**：
- Python 分支1 `isinstance(message, dict)` → Go 断言 `map[string]any`
- Python 分支2（有 role 属性）和分支3（有 model_dump）→ Go 合并为 `json.Marshal→Unmarshal`
  （Go 中无法 getattr，但 JSON 序列化等价于同时处理"提取字段"和"model_dump"两种情况）
- Python 兜底 `{"role":"unknown","content":str(message)}` → Go `{"role":"unknown","content":fmt.Sprint(msg)}`

#### responseToText

```go
// responseToText 从 LLM 响应中提取文本内容。
//
// 处理规则：
//   1. 有 Content 字段（断言为有 Content 方法的接口）→ 返回 Content
//   2. map[string]any → 取 "content" 或 "text" 键
//   3. 兜底 → fmt.Sprint(response)
//
// 对应 Python: _response_to_text(response)
func responseToText(response any) string
```

---

## 第二步：9.72e BaseOptimizer + LLMResilience

### 包路径

- `internal/evolving/optimizer/` — BaseOptimizer 接口 + BaseOptimizerMixin + TextualParameter
- `internal/evolving/optimizer/llm_resilience/` — LLMInvokePolicy + 重试函数

### 文件结构

```
optimizer/
├── doc.go                # 包文档
├── base.go               # BaseOptimizer 接口 + BaseOptimizerMixin + TextualParameter
└── llm_resilience/
    ├── doc.go            # 包文档
    └── llm_resilience.go # LLMInvokePolicy + InvokeTextWithRetry + InvokeTextWithRetryAndPrompt
```

### BaseOptimizer 接口

```go
// BaseOptimizer 维度优化器的公共接口。
//
// 定义优化器的生命周期：
//   1. Bind() — 过滤并绑定可优化的 Operator，返回匹配数量
//   2. AddTrajectory() — 缓存 Trajectory 供 backward 查询
//   3. Backward() — 从信号计算梯度
//   4. Step() — 从梯度生成更新映射，由 Trainer.apply_updates 统一应用
//
// 对应 Python: BaseOptimizer
type BaseOptimizer interface {
    // Domain 返回优化器域（llm/tool/memory/skill_experience）。
    Domain() string

    // RequiresForwardData 是否需要框架执行前向推理。
    // 返回 false 的黑盒优化器（如 tool_optimizer）在内部生成/执行/评估，
    // 不依赖框架的前向推理数据。
    RequiresForwardData() bool

    // DefaultTargets 返回此维度的默认目标列表。
    DefaultTargets() []string

    // Bind 过滤并绑定可优化的 Operator，返回匹配数量；0 触发上层软退出。
    Bind(operators map[string]operator.Operator, targets []string, config map[string]any) int

    // AddTrajectory 缓存 Trajectory 供 backward 阶段查询。
    AddTrajectory(traj *trajectory.Trajectory)

    // GetTrajectories 返回当前缓存的轨迹列表（副本）。
    GetTrajectories() []*trajectory.Trajectory

    // ClearTrajectories 清空轨迹缓存。
    ClearTrajectories()

    // Backward 反向传播：从信号计算梯度。
    Backward(ctx context.Context, signals []*signal.EvolutionSignal) error

    // Step 生成更新映射，由 Trainer.apply_updates 统一应用。
    Step() map[schema.UpdateKey]any

    // Parameters 返回梯度容器的副本。
    Parameters() map[string]*TextualParameter

    // SelectSignals 选择此优化器可消费的信号。
    // 默认保留全部信号，失败驱动语义的优化器应显式覆盖此方法。
    SelectSignals(signals []*signal.EvolutionSignal) []*signal.EvolutionSignal
}
```

### BaseOptimizerMixin 结构体

```go
// BaseOptimizerMixin 优化器公共逻辑嵌入结构体。
//
// 子优化器嵌入此结构体，获得公共字段和辅助方法（Bind/AddTrajectory/ValidateParameters 等），
// 然后自己实现 BaseOptimizer 接口的全部方法。
//
// 典型子优化器实现模式：
//   - Domain()/RequiresForwardData()/DefaultTargets() — 返回维度常量
//   - Bind() — 委托 o.BaseOptimizerMixin.Bind()
//   - AddTrajectory()/GetTrajectories()/ClearTrajectories() — 委托 Mixin
//   - Backward() — 调用 Mixin.ValidateParameters() + SelectSignals() + 子类逻辑 + 错误包装
//   - Step() — 调用 Mixin.ValidateParameters() + 子类逻辑 + ClearTrajectories()
//   - Parameters()/SelectSignals() — 委托 Mixin
type BaseOptimizerMixin struct {
    operators       map[string]operator.Operator
    parameters      map[string]*TextualParameter
    targets         []string
    trajectories    []*trajectory.Trajectory
    selectedSignals []*signal.EvolutionSignal
}
```

**Mixin 提供的方法**：

| 方法 | 说明 | Python 对应 |
|---|---|---|
| `Bind(operators, targets, config) int` | 过滤绑定 + 创建 TextualParameter + 重置 trajectories/signals | `BaseOptimizer.bind()` |
| `AddTrajectory(traj)` | 缓存轨迹 | `BaseOptimizer.add_trajectory()` |
| `GetTrajectories() []*Trajectory` | 返回轨迹副本 | `BaseOptimizer.get_trajectories()` |
| `ClearTrajectories()` | 清空轨迹 | `BaseOptimizer.clear_trajectories()` |
| `Parameters() map[string]*TextualParameter` | 返回参数副本 | `BaseOptimizer.parameters()` |
| `SelectSignals(signals) []*EvolutionSignal` | 默认全选 | `BaseOptimizer._select_signals()` |
| `FilterOperators(operators, targets) map[string]Operator` | 静态过滤，日志警告不匹配的 | `BaseOptimizer.filter_operators()` |
| `ValidateParameters()` | 空参数校验，抛异常 | `BaseOptimizer._validate_parameters()` |

**子优化器如何使用 Mixin**：

Go 中嵌入结构体无法实现"子类覆盖方法后父类回调子类方法"的模式，
因此 Mixin **不实现** `Backward()`/`Step()`/`Domain()`/`RequiresForwardData()`/`DefaultTargets()`
这 5 个 BaseOptimizer 接口方法。子优化器自己实现这些接口方法，在实现中调用 Mixin 的辅助方法。

具体来说，子优化器实现 `Backward()` 的典型模式：

```go
func (o *InstructionOptimizer) Backward(ctx context.Context, signals []*signal.EvolutionSignal) error {
    // 1. 调用 Mixin 校验（对应 Python BaseOptimizer.backward 中的 _validate_parameters）
    o.BaseOptimizerMixin.ValidateParameters()
    // 2. 调用 Mixin 信号选择（对应 Python BaseOptimizer.backward 中的 _select_signals）
    o.selectedSignals = o.SelectSignals(signals)
    // 3. 执行子类自己的 backward 逻辑（对应 Python _backward）
    if err := o.backwardInner(ctx, signals); err != nil {
        return exception.NewBaseError(
            StatusCodeTOOLCHAIN_OPTIMIZER_BACKWARD_EXECUTION_ERROR,
            exception.WithMsg(err.Error()),
            exception.WithCause(err),
        )
    }
    return nil
}
```

子优化器实现 `Step()` 的典型模式：

```go
func (o *InstructionOptimizer) Step() map[schema.UpdateKey]any {
    // 1. 调用 Mixin 校验（对应 Python BaseOptimizer.step 中的 _validate_parameters）
    o.BaseOptimizerMixin.ValidateParameters()
    // 2. 执行子类自己的 step 逻辑（对应 Python _step）
    updates, err := o.stepInner()
    // 3. 无论成功失败都清空轨迹（对齐 Python BaseOptimizer.step）
    o.ClearTrajectories()
    if err != nil {
        return map[schema.UpdateKey]any{} // 或按需 panic/返回错误
    }
    return updates
}
```

这种"Mixin 提供辅助方法 + 子优化器自己组装 Backward/Step"的模式，
虽然比 Python 的"基类模板方法"稍显冗长，但职责清晰、无隐式耦合。

### TextualParameter 结构体

```go
// TextualParameter operator_id 的梯度容器，存储 target→梯度值和可选描述。
// 不再持有 Operator 引用。
//
// 对应 Python: TextualParameter
type TextualParameter struct {
    // OperatorID 所属 Operator 标识
    OperatorID string
    // Gradients 梯度映射 target → gradient value (string 或 []string)
    Gradients map[string]any
    // Description 可选描述
    Description string
}

// SetGradient 设置目标梯度值。
func (p *TextualParameter) SetGradient(name string, gradient any)

// GetGradient 获取目标梯度值。
func (p *TextualParameter) GetGradient(name string) any

// SetDescription 设置描述。
func (p *TextualParameter) SetDescription(description string)

// GetDescription 获取描述。
func (p *TextualParameter) GetDescription() string
```

### LLMResilience

#### LLMInvokePolicy

```go
// LLMInvokePolicy 单次演化层 LLM 调用的策略配置。
//
// 对应 Python: LLMInvokePolicy (frozen dataclass)
type LLMInvokePolicy struct {
    // AttemptTimeoutSecs 单次尝试超时（秒）
    AttemptTimeoutSecs float64
    // TotalBudgetSecs 总预算时间（秒）
    TotalBudgetSecs float64
    // MaxAttempts 最大尝试次数，默认 2
    MaxAttempts int
    // BackoffBaseSecs 退避基数（秒），默认 1.0
    BackoffBaseSecs float64
    // RetryEmptyResponse 是否重试空响应，默认 true
    RetryEmptyResponse bool
}
```

#### InvokeTextWithRetry

```go
// InvokeTextWithRetry 带重试策略的 LLM 文本调用，只返回文本结果。
//
// 对应 Python: invoke_text_with_retry()
func InvokeTextWithRetry(
    ctx context.Context,
    model *llm.Model,
    modelName string,
    prompt string,
    policy LLMInvokePolicy,
    opts ...InvokeRetryOption,
) (string, error)
```

#### InvokeTextWithRetryAndPrompt

```go
// InvokeTextWithRetryAndPrompt 带重试策略的 LLM 文本调用，返回文本和实际使用的 prompt。
//
// 重试逻辑处理三种失败模式：
//   1. 调用异常（超时检测 + 可选 retry_prompt 回退）
//   2. 空响应
//   3. 不可用响应（通过 isResultUsable 回调判断）
//
// 总预算控制：外层 context.WithTimeout + 每次 attempt 前手动检查剩余预算
//
// 对应 Python: invoke_text_with_retry_and_prompt()
func InvokeTextWithRetryAndPrompt(
    ctx context.Context,
    model *llm.Model,
    modelName string,
    prompt string,
    policy LLMInvokePolicy,
    opts ...InvokeRetryOption,
) (text string, promptUsed string, err error)
```

#### InvokeRetryOption

```go
// InvokeRetryOption 重试调用选项函数。
type InvokeRetryOption func(*invokeRetryConfig)

type invokeRetryConfig struct {
    retryPrompt    string                  // 超时时使用的短 prompt
    temperature    float64                 // 温度参数
    isResultUsable func(string) bool       // 结果可用性判断回调
    extra          map[string]any          // 额外参数
}

// WithRetryPrompt 设置超时重试时使用的短 prompt。
func WithRetryPrompt(prompt string) InvokeRetryOption

// WithRetryTemperature 设置温度参数。
func WithRetryTemperature(t float64) InvokeRetryOption

// WithIsResultUsable 设置结果可用性判断回调。
func WithIsResultUsable(f func(string) bool) InvokeRetryOption

// WithRetryExtra 设置额外参数。
func WithRetryExtra(extra map[string]any) InvokeRetryOption
```

#### 辅助函数

```go
// isTimeoutLike 判断错误是否为超时类型。
//
// 检测规则：
//   1. context.DeadlineExceeded → true
//   2. 错误类型名包含 "timeout" → true
//   3. 错误消息包含 "timeout" 或 "timed out" → true
//
// 对应 Python: _is_timeout_like(exc)
func isTimeoutLike(err error) bool

// sleepBeforeRetry 指数退避等待，尊重剩余预算。
//
// 退避计算：backoff = backoffBase * 2^(attempt-1)
// 实际等待 = min(backoff, remainingBudget)
//
// 对应 Python: _sleep_before_retry()
func sleepBeforeRetry(ctx context.Context, policy LLMInvokePolicy, startedAt time.Time, attempt int) error
```

#### 总预算控制实现

```go
// 外层 context.WithTimeout 包裹整个重试循环
budgetCtx, cancel := context.WithTimeout(ctx, time.Duration(policy.TotalBudgetSecs*float64(time.Second)))
defer cancel()

// 每次 attempt 前手动检查剩余预算
elapsed := time.Since(startedAt).Seconds()
remainingBudget := policy.TotalBudgetSecs - elapsed
if remainingBudget <= 0 {
    return raiseLLMResilienceError(...)
}

// 单次 attempt timeout 取 min(attemptTimeout, remainingBudget)
timeoutSecs := math.Min(policy.AttemptTimeoutSecs, remainingBudget)

// LLM 调用使用 budgetCtx + 单次 timeout
model.Invoke(budgetCtx, messages,
    WithInvokeModel(modelName),
    WithInvokeTemperature(temperature),
    WithInvokeTimeout(timeoutSecs),
)
```

#### LLM 调用对接

Python:
```python
response = await llm.invoke(
    model=model,
    messages=[{"role": "user", "content": current_prompt}],
    temperature=temperature,
    timeout=timeout_secs,
    **kwargs,
)
```

Go:
```go
messages := model_clients.MessagesParam{
    {Role: "user", Content: currentPrompt},
}
response, err := model.Invoke(budgetCtx, messages,
    model_clients.WithInvokeModel(modelName),
    model_clients.WithInvokeTemperature(temperature),
    model_clients.WithInvokeTimeout(timeoutSecs),
)
```

#### 错误构建

```go
// raiseLLMResilienceError 统一构建 LLM 弹性重试错误。
//
// 对应 Python: _raise_llm_resilience_error()
// 使用项目的 exception 包构建错误，包含：
//   - StatusCode（区分执行错误和解析错误）
//   - reason: 失败原因（total_budget_exceeded/invoke_failed/empty_response/unusable_response）
//   - details: {reason, attempts, last_response, last_error}
func raiseLLMResilienceError(
    statusCode exception.StatusCode,
    reason string,
    attempts int,
    lastError error,
    lastResponse string,
    cause error,
) error
```

---

## 第三步：回填已有代码

### 回填清单

| 文件 | 当前状态 | 回填操作 |
|---|---|---|
| `updater/single_dim/single_dim.go` | `opt any` + 5 个类型断言（binder/requirer/trajectoryAdder/backwarder/stepper） | 替换 `opt any` → `opt optimizer.BaseOptimizer`，删除所有类型断言 + 警告日志，直接调用接口方法 |
| `updater/multi_dim/multi_dim.go` | `domainOptimizers map[string]any` + 类型断言 | 替换 `map[string]any` → `map[string]optimizer.BaseOptimizer`，删除类型断言 |
| `updater/protocol.go` | `trajectories []any`（两处 `⤵️ 9.77` 标记） | 替换 `[]any` → `[]*trajectory.Trajectory` |
| `updater/single_dim/single_dim_test.go` | mockOptimizer 实现 5 个断言接口 | 重写为实现 `optimizer.BaseOptimizer` 接口的 mock 结构体 |
| `updater/multi_dim/multi_dim_test.go` | 类似 mock | 类似重写 |
| `trainer/trainer.go` | `extractor any` 注释 | 更新注释，标记 `⤴️ 9.72e` 来源 |
| `evolving/doc.go` | 子包列表缺少 trajectory/optimizer | 添加 trajectory/optimizer 子包 |
| `IMPLEMENTATION_PLAN.md` | 9.77 ☐、9.72e ☐ | 9.77 → ✅、9.72e → ✅ |

### 回填时注意事项

1. **SingleDimUpdater 回填**：将 `opt any` 替换为 `opt optimizer.BaseOptimizer` 后，
   所有方法（Bind/RequiresForwardData/Process/Update）中的类型断言全部替换为直接接口调用，
   消除所有 `⤵️ 9.72e 回填后消除` 的警告日志。

2. **MultiDimUpdater 回填**：`domainOptimizers map[string]any` 替换为 `map[string]optimizer.BaseOptimizer`，
   `WithDomainOptimizers` 选项函数参数类型同步修改，
   `DomainOptimizers()` 返回值类型同步修改。

3. **Updater protocol 回填**：`trajectories []any` 替换为 `[]*trajectory.Trajectory` 后，
   Updater 接口的 Update/Process 方法签名变化，所有实现者需同步修改。

4. **import 循环依赖检查**：
   - `optimizer` 包导入 `trajectory`、`signal`、`operator`、`schema`、`exception`、`logger`
   - `trajectory` 包零外部依赖（纯数据结构 + encoding/json + fmt + time）
   - `llm_resilience` 子包导入 `llm`、`exception`、`logger`
   - 无循环依赖风险

---

## 测试策略

### 9.77 Trajectory 测试

| 测试文件 | 覆盖内容 |
|---|---|
| `trajectory/types_test.go` | StepKind 常量、LLMCallDetail/ToolCallDetail.StepKind()、TrajectoryStep 字段、Trajectory.ToMessages()、Trajectory 默认 Source="offline" |
| `trajectory/json_safe_test.go` | JSONSafe 各分支（nil/基础类型/切片/映射/自定义对象兜底）、MessageToDict 各分支、responseToText 各分支 |

### 9.72e Optimizer 测试

| 测试文件 | 覆盖内容 |
|---|---|
| `optimizer/base_test.go` | BaseOptimizerMixin.Bind/AddTrajectory/GetTrajectories/ClearTrajectories/Parameters/FilterOperators/ValidateParameters/SelectSignals、TextualParameter 全部方法、Backward 错误包装、Step 错误包装 |
| `optimizer/llm_resilience/llm_resilience_test.go` | LLMInvokePolicy 默认值、InvokeTextWithRetry 成功/重试/超时/空响应/不可用响应各场景、isTimeoutLike 各判断分支、sleepBeforeRetry 退避计算、raiseLLMResilienceError 错误构建 |

### 回填后回归测试

| 测试文件 | 覆盖内容 |
|---|---|
| `updater/single_dim/single_dim_test.go` | 重写后全量回归 |
| `updater/multi_dim/multi_dim_test.go` | 重写后全量回归 |
| `updater/protocol_test.go` | 接口签名变化后回归 |

---

## 与 Python 的差异汇总

| # | 差异点 | Python | Go | 影响 |
|---|---|---|---|---|
| 1 | 抽象类 → 接口+Mixin | `class BaseOptimizer` 抽象类（模板方法模式） | `BaseOptimizer` 接口 + `BaseOptimizerMixin` 辅助结构体（子类组装模式） | Go 无法继承+模板方法，子优化器自己组装 Backward/Step |
| 2 | 类属性 → 接口方法 | `domain: str = ""` | `Domain() string` | 调用方式不同 |
| 3 | 静态方法 → 接口方法 | `@staticmethod requires_forward_data()` | `RequiresForwardData() bool` | 调用方式不同 |
| 4 | async context manager | `__aenter__`/`__aexit__` | 不需要 | Go 不用 |
| 5 | Union 类型 → interface | `StepDetail = Union[LLMCallDetail, ToolCallDetail]` | `StepDetail` interface + `StepKind()` 判别方法 | 增加了判别方法 |
| 6 | model_dump → JSON 序列化 | `getattr(value, "model_dump")` | `json.Marshal→json.Unmarshal` | 数值 int→float64 |
| 7 | getattr → JSON 序列化 | `getattr(message, "role")` | `json.Marshal→json.Unmarshal` + 键检查 | 合并了 Python 的两个分支 |
| 8 | asyncio.timeout → 双重控制 | `async with asyncio.timeout()` | `context.WithTimeout` + 手动检查 | 更健壮 |
| 9 | `**kwargs` → Option 模式 | `llm.invoke(..., **kwargs)` | `InvokeRetryOption` + `model_clients.WithInvokeXxx` | Go-idiomatic |
