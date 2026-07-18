package trajectory

import "testing"

// TestStepKind_常量值 验证 StepKind 常量对齐 Python Literal
func TestStepKind_常量值(t *testing.T) {
	if StepKindLLM != "llm" {
		t.Errorf("StepKindLLM = %q, want %q", StepKindLLM, "llm")
	}
	if StepKindTool != "tool" {
		t.Errorf("StepKindTool = %q, want %q", StepKindTool, "tool")
	}
}

// TestLLMCallDetail_StepKind 验证 LLMCallDetail 实现 StepDetail 接口
func TestLLMCallDetail_StepKind(t *testing.T) {
	var d StepDetail = &LLMCallDetail{}
	if d.StepKind() != StepKindLLM {
		t.Errorf("LLMCallDetail.StepKind() = %v, want %v", d.StepKind(), StepKindLLM)
	}
}

// TestToolCallDetail_StepKind 验证 ToolCallDetail 实现 StepDetail 接口
func TestToolCallDetail_StepKind(t *testing.T) {
	var d StepDetail = &ToolCallDetail{}
	if d.StepKind() != StepKindTool {
		t.Errorf("ToolCallDetail.StepKind() = %v, want %v", d.StepKind(), StepKindTool)
	}
}

// TestTrajectoryStep_字段 验证 TrajectoryStep 字段赋值
func TestTrajectoryStep_字段(t *testing.T) {
	step := &TrajectoryStep{
		Kind:           StepKindLLM,
		StartTimeMs:    1000,
		EndTimeMs:      2000,
		Detail:         &LLMCallDetail{Model: "qwen-max"},
		Reward:         0.8,
		PromptTokenIDs: []int{1, 2, 3},
		Meta:           map[string]any{"operator_id": "agent1/llm_main"},
	}
	if step.Kind != StepKindLLM {
		t.Errorf("Kind = %v, want %v", step.Kind, StepKindLLM)
	}
	if step.StartTimeMs != 1000 {
		t.Errorf("StartTimeMs = %d, want 1000", step.StartTimeMs)
	}
	if step.Reward != 0.8 {
		t.Errorf("Reward = %f, want 0.8", step.Reward)
	}
}

// TestTrajectory_默认Source 验证 Trajectory 默认 Source 为空（需调用方设置 "offline"）
func TestTrajectory_默认Source(t *testing.T) {
	traj := &Trajectory{ExecutionID: "test-001", Steps: []*TrajectoryStep{}}
	// Go 中 struct 无默认值机制，Source 默认为空字符串
	// 对齐 Python: source: str = "offline" 需在构造时显式设置
	if traj.Source != "" {
		t.Errorf("default Source = %q, want empty string", traj.Source)
	}
}

// TestTrajectory_ToMessages_空轨迹 验证空轨迹返回空列表
func TestTrajectory_ToMessages_空轨迹(t *testing.T) {
	traj := &Trajectory{ExecutionID: "test", Steps: []*TrajectoryStep{}}
	msgs := traj.ToMessages()
	if len(msgs) != 0 {
		t.Errorf("ToMessages on empty trajectory returned %d messages, want 0", len(msgs))
	}
}

// TestTrajectory_ToMessages_只有LLM步骤 验证提取 LLM 步骤消息
func TestTrajectory_ToMessages_只有LLM步骤(t *testing.T) {
	traj := &Trajectory{
		ExecutionID: "test",
		Steps: []*TrajectoryStep{
			{
				Kind: StepKindLLM,
				Detail: &LLMCallDetail{
					Messages: []map[string]any{
					map[string]any{"role": "user", "content": "hello"},
				},
					Response: map[string]any{"role": "assistant", "content": "hi"},
				},
			},
		},
	}
	msgs := traj.ToMessages()
	if len(msgs) != 2 {
		t.Fatalf("ToMessages returned %d messages, want 2", len(msgs))
	}
	if msgs[0]["role"] != "user" {
		t.Errorf("msgs[0][role] = %v, want user", msgs[0]["role"])
	}
	if msgs[1]["role"] != "assistant" {
		t.Errorf("msgs[1][role] = %v, want assistant", msgs[1]["role"])
	}
}

// TestTrajectory_ToMessages_跳过工具步骤 验证工具步骤被跳过
func TestTrajectory_ToMessages_跳过工具步骤(t *testing.T) {
	traj := &Trajectory{
		ExecutionID: "test",
		Steps: []*TrajectoryStep{
			{Kind: StepKindTool, Detail: &ToolCallDetail{ToolName: "search"}},
			{Kind: StepKindLLM, Detail: &LLMCallDetail{
				Messages: []map[string]any{map[string]any{"role": "user", "content": "hi"}},
			}},
		},
	}
	msgs := traj.ToMessages()
	if len(msgs) != 1 {
		t.Errorf("ToMessages returned %d messages, want 1 (tool step skipped)", len(msgs))
	}
}

// TestTrajectory_ToMessages_nilResponse 验证 nil response 不追加消息
func TestTrajectory_ToMessages_nilResponse(t *testing.T) {
	traj := &Trajectory{
		ExecutionID: "test",
		Steps: []*TrajectoryStep{
			{
				Kind:   StepKindLLM,
				Detail: &LLMCallDetail{Messages: []map[string]any{map[string]any{"role": "user", "content": "hi"}}, Response: nil},
			},
		},
	}
	msgs := traj.ToMessages()
	if len(msgs) != 1 {
		t.Errorf("ToMessages returned %d messages, want 1 (nil response not appended)", len(msgs))
	}
}

// TestTrajectory_ToMessages_nilDetail 验证 nil detail 步骤被跳过
func TestTrajectory_ToMessages_nilDetail(t *testing.T) {
	traj := &Trajectory{
		ExecutionID: "test",
		Steps: []*TrajectoryStep{
			{Kind: StepKindLLM, Detail: nil},
		},
	}
	msgs := traj.ToMessages()
	if len(msgs) != 0 {
		t.Errorf("ToMessages returned %d messages, want 0 (nil detail skipped)", len(msgs))
	}
}

// TestTrajectory_ToMessages_detail类型断言失败 验证 Detail 非 LLMCallDetail 时跳过
func TestTrajectory_ToMessages_detail类型断言失败(t *testing.T) {
	traj := &Trajectory{
		ExecutionID: "test",
		Steps: []*TrajectoryStep{
			{Kind: StepKindLLM, Detail: &ToolCallDetail{ToolName: "search"}},
		},
	}
	msgs := traj.ToMessages()
	if len(msgs) != 0 {
		t.Errorf("ToMessages returned %d messages, want 0 (wrong detail type skipped)", len(msgs))
	}
}

// TestCostInfo_用法 验证 CostInfo map 用法
func TestCostInfo_用法(t *testing.T) {
	cost := CostInfo{"input_tokens": 100, "output_tokens": 50}
	if cost["input_tokens"] != 100 {
		t.Errorf("input_tokens = %d, want 100", cost["input_tokens"])
	}
	if cost["output_tokens"] != 50 {
		t.Errorf("output_tokens = %d, want 50", cost["output_tokens"])
	}
}
