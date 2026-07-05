package checkpointer

import (
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/constants"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 枚举 ────────────────────────────

// 接口类型别名，统一指向 interfaces 包定义。
// 外部代码仍可通过 checkpointer.Checkpointer 引用，零破坏性迁移。

// ──────────────────────────── 枚举 ────────────────────────────

// Checkpointer 检查点器接口，定义会话状态持久化的生命周期钩子。
// 对应 Python: openjiuwen/core/session/checkpointer/base.py (Checkpointer)
// TODO: 考虑移除 reexport，让调用者直接使用 interfaces 包
type Checkpointer = interfaces.Checkpointer

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
func GetThreadID(session interfaces.InnerSession) string {
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
func GetAgentID(session interfaces.InnerSession) string {
	if provider, ok := session.(interfaces.AgentIDProvider); ok {
		if id := provider.AgentID(); id != "" {
			return id
		}
	}
	return "Na"
}

// GetTeamID 类型断言获取 Team ID，不存在返回 "Na"。
// 对应 Python: session.team_id() if hasattr(session, "team_id") else "Na"
func GetTeamID(session interfaces.InnerSession) string {
	if provider, ok := session.(interfaces.TeamIDProvider); ok {
		if id := provider.TeamID(); id != "" {
			return id
		}
	}
	return "Na"
}

// GetConfigEnv 从 session.Config() 获取环境变量值。
// 返回环境变量值和是否可用。
func GetConfigEnv(session interfaces.InnerSession, key string, defaultValue ...any) (any, bool) {
	cfg := session.Config()
	if cfg == nil {
		return nil, false
	}
	return cfg.GetEnv(key, defaultValue...), true
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getWorkflowID 从 session 获取 WorkflowID，不存在返回空字符串。
// 对齐 Python: hasattr(session, "workflow_id") 检测。
func getWorkflowID(session interfaces.InnerSession) string {
	if ws, ok := session.(interfaces.WorkflowIDProvider); ok {
		return ws.WorkflowID()
	}
	return ""
}

// processInteractiveInputs 处理交互输入并更新工作流状态。
// 对齐 Python: _process_interactive_inputs(session, inputs)。
// 提取为公共函数，InMemory 和 Persistence 版本共用，消除代码重复（CP-25）。
func processInteractiveInputs(session interfaces.InnerSession, inputs *interaction.InteractiveInput) {
	// 对齐 Python: if inputs.raw_inputs is not None → update_and_commit_workflow_state
	if inputs.RawInputs != nil {
		if wfState, ok := session.State().(state.WorkflowState); ok && wfState != nil {
			wfState.UpdateAndCommitWorkflowState(map[string]any{constants.InteractiveInputKey: inputs.RawInputs})
		}
		return
	}

	// 对齐 Python: if not inputs.user_inputs: return
	if len(inputs.UserInputs) == 0 {
		return
	}

	// 对齐 Python: for node_id, value → NodeSession(session, node_id) → append INTERACTIVE_INPUT
	// 不导入 internal 包（会循环依赖），用 WorkflowState.CreateNodeState 等价替代。
	wfState, ok := session.State().(state.WorkflowState)
	if !ok || wfState == nil {
		logger.Warn(logComponent).
			Str("session_id", session.SessionID()).
			Msg("session.State() 不是 WorkflowState，跳过 user_inputs 处理")
		return
	}

	for nodeID, value := range inputs.UserInputs {
		nodeState := wfState.CreateNodeState(nodeID, "")
		if list, ok := nodeState.Get(state.StringKey(constants.InteractiveInputKey)).([]any); ok {
			_ = nodeState.Update(map[string]any{constants.InteractiveInputKey: append(list, value)})
		} else {
			_ = nodeState.Update(map[string]any{constants.InteractiveInputKey: []any{value}})
		}
	}
	wfState.Commit()
}
