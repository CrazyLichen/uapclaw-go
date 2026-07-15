package interaction

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
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

// InteractionOutputSchema 交互中断信号的结构化输出。
// 对应 Python: OutputSchema(type=INTERACTION, index=idx, payload=payload)
// 替代之前使用 map[string]any 的方式，提供类型安全。
// Payload 统一为 InteractionOutput 类型（Python 中 wait_user_inputs 用 InteractionOutput，
// user_latest_input 用 tuple，Go 统一为 InteractionOutput 更一致且类型安全）。
type InteractionOutputSchema struct {
	// Type 输出类型（如 "interaction"）
	Type string `json:"type"`
	// Index 交互序号
	Index int `json:"index"`
	// Payload 交互负载
	Payload InteractionOutput `json:"payload"`
}

// WorkflowInteraction 工作流交互，通过 GraphInterrupt 暂停工作流图执行。
// 对应 Python: openjiuwen/core/session/interaction/interaction.py (WorkflowInteraction)
type WorkflowInteraction struct {
	// BaseInteraction 嵌入交互基类
	*BaseInteraction
}

// SimpleAgentInteraction 简单 Agent 交互，不管理输入队列。
// 仅保存检查点并触发 AgentInterrupt，无流输出。
// 对应 Python: openjiuwen/core/session/interaction/interaction.py (SimpleAgentInteraction)
type SimpleAgentInteraction struct {
	// session Agent 内部会话
	session interfaces.InnerSession
}

// AgentInteraction 完整 Agent 交互，管理输入队列 + checkpointer + stream 输出。
// 对应 Python: openjiuwen/core/session/interaction/interaction.py (AgentInteraction)
type AgentInteraction struct {
	// BaseInteraction 嵌入交互基类（内含 session 字段）
	*BaseInteraction
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewWorkflowInteraction 创建工作流交互实例。
// nodeID 通过类型断言 ExecutableIDProvider 从 session 获取（Python 中 executable_id 只在 NodeSession 上有）。
// 构造时从 workflow_state 读取并清除 INTERACTIVE_INPUT，作为 defaultInput 传入 BaseInteraction。
// 对应 Python: WorkflowInteraction.__init__(session)
// 类型断言 WorkflowState 失败时对齐 Python AttributeError：Log Error + Panic。
func NewWorkflowInteraction(session interfaces.InnerSession) *WorkflowInteraction {
	// 类型断言获取 WorkflowState
	ws, ok := session.State().(state.WorkflowState)
	if !ok {
		logger.Error(logger.ComponentAgentCore).
			Str("action", "new_workflow_interaction").
			Str("state_type", fmt.Sprintf("%T", session.State())).
			Msg("当前状态不支持 Workflow 操作，对齐 Python AttributeError")
		panic(fmt.Sprintf("当前状态 %T 不支持 Workflow 操作（未实现 WorkflowState 接口），对齐 Python AttributeError", session.State()))
	}

	// 从 workflow_state 读取 INTERACTIVE_INPUT
	workflowInteractiveInput := ws.GetWorkflowState(state.StringKey(InteractiveInputKey))
	if workflowInteractiveInput != nil {
		// 清除 workflow_state 中的 INTERACTIVE_INPUT
		ws.UpdateAndCommitWorkflowState(map[string]any{InteractiveInputKey: nil})
	}

	// 构造 BaseInteraction，workflowInteractiveInput 作为 defaultInput
	bi := NewBaseInteraction(session, workflowInteractiveInput)

	return &WorkflowInteraction{
		BaseInteraction: bi,
	}
}

// NewSimpleAgentInteraction 创建简单 Agent 交互实例。
// 对应 Python: SimpleAgentInteraction.__init__(session)
func NewSimpleAgentInteraction(session interfaces.InnerSession) *SimpleAgentInteraction {
	return &SimpleAgentInteraction{session: session}
}

// NewAgentInteraction 创建完整 Agent 交互实例。
// nodeID 通过类型断言 ExecutableIDProvider 从 session 获取（Python 中 executable_id 只在 NodeSession 上有）。
// 对应 Python: AgentInteraction.__init__(session)
func NewAgentInteraction(session interfaces.InnerSession) *AgentInteraction {
	bi := NewBaseInteraction(session)
	return &AgentInteraction{
		BaseInteraction: bi,
	}
}

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

	nodeID := getExecutableID(w.session)
	payload := InteractionOutput{ID: nodeID, Value: value}
	_ = writeInteractionOutput(w.session, InteractionType, w.idx, payload)

	logger.Info(logger.ComponentAgentCore).
		Str("action", "workflow_interaction_interrupt").
		Str("node_id", nodeID).
		Int("index", w.idx).
		Msg("工作流交互中断：等待用户输入")

	PanicGraphInterrupt(Interrupt{
		Value: InteractionOutputSchema{
			Type:    InteractionType,
			Index:   w.idx,
			Payload: payload,
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
	nodeID := getExecutableID(w.session)
	payload := InteractionOutput{ID: nodeID, Value: value}
	_ = writeInteractionOutput(w.session, InteractionType, w.idx, payload)

	logger.Info(logger.ComponentAgentCore).
		Str("action", "workflow_latest_input_interrupt").
		Str("node_id", nodeID).
		Int("index", w.idx).
		Msg("工作流最新输入中断：等待用户输入")

	PanicGraphInterrupt(Interrupt{
		Value: InteractionOutputSchema{
			Type:    InteractionType,
			Index:   w.idx,
			Payload: payload,
		},
		Resumable: true,
		NS:        nodeID,
	})
	return nil, nil // 不可达
}

// WaitUserInputs 等待用户输入（简单模式）。
// 保存检查点后触发 AgentInterrupt，无输入队列和流输出。
// 对应 Python: SimpleAgentInteraction.wait_user_inputs(message)
// 对齐 Python：await self._agent_session.checkpointer().interrupt_agent_execute(self._agent_session)
func (s *SimpleAgentInteraction) WaitUserInputs(ctx context.Context, message any) error {
	// 对齐 Python: session.checkpointer().interrupt_agent_execute(session)
	if cp := s.session.Checkpointer(); cp != nil {
		if err := cp.InterruptAgentExecute(ctx, s.session); err != nil {
			return fmt.Errorf("检查点中断 Agent 执行失败: %w", err)
		}
	}

	logger.Info(logger.ComponentAgentCore).
		Str("action", "simple_agent_interaction_interrupt").
		Str("message", fmt.Sprintf("%v", message)).
		Msg("简单 Agent 交互中断")

	PanicAgentInterrupt(message)
	return nil // 不可达
}

// WaitUserInputs 等待用户输入（完整 Agent 模式）。
// 1. 优先从输入队列获取（恢复场景）
// 2. 队列为空时：保存检查点 → 写流输出 → panic AgentInterrupt
// 对应 Python: AgentInteraction.wait_user_inputs(value)
// 对齐 Python：await self._agent_session.checkpointer().interrupt_agent_execute(self._session)
func (a *AgentInteraction) WaitUserInputs(ctx context.Context, value any) (any, error) {
	inputs := a.getNextInteractiveInput()
	if inputs != nil {
		return inputs, nil
	}

	// 队列为空，需要中断
	// 对齐 Python: session.checkpointer().interrupt_agent_execute(session)
	if cp := a.session.Checkpointer(); cp != nil {
		if err := cp.InterruptAgentExecute(ctx, a.session); err != nil {
			return nil, fmt.Errorf("检查点中断 Agent 执行失败: %w", err)
		}
	}

	nodeID := getExecutableID(a.session)
	payload := InteractionOutput{ID: nodeID, Value: value}
	_ = writeInteractionOutput(a.session, InteractionType, a.idx, payload)

	logger.Info(logger.ComponentAgentCore).
		Str("action", "agent_interaction_interrupt").
		Str("executable_id", nodeID).
		Int("index", a.idx).
		Msg("Agent 交互中断：等待用户输入")

	PanicAgentInterrupt(nil)
	return nil, nil // 不可达
}

// ──────────────────────────── 非导出函数 ────────────────────────────
