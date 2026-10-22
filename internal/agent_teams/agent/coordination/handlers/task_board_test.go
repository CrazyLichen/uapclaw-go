package handlers

import (
	"context"
	"strings"
	"testing"
)

// TestRenderTaskAssignedToSelf_普通成员 测试普通成员任务分配渲染。
// S-附录4: 对齐 Python t("dispatcher.task_assigned_to_self", task_id=...)
func TestRenderTaskAssignedToSelf_普通成员(t *testing.T) {
	result := renderTaskAssignedToSelf(context.Background(), nil, "task-001", false)
	if !strings.Contains(result, "task-001") {
		t.Errorf("内容应包含 task_id=task-001, got %q", result)
	}
	if !strings.Contains(result, "view_task") {
		t.Errorf("普通成员模板应引导 view_task, got %q", result)
	}
}

// TestRenderTaskAssignedToSelf_人类代理 测试人类代理模板渲染
func TestRenderTaskAssignedToSelf_人类代理(t *testing.T) {
	result := renderTaskAssignedToSelf(context.Background(), nil, "task-002", true)
	if !strings.Contains(result, "task-002") {
		t.Errorf("内容应包含 task_id=task-002, got %q", result)
	}
	if !strings.Contains(result, "控制者") {
		t.Errorf("HITT 模板应是给控制者的通知, got %q", result)
	}
}

// TestRenderTaskAssignedToSelf_空任务ID 测试空 taskID 不 panic
func TestRenderTaskAssignedToSelf_空任务ID(t *testing.T) {
	result := renderTaskAssignedToSelf(context.Background(), nil, "", false)
	if result == "" {
		t.Error("不应返回空字符串")
	}
}
