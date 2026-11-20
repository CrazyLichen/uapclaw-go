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

// TestEnumIntegration_AgentType字符串转换 对齐 Python system test_agent_type_string_conversion
func TestEnumIntegration_AgentType字符串转换(t *testing.T) {
	if string(AgentTypeLLMAgent) != "llm_agent" {
		t.Errorf("AgentTypeLLMAgent = %q", AgentTypeLLMAgent)
	}
	if string(AgentTypeWorkflow) != "workflow" {
		t.Errorf("AgentTypeWorkflow = %q", AgentTypeWorkflow)
	}
}

// TestEnumIntegration_BuildState状态流转 对齐 Python system test_build_state_transitions
func TestEnumIntegration_BuildState状态流转(t *testing.T) {
	for _, s := range AllBuildStates() {
		if len(string(s)) == 0 {
			t.Errorf("BuildState %q 值为空", s)
		}
	}
}

// TestEnumIntegration_ProgressStage值 对齐 Python system test_progress_stage_values
func TestEnumIntegration_ProgressStage值(t *testing.T) {
	for _, stage := range []ProgressStage{ProgressStageInitializing, ProgressStageClarifying, ProgressStageCompleted} {
		if len(string(stage)) == 0 {
			t.Errorf("ProgressStage %q 值为空", stage)
		}
	}
}

// TestEnumIntegration_ProgressStatus值 对齐 Python system test_progress_status_values
func TestEnumIntegration_ProgressStatus值(t *testing.T) {
	for _, status := range []ProgressStatus{ProgressStatusPending, ProgressStatusRunning, ProgressStatusSuccess, ProgressStatusFailed} {
		if len(string(status)) == 0 {
			t.Errorf("ProgressStatus %q 值为空", status)
		}
	}
}

// TestConstantsEnumsConsistency_枚举半边 对齐 Python system test_constants_enums_consistency 的枚举半边
// （常量半边已随 14.1 并入 constants_test.go；本用例为 14.2 回填义务，spec §5）
func TestConstantsEnumsConsistency_枚举半边(t *testing.T) {
	if len(AllAgentTypes()) < 2 {
		t.Errorf("AgentType 计数 = %d, 应 >= 2", len(AllAgentTypes()))
	}
	if len(AllBuildStates()) < 3 {
		t.Errorf("BuildState 计数 = %d, 应 >= 3", len(AllBuildStates()))
	}
}

// TestParseBuildState_正反例 测试增强（Python 无对应：语言特性级等价物的补测）
func TestParseBuildState_正反例(t *testing.T) {
	for _, s := range []string{"initial", "processing", "completed"} {
		if _, err := ParseBuildState(s); err != nil {
			t.Errorf("ParseBuildState(%q) 不应报错: %v", s, err)
		}
	}
	if _, err := ParseBuildState("invalid"); err == nil {
		t.Errorf("ParseBuildState(invalid) 应返回错误")
	}
}

// TestParseProgressStage_正反例 测试增强（Python 无对应）
func TestParseProgressStage_正反例(t *testing.T) {
	for _, s := range []string{"initializing", "clarifying", "resource_retrieving", "completed", "error", "optimizing", "generating_config", "transforming_dsl", "detecting_intention", "generating_workflow_design", "generating_dl", "validating_dl", "refining_dl", "transforming_mermaid", "transforming_workflow_dsl"} {
		if _, err := ParseProgressStage(s); err != nil {
			t.Errorf("ParseProgressStage(%q) 不应报错: %v", s, err)
		}
	}
	if _, err := ParseProgressStage("no_such_stage"); err == nil {
		t.Errorf("ParseProgressStage(no_such_stage) 应返回错误")
	}
}

// TestParseProgressStatus_正反例 测试增强（Python 无对应）
func TestParseProgressStatus_正反例(t *testing.T) {
	for _, s := range []string{"pending", "running", "success", "failed", "warning"} {
		if _, err := ParseProgressStatus(s); err != nil {
			t.Errorf("ParseProgressStatus(%q) 不应报错: %v", s, err)
		}
	}
	if _, err := ParseProgressStatus("paused"); err == nil {
		t.Errorf("ParseProgressStatus(paused) 应返回错误")
	}
}

// TestAllEnums_声明序 测试增强（Python 无对应：All 函数顺序契约）
func TestAllEnums_声明序(t *testing.T) {
	if got := AllAgentTypes(); got[0] != AgentTypeLLMAgent || got[1] != AgentTypeWorkflow {
		t.Errorf("AllAgentTypes 顺序不符: %v", got)
	}
	if got := AllBuildStates(); got[0] != BuildStateInitial || got[1] != BuildStateProcessing || got[2] != BuildStateCompleted {
		t.Errorf("AllBuildStates 顺序不符: %v", got)
	}
	if got := AllProgressStages(); len(got) != 15 || got[0] != ProgressStageInitializing || got[14] != ProgressStageTransformingWorkflowDSL {
		t.Errorf("AllProgressStages 长度/首尾不符: len=%d", len(got))
	}
	if got := AllProgressStatuses(); len(got) != 5 || got[0] != ProgressStatusPending || got[4] != ProgressStatusWarning {
		t.Errorf("AllProgressStatuses 长度/首尾不符: len=%d", len(got))
	}
}
