package adapter

import (
	"context"
	"errors"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// sessionMessageInternalKey 跨会话内部消息的载荷键。
	// Python: gateway/common/session_message.py:7 SESSION_MESSAGE_INTERNAL_KEY
	sessionMessageInternalKey = "_jiuwenswarm_cross_session"
	// reqMethodCommandGoal goal 控制指令的请求方法。
	// Python: ReqMethod.COMMAND_GOAL
	reqMethodCommandGoal = "command.goal"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// personDroveTurn 该轮是否由用户发起（发消息或回答问题）。
// 后台推荐、团队成员、心跳、cron 与其它会话发起的轮次都不算。
// Python: interface.py:467-479 _person_drove_turn
func personDroveTurn(
	params map[string]any,
	channelID, requestMethod string,
	metadata map[string]any,
	isTeamMode bool,
) bool {
	// Python: 1. if is_team_mode or _session_message_owns_completion(request): return False
	if isTeamMode || sessionMessageOwnsCompletion(params) {
		return false
	}
	// Python: 2. if is_interrupt_resume_payload(request.params): return True
	if schema.IsInterruptResumePayload(params) {
		return true
	}
	// Python: 3. return is_external_user_authored_dispatch(...)
	return isExternalUserAuthoredDispatch(params, channelID, requestMethod, metadata)
}

// sessionMessageOwnsCompletion 该轮的终态由 AgentWebSocketServer 记录（跨会话消息）。
// Python: interface.py:273-279 _session_message_owns_completion
func sessionMessageOwnsCompletion(params map[string]any) bool {
	if params == nil {
		return false
	}
	_, ok := params[sessionMessageInternalKey].(map[string]any)
	return ok
}

// isExternalUserAuthoredDispatch Host 入口是否证明该轮由外部用户发起。
// Python: interface.py:433-462 is_external_user_authored_dispatch
func isExternalUserAuthoredDispatch(
	params map[string]any, channelID, requestMethod string, metadata map[string]any,
) bool {
	// Python: 1. if not isinstance(params, dict): return False
	if params == nil {
		return false
	}
	// Python: 2. 逐个入口容器检查（合并 metadata 无法抹掉标记）。
	// 三个容器：params / metadata / params["metadata"]
	containers := []map[string]any{params, metadata}
	if nested, ok := params["metadata"].(map[string]any); ok {
		containers = append(containers, nested)
	}
	for _, container := range containers {
		if container == nil {
			continue
		}
		// Python: if isinstance(container.get(SESSION_MESSAGE_INTERNAL_KEY), dict): return False
		if _, ok := container[sessionMessageInternalKey].(map[string]any); ok {
			return false
		}
		// Python: automation.kind == "heartbeat" → return False
		if automation, ok := container["automation"].(map[string]any); ok {
			if kind, ok := automation["kind"].(string); ok &&
				strings.ToLower(strings.TrimSpace(kind)) == "heartbeat" {
				return false
			}
		}
	}
	// Python: 3. if params.get("log_as_user") is False: return False
	if logAsUser, ok := params["log_as_user"].(bool); ok && !logAsUser {
		return false
	}
	// Python: 4. if params.get("attach_goal") is True: return False
	if attachGoal, ok := params["attach_goal"].(bool); ok && attachGoal {
		return false
	}
	// Python: 5. if is_interrupt_resume_payload(params): return False
	if schema.IsInterruptResumePayload(params) {
		return false
	}
	// Python: 6. if request_method == ReqMethod.COMMAND_GOAL: return False
	if requestMethod == reqMethodCommandGoal {
		return false
	}
	// Python: 7. if channel_id.strip().lower() in {"cron", "heartbeat"}: return False
	switch strings.ToLower(strings.TrimSpace(channelID)) {
	case "cron", "heartbeat":
		return false
	}
	// Python: 8. return not str(params.get("source") or "").strip()
	source, _ := params["source"].(string)
	return strings.TrimSpace(source) == ""
}

// terminalStatusOf 把执行错误映射成 turn_ended 的终态字符串。
// Python: turn_ended(status=terminal_status)，上游取 history 终态
// （"success" / "cancelled" / 错误态）。
func terminalStatusOf(runErr error) string {
	if runErr == nil {
		return "success"
	}
	if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
		return "cancelled"
	}
	return "error"
}
