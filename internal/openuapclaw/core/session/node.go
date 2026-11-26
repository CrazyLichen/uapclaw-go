package session

import (
	"context"
	"fmt"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/kv_cache"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/stream_actor"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/internal"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/state"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/tracer"
	workflow "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/workflowconfig"
)

// ──────────────────────────── 结构体 ────────────────────────────

// NodeSessionFacade 工作流节点会话门面，提供组件开发者面向的 API。
// 包装内部层 NodeSession，为工作流组件（ComponentExecutable）提供
// 身份查询、状态读写、追踪、交互、流写入、环境变量等能力。
// Python: openjiuwen/core/session/node.py (Session)
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

// ──────────────────────────── 全局变量 ────────────────────────────

// _ 编译时验证 NodeSessionFacade 满足 SessionFacade 接口
var _ interfaces.SessionFacade = (*NodeSessionFacade)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewNodeSessionFacade 创建工作流节点会话门面实例。
// streamMode 为 true 时，Interact() 将返回错误（流式模式不支持交互）。
// Python: Session(session, stream_mode)
func NewNodeSessionFacade(inner *internal.NodeSession, streamMode bool) *NodeSessionFacade {
	logger.Info(logComponent).
		Str("action", "new_node_session_facade").
		Str("node_id", inner.NodeID()).
		Bool("stream_mode", streamMode).
		Msg("created node session facade")

	desc := fmt.Sprintf("[wf_id=%s,comp_id=%s]", inner.WorkflowID(), inner.NodeID())
	return &NodeSessionFacade{
		inner:       inner,
		streamMode:  streamMode,
		description: desc,
	}
}

// GetWorkflowID 返回工作流 ID
// Python: Session.get_workflow_id()
func (f *NodeSessionFacade) GetWorkflowID() string {
	return f.inner.WorkflowID()
}

// InnerNodeSession 暴露包装的内部节点会话（Python getattr(session, "_inner")，
// workflow.py:734 子工作流会话装配取 inner）。
// 8.14 缺陷修复（R-14b-1）：createWorkflowSession is_sub 分支经此访问器取 inner，
// 此前全仓无实现者导致子会话分支恒 nil → cleanup 空指针；8.14b SubWorkflowComponent
// 为首个真实消费者（workflow_comp.py invoke/stream 转调 is_sub=true）。
func (f *NodeSessionFacade) InnerNodeSession() interfaces.InnerSession {
	return f.inner
}

// GetComponentID 返回组件 ID（节点 ID）
// Python: Session.get_component_id()
func (f *NodeSessionFacade) GetComponentID() string {
	return f.inner.NodeID()
}

// GetComponentType 返回组件类型
// Python: Session.get_component_type()
func (f *NodeSessionFacade) GetComponentType() string {
	return f.inner.NodeType()
}

// GetComponentDescription 返回组件描述
// Python: Session.get_component_descrip()
func (f *NodeSessionFacade) GetComponentDescription() string {
	return f.description
}

// GetExecutableID 返回全局唯一可执行路径 ID
// Python: Session.get_executable_id()
func (f *NodeSessionFacade) GetExecutableID() string {
	return f.inner.ExecutableID()
}

// GetSessionID 返回会话唯一标识
// Python: Session.get_session_id()
// GetAgentID 返回会话属主 agent 标识（Node 会话 Python 无该方法 → 空串承载）。
func (f *NodeSessionFacade) GetAgentID() string { return "" }

// GetTeamID 返回团队标识（Node 会话 Python 无该方法 → 空串承载）。
func (f *NodeSessionFacade) GetTeamID() string { return "" }

func (f *NodeSessionFacade) GetSessionID() string {
	return f.inner.SessionID()
}

// UpdateState 更新组件状态，委托到 inner.State() 的 SessionState。
// Python: Session.update_state(data)
func (f *NodeSessionFacade) UpdateState(data map[string]any) {
	if err := f.inner.State().Update(data); err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("action", "update_state").
			Str("node_id", f.GetComponentID()).
			Msg("failed to update component state")
	}
}

// GetState 获取组件状态值，委托到 inner.State() 的 SessionState。
// Python: Session.get_state(key)
func (f *NodeSessionFacade) GetState(key state.StateKey) (any, error) {
	return f.inner.State().Get(key), nil
}

// UpdateGlobalState 更新全局状态，委托到 inner.State() 的 SessionState。
// Python: Session.update_global_state(data)
func (f *NodeSessionFacade) UpdateGlobalState(data map[string]any) {
	f.inner.State().UpdateGlobal(data)
}

// GetGlobalState 获取全局状态值，委托到 inner.State() 的 SessionState。
// Python: Session.get_global_state(key)
func (f *NodeSessionFacade) GetGlobalState(key state.StateKey) (any, error) {
	return f.inner.State().GetGlobal(key), nil
}

// DumpState 导出完整状态快照，委托到 inner.State() 的 SessionState。
// Python: Session.dump_state()
func (f *NodeSessionFacade) DumpState() map[string]any {
	return f.inner.State().Dump()
}

// Trace 记录组件追踪数据。
// ✅ 5.11 已回填：使用 TracerWorkflowUtils.Trace 实现真实逻辑
// Python: Session.trace(data)
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
// Python: Session.trace_error(error)
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
// SessionFacade 接口实现，仅返回 error。
// 需要返回值时使用 InteractWithResult。
// 流式模式下（streamMode=true）返回错误，因为 GraphInterrupt 无法在
// async generator 中恢复执行。这是工作流引擎的硬限制，不是设计偏好。
// ✅ 5.7 已回填：WorkflowInteraction 实现后填充真实逻辑
// Python: Session.interact(value)
func (f *NodeSessionFacade) Interact(ctx context.Context, value any) error {
	_, err := f.InteractWithResult(ctx, value)
	return err
}

// InteractWithResult 请求用户输入并返回结果。
// 流式模式下（streamMode=true）返回错误，因为 GraphInterrupt 无法在
// async generator 中恢复执行。这是工作流引擎的硬限制，不是设计偏好。
// ✅ 5.7 已回填：WorkflowInteraction 实现后填充真实逻辑
// Python: Session.interact(value)
func (f *NodeSessionFacade) InteractWithResult(ctx context.Context, value any) (any, error) {
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
// Python: Session.write_stream(data) → self._stream_writer().write(data)
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
// Python: Session.write_custom_stream(data) → self._custom_writer().write(data)
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
// Python: Session.get_env(key)
func (f *NodeSessionFacade) GetEnv(key string, defaultValue ...any) any {
	cfg := f.inner.Config()
	if cfg == nil {
		return nil
	}
	return cfg.GetEnv(key, defaultValue...)
}

// GetNodeConfig 获取节点级配置。
// Python: Session.get_node_config()
func (f *NodeSessionFacade) GetNodeConfig() *workflow.NodeSpec {
	return f.inner.NodeConfig()
}

// ClearSession 清空节点会话状态。
// 对齐 Python session.clear_session()。
// 节点会话使用 WorkflowCommitState，清除组件状态。
func (f *NodeSessionFacade) ClearSession(ctx context.Context) {
	if st := f.inner.State(); st != nil {
		if wst, ok := st.(*state.WorkflowCommitState); ok {
			wst.Clear()
		} else if coll, ok := st.(*state.AgentStateCollection); ok {
			coll.Clear()
		} else if ims, ok := st.(*state.InMemoryStateLike); ok {
			ims.Clear()
		}
	}
	logger.Info(logComponent).
		Str("action", "node_session_clear").
		Str("node_id", f.inner.NodeID()).
		Msg("NodeSession cleared")
}

// ──────────────────────────── 导出函数（续：KV 亲和访问器） ────────────────────────────

// GetCacheIdentity 返回默认缓存身份（本类型无独立 cache identity，返回零值）。
// Python: 各 Session 子类的 get_cache_identity；Go 侧未迁该能力时由
// kv_cache.ResolveSessionLineage 的 fallback 链兜住。
func (f *NodeSessionFacade) GetCacheIdentity() kv_cache.KVCacheIdentity {
	return kv_cache.KVCacheIdentity{}
}

// GetParentSessionID 返回父会话标识；本类型无父会话，返回空串。
// Python: agent.py:74 get_parent_session_id() -> str | None
func (f *NodeSessionFacade) GetParentSessionID() string {
	return ""
}

// WorkflowVertexSession 工作流顶点会话消费面（8.11 Vertex 依赖）：
// internal.NodeSession 全量方法的窄化视图，由 *internal.NodeSession 实现。
// 独立接口的原因：graph 包受 Go internal 包可见性约束无法导入 session/internal，
// 构造经 NewWorkflowVertexSession 导出代理（构造调用位置仍在 Vertex.Init，
// 对齐 Python vertex.py:88 NodeSession(session, ...)；偏差 D-2 登记 8.11 spec §4.2）。
type WorkflowVertexSession interface {
	// interfaces.InnerSession 会话全量面（Config/State/Tracer/StreamWriterManager/
	// SessionID/Checkpointer/ActorManager/Close）——internal.NodeSession 已实现
	interfaces.InnerSession
	// tracer.WorkflowNodeSession 身份面（ExecutableID/ParentID/WorkflowID/
	// NodeID/NodeType）
	tracer.WorkflowNodeSession
	// StreamWriterManager 流写入管理器（Python: stream_writer_manager()）
	StreamWriterManager() *stream.StreamWriterManager
	// ActorManager Actor 管理器（Python: actor_manager()）
	ActorManager() *stream_actor.ActorManager
	// NodeConfig 节点规格（Python: node_config()）
	NodeConfig() *workflow.NodeSpec
	// SkipTrace 是否跳过追踪（Python: skip_trace）
	SkipTrace() bool
}

// NewWorkflowVertexSession 创建工作流顶点会话（内部层 NodeSession 的导出构造代理）。
// Python: session/internal/workflow.py NodeSession(session, node_id, node_type, skip_trace)。
func NewWorkflowVertexSession(parent interfaces.InnerSession, nodeID, nodeType string, skipTrace bool) WorkflowVertexSession {
	return internal.NewNodeSession(parent, nodeID, nodeType, skipTrace)
}

// NewWorkflowNodeSessionPair 工作流节点会话成对构造（8.14 前置件）：
// 返回组件开发者面门面与内部层节点会话（后者供 ExecuteSingleComponent 的
// state/commit 操作；调用方以类型推断持有，无需命名 internal 类型）。
func NewWorkflowNodeSessionPair(parent interfaces.InnerSession, nodeID, nodeType string,
	skipTrace bool) (*NodeSessionFacade, *internal.NodeSession) {
	inner := internal.NewNodeSession(parent, nodeID, nodeType, skipTrace)
	return NewNodeSessionFacade(inner, false), inner
}

// NewComponentSession 组件级会话构造：校验入参为工作流顶点会话（底层须为
// internal.NodeSession）并包装为组件开发者面门面。
// Python: components/component.py:95-99 isinstance(session, NodeSession) 校验 +
// Session(session, False/True) 包装（8.17 component.go 四钩子调用）。
// 类型不符返回 StatusWorkflowInnerOrchestrationError（Python
// WORKFLOW_INNER_ORCHESTRATION_ERROR 语义对齐）。
func NewComponentSession(sess BaseSession, streamMode bool) (*NodeSessionFacade, error) {
	vsess, ok := sess.(WorkflowVertexSession)
	if !ok {
		return nil, exception.BuildError(exception.StatusWorkflowInnerOrchestrationError,
			exception.WithParam("reason", "session type must be NodeSession when component hook invoked"))
	}
	inner, ok2 := vsess.(*internal.NodeSession)
	if !ok2 {
		return nil, exception.BuildError(exception.StatusWorkflowInnerOrchestrationError,
			exception.WithParam("reason", "session type must be NodeSession when component hook invoked"))
	}
	return NewNodeSessionFacade(inner, streamMode), nil
}
