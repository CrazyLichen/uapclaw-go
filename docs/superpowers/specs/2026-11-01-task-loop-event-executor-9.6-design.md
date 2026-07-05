# 9.6 TaskLoopEventExecutor + TaskLoopEventHandler 实现设计

## 流程位置与作用

### 在 DeepAgent 执行流程中的位置

```
用户输入
  ↓
DeepAgent._run_task_loop (外层循环)
  ↓
TaskLoopController.SubmitRound()        ← 9.4 已实现
  ↓
TaskLoopEventHandler.HandleInput()      ← 9.6 (创建 core Task)
  ↓
TaskScheduler 调度                       ← 6.x 已实现
  ↓
TaskLoopEventExecutor.ExecuteAbility()  ← 9.6 (核心！调用内层 ReActAgent)
  ↓
ReActAgent.invoke()                     ← 7.x 已实现
  ↓
结果通过 EventQueue 回传
  ↓
TaskLoopEventHandler.HandleTaskCompletion()  ← 9.6 (resolve Future)
  ↓
TaskLoopController.WaitRoundCompletion()     ← 9.4 已实现
  ↓
LoopCoordinator 评估是否继续               ← 9.5 已实现
  ↓
下一轮 or 退出
```

### TaskLoopEventExecutor 作用

DeepAgent 外层循环与内层 ReActAgent 之间的**桥梁**：

- 将 ReActAgent 的执行**包装为 `TaskExecutor` 接口**，使 TaskScheduler 可以调度它
- 在执行前后**触发 Rails 生命周期钩子**（BEFORE/AFTER_TASK_ITERATION）
- 将 **steering queue** 注入到内层 ReAct 循环，实现外部干预能力
- 在 **TaskPlan** 中标记任务状态（in_progress → completed / cancelled）

### TaskLoopEventHandler 作用

外层循环的**事件路由器**：

- 将 EventQueue 事件**转换为 TaskScheduler 的 Task 提交**
- 使用 **per-round channel + round_id 模式** 实现同步等待异步完成
- 路由 **steer / follow_up / completion / failed / abort** 事件
- 实现 `interactionQueuesProvider` 接口（9.4 中 controller.go 已预留）

## Python 对应源码

- `openjiuwen/harness/task_loop/task_loop_event_executor.py` (382 行)
- `openjiuwen/harness/task_loop/task_loop_event_handler.py` (650 行)

## 实现方案

方案 A：Executor + Handler 一体化实现

- 在 `task_loop/` 下新建 `executor.go` + `handler.go` 两个文件
- `executor.go`：DeepAgentProvider 接口（⤵️ 9.1 回填）→ TaskLoopEventExecutor → BuildDeepExecutor
- `handler.go`：TaskLoopEventHandler（嵌入 EventHandlerBase）→ 实现 EventHandler + interactionQueuesProvider → per-round channel + round_id 模式
- SessionSpawn 用 `any` + 注释标注 ⤵️ 9.7 回填
- 更新 doc.go 文件目录
- 补充单元测试

## Part 1：DeepAgentProvider 接口 + 常量

**文件：** `executor.go`

```go
// DeepAgentProvider DeepAgent 临时解耦接口，提供 Executor/Handler 所需的运行时访问。
// ⤵️ 9.1 回填：9.1 实现 DeepAgent 后，直接用 *DeepAgent 替换此接口，
// 并删除 DeepAgentProvider 定义。此接口不是真实实现，仅用于解耦编译依赖。
type DeepAgentProvider interface {
    // ReactAgent 返回内层 ReActAgent 实例
    ReactAgent() *agents.ReActAgent
    // LoopCoordinator 返回循环协调器
    LoopCoordinator() *LoopCoordinator
    // EventHandler 返回事件处理器
    EventHandler() modules.EventHandler
    // LoadState 从会话加载 DeepAgentState
    LoadState(sess sessioninterfaces.SessionFacade) *schema.DeepAgentState
    // DeepConfig 返回 DeepAgent 配置
    DeepConfig() *schema.DeepAgentConfig
    // IsInvokeActive 判断是否有活跃的 invoke
    IsInvokeActive() bool
    // IsAutoInvokeScheduled 判断是否已调度自动 invoke
    IsAutoInvokeScheduled() bool
    // SetAutoInvokeScheduled 设置自动 invoke 调度标记
    SetAutoInvokeScheduled(scheduled bool)
    // ScheduleAutoInvokeOnSpawnDone 延迟调度自动 invoke
    // ⤵️ 9.7 回填
    ScheduleAutoInvokeOnSpawnDone(steerText string) error
}

// DeepTaskType DeepAgent 任务类型标识
// 对齐 Python: DEEP_TASK_TYPE = "deep_agent_task"
const DeepTaskType = "deep_agent_task"
```

## Part 2：TaskLoopEventExecutor

**文件：** `executor.go`

### 结构体

```go
// TaskLoopEventExecutor TaskExecutor 的 DeepAgent 专用实现，
// 将内层 ReActAgent 的执行包装为 TaskExecutor 接口，使 TaskScheduler 可以调度。
// 对齐 Python: TaskLoopEventExecutor(TaskExecutor)
type TaskLoopEventExecutor struct {
    deps    modules.TaskExecutorDependencies
    provider DeepAgentProvider  // ⤵️ 9.1 回填：替换为 *DeepAgent 直接引用
}
```

### ExecuteAbility 核心流程（17 步，对齐 Python 第 68-265 行）

1. 获取 `agent = e.provider`
2. `agent.ReactAgent()` 为 nil → Warn 日志 + 返回空 channel
3. 从 TaskManager 获取 core_task（MakeFilter(taskID)）
4. 提取 query（从 description）+ raw_input（从 InputEvent 提取 InteractiveInput）
5. 获取 `state = agent.LoadState(sess)`
6. 获取 `plan_task = GetPlanTask(state, taskID)`，若存在则覆盖 query
7. 读取 is_follow_up（从 task.metadata）
8. 计算 `iteration = coordinator.CurrentIteration + 1`
9. 日志记录（IS_SENSITIVE 双模式）
10. 构建 TaskIterationInputs + AgentCallbackContext
11. plan_task 存在 → `state.TaskPlan.MarkInProgress(taskID)`
12. `ctx.FireLifecycle(CallbackBeforeTaskIteration)` — Rails 可修改 query
13. `effective_query = raw_input || iterInputs.Query || query`
14. 构建 effective map（query + conversation_id + run_kind + run_context）
15. 注入 steering_queue（从 handler.interaction_queues）
16. try: reactAgent.Invoke → MarkCompleted → FireLifecycle(AFTER_TASK_ITERATION) → 构建 TASK_COMPLETION payload → 发送到 channel
17. except: MarkCancelled → 构建 TASK_FAILED payload → 发送到 channel

### 其余 TaskExecutor 接口方法

| 方法 | 实现 |
|------|------|
| `CanPause()` | 返回 `false, "pause not supported"` |
| `Pause()` | 返回 `false` |
| `CanCancel()` | 返回 `true, ""` |
| `Cancel()` | MarkCancelled + coordinator.RequestAbort() |

### 辅助方法

| 方法 | 实现 |
|------|------|
| `getState(sess)` | `e.provider.LoadState(sess)` |
| `getPlanTask(state, taskID)` | `state.TaskPlan.GetTask(taskID)`，nil 安全 |
| `MakeFilter(taskID)` | 构建 TaskFilter（静态方法） |
| `ExtractInteractiveInput(event)` | 从 InputEvent 的 input_data 提取 InteractiveInput（静态方法） |
| `isSensitive()` | 读 `IS_SENSITIVE` 环境变量，对齐 base_client.go |

### BuildDeepExecutor 工厂

```go
// BuildDeepExecutor 创建 TaskLoopEventExecutor 构建闭包，用于注册到 TaskExecutorRegistry。
// 对齐 Python: build_deep_executor(deep_agent)
func BuildDeepExecutor(provider DeepAgentProvider) func(modules.TaskExecutorDependencies) modules.TaskExecutor {
    return func(deps modules.TaskExecutorDependencies) modules.TaskExecutor {
        return &TaskLoopEventExecutor{deps: deps, provider: provider}
    }
}
```

## Part 3：TaskLoopEventHandler

**文件：** `handler.go`

### 结构体

```go
// TaskLoopEventHandler 事件处理器，驱动外层任务循环。
// 使用 per-round channel 模式：每轮迭代创建新 resultCh + 递增 round_id，
// completion/failed/abort 事件通过 resolveRound 写入 channel。
// 对齐 Python: TaskLoopEventHandler(EventHandler)
type TaskLoopEventHandler struct {
    base             modules.EventHandlerBase
    provider         DeepAgentProvider        // ⤵️ 9.1 回填：替换为 *DeepAgent 直接引用
    lastResult       map[string]any
    currentCh        chan map[string]any
    roundID          int
    interactionQueues *LoopQueues
    sessionToolkit   any                     // ⤵️ 9.7 回填：替换为具体 SessionToolkit 类型
}
```

### per-round channel 核心机制

| 方法 | 实现 |
|------|------|
| `PrepareRound()` | 若 currentCh 未关闭则关闭旧 channel；roundID++；创建新 `currentCh = make(chan map[string]any, 1)`；返回 roundID |
| `WaitCompletion(ctx, timeout)` | select 等待 currentCh 或 ctx.Done 或 timeout；nil channel 返回 error；超时返回 `{"error": "completion_timeout"}` |
| `resolveRound(result, roundID)` | roundID 不匹配 → Warn 日志丢弃；匹配 → 非阻塞写入 currentCh |

### EventHandler 接口 5 个事件处理方法

| 方法 | 核心逻辑 |
|------|---------|
| `HandleInput(ctx, input)` | 从 event 提取 query/metadata → 解析 task_id（从 TaskPlan 或 UUID 兜底）→ 构建 CoreTask（DeepTaskType）→ TaskManager.AddTask → 返回 `{"status": "submitted", "task_id": ...}`；异常路径 resolveRound error |
| `HandleTaskInteraction(ctx, input)` | 从 TaskInteractionEvent 提取 steer 消息 → `interactionQueues.PushSteer(msg)` → 返回 `{"status": "steer_injected"}` |
| `HandleTaskCompletion(ctx, input)` | 判断 task_type == SESSION_SPAWN → ⤵️ 9.7 回填；否则从 task_result 提取 result dict → `resolveRound(result, roundID)` |
| `HandleTaskFailed(ctx, input)` | 判断 task_type == SESSION_SPAWN → ⤵️ 9.7 回填；否则 `resolveRound({"error": msg}, roundID)` |
| `HandleFollowUp(ctx, input)` | 从 FollowUpEvent 提取文本 → `interactionQueues.PushFollowUp(msg)` → 返回 `{"status": "follow_up_queued"}` |

### interactionQueuesProvider 接口实现

```go
// InteractionQueues 返回交互队列，实现 interactionQueuesProvider 接口。
func (h *TaskLoopEventHandler) InteractionQueues() *LoopQueues {
    return h.interactionQueues
}
```

### OnAbort

```go
func (h *TaskLoopEventHandler) OnAbort() {
    h.resolveRound(map[string]any{"error": "aborted"}, h.roundID)
}
```

### SetInteractionQueues / SetSessionToolkit setter

### 辅助静态方法

| 方法 | 实现 |
|------|------|
| `extractQuery(event)` | 从 InputEvent 的 input_data 提取 text/query |
| `extractResultFromEvent(input)` | 从 completion event 的 task_result 提取 output |
| `extractErrorFromEvent(input)` | 从 failure event 提取 error_message |
| `formatSessionSpawnSteer(...)` | ⤵️ 9.7 回填：中英文模板格式化 |

## Part 4：文件组织 + 回填标记 + 测试策略

### 文件组织

```
task_loop/
├── doc.go                 # 包文档（更新：添加 executor.go + handler.go 条目）
├── controller.go          # 9.4 ✅ TaskLoopController + interactionQueuesProvider 接口
├── loop_coordinator.go    # 9.5 ✅ LoopCoordinator
├── loop_queues.go         # 9.4 ✅ LoopQueues
├── stop_condition.go      # 9.5 ✅ StopConditionEvaluator
├── executor.go            # 9.6 🆕 TaskLoopEventExecutor + DeepAgentProvider + BuildDeepExecutor
├── handler.go             # 9.6 🆕 TaskLoopEventHandler
├── executor_test.go       # 9.6 🆕 Executor 单元测试
└── handler_test.go        # 9.6 🆕 Handler 单元测试
```

### 回填标记汇总

| 标记 | 位置 | 说明 | 回填方 |
|------|------|------|--------|
| `⤵️ 9.1 回填` | `executor.go` DeepAgentProvider 接口定义 | 临时解耦接口，9.1 实现 DeepAgent 后替换为 `*DeepAgent` 直接引用并删除此接口 | 9.1 |
| `⤵️ 9.1 回填` | `handler.go` provider 字段注释 | 同上 | 9.1 |
| `⤵️ 9.7 回填` | `handler.go` sessionToolkit 字段 | `any` 类型占位，9.7 实现后替换为具体 `SessionToolkit` 类型 | 9.7 |
| `⤵️ 9.7 回填` | `handler.go` HandleTaskCompletion/HandleTaskFailed 中 SESSION_SPAWN 分支 | 空实现 + 注释标注，9.7 补全 `_completeSessionSpawn` 逻辑 | 9.7 |
| `⤵️ 9.7 回填` | `handler.go` formatSessionSpawnSteer | 空实现 + 注释标注，9.7 补全中英文模板 | 9.7 |
| `⤵️ 9.7 回填` | `executor.go` DeepAgentProvider.ScheduleAutoInvokeOnSpawnDone | 接口方法占位，9.7 补全实现 | 9.7 |

### 回填来源标记（9.6 实现后）

| 标记 | 位置 | 说明 |
|------|------|------|
| `⤴️ 9.6 回填` | `controller.go` interactionQueuesProvider 接口注释 | 9.6 的 TaskLoopEventHandler 已实现此接口，更新注释说明 |
| `⤴️ 9.6 回填` | `IMPLEMENTATION_PLAN.md` 9.6 行 | 状态 ☐ → ✅ |

### 测试策略

**executor_test.go（12 个用例）：**

- `TestNewTaskLoopEventExecutor` — 构造函数
- `TestTaskLoopEventExecutor_CanPause返回不支持` — CanPause 返回 false
- `TestTaskLoopEventExecutor_CanCancel始终允许` — CanCancel 返回 true
- `TestTaskLoopEventExecutor_Pause返回不支持` — Pause 返回 false
- `TestTaskLoopEventExecutor_Cancel标记取消并请求中止` — Cancel 调用 MarkCancelled + RequestAbort
- `TestTaskLoopEventExecutor_ExecuteAbility_ReactAgent为Nil时返回空` — nil agent 路径
- `TestTaskLoopEventExecutor_ExecuteAbility_正常执行` — 完整流程（mock DeepAgentProvider + ReActAgent）
- `TestTaskLoopEventExecutor_ExecuteAbility_执行失败` — 异常路径
- `TestBuildDeepExecutor` — 工厂函数
- `TestMakeFilter` — TaskFilter 构建
- `TestExtractInteractiveInput` — InteractiveInput 提取
- `TestIsSensitive` — IS_SENSITIVE 环境变量读取

**handler_test.go（16 个用例）：**

- `TestNewTaskLoopEventHandler` — 构造函数
- `TestTaskLoopEventHandler_PrepareRound递增` — roundID 递增 + channel 重建
- `TestTaskLoopEventHandler_WaitCompletion_正常完成` — channel 收到结果
- `TestTaskLoopEventHandler_WaitCompletion_超时` — timeout 路径
- `TestTaskLoopEventHandler_WaitCompletion_无活跃轮次` — nil channel 路径
- `TestTaskLoopEventHandler_ResolveRound_匹配RoundID` — 正常写入
- `TestTaskLoopEventHandler_ResolveRound_过期RoundID丢弃` — roundID 不匹配
- `TestTaskLoopEventHandler_HandleInput_正常提交` — 创建 Task 并 AddTask
- `TestTaskLoopEventHandler_HandleInput_无LoopCoordinator时失败` — coordinator nil 路径
- `TestTaskLoopEventHandler_HandleTaskInteraction_注入Steer` — push_steer
- `TestTaskLoopEventHandler_HandleTaskCompletion_正常完成` — resolveRound
- `TestTaskLoopEventHandler_HandleTaskFailed_正常失败` — resolveRound with error
- `TestTaskLoopEventHandler_HandleFollowUp_入队` — push_follow_up
- `TestTaskLoopEventHandler_OnAbort` — resolveRound with aborted
- `TestTaskLoopEventHandler_InteractionQueues` — 实现 interactionQueuesProvider
- `TestExtractQuery` — InputEvent 文本提取

**mock 方式：** 使用 fake struct 实现 DeepAgentProvider 接口，fake ReActAgent 用 mock invoke 返回预设结果。

## 日志对齐

- IS_SENSITIVE 环境变量复用 base_client.go 已有模式，不回填 2.16
- 所有日志使用 `logger.Info/Warn/Error(logComponent)` 组件级调用
- Python `[OuterLoop]` 前缀 → Go 日志 `Msg()` 中包含 `外层循环` 关键词
- 敏感模式隐藏 query/output，非敏感模式显示（截断到 120/200 字符）
