package harness

import (
	"context"

	bm "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/browser_move"
	hpromts "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	mcptypes "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/subagents"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// CreateBrowserAgent 创建并配置 BrowserAgent DeepAgent 实例。
//
// 对齐 Python: create_browser_agent(model, card=..., system_prompt=..., ...)
// 对齐 Python: openjiuwen/harness/subagents/browser_agent.py (L195-262)
//
// 预定义 BrowserAgent 配备 BrowserRuntimeRail + 运行时工具集，用户可自由覆盖配置。
// Full override rule：如果用户传了 rails，则使用用户的，否则默认注入 [BrowserRuntimeRail()]。
func CreateBrowserAgent(ctx context.Context, params *hschema.SubagentCreateParams) (*DeepAgent, error) {
	language := hpromts.ResolveLanguage(params.Language)

	// 从 model 参数推断 RuntimeSettings
	// 对齐 Python: settings = resolve_runtime_settings(model, factory_kwargs.get("settings"))
	settings := bm.ResolveRuntimeSettings(params.Model, nil)

	// 创建 BrowserAgentRuntime
	runtime := bm.NewBrowserAgentRuntime(
		settings.Provider,
		settings.APIKey,
		settings.APIBase,
		settings.ModelName,
		settings.MCPCfg,
		settings.Guardrails,
	)

	// 构建 runtime tools
	runtimeTools := bm.BuildBrowserRuntimeTools(runtime)

	// 合并用户工具 + 运行时工具
	// 对齐 Python: tools = list(tools or []) + runtime_tools
	allToolInstances := runtimeTools
	if params.ToolInstances != nil {
		allToolInstances = append(allToolInstances, params.ToolInstances...)
	}

	// Full override rule：用户传了 rails 就用用户的，否则默认注入 BrowserRuntimeRail
	// 对齐 Python: final_rails = rails if rails is not None else [BrowserRuntimeRail(runtime)]
	finalRails := params.Rails
	if finalRails == nil {
		finalRails = []sainterfaces.AgentRail{bm.NewBrowserRuntimeRail(runtime)}
	}

	// 默认 AgentCard
	// 对齐 Python: final_card = card or AgentCard(name="browser_agent", description=...)
	card := params.Card
	if card == nil {
		desc := subagents.DefaultBrowserAgentDescription(language)
		card = agentschema.NewAgentCard(
			agentschema.WithAgentName(subagents.BrowserAgentFactoryName),
			agentschema.WithAgentDescription(desc),
		)
	}

	// 默认 SystemPrompt
	// 对齐 Python: final_prompt = system_prompt or DEFAULT_BROWSER_AGENT_SYSTEM_PROMPT.get(...)
	systemPrompt := params.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = subagents.DefaultBrowserAgentSystemPrompt(language)
	}

	// 默认 MaxIterations
	// 对齐 Python: max_iterations=25（browser_agent 比 research_agent 需要更多迭代）
	maxIterations := params.MaxIterations
	if maxIterations == 0 {
		maxIterations = 25
	}

	// RestrictToWorkDir：browser agent 默认 false（浏览器操作不限制工作目录）
	// 对齐 Python: browser_agent 不传 restrict_to_work_dir
	restrictToWorkDir := false
	if params.RestrictToWorkDir != nil {
		restrictToWorkDir = *params.RestrictToWorkDir
	}

	// 合并 MCP 服务器配置：用户配置 + runtime MCP
	// 对齐 Python: mcps = list(mcps or []) + [runtime.service.mcp_cfg]（如果未包含）
	var mcps []*mcptypes.McpServerConfig
	if params.Mcps != nil {
		mcps = append(mcps, params.Mcps...)
	}
	if settings.MCPCfg != nil {
		mcps = append(mcps, settings.MCPCfg)
	}

	// 转换为 CreateDeepAgentParams 并调用工厂
	return CreateDeepAgent(ctx, hconfig.CreateDeepAgentParams{
		Model:              params.Model,
		Card:               card,
		SystemPrompt:       systemPrompt,
		ToolCards:          params.Tools,
		ToolInstances:      allToolInstances,
		Mcps:               mcps,
		Rails:              finalRails,
		EnableTaskLoop:     params.EnableTaskLoop,
		MaxIterations:      maxIterations,
		Workspace:          params.Workspace,
		Skills:             params.Skills,
		Backend:            params.Backend,
		SysOperation:       params.SysOperation,
		Language:           language,
		PromptMode:         params.PromptMode,
		EnableTaskPlanning: params.EnablePlanMode,
		RestrictToWorkDir:  &restrictToWorkDir,
	})
}
