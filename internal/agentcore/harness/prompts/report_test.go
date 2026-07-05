package prompts

import (
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewPromptReport 测试从构建器创建诊断报告
func TestNewPromptReport(t *testing.T) {
	builder := NewSystemPromptBuilder("cn", hschema.PromptModeFull)
	builder.AddSection(saprompt.NewPromptSection(sections.SectionIdentity, map[string]string{"cn": "身份信息"}, 10))
	builder.AddSection(saprompt.NewPromptSection(sections.SectionSafety, map[string]string{"cn": "安全规则"}, 20))

	report := NewPromptReport(builder)
	if report.SectionCount != 2 {
		t.Errorf("节数量期望 2，实际 %d", report.SectionCount)
	}
	if report.TotalChars == 0 {
		t.Error("总字符数不应为 0")
	}
	if report.EstimatedTokens == 0 {
		t.Error("估算 token 数不应为 0")
	}
	if report.Mode != "full" {
		t.Errorf("模式期望 full，实际 %s", report.Mode)
	}
	if report.Language != "cn" {
		t.Errorf("语言期望 cn，实际 %s", report.Language)
	}
}

// TestPromptReport_CN语言Token估算 测试中文 token 估算（每 token 约 2.5 字符）
func TestPromptReport_CN语言Token估算(t *testing.T) {
	builder := NewSystemPromptBuilder("cn", hschema.PromptModeFull)
	// 添加 25 个中文字符
	builder.AddSection(saprompt.NewPromptSection(sections.SectionIdentity, map[string]string{"cn": "一二三四五六七八九十一二三四五六七八九十一二三四五"}, 10))

	report := NewPromptReport(builder)
	// 25 / 2.5 = 10
	if report.EstimatedTokens != 10 {
		t.Errorf("中文 token 估算期望 10，实际 %d", report.EstimatedTokens)
	}
}

// TestPromptReport_EN语言Token估算 测试英文 token 估算（每 token 约 4.0 字符）
func TestPromptReport_EN语言Token估算(t *testing.T) {
	builder := NewSystemPromptBuilder("en", hschema.PromptModeFull)
	// 添加 40 个英文字符
	builder.AddSection(saprompt.NewPromptSection(sections.SectionIdentity, map[string]string{"en": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, 10))

	report := NewPromptReport(builder)
	// 40 / 4.0 = 10
	if report.EstimatedTokens != 10 {
		t.Errorf("英文 token 估算期望 10，实际 %d", report.EstimatedTokens)
	}
}

// TestPromptReport_Summary 测试摘要字符串
func TestPromptReport_Summary(t *testing.T) {
	builder := NewSystemPromptBuilder("cn", hschema.PromptModeFull)
	builder.AddSection(saprompt.NewPromptSection(sections.SectionIdentity, map[string]string{"cn": "身份"}, 10))

	report := NewPromptReport(builder)
	summary := report.Summary()
	if summary == "" {
		t.Error("摘要不应为空")
	}
	// 摘要应包含关键字段
	for _, sub := range []string{"mode=", "lang=", "sections=", "chars=", "est_tokens≈"} {
		if !strings.Contains(summary, sub) {
			t.Errorf("摘要缺少关键字段 %q: %s", sub, summary)
		}
	}
}

// TestPromptReport_ToDict 测试字典序列化
func TestPromptReport_ToDict(t *testing.T) {
	builder := NewSystemPromptBuilder("cn", hschema.PromptModeFull)
	builder.AddSection(saprompt.NewPromptSection(sections.SectionIdentity, map[string]string{"cn": "身份"}, 10))

	report := NewPromptReport(builder)
	dict := report.ToDict()

	for _, key := range []string{"total_chars", "estimated_tokens", "section_count", "sections", "mode", "language"} {
		if _, ok := dict[key]; !ok {
			t.Errorf("ToDict 应包含 %s", key)
		}
	}
}
