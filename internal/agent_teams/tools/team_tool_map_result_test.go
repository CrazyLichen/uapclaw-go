package tools

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestBuildTeamToolMapResult 测试 BuildTeamTool.MapResult
func TestBuildTeamToolMapResult(t *testing.T) {
	tool := &BuildTeamTool{}

	// 成功路径
	result := tool.MapResult(map[string]any{
		"success": true,
		"data": map[string]any{
			"team_name":           "alpha",
			"display_name":        "Alpha",
			"leader_member_name":  "leader-1",
			"leader_display_name": "Leader",
			"enable_hitt":         true,
		},
	})
	assert.Contains(t, result, "Team created")
	assert.Contains(t, result, "alpha")

	// 失败路径：有 error
	result = tool.MapResult(map[string]any{
		"success": false,
		"error":   "team already exists",
	})
	assert.Equal(t, "team already exists", result)

	// 失败路径：无 error
	result = tool.MapResult(map[string]any{"success": false})
	assert.Equal(t, "Failed to build team", result)

	// 成功路径：data 为 nil
	result = tool.MapResult(map[string]any{"success": true})
	assert.Contains(t, result, "Team created")
}

// TestCleanTeamToolMapResult 测试 CleanTeamTool.MapResult
func TestCleanTeamToolMapResult(t *testing.T) {
	tool := &CleanTeamTool{}

	result := tool.MapResult(map[string]any{
		"success": true,
		"data":    map[string]any{"team_name": "alpha"},
	})
	assert.Contains(t, result, "Team cleaned")

	result = tool.MapResult(map[string]any{"success": false, "error": "not found"})
	assert.Equal(t, "not found", result)

	result = tool.MapResult(map[string]any{"success": false})
	assert.Equal(t, "Failed to clean team", result)
}

// TestSpawnMemberToolMapResult 测试 SpawnMemberTool.MapResult
func TestSpawnMemberToolMapResult(t *testing.T) {
	tool := &SpawnMemberTool{}

	// 有 role_type
	result := tool.MapResult(map[string]any{
		"success": true,
		"data": map[string]any{
			"member_name":  "member-1",
			"display_name": "Member1",
			"role_type":    "leader",
		},
	})
	assert.Contains(t, result, "leader")

	// 无 role_type（默认 teammate）
	result = tool.MapResult(map[string]any{
		"success": true,
		"data": map[string]any{
			"member_name":  "member-2",
			"display_name": "Member2",
		},
	})
	assert.Contains(t, result, "teammate")

	// 失败路径
	result = tool.MapResult(map[string]any{"success": false, "error": "pool full"})
	assert.Equal(t, "pool full", result)
}

// TestShutdownMemberToolMapResult 测试 ShutdownMemberTool.MapResult
func TestShutdownMemberToolMapResult(t *testing.T) {
	tool := &ShutdownMemberTool{}

	result := tool.MapResult(map[string]any{
		"success": true,
		"data":    map[string]any{"member_name": "member-1"},
	})
	assert.Contains(t, result, "Member shutdown")

	result = tool.MapResult(map[string]any{"success": false})
	assert.Equal(t, "Failed to shutdown member", result)
}

// TestApprovePlanToolMapResult 测试 ApprovePlanTool.MapResult
func TestApprovePlanToolMapResult(t *testing.T) {
	tool := &ApprovePlanTool{}

	// approved
	result := tool.MapResult(map[string]any{
		"success": true,
		"data":    map[string]any{"approved": true, "plan_id": "p1"},
	})
	assert.Contains(t, result, "approved")

	// rejected
	result = tool.MapResult(map[string]any{
		"success": true,
		"data":    map[string]any{"approved": false, "plan_id": "p2"},
	})
	assert.Contains(t, result, "rejected")

	// 失败路径
	result = tool.MapResult(map[string]any{"success": false})
	assert.Equal(t, "Failed to approve/reject plan", result)
}

// TestApproveToolCallToolMapResult 测试 ApproveToolCallTool.MapResult
func TestApproveToolCallToolMapResult(t *testing.T) {
	tool := &ApproveToolCallTool{}

	result := tool.MapResult(map[string]any{
		"success": true,
		"data": map[string]any{
			"approved":     true,
			"tool_call_id": "tc-1",
			"member_name":  "member-1",
		},
	})
	assert.Contains(t, result, "approved")

	result = tool.MapResult(map[string]any{"success": false})
	assert.Equal(t, "Failed to approve/reject tool call", result)
}

// TestListMembersToolMapResult 测试 ListMembersTool.MapResult
func TestListMembersToolMapResult(t *testing.T) {
	tool := &ListMembersTool{}

	// 有成员
	result := tool.MapResult(map[string]any{
		"success": true,
		"data": map[string]any{
			"members": []any{
				map[string]any{"member_name": "m1", "display_name": "M1", "status": "running"},
				map[string]any{"member_name": "m2", "display_name": "M2", "status": "paused"},
			},
		},
	})
	assert.Contains(t, result, "m1")
	assert.Contains(t, result, "m2")

	// 无成员
	result = tool.MapResult(map[string]any{
		"success": true,
		"data":    map[string]any{"members": []any{}},
	})
	assert.Equal(t, "No members", result)

	// 失败
	result = tool.MapResult(map[string]any{"success": false})
	assert.Equal(t, "Failed to list members", result)
}

// TestTaskCreateToolMapResult 测试 TaskCreateTool.MapResult
func TestTaskCreateToolMapResult(t *testing.T) {
	tool := &TaskCreateTool{}

	// 单任务路径
	result := tool.MapResult(map[string]any{
		"success": true,
		"data":    map[string]any{"task_id": "t1", "title": "My Task"},
	})
	assert.Contains(t, result, "Task created")

	// 批量路径
	result = tool.MapResult(map[string]any{
		"success": true,
		"data": map[string]any{
			"tasks": []any{
				map[string]any{"task_id": "t1", "title": "T1"},
			},
			"count":   1,
			"skipped": 0,
		},
	})
	assert.Contains(t, result, "Created 1")

	// 批量路径含失败
	result = tool.MapResult(map[string]any{
		"success": true,
		"data": map[string]any{
			"tasks":   []any{},
			"count":   0,
			"skipped": 1,
			"failures": []any{
				map[string]any{"spec": "bad-spec", "reason": "invalid"},
			},
		},
	})
	assert.Contains(t, result, "skipped")

	// 失败路径
	result = tool.MapResult(map[string]any{"success": false})
	assert.Equal(t, "Operation failed", result)
}

// TestViewTaskToolMapResult 测试 ViewTaskTool.MapResult
func TestViewTaskToolMapResult(t *testing.T) {
	tool := &ViewTaskTool{}

	// 详情视图（含 content）
	result := tool.MapResult(map[string]any{
		"success": true,
		"data": map[string]any{
			"task_id": "1", "title": "T1", "status": "in_progress",
			"content": "do something", "assignee": "member-1",
		},
	})
	assert.Contains(t, result, "do something")
	assert.Contains(t, result, "Assignee")

	// 详情视图含 blocked_by 和 blocks
	result = tool.MapResult(map[string]any{
		"success": true,
		"data": map[string]any{
			"task_id": "2", "title": "T2", "status": "blocked",
			"content": "waiting", "blocked_by": []any{"1"}, "blocks": []any{"3"},
		},
	})
	assert.Contains(t, result, "Blocked by")
	assert.Contains(t, result, "Blocks")

	// 列表视图
	result = tool.MapResult(map[string]any{
		"success": true,
		"data": map[string]any{
			"tasks": []any{
				map[string]any{"task_id": "1", "title": "T1", "status": "open"},
			},
		},
	})
	assert.Contains(t, result, "#1")

	// 列表视图含 assignee 和 blocked_by
	result = tool.MapResult(map[string]any{
		"success": true,
		"data": map[string]any{
			"tasks": []any{
				map[string]any{
					"task_id": "2", "title": "T2", "status": "blocked",
					"assignee": "m1", "blocked_by": []any{"1"},
				},
			},
		},
	})
	assert.Contains(t, result, "m1")
	assert.Contains(t, result, "blocked by")

	// 列表视图空
	result = tool.MapResult(map[string]any{
		"success": true,
		"data":    map[string]any{"tasks": []any{}},
	})
	assert.Equal(t, "No tasks found", result)

	// 失败
	result = tool.MapResult(map[string]any{"success": false})
	assert.Equal(t, "Task not found", result)
}

// TestUpdateTaskToolMapResult 测试 UpdateTaskTool.MapResult
func TestUpdateTaskToolMapResult(t *testing.T) {
	tool := &UpdateTaskTool{}

	// 取消路径
	result := tool.MapResult(map[string]any{
		"success": true,
		"data":    map[string]any{"cancelled_count": 3},
	})
	assert.Contains(t, result, "Cancelled 3")

	// 普通更新路径
	result = tool.MapResult(map[string]any{
		"success": true,
		"data":    map[string]any{"task_id": "1", "status": "completed"},
	})
	assert.Contains(t, result, "completed")

	// 失败
	result = tool.MapResult(map[string]any{"success": false})
	assert.Equal(t, "Operation failed", result)
}

// TestSubmitPlanToolMapResult 测试 SubmitPlanTool.MapResult
func TestSubmitPlanToolMapResult(t *testing.T) {
	tool := &SubmitPlanTool{}

	result := tool.MapResult(map[string]any{
		"success": true,
		"data": map[string]any{
			"task_id": "1", "plan_id": "p1", "status": "approved",
			"member_plan_md": "## Plan",
		},
	})
	assert.Contains(t, result, "Member plan submitted")

	result = tool.MapResult(map[string]any{"success": false})
	assert.Equal(t, "Failed to submit member plan", result)
}

// TestClaimTaskToolMapResult 测试 ClaimTaskTool.MapResult
func TestClaimTaskToolMapResult(t *testing.T) {
	tool := &ClaimTaskTool{}

	// 非完成状态
	result := tool.MapResult(map[string]any{
		"success": true,
		"data": map[string]any{
			"task_id":       "1",
			"status_change": map[string]any{"from": "open", "to": "in_progress"},
		},
	})
	assert.Contains(t, result, "in_progress")
	assert.NotContains(t, result, "completed")

	// 完成状态
	result = tool.MapResult(map[string]any{
		"success": true,
		"data": map[string]any{
			"task_id":       "2",
			"status_change": map[string]any{"from": "in_progress", "to": "completed"},
		},
	})
	assert.Contains(t, result, "completed")
	assert.Contains(t, result, "view_task")

	// 失败
	result = tool.MapResult(map[string]any{"success": false})
	assert.Equal(t, "Task not found", result)
}

// TestMemberCompleteTaskToolMapResult 测试 MemberCompleteTaskTool.MapResult
func TestMemberCompleteTaskToolMapResult(t *testing.T) {
	tool := &MemberCompleteTaskTool{}

	// 有 note
	result := tool.MapResult(map[string]any{
		"success": true,
		"data":    map[string]any{"task_id": "1", "note": "done well"},
	})
	assert.Contains(t, result, "done well")

	// 无 note
	result = tool.MapResult(map[string]any{
		"success": true,
		"data":    map[string]any{"task_id": "2"},
	})
	assert.Contains(t, result, "completed")
	assert.NotContains(t, result, "note")

	// 失败
	result = tool.MapResult(map[string]any{"success": false})
	assert.Equal(t, "Failed to complete task", result)
}

// TestSendMessageToolMapResult 测试 SendMessageTool.MapResult
func TestSendMessageToolMapResult(t *testing.T) {
	tool := &SendMessageTool{}

	// broadcast
	result := tool.MapResult(map[string]any{
		"success": true,
		"data":    map[string]any{"type": "broadcast", "from": "leader"},
	})
	assert.Contains(t, result, "Broadcast sent")

	// multicast
	result = tool.MapResult(map[string]any{
		"success": true,
		"data": map[string]any{
			"type": "multicast", "from": "leader",
			"delivered": []any{"m1", "m2"}, "failed": []any{},
		},
	})
	assert.Contains(t, result, "Multicast sent")

	// 默认（direct）
	result = tool.MapResult(map[string]any{
		"success": true,
		"data":    map[string]any{"type": "direct", "from": "m1", "to": "m2"},
	})
	assert.Contains(t, result, "Message sent")

	// 失败路径含 multicast
	result = tool.MapResult(map[string]any{
		"success": false,
		"error":   "partial failure",
		"data":    map[string]any{"type": "multicast", "delivered": []any{"m1"}, "failed": []any{}},
	})
	assert.Contains(t, result, "partial failure")
	assert.Contains(t, result, "delivered")

	// 失败路径无 data
	result = tool.MapResult(map[string]any{"success": false, "error": "network error"})
	assert.Equal(t, "network error", result)

	// 失败路径无 error
	result = tool.MapResult(map[string]any{"success": false})
	assert.Equal(t, "Failed to send message", result)
}

// TestFormatMulticastText 测试 formatMulticastText 辅助函数
func TestFormatMulticastText(t *testing.T) {
	// 有错误消息 + delivered + failed
	result := formatMulticastText("partial fail", map[string]any{
		"from":      "leader",
		"delivered": []any{"m1"},
		"failed": []any{
			map[string]any{"to": "m2", "reason": "timeout"},
		},
	})
	assert.Contains(t, result, "partial fail")
	assert.Contains(t, result, "m1")
	assert.Contains(t, result, "m2")
	assert.Contains(t, result, "timeout")

	// 无错误消息 + delivered
	result = formatMulticastText("", map[string]any{
		"from":      "leader",
		"delivered": []any{"m1", "m2"},
		"failed":    []any{},
	})
	assert.Contains(t, result, "Multicast sent from leader")
	assert.Contains(t, result, "m1, m2")

	// 无错误消息 + 无 delivered + 有 failed
	result = formatMulticastText("", map[string]any{
		"from":      "leader",
		"delivered": []any{},
		"failed": []any{
			map[string]any{"to": "m1", "reason": "offline"},
		},
	})
	assert.Contains(t, result, "failed")
	assert.Contains(t, result, "offline")

	// 无错误消息 + delivered + 无 failed
	result = formatMulticastText("", map[string]any{
		"from":      "leader",
		"delivered": []any{"m1"},
		"failed":    []any{},
	})
	assert.Contains(t, result, "1 delivered")
	assert.NotContains(t, result, "failed")

	// 确保输出使用分号分隔
	result = formatMulticastText("err", map[string]any{
		"from":      "leader",
		"delivered": []any{"m1"},
		"failed": []any{
			map[string]any{"to": "m2", "reason": "timeout"},
		},
	})
	assert.True(t, strings.Contains(result, "; "), "多段内容应使用分号分隔")
}
