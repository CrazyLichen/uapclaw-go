package prompts

import (
	"os"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
)

// ──────────────────────────── 结构体 ────────────────────────────

type SystemPromptBuilder struct {
	*saprompt.SystemPromptBuilder
	// mode 提示词模式
	mode hschema.PromptMode
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// DefaultLanguage 默认提示词语言
	// 对应 Python: DEFAULT_LANGUAGE = "cn"
	DefaultLanguage = "cn"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// SupportedLanguages 支持的语言列表
	// 对应 Python: SUPPORTED_LANGUAGES = ("cn", "en")
	SupportedLanguages = []string{"cn", "en"}

	// MinimalSections 精简模式下保留的节名称集合
	// 对应 Python: SystemPromptBuilder._MINIMAL_SECTIONS
	MinimalSections = map[string]bool{
		sections.SectionIdentity: true,
		sections.SectionSafety:   true,
		sections.SectionSkills:   true,
		sections.SectionTools:    true,
		sections.SectionRuntime:  true,
		sections.SectionMemory:   true,
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

func NewSystemPromptBuilder(language string, mode hschema.PromptMode) *SystemPromptBuilder {
	// 按 PromptMode 创建基础构建器（对齐 Python: harness/prompts/builder.py）
	var base *saprompt.SystemPromptBuilder
	switch mode {
	case hschema.PromptModeFull:
		base = saprompt.NewSystemPromptBuilderWithFilter(language, nil)
	case hschema.PromptModeMinimal:
		base = saprompt.NewSystemPromptBuilderWithFilter(language, func(sections []saprompt.PromptSection) []saprompt.PromptSection {
			result := make([]saprompt.PromptSection, 0, len(sections))
			for _, s := range sections {
				if s.Priority <= 20 {
					result = append(result, s)
				}
			}
			return result
		})
	case hschema.PromptModeNone:
		base = saprompt.NewSystemPromptBuilderWithFilter(language, func(_ []saprompt.PromptSection) []saprompt.PromptSection {
			return nil
		})
	default:
		base = saprompt.NewSystemPromptBuilderWithFilter(language, nil)
	}
	return &SystemPromptBuilder{
		SystemPromptBuilder: base,
		mode:                mode,
	}
}

func (b *SystemPromptBuilder) Build() string {
	switch b.mode {
	case hschema.PromptModeNone:
		// NONE 模式：仅渲染 identity 节
		section := b.GetSection(sections.SectionIdentity)
		if section == nil {
			return ""
		}
		return section.Render(b.Language())
	case hschema.PromptModeMinimal:
		// MINIMAL 模式：仅渲染 MinimalSections 中的节
		return b.buildWithFilter(func(s saprompt.PromptSection) bool {
			return MinimalSections[s.Name]
		})
	default:
		// FULL 模式：委托给基础构建器
		return b.SystemPromptBuilder.Build()
	}
}

func (b *SystemPromptBuilder) BuildReport() *PromptReport {
	return NewPromptReport(b)
}

func ResolveLanguage(configLanguage string) string {
	if configLanguage != "" && isSupportedLanguage(configLanguage) {
		return configLanguage
	}
	if envLang := os.Getenv("AGENT_PROMPT_LANGUAGE"); isSupportedLanguage(envLang) {
		return envLang
	}
	return DefaultLanguage
}

// ResolveMode 从配置字符串解析 PromptMode，空串默认为 PromptModeFull。
//
// 对应 Python: resolve_mode()
func ResolveMode(configMode string) hschema.PromptMode {
	if configMode == "" {
		return hschema.PromptModeFull
	}
	mode, err := hschema.ParsePromptMode(configMode)
	if err != nil {
		return hschema.PromptModeFull
	}
	return mode
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isSupportedLanguage 检查语言是否在支持列表中。
//
// 对应 Python: config_language in SUPPORTED_LANGUAGES
func isSupportedLanguage(lang string) bool {
	for _, supported := range SupportedLanguages {
		if lang == supported {
			return true
		}
	}
	return false
}

// buildWithFilter 按过滤函数筛选节后构建提示词，不修改基础构建器的节注册。
func (b *SystemPromptBuilder) buildWithFilter(keep func(saprompt.PromptSection) bool) string {
	all := b.GetAllSections()
	filtered := make([]saprompt.PromptSection, 0, len(all))
	for _, s := range all {
		if keep(s) {
			filtered = append(filtered, s)
		}
	}

	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Priority < filtered[j].Priority
	})

	parts := make([]string, 0, len(filtered))
	for _, s := range filtered {
		if content := s.Render(b.Language()); content != "" {
			parts = append(parts, content)
		}
	}

	return strings.Join(parts, "\n\n")
}
