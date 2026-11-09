package adapter

// ──────────────────────────── 常量 ────────────────────────────

const (
	// goalUpdatedEventType 目标记录变更事件类型。
	// Python: interface_deep.py:188 GOAL_UPDATED_EVENT_TYPE
	goalUpdatedEventType = "goal.updated"

	// executionErrorEventType 交互层执行错误事件类型。
	// Python: interface_deep.py:192 ERROR_EVENT_TYPE（InteractionEventType.EXECUTION_ERROR）
	executionErrorEventType = "execution.error"

	// goalSnapshotEventType 目标快照事件类型（上游 payload 字面量）。
	goalSnapshotEventType = "goal.snapshot"

	// goalConfirmRequiredEventType 目标覆盖确认事件类型（上游 payload 字面量）。
	goalConfirmRequiredEventType = "goal.confirm_required"

	// runtimeAcceptedEventType 请求已受理确认事件类型。
	// Python: interface_deep.py:19801
	runtimeAcceptedEventType = "runtime.accepted"

	// goalIntermediateField 降级后的中间 final 标记字段名。
	// Python: interface_deep.py:15635 adapted["goal_intermediate"] = True
	goalIntermediateField = "goal_intermediate"
)

const (
	// goalResultStream 结果类型：ACTIVE 目标已就绪，继续消费输出流。
	// Python: interface_deep.py:17903
	goalResultStream = "goal_stream"
	// goalResultControl 结果类型：纯控制类响应，不进入执行。
	// Python: interface_deep.py:17929
	goalResultControl = "goal_control"
	// goalResultError 结果类型：操作失败。
	goalResultError = "goal_error"
	// goalResultConfirmRequired 结果类型：已存在目标，需用户确认覆盖。
	// Python: interface_deep.py:17903
	goalResultConfirmRequired = "goal_confirm_required"
)

const (
	// goalErrManagerNotStarted 目标管理器尚未启动（会话未 start_interaction）。
	// Python: interface_deep.py:17815
	goalErrManagerNotStarted = "goal_manager_not_started"
	// goalErrNoGoal 会话内没有目标。
	// Python: interface_deep.py:17833
	goalErrNoGoal = "no_goal"
	// goalErrInvalidState 目标状态不允许该操作。
	// Python: interface_deep.py:17862
	goalErrInvalidState = "invalid_state"
	// goalErrInvalidAction 不支持的 action。
	// Python: interface_deep.py:17900
	goalErrInvalidAction = "invalid_action"
	// goalErrAlreadyExists 已存在目标且未确认覆盖。
	// Python: goal/manager.py GoalOperationError(code="already_exists")
	goalErrAlreadyExists = "already_exists"
)
