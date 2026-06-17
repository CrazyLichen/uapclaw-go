package checkpointer

import (
	"context"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
)

// ──────────────────────────── 接口 ────────────────────────────

// Checkpointer 检查点器接口，定义会话状态持久化的生命周期钩子。
// 对应 Python: openjiuwen/core/session/checkpointer/base.py (Checkpointer)
type Checkpointer interface {
	// GetThreadID 获取线程 ID（session_id:workflow_id）
	GetThreadID(session CheckpointerSession) string
	// PreWorkflowExecute 工作流执行前
	PreWorkflowExecute(ctx context.Context, session CheckpointerSession, inputs any) error
	// PostWorkflowExecute 工作流执行后
	PostWorkflowExecute(ctx context.Context, session CheckpointerSession, result any, exception error) error
	// PreAgentExecute Agent 执行前
	PreAgentExecute(ctx context.Context, session CheckpointerSession, inputs any) error
	// PreAgentTeamExecute AgentTeam 执行前
	PreAgentTeamExecute(ctx context.Context, session CheckpointerSession, inputs any) error
	// InterruptAgentExecute Agent 中断时保存检查点
	InterruptAgentExecute(ctx context.Context, session CheckpointerSession) error
	// PostAgentExecute Agent 执行后保存检查点
	PostAgentExecute(ctx context.Context, session CheckpointerSession) error
	// PostAgentTeamExecute AgentTeam 执行后保存检查点
	PostAgentTeamExecute(ctx context.Context, session CheckpointerSession) error
	// SessionExists 检查会话是否存在
	SessionExists(ctx context.Context, sessionID string) (bool, error)
	// Release 释放会话资源
	Release(ctx context.Context, sessionID string) error
	// GraphStore 获取图状态存储
	// ⤵️ 8.7 回填：Graph Store 实现后返回 Store 实例
	GraphStore() any
}

// Storage 状态存储接口，负责单个实体的状态保存/恢复/清除。
// 对应 Python: openjiuwen/core/session/checkpointer/base.py (Storage)
type Storage interface {
	// Save 保存会话状态
	Save(ctx context.Context, session CheckpointerSession) error
	// Recover 恢复会话状态
	Recover(ctx context.Context, session CheckpointerSession, inputs any) error
	// Clear 清除会话数据
	Clear(ctx context.Context, entityID string) error
	// Exists 检查状态是否存在
	Exists(ctx context.Context, session CheckpointerSession) (bool, error)
}

// CheckpointerSession Checkpointer 所需的会话最小接口。
// AgentSession/WorkflowSession/NodeSession 天然满足此接口。
// AgentID/TeamID 通过 AgentIDProvider/TeamIDProvider 类型断言获取。
// WorkflowState 的扩展方法（GetUpdates/SetUpdates/Commit 等）通过
// 类型断言为 state.WorkflowState 接口获取（比断言具体类型更优雅）。
//
// ⤵️ 5.12 回填：Config() 返回类型从 CheckpointerConfig 改为 SessionConfig（5.12 定义），
// SessionConfig 将满足 CheckpointerConfig 接口。同时 AgentSession 添加 AgentID()/
// WorkflowID("")/Parent(nil) 方法后可直接满足此接口，消除 agentCheckpointerSession 适配器。
type CheckpointerSession interface {
	// SessionID 获取会话唯一标识
	SessionID() string
	// WorkflowID 获取工作流 ID
	WorkflowID() string
	// State 获取会话状态
	State() state.SessionState
	// Config 获取会话配置
	// ⤵️ 5.12 回填：返回类型从 CheckpointerConfig 改为 SessionConfig
	Config() CheckpointerConfig
	// Parent 获取父会话
	Parent() CheckpointerSession
}

// CheckpointerConfig Checkpointer 所需的配置最小接口。
// ⤵️ 5.12 回填：此接口可被 SessionConfig 替代或作为 SessionConfig 的子集，
// 届时 CheckpointerSession.Config() 直接返回 SessionConfig
type CheckpointerConfig interface {
	// GetEnv 获取环境变量值
	GetEnv(key string, defaultValue ...any) any
}

// AgentIDProvider 提供 Agent ID 的接口（通过类型断言获取）。
// AgentSession 天然满足此接口。
type AgentIDProvider interface {
	AgentID() string
}

// TeamIDProvider 提供 Team ID 的接口（通过类型断言获取）。
// AgentTeamSession 天然满足此接口。
type TeamIDProvider interface {
	TeamID() string
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// SessionNamespaceAgent Agent 状态命名空间
	SessionNamespaceAgent = "agent"
	// SessionNamespaceAgentTeam AgentTeam 状态命名空间
	SessionNamespaceAgentTeam = "agent-team"
	// SessionNamespaceWorkflow Workflow 状态命名空间
	SessionNamespaceWorkflow = "workflow"
	// WorkflowNamespaceGraph Workflow 图状态命名空间
	WorkflowNamespaceGraph = "workflow-graph"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// GetThreadID 获取线程 ID（session_id:workflow_id）。
// 对应 Python: Checkpointer.get_thread_id()
func GetThreadID(session CheckpointerSession) string {
	return session.SessionID() + ":" + session.WorkflowID()
}

// BuildKey 用 ":" 连接各部分，构建存储键。
// 对应 Python: build_key(*parts)
func BuildKey(parts ...string) string {
	return strings.Join(parts, ":")
}

// BuildKeyWithNamespace 构建带命名空间的存储键：session:namespace:entity:suffixes。
// 对应 Python: build_key_with_namespace(session_id, namespace, entity_id, *suffixes)
func BuildKeyWithNamespace(sessionID, namespace, entityID string, suffixes ...string) string {
	parts := []string{sessionID, namespace, entityID}
	parts = append(parts, suffixes...)
	return BuildKey(parts...)
}

// GetAgentID 类型断言获取 Agent ID，不存在返回空字符串。
// 对应 Python: session.agent_id() if hasattr(session, "agent_id") else "Na"
func GetAgentID(session CheckpointerSession) string {
	if provider, ok := session.(AgentIDProvider); ok {
		return provider.AgentID()
	}
	return ""
}

// GetTeamID 类型断言获取 Team ID，不存在返回空字符串。
// 对应 Python: session.team_id() if hasattr(session, "team_id") else "Na"
func GetTeamID(session CheckpointerSession) string {
	if provider, ok := session.(TeamIDProvider); ok {
		return provider.TeamID()
	}
	return ""
}
