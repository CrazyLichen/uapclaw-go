package schema

import "testing"

// ──────────────────────────── 导出函数 ────────────────────────────

func TestIntentType_值对齐Python(t *testing.T) {
	// 验证 9 个枚举值与 Python IntentType 字符串值完全对齐
	expected := map[IntentType]string{
		IntentCreateTask:    "create_task",
		IntentPauseTask:     "pause_task",
		IntentResumeTask:    "resume_task",
		IntentContinueTask:  "continue_task",
		IntentSupplementTask: "supplement_task",
		IntentCancelTask:    "cancel_task",
		IntentModifyTask:    "modify_task",
		IntentSwitchTask:    "switch_task",
		IntentUnknownTask:   "unknown_task",
	}
	for it, want := range expected {
		if string(it) != want {
			t.Errorf("IntentType 值不对齐: got %q, want %q", string(it), want)
		}
	}
	if len(expected) != 9 {
		t.Errorf("IntentType 枚举数量 = %d, want 9", len(expected))
	}
}
