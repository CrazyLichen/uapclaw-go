package agents

import (
	"context"
	"fmt"
	"sort"

	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	saconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/rail"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/skills"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
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
func (a *ReActAgent) Configure(ctx context.Context, config interfaces.AgentConfig) error {
	cfg, ok := config.(*saconfig.ReActAgentConfig)
	if !ok {
		return fmt.Errorf("config 类型应为 *ReActAgentConfig，实际 %T", config)
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("config 校验失败: %w", err)
	}
	a.config = cfg
	a.promptBuilder = NewSystemPromptBuilder()
	if cfg.PromptTemplateName != "" {
		a.AddPromptBuilderSection("identity", map[string]string{defaultLanguage: cfg.PromptTemplateName}, 10)
	}
	return nil
}

// CallbackManager 返回回调管理器（满足 RailAgent 接口）。
func (a *ReActAgent) CallbackManager() *rail.AgentCallbackManager {
	return a.callbackManager
}

// AgentID 返回 Agent 唯一标识（满足 RailAgent 接口）。
func (a *ReActAgent) AgentID() string {
	if a.card != nil {
		return a.card.ID
	}
	return ""
}

// ContextEngine 返回上下文引擎（满足 InterruptAgent 接口）。
func (a *ReActAgent) ContextEngine() ceinterface.ContextEngine {
	return a.contextEngine
}

// Card 返回Agent身份卡片。
// 对齐 Python: BaseAgent.card 属性
func (a *ReActAgent) Card() *agentschema.AgentCard {
	return a.card
}

// Config 返回当前配置。
// 对齐 Python: BaseAgent.config 属性
func (a *ReActAgent) Config() interfaces.AgentConfig {
	return a.config
}

// AbilityManager 返回能力管理器。
// 对齐 Python: BaseAgent.ability_manager 属性
// 返回 any，调用方通过类型断言获取 *ability.AbilityManager。
func (a *ReActAgent) AbilityManager() any {
	return a.abilityManager
}

// RegisterCallback 注册回调。
// 对齐 Python: BaseAgent.register_callback(event, callback, priority)
func (a *ReActAgent) RegisterCallback(ctx context.Context, event any, fn any, opts ...callback.CallbackOption) error {
	if a.callbackManager != nil {
		a.callbackManager.RegisterCallback(ctx, event.(rail.AgentCallbackEvent), fn.(callback.PerAgentCallbackFunc), opts...)
	}
	return nil
}

// RegisterRail 注册 Rail。
// 对齐 Python: BaseAgent.register_rail(rail)
func (a *ReActAgent) RegisterRail(ctx context.Context, r rail.AgentRail, opts ...callback.CallbackOption) error {
	if a.callbackManager != nil {
		if err := r.Init(a); err != nil {
			return err
		}
		return a.callbackManager.RegisterRail(ctx, r, opts...)
	}
	return nil
}

// UnregisterRail 注销 Rail。
// 对齐 Python: BaseAgent.unregister_rail(rail)
func (a *ReActAgent) UnregisterRail(ctx context.Context, r rail.AgentRail) error {
	if a.callbackManager != nil {
		err := a.callbackManager.UnregisterRail(ctx, r)
		if uninitErr := r.Uninit(a); uninitErr != nil {
			if err == nil {
				return uninitErr
			}
			logger.Error(logger.ComponentAgentCore).
				Str("event_type", "rail_uninit_error").
				Err(uninitErr).
				Msg("Rail Uninit 返回错误")
		}
		return err
	}
	return nil
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

// 编译期接口检查：ReActAgent 必须满足 interfaces.BaseAgent
var _ interfaces.BaseAgent = (*ReActAgent)(nil)
