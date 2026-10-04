package tools

import (
	"fmt"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// MapResult 将 BuildTeamTool 执行结果转换为 LLM 可读文本。
// 对齐 Python: BuildTeamTool.map_result (tools/team_tools.py L230-240)
func (t *BuildTeamTool) MapResult(output map[string]any) string {
	success, _ := output["success"].(bool)
	if !success {
		if errVal, ok := output["error"]; ok {
			return fmt.Sprintf("%v", errVal)
		}
		return "Failed to build team"
	}
	d, _ := output["data"].(map[string]any)
	if d == nil {
		d = map[string]any{}
	}
	return fmt.Sprintf("Team created: team_name=%v display_name=%v leader_member_name=%v leader_display_name=%v hitt_enabled=%v",
		d["team_name"], d["display_name"], d["leader_member_name"], d["leader_display_name"], d["enable_hitt"])
}

// MapResult 将 CleanTeamTool 执行结果转换为 LLM 可读文本。
// 对齐 Python: CleanTeamTool.map_result (tools/team_tools.py L271-274)
func (t *CleanTeamTool) MapResult(output map[string]any) string {
	success, _ := output["success"].(bool)
	if !success {
		if errVal, ok := output["error"]; ok {
			return fmt.Sprintf("%v", errVal)
		}
		return "Failed to clean team"
	}
	d, _ := output["data"].(map[string]any)
	return fmt.Sprintf("Team cleaned: team_name=%v", d["team_name"])
}

// MapResult 将 SpawnMemberTool 执行结果转换为 LLM 可读文本。
// 对齐 Python: SpawnMemberTool.map_result (tools/team_tools.py L414-419)
func (t *SpawnMemberTool) MapResult(output map[string]any) string {
	success, _ := output["success"].(bool)
	if !success {
		if errVal, ok := output["error"]; ok {
			return fmt.Sprintf("%v", errVal)
		}
		return "Failed to spawn member"
	}
	d, _ := output["data"].(map[string]any)
	role, _ := d["role_type"].(string)
	if role == "" {
		role = "teammate"
	}
	return fmt.Sprintf("Member spawned: member_name=%v display_name=%v role=%v", d["member_name"], d["display_name"], role)
}

// MapResult 将 ShutdownMemberTool 执行结果转换为 LLM 可读文本。
// 对齐 Python: ShutdownMemberTool.map_result (tools/team_tools.py L458-461)
func (t *ShutdownMemberTool) MapResult(output map[string]any) string {
	success, _ := output["success"].(bool)
	if !success {
		if errVal, ok := output["error"]; ok {
			return fmt.Sprintf("%v", errVal)
		}
		return "Failed to shutdown member"
	}
	d, _ := output["data"].(map[string]any)
	return fmt.Sprintf("Member shutdown: member_name=%v", d["member_name"])
}

// MapResult 将 ApprovePlanTool 执行结果转换为 LLM 可读文本。
// 对齐 Python: ApprovePlanTool.map_result (tools/team_tools.py L506-511)
func (t *ApprovePlanTool) MapResult(output map[string]any) string {
	success, _ := output["success"].(bool)
	if !success {
		if errVal, ok := output["error"]; ok {
			return fmt.Sprintf("%v", errVal)
		}
		return "Failed to approve/reject plan"
	}
	d, _ := output["data"].(map[string]any)
	approved, _ := d["approved"].(bool)
	decision := "rejected"
	if approved {
		decision = "approved"
	}
	return fmt.Sprintf("Plan %s: plan_id=%v decision=%s", decision, d["plan_id"], decision)
}

// MapResult 将 ApproveToolCallTool 执行结果转换为 LLM 可读文本。
// 对齐 Python: ApproveToolCallTool.map_result (tools/team_tools.py L558-565)
func (t *ApproveToolCallTool) MapResult(output map[string]any) string {
	success, _ := output["success"].(bool)
	if !success {
		if errVal, ok := output["error"]; ok {
			return fmt.Sprintf("%v", errVal)
		}
		return "Failed to approve/reject tool call"
	}
	d, _ := output["data"].(map[string]any)
	approved, _ := d["approved"].(bool)
	decision := "rejected"
	if approved {
		decision = "approved"
	}
	return fmt.Sprintf("Tool call %s: tool_call_id=%v member_name=%v decision=%s",
		decision, d["tool_call_id"], d["member_name"], decision)
}

// MapResult 将 ListMembersTool 执行结果转换为 LLM 可读文本。
// 对齐 Python: ListMembersTool.map_result (tools/team_tools.py L588-597)
func (t *ListMembersTool) MapResult(output map[string]any) string {
	success, _ := output["success"].(bool)
	if !success {
		if errVal, ok := output["error"]; ok {
			return fmt.Sprintf("%v", errVal)
		}
		return "Failed to list members"
	}
	d, _ := output["data"].(map[string]any)
	members, _ := d["members"].([]any)
	if len(members) == 0 {
		return "No members"
	}
	lines := make([]string, 0, len(members))
	for _, m := range members {
		mm, _ := m.(map[string]any)
		lines = append(lines, fmt.Sprintf("member_name=%v display_name=%v status=%v",
			mm["member_name"], mm["display_name"], mm["status"]))
	}
	return strings.Join(lines, "\n")
}

// MapResult 将 TaskCreateTool 执行结果转换为 LLM 可读文本。
// 对齐 Python: TaskCreateTool.map_result (tools/team_tools.py L732-745)
func (t *TaskCreateTool) MapResult(output map[string]any) string {
	success, _ := output["success"].(bool)
	if !success {
		if errVal, ok := output["error"]; ok {
			return fmt.Sprintf("%v", errVal)
		}
		return "Operation failed"
	}
	d, _ := output["data"].(map[string]any)
	// 单任务路径
	if _, hasTaskID := d["task_id"]; hasTaskID {
		if _, hasTitle := d["title"]; hasTitle {
			return fmt.Sprintf("Task created: task_id=%v title=%v", d["task_id"], d["title"])
		}
	}
	// 批量路径
	tasks, _ := d["tasks"].([]any)
	skipped := d["skipped"]
	count := d["count"]
	lines := make([]string, 0, len(tasks)+2)
	for _, t := range tasks {
		tm, _ := t.(map[string]any)
		lines = append(lines, fmt.Sprintf("task_id=%v title=%v", tm["task_id"], tm["title"]))
	}
	lines = append(lines, fmt.Sprintf("Created %v, skipped %v", count, skipped))
	failures, _ := d["failures"].([]any)
	for _, f := range failures {
		fm, _ := f.(map[string]any)
		lines = append(lines, fmt.Sprintf("  - skipped %v: %v", fm["spec"], fm["reason"]))
	}
	return strings.Join(lines, "\n")
}

// MapResult 将 ViewTaskTool 执行结果转换为 LLM 可读文本。
// 对齐 Python: ViewTaskToolV2.map_result (tools/team_tools.py L803-834)
func (t *ViewTaskTool) MapResult(output map[string]any) string {
	success, _ := output["success"].(bool)
	if !success {
		if errVal, ok := output["error"]; ok {
			return fmt.Sprintf("%v", errVal)
		}
		return "Task not found"
	}
	d, _ := output["data"].(map[string]any)
	// Detail view（get action）——含 content 字段
	if _, hasContent := d["content"]; hasContent {
		lines := []string{
			fmt.Sprintf("Task #%v: %v", d["task_id"], d["title"]),
			fmt.Sprintf("Status: %v", d["status"]),
			fmt.Sprintf("Content: %v", d["content"]),
		}
		if assignee, _ := d["assignee"].(string); assignee != "" {
			lines = append(lines, fmt.Sprintf("Assignee: %s", assignee))
		}
		if blockedBy, _ := d["blocked_by"].([]any); len(blockedBy) > 0 {
			ids := make([]string, 0, len(blockedBy))
			for _, tid := range blockedBy {
				ids = append(ids, fmt.Sprintf("#%v", tid))
			}
			lines = append(lines, fmt.Sprintf("Blocked by: %s", strings.Join(ids, ", ")))
		}
		if blocks, _ := d["blocks"].([]any); len(blocks) > 0 {
			ids := make([]string, 0, len(blocks))
			for _, tid := range blocks {
				ids = append(ids, fmt.Sprintf("#%v", tid))
			}
			lines = append(lines, fmt.Sprintf("Blocks: %s", strings.Join(ids, ", ")))
		}
		return strings.Join(lines, "\n")
	}
	// 列表视图（list/claimable 动作）
	tasks, _ := d["tasks"].([]any)
	if len(tasks) == 0 {
		return "No tasks found"
	}
	lines := make([]string, 0, len(tasks))
	for _, t := range tasks {
		tm, _ := t.(map[string]any)
		parts := []string{fmt.Sprintf("#%v [%v] %v", tm["task_id"], tm["status"], tm["title"])}
		if assignee, _ := tm["assignee"].(string); assignee != "" {
			parts = append(parts, fmt.Sprintf("(%s)", assignee))
		}
		if blockedBy, _ := tm["blocked_by"].([]any); len(blockedBy) > 0 {
			ids := make([]string, 0, len(blockedBy))
			for _, tid := range blockedBy {
				ids = append(ids, fmt.Sprintf("#%v", tid))
			}
			parts = append(parts, fmt.Sprintf("[blocked by %s]", strings.Join(ids, ", ")))
		}
		lines = append(lines, strings.Join(parts, " "))
	}
	return strings.Join(lines, "\n")
}

// MapResult 将 UpdateTaskTool 执行结果转换为 LLM 可读文本。
// 对齐 Python: UpdateTaskTool.map_result (tools/team_tools.py L1025-1031)
func (t *UpdateTaskTool) MapResult(output map[string]any) string {
	success, _ := output["success"].(bool)
	if !success {
		if errVal, ok := output["error"]; ok {
			return fmt.Sprintf("%v", errVal)
		}
		return "Operation failed"
	}
	d, _ := output["data"].(map[string]any)
	if _, hasCancelledCount := d["cancelled_count"]; hasCancelledCount {
		return fmt.Sprintf("Cancelled %v tasks", d["cancelled_count"])
	}
	return fmt.Sprintf("Task #%v %v", d["task_id"], d["status"])
}

// MapResult 将 SubmitPlanTool 执行结果转换为 LLM 可读文本。
// 对齐 Python: SubmitPlanTool.map_result (tools/team_tools.py L1075-1083)
func (t *SubmitPlanTool) MapResult(output map[string]any) string {
	success, _ := output["success"].(bool)
	if !success {
		if errVal, ok := output["error"]; ok {
			return fmt.Sprintf("%v", errVal)
		}
		return "Failed to submit member plan"
	}
	d, _ := output["data"].(map[string]any)
	return fmt.Sprintf("Member plan submitted: task_id=%v plan_id=%v status=%v member_plan_md=%v",
		d["task_id"], d["plan_id"], d["status"], d["member_plan_md"])
}

// MapResult 将 ClaimTaskTool 执行结果转换为 LLM 可读文本。
// 对齐 Python: ClaimTaskTool.map_result (tools/team_tools.py L1143-1152)
func (t *ClaimTaskTool) MapResult(output map[string]any) string {
	success, _ := output["success"].(bool)
	if !success {
		if errVal, ok := output["error"]; ok {
			return fmt.Sprintf("%v", errVal)
		}
		return "Task not found"
	}
	d, _ := output["data"].(map[string]any)
	sc, _ := d["status_change"].(map[string]any)
	result := fmt.Sprintf("Task #%v %v → %v", d["task_id"], sc["from"], sc["to"])
	if fmt.Sprintf("%v", sc["to"]) == "completed" {
		result += "\n\nTask completed. Call view_task now to find your next available task."
	}
	return result
}

// MapResult 将 MemberCompleteTaskTool 执行结果转换为 LLM 可读文本。
// 对齐 Python: MemberCompleteTaskTool.map_result (tools/team_tools.py L1233-1240)
func (t *MemberCompleteTaskTool) MapResult(output map[string]any) string {
	success, _ := output["success"].(bool)
	if !success {
		if errVal, ok := output["error"]; ok {
			return fmt.Sprintf("%v", errVal)
		}
		return "Failed to complete task"
	}
	d, _ := output["data"].(map[string]any)
	line := fmt.Sprintf("Task #%v completed", d["task_id"])
	if note, _ := d["note"].(string); note != "" {
		line += fmt.Sprintf(" (note: %s)", note)
	}
	return line
}

// MapResult 将 SendMessageTool 执行结果转换为 LLM 可读文本。
// 对齐 Python: SendMessageTool.map_result (tools/team_tools.py L1435-1468)
func (t *SendMessageTool) MapResult(output map[string]any) string {
	success, _ := output["success"].(bool)
	d, _ := output["data"].(map[string]any)
	if !success {
		base := "Failed to send message"
		if errVal, ok := output["error"]; ok {
			base = fmt.Sprintf("%v", errVal)
		}
		if d != nil {
			if typ, _ := d["type"].(string); typ == "multicast" {
				return formatMulticastText(base, d)
			}
		}
		return base
	}
	typ, _ := d["type"].(string)
	switch typ {
	case "broadcast":
		return fmt.Sprintf("Broadcast sent from %v", d["from"])
	case "multicast":
		return formatMulticastText("", d)
	default:
		return fmt.Sprintf("Message sent from %v to %v", d["from"], d["to"])
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// formatMulticastText 渲染多播结果，含 delivered/failed 列表。
// 对齐 Python: SendMessageTool._format_multicast_text (tools/team_tools.py L1461-1468)
func formatMulticastText(errMsg string, d map[string]any) string {
	delivered, _ := d["delivered"].([]any)
	failed, _ := d["failed"].([]any)
	sender, _ := d["from"].(string)
	var parts []string
	if errMsg != "" {
		parts = append(parts, errMsg)
	} else {
		head := fmt.Sprintf("Multicast sent from %s", sender)
		if len(delivered) > 0 {
			names := make([]string, 0, len(delivered))
			for _, name := range delivered {
				names = append(names, fmt.Sprintf("%v", name))
			}
			head += fmt.Sprintf(" to: %s", strings.Join(names, ", "))
		}
		head += fmt.Sprintf(" (%d delivered)", len(delivered))
		parts = append(parts, head)
	}
	if errMsg != "" && len(delivered) > 0 {
		names := make([]string, 0, len(delivered))
		for _, name := range delivered {
			names = append(names, fmt.Sprintf("%v", name))
		}
		parts = append(parts, fmt.Sprintf("delivered: %s", strings.Join(names, ", ")))
	}
	if len(failed) > 0 {
		failedTexts := make([]string, 0, len(failed))
		for _, f := range failed {
			fm, _ := f.(map[string]any)
			failedTexts = append(failedTexts, fmt.Sprintf("%v — %v", fm["to"], fm["reason"]))
		}
		parts = append(parts, fmt.Sprintf("failed: %s", strings.Join(failedTexts, "; ")))
	}
	return strings.Join(parts, "; ")
}
