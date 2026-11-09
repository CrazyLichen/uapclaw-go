package adapter

import (
	"context"
	"errors"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/goal"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
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
