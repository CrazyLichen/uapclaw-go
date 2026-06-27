package agents

import (
	"context"
	"fmt"
	"sort"

	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	saconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/rail"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/skills"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// AddPromptBuilderSection 添加或替换提示节。
func (a *ReActAgent) AddPromptBuilderSection(name string, content map[string]string, priority int) {
	a.promptBuilder.AddSection(PromptSection{
		Name:     name,
		Content:  content,
		Priority: priority,
	})
}

// Configure 配置 ReActAgent。
//
// 对应 Python: ReActAgent.configure(config)
func (a *ReActAgent) Configure(ctx context.Context, config *saconfig.ReActAgentConfig) error {
	if config == nil {
		return fmt.Errorf("config 不能为 nil")
	}
	if err := config.Validate(); err != nil {
		return fmt.Errorf("config 校验失败: %w", err)
	}
	a.config = config
	a.promptBuilder = NewSystemPromptBuilder()
	if config.PromptTemplateName != "" {
		a.AddPromptBuilderSection("identity", map[string]string{defaultLanguage: config.PromptTemplateName}, 10)
	}
	return nil
}

// CallbackManager 返回回调管理器（满足 RailAgent 接口）。
func (a *ReActAgent) CallbackManager() *rail.AgentCallbackManager {
	return a.base.CallbackManager()
}

// AgentID 返回 Agent 唯一标识（满足 RailAgent 接口）。
func (a *ReActAgent) AgentID() string {
	return a.base.AgentID()
}

// ContextEngine 返回上下文引擎（满足 InterruptAgent 接口）。
func (a *ReActAgent) ContextEngine() ceinterface.ContextEngine {
	return a.contextEngine
}

// AddSection 添加或替换节。
func (b *SystemPromptBuilder) AddSection(section PromptSection) *SystemPromptBuilder {
	b.sections[section.Name] = section
	return b
}

// RemoveSection 移除指定名称的节。
func (b *SystemPromptBuilder) RemoveSection(name string) *SystemPromptBuilder {
	delete(b.sections, name)
	return b
}

// HasSection 检查节是否存在。
func (b *SystemPromptBuilder) HasSection(name string) bool {
	_, ok := b.sections[name]
	return ok
}

// Build 按优先级排序并拼接为完整系统提示词。
func (b *SystemPromptBuilder) Build() string {
	sections := make([]PromptSection, 0, len(b.sections))
	for _, s := range b.sections {
		sections = append(sections, s)
	}
	sort.Slice(sections, func(i, j int) bool {
		return sections[i].Priority < sections[j].Priority
	})

	parts := make([]string, 0, len(sections))
	for _, s := range sections {
		if content := s.Render(b.Language); content != "" {
			parts = append(parts, content)
		}
	}

	result := ""
	for i, part := range parts {
		if i > 0 {
			result += "\n\n"
		}
		result += part
	}
	return result
}

// Render 渲染指定语言的内容。
func (s *PromptSection) Render(language string) string {
	if content, ok := s.Content[language]; ok {
		return content
	}
	if content, ok := s.Content[defaultLanguage]; ok {
		return content
	}
	for _, v := range s.Content {
		return v
	}
	return ""
}

// updateSkillPromptBuilderSection 更新技能提示词区段。
//
// 在 invoke 入口阶段，根据已注册技能更新 prompt_builder 的 skills section。
// 如果渲染后的系统提示词为空、skillUtil 为 nil 或无已注册技能，则移除 skills section。
// 否则将技能提示词注入 skills section（priority=90）。
//
// 对应 Python: ReActAgent._update_skill_prompt_builder_section(rendered_system_prompt)
func (a *ReActAgent) updateSkillPromptBuilderSection(renderedSystemPrompt string) {
	if renderedSystemPrompt == "" || a.skillUtil == nil || !a.skillUtil.HasSkill() {
		a.promptBuilder.RemoveSection(skillsSection)
		return
	}
	skillPrompt := a.skillUtil.GetSkillPrompt()
	a.AddPromptBuilderSection(skillsSection, map[string]string{defaultLanguage: skillPrompt}, skillsSectionPriority)
}

// SkillUtil 返回技能工具实例。
func (a *ReActAgent) SkillUtil() *skills.SkillUtil {
	return a.skillUtil
}

// SetSkillUtil 设置技能工具实例。
func (a *ReActAgent) SetSkillUtil(su *skills.SkillUtil) {
	a.skillUtil = su
}
