package events

// ──────────────────────────── 结构体 ────────────────────────────

// PlanApprovalEvent 计划审批事件。
// Python: PlanApprovalEvent
type PlanApprovalEvent struct {
	BaseEventMessage
	// Approved 是否批准
	Approved bool
}

// ToolApprovalResultEvent 工具调用审批结果事件。
// Python: ToolApprovalResultEvent
type ToolApprovalResultEvent struct {
	BaseEventMessage
	// ToolCallID 被中断的工具调用 ID
	ToolCallID string
	// Approved 是否批准
	Approved bool
	// Feedback Leader 反馈
	Feedback string
	// AutoConfirm 是否自动确认后续同名工具调用
	AutoConfirm bool
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// EventTypeName 返回计划审批事件类型名。
func (e PlanApprovalEvent) EventTypeName() string { return TeamEventPlanApproval }

// ToPayload 转换为事件载荷。
func (e PlanApprovalEvent) ToPayload() map[string]any {
	return map[string]any{"team_name": e.TeamName, "member_name": e.MemberName, "approved": e.Approved}
}

// EventTypeName 返回工具审批结果事件类型名。
func (e ToolApprovalResultEvent) EventTypeName() string { return TeamEventToolApprovalResult }

// ToPayload 转换为事件载荷。
func (e ToolApprovalResultEvent) ToPayload() map[string]any {
	return map[string]any{"team_name": e.TeamName, "member_name": e.MemberName, "tool_call_id": e.ToolCallID, "approved": e.Approved, "feedback": e.Feedback, "auto_confirm": e.AutoConfirm}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
