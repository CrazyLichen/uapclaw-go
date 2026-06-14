# 5.7 Interaction 完整实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现完整的 Interaction 体系（BaseInteraction + WorkflowInteraction + SimpleAgentInteraction + AgentInteraction + InteractiveInput + InteractionOutput + GraphInterrupt/Interrupt/AgentInterrupt），并回填 NodeSessionFacade.Interact() 和 Session.Interact()。

**Architecture:** 在 `session/interaction/` 子包中实现全部 Interaction 类型，通过最小 `baseSession` 接口避免循环导入（与 controller.StateAccessor 模式一致），GraphInterrupt/AgentInterrupt 通过 panic/recover 传播，Checkpointer/StreamWriter 依赖通过类型断言延迟绑定（5.8/5.10 实现后自动生效）。

**Tech Stack:** Go 1.22+, 项目异常体系 `exception.BaseError`, 日志体系 `logger.ComponentAgentCore`

**设计文档:** `docs/superpowers/specs/2026-08-25-interaction-design.md`

---

## 文件结构

| 操作 | 文件路径 | 职责 |
|------|---------|------|
| 创建 | `internal/agentcore/session/interaction/doc.go` | 包文档 |
| 创建 | `internal/agentcore/session/interaction/base.go` | baseSession 接口 + BaseInteraction + GraphInterrupt/Interrupt + AgentInterrupt + 常量 + 依赖接口 + 辅助函数 |
| 创建 | `internal/agentcore/session/interaction/interaction.go` | WorkflowInteraction + SimpleAgentInteraction + AgentInteraction + InteractionOutput |
| 创建 | `internal/agentcore/session/interaction/interactive_input.go` | InteractiveInput |
| 创建 | `internal/agentcore/session/interaction/base_test.go` | baseSession fake + BaseInteraction 测试 |
| 创建 | `internal/agentcore/session/interaction/interaction_test.go` | 三种 Interaction + InteractionOutput 测试 |
| 创建 | `internal/agentcore/session/interaction/interactive_input_test.go` | InteractiveInput 测试 |
| 修改 | `internal/agentcore/session/node.go` | 回填 5.5: interaction 字段类型 + Interact() 实现 |
| 修改 | `internal/agentcore/session/agent.go` | 回填 5.9: interaction 字段类型 + Interact() 实现 |
| 修改 | `internal/agentcore/session/node_test.go` | 更新 Interact 测试 |
| 修改 | `internal/agentcore/session/agent_test.go` | 更新 Interact 测试 |
| 修改 | `internal/agentcore/session/doc.go` | 文件目录增加 interaction 子包 |

---

### Task 1: 创建 interaction/doc.go

**Files:**
- Create: `internal/agentcore/session/interaction/doc.go`

- [ ] **Step 1: 创建 doc.go 文件**

```go
// Package interaction 提供会话交互管理，支持工作流和 Agent 的用户输入中断-恢复机制。
//
// 本包实现三种交互模式：
//   - WorkflowInteraction：工作流节点交互，通过 GraphInterrupt 暂停图执行
//   - SimpleAgentInteraction：简单 Agent 交互，无输入队列管理
//   - AgentInteraction：完整 Agent 交互，含输入队列 + 检查点 + 流输出
//
// 中断-恢复机制：
//   - WorkflowInteraction 通过 panic(GraphInterrupt) 暂停工作流图执行
//   - Agent 侧通过 panic(AgentInterrupt) 暂停 Agent 执行
//   - 恢复时用户输入通过 InteractiveInput 注入到 session state，
//     交互实例从输入队列自动消费，无需再次中断
//
// 依赖接口（暂放本包，后续迁移）：
//   - InteractionCheckpointer → 5.8 时迁移到 session 包
//   - InteractionOutputWriter / InteractionOutputWriterProvider → 5.10 时迁移到 session/stream 包
//
// 文件目录：
//
//	interaction/
//	├── doc.go                # 包文档
//	├── base.go               # baseSession 接口 + BaseInteraction + GraphInterrupt/Interrupt + AgentInterrupt + 常量 + 依赖接口
//	├── interaction.go        # WorkflowInteraction + SimpleAgentInteraction + AgentInteraction + InteractionOutput
//	└── interactive_input.go  # InteractiveInput 用户输入容器
//
// 对应 Python 代码：openjiuwen/core/session/interaction/
package interaction
```

- [ ] **Step 2: 提交**

```bash
git add internal/agentcore/session/interaction/doc.go
git commit -m "feat(interaction): 添加 interaction 包文档"
```

---

### Task 2: 创建 interaction/base.go — 核心类型与接口

**Files:**
- Create: `internal/agentcore/session/interaction/base.go`

- [ ] **Step 1: 创建 base.go**

```go
package interaction

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 接口 ────────────────────────────

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

// InteractionCheckpointer 交互所需的检查点器接口。
// 5.8 实现后，session 包的 Checkpointer 类型天然满足此接口，届时迁移到 session 包。
type InteractionCheckpointer interface {
	// InterruptAgentExecute 中断 Agent 执行并保存检查点
	InterruptAgentExecute(session any) error
}

// InteractionOutputWriterProvider 交互所需的输出写入器提供者接口。
// 5.10 实现后，session/stream 包的 StreamWriterManager 类型天然满足此接口，届时迁移到 session/stream 包。
type InteractionOutputWriterProvider interface {
	GetOutputWriter() InteractionOutputWriter
}

// InteractionOutputWriter 交互输出写入器接口。
// 5.10 实现后，session/stream 包的 OutputWriter 类型天然满足此接口，届时迁移到 session/stream 包。
type InteractionOutputWriter interface {
	WriteInteraction(outputType string, index int, payload any) error
}

// ──────────────────────────── 结构体 ────────────────────────────

// Interrupt 图中断信号
type Interrupt struct {
	// Value 中断携带的值（OutputSchema 类型）
	Value any
	// Resumable 是否可恢复
	Resumable bool
	// NS 命名空间（节点 ID）
	NS string
}

// GraphInterrupt 图级中断异常，通过 panic 传播。
// 对应 Python: openjiuwen/core/graph/pregel/base.py (GraphInterrupt)
// 暂放 interaction 包，8.7 实现 graph/pregel 包时可迁移。
type GraphInterrupt struct {
	// Interrupts 中断信号列表
	Interrupts []Interrupt
}

// AgentInterrupt Agent 中断异常，通过 panic 传播。
// 对应 Python: openjiuwen/core/session/interaction/base.py (AgentInterrupt)
type AgentInterrupt struct {
	// Message 中断消息
	Message string
}

// BaseInteraction 交互基类，管理交互输入队列。
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

// ──────────────────────────── 常量 ────────────────────────────

const (
	// InteractionType 交互事件类型标识
	// 对应 Python: INTERACTION = "__interaction__"
	InteractionType = "__interaction__"
	// InteractiveInputKey 交互输入在 session state 中的键
	// 对应 Python: INTERACTIVE_INPUT = "__interactive_input__"
	InteractiveInputKey = "__interactive_input__"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewBaseInteraction 创建交互基类实例。
// defaultInput 为可选的默认输入，会被追加到从 session state 读取的输入队列之后。
// 对应 Python: BaseInteraction.__init__(session, default_input)
func NewBaseInteraction(session baseSession, defaultInput ...any) *BaseInteraction {
	bi := &BaseInteraction{
		session: session,
	}
	if len(defaultInput) > 0 && defaultInput[0] != nil {
		bi.interactiveInputs = []any{defaultInput[0]}
	}
	bi.initInteractiveInputs()
	return bi
}

// PanicGraphInterrupt 触发图级中断 panic。
// 对应 Python: raise GraphInterrupt(...)
func PanicGraphInterrupt(interrupts ...Interrupt) {
	panic(&GraphInterrupt{Interrupts: interrupts})
}

// PanicAgentInterrupt 触发 Agent 中断 panic。
// 对应 Python: raise AgentInterrupt(message)
func PanicAgentInterrupt(msg string) {
	panic(&AgentInterrupt{Message: msg})
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// initInteractiveInputs 从 session state 读取已有的交互输入，合并到输入队列。
// 对应 Python: BaseInteraction._init_interactive_inputs()
func (b *BaseInteraction) initInteractiveInputs() {
	st := b.session.State()
	if st == nil {
		return
	}
	existing := st.Get(state.StringKey(InteractiveInputKey))
	if existing == nil {
		// 无已有输入，仅更新 session state
		if len(b.interactiveInputs) > 0 {
			st.Update(map[string]any{InteractiveInputKey: b.interactiveInputs})
		}
		if len(b.interactiveInputs) > 0 {
			b.latestInteractiveInput = b.interactiveInputs[len(b.interactiveInputs)-1]
		}
		return
	}
	// 已有输入列表
	if inputList, ok := existing.([]any); ok {
		if len(b.interactiveInputs) > 0 {
			// session state 输入在前，defaultInput 在后
			b.interactiveInputs = append(inputList, b.interactiveInputs...)
		} else {
			b.interactiveInputs = inputList
		}
	}
	// 写回 session state
	if len(b.interactiveInputs) > 0 {
		st.Update(map[string]any{InteractiveInputKey: b.interactiveInputs})
		b.latestInteractiveInput = b.interactiveInputs[len(b.interactiveInputs)-1]
	}
}

// getNextInteractiveInput 从输入队列获取下一个输入。
// 对应 Python: BaseInteraction._get_next_interactive_input()
func (b *BaseInteraction) getNextInteractiveInput() any {
	if b.interactiveInputs != nil && b.idx < len(b.interactiveInputs) {
		res := b.interactiveInputs[b.idx]
		b.idx++
		logger.Info(logger.ComponentAgentCore).
			Str("action", "interaction_resume").
			Int("index", b.idx).
			Msg("交互恢复：从队列获取到用户输入")
		return res
	}
	return nil
}

// interruptAgentExecute 调用检查点器的中断方法。
// 通过类型断言延迟绑定，5.8 实现后自动生效。
func interruptAgentExecute(session baseSession) error {
	cp := session.Checkpointer()
	if cp == nil {
		return nil
	}
	if interrupter, ok := cp.(InteractionCheckpointer); ok {
		err := interrupter.InterruptAgentExecute(session)
		if err != nil {
			logger.Error(logger.ComponentAgentCore).
				Err(err).
				Str("event_type", "LLM_CALL_ERROR").
				Msg("检查点中断 Agent 执行失败")
		}
		return err
	}
	return nil
}

// writeInteractionOutput 写入交互输出到流。
// 通过类型断言延迟绑定，5.10 实现后自动生效。
func writeInteractionOutput(session baseSession, outputType string, index int, payload any) error {
	mgr := session.StreamWriterManager()
	if mgr == nil {
		return nil
	}
	if provider, ok := mgr.(InteractionOutputWriterProvider); ok {
		writer := provider.GetOutputWriter()
		err := writer.WriteInteraction(outputType, index, payload)
		if err != nil {
			logger.Warn(logger.ComponentAgentCore).
				Err(err).
				Str("output_type", outputType).
				Msg("交互输出写入流失败")
		}
		return err
	}
	return nil
}

// commitCMP 提交检查点状态。
// 对应 Python: session.state().commit_cmp()
func commitCMP(session baseSession) {
	st := session.State()
	if cs, ok := st.(*state.WorkflowCommitState); ok {
		cs.CommitCmp()
	}
}
```

- [ ] **Step 2: 编译检查**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/session/interaction/...
```

预期：编译通过，无错误。

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/session/interaction/base.go
git commit -m "feat(interaction): 添加 baseSession 接口 + BaseInteraction + GraphInterrupt/Interrupt + AgentInterrupt + 依赖接口"
```

---

### Task 3: 创建 interaction/interactive_input.go

**Files:**
- Create: `internal/agentcore/session/interaction/interactive_input.go`

- [ ] **Step 1: 创建 interactive_input.go**

```go
package interaction

import (
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// InteractiveInput 用户交互输入容器。
// 支持两种输入模式：RawInputs（未绑定节点 ID，首次交互）和 UserInputs（按节点 ID 绑定）。
// 两者互斥：RawInputs 已设置时不能调用 Update。
//
// 对应 Python: openjiuwen/core/session/interaction/interactive_input.py (InteractiveInput)
type InteractiveInput struct {
	// UserInputs 按节点 ID 绑定的输入映射
	UserInputs map[string]any
	// RawInputs 未绑定节点 ID 的原始输入（首次交互使用）
	RawInputs any
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewInteractiveInput 创建交互输入实例。
// rawInputs 为可选参数：不传则 RawInputs 为 nil（标记为"未提供"），
// 传入 nil 则返回错误（与 Python 一致：raw_inputs=None 被拒绝）。
// 对应 Python: InteractiveInput.__init__(raw_inputs)
func NewInteractiveInput(rawInputs ...any) (*InteractiveInput, error) {
	input := &InteractiveInput{
		UserInputs: make(map[string]any),
	}
	if len(rawInputs) == 0 {
		// 未提供 rawInputs，RawInputs 保持 nil
		return input, nil
	}
	if rawInputs[0] == nil {
		// 显式传入 nil，与 Python 一致拒绝
		return nil, exception.RaiseError(exception.StatusInteractionInputInvalid,
			exception.WithParam("reason", "value of raw_inputs is none"),
		)
	}
	input.RawInputs = rawInputs[0]
	return input, nil
}

// ──────────────────────────── InteractiveInput 方法 ────────────────────────────

// Update 添加节点绑定的输入。
// RawInputs 已设置时返回错误（互斥约束），nodeID 或 value 为 nil 时返回错误。
// 对应 Python: InteractiveInput.update(node_id, value)
func (i *InteractiveInput) Update(nodeID string, value any) error {
	if i.RawInputs != nil {
		return exception.RaiseError(exception.StatusInteractionInputInvalid,
			exception.WithParam("reason", "raw_inputs existed, update is invalid"),
		)
	}
	if nodeID == "" || value == nil {
		return exception.RaiseError(exception.StatusInteractionInputInvalid,
			exception.WithParam("reason", "value is none or node_id is none"),
		)
	}
	i.UserInputs[nodeID] = value
	return nil
}
```

- [ ] **Step 2: 编译检查**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/session/interaction/...
```

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/session/interaction/interactive_input.go
git commit -m "feat(interaction): 添加 InteractiveInput 用户输入容器"
```

---

### Task 4: 创建 interaction/interaction.go — 三种 Interaction 实现

**Files:**
- Create: `internal/agentcore/session/interaction/interaction.go`

- [ ] **Step 1: 创建 interaction.go**

```go
package interaction

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// InteractionOutput 交互输出数据，标识一次交互的节点和值。
// 对应 Python: openjiuwen/core/session/interaction/interaction.py (InteractionOutput)
type InteractionOutput struct {
	// ID 节点/可执行 ID
	ID string
	// Value 交互携带的值
	Value any
}

// WorkflowInteraction 工作流交互，通过 GraphInterrupt 暂停工作流图执行。
// 对应 Python: openjiuwen/core/session/interaction/interaction.py (WorkflowInteraction)
type WorkflowInteraction struct {
	// BaseInteraction 嵌入交互基类
	*BaseInteraction
	// nodeID 节点 ID（从 session.ExecutableID() 获取）
	nodeID string
}

// SimpleAgentInteraction 简单 Agent 交互，不管理输入队列。
// 仅保存检查点并触发 AgentInterrupt，无流输出。
// 对应 Python: openjiuwen/core/session/interaction/interaction.py (SimpleAgentInteraction)
type SimpleAgentInteraction struct {
	// session Agent 内部会话
	session baseSession
}

// AgentInteraction 完整 Agent 交互，管理输入队列 + checkpointer + stream 输出。
// 对应 Python: openjiuwen/core/session/interaction/interaction.py (AgentInteraction)
type AgentInteraction struct {
	// BaseInteraction 嵌入交互基类
	*BaseInteraction
	// session Agent 内部会话
	session baseSession
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewWorkflowInteraction 创建工作流交互实例。
// 构造时从 workflow_state 读取并清除 INTERACTIVE_INPUT，作为 defaultInput 传入 BaseInteraction。
// 对应 Python: WorkflowInteraction.__init__(session)
func NewWorkflowInteraction(session baseSession) *WorkflowInteraction {
	nodeID := session.ExecutableID()

	// 从 workflow_state 读取 INTERACTIVE_INPUT
	var workflowInteractiveInput any
	if st, ok := session.State().(*state.WorkflowCommitState); ok {
		workflowInteractiveInput = st.GetWorkflowState(state.StringKey(InteractiveInputKey))
		if workflowInteractiveInput != nil {
			// 清除 workflow_state 中的 INTERACTIVE_INPUT
			st.UpdateAndCommitWorkflowState(map[string]any{InteractiveInputKey: nil})
		}
	}

	// 构造 BaseInteraction，workflowInteractiveInput 作为 defaultInput
	bi := NewBaseInteraction(session, workflowInteractiveInput)

	return &WorkflowInteraction{
		BaseInteraction: bi,
		nodeID:          nodeID,
	}
}

// NewSimpleAgentInteraction 创建简单 Agent 交互实例。
// 对应 Python: SimpleAgentInteraction.__init__(session)
func NewSimpleAgentInteraction(session baseSession) *SimpleAgentInteraction {
	return &SimpleAgentInteraction{session: session}
}

// NewAgentInteraction 创建完整 Agent 交互实例。
// 对应 Python: AgentInteraction.__init__(session)
func NewAgentInteraction(session baseSession) *AgentInteraction {
	bi := NewBaseInteraction(session)
	return &AgentInteraction{
		BaseInteraction: bi,
		session:         session,
	}
}

// ──────────────────────────── WorkflowInteraction 方法 ────────────────────────────

// WaitUserInputs 等待用户输入。
// 1. 优先从输入队列获取（恢复场景）
// 2. 队列为空时：提交检查点 → 写流输出 → panic GraphInterrupt
// 对应 Python: WorkflowInteraction.wait_user_inputs(value)
func (w *WorkflowInteraction) WaitUserInputs(ctx context.Context, value any) (any, error) {
	res := w.getNextInteractiveInput()
	if res != nil {
		return res, nil
	}

	// 队列为空，需要中断等待用户输入
	commitCMP(w.session)

	payload := InteractionOutput{ID: w.nodeID, Value: value}
	_ = writeInteractionOutput(w.session, InteractionType, w.idx, payload)

	logger.Info(logger.ComponentAgentCore).
		Str("action", "workflow_interaction_interrupt").
		Str("node_id", w.nodeID).
		Int("index", w.idx).
		Msg("工作流交互中断：等待用户输入")

	PanicGraphInterrupt(Interrupt{
		Value: map[string]any{
			"type":    InteractionType,
			"index":   w.idx,
			"payload": payload,
		},
	})
	return nil, nil // 不可达，panic 后不会执行
}

// UserLatestInput 获取最近一次用户输入。
// 1. 有缓存输入时直接返回并清空
// 2. 无缓存时：写流输出 → panic GraphInterrupt(resumable=true)
// 对应 Python: WorkflowInteraction.user_latest_input(value)
func (w *WorkflowInteraction) UserLatestInput(ctx context.Context, value any) (any, error) {
	if w.latestInteractiveInput != nil {
		res := w.latestInteractiveInput
		w.latestInteractiveInput = nil
		return res, nil
	}

	// 无缓存，需要中断
	payload := &InteractionOutput{ID: w.nodeID, Value: value}
	_ = writeInteractionOutput(w.session, InteractionType, w.idx, payload)

	logger.Info(logger.ComponentAgentCore).
		Str("action", "workflow_latest_input_interrupt").
		Str("node_id", w.nodeID).
		Int("index", w.idx).
		Msg("工作流最新输入中断：等待用户输入")

	PanicGraphInterrupt(Interrupt{
		Value: map[string]any{
			"type":    InteractionType,
			"index":   w.idx,
			"payload": payload,
		},
		Resumable: true,
		NS:        w.nodeID,
	})
	return nil, nil // 不可达
}

// ──────────────────────────── SimpleAgentInteraction 方法 ────────────────────────────

// WaitUserInputs 等待用户输入（简单模式）。
// 保存检查点后触发 AgentInterrupt，无输入队列和流输出。
// 对应 Python: SimpleAgentInteraction.wait_user_inputs(message)
func (s *SimpleAgentInteraction) WaitUserInputs(ctx context.Context, message any) error {
	_ = interruptAgentExecute(s.session)

	msg := ""
	if m, ok := message.(string); ok {
		msg = m
	}

	logger.Info(logger.ComponentAgentCore).
		Str("action", "simple_agent_interaction_interrupt").
		Str("message", msg).
		Msg("简单 Agent 交互中断")

	PanicAgentInterrupt(msg)
	return nil // 不可达
}

// ──────────────────────────── AgentInteraction 方法 ────────────────────────────

// WaitUserInputs 等待用户输入（完整 Agent 模式）。
// 1. 优先从输入队列获取（恢复场景）
// 2. 队列为空时：保存检查点 → 写流输出 → panic AgentInterrupt
// 对应 Python: AgentInteraction.wait_user_inputs(value)
func (a *AgentInteraction) WaitUserInputs(ctx context.Context, value any) (any, error) {
	inputs := a.getNextInteractiveInput()
	if inputs != nil {
		return inputs, nil
	}

	// 队列为空，需要中断
	_ = interruptAgentExecute(a.session)

	payload := InteractionOutput{ID: a.session.ExecutableID(), Value: value}
	_ = writeInteractionOutput(a.session, InteractionType, a.idx, payload)

	logger.Info(logger.ComponentAgentCore).
		Str("action", "agent_interaction_interrupt").
		Str("executable_id", a.session.ExecutableID()).
		Int("index", a.idx).
		Msg("Agent 交互中断：等待用户输入")

	PanicAgentInterrupt("")
	return nil, nil // 不可达
}
```

- [ ] **Step 2: 编译检查**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/session/interaction/...
```

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/session/interaction/interaction.go
git commit -m "feat(interaction): 添加 WorkflowInteraction + SimpleAgentInteraction + AgentInteraction + InteractionOutput"
```

---

### Task 5: 创建 interaction/base_test.go — BaseInteraction 测试

**Files:**
- Create: `internal/agentcore/session/interaction/base_test.go`

- [ ] **Step 1: 创建 base_test.go**

```go
package interaction

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
)

// ──────────────────────────── fake 实现 ────────────────────────────

// fakeBaseSession 用于测试的最小 baseSession 实现
type fakeBaseSession struct {
	stateValue  state.State
	execIDValue string
	swMgrValue  any
	cpValue     any
}

func (f *fakeBaseSession) State() state.State            { return f.stateValue }
func (f *fakeBaseSession) ExecutableID() string          { return f.execIDValue }
func (f *fakeBaseSession) StreamWriterManager() any      { return f.swMgrValue }
func (f *fakeBaseSession) Checkpointer() any             { return f.cpValue }

// newFakeBaseSession 创建测试用 fake session
func newFakeBaseSession() *fakeBaseSession {
	return &fakeBaseSession{
		stateValue:  state.NewInMemoryWorkflowState(),
		execIDValue: "test.exec.id",
	}
}

// ──────────────────────────── NewBaseInteraction 测试 ────────────────────────────

// TestNewBaseInteraction_无默认输入 测试无 defaultInput 时队列为空
func TestNewBaseInteraction_无默认输入(t *testing.T) {
	session := newFakeBaseSession()
	bi := NewBaseInteraction(session)

	if bi == nil {
		t.Fatal("NewBaseInteraction 返回 nil")
	}
	if len(bi.interactiveInputs) != 0 {
		t.Errorf("无 defaultInput 时 interactiveInputs 应为空，实际长度=%d", len(bi.interactiveInputs))
	}
	if bi.idx != 0 {
		t.Errorf("idx 应为 0，实际=%d", bi.idx)
	}
}

// TestNewBaseInteraction_有默认输入 测试有 defaultInput 时队列包含默认值
func TestNewBaseInteraction_有默认输入(t *testing.T) {
	session := newFakeBaseSession()
	bi := NewBaseInteraction(session, "default_input")

	if len(bi.interactiveInputs) != 1 {
		t.Fatalf("interactiveInputs 长度应为 1，实际=%d", len(bi.interactiveInputs))
	}
	if bi.interactiveInputs[0] != "default_input" {
		t.Errorf("interactiveInputs[0] 期望 'default_input'，实际=%v", bi.interactiveInputs[0])
	}
	if bi.latestInteractiveInput != "default_input" {
		t.Errorf("latestInteractiveInput 期望 'default_input'，实际=%v", bi.latestInteractiveInput)
	}
}

// TestNewBaseInteraction_从SessionState读取输入 测试从 session state 合并已有输入
func TestNewBaseInteraction_从SessionState读取输入(t *testing.T) {
	session := newFakeBaseSession()
	// 预设 session state 中的输入
	session.State().Update(map[string]any{InteractiveInputKey: []any{"existing_input"}})

	bi := NewBaseInteraction(session, "default_input")

	// existing_input 在前，default_input 在后
	if len(bi.interactiveInputs) != 2 {
		t.Fatalf("interactiveInputs 长度应为 2，实际=%d", len(bi.interactiveInputs))
	}
	if bi.interactiveInputs[0] != "existing_input" {
		t.Errorf("interactiveInputs[0] 期望 'existing_input'，实际=%v", bi.interactiveInputs[0])
	}
	if bi.interactiveInputs[1] != "default_input" {
		t.Errorf("interactiveInputs[1] 期望 'default_input'，实际=%v", bi.interactiveInputs[1])
	}
}

// ──────────────────────────── getNextInteractiveInput 测试 ────────────────────────────

// TestGetNextInteractiveInput_顺序消费 测试输入队列顺序消费
func TestGetNextInteractiveInput_顺序消费(t *testing.T) {
	session := newFakeBaseSession()
	bi := NewBaseInteraction(session, "input1", "input2")

	// 第一次消费
	result := bi.getNextInteractiveInput()
	if result != "input1" {
		t.Errorf("第一次消费期望 'input1'，实际=%v", result)
	}
	if bi.idx != 1 {
		t.Errorf("消费后 idx 应为 1，实际=%d", bi.idx)
	}

	// 队列耗尽
	result2 := bi.getNextInteractiveInput()
	if result2 != nil {
		t.Errorf("队列耗尽后应返回 nil，实际=%v", result2)
	}
}

// TestGetNextInteractiveInput_队列为空 测试空队列返回 nil
func TestGetNextInteractiveInput_队列为空(t *testing.T) {
	session := newFakeBaseSession()
	bi := NewBaseInteraction(session)

	result := bi.getNextInteractiveInput()
	if result != nil {
		t.Errorf("空队列应返回 nil，实际=%v", result)
	}
}

// ──────────────────────────── GraphInterrupt 测试 ────────────────────────────

// TestPanicGraphInterrupt 测试 GraphInterrupt panic
func TestPanicGraphInterrupt(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("期望 panic GraphInterrupt，但未发生")
		}
		gi, ok := r.(*GraphInterrupt)
		if !ok {
			t.Fatalf("期望 *GraphInterrupt，得到 %T", r)
		}
		if len(gi.Interrupts) != 1 {
			t.Fatalf("Interrupts 长度应为 1，实际=%d", len(gi.Interrupts))
		}
		if gi.Interrupts[0].Value != "test_value" {
			t.Errorf("Interrupt.Value 期望 'test_value'，实际=%v", gi.Interrupts[0].Value)
		}
	}()
	PanicGraphInterrupt(Interrupt{Value: "test_value"})
}

// TestPanicAgentInterrupt 测试 AgentInterrupt panic
func TestPanicAgentInterrupt(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("期望 panic AgentInterrupt，但未发生")
		}
		ai, ok := r.(*AgentInterrupt)
		if !ok {
			t.Fatalf("期望 *AgentInterrupt，得到 %T", r)
		}
		if ai.Message != "test_msg" {
			t.Errorf("Message 期望 'test_msg'，实际=%s", ai.Message)
		}
	}()
	PanicAgentInterrupt("test_msg")
}

// ──────────────────────────── commitCMP 测试 ────────────────────────────

// TestCommitCMP_WorkflowCommitState 测试 WorkflowCommitState 提交
func TestCommitCMP_WorkflowCommitState(t *testing.T) {
	cs := state.NewInMemoryWorkflowState()
	session := &fakeBaseSession{stateValue: cs}

	// 不应 panic
	commitCMP(session)
}

// TestCommitCMP_非WorkflowCommitState 测试非 WorkflowCommitState 不 panic
func TestCommitCMP_非WorkflowCommitState(t *testing.T) {
	session := &fakeBaseSession{stateValue: state.NewInMemoryState()}

	// 不应 panic
	commitCMP(session)
}
```

- [ ] **Step 2: 运行测试**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test -v ./internal/agentcore/session/interaction/ -run "TestNewBaseInteraction|TestGetNextInteractiveInput|TestPanicGraphInterrupt|TestPanicAgentInterrupt|TestCommitCMP"
```

预期：全部 PASS。

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/session/interaction/base_test.go
git commit -m "test(interaction): 添加 BaseInteraction + GraphInterrupt + AgentInterrupt 测试"
```

---

### Task 6: 创建 interaction/interactive_input_test.go

**Files:**
- Create: `internal/agentcore/session/interaction/interactive_input_test.go`

- [ ] **Step 1: 创建 interactive_input_test.go**

```go
package interaction

import (
	"testing"
)

// ──────────────────────────── NewInteractiveInput 测试 ────────────────────────────

// TestNewInteractiveInput_无参数 测试不传参数时 RawInputs 为 nil
func TestNewInteractiveInput_无参数(t *testing.T) {
	input, err := NewInteractiveInput()
	if err != nil {
		t.Fatalf("不应返回错误：%v", err)
	}
	if input.RawInputs != nil {
		t.Errorf("RawInputs 应为 nil，实际=%v", input.RawInputs)
	}
	if input.UserInputs == nil {
		t.Error("UserInputs 不应为 nil")
	}
}

// TestNewInteractiveInput_有值 测试传入有效值时 RawInputs 被设置
func TestNewInteractiveInput_有值(t *testing.T) {
	input, err := NewInteractiveInput("user_response")
	if err != nil {
		t.Fatalf("不应返回错误：%v", err)
	}
	if input.RawInputs != "user_response" {
		t.Errorf("RawInputs 期望 'user_response'，实际=%v", input.RawInputs)
	}
}

// TestNewInteractiveInput_传入nil返回错误 测试显式传入 nil 被拒绝
func TestNewInteractiveInput_传入nil返回错误(t *testing.T) {
	_, err := NewInteractiveInput(nil)
	if err == nil {
		t.Fatal("传入 nil 时应返回错误")
	}
}

// ──────────────────────────── Update 测试 ────────────────────────────

// TestInteractiveInput_Update_正常 测试正常 Update
func TestInteractiveInput_Update_正常(t *testing.T) {
	input, _ := NewInteractiveInput()
	err := input.Update("node1", "value1")
	if err != nil {
		t.Fatalf("Update 不应返回错误：%v", err)
	}
	if input.UserInputs["node1"] != "value1" {
		t.Errorf("UserInputs['node1'] 期望 'value1'，实际=%v", input.UserInputs["node1"])
	}
}

// TestInteractiveInput_Update_RawInputs已存在 测试 RawInputs 存在时 Update 被拒绝
func TestInteractiveInput_Update_RawInputs已存在(t *testing.T) {
	input, _ := NewInteractiveInput("raw_data")
	err := input.Update("node1", "value1")
	if err == nil {
		t.Fatal("RawInputs 已存在时 Update 应返回错误")
	}
}

// TestInteractiveInput_Update_nodeID为空 测试 nodeID 为空时返回错误
func TestInteractiveInput_Update_nodeID为空(t *testing.T) {
	input, _ := NewInteractiveInput()
	err := input.Update("", "value1")
	if err == nil {
		t.Fatal("nodeID 为空时 Update 应返回错误")
	}
}

// TestInteractiveInput_Update_value为nil 测试 value 为 nil 时返回错误
func TestInteractiveInput_Update_value为nil(t *testing.T) {
	input, _ := NewInteractiveInput()
	err := input.Update("node1", nil)
	if err == nil {
		t.Fatal("value 为 nil 时 Update 应返回错误")
	}
}

// TestInteractiveInput_Update_多次 测试多次 Update 追加不同节点
func TestInteractiveInput_Update_多次(t *testing.T) {
	input, _ := NewInteractiveInput()
	_ = input.Update("node1", "value1")
	_ = input.Update("node2", "value2")

	if len(input.UserInputs) != 2 {
		t.Errorf("UserInputs 长度应为 2，实际=%d", len(input.UserInputs))
	}
	if input.UserInputs["node1"] != "value1" {
		t.Errorf("UserInputs['node1'] 期望 'value1'，实际=%v", input.UserInputs["node1"])
	}
	if input.UserInputs["node2"] != "value2" {
		t.Errorf("UserInputs['node2'] 期望 'value2'，实际=%v", input.UserInputs["node2"])
	}
}
```

- [ ] **Step 2: 运行测试**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test -v ./internal/agentcore/session/interaction/ -run "TestNewInteractiveInput|TestInteractiveInput_Update"
```

预期：全部 PASS。

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/session/interaction/interactive_input_test.go
git commit -m "test(interaction): 添加 InteractiveInput 测试"
```

---

### Task 7: 创建 interaction/interaction_test.go — 三种 Interaction 测试

**Files:**
- Create: `internal/agentcore/session/interaction/interaction_test.go`

- [ ] **Step 1: 创建 interaction_test.go**

```go
package interaction

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
)

// ──────────────────────────── fake checkpointer/writer ────────────────────────────

// fakeCheckpointer 测试用检查点器
type fakeCheckpointer struct {
	interruptErr error
	interrupted  bool
}

func (f *fakeCheckpointer) InterruptAgentExecute(session any) error {
	f.interrupted = true
	return f.interruptErr
}

// fakeOutputWriter 测试用输出写入器
type fakeOutputWriter struct {
	written bool
	lastErr error
}

func (f *fakeOutputWriter) WriteInteraction(outputType string, index int, payload any) error {
	f.written = true
	return f.lastErr
}

// fakeOutputWriterProvider 测试用输出写入器提供者
type fakeOutputWriterProvider struct {
	writer *fakeOutputWriter
}

func (f *fakeOutputWriterProvider) GetOutputWriter() InteractionOutputWriter {
	return f.writer
}

// ──────────────────────────── InteractionOutput 测试 ────────────────────────────

// TestInteractionOutput 测试结构体字段
func TestInteractionOutput(t *testing.T) {
	output := InteractionOutput{ID: "node1", Value: "test_val"}
	if output.ID != "node1" {
		t.Errorf("ID 期望 'node1'，实际=%s", output.ID)
	}
	if output.Value != "test_val" {
		t.Errorf("Value 期望 'test_val'，实际=%v", output.Value)
	}
}

// ──────────────────────────── WorkflowInteraction 测试 ────────────────────────────

// TestNewWorkflowInteraction 测试构造函数
func TestNewWorkflowInteraction(t *testing.T) {
	session := newFakeBaseSession()
	wi := NewWorkflowInteraction(session)

	if wi == nil {
		t.Fatal("NewWorkflowInteraction 返回 nil")
	}
	if wi.nodeID != "test.exec.id" {
		t.Errorf("nodeID 期望 'test.exec.id'，实际=%s", wi.nodeID)
	}
}

// TestWorkflowInteraction_WaitUserInputs_队列有输入 测试恢复场景直接返回
func TestWorkflowInteraction_WaitUserInputs_队列有输入(t *testing.T) {
	session := newFakeBaseSession()
	// 预设输入到 session state
	session.State().Update(map[string]any{InteractiveInputKey: []any{"user_answer"}})

	wi := NewWorkflowInteraction(session)
	result, err := wi.WaitUserInputs(context.Background(), "question")
	if err != nil {
		t.Fatalf("不应返回错误：%v", err)
	}
	if result != "user_answer" {
		t.Errorf("期望 'user_answer'，实际=%v", result)
	}
}

// TestWorkflowInteraction_WaitUserInputs_队列空时触发GraphInterrupt 测试中断场景
func TestWorkflowInteraction_WaitUserInputs_队列空时触发GraphInterrupt(t *testing.T) {
	session := newFakeBaseSession()
	wi := NewWorkflowInteraction(session)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("期望 panic GraphInterrupt，但未发生")
		}
		gi, ok := r.(*GraphInterrupt)
		if !ok {
			t.Fatalf("期望 *GraphInterrupt，得到 %T", r)
		}
		if len(gi.Interrupts) != 1 {
			t.Fatalf("Interrupts 长度应为 1，实际=%d", len(gi.Interrupts))
		}
	}()

	wi.WaitUserInputs(context.Background(), "question")
}

// TestWorkflowInteraction_UserLatestInput_有缓存 测试缓存命中直接返回
func TestWorkflowInteraction_UserLatestInput_有缓存(t *testing.T) {
	session := newFakeBaseSession()
	session.State().Update(map[string]any{InteractiveInputKey: []any{"latest_input"}})

	wi := NewWorkflowInteraction(session)
	result, err := wi.UserLatestInput(context.Background(), "value")
	if err != nil {
		t.Fatalf("不应返回错误：%v", err)
	}
	if result != "latest_input" {
		t.Errorf("期望 'latest_input'，实际=%v", result)
	}

	// 第二次调用应触发 GraphInterrupt（缓存已清空）
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("缓存清空后应触发 GraphInterrupt")
		}
		if _, ok := r.(*GraphInterrupt); !ok {
			t.Fatalf("期望 *GraphInterrupt，得到 %T", r)
		}
	}()
	wi.UserLatestInput(context.Background(), "value2")
}

// TestWorkflowInteraction_UserLatestInput_无缓存触发GraphInterrupt 测试无缓存中断
func TestWorkflowInteraction_UserLatestInput_无缓存触发GraphInterrupt(t *testing.T) {
	session := newFakeBaseSession()
	wi := NewWorkflowInteraction(session)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("期望 panic GraphInterrupt，但未发生")
		}
		gi, ok := r.(*GraphInterrupt)
		if !ok {
			t.Fatalf("期望 *GraphInterrupt，得到 %T", r)
		}
		if len(gi.Interrupts) != 1 {
			t.Fatalf("Interrupts 长度应为 1，实际=%d", len(gi.Interrupts))
		}
		if !gi.Interrupts[0].Resumable {
			t.Error("UserLatestInput 的 GraphInterrupt 应为 Resumable")
		}
		if gi.Interrupts[0].NS != "test.exec.id" {
			t.Errorf("NS 期望 'test.exec.id'，实际=%s", gi.Interrupts[0].NS)
		}
	}()

	wi.UserLatestInput(context.Background(), "value")
}

// TestWorkflowInteraction_有StreamWriter 测试 StreamWriterManager 存在时写入交互输出
func TestWorkflowInteraction_有StreamWriter(t *testing.T) {
	session := newFakeBaseSession()
	writer := &fakeOutputWriter{}
	session.swMgrValue = &fakeOutputWriterProvider{writer: writer}

	wi := NewWorkflowInteraction(session)

	defer func() {
		recover()
		if !writer.written {
			t.Error("StreamWriterManager 存在时应写入交互输出")
		}
	}()

	wi.WaitUserInputs(context.Background(), "question")
}

// ──────────────────────────── SimpleAgentInteraction 测试 ────────────────────────────

// TestNewSimpleAgentInteraction 测试构造函数
func TestNewSimpleAgentInteraction(t *testing.T) {
	session := newFakeBaseSession()
	sai := NewSimpleAgentInteraction(session)

	if sai == nil {
		t.Fatal("NewSimpleAgentInteraction 返回 nil")
	}
}

// TestSimpleAgentInteraction_WaitUserInputs_触发AgentInterrupt 测试中断场景
func TestSimpleAgentInteraction_WaitUserInputs_触发AgentInterrupt(t *testing.T) {
	session := newFakeBaseSession()
	session.cpValue = &fakeCheckpointer{}
	sai := NewSimpleAgentInteraction(session)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("期望 panic AgentInterrupt，但未发生")
		}
		ai, ok := r.(*AgentInterrupt)
		if !ok {
			t.Fatalf("期望 *AgentInterrupt，得到 %T", r)
		}
		if ai.Message != "test_msg" {
			t.Errorf("Message 期望 'test_msg'，实际=%s", ai.Message)
		}
	}()

	sai.WaitUserInputs(context.Background(), "test_msg")
}

// TestSimpleAgentInteraction_WaitUserInputs_有Checkpointer 测试 checkpointer 被调用
func TestSimpleAgentInteraction_WaitUserInputs_有Checkpointer(t *testing.T) {
	session := newFakeBaseSession()
	cp := &fakeCheckpointer{}
	session.cpValue = cp
	sai := NewSimpleAgentInteraction(session)

	defer func() {
		recover()
		if !cp.interrupted {
			t.Error("checkpointer.InterruptAgentExecute 应被调用")
		}
	}()

	sai.WaitUserInputs(context.Background(), "msg")
}

// ──────────────────────────── AgentInteraction 测试 ────────────────────────────

// TestNewAgentInteraction 测试构造函数
func TestNewAgentInteraction(t *testing.T) {
	session := newFakeBaseSession()
	ai := NewAgentInteraction(session)

	if ai == nil {
		t.Fatal("NewAgentInteraction 返回 nil")
	}
}

// TestAgentInteraction_WaitUserInputs_队列有输入 测试恢复场景直接返回
func TestAgentInteraction_WaitUserInputs_队列有输入(t *testing.T) {
	session := newFakeBaseSession()
	session.State().Update(map[string]any{InteractiveInputKey: []any{"agent_answer"}})

	ai := NewAgentInteraction(session)
	result, err := ai.WaitUserInputs(context.Background(), "value")
	if err != nil {
		t.Fatalf("不应返回错误：%v", err)
	}
	if result != "agent_answer" {
		t.Errorf("期望 'agent_answer'，实际=%v", result)
	}
}

// TestAgentInteraction_WaitUserInputs_队列空时触发AgentInterrupt 测试中断场景
func TestAgentInteraction_WaitUserInputs_队列空时触发AgentInterrupt(t *testing.T) {
	session := newFakeBaseSession()
	cp := &fakeCheckpointer{}
	session.cpValue = cp
	ai := NewAgentInteraction(session)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("期望 panic AgentInterrupt，但未发生")
		}
		ai2, ok := r.(*AgentInterrupt)
		if !ok {
			t.Fatalf("期望 *AgentInterrupt，得到 %T", r)
		}
		if ai2.Message != "" {
			t.Errorf("AgentInteraction 的 AgentInterrupt.Message 应为空，实际=%s", ai2.Message)
		}
		if !cp.interrupted {
			t.Error("checkpointer.InterruptAgentExecute 应被调用")
		}
	}()

	ai.WaitUserInputs(context.Background(), "value")
}

// TestAgentInteraction_WaitUserInputs_有StreamWriter 测试流输出写入
func TestAgentInteraction_WaitUserInputs_有StreamWriter(t *testing.T) {
	session := newFakeBaseSession()
	writer := &fakeOutputWriter{}
	session.swMgrValue = &fakeOutputWriterProvider{writer: writer}
	cp := &fakeCheckpointer{}
	session.cpValue = cp
	ai := NewAgentInteraction(session)

	defer func() {
		recover()
		if !writer.written {
			t.Error("StreamWriterManager 存在时应写入交互输出")
		}
	}()

	ai.WaitUserInputs(context.Background(), "value")
}

// ──────────────────────────── 依赖接口类型断言测试 ────────────────────────────

// TestInterruptAgentExecute_checkpointer为nil 测试 checkpointer 为 nil 不 panic
func TestInterruptAgentExecute_checkpointer为nil(t *testing.T) {
	session := newFakeBaseSession()
	err := interruptAgentExecute(session)
	if err != nil {
		t.Errorf("checkpointer 为 nil 时应返回 nil，实际=%v", err)
	}
}

// TestInterruptAgentExecute_类型不满足接口 测试 checkpointer 不满足接口时返回 nil
func TestInterruptAgentExecute_类型不满足接口(t *testing.T) {
	session := newFakeBaseSession()
	session.cpValue = "not_a_checkpointer"
	err := interruptAgentExecute(session)
	if err != nil {
		t.Errorf("类型不满足接口时应返回 nil，实际=%v", err)
	}
}

// TestWriteInteractionOutput_manager为nil 测试 StreamWriterManager 为 nil 不 panic
func TestWriteInteractionOutput_manager为nil(t *testing.T) {
	session := newFakeBaseSession()
	err := writeInteractionOutput(session, InteractionType, 0, "payload")
	if err != nil {
		t.Errorf("StreamWriterManager 为 nil 时应返回 nil，实际=%v", err)
	}
}

// TestWriteInteractionOutput_类型不满足接口 测试 manager 不满足接口时返回 nil
func TestWriteInteractionOutput_类型不满足接口(t *testing.T) {
	session := newFakeBaseSession()
	session.swMgrValue = "not_a_provider"
	err := writeInteractionOutput(session, InteractionType, 0, "payload")
	if err != nil {
		t.Errorf("类型不满足接口时应返回 nil，实际=%v", err)
	}
}
```

- [ ] **Step 2: 运行测试**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test -v ./internal/agentcore/session/interaction/ -run "TestInteractionOutput|TestNewWorkflowInteraction|TestWorkflowInteraction|TestNewSimpleAgentInteraction|TestSimpleAgentInteraction|TestNewAgentInteraction|TestAgentInteraction|TestInterruptAgentExecute|TestWriteInteractionOutput"
```

预期：全部 PASS。

- [ ] **Step 3: 运行全包测试**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test -cover ./internal/agentcore/session/interaction/...
```

预期：覆盖率 ≥ 85%。

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/session/interaction/interaction_test.go
git commit -m "test(interaction): 添加 WorkflowInteraction + SimpleAgentInteraction + AgentInteraction 测试"
```

---

### Task 8: 回填 NodeSessionFacade.Interact()

**Files:**
- Modify: `internal/agentcore/session/node.go`

- [ ] **Step 1: 修改 interaction 字段类型**

在 `node.go` 中将 `interaction any` + `⤵️ 5.7 回填` 替换为具体类型：

将：
```go
	// interaction 交互实例（懒初始化）
	// ⤵️ 5.7 回填：any → WorkflowInteraction
	interaction any
```

替换为：
```go
	// interaction 交互实例（懒初始化）
	interaction *interaction.WorkflowInteraction
```

同时添加 import `"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"`。

- [ ] **Step 2: 回填 Interact() 方法实现**

将：
```go
func (f *NodeSessionFacade) Interact(ctx context.Context, value any) (any, error) {
	if f.streamMode {
		return nil, fmt.Errorf("interact when streaming process(transform or collect) is not supported, comp_id=%s, workflow=%s",
			f.GetComponentID(), f.GetWorkflowID())
	}
	// ⤵️ 5.7 回填：if f.interaction == nil { f.interaction = NewWorkflowInteraction(f.inner) }
	// ⤵️ 5.7 回填：return f.interaction.WaitUserInputs(ctx, value)
	return nil, nil
}
```

替换为：
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

- [ ] **Step 3: 编译检查**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/session/...
```

预期：编译通过。

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/session/node.go
git commit -m "feat(session): 回填 NodeSessionFacade.Interact() 使用 WorkflowInteraction"
```

---

### Task 9: 回填 Session.Interact()

**Files:**
- Modify: `internal/agentcore/session/agent.go`

- [ ] **Step 1: 修改 interaction 字段类型**

在 `agent.go` 中将 `interaction any` + `⤵️ 5.9 回填` 替换为具体类型：

将：
```go
	// interaction 交互实例（懒初始化）
	// ⤵️ 5.9 回填：any → SimpleAgentInteraction
	interaction any
```

替换为：
```go
	// interaction 交互实例（懒初始化）
	interaction *interaction.SimpleAgentInteraction
```

同时添加 import `"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"` 和 `"context"`（如尚未导入）。

- [ ] **Step 2: 回填 Interact() 方法实现**

将：
```go
// Interact 请求用户输入。
// ⤵️ 5.9 回填：SimpleAgentInteraction 实现后填充真实逻辑
func (s *Session) Interact(value any) error {
	return nil
}
```

替换为：
```go
// Interact 请求用户输入。
// 对应 Python: Session.interact(value)
func (s *Session) Interact(value any) error {
	if s.interaction == nil {
		s.interaction = interaction.NewSimpleAgentInteraction(s.inner)
	}
	return s.interaction.WaitUserInputs(context.Background(), value)
}
```

- [ ] **Step 3: 编译检查**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/session/...
```

预期：编译通过。

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/session/agent.go
git commit -m "feat(session): 回填 Session.Interact() 使用 SimpleAgentInteraction"
```

---

### Task 10: 更新测试文件

**Files:**
- Modify: `internal/agentcore/session/node_test.go`
- Modify: `internal/agentcore/session/agent_test.go`

- [ ] **Step 1: 更新 node_test.go 中的 Interact 测试**

将 `TestNodeSessionFacade_Interact_非流式模式` 替换为：

```go
// TestNodeSessionFacade_Interact_非流式模式触发GraphInterrupt 测试非流式模式下 Interact 触发 GraphInterrupt
func TestNodeSessionFacade_Interact_非流式模式触发GraphInterrupt(t *testing.T) {
	ws := internal.NewWorkflowSession()
	ns := internal.NewNodeSession(ws, "node1", "Test", false)
	facade := NewNodeSessionFacade(ns, false) // streamMode=false

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("非流式模式下 Interact 应触发 GraphInterrupt")
		}
		if _, ok := r.(*interaction.GraphInterrupt); !ok {
			t.Fatalf("期望 *interaction.GraphInterrupt，得到 %T", r)
		}
	}()

	facade.Interact(context.Background(), "question")
}
```

添加 import `"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"`。

- [ ] **Step 2: 更新 agent_test.go 中的 Interact 测试**

将 `TestSession_桩方法返回Nil` 中的 Interact 断言替换为 panic 测试：

将：
```go
	if err := s.Interact(nil); err != nil {
		t.Errorf("Interact 桩应返回 nil，实际 %v", err)
	}
```

替换为：
```go
	// Interact 现在触发 SimpleAgentInteraction，会 panic AgentInterrupt
	func() {
		defer func() {
			r := recover()
			if r == nil {
				t.Error("Interact 应触发 AgentInterrupt panic")
			}
			if _, ok := r.(*interaction.AgentInterrupt); !ok {
				t.Errorf("期望 *interaction.AgentInterrupt，得到 %T", r)
			}
		}()
		s.Interact(nil)
	}()
```

添加 import `"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"`。

- [ ] **Step 3: 运行测试**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test -v ./internal/agentcore/session/ -run "TestNodeSessionFacade_Interact|TestSession_桩方法返回Nil"
```

预期：全部 PASS。

- [ ] **Step 4: 运行全包测试**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test -cover ./internal/agentcore/session/...
```

预期：全部 PASS，覆盖率 ≥ 85%。

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/session/node_test.go internal/agentcore/session/agent_test.go
git commit -m "test(session): 更新 Interact 测试验证真实 Interaction 行为"
```

---

### Task 11: 更新 session/doc.go 文件目录

**Files:**
- Modify: `internal/agentcore/session/doc.go`

- [ ] **Step 1: 在文件目录中添加 interaction 子包**

在 `doc.go` 的文件目录树中，在 `internal/` 条目之前添加：

```
//	├── interaction/         # 交互管理
//	│   ├── doc.go                           # interaction 包文档
//	│   ├── base.go                          # baseSession 接口 + BaseInteraction + GraphInterrupt/Interrupt + AgentInterrupt + 常量
//	│   ├── interaction.go                   # WorkflowInteraction + SimpleAgentInteraction + AgentInteraction + InteractionOutput
//	│   └── interactive_input.go             # InteractiveInput 用户输入容器
```

- [ ] **Step 2: 更新核心类型/接口索引**

在核心类型索引部分添加：

```
//	WorkflowInteraction   — 工作流交互，通过 GraphInterrupt 暂停图执行
//	SimpleAgentInteraction — 简单 Agent 交互，无输入队列
//	AgentInteraction      — 完整 Agent 交互，含输入队列 + 检查点 + 流输出
//	InteractiveInput      — 用户交互输入容器
//	GraphInterrupt        — 图级中断异常
//	AgentInterrupt        — Agent 中断异常
```

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/session/doc.go
git commit -m "docs(session): 更新 doc.go 添加 interaction 子包"
```

---

### Task 12: 更新 IMPLEMENTATION_PLAN.md 状态

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 5.7 状态**

将 5.7 行从 `☐` 改为 `✅`：

```
| 5.7 | ✅ | Interaction | 交互管理；✅ BaseInteraction/WorkflowInteraction/SimpleAgentInteraction/AgentInteraction/InteractiveInput/InteractionOutput/GraphInterrupt/Interrupt/AgentInterrupt 已实现；⤴️ 已回填 5.5 NodeSessionFacade.Interact()；⤴️ 已回填 5.9 Session.Interact() | `openjiuwen/core/session/interaction/` |
```

- [ ] **Step 2: 更新 5.5 行的回填标记**

5.5 行中 `⤵️ 5.7 回填 Interaction` 已完成，删除该标记。

- [ ] **Step 3: 更新 5.9 行的回填标记**

5.9 行中 `⤵️ 5.9 回填` 相关部分标记为已回填。

- [ ] **Step 4: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 5.7 Interaction 实现状态为已完成"
```

---

### Task 13: 最终验证

- [ ] **Step 1: 运行 interaction 包测试**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test -v -cover ./internal/agentcore/session/interaction/...
```

预期：全部 PASS，覆盖率 ≥ 85%。

- [ ] **Step 2: 运行 session 包全量测试**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test -v -cover ./internal/agentcore/session/...
```

预期：全部 PASS，覆盖率 ≥ 85%。

- [ ] **Step 3: 运行全项目编译**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...
```

预期：编译通过，无错误。
