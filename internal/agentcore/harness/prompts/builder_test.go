package prompts

import (
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestSystemPromptBuilder_Full模式 测试 FULL 模式下所有节均参与构建
func TestSystemPromptBuilder_Full模式(t *testing.T) {
	builder := NewSystemPromptBuilder("cn", hschema.PromptModeFull)
	builder.AddSection(saprompt.NewPromptSection(sections.SectionIdentity, map[string]string{"cn": "我是身份"}, 10))
	builder.AddSection(saprompt.NewPromptSection(sections.SectionSafety, map[string]string{"cn": "安全规则"}, 20))
	builder.AddSection(saprompt.NewPromptSection(sections.SectionSkills, map[string]string{"cn": "技能描述"}, 30))

	result := builder.Build()
	if result == "" {
		t.Fatal("FULL 模式构建结果不应为空")
	}
	if !strings.Contains(result, "我是身份") {
		t.Error("FULL 模式应包含 identity 节")
	}
	if !strings.Contains(result, "安全规则") {
		t.Error("FULL 模式应包含 safety 节")
	}
	if !strings.Contains(result, "技能描述") {
		t.Error("FULL 模式应包含 skills 节")
	}
}

// TestSystemPromptBuilder_Minimal模式 测试 MINIMAL 模式仅保留 MinimalSections 中的节
func TestSystemPromptBuilder_Minimal模式(t *testing.T) {
	builder := NewSystemPromptBuilder("cn", hschema.PromptModeMinimal)
	builder.AddSection(saprompt.NewPromptSection(sections.SectionIdentity, map[string]string{"cn": "身份"}, 10))
	builder.AddSection(saprompt.NewPromptSection(sections.SectionSafety, map[string]string{"cn": "安全"}, 20))
	builder.AddSection(saprompt.NewPromptSection(sections.SectionSkills, map[string]string{"cn": "技能"}, 30))
	builder.AddSection(saprompt.NewPromptSection(sections.SectionTools, map[string]string{"cn": "工具"}, 40))
	builder.AddSection(saprompt.NewPromptSection(sections.SectionRuntime, map[string]string{"cn": "运行时"}, 50))
	builder.AddSection(saprompt.NewPromptSection(sections.SectionMemory, map[string]string{"cn": "记忆"}, 60))
	// 不在 MinimalSections 中的节
	builder.AddSection(saprompt.NewPromptSection(sections.SectionTodo, map[string]string{"cn": "待办"}, 70))
	builder.AddSection(saprompt.NewPromptSection(sections.SectionWorkspace, map[string]string{"cn": "工作空间"}, 80))

	result := builder.Build()
	if !strings.Contains(result, "身份") {
		t.Error("MINIMAL 模式应包含 identity 节")
	}
	if !strings.Contains(result, "安全") {
		t.Error("MINIMAL 模式应包含 safety 节")
	}
	if strings.Contains(result, "待办") {
		t.Error("MINIMAL 模式不应包含 todo 节")
	}
	if strings.Contains(result, "工作空间") {
		t.Error("MINIMAL 模式不应包含 workspace 节")
	}
}

// TestSystemPromptBuilder_None模式 测试 NONE 模式仅渲染 identity 节
func TestSystemPromptBuilder_None模式(t *testing.T) {
	builder := NewSystemPromptBuilder("cn", hschema.PromptModeNone)
	builder.AddSection(saprompt.NewPromptSection(sections.SectionIdentity, map[string]string{"cn": "仅身份"}, 10))
	builder.AddSection(saprompt.NewPromptSection(sections.SectionSafety, map[string]string{"cn": "安全"}, 20))

	result := builder.Build()
	if !strings.Contains(result, "仅身份") {
		t.Error("NONE 模式应包含 identity 节")
	}
	if strings.Contains(result, "安全") {
		t.Error("NONE 模式不应包含 safety 节")
	}
}

// TestSystemPromptBuilder_None模式_无Identity 测试 NONE 模式无 identity 节时返回空串
func TestSystemPromptBuilder_None模式_无Identity(t *testing.T) {
	builder := NewSystemPromptBuilder("cn", hschema.PromptModeNone)
	builder.AddSection(saprompt.NewPromptSection(sections.SectionSafety, map[string]string{"cn": "安全"}, 20))

	result := builder.Build()
	if result != "" {
		t.Errorf("NONE 模式无 identity 节时应返回空串，实际: %q", result)
	}
}

// TestResolveLanguage_优先级 测试语言解析优先级
func TestResolveLanguage_优先级(t *testing.T) {
	// 保存原始环境变量
	origEnv := t.Name()
	_ = origEnv

	// 配置参数最高优先级
	if lang := ResolveLanguage("en"); lang != "en" {
		t.Errorf("配置参数应优先，期望 en，实际 %s", lang)
	}

	// 环境变量次之
	t.Setenv("AGENT_PROMPT_LANGUAGE", "en")
	if lang := ResolveLanguage(""); lang != "en" {
		t.Errorf("环境变量应次之，期望 en，实际 %s", lang)
	}

	// 默认值兜底
	t.Setenv("AGENT_PROMPT_LANGUAGE", "")
	if lang := ResolveLanguage(""); lang != DefaultLanguage {
		t.Errorf("应回退到默认语言，期望 %s，实际 %s", DefaultLanguage, lang)
	}
}

// TestResolveMode 测试模式解析
func TestResolveMode(t *testing.T) {
	tests := []struct {
		input    string
		expected hschema.PromptMode
	}{
		{"full", hschema.PromptModeFull},
		{"minimal", hschema.PromptModeMinimal},
		{"none", hschema.PromptModeNone},
		{"", hschema.PromptModeFull},
		{"invalid", hschema.PromptModeFull},
	}
	for _, tt := range tests {
		result := ResolveMode(tt.input)
		if result != tt.expected {
			t.Errorf("ResolveMode(%q) = %v, 期望 %v", tt.input, result, tt.expected)
		}
	}
}
