# 9.5 LoopCoordinator + StopConditionEvaluator 设计

> 本文档描述 DeepAgent 外层任务循环的停止条件评估系统设计，
> 对应实现计划步骤 9.5，Python 源码 `openjiuwen/harness/task_loop/loop_coordinator.py` + `openjiuwen/harness/schema/stop_condition.py`。

## 1. 在 Agent 会话中的流程位置与作用

```
用户输入 → Gateway → E2A → AgentServer
                              ↓
                         DeepAgent（9.1）
                              ↓
                    ┌─────────────────────────┐
                    │  外层任务循环 (TaskLoop)  │  ← LoopCoordinator 在此控制循环
                    │  ┌───────────────────┐  │
                    │  │ LoopCoordinator   │  │  ← 追踪迭代/token/耗时/中止
                    │  │   ├─ MaxRounds    │  │  ← 裁判：轮次够了吗？
                    │  │   ├─ Timeout      │  │  ← 裁判：超时了吗？
                    │  │   ├─ TokenBudget  │  │  ← 裁判：token 用完了吗？
                    │  │   └─ Completion   │  │  ← 裁判：任务自己说完成了吗？
                    │  └───────────────────┘  │
                    │  Rail 前置/后置钩子      │
                    │  子 Agent 调度           │
                    └─────────┬───────────────┘
                              ↓
                      ReActAgent (6.11)       ← 内层 Think-Act-Observe 循环
                              ↓
                      LLM + Tools (2.x + 3.x)
```

**LoopCoordinator** 是外层任务循环的"调度员"，负责：
- 追踪全局循环状态（迭代次数、token 用量、耗时、中止标记）
- 每轮迭代前调用 `ShouldContinue()` 评估是否继续
- 支持状态导出/导入以实现 checkpoint 持久化

**StopConditionEvaluator** 是循环的"裁判"，每个裁判回答一个问题："该停了吗？"。LoopCoordinator 持有评估器切片，OR 语义：第一个返回 true 即停止。

## 2. Python 源码对照

### Python 调用链

```python
# LoopCoordinator.should_continue()
def should_continue(self) -> bool:
    if self._aborted:
        return False
    for evaluator in self._evaluators:
        if evaluator.should_stop(context):
            self._stop_reason = evaluator.name
            return False
    return True
```

### Python 状态导出

```python
def get_state(self):
    return {
        "iteration": self._iteration,
        "token_usage": self._token_usage,
        "stop_reason": self._stop_reason,
        "evaluator_states": {
            ev.name: ev.get_state()
            for ev in self._evaluators
        },
    }
```

### CompletionPromiseEvaluator 的特殊状态

```python
class CompletionPromiseEvaluator:
    def __init__(self, promise, required_confirmations=2):
        self._promise = promise
        self._required = required_confirmations
        self._confirmations = 0  # 跨轮次状态

    def should_stop(self, context):
        result_text = context.last_result
        if self._promise in result_text:
            self._confirmations += 1
        else:
            self._confirmations = 0
        return self._confirmations >= self._required
```

## 3. 状态持久化流程

```
LoopCoordinator (内存中)
    │ ExportState() → LoopCoordinatorState{iteration, tokenUsage, stopReason, evaluatorStates}
    ↓
DeepAgentState.stop_condition_state  ← 嵌套持有 LoopCoordinatorState
    │
    ↓
session.UpdateState({"deepagent": DeepAgentState.toSessionDict()})
    │ → AgentStateCollection.globalState.Update()
    ↓
checkpointer.PostAgentExecute()
    │ → session.State().GetState()
    │ → JSONSerializer.DumpsTyped() → JSON bytes
    ↓
KV Store / InMemory (持久化)
```

Go 的 checkpointer 已完整实现生命周期钩子，DeepAgent 只需在每轮迭代后调用 `session.UpdateState()` 写入状态，checkpointer 自动持久化。

## 4. 设计决策

| 决策项 | 选择 | 原因 |
|--------|------|------|
| StopConditionEvaluator 模式 | 接口 + 切片遍历，OR 语义 | Go 惯用，与 Python OR 语义链对齐 |
| 状态持久化 | ExportState/ImportState 方法 | 字段和逻辑与 Python 一致，类型安全 |
| 有状态评估器 | 全部接口包含状态方法 | 统一接口，无状态评估器返回空 map |
| 并发安全 | sync.Mutex 保护所有字段 | Go 多协程环境下 abort/tokenUsage 可能跨协程访问 |

## 5. Go 代码结构

```
internal/agentcore/harness/
├── task_loop/
│   ├── doc.go
│   ├── loop_coordinator.go           # LoopCoordinator + LoopCoordinatorState
│   ├── loop_coordinator_test.go
│   ├── stop_condition.go             # StopConditionEvaluator 接口 + 5 个评估器实现
│   └── stop_condition_test.go
```

## 6. 核心接口与结构体

### 6.1 StopConditionEvaluator 接口

```go
// StopConditionEvaluator 停止条件评估器接口
// 对齐 Python: StopConditionEvaluator 基类
type StopConditionEvaluator interface {
    // ShouldStop 评估是否应该停止循环
    ShouldStop(ctx StopEvaluationContext) bool

    // Name 返回评估器名称（用于状态序列化索引和日志）
    Name() string

    // ExportState 导出评估器状态（无状态评估器返回空 map）
    ExportState() map[string]any

    // ImportState 导入评估器状态（无状态评估器忽略）
    ImportState(data map[string]any)
}
```

### 6.2 StopEvaluationContext

```go
// StopEvaluationContext 停止条件评估上下文
// 对齐 Python: StopEvaluationContext
type StopEvaluationContext struct {
    // Iteration 当前迭代次数
    Iteration int
    // TokenUsage 累计 token 用量
    TokenUsage int
    // ElapsedSeconds 已用时间（秒）
    ElapsedSeconds float64
    // LastResult 上一轮结果
    LastResult map[string]any
    // Extra 额外上下文
    Extra map[string]any
}
```

### 6.3 LoopCoordinatorState

```go
// LoopCoordinatorState 循环协调器可序列化状态
// 对齐 Python: LoopCoordinator.get_state() 返回值
type LoopCoordinatorState struct {
    // Iteration 迭代次数
    Iteration int `json:"iteration"`
    // TokenUsage 累计 token 用量
    TokenUsage int `json:"token_usage"`
    // StopReason 停止原因（评估器名称）
    StopReason string `json:"stop_reason"`
    // EvaluatorStates 各评估器状态（按 Name() 索引）
    EvaluatorStates map[string]map[string]any `json:"evaluator_states"`
}
```

### 6.4 LoopCoordinator

```go
// LoopCoordinator 外层任务循环协调器
// 对齐 Python: LoopCoordinator
type LoopCoordinator struct {
    mu           sync.Mutex
    iteration    int
    tokenUsage   int
    aborted      bool
    startTime    time.Time
    stopReason   string
    lastResult   map[string]any
    evaluators   []StopConditionEvaluator
}

// NewLoopCoordinator 创建循环协调器
func NewLoopCoordinator(evaluators []StopConditionEvaluator) *LoopCoordinator

// ShouldContinue 评估是否应该继续循环
// 对齐 Python: LoopCoordinator.should_continue()
// OR 语义：遍历评估器，第一个 ShouldStop=true 即停止
func (lc *LoopCoordinator) ShouldContinue() bool

// IncrementIteration 递增迭代次数
// 对齐 Python: LoopCoordinator.increment_iteration()
func (lc *LoopCoordinator) IncrementIteration()

// AddTokenUsage 累加 token 用量
// 对齐 Python: LoopCoordinator.add_token_usage()
func (lc *LoopCoordinator) AddTokenUsage(tokens int)

// SetLastResult 设置上一轮结果
// 对齐 Python: LoopCoordinator.set_last_result()
func (lc *LoopCoordinator) SetLastResult(result map[string]any)

// RequestAbort 请求中止循环
// 对齐 Python: LoopCoordinator.request_abort()
func (lc *LoopCoordinator) RequestAbort()

// IsAborted 返回是否已中止
func (lc *LoopCoordinator) IsAborted() bool

// StopReason 返回停止原因
func (lc *LoopCoordinator) StopReason() string

// Iteration 返回当前迭代次数
func (lc *LoopCoordinator) Iteration() int

// TokenUsage 返回累计 token 用量
func (lc *LoopCoordinator) TokenUsage() int

// ElapsedSeconds 返回已用时间（秒）
func (lc *LoopCoordinator) ElapsedSeconds() float64

// ExportState 导出状态用于持久化
// 对齐 Python: LoopCoordinator.get_state()
func (lc *LoopCoordinator) ExportState() LoopCoordinatorState

// ImportState 从持久化状态恢复
// 对齐 Python: LoopCoordinator.load_state()
func (lc *LoopCoordinator) ImportState(state LoopCoordinatorState)

// GetCompletionPromiseEvaluator 返回第一个 CompletionPromiseEvaluator（如有）
// 对齐 Python: LoopCoordinator.get_completion_promise_evaluator()
func (lc *LoopCoordinator) GetCompletionPromiseEvaluator() *CompletionPromiseEvaluator
```

## 7. 评估器实现

### 7.1 MaxRoundsEvaluator

```go
// MaxRoundsEvaluator 最大轮次评估器
// 对齐 Python: MaxRoundsEvaluator
type MaxRoundsEvaluator struct {
    // maxRounds 最大轮次
    maxRounds int
}

func NewMaxRoundsEvaluator(maxRounds int) *MaxRoundsEvaluator
func (e *MaxRoundsEvaluator) ShouldStop(ctx StopEvaluationContext) bool  // ctx.Iteration >= maxRounds
func (e *MaxRoundsEvaluator) Name() string                              // "max_rounds"
func (e *MaxRoundsEvaluator) ExportState() map[string]any               // {}
func (e *MaxRoundsEvaluator) ImportState(data map[string]any)            // 忽略
```

### 7.2 TokenBudgetEvaluator

```go
// TokenBudgetEvaluator token 预算评估器
// 对齐 Python: TokenBudgetEvaluator
type TokenBudgetEvaluator struct {
    // maxTokens 最大 token 数
    maxTokens int
}

func NewTokenBudgetEvaluator(maxTokens int) *TokenBudgetEvaluator
func (e *TokenBudgetEvaluator) ShouldStop(ctx StopEvaluationContext) bool  // ctx.TokenUsage >= maxTokens
func (e *TokenBudgetEvaluator) Name() string                              // "token_budget"
func (e *TokenBudgetEvaluator) ExportState() map[string]any               // {}
func (e *TokenBudgetEvaluator) ImportState(data map[string]any)            // 忽略
```

### 7.3 TimeoutEvaluator

```go
// TimeoutEvaluator 超时评估器
// 对齐 Python: TimeoutEvaluator
type TimeoutEvaluator struct {
    // timeoutSeconds 超时秒数
    timeoutSeconds float64
}

func NewTimeoutEvaluator(timeoutSeconds float64) *TimeoutEvaluator
func (e *TimeoutEvaluator) ShouldStop(ctx StopEvaluationContext) bool  // ctx.ElapsedSeconds >= timeoutSeconds
func (e *TimeoutEvaluator) Name() string                              // "timeout"
func (e *TimeoutEvaluator) ExportState() map[string]any               // {}
func (e *TimeoutEvaluator) ImportState(data map[string]any)            // 忽略
```

### 7.4 CompletionPromiseEvaluator

```go
// CompletionPromiseEvaluator 完成承诺评估器
// 对齐 Python: CompletionPromiseEvaluator
// 有状态评估器：追踪连续检测到 promise 标签的次数
type CompletionPromiseEvaluator struct {
    // promise 要匹配的标签（如 "<promise>"）
    promise string
    // requiredConfirmations 需要连续检测到的次数
    requiredConfirmations int
    // confirmations 当前已连续检测到的次数
    confirmations int
}

func NewCompletionPromiseEvaluator(promise string, requiredConfirmations int) *CompletionPromiseEvaluator
func (e *CompletionPromiseEvaluator) ShouldStop(ctx StopEvaluationContext) bool
    // 伪代码：
    //   if promise in lastResult → confirmations++
    //   else → confirmations = 0
    //   return confirmations >= requiredConfirmations
func (e *CompletionPromiseEvaluator) Name() string                              // "completion_promise"
func (e *CompletionPromiseEvaluator) ExportState() map[string]any               // {"confirmations": e.confirmations}
func (e *CompletionPromiseEvaluator) ImportState(data map[string]any)            // 恢复 confirmations

// Confirmations 返回当前确认次数（供外部查询）
func (e *CompletionPromiseEvaluator) Confirmations() int
```

### 7.5 CustomPredicateEvaluator

```go
// CustomPredicateEvaluator 自定义谓词评估器
// 对齐 Python: CustomPredicateEvaluator
type CustomPredicateEvaluator struct {
    // name 评估器名称
    name string
    // predicate 自定义判断函数
    predicate func(ctx StopEvaluationContext) bool
}

func NewCustomPredicateEvaluator(name string, predicate func(ctx StopEvaluationContext) bool) *CustomPredicateEvaluator
func (e *CustomPredicateEvaluator) ShouldStop(ctx StopEvaluationContext) bool  // e.predicate(ctx)
func (e *CustomPredicateEvaluator) Name() string                              // e.name
func (e *CustomPredicateEvaluator) ExportState() map[string]any               // {}
func (e *CustomPredicateEvaluator) ImportState(data map[string]any)            // 忽略
```

## 8. 测试覆盖

| 测试文件 | 覆盖内容 |
|---------|---------|
| `loop_coordinator_test.go` | NewLoopCoordinator 构造; ShouldContinue 空评估器/单个/多个/OR 语义; IncrementIteration; AddTokenUsage; SetLastResult; RequestAbort; ExportState/ImportState 往返; GetCompletionPromiseEvaluator |
| `stop_condition_test.go` | MaxRoundsEvaluator 边界值; TokenBudgetEvaluator 边界值; TimeoutEvaluator 边界值; CompletionPromiseEvaluator 连续/中断/状态导出导入; CustomPredicateEvaluator 基本功能 |

## 9. 回填点

本步骤实现后需要回填的内容：无（9.5 是纯逻辑层，无上游回填依赖）。

已有回填点（不受影响，保持不变）：
- 5.21 `⤵️ 9.32 回填 ProcessorOption.SysOperation 类型 + writeOffloadToFile 改用 SysOperation`
- 5.29 `⤵️ 9.32 回填 writeOffloadToFile SysOperation`
- 5.31 `⤵️ 9.32 回填 ProcessorOption.SysOperation + writeOffloadToFile`

## 10. 前置步骤建议

9.5 无硬性前置依赖（domains 1-6 已全部完成）。但建议在 9.5 之前或同时确认：
- **9.2 DeepAgentConfig + DeepAgentState** 中 `stop_condition_state` 字段的类型为 `LoopCoordinatorState`，确保序列化兼容
- **6.22 ControllerAgent** 虽然不是 9.5 的前置，但 9.4 TaskLoopController 嵌入的 Controller 已完整实现，无需等待
