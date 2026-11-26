package session

import (
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/stream_actor"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/kv_cache"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/config"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/internal"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/state"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/tracer"
	workflow "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/workflowconfig"
)

// ──────────────────────────── 结构体 ────────────────────────────

// WorkflowSession 工作流会话门面，提供用户面向的 API。
// 组合内部层 WorkflowSession，实现状态读写、环境变量管理、工作流卡片等业务功能。
// 生命周期比 Agent Session 更简单：仅 Close（无 PreRun/PostRun）。
// Python: openjiuwen/core/session/workflow.py (Session)
type WorkflowSession struct {
	// inner 内部 WorkflowSession 实例
	inner *internal.WorkflowSession
	// envs 环境变量（从 parent.config 获取）
	envs map[string]any
	// workflowCard 工作流卡片
	workflowCard *workflow.WorkflowCard
}

// WorkflowSessionOption WorkflowSession 构造选项函数类型
type WorkflowSessionOption func(*WorkflowSession)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewWorkflowSession 创建公开层 WorkflowSession 实例。
// Python: openjiuwen/core/session/workflow.py create_workflow_session()
func NewWorkflowSession(opts ...WorkflowSessionOption) *WorkflowSession {
	logger.Info(logComponent).
		Str("action", "new_workflow_session_facade").
		Msg("created public layer WorkflowSession")

	ws := &WorkflowSession{
		envs: make(map[string]any),
	}
	for _, opt := range opts {
		opt(ws)
	}
	if ws.inner == nil {
		// Python WorkflowSession(workflow_id, parent, session_id) 无参默认：
		// inner 兜底创建，保证门面 InnerSession 契约完整（8.14 前置件）
		ws.inner = internal.NewWorkflowSession()
	}
	return ws
}

// NewWorkflowSessionFacade 工作流会话门面三参构造（Python workflow.py:711-713
// WorkflowSession(workflow_id=..., parent=..., session_id=...) 一比一）。
func NewWorkflowSessionFacade(workflowID, sessionID string, parent sessioninterfaces.InnerSession) *WorkflowSession {
	ws := NewWorkflowSession(WithWorkflowSessionID(sessionID))
	ws.inner.SetWorkflowID(workflowID)
	// Python: workflow_session.config().set_envs(session.get_envs())
	if parent != nil && parent.Config() != nil {
		ws.inner.Config().SetEnvs(parent.Config().GetEnvs())
	}
	return ws
}

// NewSubWorkflowSessionFacade 子工作流会话门面构造（Python workflow.py:735-744
// SubWorkflowSession(inner_session, workflow_id=..., actor_manager=...) 一比一）。
// inner 需为 NodeSession（Python getattr(session, "_inner") 产物）。
func NewSubWorkflowSessionFacade(inner sessioninterfaces.InnerSession, workflowID string,
	am *stream_actor.ActorManager) sessioninterfaces.InnerSession {
	ns, ok := inner.(*internal.NodeSession)
	if !ok {
		return nil
	}
	return internal.NewSubWorkflowSession(ns, workflowID, am)
}

// WithWorkflowSessionParent 设置父会话的选项
func WithWorkflowSessionParent(parent sessioninterfaces.InnerSession) WorkflowSessionOption {
	return func(ws *WorkflowSession) {
		var envs map[string]any
		if parent != nil && parent.Config() != nil {
			envs = parent.Config().GetEnvs()
		}
		if envs == nil {
			envs = make(map[string]any)
		}
		ws.envs = envs
	}
}

// WithWorkflowSessionID 设置会话 ID 的选项
func WithWorkflowSessionID(id string) WorkflowSessionOption {
	return func(ws *WorkflowSession) {
		if ws.inner == nil {
			ws.inner = internal.NewWorkflowSession(
				internal.WithWorkflowSessionID(id),
			)
		}
	}
}

// WithWorkflowSessionInner 设置内部 WorkflowSession 的选项
func WithWorkflowSessionInner(inner *internal.WorkflowSession) WorkflowSessionOption {
	return func(ws *WorkflowSession) {
		ws.inner = inner
	}
}

// GetSessionID 返回会话唯一标识
// GetAgentID 返回会话属主 agent 标识（Workflow 会话 Python 无该方法 → 空串承载）。
func (ws *WorkflowSession) GetAgentID() string { return "" }

// GetTeamID 返回团队标识（Workflow 会话 Python 无该方法 → 空串承载）。
func (ws *WorkflowSession) GetTeamID() string { return "" }

func (ws *WorkflowSession) GetSessionID() string {
	if ws.inner == nil {
		return ""
	}
	return ws.inner.SessionID()
}

// GetEnvs 返回环境变量
func (ws *WorkflowSession) GetEnvs() map[string]any {
	return ws.envs
}

// GetParent 返回父会话
func (ws *WorkflowSession) GetParent() sessioninterfaces.InnerSession {
	if ws.inner == nil {
		return nil
	}
	return ws.inner.Parent()
}

// SetWorkflowCard 设置工作流卡片
func (ws *WorkflowSession) SetWorkflowCard(card *workflow.WorkflowCard) {
	ws.workflowCard = card
}

// GetWorkflowCard 返回工作流卡片
func (ws *WorkflowSession) GetWorkflowCard() *workflow.WorkflowCard {
	return ws.workflowCard
}

// Close 关闭会话，委托 inner.Close()
func (ws *WorkflowSession) Close() error {
	if ws.inner == nil {
		return nil
	}
	return ws.inner.Close()
}

// Inner 返回内部 WorkflowSession 实例（用于高级场景）
func (ws *WorkflowSession) Inner() *internal.WorkflowSession {
	return ws.inner
}

// ──────────────────────────── 导出函数（续：KV 亲和访问器） ────────────────────────────

// GetCacheIdentity 返回默认缓存身份（本类型无独立 cache identity，返回零值）。
// Python: 各 Session 子类的 get_cache_identity；Go 侧未迁该能力时由
// kv_cache.ResolveSessionLineage 的 fallback 链兜住。
func (ws *WorkflowSession) GetCacheIdentity() kv_cache.KVCacheIdentity {
	return kv_cache.KVCacheIdentity{}
}

// GetParentSessionID 返回父会话标识；本类型无父会话，返回空串。
// Python: agent.py:74 get_parent_session_id() -> str | None
func (ws *WorkflowSession) GetParentSessionID() string {
	return ""
}

// ──────────────────────────── InnerSession 契约委托（8.14 前置件）────────────────────────────

// SessionID 返回会话唯一标识（InnerSession 契约）。
func (ws *WorkflowSession) SessionID() string {
	if ws.inner == nil {
		return ""
	}
	return ws.inner.SessionID()
}

// Config 返回会话配置（InnerSession 契约）。
func (ws *WorkflowSession) Config() config.SessionConfig {
	if ws.inner == nil {
		return nil
	}
	return ws.inner.Config()
}

// State 返回会话状态（InnerSession 契约）。
func (ws *WorkflowSession) State() state.SessionState {
	if ws.inner == nil {
		return nil
	}
	return ws.inner.State()
}

// Tracer 返回追踪器（InnerSession 契约）。
func (ws *WorkflowSession) Tracer() *tracer.Tracer {
	if ws.inner == nil {
		return nil
	}
	return ws.inner.Tracer()
}

// StreamWriterManager 返回流写入管理器（InnerSession 契约）。
func (ws *WorkflowSession) StreamWriterManager() *stream.StreamWriterManager {
	if ws.inner == nil {
		return nil
	}
	return ws.inner.StreamWriterManager()
}

// Checkpointer 返回检查点器（InnerSession 契约）。
func (ws *WorkflowSession) Checkpointer() sessioninterfaces.Checkpointer {
	if ws.inner == nil {
		return nil
	}
	return ws.inner.Checkpointer()
}

// ActorManager 返回 Actor 管理器（InnerSession 契约）。
func (ws *WorkflowSession) ActorManager() *stream_actor.ActorManager {
	if ws.inner == nil {
		return nil
	}
	return ws.inner.ActorManager()
}

// WorkflowID 返回工作流标识（WorkflowIDProvider 契约）。
func (ws *WorkflowSession) WorkflowID() string {
	if ws.inner == nil {
		return ""
	}
	return ws.inner.WorkflowID()
}

// SetWorkflowID 设置工作流标识（Python set_workflow_id）。
func (ws *WorkflowSession) SetWorkflowID(id string) {
	if ws.inner != nil {
		ws.inner.SetWorkflowID(id)
	}
}

// SetActorManager 设置 Actor 管理器（Python set_actor_manager）。
func (ws *WorkflowSession) SetActorManager(mgr *stream_actor.ActorManager) {
	if ws.inner != nil {
		ws.inner.SetActorManager(mgr)
	}
}

// SetStreamWriterManager 设置流写入管理器（Python set_stream_writer_manager）。
func (ws *WorkflowSession) SetStreamWriterManager(mgr *stream.StreamWriterManager) {
	if ws.inner != nil {
		ws.inner.SetStreamWriterManager(mgr)
	}
}

// SetTracer 设置追踪器（Python set_tracer）。
func (ws *WorkflowSession) SetTracer(t *tracer.Tracer) {
	if ws.inner != nil {
		ws.inner.SetTracer(t)
	}
}
