package interaction

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// baseSession 交互所需的会话最小接口。
// WorkflowInteraction/AgentInteraction 通过此接口委托给内部会话实例，
// 避免 interaction 子包反向导入 session 父包。
// *internal.NodeSession / *internal.AgentSession 天然实现此接口（Go 隐式接口满足）。
// 注意：Python 的 BaseSession 也没有 executable_id()，executable_id 只在 NodeSession 上。
// WorkflowInteraction/AgentInteraction 所需的 nodeID 通过类型断言 ExecutableIDProvider 获取。
type baseSession interface {
	State() state.SessionState
	StreamWriterManager() any
	Checkpointer() any
}

// ExecutableIDProvider 提供可执行路径 ID 的接口。
// NodeSession 天然满足此接口（有 ExecutableID() 方法），AgentSession 不满足。
// 通过类型断言延迟绑定：WorkflowInteraction/AgentInteraction 运行时断言获取 nodeID。
type ExecutableIDProvider interface {
	ExecutableID() string
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
// Python 中 session.state().get(INTERACTIVE_INPUT) 查询的是 agent_state，
// Go 侧对应 SessionState.Get()（AgentStateCollection 中委托到 agentState，
// WorkflowStateCollection 中委托到 compState）。
func (b *BaseInteraction) initInteractiveInputs() {
	st := b.session.State()
	if st == nil {
		return
	}

	// 从组件级/agent 级状态读取交互输入（对齐 Python state().get()）
	existing := st.Get(state.StringKey(InteractiveInputKey))

	if existing == nil {
		// 无已有输入，仅更新 session state
		if len(b.interactiveInputs) > 0 {
			if err := st.Update(map[string]any{InteractiveInputKey: b.interactiveInputs}); err != nil {
				logger.Error(logger.ComponentAgentCore).
					Err(err).
					Str("action", "init_interactive_inputs").
					Msg("更新交互输入到 session state 失败")
			}
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
		if err := st.Update(map[string]any{InteractiveInputKey: b.interactiveInputs}); err != nil {
			logger.Error(logger.ComponentAgentCore).
				Err(err).
				Str("action", "init_interactive_inputs").
				Msg("写回交互输入到 session state 失败")
		}
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

// getExecutableID 通过类型断言从 session 获取可执行路径 ID。
// NodeSession 天然满足 ExecutableIDProvider 接口，AgentSession 不满足。
// 断言失败时返回空字符串，与 Python 行为一致（Python AgentSession 无 executable_id）。
func getExecutableID(session baseSession) string {
	if provider, ok := session.(ExecutableIDProvider); ok {
		return provider.ExecutableID()
	}
	return ""
}
