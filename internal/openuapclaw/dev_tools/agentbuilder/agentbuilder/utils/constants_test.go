package utils

import (
	"regexp"
	"strings"
	"testing"
)

// TestConstants_Workflow话术 对齐 Python TestWorkflowConstants（4 用例）
func TestConstants_Workflow话术(t *testing.T) {
	t.Run("workflow_request_content", func(t *testing.T) {
		if !strings.Contains(strings.ToLower(WorkflowRequestContent), "workflow") {
			t.Errorf("WorkflowRequestContent 应包含 workflow: %q", WorkflowRequestContent)
		}
	})
	t.Run("workflow_design_response_content", func(t *testing.T) {
		if !strings.Contains(WorkflowDesignResponseContent, "Workflow design content") {
			t.Errorf("WorkflowDesignResponseContent 应包含前缀")
		}
	})
	t.Run("generate_dl_from_design_content", func(t *testing.T) {
		if !strings.Contains(GenerateDLFromDesignContent, "Process Definition Language") {
			t.Errorf("GenerateDLFromDesignContent 应包含 Process Definition Language")
		}
	})
	t.Run("modify_dl_content", func(t *testing.T) {
		if !strings.Contains(strings.ToLower(ModifyDLContent), "correct") {
			t.Errorf("ModifyDLContent 应包含 correct")
		}
	})
}

// TestConstants_默认配置 对齐 Python TestDefaultConfiguration（3 用例）
func TestConstants_默认配置(t *testing.T) {
	if DefaultMaxHistorySize != 50 {
		t.Errorf("DefaultMaxHistorySize = %d, 期望 50", DefaultMaxHistorySize)
	}
	if DefaultMaxRetries != 3 {
		t.Errorf("DefaultMaxRetries = %d, 期望 3", DefaultMaxRetries)
	}
	if DefaultTimeout != 30 {
		t.Errorf("DefaultTimeout = %d, 期望 30", DefaultTimeout)
	}
}

// TestConstants_资源类型 对齐 Python TestResourceTypes（3 用例）
func TestConstants_资源类型(t *testing.T) {
	if ResourceTypePlugin != "plugin" {
		t.Errorf("ResourceTypePlugin = %q, 期望 plugin", ResourceTypePlugin)
	}
	if ResourceTypeKnowledge != "knowledge" {
		t.Errorf("ResourceTypeKnowledge = %q, 期望 knowledge", ResourceTypeKnowledge)
	}
	if ResourceTypeWorkflow != "workflow" {
		t.Errorf("ResourceTypeWorkflow = %q, 期望 workflow", ResourceTypeWorkflow)
	}
}

// TestConstants_正则模式 对齐 Python TestRegexPatterns（1 用例）
func TestConstants_正则模式(t *testing.T) {
	re := regexp.MustCompile("(?s)" + JSONExtractPattern)
	cases := []struct {
		text     string
		expected string
	}{
		{"```json\n{\"key\": \"value\"}\n```", "{\"key\": \"value\"}"},
		{"```\n{\"key\": \"value\"}\n```", "{\"key\": \"value\"}"},
		{"```json\n[1, 2, 3]\n```", "[1, 2, 3]"},
	}
	for _, c := range cases {
		m := re.FindStringSubmatch(c.text)
		if len(m) < 2 || strings.TrimSpace(m[1]) != strings.TrimSpace(c.expected) {
			t.Errorf("正则提取 %q 失败, got %v", c.text, m)
		}
	}
}

// TestConstants_API 对齐 Python TestApiConstants（2 用例）
func TestConstants_API(t *testing.T) {
	if APIVersion != "v1" {
		t.Errorf("APIVersion = %q, 期望 v1", APIVersion)
	}
	if APIBasePath != "/api/v1" {
		t.Errorf("APIBasePath = %q, 期望 /api/v1", APIBasePath)
	}
}

// TestConstants_进度 对齐 Python TestProgressConstants（2 用例，untyped const 直接值比较）
func TestConstants_进度(t *testing.T) {
	if ProgressUpdateInterval != 0.1 {
		t.Errorf("ProgressUpdateInterval = %v, 期望 0.1", ProgressUpdateInterval)
	}
	if ProgressHeartbeatInterval != 30.0 {
		t.Errorf("ProgressHeartbeatInterval = %v, 期望 30.0", ProgressHeartbeatInterval)
	}
}

// TestConstants_长度限制 对齐 Python TestLimitConstants（6 用例：5 值 + 1 关系）
func TestConstants_长度限制(t *testing.T) {
	if MaxQueryLength != 5000 {
		t.Errorf("MaxQueryLength = %d, 期望 5000", MaxQueryLength)
	}
	if MinQueryLength != 1 {
		t.Errorf("MinQueryLength = %d, 期望 1", MinQueryLength)
	}
	if MaxSessionIDLength != 255 {
		t.Errorf("MaxSessionIDLength = %d, 期望 255", MaxSessionIDLength)
	}
	if MaxHistorySize != 1000 {
		t.Errorf("MaxHistorySize = %d, 期望 1000", MaxHistorySize)
	}
	if MinHistorySize != 1 {
		t.Errorf("MinHistorySize = %d, 期望 1", MinHistorySize)
	}
	if MinQueryLength > MaxQueryLength || MinHistorySize > MaxHistorySize {
		t.Errorf("长度约束关系非法: MinQuery=%d MaxQuery=%d MinHistory=%d MaxHistory=%d",
			MinQueryLength, MaxQueryLength, MinHistorySize, MaxHistorySize)
	}
}

// TestConstants_常量枚举一致性 对齐 Python system test_constants_enums_consistency 的常量半边
// （枚举半边归 14.2 enums_test.go，届时回填标注）
func TestConstants_常量枚举一致性(t *testing.T) {
	if DefaultMaxHistorySize <= 0 {
		t.Errorf("DefaultMaxHistorySize 应 > 0")
	}
	if MaxQueryLength <= MinQueryLength {
		t.Errorf("MaxQueryLength 应 > MinQueryLength")
	}
}
