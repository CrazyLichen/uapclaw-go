package subagent

import (
	"context"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	hsections "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	hsubagent "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/subagent"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SubagentRail 子代理委派 Rail。
// 注册 TaskTool（同步）或 SessionTools（异步）到 Agent，
// 并在每轮模型调用前注入对应的 prompt section。
//
// 对齐 Python: SubagentRail (openjiuwen/harness/rails/subagent/subagent_rail.py)
type SubagentRail struct {
	rails.DeepAgentRail
	// enableAsyncSubagent 是否启用异步子代理
	enableAsyncSubagent bool
	// tools 已注册的工具实例
	tools []tool.Tool
	// promptBuilder 系统提示词构建器引用
	promptBuilder saprompt.SystemPromptBuilderInterface
}

// SubagentRailOption 配置选项函数
type SubagentRailOption func(*SubagentRail)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// subagentRailPriority SubagentRail 优先级
	// 对齐 Python: SubagentRail.priority = 95
	subagentRailPriority = 95
)

// ──────────────────────────── 全局变量 ────────────────────────────

// knownAgentTools 已知代理工具映射
// 对齐 Python: SubagentRail._KNOWN_AGENT_TOOLS
var knownAgentTools = map[string]string{
	"explore_agent": "bash, glob, grep, list_files, read_file",
	"plan_agent":    "bash, glob, grep, list_files, read_file",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSubagentRail 创建 SubagentRail 实例。
//
// 对齐 Python: SubagentRail(enable_async_subagent=False)
func NewSubagentRail(opts ...SubagentRailOption) *SubagentRail {
	r := &SubagentRail{
		DeepAgentRail:       *rails.NewDeepAgentRail(),
		enableAsyncSubagent: false,
	}
	r.WithPriority(subagentRailPriority)
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// WithEnableAsyncSubagent 设置是否启用异步子代理。
func WithEnableAsyncSubagent(enabled bool) SubagentRailOption {
	return func(r *SubagentRail) { r.enableAsyncSubagent = enabled }
}

// Init 初始化钩子：捕获 system_prompt_builder，注册 TaskTool。
//
// 对齐 Python: SubagentRail.init(agent)
func (r *SubagentRail) Init(agent agentinterfaces.BaseAgent) error {
	// 捕获 system_prompt_builder
	// 对齐 Python: self.system_prompt_builder = getattr(agent, "system_prompt_builder", None)
	r.promptBuilder = agent.SystemPromptBuilder()

	// 获取 DeepAgentInterface 以读取 subagents
	// 对齐 Python: if not agent.deep_config.subagents: skip
	deepAgent, ok := agent.(hinterfaces.DeepAgentInterface)
	if !ok {
		return nil
	}

	deepCfg := deepAgent.DeepConfig()
	if deepCfg == nil || len(deepCfg.Subagents) == 0 {
		logger.Info(logger.ComponentAgentCore).Msg("[SubagentRail] 无子代理配置，跳过")
		return nil
	}

	// 构建可用子代理描述
	// 对齐 Python: available_agents = self._build_available_agents_description(agent.deep_config.subagents)
	availableAgents := r.buildAvailableAgentsDescription(deepCfg.Subagents)

	// 获取语言
	// 对齐 Python: language=self.system_prompt_builder.language
	language := "cn"
	if r.promptBuilder != nil {
		language = r.promptBuilder.Language()
	}

	// 获取 agentID
	// 对齐 Python: agent_id = getattr(getattr(agent, "card", None), "id", None)
	agentID := ""
	if card := agent.Card(); card != nil {
		agentID = card.GetID()
	}

	if r.enableAsyncSubagent {
		// ⤵️ 异步模式待回填：SessionToolkit + build_session_tools
		logger.Info(logger.ComponentAgentCore).
			Bool("enable_async_subagent", true).
			Int("subagent_count", len(deepCfg.Subagents)).
			Msg("[SubagentRail] 异步模式暂未实现")
	} else {
		// 同步模式：注册 TaskTool
		// 对齐 Python: self.tools = create_task_tool(parent_agent=agent, available_agents=..., language=..., agent_id=...)
		r.tools = []tool.Tool{
			hsubagent.NewTaskTool(deepAgent, availableAgents, language, agentID),
		}
	}

	// 对齐 Python: Runner.resource_mgr.add_tool(list(self.tools))
	// 对齐 Python: for tool in self.tools: agent.ability_manager.add(tool.card)
	// 注意：工具注册由 factory.go 的 addToolsToResourceManager 统一处理
	// 这里仅持有工具引用，供 BeforeModelCall 使用

	mode := "sync task"
	if r.enableAsyncSubagent {
		mode = "async session"
	}
	logger.Info(logger.ComponentAgentCore).
		Str("mode", mode).
		Int("subagent_count", len(deepCfg.Subagents)).
		Msg("[SubagentRail] 已初始化")

	return nil
}

// BeforeModelCall 模型调用前注入 prompt section。
//
// 对齐 Python: SubagentRail.before_model_call(ctx)
func (r *SubagentRail) BeforeModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if len(r.tools) == 0 || r.promptBuilder == nil {
		return nil
	}

	if !r.enableAsyncSubagent {
		// 同步模式：注入 task_tool prompt section
		// 对齐 Python: section = build_task_section(language=self.system_prompt_builder.language)
		section := hsections.BuildTaskToolSection(r.promptBuilder.Language())
		r.promptBuilder.RemoveSection(hsections.SectionTaskTool)
		r.promptBuilder.AddSection(section)
		return nil
	}

	// ⤵️ 异步模式待回填：注入 session_tools prompt section
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildAvailableAgentsDescription 构建可用子代理描述字符串。
//
// 对齐 Python: SubagentRail._build_available_agents_description(subagents)
func (r *SubagentRail) buildAvailableAgentsDescription(subagents []hschema.SubagentSpec) string {
	if len(subagents) == 0 {
		return ""
	}

	var lines []string
	for _, spec := range subagents {
		name, desc := r.extractAgentMeta(spec)
		toolsStr := r.extractAgentTools(spec, name)
		lines = append(lines, fmt.Sprintf("- %s: %s (Tools: %s)", name, desc, toolsStr))
	}
	return strings.Join(lines, "\n")
}

// extractAgentMeta 提取代理名称和描述。
//
// 对齐 Python: SubagentRail._extract_agent_meta(spec)
func (r *SubagentRail) extractAgentMeta(spec hschema.SubagentSpec) (string, string) {
	if cfg, ok := spec.(*hschema.SubAgentConfig); ok && cfg.AgentCard != nil {
		return cfg.AgentCard.GetName(), cfg.AgentCard.GetDescription()
	}
	// 对齐 Python: DeepAgent 实例回退
	// card = getattr(spec, "card", None)
	// name = getattr(card, "name", None) or "general-purpose"
	// description = getattr(card, "description", None) or "DeepAgent instance"
	return "general-purpose", "DeepAgent instance"
}

// extractAgentTools 提取代理工具列表。
// 4 级解析：显式 tools → 已注册 tools → 已知默认 → "All tools"
//
// 对齐 Python: SubagentRail._extract_agent_tools(spec, agent_name)
func (r *SubagentRail) extractAgentTools(spec hschema.SubagentSpec, agentName string) string {
	// 1. SubAgentConfig 有显式 tools
	// 对齐 Python: if isinstance(spec, SubAgentConfig) and spec.tools:
	if cfg, ok := spec.(*hschema.SubAgentConfig); ok && len(cfg.Tools) > 0 {
		var names []string
		for _, t := range cfg.Tools {
			if t != nil && t.GetName() != "" {
				names = append(names, t.GetName())
			}
		}
		if len(names) > 0 {
			return strings.Join(names, ", ")
		}
	}

	// 2. DeepAgent 实例的已注册工具 — Go 中暂不实现（需要 ability_manager 访问）

	// 3. 已知默认
	// 对齐 Python: if agent_name in self._KNOWN_AGENT_TOOLS
	if tools, ok := knownAgentTools[agentName]; ok {
		return tools
	}

	// 4. 回退
	// 对齐 Python: return "All tools"
	return "All tools"
}

// compile-time check
var _ agentinterfaces.AgentRail = (*SubagentRail)(nil)
