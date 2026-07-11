package factory

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/spawn"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/agents"
	saconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DefaultAgentCreator 默认 Agent 创建器，用 switch 按类型名直接创建。
// 对齐 Python: importlib.import_module + getattr + cls(**kwargs)
type DefaultAgentCreator struct{}

// ──────────────────────────── 常量 ────────────────────────────
const (
	// AgentTypeReAct ReAct Agent 类型名
	AgentTypeReAct = "react_agent"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译期校验：DefaultAgentCreator 必须满足 spawn.AgentCreator 接口
var _ spawn.AgentCreator = (*DefaultAgentCreator)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewDefaultAgentCreator 创建默认 Agent 创建器。
func NewDefaultAgentCreator() *DefaultAgentCreator {
	return &DefaultAgentCreator{}
}

// SupportedAgentTypes 返回当前支持的 agent_type 列表。
func SupportedAgentTypes() []string {
	return []string{AgentTypeReAct}
}

// CreateByType 根据 agent_type 创建 Agent 实例。
// 对齐 Python:
//
//	module = importlib.import_module(class_config.agent_module)
//	agent_cls = getattr(module, class_config.agent_class)
//	agent = agent_cls(**class_config.init_kwargs)
//
// agentCard 为 map[string]any 格式的 AgentCard（从 spawn 消息反序列化），
// 内部反序列化为 schema.AgentCard 后创建 Agent。
// 新增 Agent 类型时只需在此方法中加一个 case。
func (c *DefaultAgentCreator) CreateByType(
	ctx context.Context,
	agentType string,
	agentCard map[string]any,
	initKwargs map[string]any,
) (interfaces.BaseAgent, error) {
	// 反序列化 AgentCard
	card := schema.NewAgentCard()
	if agentCard != nil {
		cardData, err := json.Marshal(agentCard)
		if err != nil {
			return nil, fmt.Errorf("序列化 AgentCard 失败: %w", err)
		}
		if err := json.Unmarshal(cardData, card); err != nil {
			return nil, fmt.Errorf("反序列化 AgentCard 失败: %w", err)
		}
	}

	switch agentType {
	case AgentTypeReAct:
		// 从 initKwargs 构建 ReActAgentConfig。
		// 对齐 Python: agent = ReActAgent(**init_kwargs)
		reactCfg := buildReActAgentConfig(initKwargs)
		return agents.NewReActAgent(card, reactCfg), nil
	default:
		return nil, fmt.Errorf("不支持的 agent_type: %s（支持: %v）",
			agentType, SupportedAgentTypes())
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildReActAgentConfig 从 initKwargs 构建 ReActAgentConfig。
// 对齐 Python: agent = agent_cls(**init_kwargs)
func buildReActAgentConfig(kwargs map[string]any) *saconfig.ReActAgentConfig {
	opts := make([]saconfig.ReActAgentConfigOption, 0)

	if kwargs == nil {
		return saconfig.NewReActAgentConfig(opts...)
	}

	if v, ok := kwargs["model_name"].(string); ok && v != "" {
		opts = append(opts, saconfig.WithModelName(v))
	}
	if v, ok := kwargs["model_provider"].(string); ok && v != "" {
		opts = append(opts, saconfig.WithModelProvider(v))
	}
	if v, ok := kwargs["api_key"].(string); ok && v != "" {
		opts = append(opts, saconfig.WithAPIKey(v))
	}
	if v, ok := kwargs["api_base"].(string); ok && v != "" {
		opts = append(opts, saconfig.WithAPIBase(v))
	}
	if v, ok := kwargs["max_iterations"].(float64); ok && v > 0 {
		opts = append(opts, saconfig.WithMaxIterations(int(v)))
	}
	if v, ok := kwargs["prompt_template_name"].(string); ok && v != "" {
		opts = append(opts, saconfig.WithPromptTemplateName(v))
	}

	return saconfig.NewReActAgentConfig(opts...)
}
