package handlers

import (
	"testing"
)

// TestFormatTaskAssignedToSelf_普通成员 测试普通成员任务分配格式
func TestFormatTaskAssignedToSelf_普通成员(t *testing.T) {
	payload := map[string]any{
		"task_id":    "task-001",
		"task_title": "实现功能A",
	}
	result := formatTaskAssignedToSelf(payload, false)

	if result["task_id"] != "task-001" {
		t.Errorf("task_id = %v, want task-001", result["task_id"])
	}
	if result["task_title"] != "实现功能A" {
		t.Errorf("task_title = %v, want 实现功能A", result["task_title"])
	}
	if result["_template"] != "task_assigned_to_self" {
		t.Errorf("_template = %v, want task_assigned_to_self", result["_template"])
	}
	if result["_human"] != false {
		t.Errorf("_human = %v, want false", result["_human"])
	}
}

// TestFormatTaskAssignedToSelf_人类代理 测试人类代理任务分配格式
func TestFormatTaskAssignedToSelf_人类代理(t *testing.T) {
	payload := map[string]any{
		"task_id":    "task-002",
		"task_title": "审批文档",
	}
	result := formatTaskAssignedToSelf(payload, true)

	if result["_human"] != true {
		t.Errorf("_human = %v, want true", result["_human"])
	}
}

// TestFormatTaskAssignedToSelf_缺失字段 测试缺失字段返回零值
func TestFormatTaskAssignedToSelf_缺失字段(t *testing.T) {
	payload := map[string]any{}
	result := formatTaskAssignedToSelf(payload, false)

	if result["task_id"] != "" {
		t.Errorf("task_id = %v, want 空字符串", result["task_id"])
	}
	if result["task_title"] != "" {
		t.Errorf("task_title = %v, want 空字符串", result["task_title"])
	}
}

// TestFormatTaskAssignedToSelf_错误类型 测试字段类型错误时返回零值
func TestFormatTaskAssignedToSelf_错误类型(t *testing.T) {
	payload := map[string]any{
		"task_id":    123,
		"task_title": 456,
	}
	result := formatTaskAssignedToSelf(payload, false)

	if result["task_id"] != "" {
		t.Errorf("task_id 类型错误时应返回空字符串, got %v", result["task_id"])
	}
	if result["task_title"] != "" {
		t.Errorf("task_title 类型错误时应返回空字符串, got %v", result["task_title"])
	}
}
