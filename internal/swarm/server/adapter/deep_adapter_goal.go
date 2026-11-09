package adapter

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/goal"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// dispatchGoalControl 把协议字段映射到独立的 Goal 能力方法。
// Python: interface_deep.py:17764-17930 _dispatch_goal_control
//
// 分流头（根 → child）由 deep_adapter_sessions.go 的 DispatchGoalControl 承担，
// 本方法只跑在 child 上。
func (d *DeepAdapter) dispatchGoalControl(
	ctx context.Context, sessionID, action, objective string,
	overwriteConfirmed bool, tokenBudget, maxAttempts int,
) (map[string]any, error) {
	normalized := strings.ToLower(strings.TrimSpace(action))
	manager := d.goalManager()
	if manager == nil {
		return map[string]any{
			"result_type": goalResultError,
			"action":      normalized,
			"error_code":  goalErrManagerNotStarted,
			"error":       "goal manager is not started",
		}, nil
	}

	switch normalized {
	case "get":
		// 只读查询走无锁 peek：长跑/卡住的 goal 轮持着交互控制锁，
		// await get() 会串行在同一把锁上，把 command.goal get 拖到超时。
		// Python: interface_deep.py:17822-17829
		return d.goalControlResult(normalized, manager.Peek()), nil
	case "set":
		record, err := manager.Set(ctx, objective, goal.SetOptions{
			OverwriteConfirmed: overwriteConfirmed,
			TokenBudget:        optionalPositive(tokenBudget),
			MaxAttempts:        optionalPositive(maxAttempts),
		})
		if err != nil {
			return d.goalErrorResult(normalized, objective, err), nil
		}
		return d.goalControlResult(normalized, record), nil
	case "pause":
		before := manager.Get()
		if before == nil {
			return map[string]any{
				"result_type": goalResultError, "action": normalized,
				"error_code": goalErrNoGoal,
				"error":      "No goal in this session; cannot pause.",
				"goal":       nil,
			}, nil
		}
		beforeStatus := before.Status
		record, err := manager.Pause(ctx)
		if err != nil {
			return d.goalErrorResult(normalized, objective, err), nil
		}
		payload := goalRecordPayload(record)
		if beforeStatus != goal.GoalStatusActive {
			return map[string]any{
				"result_type": goalResultError, "action": normalized,
				"error_code": goalErrInvalidState,
				"error":      "Goal is " + string(beforeStatus) + "; only active goals can be paused.",
				"goal":       payload,
			}, nil
		}
		return map[string]any{
			"result_type": goalResultControl, "action": normalized,
			"goal": payload, "output": "Goal paused.",
		}, nil
	case "resume":
		before := manager.Get()
		if before == nil {
			return map[string]any{
				"result_type": goalResultError, "action": normalized,
				"error_code": goalErrNoGoal,
				"error":      "No goal in this session; cannot resume.",
				"goal":       nil,
			}, nil
		}
		if before.Status == goal.GoalStatusActive {
			return map[string]any{
				"result_type": goalResultControl, "action": normalized,
				"goal": goalRecordPayload(before), "output": "Goal already active.",
			}, nil
		}
		if before.Status != goal.GoalStatusPaused && before.Status != goal.GoalStatusBlocked {
			return map[string]any{
				"result_type": goalResultError, "action": normalized,
				"error_code": goalErrInvalidState,
				"error": "Goal is " + string(before.Status) +
					"; only paused/blocked goals can be resumed.",
				"goal": goalRecordPayload(before),
			}, nil
		}
		record, err := manager.Resume(ctx)
		if err != nil {
			return d.goalErrorResult(normalized, objective, err), nil
		}
		return d.goalControlResult(normalized, record), nil
	case "clear":
		removed, err := manager.Clear(ctx)
		if err != nil {
			return d.goalErrorResult(normalized, objective, err), nil
		}
		if removed == nil {
			return map[string]any{
				"result_type": goalResultError, "action": normalized,
				"error_code": goalErrNoGoal,
				"error":      "No goal in this session; nothing to clear.",
				"goal":       nil, "cleared_goal": nil,
			}, nil
		}
		return map[string]any{
			"result_type": goalResultControl, "action": normalized,
			"goal": nil, "cleared_goal": goalRecordPayload(removed),
			"output": "Goal cleared.",
		}, nil
	default:
		return map[string]any{
			"result_type": goalResultError, "action": normalized,
			"error_code": goalErrInvalidAction,
			"error":      "unsupported goal action: " + action,
		}, nil
	}
}

// goalManager 返回本适配器会话的 GoalManager（未 start_interaction 时为 nil）。
// Python: interface_deep.py:17739 _get_goal_manager
func (d *DeepAdapter) goalManager() *goal.GoalManager {
	if d.instance == nil {
		return nil
	}
	return d.instance.GoalManager()
}

// goalControlResult 组装成功结果；set/resume 且目标 ACTIVE 时进入执行流。
// Python: interface_deep.py:17904-17930
func (d *DeepAdapter) goalControlResult(action string, record *goal.GoalRecord) map[string]any {
	payload := goalRecordPayload(record)
	active := record != nil && record.Status == goal.GoalStatusActive
	resultType := goalResultControl
	if (action == "set" || action == "resume") && active {
		resultType = goalResultStream
	}
	return map[string]any{
		"result_type": resultType,
		"action":      action,
		"goal":        payload,
		"output":      formatGoalControlMessage(action, payload),
	}
}

// goalErrorResult 把 GoalOperationError 映射为协议结果。
// Python: interface_deep.py:17895-17912
func (d *DeepAdapter) goalErrorResult(action, objective string, err error) map[string]any {
	var opErr *goal.GoalOperationError
	if errors.As(err, &opErr) {
		if opErr.Code == goalErrAlreadyExists {
			return map[string]any{
				"result_type": goalResultConfirmRequired, "action": action,
				"error_code": opErr.Code, "error": opErr.Error(),
				"existing_goal":       goalRecordPayload(opErr.Goal),
				"requested_objective": objective,
			}
		}
		return map[string]any{
			"result_type": goalResultError, "action": action,
			"error_code": opErr.Code, "error": opErr.Error(),
			"goal": goalRecordPayload(opErr.Goal),
		}
	}
	logger.Error(logComponent).Err(err).
		Str("event_type", "GOAL_OPERATION_ERROR").
		Str("action", action).
		Msg("goal operation failed")
	return map[string]any{
		"result_type": goalResultError, "action": action,
		"error_code": "goal_operation_failed", "error": err.Error(),
	}
}

// goalRecordPayload 把 GoalRecord 转成协议 payload（nil 安全）。
// Python: interface_deep.py:17554-17559
func goalRecordPayload(record *goal.GoalRecord) map[string]any {
	if record == nil {
		return nil
	}
	return record.ToDict()
}

// formatGoalControlMessage 生成控制类响应的人类可读文案。
// Python: interface_deep.py:17561-17572（文案逐字照抄）
func formatGoalControlMessage(action string, payload map[string]any) string {
	switch action {
	case "get":
		if payload == nil {
			return "No goal in this session."
		}
		objective, _ := payload["objective"].(string)
		return "Goal: " + objective
	case "pause":
		if payload != nil {
			return "Goal paused."
		}
		return "No goal in this session."
	case "clear":
		if payload == nil {
			return "Goal cleared."
		}
		return "Goal was not cleared."
	case "resume":
		if payload != nil {
			return "Goal resumed."
		}
		return "No goal in this session."
	default:
		if payload != nil {
			return "Goal set."
		}
		return "Goal was not set."
	}
}

// optionalPositive 正整数才转指针（<=0 视为未指定）。
// Python: _structured_goal_op_from_request 只接受 int 且 >0 的预算字段
func optionalPositive(value int) *int {
	if value <= 0 {
		return nil
	}
	return &value
}

// parseGoalSlashIntent 把 "/goal ..." 解析成 action 字典，不触碰 GoalManager。
// Python: interface_deep.py:15739-15757
func parseGoalSlashIntent(query string) map[string]any {
	text := strings.TrimSpace(query)
	if !strings.HasPrefix(text, "/goal") {
		return nil
	}
	args := strings.TrimSpace(text[5:])
	if args == "" {
		return map[string]any{"action": "get"}
	}
	lower := strings.ToLower(args)
	if lower == "pause" || lower == "resume" || lower == "clear" {
		return map[string]any{"action": lower}
	}
	if strings.HasPrefix(lower, "set ") {
		return map[string]any{"action": "set", "objective": strings.TrimSpace(args[4:])}
	}
	if lower == "set" {
		return map[string]any{"action": "set", "objective": ""}
	}
	// get / stop 不是用户命令词，保留为 "/goal <objective>" 形式的正文
	return map[string]any{"action": "set", "objective": args}
}

// shouldParseTUIGoalSlash 是否需要把聊天文本 "/goal ..." 当意图解析。
// Python: interface_deep.py:15724-15737 —— 仅 TUI 通道，且无结构化 op / 非 attach。
func shouldParseTUIGoalSlash(
	pendingGoalOp map[string]any, attachGoalRequest bool, channelID string, query any,
) bool {
	if pendingGoalOp != nil || attachGoalRequest {
		return false
	}
	if strings.ToLower(strings.TrimSpace(channelID)) != "tui" {
		return false
	}
	_, isString := query.(string)
	return isString
}

// structuredGoalOpFromRequest 把流式 command.goal 的 set/resume 映射成
// attach→control 两段路径的 pending op。
// Python: interface_deep.py:16251-16281
//
// 普通聊天文本 "/goal set ..." 不走这里——只有显式 command.goal 方法才映射。
func structuredGoalOpFromRequest(req *schema.AgentRequest) map[string]any {
	if req == nil || req.ReqMethod != schema.ReqMethodCommandGoal {
		return nil
	}
	raw := parseParams(req.Params)
	action := strings.ToLower(strings.TrimSpace(paramsString(raw, "action", "")))
	if action == "" {
		action = "get"
	}
	if action != "set" && action != "resume" {
		return nil
	}
	op := map[string]any{"action": action}
	if action != "set" {
		return op
	}
	objective, _ := raw["objective"].(string)
	op["objective"] = objective
	overwrite, _ := raw["overwrite_confirmed"].(bool)
	op["overwrite_confirmed"] = overwrite
	for _, key := range []string{"token_budget", "max_attempts"} {
		if value, ok := positiveIntField(raw, key); ok {
			op[key] = value
		}
	}
	return op
}

// handleGoalSlashCommand 把 "/goal ..." 翻译成能力调用。
// Python: interface_deep.py:17949-17977
//
// 返回 nil 表示不是 goal 斜杠文本，调用方继续走其它斜杠分发。
func (d *DeepAdapter) handleGoalSlashCommand(
	ctx context.Context, query, sessionID string,
) map[string]any {
	intent := parseGoalSlashIntent(query)
	if intent == nil {
		return nil
	}
	action, _ := intent["action"].(string)
	objective, _ := intent["objective"].(string)
	out, err := d.DispatchGoalControl(ctx, sessionID, action, objective, false, 0, 0)
	if err != nil {
		logger.Error(logComponent).Err(err).
			Str("event_type", "GOAL_SLASH_FAILED").
			Str("session_id", sessionID).
			Msg("goal slash command failed")
		return map[string]any{
			"result_type": goalResultError,
			"error_code":  "goal_operation_failed",
			"error":       err.Error(),
		}
	}
	return out
}

// positiveIntField 取正整数参数（接受 int / 纯数字字符串，拒绝 bool）。
// Python: isinstance(value, int) 或 str.strip().lstrip("-").isdigit()
func positiveIntField(params map[string]any, key string) (int, bool) {
	value, ok := params[key]
	if !ok || value == nil {
		return 0, false
	}
	switch typed := value.(type) {
	case bool:
		return 0, false
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	case string:
		trimmed := strings.TrimSpace(typed)
		digits := strings.TrimPrefix(trimmed, "-")
		if digits == "" || strings.TrimLeft(digits, "0123456789") != "" {
			return 0, false
		}
		parsed, err := strconv.Atoi(trimmed)
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}
