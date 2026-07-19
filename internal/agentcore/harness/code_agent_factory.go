package harness

import (
	"context"
	"reflect"

	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	hpromts "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/interrupt"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/subagents"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// requiredRailEntry 必需 Rail 条目，用于 mergeRailsWithRequired 去重合并。
// 对齐 Python: _merge_rails_with_required 的 Sequence[Tuple[type[AgentRail], Callable[[], AgentRail]]]
type requiredRailEntry struct {
	// railType Rail 类型（零值指针），用于 reflect.TypeOf 去重
	railType sainterfaces.AgentRail
	// factory 创建 Rail 实例的工厂函数
	factory func() sainterfaces.AgentRail
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// CreateCodeAgent 创建并配置 CodeAgent DeepAgent 实例。
// 对齐 Python: create_code_agent(model, card=..., system_prompt=..., ...)
//
// 预定义 CodeAgent 配备 SysOperationRail + AgentModeRail + AskUserRail + ConfirmInterruptRail，
// 并自动注入 ExploreAgent + PlanAgent 作为子 Agent。用户可自由覆盖配置。
//
// ⤵️ 9.19-23 CodingMemoryRail 回填：当 EmbeddingConfig 可用时条件注入 CodingMemoryRail
func CreateCodeAgent(ctx context.Context, params *hschema.SubagentCreateParams) (*DeepAgent, error) {
	language := hpromts.ResolveLanguage(params.Language)

	// 1. 注入内置子 Agent（explore_agent + plan_agent）
	// 对齐 Python: effective_subagents = _inject_builtin_plan_agents(list(subagents or []), ...)
	// 将 params.Subagents ([]SubAgentConfig) 转换为 []SubagentSpec
	userSubagents := make([]hschema.SubagentSpec, len(params.Subagents))
	for i := range params.Subagents {
		userSubagents[i] = &params.Subagents[i]
	}
	effectiveSubagents := injectBuiltinPlanAgents(userSubagents, params.Model, language)

	// 2. 合并必需 Rails（去重）
	// 对齐 Python: final_rails = _merge_rails_with_required(rails, [...])
	finalRails := mergeRailsWithRequired(params.Rails, []requiredRailEntry{
		{railType: (*rails.SysOperationRail)(nil), factory: func() sainterfaces.AgentRail { return rails.NewSysOperationRail() }},
		{railType: (*rails.AgentModeRail)(nil), factory: func() sainterfaces.AgentRail { return rails.NewAgentModeRail(nil) }},
		{railType: (*interrupt.AskUserRail)(nil), factory: func() sainterfaces.AgentRail { return interrupt.NewAskUserRail() }},
		{railType: (*interrupt.ConfirmInterruptRail)(nil), factory: func() sainterfaces.AgentRail { return interrupt.NewConfirmInterruptRail("switch_mode") }},
	})

	// ⤵️ 9.19-23 CodingMemoryRail 回填
	// if embeddingConfig != nil {
	//     codingMemoryDir := resolveCodingMemoryDir(params.Workspace)
	//     finalRails = append(finalRails, NewCodingMemoryRail(codingMemoryDir, embeddingConfig, language))
	// }

	// 3. 默认 AgentCard
	// 对齐 Python: final_card = card or AgentCard(name="code_agent", description=...)
	card := params.Card
	if card == nil {
		desc := subagents.DefaultCodeAgentDescription(language)
		card = agentschema.NewAgentCard(
			agentschema.WithAgentName(subagents.CodeAgentFactoryName),
			agentschema.WithAgentDescription(desc),
		)
	}

	// 4. 默认 SystemPrompt
	// 对齐 Python: final_prompt = system_prompt or DEFAULT_CODE_AGENT_SYSTEM_PROMPT.get(...)
	systemPrompt := params.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = subagents.DefaultCodeAgentSystemPrompt(language)
	}

	// 5. 默认 MaxIterations
	// 对齐 Python: max_iterations=15
	maxIterations := params.MaxIterations
	if maxIterations == 0 {
		maxIterations = 15
	}

	// 6. RestrictToWorkDir：CodeAgent 默认 false（需要读写整个代码库）
	// 对齐 Python: create_code_agent 不传 restrict_to_work_dir
	restrictToWorkDir := false
	if params.RestrictToWorkDir != nil {
		restrictToWorkDir = *params.RestrictToWorkDir
	}

	// 7. 转换为 CreateDeepAgentParams 并调用工厂
	// 对齐 Python: return create_deep_agent(model=model, card=final_card, ..., enable_task_planning=True)
	return CreateDeepAgent(ctx, hconfig.CreateDeepAgentParams{
		Model:              params.Model,
		Card:               card,
		SystemPrompt:       systemPrompt,
		ToolCards:          params.Tools,
		ToolInstances:      params.ToolInstances,
		Mcps:               params.Mcps,
		Subagents:          effectiveSubagents,
		Rails:              finalRails,
		EnableTaskLoop:     params.EnableTaskLoop,
		MaxIterations:      maxIterations,
		Workspace:          params.Workspace,
		Skills:             params.Skills,
		Backend:            params.Backend,
		SysOperation:       params.SysOperation,
		Language:           language,
		PromptMode:         params.PromptMode,
		EnableTaskPlanning: true, // 关键区别：CodeAgent 默认启用任务规划
		RestrictToWorkDir:  &restrictToWorkDir,
	})
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// injectBuiltinPlanAgents 自动注入 explore_agent 和 plan_agent 子 Agent（如果缺失）。
// 对齐 Python: _inject_builtin_plan_agents(subagents, *, resolved_language, model)
func injectBuiltinPlanAgents(
	userSubagents []hschema.SubagentSpec,
	model *llm.Model,
	language string,
) []hschema.SubagentSpec {
	effective := make([]hschema.SubagentSpec, len(userSubagents))
	copy(effective, userSubagents)

	// 检查是否已有 explore_agent
	if !hasAgent(effective, "explore_agent") {
		exploreCfg := subagents.BuildExploreAgentConfig(model, &hschema.SubagentCreateParams{
			Model:         model,
			Language:      language,
			MaxIterations: 25,
		})
		effective = append(effective, exploreCfg)
	}

	// 检查是否已有 plan_agent
	if !hasAgent(effective, "plan_agent") {
		planCfg := subagents.BuildPlanAgentConfig(model, &hschema.SubagentCreateParams{
			Model:         model,
			Language:      language,
			MaxIterations: 25,
		})
		effective = append(effective, planCfg)
	}

	return effective
}

// hasAgent 检查子 Agent 列表中是否已有指定名称的 Agent。
// 对齐 Python: _has_agent(subagents, name)
func hasAgent(specs []hschema.SubagentSpec, name string) bool {
	for _, spec := range specs {
		if spec.SpecName() == name {
			return true
		}
	}
	return false
}

// mergeRailsWithRequired 合并用户 Rails 与必需 Rails，按类型去重。
// 对齐 Python: _merge_rails_with_required(user_rails, required_rails)
//
// 去重规则：遍历 required 列表，如果用户 Rails 中已有该类型的实例则跳过，否则追加。
// 使用 reflect.TypeOf 比较（取指针元素的类型）。
func mergeRailsWithRequired(
	userRails []sainterfaces.AgentRail,
	required []requiredRailEntry,
) []sainterfaces.AgentRail {
	merged := make([]sainterfaces.AgentRail, len(userRails))
	copy(merged, userRails)

	for _, req := range required {
		requiredType := reflect.TypeOf(req.railType)
		found := false
		for _, r := range merged {
			if reflect.TypeOf(r) == requiredType {
				found = true
				break
			}
		}
		if !found {
			merged = append(merged, req.factory())
		}
	}

	return merged
}
