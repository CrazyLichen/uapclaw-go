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
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SubagentRail 子代理委派 Rail。
// 注册 TaskTool（同步）或 SessionTools（异步）到 Agent，
// 并在每轮模型调用前注入对应的 prompt section。
//
// Python: SubagentRail (openjiuwen/harness/rails/subagent/subagent_rail.py)
type SubagentRail struct {
	rails.DeepAgentRail
	// enableAsyncSubagent 是否启用异步子代理
	enableAsyncSubagent bool
	// tools 已注册的工具实例
	tools []tool.Tool
	// toolkit 异步模式的会话任务注册表（仅 async 分支使用）
	// Python: self._toolkit = None  # only used in async branch
	toolkit *hsubagent.SessionToolkit
	// promptBuilder 系统提示词构建器引用
	promptBuilder saprompt.SystemPromptBuilderInterface
}

// SubagentRailOption 配置选项函数
type SubagentRailOption func(*SubagentRail)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// subagentRailPriority SubagentRail 优先级
	// Python: SubagentRail.priority = 95
	subagentRailPriority = 95
)

const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 全局变量 ────────────────────────────

// knownAgentTools 已知代理工具映射
// Python: SubagentRail._KNOWN_AGENT_TOOLS
var knownAgentTools = map[string]string{
	"explore_agent": "bash, glob, grep, list_files, read_file",
	"plan_agent":    "bash, glob, grep, list_files, read_file",
}

// sessionToolkitSetter 设置会话工具包的本地等价接口。
// DeepAgentInterface 无法直接声明 SetSessionToolkit(*SessionToolkit)——
// interfaces ← hsubagent 存在依赖方向（session_tools.go 使用 interfaces.DeepAgentInterface），
// 反向导入会形成循环依赖，故在此处用结构化接口断言等价访问。
// *harness.DeepAgent 通过具体方法签名自动满足本接口。
type sessionToolkitSetter interface {
	SetSessionToolkit(toolkit *hsubagent.SessionToolkit)
}

// 编译时接口检查
// _ 编译时验证 SubagentRail 满足 AgentRail 接口
var _ agentinterfaces.AgentRail = (*SubagentRail)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSubagentRail 创建 SubagentRail 实例。
//
// Python: SubagentRail(enable_async_subagent=False)
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
// Python: SubagentRail.init(agent)
func (r *SubagentRail) Init(_ context.Context, agent agentinterfaces.BaseAgent) error {
	// 捕获 system_prompt_builder
	// Python: self.system_prompt_builder = getattr(agent, "system_prompt_builder", None)
	r.promptBuilder = agent.SystemPromptBuilder()

	// 获取 DeepAgentInterface 以读取 subagents
	// Python: if not agent.deep_config.subagents: skip
	deepAgent, ok := agent.(hinterfaces.DeepAgentInterface)
	if !ok {
		return nil
	}

	deepCfg := deepAgent.DeepConfig()
	if deepCfg == nil || len(deepCfg.Subagents) == 0 {
		logger.Info(logComponent).Msg("[SubagentRail] nosubagentconfig， skipped")
		return nil
	}

	// 构建可用子代理描述
	// Python: available_agents = self._build_available_agents_description(agent.deep_config.subagents)
	availableAgents := r.buildAvailableAgentsDescription(deepCfg.Subagents)

	// 获取语言
	// Python: language=self.system_prompt_builder.language
	language := "cn"
	if r.promptBuilder != nil {
		language = r.promptBuilder.Language()
	}

	// 获取 agentID
	// Python: agent_id = getattr(getattr(agent, "card", None), "id", None)
	agentID := ""
	if card := agent.Card(); card != nil {
		agentID = card.GetID()
	}

	if r.enableAsyncSubagent {
		// 异步模式：SessionToolkit + build_session_tools
		// Python: self._toolkit = SessionToolkit()
		// Python: agent.set_session_toolkit(self._toolkit)
		// Python: self.tools = build_session_tools(parent_agent=agent, toolkit=..., language=..., available_agents=..., agent_id=...)
		r.toolkit = hsubagent.NewSessionToolkit()
		// 接口断言而非直接调用：DeepAgentInterface 不能导入 SessionToolkit
		// （hsubagent → interfaces 已存在，反向导入会形成循环依赖），
		// 此处用本地接口等价访问，与项目"循环依赖用本地等价接口"惯例一致。
		if setter, ok := deepAgent.(sessionToolkitSetter); ok {
			setter.SetSessionToolkit(r.toolkit)
		}
		r.tools = hsubagent.BuildSessionTools(deepAgent, r.toolkit, language, availableAgents, agentID)
	} else {
		// 同步模式：注册 TaskTool
		// Python: self.tools = create_task_tool(parent_agent=agent, available_agents=..., language=..., agent_id=...)
		r.tools = []tool.Tool{
			hsubagent.NewTaskTool(deepAgent, availableAgents, language, agentID),
		}
	}

	// Python: Runner.resource_mgr.add_tool(list(self.tools))
	// Python: for tool in self.tools: agent.ability_manager.add(tool.card)
	am := agent.AbilityManager()
	resourceMgr := runner.GetResourceMgr()
	for _, t := range r.tools {
		if am != nil {
			am.Add(t.Card())
		}
		if resourceMgr != nil {
			_ = resourceMgr.AddTool(t)
		}
	}

	mode := "sync task"
	if r.enableAsyncSubagent {
		mode = "async session"
	}
	logger.Info(logComponent).
		Str("mode", mode).
		Int("subagent_count", len(deepCfg.Subagents)).
		Msg("[SubagentRail] initialized")

	return nil
}

// BeforeModelCall 模型调用前注入 prompt section。
//
// Python: SubagentRail.before_model_call(ctx)
func (r *SubagentRail) BeforeModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if len(r.tools) == 0 || r.promptBuilder == nil {
		return nil
	}

	if !r.enableAsyncSubagent {
		// 同步模式：注入 task_tool prompt section
		// Python: section = build_task_section(language=self.system_prompt_builder.language)
		section := hsections.BuildTaskToolSection(r.promptBuilder.Language())
		r.promptBuilder.RemoveSection(hsections.SectionTaskTool)
		r.promptBuilder.AddSection(section)
		return nil
	}

	// 异步模式：注入 session_tools prompt section
	// Python: section = build_session_tools_section(language=self.system_prompt_builder.language)
	// Go 中 BuildSessionToolsSection 返回值类型（非指针），恒非 nil，
	// 与同步分支的 Remove+Add 注入模式保持一致。
	section := hsections.BuildSessionToolsSection(r.promptBuilder.Language())
	r.promptBuilder.RemoveSection(hsections.SectionSessionTools)
	r.promptBuilder.AddSection(section)
	return nil
}

// Uninit 注销 Rail：移除已注册工具，异步模式清空 session toolkit。
//
// Python: SubagentRail.uninit(agent)
func (r *SubagentRail) Uninit(agent agentinterfaces.BaseAgent) error {
	// Python: if self.tools and hasattr(agent, "ability_manager")
	am := agent.AbilityManager()
	resourceMgr := runner.GetResourceMgr()
	for _, t := range r.tools {
		func(t tool.Tool) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Warn(logComponent).
						Str("event_type", "subagent_rail_uninit").
						Str("tool_name", func() string {
							if t != nil && t.Card() != nil {
								return t.Card().Name
							}
							return ""
						}()).
						Msgf("failed to unregister tool: %v", rec)
				}
			}()
			if t == nil || t.Card() == nil {
				return
			}
			// Python: name = getattr(tool.card, "name", None) → ability_manager.remove(name)
			if am != nil {
				am.Remove(t.Card().Name)
			}
			// Python: tool_id = tool.card.id → Runner.resource_mgr.remove_tool(tool_id)
			if resourceMgr != nil {
				_, _ = resourceMgr.RemoveTool([]string{t.Card().ID})
			}
		}(t)
	}
	r.tools = nil

	// Python: if self.enable_async_subagent: agent.set_session_toolkit(None)
	if r.enableAsyncSubagent {
		if setter, ok := agent.(sessionToolkitSetter); ok {
			setter.SetSessionToolkit(nil)
		}
		r.toolkit = nil
	}

	mode := "sync task"
	if r.enableAsyncSubagent {
		mode = "async session"
	}
	logger.Info(logComponent).
		Str("mode", mode).
		Msg("[SubagentRail] Unregistered tools")

	return nil
}

// GetCallbacks 覆盖基类回调映射，增加 BeforeModelCall。
//
// Python: SubagentRail 隐式覆盖 before_model_call
func (r *SubagentRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	callbacks := r.DeepAgentRail.GetCallbacks()

	callbacks[agentinterfaces.CallbackBeforeModelCall] = func(ctx context.Context, railCtx any) error {
		return r.BeforeModelCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}

	return callbacks
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildAvailableAgentsDescription 构建可用子代理描述字符串。
//
// Python: SubagentRail._build_available_agents_description(subagents)
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
// Python: SubagentRail._extract_agent_meta(spec)
func (r *SubagentRail) extractAgentMeta(spec hschema.SubagentSpec) (string, string) {
	if cfg, ok := spec.(*hschema.SubAgentConfig); ok && cfg.AgentCard != nil {
		return cfg.AgentCard.GetName(), cfg.AgentCard.GetDescription()
	}
	// Python: DeepAgent 实例回退
	// Python: card = getattr(spec, "card", None)
	// Python: name = getattr(card, "name", None) or "general-purpose"
	// Python: description = getattr(card, "description", None) or "DeepAgent instance"
	return "general-purpose", "DeepAgent instance"
}

// extractAgentTools 提取代理工具列表。
// 4 级解析：显式 tools → 已注册 tools → 已知默认 → "All tools"
//
// Python: SubagentRail._extract_agent_tools(spec, agent_name)
func (r *SubagentRail) extractAgentTools(spec hschema.SubagentSpec, agentName string) string {
	// 1. SubAgentConfig 有显式 tools
	// Python: if isinstance(spec, SubAgentConfig) and spec.tools:
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
	// Python: if agent_name in self._KNOWN_AGENT_TOOLS
	if tools, ok := knownAgentTools[agentName]; ok {
		return tools
	}

	// 4. 回退
	// Python: return "All tools"
	return "All tools"
}
