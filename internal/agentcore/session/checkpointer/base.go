package checkpointer

import (
	"context"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/graph/pregel"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/constants"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

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
// Python: Checkpointer.get_thread_id()（@staticmethod）
func GetThreadID(session interfaces.InnerSession) string {
	return session.SessionID() + ":" + getWorkflowID(session)
}

// BuildKey 用 ":" 连接各部分，构建存储键。
// Python: build_key(*parts)
func BuildKey(parts ...string) string {
	return strings.Join(parts, ":")
}

// BuildKeyWithNamespace 构建带命名空间的存储键：session:namespace:entity:suffixes。
// Python: build_key_with_namespace(session_id, namespace, entity_id, *suffixes)
func BuildKeyWithNamespace(sessionID, namespace, entityID string, suffixes ...string) string {
	parts := []string{sessionID, namespace, entityID}
	parts = append(parts, suffixes...)
	return BuildKey(parts...)
}

// GetAgentID 类型断言获取 Agent ID，不存在返回 "Na"。
// Python: session.agent_id() if hasattr(session, "agent_id") else "Na"
func GetAgentID(session interfaces.InnerSession) string {
	if provider, ok := session.(interfaces.AgentIDProvider); ok {
		if id := provider.AgentID(); id != "" {
			return id
		}
	}
	return "Na"
}

// GetTeamID 类型断言获取 Team ID，不存在返回 "Na"。
// Python: session.team_id() if hasattr(session, "team_id") else "Na"
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

// ValidateInteractiveResume InteractiveInput 恢复的共享前置校验
// （Python base.py:23-93，修复 6 的三后端统一实现；Go 侧 InMemory/Persistence
// 双后端经本函数统一接入，Redis 后端 Python 有 Go 未移植——见偏差注记）。
//
// 交互恢复（InteractiveInput）必须有可恢复的 checkpoint。此前快照缺失时
// recover() 静默返回，图从 start 节点全新起跑，在首个节点因拿不到用户输入
// 以难以定位的校验错误崩溃，并连带 error 分支 release 删除会话数据。这里
// 显式校验并抛错，把静默降级变成可诊断错误：
//  1. io 侧 workflow state 必须存在；
//  2. 图侧 graph checkpoint 必须存在且可续跑（GraphState.IsResumable()，
//     与 PregelLoop 的 isResume 同口径），否则引擎仍会全新起跑。
//
// 单组件直调（execute_single_component，SINGLE_COMPONENT_EXECUTION env 标记）
// 不经过 Pregel 图引擎，中断状态只存 io 侧、图侧快照本就不存在，
// io 侧校验通过即放行。
//
// workflowStore: io 侧工作流状态存储；graphStore: 图侧快照存储（pregel.Store）；
// storageType: 后端标识（persistence/in_memory），仅用于日志。
func ValidateInteractiveResume(
	ctx context.Context,
	session interfaces.InnerSession,
	workflowStore interfaces.Storage,
	graphStore pregel.Store,
	storageType string,
) error {
	sessionID := session.SessionID()
	workflowID := getWorkflowID(session)
	exists, err := workflowStore.Exists(ctx, session)
	if err != nil {
		return err
	}
	if !exists {
		logger.Error(logComponent).
			Str("session_id", sessionID).
			Str("workflow_id", workflowID).
			Str("event_type", "CHECKPOINT_ERROR").
			Str("operation", "pre_execute").
			Str("storage_type", storageType).
			Msg("Interactive resume rejected: no workflow checkpoint")
		return BuildErrorWithWorkflow(exception.StatusCheckpointerPreWorkflowExecutionError,
			workflowID,
			fmt.Sprintf("interactive resume requested but no workflow checkpoint exists for session %s", sessionID))
	}
	// Python base.py:73-76：单组件直调不经过图引擎，io 侧校验通过即放行。
	if val, ok := GetConfigEnv(session, constants.SingleComponentExecutionEnvKey, false); ok {
		if flag, isBool := val.(bool); isBool && flag {
			return nil
		}
	}
	// Python base.py:78-80：完整图执行的中断恢复必须有图侧快照（否则引擎全新起跑）。
	graphState, err := graphStore.Get(ctx, sessionID, workflowID)
	if err != nil {
		return err
	}
	if graphState != nil && graphState.IsResumable() {
		return nil
	}
	logger.Error(logComponent).
		Str("session_id", sessionID).
		Str("workflow_id", workflowID).
		Str("event_type", "CHECKPOINT_ERROR").
		Str("operation", "pre_execute").
		Str("storage_type", storageType).
		Msg("Interactive resume rejected: graph checkpoint not resumable")
	return BuildErrorWithWorkflow(exception.StatusCheckpointerPreWorkflowExecutionError,
		workflowID,
		fmt.Sprintf("interactive resume requested but graph checkpoint is missing or empty for session %s", sessionID))
}

// BuildErrorWithWorkflow 构建带 workflow 参数的检查点错误（Python build_error(workflow=...)）。
func BuildErrorWithWorkflow(status exception.StatusCode, workflowID, reason string) *exception.BaseError {
	return exception.BuildError(status,
		exception.WithParam("workflow", workflowID),
		exception.WithParam("reason", reason))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getWorkflowID 从 session 获取 WorkflowID，不存在返回空字符串。
// Python: hasattr(session, "workflow_id") 检测。
func getWorkflowID(session interfaces.InnerSession) string {
	if ws, ok := session.(interfaces.WorkflowIDProvider); ok {
		return ws.WorkflowID()
	}
	return ""
}

// processInteractiveInputs 处理交互输入并更新工作流状态。
// Python: _process_interactive_inputs(session, inputs)。
// 提取为公共函数，InMemory 和 Persistence 版本共用，消除代码重复（CP-25）。
func processInteractiveInputs(session interfaces.InnerSession, inputs *interaction.InteractiveInput) {
	// Python: if inputs.raw_inputs is not None → update_and_commit_workflow_state
	if inputs.RawInputs != nil {
		if wfState, ok := session.State().(state.WorkflowState); ok && wfState != nil {
			wfState.UpdateAndCommitWorkflowState(map[string]any{constants.InteractiveInputKey: inputs.RawInputs})
		}
		return
	}

	// Python: if not inputs.user_inputs: return
	if len(inputs.UserInputs) == 0 {
		return
	}

	// Python: for node_id, value → NodeSession(session, node_id) → append INTERACTIVE_INPUT
	// 不导入 internal 包（会循环依赖），用 WorkflowState.CreateNodeState 等价替代。
	wfState, ok := session.State().(state.WorkflowState)
	if !ok || wfState == nil {
		logger.Warn(logComponent).
			Str("session_id", session.SessionID()).
			Msg("session.State() is not WorkflowState, skipping user_inputs handling")
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
