package context_engineer

import (
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	sessstate "github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// sessionRuntimeAttr 会话运行时属性键
	// 对齐 Python: _SESSION_RUNTIME_ATTR = "_deep_agent_runtime_state"
	sessionRuntimeAttr = "_deep_agent_runtime_state"

	// sessionStateKey 会话状态持久化键
	// 对齐 Python: _SESSION_STATE_KEY = "deep_agent_state"
	sessionStateKey = "deep_agent_state"
)

// RefreshTaskStateRuntime 同步运行时状态到会话顶层状态。
//
// 从 session 的 _deep_agent_runtime_state 属性中读取 DeepAgentState，
// 序列化为字典后回写到 session 的顶层状态（task_state/iteration/pending_follow_ups/plan_mode）。
//
// 对齐 Python: ContextProcessorRail._refresh_task_state_runtime(ctx)
// 调用时机：beforeModelCall / afterModelCall / afterToolCall / onModelException
// ──────────────────────────── 导出函数 ────────────────────────────

func RefreshTaskStateRuntime(ctx *sainterfaces.AgentCallbackContext) {
	sess := ctx.Session()
	if sess == nil {
		return
	}

	// 1. 尝试从运行时属性获取 DeepAgentState
	var serialized map[string]any

	runtimeData, err := sess.GetState(sessstate.StringKey(sessionRuntimeAttr))
	if err == nil && runtimeData != nil {
		if runtimeState, ok := runtimeData.(*hschema.DeepAgentState); ok {
			serialized = runtimeState.ToSessionDict()
		}
	}

	// 2. 回退：从持久化状态获取
	if serialized == nil {
		persistedData, err2 := sess.GetState(sessstate.StringKey(sessionStateKey))
		if err2 == nil && persistedData != nil {
			if persisted, ok := persistedData.(map[string]any); ok {
				serialized = persisted
			}
		}
	}

	if len(serialized) == 0 {
		return
	}

	// 3. 提取 iteration
	iteration := 0
	if stopCondState, ok := serialized["stop_condition_state"].(map[string]any); ok {
		iteration = toInt(stopCondState["iteration"])
	} else {
		iteration = toInt(serialized["iteration"])
	}

	// 4. 提取 pending_follow_ups
	var pendingFollowUps []string
	if pfu, ok := serialized["pending_follow_ups"]; ok {
		pendingFollowUps = toStringSlice(pfu)
	}

	// 5. 提取 plan_mode
	var planMode interface{}
	if pm, ok := serialized["plan_mode"]; ok {
		planMode = pm
	}

	// 6. 回写到 session 顶层状态
	sess.UpdateState(map[string]any{
		"task_state":         serialized,
		"iteration":          iteration,
		"pending_follow_ups": pendingFollowUps,
		"plan_mode":          planMode,
	})
}

// toInt 将 any 值转换为 int，对齐 Python: int(value or 0)
// ──────────────────────────── 非导出函数 ────────────────────────────

func toInt(val interface{}) int {
	switch v := val.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

// toStringSlice 将 any 值转换为 []string，对齐 Python: serialized.get("pending_follow_ups", [])
func toStringSlice(val interface{}) []string {
	switch v := val.(type) {
	case []string:
		return v
	case []interface{}:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	default:
		return nil
	}
}

// 确保 SessionFacade 接口满足使用需求
var _ sessioninterfaces.SessionFacade = nil
