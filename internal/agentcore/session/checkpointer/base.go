package checkpointer

import (
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
)

// ──────────────────────────── 结构体 ────────────────────────────

// 接口类型别名，统一指向 interfaces 包定义。
// 外部代码仍可通过 checkpointer.Checkpointer 等引用，零破坏性迁移。
type (
	// Checkpointer 检查点器接口，定义会话状态持久化的生命周期钩子。
	// 对应 Python: openjiuwen/core/session/checkpointer/base.py (Checkpointer)
	Checkpointer = interfaces.Checkpointer
	// Storage 状态存储接口，负责单个实体的状态保存/恢复/清除。
	// 对应 Python: openjiuwen/core/session/checkpointer/base.py (Storage)
	Storage = interfaces.Storage
	// WorkflowIDProvider 提供 WorkflowID 的接口（通过类型断言获取）。
	// 对齐 Python: hasattr(session, "workflow_id") 检测。
	WorkflowIDProvider = interfaces.WorkflowIDProvider
	// ParentProvider 提供 Parent 的接口（通过类型断言获取）。
	// 对齐 Python: isinstance(session.parent(), AgentSession) 检测。
	ParentProvider = interfaces.ParentProvider
	// CheckpointerConfigProvider 提供 GetEnv 方法的接口（通过类型断言获取）。
	// ⤵️ 5.12 回填：Config() 返回 SessionConfig 后此接口可移除。
	CheckpointerConfigProvider = interfaces.CheckpointerConfigProvider
	// AgentIDProvider 提供 Agent ID 的接口（通过类型断言获取）。
	// 对齐 Python: hasattr(session, "agent_id") 检测。
	AgentIDProvider = interfaces.AgentIDProvider
	// TeamIDProvider 提供 Team ID 的接口（通过类型断言获取）。
	// 对齐 Python: hasattr(session, "team_id") 检测。
	TeamIDProvider = interfaces.TeamIDProvider
)

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
func GetThreadID(session interfaces.BaseSession) string {
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
func GetAgentID(session interfaces.BaseSession) string {
	if provider, ok := session.(AgentIDProvider); ok {
		if id := provider.AgentID(); id != "" {
			return id
		}
	}
	return "Na"
}

// GetTeamID 类型断言获取 Team ID，不存在返回 "Na"。
// 对应 Python: session.team_id() if hasattr(session, "team_id") else "Na"
func GetTeamID(session interfaces.BaseSession) string {
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
func GetConfigEnv(session interfaces.BaseSession, key string, defaultValue ...any) (any, bool) {
	if cfg, ok := session.Config().(CheckpointerConfigProvider); ok {
		return cfg.GetEnv(key, defaultValue...), true
	}
	return nil, false
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getWorkflowID 从 session 获取 WorkflowID，不存在返回空字符串。
// 对齐 Python: hasattr(session, "workflow_id") 检测。
func getWorkflowID(session interfaces.BaseSession) string {
	if ws, ok := session.(WorkflowIDProvider); ok {
		return ws.WorkflowID()
	}
	return ""
}
