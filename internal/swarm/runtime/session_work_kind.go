package runtime

import (
	"encoding/json"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	sessionpkg "github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	swarmsession "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// 枚举复用 sessionpkg.SessionWorkKind（12 成员，与 Python 逐一相同），不新建。
// spec DEC-8。
//
// 本文件复用同包已有的能力，不重复定义：
//   - chatTurnRequestMethods（request.go）—— Python request.py CHAT_TURN_METHODS
//   - sessionMessageInternalKey（request.go）—— Python session_message.SESSION_MESSAGE_INTERNAL_KEY
//   - ResolveAgentRequestMode（request.go）—— Python request.py resolve_agent_request_mode

// ──────────────────────────── 导出函数 ────────────────────────────

// SessionWorkKind 在 Runtime 边界给一次会话工作分类。
// Python: runtime/service.py AgentRuntime.session_work_kind（:3868-3921）
//
// 返回 nil 表示"不归 Runtime 管"，调用方走门面 SessionManager 队列。
// 十个分支的顺序与上游逐条对应，不得调整（短路顺序即语义边界）。
func SessionWorkKind(request *schema.AgentRequest, background bool) *sessionpkg.SessionWorkKind {
	// 前提：有 session_id
	if request == nil || request.SessionID == nil || strings.TrimSpace(*request.SessionID) == "" {
		return nil
	}
	params := parseWorkKindParams(request)

	// 步骤 1：团队判定优先（team 模式走独立三类）
	if requestTargetsTeam(request, params) {
		if !isChatTurnMethod(request.ReqMethod) {
			return nil
		}
		if teamBackgroundTrigger(params) != "" {
			return kindPtr(sessionpkg.SessionWorkKindTeamBackground)
		}
		if IsInterruptResumeRequest(params) {
			return kindPtr(sessionpkg.SessionWorkKindTeamControl)
		}
		return kindPtr(sessionpkg.SessionWorkKindTeamRequest)
	}

	// 步骤 2：后台分支（跨会话消息后台投递）
	if background {
		if isChatTurnMethod(request.ReqMethod) &&
			isSessionMessageParams(params) && isSingleAgentSessionMode(params) {
			return kindPtr(sessionpkg.SessionWorkKindSessionMessage)
		}
		return nil
	}

	// 步骤 3：单 agent 会话模式门卫（code 模式等不归 Runtime）
	if !isSingleAgentSessionMode(params) {
		return nil
	}

	// 步骤 4：中断续答
	if IsInterruptResumeRequest(params) {
		return kindPtr(sessionpkg.SessionWorkKindControlInput)
	}

	// 步骤 5：目标命令（COMMAND_GOAL 按 action 分流）
	if request.ReqMethod == schema.ReqMethodCommandGoal {
		action := strings.ToLower(strings.TrimSpace(stringParam(params, "action")))
		if action == "" {
			action = "get"
		}
		if action == "set" || action == "resume" {
			return kindPtr(sessionpkg.SessionWorkKindGoalStream)
		}
		return kindPtr(sessionpkg.SessionWorkKindGoalControl)
	}

	// 步骤 6：非聊天轮次不归 Runtime
	if !isChatTurnMethod(request.ReqMethod) {
		return nil
	}

	// 步骤 7：目标挂载
	if attachGoal, _ := params["attach_goal"].(bool); attachGoal {
		return kindPtr(sessionpkg.SessionWorkKindGoalAttach)
	}

	// 步骤 8：补充输入
	if ResolveSessionInputMode(params) != nil {
		return kindPtr(sessionpkg.SessionWorkKindSessionInput)
	}

	// 步骤 9：兜底按流式与否分流
	if request.IsStream {
		return kindPtr(sessionpkg.SessionWorkKindChatStream)
	}
	return kindPtr(sessionpkg.SessionWorkKindChatUnary)
}

// UsesSessionRuntime 这次执行是否由 Session Runtime 持有调度。
// Python: runtime/service.py uses_session_runtime（:3843）
//
// 为真时调用方走 ExecuteMessage（协调器调度），为假时走 ProcessMessage（门面队列）。
// 团队与 goal 类请求同样为真——它们都由 Runtime 协调器持有执行与代次。
//
// 历史上的同名函数曾判「是否占用会话流槽」（team 返回 false），与上游语义相反
// 且零生产调用方，已按规则 8 删除。
func UsesSessionRuntime(request *schema.AgentRequest, background bool) bool {
	return SessionWorkKind(request, background) != nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// kindPtr 取枚举指针。
func kindPtr(kind sessionpkg.SessionWorkKind) *sessionpkg.SessionWorkKind {
	return &kind
}

// parseWorkKindParams 解析请求参数（解析失败或空按空 map）。
// Python: request.params if isinstance(request.params, dict) else {}
func parseWorkKindParams(request *schema.AgentRequest) map[string]any {
	if request == nil || len(request.Params) == 0 {
		return map[string]any{}
	}
	out := map[string]any{}
	if err := json.Unmarshal(request.Params, &out); err != nil {
		return map[string]any{}
	}
	return out
}

// stringParam 取字符串参数（非字符串返回空串）。
func stringParam(params map[string]any, key string) string {
	value, _ := params[key].(string)
	return value
}

// isChatTurnMethod 是否为聊天轮次方法。
// Python: request.req_method in CHAT_TURN_METHODS（chat.send / chat.resume / chat.answer）
func isChatTurnMethod(method schema.ReqMethod) bool {
	_, ok := chatTurnRequestMethods[string(method)]
	return ok
}

// requestTargetsTeam 解析团队准入：参数无 mode 时回退到会话元数据。
// Python: runtime/service.py _request_targets_team（:3753）
//
// 回退理由：请求没带 mode 时，只能看会话存下来的是什么模式，
// 否则同一个会话的后续请求会被判成非团队。
func requestTargetsTeam(request *schema.AgentRequest, params map[string]any) bool {
	rawMode := stringParam(params, "mode")
	workMode := stringParam(params, "work_mode")
	if strings.TrimSpace(rawMode) == "" && request.SessionID != nil {
		sessionID := strings.TrimSpace(*request.SessionID)
		if sessionID != "" {
			metadata := swarmsession.GetSessionMetadata(sessionID)
			if metadata == nil {
				logger.Warn(logComponent).Str("session_id", sessionID).
					Msg("Runtime admission could not read session metadata")
			} else {
				if mode, ok := metadata["mode"].(string); ok {
					rawMode = mode
				}
				if stored, ok := metadata["work_mode"].(string); ok && stored != "" {
					workMode = stored
				}
			}
		}
	}
	_, _, canonical := ResolveAgentRequestMode(rawMode, workMode)
	return schema.IsTeamMode(canonical)
}

// isSingleAgentSessionMode 是否为单 agent 会话模式。
// Python: runtime/service.py _is_single_agent_session_mode
func isSingleAgentSessionMode(params map[string]any) bool {
	_, _, canonical := ResolveAgentRequestMode(
		stringParam(params, "mode"), stringParam(params, "work_mode"))
	return schema.IsSingleAgentMode(canonical)
}

// isSessionMessageParams 参数里是否带跨会话消息载荷。
// Python: isinstance(params.get(SESSION_MESSAGE_INTERNAL_KEY), dict)
func isSessionMessageParams(params map[string]any) bool {
	_, ok := params[sessionMessageInternalKey].(map[string]any)
	return ok
}

// teamBackgroundTrigger 团队自动化触发类型（heartbeat / cron / 空串）。
// Python: runtime/service.py _team_background_trigger
//
// automation 优先取 params，其次取 metadata；cron 同样两处都看。
func teamBackgroundTrigger(params map[string]any) string {
	if automation, ok := params["automation"].(map[string]any); ok {
		if automation["kind"] == "heartbeat" {
			return "heartbeat"
		}
	}
	if _, ok := params["cron"].(map[string]any); ok {
		return "cron"
	}
	return ""
}
