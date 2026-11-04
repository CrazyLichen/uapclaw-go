package agents

import (
	"sync"

	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	hprompts "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/ability"
	saconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interrupt"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/skills"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ReActAgent ReAct 循环 Agent：Think → Act → Observe。
//
// 直接持有基础字段（card/abilityManager/callbackManager），
// 自行实现 Invoke/Stream，在方法体内显式调用回调骨架。
//
// Python: ReActAgent (openjiuwen/core/single_agent/agents/react_agent.py)
type ReActAgent struct {
	// card Agent 身份卡片
	card *agentschema.AgentCard
	// abilityManager 能力管理器
	abilityManager interfaces.AbilityManagerInterface
	// callbackManager 回调管理器
	callbackManager *interfaces.AgentCallbackManager
	// config Agent 配置
	config *saconfig.ReActAgentConfig
	// contextEngine 上下文引擎
	contextEngine ceinterface.ContextEngine
	// llm LLM 模型实例（延迟初始化）
	llm *llm.Model
	// promptBuilder 系统提示词构建器
	// Python: ReActAgent.prompt_builder / ReActAgent.system_prompt_builder
	promptBuilder *prompts.SystemPromptBuilder
	// llmOnce LLM 初始化同步原语
	llmOnce sync.Once
	// kvReleaseWarningLogged KV cache 释放不支持的一次性警告标记
	kvReleaseWarningLogged bool
	// hitlHandler HITL 中断处理器
	hitlHandler *interrupt.ToolInterruptHandler
	// skillUtil 技能工具（延迟初始化，Configure 时根据 sysOperationID 创建）
	// Python: self._skill_util
	skillUtil *skills.SkillUtil
	// promptAttachments 动态提示词管理器（由 DeepAgent 注入，可空）
	// Python: ReActAgent.prompt_attachment_manager
	promptAttachments *hprompts.Manager
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────
const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore
	// defaultMaxIterations 默认最大迭代次数
	defaultMaxIterations = 5
	// identitySection identity 区段名称
	// Python: _IDENTITY_SECTION = "identity"
	identitySection = "identity"
	// IdentitySectionName identity 区段名称（导出，供外部包配置系统提示词）
	IdentitySectionName = identitySection
	// identitySectionPriority identity 区段优先级
	// Python: _IDENTITY_SECTION_PRIORITY = 10
	identitySectionPriority = 10
	// IdentitySectionPriority identity 区段优先级（导出，供外部包配置系统提示词）
	IdentitySectionPriority = identitySectionPriority
	// skillsSection skills 区段名称
	// Python: _SKILLS_SECTION = "skills"
	skillsSection = "skills"
	// skillsSectionPriority skills 区段优先级
	// Python: _SKILLS_SECTION_PRIORITY = 90
	skillsSectionPriority = 90
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewReActAgent 创建 ReActAgent 实例。
//
// Python: ReActAgent.__init__(card)
func NewReActAgent(
	card *agentschema.AgentCard,
	config *saconfig.ReActAgentConfig,
) *ReActAgent {
	agent := &ReActAgent{
		card:            card,
		abilityManager:  ability.NewAbilityManager(runner.GetResourceMgr()),
		callbackManager: interfaces.NewAgentCallbackManager(card.ID),
		config:          config,
		promptBuilder:   prompts.NewSystemPromptBuilder(),
	}

	// 初始化 HITL 中断处理器
	agent.hitlHandler = interrupt.NewToolInterruptHandler(agent)

	return agent
}

// PromptAttachments 返回动态提示词管理器（可为 nil）。
//
// Python: ReActAgent.prompt_attachment_manager。桥接回调里 ctx.agent 是
// ReActAgent 而非 DeepAgent，rail 需要它才能把动态提示词投递为历史附件。
func (a *ReActAgent) PromptAttachments() *hprompts.Manager { return a.promptAttachments }

// SetPromptAttachmentManager 注入动态提示词管理器（可为 nil）。
//
// Python: DeepAgent 把 prompt_attachment_manager 共享给内层 ReActAgent
// （deep_agent.py:851）。
func (a *ReActAgent) SetPromptAttachmentManager(m *hprompts.Manager) {
	a.promptAttachments = m
}
