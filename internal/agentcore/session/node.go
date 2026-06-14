package session

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/internal"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// NodeSessionFacade 工作流节点会话门面，提供组件开发者面向的 API。
//
// 包装内部层 NodeSession，为工作流组件（ComponentExecutable）提供
// 身份查询、状态读写、追踪、交互、流写入、环境变量等能力。
//
// 对应 Python: openjiuwen/core/session/node.py (Session)
type NodeSessionFacade struct {
	// inner 内部节点会话
	inner *internal.NodeSession
	// streamMode 流式模式标记
	// on_stream/on_collect/on_transform 时为 true
	// 流式模式下 Interact() 返回错误，因为 GraphInterrupt 无法在 async generator 中恢复
	streamMode bool
	// interaction 交互实例（懒初始化）
	interaction *interaction.WorkflowInteraction
	// description 组件描述，格式: [wf_id=xxx,comp_id=xxx]
	description string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewNodeSessionFacade 创建工作流节点会话门面实例。
//
// streamMode 为 true 时，Interact() 将返回错误（流式模式不支持交互）。
// 对应 Python: Session(session, stream_mode)
func NewNodeSessionFacade(inner *internal.NodeSession, streamMode bool) *NodeSessionFacade {
	logger.Info(logger.ComponentAgentCore).
		Str("action", "new_node_session_facade").
		Str("node_id", inner.NodeID()).
		Bool("stream_mode", streamMode).
		Msg("创建节点会话门面")

	desc := fmt.Sprintf("[wf_id=%s,comp_id=%s]", inner.WorkflowID(), inner.NodeID())
	return &NodeSessionFacade{
		inner:       inner,
		streamMode:  streamMode,
		description: desc,
	}
}

// ──────────────────────────── 身份方法 ────────────────────────────

// GetWorkflowID 返回工作流 ID
// 对应 Python: Session.get_workflow_id()
func (f *NodeSessionFacade) GetWorkflowID() string {
	return f.inner.WorkflowID()
}

// GetComponentID 返回组件 ID（节点 ID）
// 对应 Python: Session.get_component_id()
func (f *NodeSessionFacade) GetComponentID() string {
	return f.inner.NodeID()
}

// GetComponentType 返回组件类型
// 对应 Python: Session.get_component_type()
func (f *NodeSessionFacade) GetComponentType() string {
	return f.inner.NodeType()
}

// GetComponentDescription 返回组件描述
// 对应 Python: Session.get_component_descrip()
func (f *NodeSessionFacade) GetComponentDescription() string {
	return f.description
}

// GetExecutableID 返回全局唯一可执行路径 ID
// 对应 Python: Session.get_executable_id()
func (f *NodeSessionFacade) GetExecutableID() string {
	return f.inner.ExecutableID()
}

// GetSessionID 返回会话唯一标识
// 对应 Python: Session.get_session_id()
func (f *NodeSessionFacade) GetSessionID() string {
	return f.inner.SessionID()
}

// ──────────────────────────── 状态方法 ────────────────────────────

// UpdateState 更新组件状态，委托到 inner.State() 的 WorkflowCommitState。
// 对应 Python: Session.update_state(data)
func (f *NodeSessionFacade) UpdateState(data map[string]any) {
	if cs, ok := f.inner.State().(*state.WorkflowCommitState); ok {
		cs.Update(data)
	}
}

// GetState 获取组件状态值，委托到 inner.State() 的 WorkflowCommitState。
// 对应 Python: Session.get_state(key)
func (f *NodeSessionFacade) GetState(key state.StateKey) (any, error) {
	if cs, ok := f.inner.State().(*state.WorkflowCommitState); ok {
		return cs.Get(key), nil
	}
	return nil, nil
}

// UpdateGlobalState 更新全局状态，委托到 inner.State() 的 WorkflowCommitState。
// 对应 Python: Session.update_global_state(data)
func (f *NodeSessionFacade) UpdateGlobalState(data map[string]any) {
	if cs, ok := f.inner.State().(*state.WorkflowCommitState); ok {
		cs.UpdateGlobal(data)
	}
}

// GetGlobalState 获取全局状态值，委托到 inner.State() 的 WorkflowCommitState。
// 对应 Python: Session.get_global_state(key)
func (f *NodeSessionFacade) GetGlobalState(key state.StateKey) (any, error) {
	if cs, ok := f.inner.State().(*state.WorkflowCommitState); ok {
		return cs.GetGlobal(key), nil
	}
	return nil, nil
}

// DumpState 导出完整状态快照，委托到 inner.State() 的 WorkflowCommitState。
// 对应 Python: Session.dump_state()
func (f *NodeSessionFacade) DumpState() map[string]any {
	if cs, ok := f.inner.State().(*state.WorkflowCommitState); ok {
		return cs.Dump()
	}
	return nil
}

// ──────────────────────────── 追踪方法（桩实现） ────────────────────────────

// Trace 记录组件追踪数据。
// ⤵️ 5.11 回填：TracerWorkflowUtils 实现后填充真实逻辑
// 对应 Python: Session.trace(data)
func (f *NodeSessionFacade) Trace(ctx context.Context, data map[string]any) error {
	if f.inner.SkipTrace() {
		return nil
	}
	// ⤵️ 5.11 回填：await TracerWorkflowUtils.trace(f.inner, data)
	return nil
}

// TraceError 记录组件错误追踪。
// ⤵️ 5.11 回填：TracerWorkflowUtils 实现后填充真实逻辑
// 对应 Python: Session.trace_error(error)
func (f *NodeSessionFacade) TraceError(ctx context.Context, err error) error {
	if f.inner.SkipTrace() {
		return nil
	}
	// ⤵️ 5.11 回填：await TracerWorkflowUtils.trace_error(f.inner, err)
	return nil
}

// ──────────────────────────── 交互方法（桩实现） ────────────────────────────

// Interact 请求用户输入。
//
// 流式模式下（streamMode=true）返回错误，因为 GraphInterrupt 无法在
// async generator 中恢复执行。这是工作流引擎的硬限制，不是设计偏好。
//
// ⤵️ 5.7 回填：WorkflowInteraction 实现后填充真实逻辑
// 对应 Python: Session.interact(value)
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

// ──────────────────────────── 流写入方法（桩实现） ────────────────────────────

// WriteStream 写入标准输出流。
// ⤵️ 5.10 回填：StreamWriterManager 实现后填充真实逻辑
// 对应 Python: Session.write_stream(data)
func (f *NodeSessionFacade) WriteStream(ctx context.Context, data any) error {
	// ⤵️ 5.10 回填：writer := f.streamWriter(); if writer != nil { return writer.Write(ctx, data) }
	return nil
}

// WriteCustomStream 写入自定义流。
// ⤵️ 5.10 回填：StreamWriterManager 实现后填充真实逻辑
// 对应 Python: Session.write_custom_stream(data)
func (f *NodeSessionFacade) WriteCustomStream(ctx context.Context, data any) error {
	// ⤵️ 5.10 回填：writer := f.customWriter(); if writer != nil { return writer.Write(ctx, data) }
	return nil
}

// ──────────────────────────── 环境/配置方法（桩实现） ────────────────────────────

// GetEnv 获取环境变量值。
// ⤵️ 5.12 回填：Config 返回真实类型后实现 get_env
// 对应 Python: Session.get_env(key)
func (f *NodeSessionFacade) GetEnv(key string) any {
	// ⤵️ 5.12 回填：return f.inner.Config().GetEnv(key)
	return nil
}

// GetNodeConfig 获取节点级配置。
// ⤵️ 5.12 回填：Config 返回真实类型后实现 get_node_config
// 对应 Python: Session.get_node_config()
func (f *NodeSessionFacade) GetNodeConfig() any {
	// ⤵️ 5.12 回填：return f.inner.NodeConfig()
	return nil
}
