package session

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/internal"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/tracer"
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

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

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

// UpdateState 更新组件状态，委托到 inner.State() 的 SessionState。
// 对应 Python: Session.update_state(data)
func (f *NodeSessionFacade) UpdateState(data map[string]any) {
	if err := f.inner.State().Update(data); err != nil {
		logger.Error(logger.ComponentAgentCore).
			Err(err).
			Str("action", "update_state").
			Str("node_id", f.GetComponentID()).
			Msg("更新组件状态失败")
	}
}

// GetState 获取组件状态值，委托到 inner.State() 的 SessionState。
// 对应 Python: Session.get_state(key)
func (f *NodeSessionFacade) GetState(key state.StateKey) (any, error) {
	return f.inner.State().Get(key), nil
}

// UpdateGlobalState 更新全局状态，委托到 inner.State() 的 SessionState。
// 对应 Python: Session.update_global_state(data)
func (f *NodeSessionFacade) UpdateGlobalState(data map[string]any) {
	f.inner.State().UpdateGlobal(data)
}

// GetGlobalState 获取全局状态值，委托到 inner.State() 的 SessionState。
// 对应 Python: Session.get_global_state(key)
func (f *NodeSessionFacade) GetGlobalState(key state.StateKey) (any, error) {
	return f.inner.State().GetGlobal(key), nil
}

// DumpState 导出完整状态快照，委托到 inner.State() 的 SessionState。
// 对应 Python: Session.dump_state()
func (f *NodeSessionFacade) DumpState() map[string]any {
	return f.inner.State().Dump()
}

// Trace 记录组件追踪数据。
// ✅ 5.11 已回填：使用 TracerWorkflowUtils.Trace 实现真实逻辑
// 对应 Python: Session.trace(data)
func (f *NodeSessionFacade) Trace(ctx context.Context, data map[string]any) error {
	if f.inner.SkipTrace() {
		return nil
	}
	innerTracer := f.inner.Tracer()
	if innerTracer == nil {
		return nil
	}
	tracer.TracerWorkflowUtils{}.Trace(ctx, f.inner, data)
	return nil
}

// TraceError 记录组件错误追踪。
// ✅ 5.11 已回填：使用 TracerWorkflowUtils.TraceError 实现真实逻辑
// 对应 Python: Session.trace_error(error)
func (f *NodeSessionFacade) TraceError(ctx context.Context, err error) error {
	if f.inner.SkipTrace() {
		return nil
	}
	innerTracer := f.inner.Tracer()
	if innerTracer == nil {
		return nil
	}
	tracer.TracerWorkflowUtils{}.TraceError(ctx, f.inner, err)
	return nil
}

// Interact 请求用户输入。
//
// 流式模式下（streamMode=true）返回错误，因为 GraphInterrupt 无法在
// async generator 中恢复执行。这是工作流引擎的硬限制，不是设计偏好。
//
// ✅ 5.7 已回填：WorkflowInteraction 实现后填充真实逻辑
// 对应 Python: Session.interact(value)
func (f *NodeSessionFacade) Interact(ctx context.Context, value any) (any, error) {
	if f.streamMode {
		return nil, fmt.Errorf("流式处理（transform 或 collect）期间不支持交互, comp_id=%s, workflow=%s",
			f.GetComponentID(), f.GetWorkflowID())
	}
	if f.interaction == nil {
		f.interaction = interaction.NewWorkflowInteraction(f.inner)
	}
	return f.interaction.WaitUserInputs(ctx, value)
}

// WriteStream 写入标准输出流。
// 对应 Python: Session.write_stream(data) → self._stream_writer().write(data)
// Python Node 层直接传 data 给 writer（writer 内部 Pydantic model_validate 校验），
// Go 没有 Pydantic，由 normalizeOutputStream 替代校验和转换逻辑：
//   - OutputSchema → 直接使用
//   - dict 含 type/index/payload → 构造 OutputSchema
//   - 其他 → OutputSchema{Type:"message", Index:0, Payload:data}
func (f *NodeSessionFacade) WriteStream(ctx context.Context, data any) error {
	mgr := f.inner.StreamWriterManager()
	if mgr == nil {
		return nil
	}
	writer := mgr.GetOutputWriter()
	if writer == nil {
		return nil
	}
	schema := normalizeOutputStream(data)
	return writer.Write(ctx, schema)
}

// WriteCustomStream 写入自定义流。
// 对应 Python: Session.write_custom_stream(data) → self._custom_writer().write(data)
// Python CustomStreamWriter 内部 CustomSchema.model_validate(data) 校验，
// Go 由 normalizeCustomStream 替代：dict → CustomSchema，其他 → 包装为 CustomSchema。
func (f *NodeSessionFacade) WriteCustomStream(ctx context.Context, data any) error {
	mgr := f.inner.StreamWriterManager()
	if mgr == nil {
		return nil
	}
	writer := mgr.GetCustomWriter()
	if writer == nil {
		return nil
	}
	schema := normalizeCustomStream(data)
	return writer.Write(ctx, schema)
}

// GetEnv 获取环境变量值。
// 对应 Python: Session.get_env(key)
func (f *NodeSessionFacade) GetEnv(key string) any {
	cfg := f.inner.Config()
	if cfg == nil {
		return nil
	}
	return cfg.GetEnv(key)
}

// GetNodeConfig 获取节点级配置。
// 对应 Python: Session.get_node_config()
func (f *NodeSessionFacade) GetNodeConfig() any {
	return f.inner.NodeConfig()
}
