package schema

import (
	"encoding/json"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestTask_字段校验_正常(t *testing.T) {
	task := &Task{
		SessionID: "sess-1",
		TaskID:    "task-1",
		TaskType:  "chat",
		Priority:  0,
		Status:    TaskSubmitted,
	}
	if err := ValidateTask(task); err != nil {
		t.Errorf("ValidateTask() 返回错误: %v", err)
	}
}

func TestTask_字段校验_缺少TaskID(t *testing.T) {
	task := &Task{
		SessionID: "sess-1",
		TaskID:    "",
		TaskType:  "chat",
		Status:    TaskSubmitted,
	}
	if err := ValidateTask(task); err == nil {
		t.Error("缺少 task_id 时 ValidateTask() 应返回错误")
	}
}

func TestTask_字段校验_缺少SessionID(t *testing.T) {
	task := &Task{
		SessionID: "",
		TaskID:    "task-1",
		TaskType:  "chat",
		Status:    TaskSubmitted,
	}
	if err := ValidateTask(task); err == nil {
		t.Error("缺少 session_id 时 ValidateTask() 应返回错误")
	}
}

func TestTask_字段校验_缺少TaskType(t *testing.T) {
	task := &Task{
		SessionID: "sess-1",
		TaskID:    "task-1",
		TaskType:  "",
		Status:    TaskSubmitted,
	}
	if err := ValidateTask(task); err == nil {
		t.Error("缺少 task_type 时 ValidateTask() 应返回错误")
	}
}

func TestTask_字段校验_优先级为负(t *testing.T) {
	task := &Task{
		SessionID: "sess-1",
		TaskID:    "task-1",
		TaskType:  "chat",
		Priority:  -1,
		Status:    TaskSubmitted,
	}
	if err := ValidateTask(task); err == nil {
		t.Error("优先级为负数时 ValidateTask() 应返回错误")
	}
}

func TestTask_字段校验_自引用(t *testing.T) {
	task := &Task{
		SessionID:    "sess-1",
		TaskID:       "task-1",
		TaskType:     "chat",
		ParentTaskID: "task-1", // 等于 TaskID
		Status:       TaskSubmitted,
	}
	if err := ValidateTask(task); err == nil {
		t.Error("parent_task_id 等于 task_id 时 ValidateTask() 应返回错误")
	}
}

func TestTask_字段校验_FAILED缺ErrorMessage(t *testing.T) {
	task := &Task{
		SessionID: "sess-1",
		TaskID:    "task-1",
		TaskType:  "chat",
		Status:    TaskFailed,
		// ErrorMessage 为空
	}
	if err := ValidateTask(task); err == nil {
		t.Error("状态为 failed 且 error_message 为空时 ValidateTask() 应返回错误")
	}
}

func TestTask_字段校验_INPUT_REQUIRED缺InputRequiredFields(t *testing.T) {
	task := &Task{
		SessionID: "sess-1",
		TaskID:    "task-1",
		TaskType:  "chat",
		Status:    TaskInputRequired,
		// InputRequiredFields 为空
	}
	if err := ValidateTask(task); err == nil {
		t.Error("状态为 input-required 且 input_required_fields 为空时 ValidateTask() 应返回错误")
	}
}

func TestTask_JSON序列化_roundTrip(t *testing.T) {
	original := &Task{
		SessionID:    "sess-1",
		TaskID:       "task-1",
		TaskType:     "chat",
		Description:  "测试任务",
		Priority:     1,
		Status:       TaskWorking,
		ParentTaskID: "parent-1",
		ContextID:    "ctx-1",
		ErrorMessage: "",
		Metadata:     map[string]any{"key": "val"},
		Extensions:   map[string]any{"ext": 42},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}

	var parsed Task
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}

	if parsed.SessionID != original.SessionID {
		t.Errorf("SessionID = %q, want %q", parsed.SessionID, original.SessionID)
	}
	if parsed.TaskID != original.TaskID {
		t.Errorf("TaskID = %q, want %q", parsed.TaskID, original.TaskID)
	}
	if parsed.TaskType != original.TaskType {
		t.Errorf("TaskType = %q, want %q", parsed.TaskType, original.TaskType)
	}
	if parsed.Status != original.Status {
		t.Errorf("Status = %q, want %q", parsed.Status, original.Status)
	}
	if parsed.ParentTaskID != original.ParentTaskID {
		t.Errorf("ParentTaskID = %q, want %q", parsed.ParentTaskID, original.ParentTaskID)
	}
}

func TestTask_JSON序列化_Inputs多态(t *testing.T) {
	inputEvt, _ := FromUserInput("用户输入")
	task := &Task{
		SessionID: "sess-1",
		TaskID:    "task-1",
		TaskType:  "chat",
		Status:    TaskSubmitted,
		Inputs:    []Event{inputEvt},
	}

	data, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}

	var parsed Task
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}

	if len(parsed.Inputs) != 1 {
		t.Fatalf("Inputs 长度 = %d, want 1", len(parsed.Inputs))
	}
	if parsed.Inputs[0].GetEventType() != EventInput {
		t.Errorf("Inputs[0] 类型 = %q, want %q", parsed.Inputs[0].GetEventType(), EventInput)
	}
	input, ok := parsed.Inputs[0].(*InputEvent)
	if !ok {
		t.Fatal("Inputs[0] 不是 *InputEvent")
	}
	if len(input.InputData) != 1 {
		t.Fatalf("InputData 长度 = %d, want 1", len(input.InputData))
	}
	textDf, ok := input.InputData[0].(*TextDataFrame)
	if !ok {
		t.Fatal("InputData[0] 不是 *TextDataFrame")
	}
	if textDf.Text != "用户输入" {
		t.Errorf("Text = %q, want %q", textDf.Text, "用户输入")
	}
}

func TestNewTask(t *testing.T) {
	task := NewTask("sess-1", "chat")
	if task.SessionID != "sess-1" {
		t.Errorf("SessionID = %q, want %q", task.SessionID, "sess-1")
	}
	if task.TaskID == "" {
		t.Error("TaskID 不应为空")
	}
	if task.TaskType != "chat" {
		t.Errorf("TaskType = %q, want %q", task.TaskType, "chat")
	}
	if task.Status != TaskSubmitted {
		t.Errorf("Status = %q, want %q", task.Status, TaskSubmitted)
	}
	if task.Priority != 0 {
		t.Errorf("Priority = %d, want 0", task.Priority)
	}
}

// TestTask_String 测试 String() 方法
func TestTask_String(t *testing.T) {
	task := &Task{
		TaskID:    "task-1",
		SessionID: "sess-1",
		TaskType:  "chat",
		Status:    TaskWorking,
	}
	s := task.String()
	if s == "" {
		t.Error("String() 返回空字符串")
	}
	// 验证关键信息在输出中
	if !contains(s, "task-1") {
		t.Errorf("String() = %q, 应包含 task-1", s)
	}
	if !contains(s, "sess-1") {
		t.Errorf("String() = %q, 应包含 sess-1", s)
	}
	if !contains(s, "chat") {
		t.Errorf("String() = %q, 应包含 chat", s)
	}
	if !contains(s, "working") {
		t.Errorf("String() = %q, 应包含 working", s)
	}
}

// contains 检查字符串是否包含子串
func contains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
