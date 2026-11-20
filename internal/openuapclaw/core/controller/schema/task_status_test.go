package schema

import "testing"

// ──────────────────────────── 导出函数 ────────────────────────────

func TestTaskStatus_IsTerminal(t *testing.T) {
	tests := []struct {
		name   string
		status TaskStatus
		want   bool
	}{
		{"submitted不是终态", TaskSubmitted, false},
		{"working不是终态", TaskWorking, false},
		{"paused不是终态", TaskPaused, false},
		{"inputRequired不是终态", TaskInputRequired, false},
		{"completed是终态", TaskCompleted, true},
		{"canceled是终态", TaskCanceled, true},
		{"failed是终态", TaskFailed, true},
		{"waiting不是终态", TaskWaiting, false},
		{"unknown不是终态", TaskUnknown, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.status.IsTerminal(); got != tt.want {
				t.Errorf("TaskStatus.IsTerminal() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTaskStatus_IsInputRequired(t *testing.T) {
	if !TaskInputRequired.IsInputRequired() {
		t.Error("TaskInputRequired.IsInputRequired() 应为 true")
	}
	if TaskCompleted.IsInputRequired() {
		t.Error("TaskCompleted.IsInputRequired() 应为 false")
	}
}

func TestTaskStatus_值对齐Python(t *testing.T) {
	// 验证枚举值与 Python TaskStatus 字符串值完全对齐
	expected := map[TaskStatus]string{
		TaskSubmitted:     "submitted",
		TaskWorking:       "working",
		TaskPaused:        "paused",
		TaskInputRequired: "input-required",
		TaskCompleted:     "completed",
		TaskCanceled:      "canceled",
		TaskFailed:        "failed",
		TaskWaiting:       "waiting",
		TaskUnknown:       "unknown",
	}
	for status, want := range expected {
		if string(status) != want {
			t.Errorf("TaskStatus 值不对齐: got %q, want %q", string(status), want)
		}
	}
}
