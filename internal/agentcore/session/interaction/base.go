package interaction

import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/checkpointer"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// baseSession 交互所需的会话最小接口。
// 嵌入 checkpointer.CheckpointerSession（SessionID/State/Config/Checkpointer），
// 确保 session 可以直接传给 checkpointer 的 InterruptAgentExecute 等方法，
// 对齐 Python: interaction 直接将 session 传给 checkpointer，无需二次断言。
// 额外包含 StreamWriterManager，用于交互输出写入流。
// *internal.NodeSession / *internal.AgentSession 天然实现此接口（Go 隐式接口满足）。
// 注意：Python 的 BaseSession 也没有 executable_id()，executable_id 只在 NodeSession 上。
// WorkflowInteraction/AgentInteraction 所需的 nodeID 通过类型断言 ExecutableIDProvider 获取。
type baseSession interface {
	checkpointer.CheckpointerSession
	StreamWriterManager() any
}

// ExecutableIDProvider 提供可执行路径 ID 的接口。
// NodeSession 天然满足此接口（有 ExecutableID() 方法），AgentSession 不满足。
// 通过类型断言延迟绑定：WorkflowInteraction/AgentInteraction 运行时断言获取 nodeID。
type ExecutableIDProvider interface {
	ExecutableID() string
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
// Message 类型为 any（对齐 Python AgentInterrupt(message) 不限制参数类型）。
type AgentInterrupt struct {
	// Message 中断消息（可以是 string、dict 等任意类型，对齐 Python 行为）
	Message any
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

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// InteractionType 交互事件类型标识
	// 对应 Python: INTERACTION = "__interaction__"
	InteractionType = "__interaction__"
	// InteractiveInputKey 交互输入在 session state 中的键
	// 对应 Python: INTERACTIVE_INPUT = "__interactive_input__"
	InteractiveInputKey = "__interactive_input__"
)

// ──────────────────────────── 全局变量 ────────────────────────────

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
func PanicAgentInterrupt(msg any) {
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
// Python 中通过继承链直接调用，Go 中通过类型断言 WorkflowState 获取。
// 类型断言失败时对齐 Python AttributeError：Log Error + Panic。
func commitCMP(session baseSession) {
	st := session.State()
	if ws, ok := st.(state.WorkflowState); ok {
		ws.CommitCmp()
		return
	}
	logger.Error(logger.ComponentAgentCore).
		Str("action", "commit_cmp").
		Str("state_type", fmt.Sprintf("%T", st)).
		Msg("当前状态不支持 CommitCmp，对齐 Python AttributeError")
	panic(fmt.Sprintf("当前状态 %T 不支持 CommitCmp（未实现 WorkflowState 接口），对齐 Python AttributeError", st))
}

// getExecutableID 通过类型断言从 session 获取可执行路径 ID。
// NodeSession 天然满足 ExecutableIDProvider 接口，AgentSession 不满足。
// 断言失败时记录 Warn 日志并返回空字符串，与 Python 行为一致（Python AgentSession 无 executable_id）。
func getExecutableID(session baseSession) string {
	if provider, ok := session.(ExecutableIDProvider); ok {
		return provider.ExecutableID()
	}
	logger.Warn(logger.ComponentAgentCore).
		Str("action", "get_executable_id").
		Str("session_type", fmt.Sprintf("%T", session)).
		Msg("session 不满足 ExecutableIDProvider 接口，返回空字符串")
	return ""
}
