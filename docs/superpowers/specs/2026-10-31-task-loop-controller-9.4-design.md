# 9.4 TaskLoopController + LoopQueues + ControllerInterface 设计

## 概述

实现步骤 9.4 TaskLoopController（任务循环控制器），同时实现其直接依赖 LoopQueues（双队列缓冲）以及在 controller 包内新增 ControllerInterface 接口以支持多态调用。

对应 Python 代码：`openjiuwen/harness/task_loop/task_loop_controller.py` + `openjiuwen/harness/task_loop/loop_queues.py`

## 在 Agent 会话中的流程位置

```
用户请求 → DeepAgent.invoke()
             │
             ├→ TaskLoopController.SubmitRound()     ← 【9.4 在此处】
             │     ├ EventHandler.PrepareRound()     创建轮次 Future
             │     ├ 构建 InputEvent + 注入元数据
             │     └ Controller.PublishEventAsync()  发布事件
             │
             ├→ TaskLoopEventHandler.HandleInput()   → TaskScheduler → TaskLoopEventExecutor
             │     └ ReActAgent.invoke()             ← 内层 ReAct 循环
             │
             ├→ TaskLoopController.WaitRoundCompletion()  等待本轮完成
             │
             ├→ TaskLoopController.DrainFollowUp()   排空 follow-up 消息
             │
             └→ LoopCoordinator.ShouldContinue()     评估是否继续（9.5✅ 已完成）
                   └ 继续 or 结束
```

**作用**：TaskLoopController 是 DeepAgent 外层循环的"方向盘"——驱动循环前进，而 LoopCoordinator（9.5✅）是"刹车"——决定何时停止。两者协作完成多轮任务循环。

## 依赖关系

| 组件 | Python 路径 | Go 状态 |
|------|------------|---------|
| `Controller` 基类 | `core/controller/base.py` | ✅ 已实现 |
| `EventHandler` 接口 | `core/controller/modules/event_handler.py` | ✅ 已实现 |
| `InputEvent` | `core/controller/schema/event.py` | ✅ 已实现 |
| `Session` | `core/session/agent.py` | ✅ 已实现 |
| `LoopCoordinator` | `harness/task_loop/loop_coordinator.py` | ✅ 已实现（9.5） |
| `LoopQueues` | `harness/task_loop/loop_queues.py` | ❌ 未实现（本次实现） |
| `TaskLoopEventHandler` | `harness/task_loop/task_loop_event_handler.py` | ❌ 未实现（9.6，延后） |

## 设计决策

### 1. 继承方式：嵌入 *Controller

Python 中 `TaskLoopController(Controller)` 是继承关系。Go 中使用嵌入 `*controller.Controller` 实现组合复用，直接复用 `PublishEventAsync` 等基类方法，同时新增轮次管理扩展方法。

TaskLoopController 并未覆写 Controller 的任何方法，只新增了 6 个方法。

### 2. ControllerInterface 在 controller 包内定义

在 `internal/agentcore/controller/interface.go` 新增 `ControllerInterface` 接口，包含 Controller 的核心公开方法。`Controller` 和 `TaskLoopController` 均满足此接口。

现有 `Controller.AddTaskExecutor` 返回类型从 `*Controller` 改为 `ControllerInterface`，支持链式调用时的接口级联。

### 3. ControllerInterface 方法列表

```go
type ControllerInterface interface {
    Init(card, cfg, abilityMgr, contextEngine)
    Start(ctx) error
    Stop(ctx) error
    Invoke(ctx, inputs, sess) (*ControllerOutput, error)
    Stream(ctx, inputs, sess, streamModes) (<-chan *OutputSchema, <-chan error)
    PublishEventAsync(ctx, sess, event) error
    SetEventHandler(handler)
    AddTaskExecutor(taskType, builder) ControllerInterface
    BindSession(ctx, sess) error
    UnbindSession(ctx, sess) error
    Config() *ControllerConfig
    EventHandler() EventHandler
}
```

- `Init` 保留在接口中（4 个参数，对齐 Python `Controller.init`）
- `AddTaskExecutor` 返回 `ControllerInterface` 支持链式调用
- 不包含 setter 方法（SetConfig、SetContextEngine 等）

### 4. LoopQueues：2 个带缓冲 channel + 非阻塞操作

Python 使用 2 个 `asyncio.Queue`（steering + follow_up），Go 对齐为 2 个带缓冲 `chan string`。

所有操作非阻塞，对齐 Python 的 `put_nowait`/`get_nowait` 语义：
- `PushSteer` / `PushFollowUp`：`select + default` 非阻塞写入，满时丢弃并记录日志
- `DrainSteering` / `DrainFollowUp`：`for-select` 非阻塞一次性排空
- `HasFollowUp`：`len(ch) > 0` 非阻塞检查

默认缓冲区大小 64。

### 5. getInteractionQueues：类型断言（对齐 Python getattr）

Python 中 `interaction_queues` 是 `TaskLoopEventHandler` 独有属性，不是 EventHandler 基类属性。通过 `getattr(handler, "interaction_queues", None)` 鸭子类型获取。

Go 中使用类型断言对齐此语义：

```go
type interactionQueuesProvider interface {
    InteractionQueues() *LoopQueues
}

func (tc *TaskLoopController) getInteractionQueues() *LoopQueues {
    handler := tc.EventHandler()
    if handler == nil {
        return nil
    }
    provider, ok := handler.(interactionQueuesProvider)
    if !ok {
        return nil
    }
    return provider.InteractionQueues()
}
```

不改 EventHandler 接口。未来 TaskLoopEventHandler（9.6）实现 `InteractionQueues()` 方法即可自动满足 `interactionQueuesProvider`。

### 6. 不提前实现其他章节

9.1 DeepAgent、9.3 DeepAgent Factory、9.6 TaskLoopEventExecutor、9.7 SessionSpawnExecutor 均延后。9.4 独立实现，可独立测试。

## 文件变更清单

| 文件 | 操作 | 说明 |
|------|------|------|
| `controller/interface.go` | **新增** | ControllerInterface 接口定义 + 编译时断言 |
| `controller/controller.go` | **修改** | AddTaskExecutor 返回类型改为 ControllerInterface；新增编译时断言 |
| `harness/task_loop/loop_queues.go` | **新增** | LoopQueues 双队列缓冲 |
| `harness/task_loop/loop_queues_test.go` | **新增** | LoopQueues 单元测试 |
| `harness/task_loop/controller.go` | **新增** | TaskLoopController |
| `harness/task_loop/controller_test.go` | **新增** | TaskLoopController 单元测试 |
| `harness/task_loop/doc.go` | **修改** | 更新文件目录，新增 loop_queues.go 和 controller.go |

## 回填检查

- LoopQueues：在 MVP 参考文档中明确归属于 9.4，与 TaskLoopController 同步实现 ✅
- ControllerInterface：新增 Go 设计（Python 无对应），不涉及回填
- AddTaskExecutor 返回类型变更：影响现有 Controller 调用方，需检查兼容性
- 9.4 实现计划中无 ⤵️/⤴️ 回填标记
