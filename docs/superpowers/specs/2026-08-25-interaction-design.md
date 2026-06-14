# 5.7 Interaction 完整实现设计

## 概述

将 Python 项目 `openjiuwen/core/session/interaction/` 的完整 Interaction 体系用 Go 实现，
包括 BaseInteraction、WorkflowInteraction、SimpleAgentInteraction、AgentInteraction、
InteractiveInput、InteractionOutput、AgentInterrupt、GraphInterrupt/Interrupt。

## 对应 Python 源码

```
openjiuwen/core/session/interaction/
├── __init__.py              # 空文件
├── base.py                  # BaseInteraction + AgentInterrupt
├── interaction.py           # WorkflowInteraction, SimpleAgentInteraction, AgentInteraction, InteractionOutput
└── interactive_input.py     # InteractiveInput
```

## Go 包结构

```
session/interaction/
├── doc.go                    # 包文档
├── base.go                   # BaseInteraction + AgentInterrupt + GraphInterrupt/Interrupt
├── interaction.go            # WorkflowInteraction + SimpleAgentInteraction + AgentInteraction + InteractionOutput
├── interactive_input.go      # InteractiveInput
```

### 4 文件与 Python 完全对齐

| Python 文件 | Go 文件 | 职责 |
|------------|---------|------|
| `base.py` | `base.go` | BaseInteraction + AgentInterrupt + GraphInterrupt/Interrupt |
| `interaction.py` | `interaction.go` | 三种 Interaction 实现 + InteractionOutput |
| `interactive_input.py` | `interactive_input.go` | InteractiveInput 用户输入容器 |
| `__init__.py` | `doc.go` | 包文档 |

## 核心类型定义

### baseSession 最小接口（避免循环导入）

采用与 `controller.StateAccessor` 一致的模式：定义最小接口，只含 Interaction 需要的方法，
父包的真实类型通过 Go 隐式接口自动满足。

```go
// baseSession 交互所需的会话最小接口。
// WorkflowInteraction/AgentInteraction 通过此接口委托给内部会话实例，
// 避免 interaction 子包反向导入 session 父包。
// *internal.NodeSession / *internal.AgentSession 天然实现此接口（Go 隐式接口满足）。
type baseSession interface {
    State() state.State
    ExecutableID() string
    StreamWriterManager() any
    Checkpointer() any
}
```

### GraphInterrupt / Interrupt

通过 panic/recover 模拟 Python 的异常传播机制。

```go
// Interrupt 图中断信号
type Interrupt struct {
    // Value 中断携带的值（OutputSchema 类型）
    Value any
    // Resumable 是否可恢复
    Resumable bool
    // NS 命名空间（节点 ID）
    NS string
}

// GraphInterrupt 图级中断异常，通过 panic 传播
// 对应 Python: openjiuwen/core/graph/pregel/base.py (GraphInterrupt)
type GraphInterrupt struct {
    // Interrupts 中断信号列表
    Interrupts []Interrupt
}

// PanicGraphInterrupt 触发图级中断 panic
func PanicGraphInterrupt(interrupts ...Interrupt)
```

注：GraphInterrupt 暂放 interaction 包，8.7 实现 graph/pregel 包时可迁移。

### AgentInterrupt

```go
// AgentInterrupt Agent 中断异常，通过 panic 传播
// 对应 Python: openjiuwen/core/session/interaction/base.py (AgentInterrupt)
type AgentInterrupt struct {
    // Message 中断消息
    Message string
}

// PanicAgentInterrupt 触发 Agent 中断 panic
func PanicAgentInterrupt(msg string)
```

### BaseInteraction

保持与 Python 一致，使用 baseSession 接口（对应 Python 的 BaseSession）。

```go
// BaseInteraction 交互基类，管理交互输入队列
// 对应 Python: openjiuwen/core/session/interaction/base.py (BaseInteraction)
type BaseInteraction struct {
    // interactiveInputs 交互输入队列
    interactiveInputs []any
    // latestInteractiveInput 最近一次交互输入
    latestInteractiveInput any
    // idx 当前输入队列消费索引
    idx int
    // session 基础会话
    session baseSession
}

// NewBaseInteraction 创建交互基类实例
func NewBaseInteraction(session baseSession, defaultInput ...any) *BaseInteraction
```

构造函数逻辑与 Python 一致：
1. 设置 defaultInput 为 interactiveInputs
2. 调用 initInteractiveInputs()：从 session.State() 读取 INTERACTIVE_INPUT，合并到队列，写回 state

### InteractionOutput

```go
// InteractionOutput 交互输出数据
// 对应 Python: openjiuwen/core/session/interaction/interaction.py (InteractionOutput)
type InteractionOutput struct {
    // ID 节点/可执行 ID
    ID string
    // Value 交互携带的值
    Value any
}
```

### InteractiveInput

```go
// InteractiveInput 用户交互输入容器
// 对应 Python: openjiuwen/core/session/interaction/interactive_input.py (InteractiveInput)
type InteractiveInput struct {
    // UserInputs 按节点 ID 绑定的输入映射
    UserInputs map[string]any
    // RawInputs 未绑定节点 ID 的原始输入（首次交互使用）
    RawInputs any
}

// NewInteractiveInput 创建交互输入实例
// rawInputs 为 nil 时返回错误（与 Python 一致：raw_inputs=None 被拒绝）
func NewInteractiveInput(rawInputs ...any) (*InteractiveInput, error)

// Update 添加节点绑定的输入
// RawInputs 已设置时返回错误（互斥约束）
func (i *InteractiveInput) Update(nodeID string, value any) error
```

## 三种 Interaction 实现

### WorkflowInteraction

```go
// WorkflowInteraction 工作流交互，通过 GraphInterrupt 暂停工作流图执行
// 对应 Python: openjiuwen/core/session/interaction/interaction.py (WorkflowInteraction)
type WorkflowInteraction struct {
    *BaseInteraction
    // nodeID 节点 ID（从 session.ExecutableID() 获取）
    nodeID string
}

func NewWorkflowInteraction(session baseSession) *WorkflowInteraction
func (w *WorkflowInteraction) WaitUserInputs(ctx context.Context, value any) (any, error)
func (w *WorkflowInteraction) UserLatestInput(ctx context.Context, value any) (any, error)
```

**WaitUserInputs 流程**（与 Python 逐行对齐）：
1. `res := w.getNextInteractiveInput()` — 从队列取
2. `if res != nil → return res` — 恢复场景，直接返回
3. `commitCMP()` — 提交检查点状态
4. `payload := InteractionOutput{id, value}`
5. `if streamWriterManager != nil → writeOutput(INTERACTION, idx, payload)`
6. `panicGraphInterrupt(Interrupt{Value: OutputSchema{...}})`

**UserLatestInput 流程**：
1. 有缓存输入时直接返回并清空
2. 无缓存时：写流输出 → panic GraphInterrupt(resumable=true, ns=nodeID)

**构造函数特殊逻辑**：
- 读取并清除 workflow_state 中的 INTERACTIVE_INPUT
- 将其作为 defaultInput 传入 BaseInteraction

### SimpleAgentInteraction

```go
// SimpleAgentInteraction 简单 Agent 交互，不管理输入队列
// 对应 Python: openjiuwen/core/session/interaction/interaction.py (SimpleAgentInteraction)
type SimpleAgentInteraction struct {
    // session Agent 内部会话
    session baseSession
}

func NewSimpleAgentInteraction(session baseSession) *SimpleAgentInteraction
func (s *SimpleAgentInteraction) WaitUserInputs(ctx context.Context, message any) error
```

**WaitUserInputs 流程**：
1. `checkpointer.interruptAgentExecute(session)` — 保存检查点
2. `panicAgentInterrupt(message)` — 触发中断

不嵌入 BaseInteraction，与 Python 一致。

### AgentInteraction

```go
// AgentInteraction 完整 Agent 交互，管理输入队列 + checkpointer + stream 输出
// 对应 Python: openjiuwen/core/session/interaction/interaction.py (AgentInteraction)
type AgentInteraction struct {
    *BaseInteraction
    // session Agent 内部会话
    session baseSession
}

func NewAgentInteraction(session baseSession) *AgentInteraction
func (a *AgentInteraction) WaitUserInputs(ctx context.Context, value any) (any, error)
```

**WaitUserInputs 流程**：
1. `inputs := a.getNextInteractiveInput()` — 从队列取
2. `if inputs != nil → return inputs` — 恢复场景
3. `checkpointer.interruptAgentExecute(session)` — 保存检查点
4. `if streamWriterManager != nil → writeOutput(INTERACTION, idx, payload)`
5. `panicAgentInterrupt()` — 触发中断

## 依赖接口（暂放 interaction 包，后续迁移）

### InteractionCheckpointer（5.8 时迁移到 session 包）

```go
// InteractionCheckpointer 交互所需的检查点器接口
// 5.8 实现后，session 包的 Checkpointer 类型天然满足此接口
type InteractionCheckpointer interface {
    InterruptAgentExecute(session any) error
}
```

### InteractionOutputWriter（5.10 时迁移到 session/stream 包）

```go
// InteractionOutputWriterProvider 交互所需的输出写入器提供者接口
// 5.10 实现后，session/stream 包的 StreamWriterManager 类型天然满足此接口
type InteractionOutputWriterProvider interface {
    GetOutputWriter() InteractionOutputWriter
}

// InteractionOutputWriter 交互输出写入器接口
// 5.10 实现后，session/stream 包的 OutputWriter 类型天然满足此接口
type InteractionOutputWriter interface {
    WriteInteraction(outputType string, index int, payload any) error
}
```

### 依赖调用方式

通过 `any` 类型获取后做类型断言，5.8/5.10 实现后自动生效：

```go
// interruptAgentExecute 调用检查点器的中断方法
func interruptAgentExecute(session baseSession) error {
    cp := session.Checkpointer()
    if cp == nil {
        return nil
    }
    if interrupter, ok := cp.(InteractionCheckpointer); ok {
        return interrupter.InterruptAgentExecute(session)
    }
    return nil
}

// writeInteractionOutput 写入交互输出到流
func writeInteractionOutput(session baseSession, outputType string, index int, payload any) error {
    mgr := session.StreamWriterManager()
    if mgr == nil {
        return nil
    }
    if provider, ok := mgr.(InteractionOutputWriterProvider); ok {
        writer := provider.GetOutputWriter()
        return writer.WriteInteraction(outputType, index, payload)
    }
    return nil
}
```

## 常量定义

```go
const (
    // InteractionType 交互事件类型标识
    // 对应 Python: INTERACTION = "__interaction__"
    InteractionType = "__interaction__"
    // InteractiveInputKey 交互输入在 session state 中的键
    // 对应 Python: INTERACTIVE_INPUT = "__interactive_input__"
    InteractiveInputKey = "__interactive_input__"
)
```

## 回填点清单

实现 5.7 后需要回填的位置：

| 回填位置 | 文件 | 当前代码 | 回填为 |
|----------|------|---------|--------|
| 5.5 NodeSessionFacade.interaction | `session/node.go:29` | `interaction any` + `⤵️ 5.7 回填` | `interaction *interaction.WorkflowInteraction` |
| 5.5 NodeSessionFacade.Interact() | `session/node.go:171-178` | 桩实现返回 `(nil, nil)` | 懒初始化 WorkflowInteraction + 调用 WaitUserInputs |
| 5.9 Session.interaction | `session/agent.go:35` | `interaction any` + `⤵️ 5.9 回填` | `interaction *interaction.SimpleAgentInteraction` |
| 5.9 Session.Interact() | `session/agent.go:258-260` | 桩实现返回 `nil` | 懒初始化 SimpleAgentInteraction + 调用 WaitUserInputs |

### NodeSessionFacade.Interact 回填后

```go
func (f *NodeSessionFacade) Interact(ctx context.Context, value any) (any, error) {
    if f.streamMode {
        return nil, fmt.Errorf("interact when streaming process(transform or collect) is not supported, comp_id=%s, workflow=%s",
            f.GetComponentID(), f.GetWorkflowID())
    }
    if f.interaction == nil {
        f.interaction = interaction.NewWorkflowInteraction(f.inner)
    }
    return f.interaction.WaitUserInputs(ctx, value)
}
```

### Session.Interact 回填后

```go
func (s *Session) Interact(value any) error {
    if s.interaction == nil {
        s.interaction = interaction.NewSimpleAgentInteraction(s.inner)
    }
    return s.interaction.WaitUserInputs(context.Background(), value)
}
```

## 错误处理

| 场景 | 异常码 | 触发位置 |
|------|--------|---------|
| 流式模式下调用 Interact | `StatusCompSessionInteractError`(111005) | `NodeSessionFacade.Interact()` |
| InteractiveInput.RawInputs 为 nil | `StatusInteractionInputInvalid`(111110) | `NewInteractiveInput()` |
| InteractiveInput.Update 时 RawInputs 已存在 | `StatusInteractionInputInvalid`(111110) | `InteractiveInput.Update()` |
| InteractiveInput.Update 时 nodeID 或 value 为 nil | `StatusInteractionInputInvalid`(111110) | `InteractiveInput.Update()` |

## 日志补充

Python 侧 Interaction 代码无直接 logger 调用，按项目规则3补充防御性日志：

| 日志位置 | 级别 | 内容 |
|---------|------|------|
| WaitUserInputs 中断时 | Info | 交互中断，含 node_id、index |
| WaitUserInputs 恢复时 | Info | 交互恢复（从队列获取到输入） |
| interruptAgentExecute 失败时 | Error | 检查点保存失败，含 event_type=LLM_CALL_ERROR |
| writeInteractionOutput 失败时 | Warn | 流输出写入失败 |

## 测试策略

### 可 mock 的测试（不走 build tag）

| 测试文件 | 测试内容 |
|---------|---------|
| `interaction/base_test.go` | BaseInteraction 输入队列管理：初始化、消费、索引递增 |
| `interaction/interactive_input_test.go` | InteractiveInput：构造、RawInputs nil 拒绝、Update 互斥约束、参数校验 |
| `interaction/interaction_test.go` | WorkflowInteraction：队列有输入时直接返回；队列空时 panic GraphInterrupt；UserLatestInput 缓存命中/未命中 |
| `interaction/interaction_test.go` | SimpleAgentInteraction：panic AgentInterrupt |
| `interaction/interaction_test.go` | AgentInteraction：队列有输入时直接返回；队列空时 panic AgentInterrupt |

### mock 方式

- `baseSession` 接口：创建 `fakeBaseSession` 结构体
- `InteractionCheckpointer`：创建 `fakeCheckpointer`
- `InteractionOutputWriterProvider`：创建 `fakeOutputWriterProvider`

### panic 测试模式

```go
func TestWorkflowInteraction_WaitUserInputs_队列空时触发GraphInterrupt(t *testing.T) {
    wi := NewWorkflowInteraction(fakeSession)
    defer func() {
        r := recover()
        if r == nil {
            t.Fatal("期望 panic GraphInterrupt，但未发生")
        }
        gi, ok := r.(*GraphInterrupt)
        if !ok {
            t.Fatalf("期望 *GraphInterrupt，得到 %T", r)
        }
        // 验证 Interrupt 内容
    }()
    wi.WaitUserInputs(context.Background(), "test_value")
}
```

### 回填后测试更新

- `session/node_test.go`：更新 `TestNodeSessionFacade_Interact_非流式模式` 验证真实 WorkflowInteraction 行为
- `session/agent_test.go`：更新 `TestSession_桩方法返回Nil` 验证真实 SimpleAgentInteraction 行为
