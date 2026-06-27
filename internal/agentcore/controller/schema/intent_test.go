//go:build test

package schema

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestIntentType_值对齐Python 验证 9 个枚举值与 Python IntentType 字符串值完全对齐。
func TestIntentType_值对齐Python(t *testing.T) {
	expected := map[IntentType]string{
		IntentCreateTask:     "create_task",
		IntentPauseTask:      "pause_task",
		IntentResumeTask:     "resume_task",
		IntentContinueTask:   "continue_task",
		IntentSupplementTask: "supplement_task",
		IntentCancelTask:     "cancel_task",
		IntentModifyTask:     "modify_task",
		IntentSwitchTask:     "switch_task",
		IntentUnknownTask:    "unknown_task",
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

// TestNewIntent_创建成功 测试合法意图创建。
func TestNewIntent_创建成功(t *testing.T) {
	event := &InputEvent{BaseEvent: BaseEvent{EventID: "e1"}}
	intent, err := NewIntent(IntentCreateTask, event,
		WithTargetTaskDescription("做某事"),
	)
	assert.NoError(t, err)
	assert.Equal(t, IntentCreateTask, intent.IntentType)
	assert.Equal(t, "e1", intent.Event.GetEventID())
	assert.Equal(t, "做某事", intent.TargetTaskDescription)
	assert.Equal(t, 1.0, intent.Confidence)
}

// TestNewIntent_置信度越界 测试置信度超出 [0.0, 1.0] 范围返回错误。
func TestNewIntent_置信度越界(t *testing.T) {
	event := &InputEvent{BaseEvent: BaseEvent{EventID: "e1"}}
	_, err := NewIntent(IntentCreateTask, event,
		WithTargetTaskDescription("做某事"),
		WithConfidence(1.5),
	)
	assert.Error(t, err)
}

// TestValidate_CREATE_TASK缺描述 测试 CREATE_TASK 缺少 target_task_description。
func TestValidate_CREATE_TASK缺描述(t *testing.T) {
	event := &InputEvent{BaseEvent: BaseEvent{EventID: "e1"}}
	intent := &Intent{IntentType: IntentCreateTask, Event: event, Confidence: 1.0}
	err := intent.Validate()
	assert.Error(t, err)
}

// TestValidate_CONTINUE_TASK缺依赖 测试 CONTINUE_TASK 缺少 depend_task_id。
func TestValidate_CONTINUE_TASK缺依赖(t *testing.T) {
	event := &InputEvent{BaseEvent: BaseEvent{EventID: "e1"}}
	intent := &Intent{IntentType: IntentContinueTask, Event: event, Confidence: 1.0}
	err := intent.Validate()
	assert.Error(t, err)
}

// TestValidate_SUPPLEMENT_TASK缺字段 测试 SUPPLEMENT_TASK 缺少必填字段。
func TestValidate_SUPPLEMENT_TASK缺字段(t *testing.T) {
	event := &InputEvent{BaseEvent: BaseEvent{EventID: "e1"}}
	// 缺 target_task_id
	intent := &Intent{IntentType: IntentSupplementTask, Event: event, Confidence: 1.0, SupplementaryInfo: "info"}
	assert.Error(t, intent.Validate())
	// 缺 supplementary_info
	intent2 := &Intent{IntentType: IntentSupplementTask, Event: event, Confidence: 1.0, TargetTaskID: "t1"}
	assert.Error(t, intent2.Validate())
}

// TestValidate_MODIFY_TASK缺字段 测试 MODIFY_TASK 缺少必填字段。
func TestValidate_MODIFY_TASK缺字段(t *testing.T) {
	event := &InputEvent{BaseEvent: BaseEvent{EventID: "e1"}}
	intent := &Intent{IntentType: IntentModifyTask, Event: event, Confidence: 1.0, ModificationDetails: "改"}
	assert.Error(t, intent.Validate())
}

// TestValidate_PAUSE_TASK缺ID 测试 PAUSE_TASK/RESUME_TASK/CANCEL_TASK 缺少 target_task_id。
func TestValidate_PAUSE_TASK缺ID(t *testing.T) {
	event := &InputEvent{BaseEvent: BaseEvent{EventID: "e1"}}
	for _, it := range []IntentType{IntentPauseTask, IntentResumeTask, IntentCancelTask} {
		intent := &Intent{IntentType: it, Event: event, Confidence: 1.0}
		assert.Error(t, intent.Validate(), "IntentType=%s should require target_task_id", it)
	}
}

// TestValidate_SWITCH_TASK缺描述 测试 SWITCH_TASK 缺少 target_task_description。
func TestValidate_SWITCH_TASK缺描述(t *testing.T) {
	event := &InputEvent{BaseEvent: BaseEvent{EventID: "e1"}}
	intent := &Intent{IntentType: IntentSwitchTask, Event: event, Confidence: 1.0}
	assert.Error(t, intent.Validate())
}

// TestValidate_UNKNOWN_TASK缺提示 测试 UNKNOWN_TASK 缺少 clarification_prompt。
func TestValidate_UNKNOWN_TASK缺提示(t *testing.T) {
	event := &InputEvent{BaseEvent: BaseEvent{EventID: "e1"}}
	intent := &Intent{IntentType: IntentUnknownTask, Event: event, Confidence: 1.0}
	assert.Error(t, intent.Validate())
}

// TestValidate_各类型合法 测试各意图类型满足必填字段后 Validate 通过。
func TestValidate_各类型合法(t *testing.T) {
	event := &InputEvent{BaseEvent: BaseEvent{EventID: "e1"}}
	cases := []struct {
		name   string
		intent *Intent
	}{
		{"CREATE_TASK", &Intent{IntentType: IntentCreateTask, Event: event, TargetTaskDescription: "做", Confidence: 1.0}},
		{"CONTINUE_TASK", &Intent{IntentType: IntentContinueTask, Event: event, DependTaskID: []string{"t1"}, Confidence: 1.0}},
		{"SUPPLEMENT_TASK", &Intent{IntentType: IntentSupplementTask, Event: event, TargetTaskID: "t1", SupplementaryInfo: "补", Confidence: 1.0}},
		{"MODIFY_TASK", &Intent{IntentType: IntentModifyTask, Event: event, TargetTaskID: "t1", ModificationDetails: "改", Confidence: 1.0}},
		{"PAUSE_TASK", &Intent{IntentType: IntentPauseTask, Event: event, TargetTaskID: "t1", Confidence: 1.0}},
		{"RESUME_TASK", &Intent{IntentType: IntentResumeTask, Event: event, TargetTaskID: "t1", Confidence: 1.0}},
		{"CANCEL_TASK", &Intent{IntentType: IntentCancelTask, Event: event, TargetTaskID: "t1", Confidence: 1.0}},
		{"SWITCH_TASK", &Intent{IntentType: IntentSwitchTask, Event: event, TargetTaskDescription: "切", Confidence: 1.0}},
		{"UNKNOWN_TASK", &Intent{IntentType: IntentUnknownTask, Event: event, ClarificationPrompt: "请澄清", Confidence: 1.0}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.NoError(t, c.intent.Validate())
		})
	}
}
