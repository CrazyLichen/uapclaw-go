package tools

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// 事件类型常量（对齐 Python: TeamEvent，与 schema 包 TeamEvent 常量等价）
const (
	eventTaskCreated      = "task_created"
	eventTaskClaimed      = "task_claimed"
	eventTaskCompleted    = "task_completed"
	eventTaskCancelled    = "task_cancelled"
	eventTaskUpdated      = "task_updated"
	eventTaskUnblocked    = "task_unblocked"
	eventTaskListDrained  = "task_list_drained"
	eventTaskPlanRequest  = "task_plan_request"
	eventTaskPlanResponse = "task_plan_response"
	eventMessage          = "message"
	eventBroadcast        = "broadcast"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildTopic 构建 topic 字符串。
// 对齐 Python: TeamTopic.build(session_id, team_name)
func buildTopic(sessionID, teamName, topic string) string {
	return "session:" + sessionID + ":team:" + teamName + ":" + topic
}

// buildTaskTopic 构建任务事件 topic。
func buildTaskTopic(sessionID, teamName string) string {
	return buildTopic(sessionID, teamName, "task")
}

// buildMessageTopic 构建消息事件 topic。
func buildMessageTopic(sessionID, teamName string) string {
	return buildTopic(sessionID, teamName, "message")
}
