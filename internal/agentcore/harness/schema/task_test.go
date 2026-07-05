package schema

import (
	"encoding/json"
	"reflect"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestTodoStatus_JSON 验证 TodoStatus 枚举的 JSON 往返序列化
func TestTodoStatus_JSON(t *testing.T) {
	statuses := []TodoStatus{
		TodoStatusPending,
		TodoStatusInProgress,
		TodoStatusCompleted,
		TodoStatusCancelled,
	}
	for _, status := range statuses {
		data, err := json.Marshal(status)
		if err != nil {
			t.Errorf("MarshalJSON 失败: %v", err)
			continue
		}
		var parsed TodoStatus
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Errorf("UnmarshalJSON 失败: %v", err)
			continue
		}
		if parsed != status {
			t.Errorf("往返不一致: 期望 = %d, 实际 = %d", status, parsed)
		}
	}
}

// TestTodoStatus_Parse 验证 ParseTodoStatus 正常和无效输入
func TestTodoStatus_Parse(t *testing.T) {
	// 正常输入
	cases := map[string]TodoStatus{
		"pending":     TodoStatusPending,
		"in_progress": TodoStatusInProgress,
		"completed":   TodoStatusCompleted,
		"cancelled":   TodoStatusCancelled,
	}
	for input, expected := range cases {
		got, err := ParseTodoStatus(input)
		if err != nil {
			t.Errorf("ParseTodoStatus(%q) 意外错误: %v", input, err)
		}
		if got != expected {
			t.Errorf("ParseTodoStatus(%q) = %d, 期望 = %d", input, got, expected)
		}
	}
	// 无效输入
	_, err := ParseTodoStatus("unknown_status")
	if err == nil {
		t.Error("ParseTodoStatus 对无效输入应返回错误")
	}
}

// TestTodoStatus_String 验证 TodoStatus 的 String 方法
func TestTodoStatus_String(t *testing.T) {
	expected := map[TodoStatus]string{
		TodoStatusPending:    "pending",
		TodoStatusInProgress: "in_progress",
		TodoStatusCompleted:  "completed",
		TodoStatusCancelled:  "cancelled",
	}
	for status, exp := range expected {
		if got := status.String(); got != exp {
			t.Errorf("TodoStatus(%d).String() = %q, 期望 = %q", status, got, exp)
		}
	}
}

// TestStatusIcons 验证 StatusIcons 映射值
func TestStatusIcons(t *testing.T) {
	expected := map[TodoStatus]string{
		TodoStatusPending:    "[ ]",
		TodoStatusInProgress: "[→]",
		TodoStatusCompleted:  "[√]",
		TodoStatusCancelled:  "[×]",
	}
	for status, exp := range expected {
		got, ok := StatusIcons[status]
		if !ok {
			t.Errorf("StatusIcons 缺少 TodoStatus(%d)", status)
		}
		if got != exp {
			t.Errorf("StatusIcons[%d] = %q, 期望 = %q", status, got, exp)
		}
	}
}

// TestTodoItem_默认值 验证 NewTodoItem 返回的默认值
func TestTodoItem_默认值(t *testing.T) {
	item := NewTodoItem()
	if item.ID == "" {
		t.Error("期望 ID 不为空")
	}
	if item.Status != TodoStatusPending {
		t.Errorf("期望 Status = TodoStatusPending, 实际 = %d", item.Status)
	}
}

// TestTodoItem_ToDict 验证 TodoItem 序列化
func TestTodoItem_ToDict(t *testing.T) {
	item := TodoItem{
		ID:              "test-id",
		Content:         "测试内容",
		ActiveForm:      "正在测试",
		Description:     "详细描述",
		Status:          TodoStatusInProgress,
		DependsOn:       []string{"dep1", "dep2"},
		ResultSummary:   "结果摘要",
		MetaData:        map[string]any{"key": "value"},
		SelectedModelID: "model-1",
	}
	d := item.ToDict()

	if d["id"] != "test-id" {
		t.Errorf("期望 id = %q, 实际 = %v", "test-id", d["id"])
	}
	if d["status"] != "in_progress" {
		t.Errorf("期望 status = %q, 实际 = %v", "in_progress", d["status"])
	}
	if d["content"] != "测试内容" {
		t.Errorf("期望 content = %q, 实际 = %v", "测试内容", d["content"])
	}
	if d["selected_model_id"] != "model-1" {
		t.Errorf("期望 selected_model_id = %q, 实际 = %v", "model-1", d["selected_model_id"])
	}
}

// TestTodoItem_FromDict 验证从字典反序列化
func TestTodoItem_FromDict(t *testing.T) {
	data := map[string]any{
		"id":                "item-1",
		"content":           "内容",
		"activeForm":        "进行中",
		"description":       "描述",
		"status":            "completed",
		"depends_on":        []any{"dep1"},
		"result_summary":    "摘要",
		"meta_data":         map[string]any{"k": "v"},
		"selected_model_id": "m1",
	}
	item := TodoItem{}.FromDict(data)

	if item.ID != "item-1" {
		t.Errorf("期望 ID = %q, 实际 = %q", "item-1", item.ID)
	}
	if item.Status != TodoStatusCompleted {
		t.Errorf("期望 Status = TodoStatusCompleted, 实际 = %d", item.Status)
	}
	if len(item.DependsOn) != 1 || item.DependsOn[0] != "dep1" {
		t.Errorf("期望 DependsOn = [dep1], 实际 = %v", item.DependsOn)
	}
	if item.SelectedModelID != "m1" {
		t.Errorf("期望 SelectedModelID = %q, 实际 = %q", "m1", item.SelectedModelID)
	}
}

// TestTodoItem_往返 验证 ToDict → FromDict 往返一致
func TestTodoItem_往返(t *testing.T) {
	original := TodoItem{
		ID:              "rt-id",
		Content:         "往返测试",
		ActiveForm:      "测试中",
		Description:     "往返描述",
		Status:          TodoStatusInProgress,
		DependsOn:       []string{"d1"},
		ResultSummary:   "摘要",
		MetaData:        map[string]any{"k": "v"},
		SelectedModelID: "model-x",
	}
	restored := TodoItem{}.FromDict(original.ToDict())

	if restored.ID != original.ID {
		t.Errorf("期望 ID = %q, 实际 = %q", original.ID, restored.ID)
	}
	if restored.Status != original.Status {
		t.Errorf("期望 Status = %d, 实际 = %d", original.Status, restored.Status)
	}
	if restored.Content != original.Content {
		t.Errorf("期望 Content = %q, 实际 = %q", original.Content, restored.Content)
	}
	if restored.SelectedModelID != original.SelectedModelID {
		t.Errorf("期望 SelectedModelID = %q, 实际 = %q", original.SelectedModelID, restored.SelectedModelID)
	}
}

// TestTaskPlan_默认值 验证 NewTaskPlan 返回的默认值
func TestTaskPlan_默认值(t *testing.T) {
	tp := NewTaskPlan("my-task", "my goal")
	if tp.TaskName != "my-task" {
		t.Errorf("期望 TaskName = %q, 实际 = %q", "my-task", tp.TaskName)
	}
	if tp.Goal != "my goal" {
		t.Errorf("期望 Goal = %q, 实际 = %q", "my goal", tp.Goal)
	}
	if len(tp.Tasks) != 0 {
		t.Errorf("期望 Tasks 为空, 实际长度 = %d", len(tp.Tasks))
	}
}

// TestTaskPlan_GetTask_找到 验证 GetTask 找到存在的任务
func TestTaskPlan_GetTask_找到(t *testing.T) {
	tp := NewTaskPlan("test", "goal")
	item := NewTodoItem()
	item.Content = "找到我"
	tp.AddTask(item)

	found := tp.GetTask(item.ID)
	if found == nil {
		t.Error("期望找到任务, 实际为 nil")
	} else if found.Content != "找到我" {
		t.Errorf("期望 Content = %q, 实际 = %q", "找到我", found.Content)
	}
}

// TestTaskPlan_GetTask_未找到 验证 GetTask 对不存在的 ID 返回 nil
func TestTaskPlan_GetTask_未找到(t *testing.T) {
	tp := NewTaskPlan("test", "goal")
	found := tp.GetTask("nonexistent")
	if found != nil {
		t.Error("期望 nil, 实际不为 nil")
	}
}

// TestTaskPlan_GetNextTask_无依赖 验证无依赖时返回第一个 Pending 任务
func TestTaskPlan_GetNextTask_无依赖(t *testing.T) {
	tp := NewTaskPlan("test", "goal")
	item1 := NewTodoItem()
	item1.Content = "第一个"
	item2 := NewTodoItem()
	item2.Content = "第二个"
	tp.AddTask(item1)
	tp.AddTask(item2)

	next := tp.GetNextTask()
	if next == nil {
		t.Error("期望找到下一个任务, 实际为 nil")
	} else if next.ID != item1.ID {
		t.Errorf("期望 ID = %q, 实际 = %q", item1.ID, next.ID)
	}
}

// TestTaskPlan_GetNextTask_依赖排序 验证依赖未完成时不可选
func TestTaskPlan_GetNextTask_依赖排序(t *testing.T) {
	tp := NewTaskPlan("test", "goal")

	// A 依赖于 B
	itemB := NewTodoItem()
	itemB.Content = "B"
	itemA := NewTodoItem()
	itemA.Content = "A"
	itemA.DependsOn = []string{itemB.ID}

	tp.AddTask(itemA)
	tp.AddTask(itemB)

	// B 未完成，A 不可选
	next := tp.GetNextTask()
	if next == nil {
		t.Error("期望找到 B, 实际为 nil")
	} else if next.ID != itemB.ID {
		t.Errorf("期望 ID = %q (B), 实际 = %q", itemB.ID, next.ID)
	}

	// 完成 B 后，A 可选
	_ = tp.MarkCompleted(itemB.ID, "B 完成")
	next = tp.GetNextTask()
	if next == nil {
		t.Error("期望找到 A, 实际为 nil")
	} else if next.ID != itemA.ID {
		t.Errorf("期望 ID = %q (A), 实际 = %q", itemA.ID, next.ID)
	}
}

// TestTaskPlan_AddTask 验证添加任务
func TestTaskPlan_AddTask(t *testing.T) {
	tp := NewTaskPlan("test", "goal")
	item := NewTodoItem()
	item.Content = "新增任务"
	tp.AddTask(item)

	if len(tp.Tasks) != 1 {
		t.Errorf("期望 Tasks 长度 = 1, 实际 = %d", len(tp.Tasks))
	}
	if tp.Tasks[0].Content != "新增任务" {
		t.Errorf("期望 Content = %q, 实际 = %q", "新增任务", tp.Tasks[0].Content)
	}
}

// TestTaskPlan_MarkInProgress 验证标记执行中
func TestTaskPlan_MarkInProgress(t *testing.T) {
	tp := NewTaskPlan("test", "goal")
	item := NewTodoItem()
	tp.AddTask(item)

	err := tp.MarkInProgress(item.ID)
	if err != nil {
		t.Errorf("意外错误: %v", err)
	}
	updated := tp.GetTask(item.ID)
	if updated.Status != TodoStatusInProgress {
		t.Errorf("期望 Status = TodoStatusInProgress, 实际 = %d", updated.Status)
	}
	if tp.CurrentTaskID != item.ID {
		t.Errorf("期望 CurrentTaskID = %q, 实际 = %q", item.ID, tp.CurrentTaskID)
	}
}

// TestTaskPlan_MarkCompleted 验证标记完成
func TestTaskPlan_MarkCompleted(t *testing.T) {
	tp := NewTaskPlan("test", "goal")
	item := NewTodoItem()
	tp.AddTask(item)

	err := tp.MarkCompleted(item.ID, "完成摘要")
	if err != nil {
		t.Errorf("意外错误: %v", err)
	}
	updated := tp.GetTask(item.ID)
	if updated.Status != TodoStatusCompleted {
		t.Errorf("期望 Status = TodoStatusCompleted, 实际 = %d", updated.Status)
	}
	if updated.ResultSummary != "完成摘要" {
		t.Errorf("期望 ResultSummary = %q, 实际 = %q", "完成摘要", updated.ResultSummary)
	}
}

// TestTaskPlan_MarkCancelled 验证标记取消
func TestTaskPlan_MarkCancelled(t *testing.T) {
	tp := NewTaskPlan("test", "goal")
	item := NewTodoItem()
	tp.AddTask(item)

	err := tp.MarkCancelled(item.ID, "取消原因")
	if err != nil {
		t.Errorf("意外错误: %v", err)
	}
	updated := tp.GetTask(item.ID)
	if updated.Status != TodoStatusCancelled {
		t.Errorf("期望 Status = TodoStatusCancelled, 实际 = %d", updated.Status)
	}
	if updated.ResultSummary != "取消原因" {
		t.Errorf("期望 ResultSummary = %q, 实际 = %q", "取消原因", updated.ResultSummary)
	}
}

// TestTaskPlan_Mark_不存在 验证对不存在的任务 ID 返回错误
func TestTaskPlan_Mark_不存在(t *testing.T) {
	tp := NewTaskPlan("test", "goal")

	if err := tp.MarkInProgress("no-id"); err == nil {
		t.Error("MarkInProgress 对不存在的 ID 应返回错误")
	}
	if err := tp.MarkCompleted("no-id", ""); err == nil {
		t.Error("MarkCompleted 对不存在的 ID 应返回错误")
	}
	if err := tp.MarkCancelled("no-id", ""); err == nil {
		t.Error("MarkCancelled 对不存在的 ID 应返回错误")
	}
}

// TestTaskPlan_GetProgressSummary 验证进度摘要
func TestTaskPlan_GetProgressSummary(t *testing.T) {
	tp := NewTaskPlan("test", "goal")
	for i := 0; i < 7; i++ {
		item := NewTodoItem()
		tp.AddTask(item)
	}
	// 完成 3 个
	_ = tp.MarkCompleted(tp.Tasks[0].ID, "")
	_ = tp.MarkCompleted(tp.Tasks[1].ID, "")
	_ = tp.MarkCompleted(tp.Tasks[2].ID, "")

	summary := tp.GetProgressSummary()
	expected := "3/7 completed"
	if summary != expected {
		t.Errorf("期望 %q, 实际 = %q", expected, summary)
	}
}

// TestTaskPlan_ToMarkdown 验证 Markdown 输出格式
func TestTaskPlan_ToMarkdown(t *testing.T) {
	tp := NewTaskPlan("my-task", "实现功能")
	item1 := NewTodoItem()
	item1.Content = "步骤一"
	item1.Status = TodoStatusCompleted
	item2 := NewTodoItem()
	item2.Content = "步骤二"
	item2.Status = TodoStatusInProgress
	item3 := NewTodoItem()
	item3.Content = "步骤三"
	tp.AddTask(item1)
	tp.AddTask(item2)
	tp.AddTask(item3)

	md := tp.ToMarkdown()
	if !containsSubstring(md, "# my-task") {
		t.Errorf("Markdown 缺少标题行: %q", md)
	}
	if !containsSubstring(md, "**Goal:** 实现功能") {
		t.Errorf("Markdown 缺少目标行: %q", md)
	}
	if !containsSubstring(md, "- [√] 步骤一") {
		t.Errorf("Markdown 缺少完成项: %q", md)
	}
	if !containsSubstring(md, "- [→] 步骤二") {
		t.Errorf("Markdown 缺少进行中项: %q", md)
	}
	if !containsSubstring(md, "- [ ] 步骤三") {
		t.Errorf("Markdown 缺少待执行项: %q", md)
	}
}

// TestTaskPlan_ToDict 验证 TaskPlan 序列化
func TestTaskPlan_ToDict(t *testing.T) {
	tp := NewTaskPlan("plan-name", "目标")
	item := NewTodoItem()
	item.Content = "任务1"
	tp.AddTask(item)

	d := tp.ToDict()
	if d["task_name"] != "plan-name" {
		t.Errorf("期望 task_name = %q, 实际 = %v", "plan-name", d["task_name"])
	}
	if d["goal"] != "目标" {
		t.Errorf("期望 goal = %q, 实际 = %v", "目标", d["goal"])
	}
	tasks, ok := d["tasks"].([]any)
	if !ok || len(tasks) != 1 {
		t.Errorf("期望 tasks 长度 = 1, 实际 = %v", d["tasks"])
	}
}

// TestTaskPlan_FromDict 验证从字典反序列化
func TestTaskPlan_FromDict(t *testing.T) {
	data := map[string]any{
		"task_name": "plan-1",
		"goal":      "目标1",
		"tasks": []any{
			map[string]any{
				"id":      "t1",
				"content": "任务1",
				"status":  "pending",
			},
		},
		"current_task_id": "t1",
	}
	tp := TaskPlan{}.FromDict(data)

	if tp.TaskName != "plan-1" {
		t.Errorf("期望 TaskName = %q, 实际 = %q", "plan-1", tp.TaskName)
	}
	if tp.Goal != "目标1" {
		t.Errorf("期望 Goal = %q, 实际 = %q", "目标1", tp.Goal)
	}
	if len(tp.Tasks) != 1 {
		t.Errorf("期望 Tasks 长度 = 1, 实际 = %d", len(tp.Tasks))
	}
	if tp.CurrentTaskID != "t1" {
		t.Errorf("期望 CurrentTaskID = %q, 实际 = %q", "t1", tp.CurrentTaskID)
	}
}

// TestTaskPlan_往返 验证 ToDict → FromDict 往返一致
func TestTaskPlan_往返(t *testing.T) {
	original := NewTaskPlan("rt-task", "往返目标")
	item1 := NewTodoItem()
	item1.Content = "任务1"
	item1.Status = TodoStatusCompleted
	item1.ResultSummary = "完成"
	item2 := NewTodoItem()
	item2.Content = "任务2"
	item2.DependsOn = []string{item1.ID}
	original.AddTask(item1)
	original.AddTask(item2)
	original.CurrentTaskID = item2.ID

	restored := TaskPlan{}.FromDict(original.ToDict())

	if restored.TaskName != original.TaskName {
		t.Errorf("期望 TaskName = %q, 实际 = %q", original.TaskName, restored.TaskName)
	}
	if restored.Goal != original.Goal {
		t.Errorf("期望 Goal = %q, 实际 = %q", original.Goal, restored.Goal)
	}
	if len(restored.Tasks) != len(original.Tasks) {
		t.Errorf("期望 Tasks 长度 = %d, 实际 = %d", len(original.Tasks), len(restored.Tasks))
	}
	if restored.CurrentTaskID != original.CurrentTaskID {
		t.Errorf("期望 CurrentTaskID = %q, 实际 = %q", original.CurrentTaskID, restored.CurrentTaskID)
	}
}

// TestModelUsageRecord_Add 验证 Add 累加
func TestModelUsageRecord_Add(t *testing.T) {
	r := ModelUsageRecord{ModelID: "gpt-4"}
	r.Add(100, 50)
	if r.InputTokens != 100 || r.OutputTokens != 50 {
		t.Errorf("期望 (100, 50), 实际 = (%d, %d)", r.InputTokens, r.OutputTokens)
	}
	r.Add(200, 150)
	if r.InputTokens != 300 || r.OutputTokens != 200 {
		t.Errorf("期望 (300, 200), 实际 = (%d, %d)", r.InputTokens, r.OutputTokens)
	}
}

// TestModelUsageRecord_String 验证 String 格式
func TestModelUsageRecord_String(t *testing.T) {
	r := ModelUsageRecord{
		ModelID:      "gpt-4",
		InputTokens:  100,
		OutputTokens: 200,
	}
	expected := "gpt-4: input=100, output=200"
	if got := r.String(); got != expected {
		t.Errorf("期望 %q, 实际 = %q", expected, got)
	}
}

// TestTaskPlanFromDict 验证 TaskPlanFromDict 包级函数
func TestTaskPlanFromDict(t *testing.T) {
	data := map[string]any{
		"task_name": "fp-task",
		"goal":      "函数目标",
	}
	tp := TaskPlanFromDict(data)
	if !reflect.DeepEqual(tp, TaskPlan{}.FromDict(data)) {
		t.Errorf("TaskPlanFromDict 与 FromDict 结果不一致")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// containsSubstring 检查字符串是否包含子串
func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		(len(s) > 0 && len(sub) > 0 && findSubstring(s, sub)))
}

// findSubstring 在字符串中查找子串
func findSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
