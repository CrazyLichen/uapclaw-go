# 9.12 TaskCompletionRail 设计文档

## 概述

TaskCompletionRail 是 DeepAgent 任务循环的"终止信号管理器"，负责三件事：

1. **注入完成信号提示**（`BeforeModelCall`）：在每次 LLM 调用前，向系统提示词注入 `<promise>{token}</promise>` 指令
2. **格式化首轮指令**（`BeforeTaskIteration`）：首轮迭代时将 `taskInstruction` 模板应用到查询
3. **检测完成承诺**（`AfterTaskIteration`）：迭代完成后检测输出中的 `<promise>...</promise>` 标签，通知 `CompletionPromiseEvaluator`

## 在 Agent 会话流程中的位置

```
DeepAgent.configure()
  └── queuePendingRails()  →  enable_task_loop=True 时自动创建 TaskCompletionRail()

DeepAgent.ensureInitialized()
  └── TaskCompletionRail.Init(agent)
  └── registerRailSelective()  →  注册 3 个生命周期回调

DeepAgent.setupTaskLoop()
  └── taskCompletionRail.BuildEvaluators()  →  [MaxRoundsEvaluator, TimeoutEvaluator, CompletionPromiseEvaluator]
  └── LoopCoordinator(evaluators=evaluators)

Loop while coordinator.ShouldContinue():
  ├── BeforeModelCall:        注入 <promise> 完成信号提示到 system prompt
  ├── BeforeTaskIteration:    格式化 taskInstruction 模板到首轮查询
  ├── 执行一轮 ReActAgent
  ├── AfterTaskIteration:     检测 <promise> 标签 → NotifyFulfilled()
  └── coordinator.ShouldContinue()  →  False（循环结束）
```

## 依赖状态（全部 ✅）

| 依赖 | 条目 | 状态 |
|------|------|------|
| TaskLoopController/LoopCoordinator | 9.4-9.6 | ✅ |
| StopConditionEvaluator（含 CompletionPromiseEvaluator） | 9.5子组件 | ✅ |
| PromptSection + SystemPromptBuilder | 6.29 | ✅ |
| BuildCompletionSignalSection | 9.51-53子组件 | ✅ |
| ProgressiveToolRail（同组参考实现） | 9.11 | ✅ |

## 文件变更清单

| # | 文件 | 变更类型 | 说明 |
|---|------|---------|------|
| 1 | `rails/task_completion.go` | 新建 | TaskCompletionRail 结构体 + 3钩子 + BuildEvaluators + 辅助函数 |
| 2 | `rails/task_completion_test.go` | 新建 | 单元测试 |
| 3 | `rails/doc.go` | 修改 | 文件目录添加 task_completion.go |
| 4 | `harness/interfaces/deep_agent.go` | 修改 | LoopCoordinatorInterface 添加 GetCompletionPromiseEvaluator() |
| 5 | `harness/deep_agent.go` | 修改 | 回填5处占位 |

## 详细设计

### 1. TaskCompletionRail 结构体

```go
type TaskCompletionRail struct {
    DeepAgentRail
    taskInstruction       string
    completionPromise     string
    requiredConfirmations int
    allowPromiseDetails   bool
    maxRounds             int
    timeoutSeconds        float64
    extraEvaluators       []task_loop.StopConditionEvaluator
}
```

**字段说明：**

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| taskInstruction | string | "" | 带 `{query}` 占位符的格式模板，首轮迭代时应用到查询 |
| completionPromise | string | "" | 模型须在 `<promise>...</promise>` 标签内输出以宣告完成的令牌 |
| requiredConfirmations | int | 1 | 需要连续确认的次数 |
| allowPromiseDetails | bool | false | 是否允许 promise 块包含额外详情 |
| maxRounds | int | 0 | 外层循环最大轮数（0 = 不限） |
| timeoutSeconds | float64 | 0 | 整个任务循环的墙钟超时秒数（0 = 不限） |
| extraEvaluators | []StopConditionEvaluator | nil | 额外自定义评估器 |

**构造函数：**

```go
func NewTaskCompletionRail(opts ...TaskCompletionOption) *TaskCompletionRail
```

使用 Functional Options 模式，与 Python 的 `TaskCompletionRail(**kwargs)` 对齐。

### 2. 三个生命周期钩子

#### BeforeModelCall

```
如果 completionPromise 为空 → 直接返回
获取 builder := cbc.Agent().SystemPromptBuilder()
如果 builder 为 nil → 直接返回
获取 language := builder.Language()
构建 section := sections.BuildCompletionSignalSection(completionPromise, language)
builder.AddSection(section)
```

#### BeforeTaskIteration

```
如果 taskInstruction 为空 → 直接返回
获取 inputs := cbc.Inputs().(*TaskIterationInputs)
如果 inputs.Query 为空 → 直接返回
如果 inputs.IsFollowUp → 直接返回（follow-up 查询不格式化）
inputs.Query = strings.ReplaceAll(taskInstruction, "{query}", inputs.Query)
```

#### AfterTaskIteration

```
如果 completionPromise 为空 → 直接返回
获取 content := extractOutput(cbc)（从 inputs.Result["output"] 提取）
如果 content 为空 → 直接返回
promiseBlock := extractPromiseBlock(content)
如果 promiseBlock 为空 → 直接返回
matched := normalize(promiseBlock)
expected := normalize(completionPromise)
如果 matched != expected:
    如果 !allowPromiseDetails → 直接返回
    如果 !promiseMatches(promiseBlock, completionPromise) → 直接返回
    matched = expected
记录日志: promise fulfilled
notifyEvaluator(cbc, matched)
```

### 3. BuildEvaluators 方法

```go
func (r *TaskCompletionRail) BuildEvaluators() []task_loop.StopConditionEvaluator
```

按顺序构建评估器链：
1. maxRounds > 0 → 添加 MaxRoundsEvaluator
2. timeoutSeconds > 0 → 添加 TimeoutEvaluator
3. completionPromise != "" → 添加 CompletionPromiseEvaluator
4. 追加 extraEvaluators

### 4. GetCallbacks 覆盖

```go
func (r *TaskCompletionRail) GetCallbacks() map[...]cb.PerAgentCallbackFunc {
    callbacks := r.DeepAgentRail.GetCallbacks()
    callbacks[CallbackBeforeModelCall] = ...
    callbacks[CallbackBeforeTaskIteration] = ...
    callbacks[CallbackAfterTaskIteration] = ...
    return callbacks
}
```

### 5. 辅助函数

| 函数 | 说明 | 对齐Python |
|------|------|-----------|
| normalize(text string) string | 折叠空白字符用于 promise 比较 | _normalize |
| extractPromiseBlock(text string) string | 用正则提取 `<promise>...</promise>` 内容 | extract_promise_block |
| promiseMatches(block, expected string) bool | 判断 promise 块首行是否以期望令牌开头 | promise_matches |
| extractOutput(cbc) string | 从 TaskIterationInputs.Result["output"] 提取输出文本 | _extract_output |
| notifyEvaluator(cbc, text) | 获取 LoopCoordinator → CompletionPromiseEvaluator → NotifyFulfilled | _notify_evaluator |

### 6. LoopCoordinatorInterface 扩展（方案C：最小接口）

由于 `interfaces` 包不能导入 `task_loop` 包（循环依赖：task_loop → interfaces），采用定义最小接口方式：

```go
// CompletionPromiseEvaluatorInterface 完成承诺评估器接口（最小集）。
type CompletionPromiseEvaluatorInterface interface {
    NotifyFulfilled(matchedText string)
}

type LoopCoordinatorInterface interface {
    Iteration() int
    RequestAbort()
    GetCompletionPromiseEvaluator() CompletionPromiseEvaluatorInterface
}
```

`*task_loop.CompletionPromiseEvaluator` 隐式满足 `CompletionPromiseEvaluatorInterface`（它已有 `NotifyFulfilled(string)` 方法）。

`notifyEvaluator` 只需一层类型断言（DeepAgentInterface），通过接口直接调用 `GetCompletionPromiseEvaluator()`，无需断言 `*task_loop.LoopCoordinator`。

所有断言失败路径均有 Warn 日志记录。

使用具体类型 `*task_loop.CompletionPromiseEvaluator`，不引入额外接口。

### 7. deep_agent.go 回填

| # | 行号 | 当前 | 回填为 |
|---|------|------|--------|
| R1 | 77 | `taskCompletionRail agentinterfaces.AgentRail` | 保持接口类型，BuildEvaluators调用处类型断言 |
| R2 | 1265 | 注释掉的 `NewTaskCompletionRail()` | `d.pendingRails = append(d.pendingRails, rails.NewTaskCompletionRail())` |
| R3 | 1976 | `evaluators = []task_loop.StopConditionEvaluator{}` | `evaluators = taskCompRail.BuildEvaluators()` |
| R4 | 1996 | `evaluators = []task_loop.StopConditionEvaluator{}` | `evaluators = taskCompRail.BuildEvaluators()` |
| R5 | 2337 | `reflect.TypeOf(r).String() == "TaskCompletionRail"` | `_, ok := r.(*rails.TaskCompletionRail); return ok` |

## 测试策略

- TaskCompletionRail 结构体创建 + 默认值
- BuildEvaluators 各参数组合
- BeforeModelCall 注入 section
- BeforeTaskIteration 格式化 query + isFollowUp 跳过
- AfterTaskIteration promise 检测 + NotifyFulfilled 调用
- normalize / extractPromiseBlock / promiseMatches 辅助函数
- GetCallbacks 返回正确的回调映射
