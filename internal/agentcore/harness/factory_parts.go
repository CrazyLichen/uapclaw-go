package harness

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ResolveDeepAgentParts 组装 DeepAgent 的 config + rails + tools，不创建实例。
//
// 这是 CreateDeepAgent 的"解析那一半"：默认卡片、工具规范化、语言解析、
// 通用子 agent 注入、workspace 与 sys_operation 构建、config 组装。
// 不修改入参（在副本上操作）。
//
// Python: resolve_deep_agent_parts (harness/factory.py)
func ResolveDeepAgentParts(params *harness_config.CreateDeepAgentParams) (*hschema.DeepAgentParts, error) {
	if params == nil {
		return nil, fmt.Errorf("resolve deep agent parts: params is nil")
	}
	// 复制一份，保证不改写入参
	local := *params

	// 步骤 1：默认 AgentCard
	// Python: factory.py L219-223
	card := local.Card
	if card == nil {
		card = agentschema.NewAgentCard(
			agentschema.WithAgentName("deep_agent"),
			agentschema.WithAgentDescription("DeepAgent instance"),
		)
	}
	local.Card = card

	// 步骤 2：工具规范化
	// Python: factory.py _normalize_tools
	normalizedCards, toolInstances := normalizeTools(local.ToolCards, local.ToolInstances)

	// 步骤 3：语言解析
	// Python: factory.py L232-233
	resolvedLanguage := prompts.ResolveLanguage(local.Language)

	// 步骤 4：通用子 Agent 注入
	// Python: factory.py L235-257
	effectiveSubagents := injectGeneralPurposeSubagent(
		local.Subagents,
		local.AddGeneralPurposeAgent,
		resolvedLanguage,
		local.Rails,
		local.SystemPrompt,
		local.ToolCards,
		local.ToolInstances,
		local.Mcps,
		local.Model,
		local.Skills,
	)

	// 步骤 5：Workspace 构建
	// Python: factory.py L260-265
	workspaceObj := buildWorkspace(local.Workspace, resolvedLanguage)

	// 步骤 6：SysOperation 构建
	// Python: factory.py L267-281
	sysOp, err := buildSysOperation(card, local.SysOperation, local.RestrictToWorkDir)
	if err != nil {
		return nil, fmt.Errorf("构建 SysOperation 失败: %w", err)
	}

	// 步骤 7：DeepAgentConfig 组装
	// Python: factory.py L283-355
	config := buildDeepAgentConfig(&local, card, workspaceObj, sysOp, resolvedLanguage,
		effectiveSubagents, normalizedCards)

	// 步骤 8：默认 rails **在 resolve 阶段就算好并构造实例**，
	// 与上游 factory.py:394-415 一致（all_rails = user + default）。
	// apply 阶段只负责逐个挂载，不再做任何判断。
	allRails := make([]agentinterfaces.AgentRail, 0, len(local.Rails)+8)
	allRails = append(allRails, local.Rails...)
	allRails = append(allRails, addDefaultRails(
		local, config, effectiveSubagents, workspaceObj, resolvedLanguage,
	)...)

	return &hschema.DeepAgentParts{
		Config:        config,
		Rails:         allRails,
		ToolCards:     normalizedCards,
		ToolInstances: toolInstances,
	}, nil
}

// ApplyDeepAgentParts 把 parts 物化成一台 DeepAgent。
//
// 这是 CreateDeepAgent 的"物化那一半"：实例化、配置下发、工具注册、
// rail 挂载 + 默认 rail 自动补齐。
//
// ⚠️ 偏差（并入 D-9）：默认 rails 在此补齐（上游在 resolve 阶段算好放进
// parts）。原因：addDefaultRails 依赖 DeepAgent 与本包私有能力，
// schema 不能引用它（会成环）。**最终 agent 上的 rail 集合完全一致**。
func ApplyDeepAgentParts(ctx context.Context, parts *hschema.DeepAgentParts) (*DeepAgent, error) {
	if parts == nil || parts.Config == nil {
		return nil, fmt.Errorf("apply deep agent parts: parts or config is nil")
	}
	card := parts.Config.Card

	// 步骤 8：实例化
	agent := NewDeepAgent(card)
	if err := agent.ConfigureDeepConfig(ctx, parts.Config); err != nil {
		return nil, fmt.Errorf("配置 DeepAgent 失败: %w", err)
	}

	// 步骤 9：工具注册
	if len(parts.ToolInstances) > 0 {
		tag := card.GetID()
		if tag == "" {
			tag = card.GetName()
		}
		if regErr := registerToolInstances(parts.ToolInstances, tag); regErr != nil {
			logger.Warn(logComponent).Err(regErr).Msg("failed to register tool instance")
		}
	}
	for _, tc := range parts.ToolCards {
		agent.abilityManager.Add(tc)
	}

	// 步骤 10：Rail 注册
	for _, r := range parts.Rails {
		agent.AddRail(r)
	}
	return agent, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildDeepAgentConfig 组装 DeepAgentConfig（factory.go 步骤 7 的抽取版）。
func buildDeepAgentConfig(
	params *harness_config.CreateDeepAgentParams,
	card *agentschema.AgentCard,
	workspaceObj *workspace.Workspace,
	sysOp sysop.SysOperation,
	resolvedLanguage string,
	effectiveSubagents []hschema.SubagentSpec,
	normalizedCards []*tool.ToolCard,
) *hschema.DeepAgentConfig {
	config := hschema.NewDeepAgentConfig()
	config.Model = params.Model
	config.Card = card
	config.SystemPrompt = params.SystemPrompt
	config.EnableTaskLoop = params.EnableTaskLoop
	config.ProgressiveToolEnabled = params.ProgressiveToolEnabled
	if params.ToolSearchLimit > 0 {
		config.ToolSearchLimit = params.ToolSearchLimit
	}
	config.EnableAsyncSubagent = params.EnableAsyncSubagent
	config.AddGeneralPurposeAgent = params.AddGeneralPurposeAgent
	config.MaxIterations = params.MaxIterations
	if params.TaskLoopNoProgressGuard != nil {
		config.TaskLoopNoProgressGuard = *params.TaskLoopNoProgressGuard
	}
	config.SkillBudgetMaxSkills = params.SkillBudgetMaxSkills
	config.SkillBudgetMaxTotalChars = params.SkillBudgetMaxTotalChars
	// Python: subagents=effective_subagents or None（空列表 → nil）
	if len(effectiveSubagents) > 0 {
		config.Subagents = effectiveSubagents
	}
	if len(normalizedCards) > 0 {
		config.Tools = normalizedCards
	}
	config.Mcps = params.Mcps
	config.Workspace = workspaceObj
	config.Skills = params.Skills
	config.EnableSkillDiscovery = params.EnableSkillDiscovery
	config.Backend = params.Backend
	config.SysOperation = sysOp
	config.Language = resolvedLanguage
	config.PromptMode = params.PromptMode
	config.VisionModelConfig = params.VisionModelConfig
	config.AudioModelConfig = params.AudioModelConfig
	config.EnableReadImageMultimodal = params.EnableReadImageMultimodal
	config.DefaultMode = params.DefaultMode
	config.EnablePlanMode = params.EnableTaskPlanning
	config.ModelSelection = params.ModelSelection
	config.AutoCreateWorkspace = params.AutoCreateWorkspace
	if params.CompletionTimeout > 0 {
		config.CompletionTimeout = params.CompletionTimeout
	}
	// Python: factory.py L305-310「Forward extra kwargs to config fields」
	if params.ContextEngineConfig != nil {
		config.ContextEngineConfig = params.ContextEngineConfig
	}
	return config
}

// ──────────────────────────── 非导出函数（init 注入） ────────────────────────────

// init 把默认 rails 计算器注入 schema 包。
//
// schema 包不能 import harness（成环），而 spec.ResolveParts 又需要
// addDefaultRails 的结果。用构造期函数指针注入，避免 Go embedding
// 虚方法派发陷阱（基类方法体内 t.xxx() 会静态绑定到空实现）。
func init() {
	hschema.SetDefaultRailsResolver(computeDefaultRails)
}

// computeDefaultRails 把 spec 字段摊成 addDefaultRails 所需的参数并计算。
//
// 与 ResolveDeepAgentParts 里的调用完全同源（同一个 addDefaultRails），
// 只是入参来源从 CreateDeepAgentParams 变成 DeepAgentSpec。
//
// Python: resolve_deep_agent_parts 的 default_rails 循环 (factory.py:394-415)
func computeDefaultRails(spec *hschema.DeepAgentSpec, config *hschema.DeepAgentConfig, userRails []agentinterfaces.AgentRail) []agentinterfaces.AgentRail {
	params := harness_config.CreateDeepAgentParams{
		Rails:               userRails, // 防重复追加：addDefaultRails 会检查已提供的同类型 rail
		EnableTaskPlanning:  spec.EnableTaskPlanning,
		EnableAsyncSubagent: spec.EnableAsyncSubagent,
		Skills:              spec.Skills,
	}
	return addDefaultRails(
		params, config, config.Subagents, config.Workspace, config.Language,
	)
}
