package schema

import (
	"reflect"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestPlanModeState_默认值 验证 NewPlanModeState 返回的默认值
func TestPlanModeState_默认值(t *testing.T) {
	s := NewPlanModeState()
	if s.Mode != "normal" {
		t.Errorf("期望 Mode = %q, 实际 = %q", "normal", s.Mode)
	}
	if s.PrePlanMode != "normal" {
		t.Errorf("期望 PrePlanMode = %q, 实际 = %q", "normal", s.PrePlanMode)
	}
	if s.PlanSlug != "" {
		t.Errorf("期望 PlanSlug 为空, 实际 = %q", s.PlanSlug)
	}
	if s.PromptContext != "" {
		t.Errorf("期望 PromptContext 为空, 实际 = %q", s.PromptContext)
	}
}

// TestPlanModeState_ToDict 验证 PlanModeState 序列化
func TestPlanModeState_ToDict(t *testing.T) {
	s := PlanModeState{
		Mode:          "plan",
		PrePlanMode:   "normal",
		PlanSlug:      "gleaming-brewing-phoenix",
		PromptContext: "legacy",
	}
	d := s.ToDict()

	if d["mode"] != "plan" {
		t.Errorf("期望 mode = %q, 实际 = %v", "plan", d["mode"])
	}
	if d["pre_plan_mode"] != "normal" {
		t.Errorf("期望 pre_plan_mode = %q, 实际 = %v", "normal", d["pre_plan_mode"])
	}
	if d["plan_slug"] != "gleaming-brewing-phoenix" {
		t.Errorf("期望 plan_slug = %q, 实际 = %v", "gleaming-brewing-phoenix", d["plan_slug"])
	}
	if d["prompt_context"] != "legacy" {
		t.Errorf("期望 prompt_context = %q, 实际 = %v", "legacy", d["prompt_context"])
	}

	// 验证包含所有预期键
	expectedKeys := []string{"mode", "pre_plan_mode", "plan_slug", "prompt_context"}
	for _, key := range expectedKeys {
		if _, ok := d[key]; !ok {
			t.Errorf("缺少键 %q", key)
		}
	}
}

// TestPlanModeState_FromDict 验证从字典反序列化
func TestPlanModeState_FromDict(t *testing.T) {
	data := map[string]any{
		"mode":           "plan",
		"pre_plan_mode":  "normal",
		"plan_slug":      "my-plan",
		"prompt_context": "ctx",
	}
	s := PlanModeState{}.FromDict(data)

	if s.Mode != "plan" {
		t.Errorf("期望 Mode = %q, 实际 = %q", "plan", s.Mode)
	}
	if s.PrePlanMode != "normal" {
		t.Errorf("期望 PrePlanMode = %q, 实际 = %q", "normal", s.PrePlanMode)
	}
	if s.PlanSlug != "my-plan" {
		t.Errorf("期望 PlanSlug = %q, 实际 = %q", "my-plan", s.PlanSlug)
	}
	if s.PromptContext != "ctx" {
		t.Errorf("期望 PromptContext = %q, 实际 = %q", "ctx", s.PromptContext)
	}
}

// TestPlanModeState_FromDict_Nil 验证 nil 输入返回默认值
func TestPlanModeState_FromDict_Nil(t *testing.T) {
	s := PlanModeState{}.FromDict(nil)
	expected := NewPlanModeState()

	if s.Mode != expected.Mode {
		t.Errorf("期望 Mode = %q, 实际 = %q", expected.Mode, s.Mode)
	}
	if s.PrePlanMode != expected.PrePlanMode {
		t.Errorf("期望 PrePlanMode = %q, 实际 = %q", expected.PrePlanMode, s.PrePlanMode)
	}
}

// TestPlanModeState_往返 验证 ToDict → FromDict 往返一致
func TestPlanModeState_往返(t *testing.T) {
	original := PlanModeState{
		Mode:          "plan",
		PrePlanMode:   "normal",
		PlanSlug:      "test-slug",
		PromptContext: "test-ctx",
	}
	restored := PlanModeState{}.FromDict(original.ToDict())

	if !reflect.DeepEqual(original, restored) {
		t.Errorf("往返不一致: 原始 = %+v, 恢复 = %+v", original, restored)
	}
}

// TestDeepAgentState_默认值 验证 NewDeepAgentState 返回的默认值
func TestDeepAgentState_默认值(t *testing.T) {
	s := NewDeepAgentState()
	if s.Iteration != 0 {
		t.Errorf("期望 Iteration = 0, 实际 = %d", s.Iteration)
	}
	if s.TaskPlan != nil {
		t.Errorf("期望 TaskPlan 为 nil, 实际 = %v", s.TaskPlan)
	}
	if s.StopConditionState != nil {
		t.Errorf("期望 StopConditionState 为 nil")
	}
	if len(s.PendingFollowUps) != 0 {
		t.Errorf("期望 PendingFollowUps 为空")
	}
	if s.PlanMode.Mode != "normal" {
		t.Errorf("期望 PlanMode.Mode = %q, 实际 = %q", "normal", s.PlanMode.Mode)
	}
}

// TestDeepAgentState_ToSessionDict 验证 DeepAgentState 序列化包含 deepagent 键
func TestDeepAgentState_ToSessionDict(t *testing.T) {
	s := DeepAgentState{
		Iteration: 3,
		PlanMode: PlanModeState{
			Mode:        "plan",
			PrePlanMode: "normal",
			PlanSlug:    "my-slug",
		},
		PendingFollowUps:   []string{"follow-up-1"},
		StopConditionState: map[string]any{"key": "value"},
	}
	d := s.ToSessionDict()

	// 必须包含 deepagent 键
	inner, ok := d["deepagent"].(map[string]any)
	if !ok {
		t.Fatal("缺少 'deepagent' 键或类型不正确")
	}

	if inner["iteration"] != 3 {
		t.Errorf("期望 iteration = 3, 实际 = %v", inner["iteration"])
	}
	if inner["task_plan"] != nil {
		t.Errorf("期望 task_plan = nil, 实际 = %v", inner["task_plan"])
	}
	if inner["pending_follow_ups"] == nil {
		t.Error("期望 pending_follow_ups 不为 nil")
	}
	if inner["stop_condition_state"] == nil {
		t.Error("期望 stop_condition_state 不为 nil")
	}
	if inner["plan_mode"] == nil {
		t.Error("期望 plan_mode 不为 nil")
	}
}

// TestDeepAgentState_FromSessionDict 验证从会话字典反序列化
func TestDeepAgentState_FromSessionDict(t *testing.T) {
	data := map[string]any{
		"deepagent": map[string]any{
			"iteration": 5,
			"task_plan": nil,
			"stop_condition_state": map[string]any{
				"condition": "met",
			},
			"pending_follow_ups": []any{"item1", "item2"},
			"plan_mode": map[string]any{
				"mode":          "plan",
				"pre_plan_mode": "normal",
				"plan_slug":     "test-slug",
			},
		},
	}
	s := DeepAgentState{}.FromSessionDict(data)

	if s.Iteration != 5 {
		t.Errorf("期望 Iteration = 5, 实际 = %d", s.Iteration)
	}
	if s.TaskPlan != nil {
		t.Errorf("期望 TaskPlan 为 nil, 实际 = %v", s.TaskPlan)
	}
	if len(s.PendingFollowUps) != 2 {
		t.Errorf("期望 PendingFollowUps 长度 = 2, 实际 = %d", len(s.PendingFollowUps))
	}
	if s.PlanMode.Mode != "plan" {
		t.Errorf("期望 PlanMode.Mode = %q, 实际 = %q", "plan", s.PlanMode.Mode)
	}
	if s.PlanMode.PlanSlug != "test-slug" {
		t.Errorf("期望 PlanMode.PlanSlug = %q, 实际 = %q", "test-slug", s.PlanMode.PlanSlug)
	}
}

// TestDeepAgentState_FromSessionDict_Nil 验证 nil 输入返回默认值
func TestDeepAgentState_FromSessionDict_Nil(t *testing.T) {
	s := DeepAgentState{}.FromSessionDict(nil)
	expected := NewDeepAgentState()

	if s.Iteration != expected.Iteration {
		t.Errorf("期望 Iteration = %d, 实际 = %d", expected.Iteration, s.Iteration)
	}
	if s.PlanMode.Mode != expected.PlanMode.Mode {
		t.Errorf("期望 PlanMode.Mode = %q, 实际 = %q", expected.PlanMode.Mode, s.PlanMode.Mode)
	}
}

// TestDeepAgentState_往返 验证 ToSessionDict → FromSessionDict 往返一致
func TestDeepAgentState_往返(t *testing.T) {
	original := DeepAgentState{
		Iteration: 2,
		PlanMode: PlanModeState{
			Mode:          "plan",
			PrePlanMode:   "normal",
			PlanSlug:      "round-trip-slug",
			PromptContext: "ctx",
		},
		PendingFollowUps:   []string{"a", "b"},
		StopConditionState: map[string]any{"k": "v"},
	}
	restored := DeepAgentState{}.FromSessionDict(original.ToSessionDict())

	if restored.Iteration != original.Iteration {
		t.Errorf("期望 Iteration = %d, 实际 = %d", original.Iteration, restored.Iteration)
	}
	if restored.PlanMode.Mode != original.PlanMode.Mode {
		t.Errorf("期望 PlanMode.Mode = %q, 实际 = %q", original.PlanMode.Mode, restored.PlanMode.Mode)
	}
	if restored.PlanMode.PlanSlug != original.PlanMode.PlanSlug {
		t.Errorf("期望 PlanMode.PlanSlug = %q, 实际 = %q", original.PlanMode.PlanSlug, restored.PlanMode.PlanSlug)
	}
	if restored.PlanMode.PromptContext != original.PlanMode.PromptContext {
		t.Errorf("期望 PlanMode.PromptContext = %q, 实际 = %q", original.PlanMode.PromptContext, restored.PlanMode.PromptContext)
	}
	if len(restored.PendingFollowUps) != len(original.PendingFollowUps) {
		t.Errorf("期望 PendingFollowUps 长度 = %d, 实际 = %d", len(original.PendingFollowUps), len(restored.PendingFollowUps))
	}
}

// TestDeepAgentState_包含TaskPlan 验证 TaskPlan 非空时的序列化与反序列化
func TestDeepAgentState_包含TaskPlan(t *testing.T) {
	// 构造一个简单的 TaskPlan（依赖 task.go 中的定义）
	tp := NewTaskPlan("test-task", "test goal")
	original := DeepAgentState{
		Iteration: 1,
		TaskPlan:  &tp,
		PlanMode:  NewPlanModeState(),
	}
	d := original.ToSessionDict()

	// 验证 task_plan 不为 nil
	inner, ok := d["deepagent"].(map[string]any)
	if !ok {
		t.Fatal("缺少 'deepagent' 键")
	}
	if inner["task_plan"] == nil {
		t.Error("期望 task_plan 不为 nil")
	}

	// 反序列化
	restored := DeepAgentState{}.FromSessionDict(d)
	if restored.TaskPlan == nil {
		t.Error("期望恢复后 TaskPlan 不为 nil")
	}
	if restored.TaskPlan.TaskName != "test-task" {
		t.Errorf("期望 TaskName = %q, 实际 = %q", "test-task", restored.TaskPlan.TaskName)
	}
}
