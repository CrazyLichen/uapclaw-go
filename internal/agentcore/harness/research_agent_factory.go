package harness

import (
	"context"

	hprompts "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/subagents"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// CreateResearchAgent 创建并配置 ResearchAgent DeepAgent 实例。
// 对齐 Python: create_research_agent(model, card=..., system_prompt=..., ...)
//
// 预定义 ResearchAgent 配备 SysOperationRail，用户可自由覆盖配置。
// Full override rule：如果用户传了 rails，则使用用户的，否则默认注入 [SysOperationRail()]。
// 对齐 Python: final_rails = rails if rails is not None else [SysOperationRail()]
func CreateResearchAgent(ctx context.Context, params *hschema.SubagentCreateParams) (*DeepAgent, error) {
	language := hprompts.ResolveLanguage(params.Language)

	// Full override rule：用户传了 rails 就用用户的，否则默认注入 SysOperationRail
	// 对齐 Python: create_research_agent 中 final_rails = rails if rails is not None else [SysOperationRail()]
	finalRails := params.Rails
	if finalRails == nil {
		finalRails = []sainterfaces.AgentRail{rails.NewSysOperationRail()}
	}

	// 默认 AgentCard
	// 对齐 Python: final_card = card or AgentCard(name="research_agent", description=...)
	card := params.Card
	if card == nil {
		desc := subagents.DefaultResearchAgentDescription(language)
		card = agentschema.NewAgentCard(
			agentschema.WithAgentName(subagents.ResearchAgentFactoryName),
			agentschema.WithAgentDescription(desc),
		)
	}

	// 默认 SystemPrompt
	// 对齐 Python: final_prompt = system_prompt or DEFAULT_RESEARCH_AGENT_SYSTEM_PROMPT.get(...)
	systemPrompt := params.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = subagents.DefaultResearchAgentSystemPrompt(language)
	}

	// 默认 MaxIterations
	// 对齐 Python: max_iterations=15
	maxIterations := params.MaxIterations
	if maxIterations == 0 {
		maxIterations = 15
	}

	// 转换为 CreateDeepAgentParams 并调用工厂
	// 对齐 Python: return create_deep_agent(model=model, card=final_card, ...)
	return CreateDeepAgent(ctx, hconfig.CreateDeepAgentParams{
		Model:              params.Model,
		Card:               card,
		SystemPrompt:       systemPrompt,
		ToolCards:           params.Tools,
		ToolInstances:       params.ToolInstances,
		Mcps:                params.Mcps,
		Rails:               finalRails,
		EnableTaskLoop:      params.EnableTaskLoop,
		MaxIterations:       maxIterations,
		Workspace:           params.Workspace,
		Skills:              params.Skills,
		Backend:             params.Backend,
		SysOperation:        params.SysOperation,
		Language:            language,
		PromptMode:          params.PromptMode,
		EnableTaskPlanning:  params.EnablePlanMode,
		// RestrictToWorkDir：*bool 指针，nil 时默认 true（对齐 Python 默认行为），
		// 非 nil 时使用用户显式指定的值
		RestrictToWorkDir:   restrictToWorkDirValue(params.RestrictToWorkDir, true),
	})
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// restrictToWorkDirValue 从 *bool 指针解析出有效的 RestrictToWorkDir 值。
// p 为 nil 时返回 defaultVal，非 nil 时返回 *p。
func restrictToWorkDirValue(p *bool, defaultVal bool) bool {
	if p == nil {
		return defaultVal
	}
	return *p
}
