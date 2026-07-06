package prompts

import (
	"sort"
	"strings"
	"unicode/utf8"

	hs "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PromptSection 系统提示词的单一节，支持多语言内容。
//
// 对应 Python: PromptSection (openjiuwen/core/single_agent/prompts/builder.py)
type PromptSection struct {
	// Name 节名称（同名称覆盖）
	Name string
	// Content 多语言内容映射：language → content
	Content map[string]string
	// Priority 优先级（数值越小越靠前）
	Priority int
}

// SystemPromptBuilderInterface 系统提示词构建器最小接口。
//
// 供 Rail 等消费者通过 RailAgent 接口访问 SystemPromptBuilder，
// 避免依赖具体类型。saprompt.SystemPromptBuilder 和
// hprompts.SystemPromptBuilder（嵌入 base）均隐式满足此接口。
//
// 对齐 Python: agent.system_prompt_builder 属性的类型约束
type SystemPromptBuilderInterface interface {
	// AddSection 添加或替换节
	AddSection(section PromptSection) *SystemPromptBuilder
	// RemoveSection 移除指定名称的节
	RemoveSection(name string) *SystemPromptBuilder
	// Language 返回当前语言
	Language() string
	// GetSection 按名称获取单个节
	GetSection(name string) *PromptSection
	// HasSection 检查节是否存在
	HasSection(name string) bool
}

// SystemPromptBuilder 基于节的系统提示词构建器。
//
// 本类仅提供通用的节注册和渲染功能。
// Agent 簇特定的提示词策略（如模式切换或提示词诊断）
// 应通过 sectionsFilter 函数字段在外部实现。
//
// 对应 Python: SystemPromptBuilder (openjiuwen/core/single_agent/prompts/builder.py)
type SystemPromptBuilder struct {
	// language 当前语言（默认 "cn"）
	language string
	// sectionsFilter 节过滤钩子，Build 时调用。nil 表示不过滤。
	// 用于 harness 层的 PromptMode（FULL/MINIMAL/NONE）过滤。
	// 对应 Python: SystemPromptBuilder._get_sections_for_build() 钩子方法
	sectionsFilter func([]PromptSection) []PromptSection
	// sections 已注册的节映射：name → PromptSection
	// 对应 Python: SystemPromptBuilder._sections
	sections map[string]PromptSection
}

// ──────────────────────────── 常量 ────────────────────────────
const (
	// DefaultLanguage 默认提示词语言
	// 对应 Python: DEFAULT_LANGUAGE = "cn"
	DefaultLanguage = "cn"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译时验证 *SystemPromptBuilder 满足 SystemPromptBuilderInterface 接口。
var _ SystemPromptBuilderInterface = (*SystemPromptBuilder)(nil)

var (
	// SupportedLanguages 支持的语言列表
	// 对应 Python: SUPPORTED_LANGUAGES = ("cn", "en")
	SupportedLanguages = [2]string{"cn", "en"}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSystemPromptBuilder 创建系统提示词构建器（默认语言 "cn"，无过滤）。
//
// 对应 Python: SystemPromptBuilder(language=DEFAULT_LANGUAGE)
func NewSystemPromptBuilder() *SystemPromptBuilder {
	return &SystemPromptBuilder{
		language: DefaultLanguage,
		sections: make(map[string]PromptSection),
	}
}

// NewSystemPromptBuilderWithFilter 创建带自定义语言和过滤函数的构建器。
//
// 对应 Python: harness/prompts/builder.py SystemPromptBuilder(language, mode) 子类构造
func NewSystemPromptBuilderWithFilter(lang string, filter func([]PromptSection) []PromptSection) *SystemPromptBuilder {
	return &SystemPromptBuilder{
		language:       lang,
		sectionsFilter: filter,
		sections:       make(map[string]PromptSection),
	}
}

// NewSystemPromptBuilderWithPromptMode 创建按 PromptMode 过滤节的系统提示词构建器。
//
// 三种模式：
//   - PromptModeFull：不过滤，等效于 NewSystemPromptBuilder
//   - PromptModeMinimal：仅保留 Priority <= 20 的节
//   - PromptModeNone：所有节均不参与构建（返回空节列表）
//
// 对应 Python: harness/prompts/builder.py SystemPromptBuilder(language, mode)
func NewSystemPromptBuilderWithPromptMode(language string, mode hs.PromptMode) *SystemPromptBuilder {
	switch mode {
	case hs.PromptModeFull:
		return NewSystemPromptBuilderWithFilter(language, nil)
	case hs.PromptModeMinimal:
		return NewSystemPromptBuilderWithFilter(language, func(sections []PromptSection) []PromptSection {
			result := make([]PromptSection, 0, len(sections))
			for _, s := range sections {
				if s.Priority <= 20 {
					result = append(result, s)
				}
			}
			return result
		})
	case hs.PromptModeNone:
		return NewSystemPromptBuilderWithFilter(language, func(_ []PromptSection) []PromptSection {
			return nil
		})
	default:
		return NewSystemPromptBuilderWithFilter(language, nil)
	}
}

// NewPromptSection 创建提示节。
//
// 对应 Python: PromptSection(name, content, priority)
func NewPromptSection(name string, content map[string]string, priority int) PromptSection {
	return PromptSection{
		Name:     name,
		Content:  content,
		Priority: priority,
	}
}

// AddSection 添加或替换节（链式调用）。
//
// 对应 Python: SystemPromptBuilder.add_section(section)
func (b *SystemPromptBuilder) AddSection(section PromptSection) *SystemPromptBuilder {
	b.sections[section.Name] = section
	return b
}

// RemoveSection 移除指定名称的节（链式调用）。
//
// 对应 Python: SystemPromptBuilder.remove_section(name)
func (b *SystemPromptBuilder) RemoveSection(name string) *SystemPromptBuilder {
	delete(b.sections, name)
	return b
}

// GetAllSections 返回所有注册节的副本。
//
// 对应 Python: SystemPromptBuilder.get_all_sections()
func (b *SystemPromptBuilder) GetAllSections() map[string]PromptSection {
	result := make(map[string]PromptSection, len(b.sections))
	for k, v := range b.sections {
		result[k] = v
	}
	return result
}

// HasSection 检查节是否存在。
//
// 对应 Python: SystemPromptBuilder.has_section(name)
func (b *SystemPromptBuilder) HasSection(name string) bool {
	_, ok := b.sections[name]
	return ok
}

// GetSection 按名称获取单个节，不存在返回 nil。
//
// 对应 Python: SystemPromptBuilder.get_section(name)
func (b *SystemPromptBuilder) GetSection(name string) *PromptSection {
	if s, ok := b.sections[name]; ok {
		return &s
	}
	return nil
}

// Language 返回当前语言设置。
func (b *SystemPromptBuilder) Language() string {
	return b.language
}

// SetLanguage 设置当前语言。
func (b *SystemPromptBuilder) SetLanguage(lang string) {
	b.language = lang
}

// Build 按优先级排序并拼接为完整系统提示词。
//
// 安全多次调用，每次从当前所有注册节生成完整提示词。
//
// 对应 Python: SystemPromptBuilder.build()
func (b *SystemPromptBuilder) Build() string {
	sections := b.getSectionsForBuild()
	sort.Slice(sections, func(i, j int) bool {
		return sections[i].Priority < sections[j].Priority
	})

	parts := make([]string, 0, len(sections))
	for _, s := range sections {
		if content := s.Render(b.language); content != "" {
			parts = append(parts, content)
		}
	}

	return strings.Join(parts, "\n\n")
}

// Render 渲染指定语言的内容。
//
// 回退顺序：精确匹配 → DefaultLanguage → map 中首个值 → 空字符串。
//
// 对应 Python: PromptSection.render(language)
func (s *PromptSection) Render(language string) string {
	if content, ok := s.Content[language]; ok {
		return content
	}
	if content, ok := s.Content[DefaultLanguage]; ok {
		return content
	}
	for _, v := range s.Content {
		return v
	}
	return ""
}

// CharCount 返回指定语言渲染后的字符数（Unicode 字符数，对齐 Python len()）。
//
// 对应 Python: PromptSection.char_count(language)
func (s *PromptSection) CharCount(language string) int {
	return utf8.RuneCountInString(s.Render(language))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getSectionsForBuild 返回参与 Build 的节列表，应用 sectionsFilter 过滤。
//
// 对应 Python: SystemPromptBuilder._get_sections_for_build()
func (b *SystemPromptBuilder) getSectionsForBuild() []PromptSection {
	all := make([]PromptSection, 0, len(b.sections))
	for _, s := range b.sections {
		all = append(all, s)
	}
	if b.sectionsFilter != nil {
		return b.sectionsFilter(all)
	}
	return all
}
