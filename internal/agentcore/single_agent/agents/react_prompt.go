package agents

import (
	"context"
	"fmt"
	"sort"
	"strings"

	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	saconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
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
// _ 编译时验证 ReActAgent 满足 BaseAgent 接口
var _ interfaces.BaseAgent = (*ReActAgent)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// AddPromptBuilderSection 添加或替换提示节，空内容时移除该节。
//
// Python: ReActAgent.add_prompt_builder_section(name, content, *, priority)
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
// Python: ReActAgent.configure(config)
func (a *ReActAgent) Configure(ctx context.Context, config interfaces.AgentConfig) error {
	cfg, ok := config.(*saconfig.ReActAgentConfig)
	if !ok {
		return fmt.Errorf("config 类型应为 *ReActAgentConfig，实际 %T", config)
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("config 校验失败: %w", err)
	}
	// 模型上下文元数据装配（对齐 Python configure 里的两个 _with_context_engine_model_* 步骤）
	withContextEngineModelName(cfg)
	withContextEngineModelWindow(cfg)
	a.config = cfg
	a.promptBuilder = prompts.NewSystemPromptBuilder()
	if cfg.PromptTemplateName != "" {
		a.AddPromptBuilderSection(identitySection, cfg.PromptTemplateName, identitySectionPriority)
	}
	return nil
}

// CallbackManager 返回回调管理器（满足 RailAgent 接口）。
func (a *ReActAgent) CallbackManager() *interfaces.AgentCallbackManager {
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

// SetContextEngine 设置上下文引擎。
// 对齐 Python: ReActAgent.__init__ 中 self.context_engine = ContextEngine(...)
// 以及 self._ability_manager.set_context_engine(self.context_engine)
func (a *ReActAgent) SetContextEngine(ce ceinterface.ContextEngine) {
	a.contextEngine = ce
	if a.abilityManager != nil {
		a.abilityManager.SetContextEngine(ce)
	}
}

// UpdateModelContext 热切换当前模型与其上下文窗口（不重建上下文）。
//
// 引擎侧把新元数据推给池内全部已建上下文；Agent 侧同步自身配置，
// 使后续 Configure/诊断读到同一份口径（对齐 Python ReActAgent.update_model_context）。
func (a *ReActAgent) UpdateModelContext(modelName string, contextWindowTokens int) {
	if a.config != nil {
		if modelName != "" {
			a.config.ContextEngineConfig.ModelName = modelName
		}
		if contextWindowTokens > 0 {
			a.config.ContextEngineConfig.ModelContextWindowTokensOverride = contextWindowTokens
		} else {
			a.config.ContextEngineConfig.ModelContextWindowTokensOverride = 0
		}
	}
	// 可选能力：只有 SessionModelContext 的引擎实现具备（对齐 Python 同名方法）。
	// 用类型断言而非拓宽 ContextEngine 接口，避免每个测试桩都被迫实现空方法
	// （同 runtimeConfigApplier 的既有做法）。
	if updater, ok := a.contextEngine.(interface {
		UpdateModelContext(string, int)
	}); ok {
		updater.UpdateModelContext(modelName, contextWindowTokens)
	}
}

// Card 返回Agent身份卡片。
// Python: BaseAgent.card 属性
func (a *ReActAgent) Card() *agentschema.AgentCard {
	return a.card
}

// Config 返回当前配置。
// Python: BaseAgent.config 属性
func (a *ReActAgent) Config() interfaces.AgentConfig {
	return a.config
}

// AbilityManager 返回能力管理器。
// Python: BaseAgent.ability_manager 属性
func (a *ReActAgent) AbilityManager() interfaces.AbilityManagerInterface {
	return a.abilityManager
}

// RegisterCallback 注册回调。
// Python: BaseAgent.register_callback(event, callback, priority)
func (a *ReActAgent) RegisterCallback(ctx context.Context, event interfaces.AgentCallbackEvent, fn callback.PerAgentCallbackFunc, opts ...callback.CallbackOption) error {
	if a.callbackManager != nil {
		a.callbackManager.RegisterCallback(ctx, event, fn, opts...)
	}
	return nil
}

// RegisterRail 注册 Rail。
// Python: BaseAgent.register_rail(rail)
func (a *ReActAgent) RegisterRail(ctx context.Context, r interfaces.AgentRail, opts ...callback.CallbackOption) error {
	if a.callbackManager != nil {
		if err := r.Init(ctx, a); err != nil {
			return err
		}
		return a.callbackManager.RegisterRail(ctx, r, opts...)
	}
	return nil
}

// UnregisterRail 注销 Rail。
// Python: BaseAgent.unregister_rail(rail)
func (a *ReActAgent) UnregisterRail(ctx context.Context, r interfaces.AgentRail) error {
	if a.callbackManager != nil {
		err := a.callbackManager.UnregisterRail(ctx, r)
		if uninitErr := r.Uninit(a); uninitErr != nil {
			if err == nil {
				return uninitErr
			}
			logger.Error(logComponent).
				Str("event_type", "rail_uninit_error").
				Err(uninitErr).
				Msg("Rail Uninit returned error")
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
// Python: ReActAgent.prompt_builder / ReActAgent.system_prompt_builder
func (a *ReActAgent) PromptBuilder() *prompts.SystemPromptBuilder {
	return a.promptBuilder
}

// SystemPromptBuilder 返回系统提示词构建器接口（满足 RailAgent 接口）。
//
// 委托给 PromptBuilder()，对齐 Python: ReActAgent.system_prompt_builder 属性
func (a *ReActAgent) SystemPromptBuilder() prompts.SystemPromptBuilderInterface {
	if a.promptBuilder != nil {
		return a.promptBuilder
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// updateSkillPromptBuilderSection 更新技能提示词区段。
//
// 技能小节是否注入只由「是否有已注册技能」决定：渲染后的系统提示词为空时
// 不得静默移除 skills section（否则 register_skill 后 agent 看不到已注册技能）。
//
// Python: ReActAgent._update_skill_prompt_builder_section()
func (a *ReActAgent) updateSkillPromptBuilderSection(ctx context.Context) {
	if a.skillUtil == nil {
		return
	}
	if !a.skillUtil.HasSkill() {
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
// Python: ReActAgent._warn_missing_skill_read_file_tool()
func (a *ReActAgent) warnMissingSkillReadFileTool(ctx context.Context) {
	toolInfos, err := a.abilityManager.ListToolInfo(ctx, nil)
	if err != nil {
		logger.Warn(logComponent).
			Str("event_type", "list_tool_info_error").
			Err(err).
			Msg("failed to get tool list, skipping read_file check")
		return
	}

	hasReadFile := false
	existingToolNames := make([]string, 0)

	for _, t := range toolInfos {
		if t.GetName() != "" {
			existingToolNames = append(existingToolNames, t.GetName())
			if t.GetName() == "read_file" {
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

// ──────────────────────────── 非导出函数 ────────────────────────────

// withContextEngineModelName 在未显式配置时用当前模型名补齐引擎 model_name。
//
// Python: ReActAgent._with_context_engine_model_name()
func withContextEngineModelName(cfg *saconfig.ReActAgentConfig) {
	if strings.TrimSpace(cfg.ContextEngineConfig.ModelName) != "" {
		return
	}
	name := cfg.ModelNameVal
	if cfg.ModelRequestConfig != nil && cfg.ModelRequestConfig.ModelName != "" {
		name = cfg.ModelRequestConfig.ModelName
	}
	if name == "" {
		return
	}
	cfg.ContextEngineConfig.ModelName = name
}

// withContextEngineModelWindow 把模型条目声明的 context_window 写入引擎 override。
//
// 不清空已有显式值：上游语义是"没有模型元数据时保留显式 override"。
//
// Python: ReActAgent._with_context_engine_model_window()
func withContextEngineModelWindow(cfg *saconfig.ReActAgentConfig) {
	if cfg.ModelRequestConfig == nil || cfg.ModelRequestConfig.ContextWindow == nil {
		return
	}
	window := *cfg.ModelRequestConfig.ContextWindow
	if window <= 0 || cfg.ContextEngineConfig.ModelContextWindowTokensOverride == window {
		return
	}
	cfg.ContextEngineConfig.ModelContextWindowTokensOverride = window
}
