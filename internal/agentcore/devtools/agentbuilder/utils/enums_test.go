package utils

import "testing"

// TestAgentType 对齐 Python TestAgentType（4 用例）
func TestAgentType(t *testing.T) {
	t.Run("值断言", func(t *testing.T) {
		if string(AgentTypeLLMAgent) != "llm_agent" {
			t.Errorf("AgentTypeLLMAgent = %q, 期望 llm_agent", AgentTypeLLMAgent)
		}
		if string(AgentTypeWorkflow) != "workflow" {
			t.Errorf("AgentTypeWorkflow = %q, 期望 workflow", AgentTypeWorkflow)
		}
	})
	t.Run("is_string_enum", func(t *testing.T) {
		// isinstance(X, str) → Go 类型转换即等价物
		if AgentTypeLLMAgent != AgentType("llm_agent") {
			t.Errorf("AgentTypeLLMAgent 应可与字符串互转")
		}
		if AgentTypeWorkflow != AgentType("workflow") {
			t.Errorf("AgentTypeWorkflow 应可与字符串互转")
		}
	})
	t.Run("from_string", func(t *testing.T) {
		got, err := ParseAgentType("llm_agent")
		if err != nil || got != AgentTypeLLMAgent {
			t.Errorf("ParseAgentType(llm_agent) = %v, %v", got, err)
		}
		got, err = ParseAgentType("workflow")
		if err != nil || got != AgentTypeWorkflow {
			t.Errorf("ParseAgentType(workflow) = %v, %v", got, err)
		}
	})
	t.Run("invalid_value", func(t *testing.T) {
		// pytest.raises(ValueError) → assert err != nil
		_, err := ParseAgentType("invalid_type")
		if err == nil {
			t.Errorf("ParseAgentType(invalid_type) 应返回错误")
		}
	})
}

// TestBuildState 对齐 Python TestBuildState（3 用例）
func TestBuildState(t *testing.T) {
	t.Run("值断言", func(t *testing.T) {
		cases := []struct {
			got  BuildState
			want string
		}{
			{BuildStateInitial, "initial"},
			{BuildStateProcessing, "processing"},
			{BuildStateCompleted, "completed"},
		}
		for _, c := range cases {
			if string(c.got) != c.want {
				t.Errorf("BuildState = %q, 期望 %q", c.got, c.want)
			}
		}
	})
	t.Run("is_string_enum", func(t *testing.T) {
		if BuildStateInitial != BuildState("initial") || BuildStateProcessing != BuildState("processing") || BuildStateCompleted != BuildState("completed") {
			t.Errorf("BuildState 应可与字符串互转")
		}
	})
	t.Run("from_string", func(t *testing.T) {
		for _, s := range []string{"initial", "processing", "completed"} {
			if _, err := ParseBuildState(s); err != nil {
				t.Errorf("ParseBuildState(%q) 不应报错: %v", s, err)
			}
		}
	})
}

// TestProgressStage 对齐 Python TestProgressStage（2 用例）
func TestProgressStage(t *testing.T) {
	t.Run("值断言", func(t *testing.T) {
		cases := []struct {
			got  ProgressStage
			want string
		}{
			{ProgressStageInitializing, "initializing"},
			{ProgressStageClarifying, "clarifying"},
			{ProgressStageResourceRetrieving, "resource_retrieving"},
			{ProgressStageCompleted, "completed"},
			{ProgressStageError, "error"},
			{ProgressStageOptimizing, "optimizing"},
			{ProgressStageGeneratingConfig, "generating_config"},
			{ProgressStageTransformingDSL, "transforming_dsl"},
			{ProgressStageDetectingIntention, "detecting_intention"},
			{ProgressStageGeneratingWorkflowDesign, "generating_workflow_design"},
			{ProgressStageGeneratingDL, "generating_dl"},
			{ProgressStageValidatingDL, "validating_dl"},
			{ProgressStageRefiningDL, "refining_dl"},
			{ProgressStageTransformingMermaid, "transforming_mermaid"},
			{ProgressStageTransformingWorkflowDSL, "transforming_workflow_dsl"},
		}
		for _, c := range cases {
			if string(c.got) != c.want {
				t.Errorf("ProgressStage = %q, 期望 %q", c.got, c.want)
			}
		}
	})
	t.Run("is_string_enum", func(t *testing.T) {
		if ProgressStageInitializing != ProgressStage("initializing") || ProgressStageCompleted != ProgressStage("completed") {
			t.Errorf("ProgressStage 应可与字符串互转")
		}
	})
}

// TestProgressStatus 对齐 Python TestProgressStatus（2 用例）
func TestProgressStatus(t *testing.T) {
	t.Run("值断言", func(t *testing.T) {
		cases := []struct {
			got  ProgressStatus
			want string
		}{
			{ProgressStatusPending, "pending"},
			{ProgressStatusRunning, "running"},
			{ProgressStatusSuccess, "success"},
			{ProgressStatusFailed, "failed"},
			{ProgressStatusWarning, "warning"},
		}
		for _, c := range cases {
			if string(c.got) != c.want {
				t.Errorf("ProgressStatus = %q, 期望 %q", c.got, c.want)
			}
		}
	})
	t.Run("is_string_enum", func(t *testing.T) {
		if ProgressStatusPending != ProgressStatus("pending") || ProgressStatusSuccess != ProgressStatus("success") {
			t.Errorf("ProgressStatus 应可与字符串互转")
		}
	})
}

// TestAgentTypeLiteral 对齐 Python TestAgentTypeLiteral（1 用例）
// Python AgentTypeLiteral 为纯静态类型别名（零运行时行为，spec §4-1 显式偏差：
// 无 Go 对应物）；本用例转为 AllAgentTypes 集合断言（语义等价，spec §4-5）。
func TestAgentTypeLiteral(t *testing.T) {
	got := AllAgentTypes()
	want := map[AgentType]bool{AgentTypeLLMAgent: true, AgentTypeWorkflow: true}
	if len(got) != 2 {
		t.Errorf("AllAgentTypes 长度 = %d, 期望 2", len(got))
	}
	for _, v := range got {
		if !want[v] {
			t.Errorf("AllAgentTypes 含意外值 %q", v)
		}
	}
}
