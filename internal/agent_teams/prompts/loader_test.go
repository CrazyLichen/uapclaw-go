package prompts

import (
	"testing"
)

// TestLoadTemplate_中文模板 测试加载中文模板
func TestLoadTemplate_中文模板(t *testing.T) {
	tpl, err := LoadTemplate("leader_policy", "cn")
	if err != nil {
		t.Fatalf("LoadTemplate(leader_policy, cn) 返回错误: %v", err)
	}
	content := tpl.Render(nil)
	if content == "" {
		t.Fatal("leader_policy cn template is empty")
	}
}

// TestLoadTemplate_英文模板 测试加载英文模板
func TestLoadTemplate_英文模板(t *testing.T) {
	tpl, err := LoadTemplate("leader_policy", "en")
	if err != nil {
		t.Fatalf("LoadTemplate(leader_policy, en) 返回错误: %v", err)
	}
	content := tpl.Render(nil)
	if content == "" {
		t.Fatal("leader_policy en template is empty")
	}
}

// TestLoadSharedTemplate 测试加载共享模板
func TestLoadSharedTemplate(t *testing.T) {
	tpl, err := LoadSharedTemplate("system_prompt")
	if err != nil {
		t.Fatalf("LoadSharedTemplate(system_prompt) 返回错误: %v", err)
	}
	if tpl.Render(nil) == "" {
		t.Fatal("system_prompt template is empty")
	}
}

// TestPromptTemplate_Render_单花括号 测试单花括号 {var} 占位符渲染
func TestPromptTemplate_Render_单花括号(t *testing.T) {
	tpl, err := LoadTemplate("team_plan_mode", "cn")
	if err != nil {
		t.Fatalf("team_plan_mode cn template 加载失败: %v", err)
	}
	result := tpl.Render(map[string]string{
		"enter_plan_mode_status": "尚未调用 enter_plan_mode",
		"plan_file_info":         "Plan 文件: /tmp/plan.md",
	})
	if result == "" {
		t.Fatal("Render returned empty string")
	}
}

// TestPromptTemplate_Render_双花括号 测试双花括号 {{var}} 占位符渲染
func TestPromptTemplate_Render_双花括号(t *testing.T) {
	tpl, err := LoadSharedTemplate("system_prompt")
	if err != nil {
		t.Fatalf("LoadSharedTemplate(system_prompt) 返回错误: %v", err)
	}
	result := tpl.Render(map[string]string{
		"member_name_section":  "[name]",
		"role_policy":          "[role]",
		"workflow_section":     "[wf]",
		"lifecycle_section":    "[lc]",
		"persona_label":        "[pl]",
		"persona":              "[p]",
		"team_info_section":    "[info]",
		"team_members_section": "[members]",
		"base_prompt_section":  "[bp]",
	})
	if result == "" {
		t.Fatal("Render returned empty string")
	}
}

// TestLoadTemplate_不存在的模板返回错误 模板缺失必须报错（对齐 Python FileNotFoundError）
func TestLoadTemplate_不存在的模板返回错误(t *testing.T) {
	tpl, err := LoadTemplate("nonexistent", "cn")
	if err == nil {
		t.Fatal("expected error for nonexistent template")
	}
	if tpl != nil {
		t.Fatal("expected nil template on error")
	}
}

// TestLoadTemplate_缓存命中 测试重复加载返回缓存
func TestLoadTemplate_缓存命中(t *testing.T) {
	tpl1, err := LoadTemplate("leader_policy", "cn")
	if err != nil {
		t.Fatalf("首次加载失败: %v", err)
	}
	tpl2, err := LoadTemplate("leader_policy", "cn")
	if err != nil {
		t.Fatalf("二次加载失败: %v", err)
	}
	if tpl1 != tpl2 {
		t.Fatal("expected same template instance from cache")
	}
}

// TestPromptTemplate_Content 测试 Content 方法
func TestPromptTemplate_Content(t *testing.T) {
	tpl, err := LoadTemplate("leader_policy", "cn")
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if tpl.Content() == "" {
		t.Fatal("Content() returned empty string")
	}
}

// TestPromptTemplate_ToSection 测试 ToSection 便捷方法
func TestPromptTemplate_ToSection(t *testing.T) {
	tpl, err := LoadTemplate("leader_policy", "cn")
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	section := tpl.ToSection("test_section", "cn", 10)
	if section.Name != "test_section" {
		t.Fatalf("expected section name test_section, got %s", section.Name)
	}
	if section.Priority != 10 {
		t.Fatalf("expected priority 10, got %d", section.Priority)
	}
	if section.Content["cn"] == "" {
		t.Fatal("section cn content is empty")
	}
}
