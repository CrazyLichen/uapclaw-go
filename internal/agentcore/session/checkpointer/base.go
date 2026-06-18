package checkpointer

import (
	"context"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Checkpointer 检查点器接口，定义会话状态持久化的生命周期钩子。
// 对应 Python: openjiuwen/core/session/checkpointer/base.py (Checkpointer)
type Checkpointer interface {
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
	// Release 释放会话资源。
	// agentID 可选参数：提供时仅释放指定 Agent 的检查点（支持多个，循环清除），否则释放整个 session。
	// 对齐 Python: release(session_id, agent_id=None)，Go 扩展支持批量 agentID。
	Release(ctx context.Context, sessionID string, agentID ...string) error
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
	// entityID 为实体标识（Agent 的 agentID / Workflow 的 workflowID）
	// sessionID 为会话标识，Persistence 版用于构建 KV key，InMemory 版忽略
	Clear(ctx context.Context, entityID, sessionID string) error
	// Exists 检查状态是否存在
	Exists(ctx context.Context, session CheckpointerSession) (bool, error)
}

// CheckpointerSession Checkpointer 所需的会话最小接口。
// 对齐 Python BaseSession：只包含 Checkpointer 实际使用的 BaseSession 方法子集。
// AgentSession/WorkflowSession/NodeSession 天然满足此接口。
// AgentID/TeamID 通过 AgentIDProvider/TeamIDProvider 类型断言获取。
// WorkflowID 通过 WorkflowIDProvider 类型断言获取（WorkflowSession/NodeSession 满足）。
// Parent 通过 ParentProvider 类型断言获取（WorkflowSession/NodeSession 满足）。
// Config 返回 any，需要 GetEnv 时通过 CheckpointerConfigProvider 类型断言获取。
// ⤵️ 5.12 回填：Config() 返回类型改为 SessionConfig 后 CheckpointerConfigProvider 可移除。
type CheckpointerSession interface {
	// SessionID 获取会话唯一标识
	SessionID() string
	// State 获取会话状态
	State() state.SessionState
	// Config 获取会话配置
	// ⤵️ 5.12 回填：返回类型从 any 改为 SessionConfig
	Config() any
	// Checkpointer 获取检查点管理器
	Checkpointer() Checkpointer
}

// WorkflowIDProvider 提供 WorkflowID 的接口（通过类型断言获取）。
// WorkflowSession/NodeSession 天然满足此接口。
// 对齐 Python: hasattr(session, "workflow_id") 检测。
type WorkflowIDProvider interface {
	WorkflowID() string
}

// ParentProvider 提供 Parent 的接口（通过类型断言获取）。
// WorkflowSession/NodeSession 天然满足此接口，AgentSession 不满足。
// 对齐 Python: isinstance(session.parent(), AgentSession) 检测。
type ParentProvider interface {
	Parent() CheckpointerSession
}

// CheckpointerConfigProvider 提供 GetEnv 方法的接口（通过类型断言获取）。
// 用于 session.Config() 返回 any 后，需要调用 GetEnv 的场景。
// ⤵️ 5.12 回填：Config() 返回 SessionConfig 后此接口可移除。
type CheckpointerConfigProvider interface {
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

// ──────────────────────────── 枚举 ────────────────────────────

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
// 对应 Python: Checkpointer.get_thread_id()（@staticmethod）
func GetThreadID(session CheckpointerSession) string {
	return session.SessionID() + ":" + getWorkflowID(session)
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

// GetAgentID 类型断言获取 Agent ID，不存在返回 "Na"。
// 对应 Python: session.agent_id() if hasattr(session, "agent_id") else "Na"
func GetAgentID(session CheckpointerSession) string {
	if provider, ok := session.(AgentIDProvider); ok {
		if id := provider.AgentID(); id != "" {
			return id
		}
	}
	return "Na"
}

// GetTeamID 类型断言获取 Team ID，不存在返回 "Na"。
// 对应 Python: session.team_id() if hasattr(session, "team_id") else "Na"
func GetTeamID(session CheckpointerSession) string {
	if provider, ok := session.(TeamIDProvider); ok {
		if id := provider.TeamID(); id != "" {
			return id
		}
	}
	return "Na"
}

// GetConfigEnv 从 session.Config() 断言为 CheckpointerConfigProvider 后调用 GetEnv。
// 返回环境变量值和是否可用。
// ⤵️ 5.12 回填：Config() 返回 SessionConfig 后可直接调用 Config().GetEnv()。
func GetConfigEnv(session CheckpointerSession, key string, defaultValue ...any) (any, bool) {
	if cfg, ok := session.Config().(CheckpointerConfigProvider); ok {
		return cfg.GetEnv(key, defaultValue...), true
	}
	return nil, false
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getWorkflowID 从 session 获取 WorkflowID，不存在返回空字符串。
// 对齐 Python: hasattr(session, "workflow_id") 检测。
func getWorkflowID(session CheckpointerSession) string {
	if ws, ok := session.(WorkflowIDProvider); ok {
		return ws.WorkflowID()
	}
	return ""
}
