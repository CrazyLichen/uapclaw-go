package prompt

import (
	"testing"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

func TestBuildCodeSystemPrompt(t *testing.T) {
	result := BuildCodeSystemPrompt()

	if result == "" {
		t.Error("BuildCodeSystemPrompt 返回空字符串")
	}

	// 验证包含所有 8 个 section 的关键内容
	checks := []string{
		"JiuwenSwarm",                 // code_intro
		"# System",                    // code_system
		"# Doing tasks",               // code_doing_tasks
		"# Using your tools",          // code_using_your_tools
		"Git Safety Protocol",         // code_using_your_tools 子段
		"Executing actions with care", // code_actions_with_care
		"# Tone and style",            // code_tone_and_style
		"Text output",                 // code_output_efficiency
		"Session-specific guidance",   // code_session_guidance
	}
	for _, want := range checks {
		if !contains(result, want) {
			t.Errorf("BuildCodeSystemPrompt 缺少 %q", want)
		}
	}
}

func TestBuildCodeSystemPrompt_英文内容(t *testing.T) {
	result := BuildCodeSystemPrompt()

	// 验证内容为英文
	if !contains(result, "interactive coding agent") {
		t.Error("缺少英文内容 'interactive coding agent'")
	}
	if !contains(result, "software engineering tasks") {
		t.Error("缺少英文内容 'software engineering tasks'")
	}
}

func TestBuildCodeIntroSection(t *testing.T) {
	section := BuildCodeIntroSection()

	if section.Name != "code_intro" {
		t.Errorf("Name = %q, want %q", section.Name, "code_intro")
	}
	if section.Priority != 10 {
		t.Errorf("Priority = %d, want %d", section.Priority, 10)
	}
	if _, ok := section.Content["en"]; !ok {
		t.Error("缺少 en 内容")
	}
	if len(section.Content) != 1 {
		t.Errorf("Content 应仅有 1 个键，got %d", len(section.Content))
	}
}

func TestBuildCodeSystemSection(t *testing.T) {
	section := BuildCodeSystemSection()

	if section.Name != "code_system" {
		t.Errorf("Name = %q, want %q", section.Name, "code_system")
	}
	if section.Priority != 15 {
		t.Errorf("Priority = %d, want %d", section.Priority, 15)
	}
}

func TestBuildCodeDoingTasksSection(t *testing.T) {
	section := BuildCodeDoingTasksSection()

	if section.Name != "code_doing_tasks" {
		t.Errorf("Name = %q, want %q", section.Name, "code_doing_tasks")
	}
	if section.Priority != 25 {
		t.Errorf("Priority = %d, want %d", section.Priority, 25)
	}
}

func TestBuildCodeUsingYourToolsSection(t *testing.T) {
	section := BuildCodeUsingYourToolsSection()

	if section.Name != "code_using_your_tools" {
		t.Errorf("Name = %q, want %q", section.Name, "code_using_your_tools")
	}
	if section.Priority != 31 {
		t.Errorf("Priority = %d, want %d", section.Priority, 31)
	}
}

func TestBuildCodeActionsWithCareSection(t *testing.T) {
	section := BuildCodeActionsWithCareSection()

	if section.Name != "code_actions_with_care" {
		t.Errorf("Name = %q, want %q", section.Name, "code_actions_with_care")
	}
	if section.Priority != 35 {
		t.Errorf("Priority = %d, want %d", section.Priority, 35)
	}
}

func TestBuildCodeToneAndStyleSection(t *testing.T) {
	section := BuildCodeToneAndStyleSection()

	if section.Name != "code_tone_and_style" {
		t.Errorf("Name = %q, want %q", section.Name, "code_tone_and_style")
	}
	if section.Priority != 45 {
		t.Errorf("Priority = %d, want %d", section.Priority, 45)
	}
}

func TestBuildCodeOutputEfficiencySection(t *testing.T) {
	section := BuildCodeOutputEfficiencySection()

	if section.Name != "code_output_efficiency" {
		t.Errorf("Name = %q, want %q", section.Name, "code_output_efficiency")
	}
	if section.Priority != 50 {
		t.Errorf("Priority = %d, want %d", section.Priority, 50)
	}
}

func TestBuildCodeSessionGuidanceSection(t *testing.T) {
	section := BuildCodeSessionGuidanceSection()

	if section.Name != "code_session_guidance" {
		t.Errorf("Name = %q, want %q", section.Name, "code_session_guidance")
	}
	if section.Priority != 55 {
		t.Errorf("Priority = %d, want %d", section.Priority, 55)
	}
}

func TestCodePromptPriority_顺序(t *testing.T) {
	priorities := []int{
		int(CodePriorityIntro),
		int(CodePrioritySystem),
		int(CodePriorityDoingTasks),
		int(CodePriorityUsingTools),
		int(CodePriorityActionsWithCare),
		int(CodePriorityToneAndStyle),
		int(CodePriorityOutputEfficiency),
		int(CodePrioritySessionGuidance),
	}

	for i := 1; i < len(priorities); i++ {
		if priorities[i] <= priorities[i-1] {
			t.Errorf("Priority[%d]=%d 应大于 Priority[%d]=%d", i, priorities[i], i-1, priorities[i-1])
		}
	}
}

// contains 检查字符串是否包含子串
func contains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
