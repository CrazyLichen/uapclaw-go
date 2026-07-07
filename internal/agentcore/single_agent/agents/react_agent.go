package agents

import (
	"sync"

	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
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
// 对应 Python: ReActAgent (openjiuwen/core/single_agent/agents/react_agent.py)
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
	// 对应 Python: ReActAgent.prompt_builder / ReActAgent.system_prompt_builder
	promptBuilder *prompts.SystemPromptBuilder
	// llmOnce LLM 初始化同步原语
	llmOnce sync.Once
	// kvReleaseWarningLogged KV cache 释放不支持的一次性警告标记
	kvReleaseWarningLogged bool
	// hitlHandler HITL 中断处理器
	hitlHandler *interrupt.ToolInterruptHandler
	// skillUtil 技能工具（延迟初始化，Configure 时根据 sysOperationID 创建）
	// 对应 Python: self._skill_util
	skillUtil *skills.SkillUtil
}

// ──────────────────────────── 常量 ────────────────────────────
const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore
	// defaultMaxIterations 默认最大迭代次数
	defaultMaxIterations = 5
	// identitySection identity 区段名称
	// 对应 Python: _IDENTITY_SECTION = "identity"
	identitySection = "identity"
	// identitySectionPriority identity 区段优先级
	// 对应 Python: _IDENTITY_SECTION_PRIORITY = 10
	identitySectionPriority = 10
	// skillsSection skills 区段名称
	// 对应 Python: _SKILLS_SECTION = "skills"
	skillsSection = "skills"
	// skillsSectionPriority skills 区段优先级
	// 对应 Python: _SKILLS_SECTION_PRIORITY = 90
	skillsSectionPriority = 90
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewReActAgent 创建 ReActAgent 实例。
//
// 对应 Python: ReActAgent.__init__(card)
func NewReActAgent(
	card *agentschema.AgentCard,
	config *saconfig.ReActAgentConfig,
) *ReActAgent {
	agent := &ReActAgent{
		card:            card,
		abilityManager:  ability.NewAbilityManager(nil),
		callbackManager: interfaces.NewAgentCallbackManager(card.ID),
		config:          config,
		promptBuilder:   prompts.NewSystemPromptBuilder(),
	}

	// 初始化 HITL 中断处理器
	agent.hitlHandler = interrupt.NewToolInterruptHandler(agent)

	return agent
}
