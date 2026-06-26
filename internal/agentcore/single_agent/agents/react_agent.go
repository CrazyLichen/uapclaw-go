package agents

import (
	"sync"

	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent"
	saconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interrupt"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/resource"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
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

// SystemPromptBuilder 基于节的系统提示词构建器。
//
// 对应 Python: SystemPromptBuilder (openjiuwen/core/single_agent/prompts/builder.py)
type SystemPromptBuilder struct {
	// Language 当前语言（默认 "cn"）
	Language string
	// sections 已注册的节映射：name → PromptSection
	sections map[string]PromptSection
}

// ReActAgent ReAct 循环 Agent：Think → Act → Observe。
//
// 内嵌 BaseAgent 获取配置/管理能力，
// 自行实现 Invoke/Stream，在方法体内显式调用回调骨架。
//
// 对应 Python: ReActAgent (openjiuwen/core/single_agent/agents/react_agent.py)
type ReActAgent struct {
	// base 基础 Agent（提供 Configure/Card/AbilityManager/CallbackManager 等方法）
	base *single_agent.BaseAgent
	// config Agent 配置
	config *saconfig.ReActAgentConfig
	// contextEngine 上下文引擎
	contextEngine ceinterface.ContextEngine
	// llm LLM 模型实例（延迟初始化）
	llm *llm.Model
	// promptBuilder 系统提示词构建器
	promptBuilder *SystemPromptBuilder
	// llmOnce LLM 初始化同步原语
	llmOnce sync.Once
	// kvReleaseWarningLogged KV cache 释放不支持的一次性警告标记
	kvReleaseWarningLogged bool
	// hitlHandler HITL 中断处理器
	hitlHandler *interrupt.ToolInterruptHandler
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore
	// defaultLanguage 默认提示词语言
	defaultLanguage = "cn"
	// defaultMaxIterations 默认最大迭代次数
	defaultMaxIterations = 5
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewReActAgent 创建 ReActAgent 实例。
//
// 对应 Python: ReActAgent.__init__(card)
func NewReActAgent(
	card *agentschema.AgentCard,
	config *saconfig.ReActAgentConfig,
) *ReActAgent {
	base := single_agent.NewBaseAgent(card, &resource.NoopResourceManager{})

	agent := &ReActAgent{
		base:          base,
		config:        config,
		promptBuilder: NewSystemPromptBuilder(),
	}

	// 初始化 HITL 中断处理器
	agent.hitlHandler = interrupt.NewToolInterruptHandler(agent)

	return agent
}

// NewSystemPromptBuilder 创建系统提示词构建器。
func NewSystemPromptBuilder() *SystemPromptBuilder {
	return &SystemPromptBuilder{
		Language: defaultLanguage,
		sections: make(map[string]PromptSection),
	}
}

// NewPromptSection 创建提示节。
func NewPromptSection(name string, content map[string]string, priority int) PromptSection {
	return PromptSection{
		Name:     name,
		Content:  content,
		Priority: priority,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
