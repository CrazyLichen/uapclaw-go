package harness

import (
	"context"

	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	hpromts "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/subagents"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// CreatePlanAgent 创建并配置 PlanAgent DeepAgent 实例。
// 对齐 Python: create_plan_agent(model, card=..., system_prompt=..., ...)
//
// 预定义 PlanAgent 配备 SysOperationRail(WithReadOnly(true))，用户可自由覆盖配置。
// 完整覆盖规则：如果用户传了 rails，则使用用户的，否则默认注入 [SysOperationRail(WithReadOnly(true))]。
// 对齐 Python: final_rails = rails if rails is not None else [SysOperationRail()]
// Go 额外约束：默认 Rail 使用 WithReadOnly(true) 实现双重只读保障（提示词 + Rail）
func CreatePlanAgent(ctx context.Context, params *hschema.SubagentCreateParams) (*DeepAgent, error) {
	language := hpromts.ResolveLanguage(params.Language)

	// 完整覆盖规则：用户传了 rails 就用用户的，否则默认注入 SysOperationRail(WithReadOnly(true))
	// 对齐 Python: create_plan_agent 中 rails=rails if rails is not None else [SysOperationRail()]
	// Go 增强：默认 Rail 使用 WithReadOnly(true) 双重约束
	finalRails := params.Rails
	if finalRails == nil {
		finalRails = []sainterfaces.AgentRail{rails.NewSysOperationRail(rails.WithReadOnly(true))}
	}

	// 默认 AgentCard
	// 对齐 Python: card or AgentCard(name="plan_agent", description=PLAN_AGENT_DESC.get(...))
	card := params.Card
	if card == nil {
		desc := subagents.DefaultPlanAgentDescription(language)
		card = agentschema.NewAgentCard(
			agentschema.WithAgentName(subagents.PlanAgentFactoryName),
			agentschema.WithAgentDescription(desc),
		)
	}

	// 默认 SystemPrompt
	// 对齐 Python: system_prompt or (PLAN_AGENT_SYSTEM_PROMPT_CN if cn else PLAN_AGENT_SYSTEM_PROMPT_EN)
	systemPrompt := params.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = subagents.DefaultPlanAgentSystemPrompt(language)
	}

	// 默认 MaxIterations
	// 对齐 Python: max_iterations=25
	maxIterations := params.MaxIterations
	if maxIterations == 0 {
		maxIterations = 25
	}

	// RestrictToWorkDir：PlanAgent 默认 false（区别于 ResearchAgent 的 true）
	// 对齐 Python: restrict_to_work_dir=False
	// *bool 指针，nil 时默认 false
	restrictToWorkDir := false
	if params.RestrictToWorkDir != nil {
		restrictToWorkDir = *params.RestrictToWorkDir
	}

	// 转换为 CreateDeepAgentParams 并调用工厂
	// 对齐 Python: return create_deep_agent(model=model, card=final_card, ...)
	return CreateDeepAgent(ctx, hconfig.CreateDeepAgentParams{
		Model:              params.Model,
		Card:               card,
		SystemPrompt:       systemPrompt,
		ToolCards:          params.Tools,
		ToolInstances:      params.ToolInstances,
		Mcps:               params.Mcps,
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
