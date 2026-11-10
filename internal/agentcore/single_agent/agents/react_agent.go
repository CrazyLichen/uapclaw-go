package agents

import (
	"sync"

	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	hprompts "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/kv_cache"
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
// 直接持有基础字段（card/abilityManager/callbackManager），
// 自行实现 Invoke/Stream，在方法体内显式调用回调骨架。
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
	// usageBuildContext 团队成员的构建上下文（成员名来源，可空）
	// Python: ReActAgent._usage_build_context（native_harness.py L186 注入）
	usageBuildContext UsageIdentityProvider
	// usageMemberName 宿主动态挂在 ReActAgent 上的成员名（Python 侧无声明、无赋值点）
	// Python: getattr(self, "member_name", None)（react_agent.py L1370-1371）
	usageMemberName string
	// usageParentSessionID 显式父会话覆盖（Python 侧无赋值点，仅回退链读取）
	// Python: ReActAgent._usage_parent_session_id（react_agent.py L2890）
	usageParentSessionID string
	// usageParentInvocationID 显式父调用标识覆盖（Python 侧无赋值点）
	// Python: ReActAgent._usage_parent_invocation_id（react_agent.py L2892）
	usageParentInvocationID string
	// usageDelegationID 显式委派标识覆盖（Python 侧无赋值点）
	// Python: ReActAgent._usage_delegation_id（react_agent.py L2901）
	usageDelegationID string
	// usageAgentPath 显式调用链路径覆盖（Python 侧无赋值点）
	// Python: ReActAgent._usage_agent_path（react_agent.py L2906）
	usageAgentPath []string
	// usageDepth 显式调用深度覆盖；nil 表示未提供（Python 侧无赋值点）
	// Python: ReActAgent._usage_depth（react_agent.py L2919）
	usageDepth *int
	// kvCacheHook KV cache 亲和调用钩子（延迟初始化）
	// Python: react_agent.py:591 self._kv_cache_model_call_hook
	kvCacheHook *kv_cache.KVCacheModelCallHook
	// kvCacheAffinityCfg KV cache 亲和开关；nil 等价默认 false
	// Python: self._config.kv_cache_affinity_config
	kvCacheAffinityCfg *kv_cache.KVCacheAffinityConfig
}

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

// ──────────────────────────── 导出函数 ────────────────────────────

// NewReActAgent 创建 ReActAgent 实例。
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
// Python: ReActAgent.prompt_attachment_manager。桥接回调里 ctx.agent 是
// ReActAgent 而非 DeepAgent，rail 需要它才能把动态提示词投递为历史附件。
func (a *ReActAgent) PromptAttachments() *hprompts.Manager { return a.promptAttachments }

// SetPromptAttachmentManager 注入动态提示词管理器（可为 nil）。
// Python: DeepAgent 把 prompt_attachment_manager 共享给内层 ReActAgent
// （deep_agent.py:851）。
func (a *ReActAgent) SetPromptAttachmentManager(m *hprompts.Manager) {
	a.promptAttachments = m
}

// kvCacheModelCallHook 返回本 agent 唯一的 KV 调用钩子（延迟初始化）。
// Python: react_agent.py:591 self._kv_cache_model_call_hook = KVCacheModelCallHook()
// ★ 必须与 recommendation 包重放侧共用同一实例，否则父子 kwargs 可能不一致。
func (a *ReActAgent) kvCacheModelCallHook() *kv_cache.KVCacheModelCallHook {
	if a.kvCacheHook == nil {
		a.kvCacheHook = kv_cache.NewKVCacheModelCallHook()
	}
	return a.kvCacheHook
}

// kvCacheAffinityConfig 返回亲和开关配置；未配置时返回 nil（等价默认 false）。
// Python: self._config.kv_cache_affinity_config
func (a *ReActAgent) kvCacheAffinityConfig() *kv_cache.KVCacheAffinityConfig {
	return a.kvCacheAffinityCfg
}

// KVCacheModelCallHook 导出访问器，供 recommendation 的 _main_call_parameters
// 取同一 hook 实例（Python 侧直接 getattr(agent, "_kv_cache_model_call_hook")）。
func (a *ReActAgent) KVCacheModelCallHook() *kv_cache.KVCacheModelCallHook {
	return a.kvCacheModelCallHook()
}

// SetKVCacheAffinityConfig 注入亲和开关（由 DeepAgent 配置装配时调用）。
func (a *ReActAgent) SetKVCacheAffinityConfig(cfg *kv_cache.KVCacheAffinityConfig) {
	a.kvCacheAffinityCfg = cfg
}
