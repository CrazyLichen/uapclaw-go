package todo_render

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 渲染 ────────────────────────────

// TestRenderTodoItem_四状态 对齐 todo_render.py:33-49。
func TestRenderTodoItem_四状态(t *testing.T) {
	assert.Equal(t, "[green]☑ 完成[/green]", RenderTodoItem("完成", "completed"))
	assert.Equal(t, "[yellow]◐ 进行[/yellow]", RenderTodoItem("进行", "in_progress"))
	assert.Equal(t, "[dim]☐ 待办[/dim]", RenderTodoItem("待办", "pending"))
	assert.Equal(t, "[dim strike]☒ 取消[/dim strike]", RenderTodoItem("取消", "cancelled"))
	// 未知状态回退 pending 样式
	assert.Equal(t, "[dim]☐ 未知[/dim]", RenderTodoItem("未知", "whatever"))
}

// TestRenderTodoList_前缀与activeForm 对齐 todo_render.py:51-72。
func TestRenderTodoList_前缀与activeForm(t *testing.T) {
	items := []map[string]any{
		{"content": "任务一", "status": "completed"},
		{"activeForm": "正在二", "status": "in_progress"},
	}
	lines := RenderTodoList(items)
	require.Len(t, lines, 2)
	assert.Contains(t, lines[0], "⎿  [green]☑ 任务一")
	assert.Contains(t, lines[1], "◐ 正在二", "content 缺省取 activeForm")
}

// TestRenderTodoSummary_计数 对齐 todo_render.py:74-105。
func TestRenderTodoSummary_计数(t *testing.T) {
	items := []map[string]any{
		{"status": "completed"}, {"status": "completed"},
		{"status": "in_progress"},
		{"status": "pending"}, {"status": "pending"}, {"status": "pending"},
		{"status": "cancelled"}, // cancelled 不进摘要
	}
	assert.Equal(t, "✓2 ◐1 ☐3", RenderTodoSummary(items))
	assert.Equal(t, "No tasks", RenderTodoSummary(nil))
}

// ──────────────────────────── 解析 ────────────────────────────

// TestParseTodoResult_JSON列表 对齐策略 1。
func TestParseTodoResult_JSON列表(t *testing.T) {
	items := ParseTodoResult(`[{"content":"a","status":"pending"},{"content":"b","status":"done"}]`)
	require.Len(t, items, 2)
	assert.Equal(t, "a", items[0]["content"])
}

// TestParseTodoResult_JSON包装对象 items/tasks/todos/result 四键。
func TestParseTodoResult_JSON包装对象(t *testing.T) {
	for _, key := range []string{"items", "tasks", "todos", "result"} {
		items := ParseTodoResult(`{"` + key + `": [{"content":"x"}]}`)
		require.Len(t, items, 1, key)
		assert.Equal(t, "x", items[0]["content"])
	}
	// 单对象带 content
	items := ParseTodoResult(`{"content":"单条","status":"pending"}`)
	require.Len(t, items, 1)
	// 非列表键不解析
	assert.Nil(t, ParseTodoResult(`{"items": "不是列表"}`))
}

// TestParseTodoResult_SDK文本_CreateTool 对齐策略 2 形态 1。
func TestParseTodoResult_SDK文本_CreateTool(t *testing.T) {
	text := "Successfully created 3 task(s):\n" +
		"  [>] task_id: abc , content: Write tests\n" +
		"  [ ] task_id: def , content: Deploy\n" +
		"  [√] task_id: ghi , content: Review"
	items := ParseTodoResult(text)
	require.Len(t, items, 3)
	assert.Equal(t, "in_progress", items[0]["status"])
	assert.Equal(t, "pending", items[1]["status"])
	assert.Equal(t, "completed", items[2]["status"])
}

// TestParseTodoResult_SDK文本_ListTool 对齐策略 2 形态 2。
func TestParseTodoResult_SDK文本_ListTool(t *testing.T) {
	text := "Todo List (Total: 3 items):\n\n" +
		"[>] In Progress Task\n" +
		" [uuid] Write tests\n\n" +
		"[ ] Pending Tasks\n" +
		" [uuid] Deploy\n" +
		" [uuid] Review"
	items := ParseTodoResult(text)
	require.Len(t, items, 3)
	assert.Equal(t, "in_progress", items[0]["status"])
	assert.Equal(t, "Write tests", items[0]["content"])
	assert.Equal(t, "pending", items[1]["status"])
	assert.Equal(t, "pending", items[2]["status"])
}

// TestParseTodoResult_尾部残留清理 dict repr 残留剥离后可解析。
func TestParseTodoResult_尾部残留清理(t *testing.T) {
	// 模拟 str(dict) 的尾部 "'}"
	text := "  [ ] task_id: a , content: 任务'}"
	items := ParseTodoResult(text)
	require.Len(t, items, 1)
	assert.Equal(t, "任务", items[0]["content"])
}

// TestParseTodoResult_无法解析 空串与噪音。
func TestParseTodoResult_无法解析(t *testing.T) {
	assert.Nil(t, ParseTodoResult(""))
	assert.Nil(t, ParseTodoResult("Successfully updated"))
}

// ──────────────────────────── todo_modify 应用 ────────────────────────────

// baseItems 构造三件套。
func baseItems() []map[string]any {
	return []map[string]any{
		{"id": "1", "content": "一", "status": "pending"},
		{"id": "2", "content": "二", "status": "in_progress", "activeForm": "正在二"},
		{"id": "3", "content": "三", "status": "completed"},
	}
}

// TestApplyTodoModifyArgs_无action 无 action 返回 nil。
func TestApplyTodoModifyArgs_无action(t *testing.T) {
	assert.Nil(t, ApplyTodoModifyArgs(baseItems(), map[string]any{}))
	assert.Nil(t, ApplyTodoModifyArgs(baseItems(), "not-json"))
}

// TestApplyTodoModifyArgs_update 按 id 合并并同步 content。
func TestApplyTodoModifyArgs_update(t *testing.T) {
	result := ApplyTodoModifyArgs(baseItems(), map[string]any{
		"action": "update",
		"todos": []any{
			map[string]any{"id": "1", "status": "completed"},
		},
	})
	require.Len(t, result, 3)
	assert.Equal(t, "completed", result[0]["status"])
}

// TestApplyTodoModifyArgs_delete 按删除。
func TestApplyTodoModifyArgs_delete(t *testing.T) {
	result := ApplyTodoModifyArgs(baseItems(), map[string]any{
		"action": "delete", "ids": []any{"2"},
	})
	require.Len(t, result, 2)
	assert.Equal(t, "1", result[0]["id"])
	assert.Equal(t, "3", result[1]["id"])
}

// TestApplyTodoModifyArgs_cancel 置 cancelled。
func TestApplyTodoModifyArgs_cancel(t *testing.T) {
	result := ApplyTodoModifyArgs(baseItems(), map[string]any{
		"action": "cancel", "ids": []any{"1", "3"},
	})
	require.Len(t, result, 3)
	assert.Equal(t, "cancelled", result[0]["status"])
	assert.Equal(t, "cancelled", result[2]["status"])
}

// TestApplyTodoModifyArgs_append 追加并归一化。
func TestApplyTodoModifyArgs_append(t *testing.T) {
	result := ApplyTodoModifyArgs(baseItems(), map[string]any{
		"action": "append",
		"todos":  []any{map[string]any{"id": "4", "status": "in_progress", "activeForm": "正在四"}},
	})
	require.Len(t, result, 4)
	assert.Equal(t, "正在四", result[3]["content"], "in_progress 时 content 取 activeForm")
}

// TestApplyTodoModifyArgs_insert 两个方向。
func TestApplyTodoModifyArgs_insert(t *testing.T) {
	newTodo := map[string]any{"id": "9", "content": "新", "status": "pending"}
	after := ApplyTodoModifyArgs(baseItems(), map[string]any{
		"action": "insert_after", "todo_data": []any{"1", []any{newTodo}},
	})
	require.Len(t, after, 4)
	assert.Equal(t, "9", after[1]["id"])
	before := ApplyTodoModifyArgs(baseItems(), map[string]any{
		"action": "insert_before", "todo_data": []any{"1", []any{newTodo}},
	})
	require.Len(t, before, 4)
	assert.Equal(t, "9", before[0]["id"])
}

// TestApplyTodoModifyArgs_失败形态 目标不存在 / todo_data 长度错。
func TestApplyTodoModifyArgs_失败形态(t *testing.T) {
	assert.Nil(t, ApplyTodoModifyArgs(baseItems(), map[string]any{
		"action": "insert_after", "todo_data": []any{"missing", []any{}},
	}))
	assert.Nil(t, ApplyTodoModifyArgs(baseItems(), map[string]any{
		"action": "insert_after", "todo_data": []any{"1"},
	}))
	assert.Nil(t, ApplyTodoModifyArgs(baseItems(), map[string]any{
		"action": "update", "todos": "不是列表",
	}))
}
