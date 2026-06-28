package agents

import (
	"context"
	"fmt"
	"sort"
	"strings"

	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	saconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/rail"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/skills"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译期接口检查：ReActAgent 必须满足 interfaces.BaseAgent
var _ interfaces.BaseAgent = (*ReActAgent)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// AddPromptBuilderSection 添加或替换提示节，空内容时移除该节。
//
// 对应 Python: ReActAgent.add_prompt_builder_section(name, content, *, priority)
// Python 行为：content 为空/None 时 remove_section，否则 add_section 且 cn/en 内容相同。
func (a *ReActAgent) AddPromptBuilderSection(name string, content string, priority int) {
	text := strings.TrimSpace(content)
	if text == "" {
		a.promptBuilder.RemoveSection(name)
		return
	}
	a.promptBuilder.AddSection(prompts.PromptSection{
		Name:     name,
		Content:  map[string]string{"cn": text, "en": text},
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
	a.promptBuilder = prompts.NewSystemPromptBuilder()
	if cfg.PromptTemplateName != "" {
		a.AddPromptBuilderSection(identitySection, cfg.PromptTemplateName, identitySectionPriority)
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

// SkillUtil 返回技能工具实例。
func (a *ReActAgent) SkillUtil() *skills.SkillUtil {
	return a.skillUtil
}

// SetSkillUtil 设置技能工具实例。
func (a *ReActAgent) SetSkillUtil(su *skills.SkillUtil) {
	a.skillUtil = su
}

// PromptBuilder 返回系统提示词构建器。
//
// 对应 Python: ReActAgent.prompt_builder / ReActAgent.system_prompt_builder
func (a *ReActAgent) PromptBuilder() *prompts.SystemPromptBuilder {
	return a.promptBuilder
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// updateSkillPromptBuilderSection 更新技能提示词区段。
//
// 在 invoke 入口阶段，根据已注册技能更新 prompt_builder 的 skills section。
// 如果渲染后的系统提示词为空、skillUtil 为 nil 或无已注册技能，则移除 skills section。
// 否则将技能提示词注入 skills section（priority=90）。
//
// 对应 Python: ReActAgent._update_skill_prompt_builder_section(rendered_system_prompt)
func (a *ReActAgent) updateSkillPromptBuilderSection(ctx context.Context, renderedSystemPrompt string) {
	if renderedSystemPrompt == "" || a.skillUtil == nil || !a.skillUtil.HasSkill() {
		a.promptBuilder.RemoveSection(skillsSection)
		return
	}
	a.warnMissingSkillReadFileTool(ctx)
	skillPrompt := a.skillUtil.GetSkillPrompt()
	a.AddPromptBuilderSection(skillsSection, skillPrompt, skillsSectionPriority)
}

// warnMissingSkillReadFileTool 检查技能提示词启用时是否缺少必需的 read_file 工具，
// 若缺少则记录警告日志。
//
// 对应 Python: ReActAgent._warn_missing_skill_read_file_tool()
func (a *ReActAgent) warnMissingSkillReadFileTool(ctx context.Context) {
	toolInfos, err := a.abilityManager.ListToolInfo(ctx, nil)
	if err != nil {
		logger.Warn(logComponent).
			Str("event_type", "list_tool_info_error").
			Err(err).
			Msg("获取工具列表失败，跳过 read_file 检查")
		return
	}

	hasReadFile := false
	existingToolNames := make([]string, 0)

	for _, t := range toolInfos {
		if t.Name != "" {
			existingToolNames = append(existingToolNames, t.Name)
			if t.Name == "read_file" {
				hasReadFile = true
			}
		}
	}

	if hasReadFile {
		return
	}

	sort.Strings(existingToolNames)
	errMsg := fmt.Sprintf(
		"skill prompt requires tool 'read_file' but it is not found in ability_manager. existing_tools=%v",
		existingToolNames,
	)
	buildErr := exception.BuildError(exception.StatusAgentToolNotFound,
		exception.WithMsg(errMsg),
	)
	logger.Warn(logComponent).
		Str("event_type", "skill_missing_read_file_tool").
		Str("existing_tools", fmt.Sprintf("%v", existingToolNames)).
		Msg(buildErr.Error())
}
