package prompts

import (
	"strings"
	"testing"
)

// TestTeamPlanAgentDesc 测试中英文描述常量
func TestTeamPlanAgentDesc(t *testing.T) {
	if TeamPlanAgentDesc["cn"] == "" {
		t.Fatal("TeamPlanAgentDesc cn is empty")
	}
	if TeamPlanAgentDesc["en"] == "" {
		t.Fatal("TeamPlanAgentDesc en is empty")
	}
}

// TestTeamPlanAgentSystemPrompt 测试系统提示词加载
func TestTeamPlanAgentSystemPrompt(t *testing.T) {
	cnPrompt := TeamPlanAgentSystemPrompt("cn")
	if cnPrompt == "" {
		t.Fatal("TeamPlanAgentSystemPrompt cn is empty")
	}
	enPrompt := TeamPlanAgentSystemPrompt("en")
	if enPrompt == "" {
		t.Fatal("TeamPlanAgentSystemPrompt en is empty")
	}
}

// TestTeamPlanAgentDescription 测试描述函数
func TestTeamPlanAgentDescription(t *testing.T) {
	cnDesc := TeamPlanAgentDescription("cn")
	if cnDesc == "" {
		t.Fatal("TeamPlanAgentDescription cn is empty")
	}
	enDesc := TeamPlanAgentDescription("en")
	if enDesc == "" {
		t.Fatal("TeamPlanAgentDescription en is empty")
	}
	// 未知语言回退到中文
	fallbackDesc := TeamPlanAgentDescription("fr")
	if fallbackDesc != TeamPlanAgentDesc["cn"] {
		t.Fatal("unknown language should fallback to cn")
	}
}

// TestDefaultTeamPlanAgentSystemPrompt 测试默认提示词映射
func TestDefaultTeamPlanAgentSystemPrompt(t *testing.T) {
	if DefaultTeamPlanAgentSystemPrompt["cn"] == "" {
		t.Fatal("DefaultTeamPlanAgentSystemPrompt cn is empty")
	}
	if DefaultTeamPlanAgentSystemPrompt["en"] == "" {
		t.Fatal("DefaultTeamPlanAgentSystemPrompt en is empty")
	}
}

// TestBuildTeamPlanAgentCard 测试构建 plan_agent AgentCard
func TestBuildTeamPlanAgentCard(t *testing.T) {
	card := BuildTeamPlanAgentCard("cn")
	if card == nil {
		t.Fatal("BuildTeamPlanAgentCard returned nil")
	}
	if card.Name != "plan_agent" {
		t.Fatalf("expected card name plan_agent, got %s", card.Name)
	}
	if card.Description == "" {
		t.Fatal("card description is empty")
	}
}

// TestGetTeamPlanModePrompt 测试获取 plan mode 模板
func TestGetTeamPlanModePrompt(t *testing.T) {
	cnPrompt := GetTeamPlanModePrompt("cn")
	if cnPrompt == "" {
		t.Fatal("GetTeamPlanModePrompt cn is empty")
	}
	enPrompt := GetTeamPlanModePrompt("en")
	if enPrompt == "" {
		t.Fatal("GetTeamPlanModePrompt en is empty")
	}
}

// TestBuildTeamPlanModePrompt 测试渲染 plan mode 模板
func TestBuildTeamPlanModePrompt(t *testing.T) {
	result := BuildTeamPlanModePrompt("cn", "尚未调用 enter_plan_mode", "Plan 文件: /tmp/plan.md")
	if result == "" {
		t.Fatal("BuildTeamPlanModePrompt returned empty")
	}
	if !strings.Contains(result, "尚未调用 enter_plan_mode") {
		t.Fatal("result should contain enter_plan_mode_status")
	}
	if !strings.Contains(result, "Plan 文件: /tmp/plan.md") {
		t.Fatal("result should contain plan_file_info")
	}
}

// TestBuildTeamPlanModePrompt_English 测试英文 plan mode 渲染
func TestBuildTeamPlanModePrompt_English(t *testing.T) {
	result := BuildTeamPlanModePrompt("en", "enter_plan_mode has been called", "Plan file: /tmp/plan.md")
	if result == "" {
		t.Fatal("BuildTeamPlanModePrompt returned empty")
	}
	if !strings.Contains(result, "enter_plan_mode has been called") {
		t.Fatal("result should contain enter_plan_mode_status")
	}
}

// TestBuildEnterPlanModeStatusCN 测试中文 plan_mode 状态
func TestBuildEnterPlanModeStatusCN(t *testing.T) {
	// 已调用
	result := BuildEnterPlanModeStatusCN("/tmp/plan.md")
	if !strings.Contains(result, "已调用") {
		t.Fatal("should indicate plan_mode has been called")
	}
	// 未调用
	result = BuildEnterPlanModeStatusCN("")
	if !strings.Contains(result, "尚未调用") {
		t.Fatal("should indicate plan_mode has NOT been called")
	}
}

// TestBuildEnterPlanModeStatusEN 测试英文 plan_mode 状态
func TestBuildEnterPlanModeStatusEN(t *testing.T) {
	result := BuildEnterPlanModeStatusEN("/tmp/plan.md")
	if !strings.Contains(result, "has been called") {
		t.Fatal("should indicate plan_mode has been called")
	}
	result = BuildEnterPlanModeStatusEN("")
	if !strings.Contains(result, "NOT called") {
		t.Fatal("should indicate plan_mode has NOT been called")
	}
}

// TestBuildPlanFileInfoCN 测试中文 plan 文件信息
func TestBuildPlanFileInfoCN(t *testing.T) {
	// 无路径
	result := BuildPlanFileInfoCN("", false)
	if !strings.Contains(result, "暂无") {
		t.Fatal("should indicate no plan file")
	}
	// 文件已存在
	result = BuildPlanFileInfoCN("/tmp/plan.md", true)
	if !strings.Contains(result, "已存在") {
		t.Fatal("should indicate plan file exists")
	}
	if !strings.Contains(result, "/tmp/plan.md") {
		t.Fatal("should contain plan file path")
	}
	// 文件不存在
	result = BuildPlanFileInfoCN("/tmp/plan.md", false)
	if !strings.Contains(result, "尚不存在") {
		t.Fatal("should indicate plan file does not exist")
	}
}

// TestBuildPlanFileInfoEN 测试英文 plan 文件信息
func TestBuildPlanFileInfoEN(t *testing.T) {
	result := BuildPlanFileInfoEN("", false)
	if !strings.Contains(result, "No plan file yet") {
		t.Fatal("should indicate no plan file")
	}
	result = BuildPlanFileInfoEN("/tmp/plan.md", true)
	if !strings.Contains(result, "already exists") {
		t.Fatal("should indicate plan file exists")
	}
	result = BuildPlanFileInfoEN("/tmp/plan.md", false)
	if !strings.Contains(result, "No plan file exists yet") {
		t.Fatal("should indicate plan file does not exist")
	}
}
