package session

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/internal"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// WorkflowSession 工作流会话门面，提供用户面向的 API。
//
// 组合内部层 WorkflowSession，实现状态读写、环境变量管理、工作流卡片等业务功能。
// 生命周期比 Agent Session 更简单：仅 Close（无 PreRun/PostRun）。
//
// 对应 Python: openjiuwen/core/session/workflow.py (Session)
type WorkflowSession struct {
	// inner 内部 WorkflowSession 实例
	inner *internal.WorkflowSession
	// envs 环境变量（从 parent.config 获取）
	envs map[string]any
	// workflowCard 工作流卡片
	workflowCard *schema.WorkflowCard
}

// ──────────────────────────── 枚举 ────────────────────────────

// WorkflowSessionOption WorkflowSession 构造选项函数类型
type WorkflowSessionOption func(*WorkflowSession)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewWorkflowSession 创建公开层 WorkflowSession 实例。
//
// 对应 Python: openjiuwen/core/session/workflow.py create_workflow_session()
func NewWorkflowSession(opts ...WorkflowSessionOption) *WorkflowSession {
	logger.Info(logger.ComponentAgentCore).
		Str("action", "new_workflow_session_facade").
		Msg("创建公开层 WorkflowSession")

	ws := &WorkflowSession{
		envs: make(map[string]any),
	}
	for _, opt := range opts {
		opt(ws)
	}
	return ws
}

// WithWorkflowSessionParent 设置父会话的选项
func WithWorkflowSessionParent(parent InnerSession) WorkflowSessionOption {
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

// WithWorkflowSessionSessionID 设置会话 ID 的选项
func WithWorkflowSessionSessionID(id string) WorkflowSessionOption {
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
func (ws *WorkflowSession) GetParent() InnerSession {
	if ws.inner == nil {
		return nil
	}
	return ws.inner.Parent()
}

// SetWorkflowCard 设置工作流卡片
func (ws *WorkflowSession) SetWorkflowCard(card *schema.WorkflowCard) {
	ws.workflowCard = card
}

// GetWorkflowCard 返回工作流卡片
func (ws *WorkflowSession) GetWorkflowCard() *schema.WorkflowCard {
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

// ──────────────────────────── 非导出函数 ────────────────────────────
