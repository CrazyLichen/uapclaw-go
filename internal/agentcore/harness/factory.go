package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	mcptypes "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	hprompts "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	hpromptstools "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/interrupt"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	resourcesmanager "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/resources_manager"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// freeSearchDDGEnabledEnv 免费搜索 DDG 启用环境变量
	// 对齐 Python: _FREE_SEARCH_DDG_ENABLED_ENV
	freeSearchDDGEnabledEnv = "FREE_SEARCH_DDG_ENABLED"
	// freeSearchBingEnabledEnv 免费搜索 Bing 启用环境变量
	// 对齐 Python: _FREE_SEARCH_BING_ENABLED_ENV
	freeSearchBingEnabledEnv = "FREE_SEARCH_BING_ENABLED"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// paidSearchAPIKeyEnvs 付费搜索 API Key 环境变量列表
	// 对齐 Python: _PAID_SEARCH_API_KEY_ENVS
	paidSearchAPIKeyEnvs = []string{
		"PERPLEXITY_API_KEY",
		"BOCHA_API_KEY",
		"JINA_API_KEY",
		"SERPER_API_KEY",
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// CreateDeepAgent 创建并配置 DeepAgent 实例。
//
// 按以下 10 步流程组装，对齐 Python create_deep_agent()：
//  1. 默认 AgentCard
//  2. 工具规范化 (normalizeTools)
//  3. 语言解析 (ResolveLanguage)
//  4. 通用子 Agent 注入 (injectGeneralPurposeSubagent)
//  5. Workspace 构建 (buildWorkspace)
//  6. SysOperation 构建 (buildSysOperation)
//  7. DeepAgentConfig 组装
//  8. DeepAgent 实例化 (NewDeepAgent + ConfigureDeepConfig)
//  9. 工具注册 (registerToolInstances + ability_manager.add)
//
// 10. Rail 注册 (显式 + 默认自动添加)
//
// 对应 Python: openjiuwen/harness/factory.py create_deep_agent()
func CreateDeepAgent(ctx context.Context, params hconfig.CreateDeepAgentParams) (*DeepAgent, error) {
	// ── 步骤 1：默认 AgentCard ──
	// 对齐 Python: factory.py L219-223
	card := params.Card
	if card == nil {
		card = agentschema.NewAgentCard(
			agentschema.WithAgentName("deep_agent"),
			agentschema.WithAgentDescription("DeepAgent instance"),
		)
	}

	// ── 步骤 2：工具规范化 ──
	// 对齐 Python: factory.py _normalize_tools(tools: List[Tool | ToolCard])
	normalizedCards, toolInstances := normalizeTools(params.ToolCards, params.ToolInstances)

	// ── 步骤 3：语言解析 ──
	// 对齐 Python: factory.py L232-233
	resolvedLanguage := hprompts.ResolveLanguage(params.Language)

	// ── 步骤 4：通用子 Agent 注入 ──
	// 对齐 Python: factory.py L235-257
	effectiveSubagents := injectGeneralPurposeSubagent(
		params.Subagents,
		params.AddGeneralPurposeAgent,
		resolvedLanguage,
		params.Rails,
		params.SystemPrompt,
		params.ToolCards,
		params.ToolInstances,
		params.Mcps,
		params.Model,
		params.Skills,
	)

	// ── 步骤 5：Workspace 构建 ──
	// 对齐 Python: factory.py L260-265
	workspaceObj := buildWorkspace(params.Workspace, resolvedLanguage)

	// ── 步骤 6：SysOperation 构建 ──
	// 对齐 Python: factory.py L267-281
	sysOp, err := buildSysOperation(card, params.SysOperation, params.RestrictToWorkDir)
	if err != nil {
		return nil, fmt.Errorf("构建 SysOperation 失败: %w", err)
	}

	// ── 步骤 7：DeepAgentConfig 组装 ──
	// 对齐 Python: factory.py L283-355
	config := hschema.NewDeepAgentConfig()
	config.Model = params.Model
	config.Card = card
	config.SystemPrompt = params.SystemPrompt
	config.EnableTaskLoop = params.EnableTaskLoop
	config.EnableAsyncSubagent = params.EnableAsyncSubagent
	config.AddGeneralPurposeAgent = params.AddGeneralPurposeAgent
	config.MaxIterations = params.MaxIterations
	// 对齐 Python: subagents=effective_subagents or None（空列表 → nil）
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

	// ── 步骤 8：DeepAgent 实例化 ──
	// 对齐 Python: factory.py L356-357
	agent := NewDeepAgent(card)
	if cfgErr := agent.ConfigureDeepConfig(ctx, config); cfgErr != nil {
		return nil, fmt.Errorf("配置 DeepAgent 失败: %w", cfgErr)
	}

	// ── 步骤 9：工具注册 ──
	// 对齐 Python: factory.py L329-344
	if len(toolInstances) > 0 {
		tag := card.GetID()
		if tag == "" {
			tag = card.GetName()
		}
		if regErr := registerToolInstances(toolInstances, tag); regErr != nil {
			logger.Warn(logComponent).Err(regErr).Msg("注册工具实例失败")
		}
	}
	for _, tc := range normalizedCards {
		agent.abilityManager.Add(tc)
	}

	// ── 步骤 10：Rail 注册 ──
	// 对齐 Python: factory.py L346-367
	// 10a：显式提供的 Rails
	for _, r := range params.Rails {
		agent.AddRail(r)
	}

	// 10b：默认 Rails 自动添加
	addDefaultRails(agent, params, config, effectiveSubagents, workspaceObj, resolvedLanguage)

	return agent, nil
}

// ──────────────────────────── 导出函数 ────────────────────────────

// IsFreeSearchEnabled 检查是否至少启用一个免费搜索后端。
// 对齐 Python: is_free_search_enabled() (web_tools.py line 444)
func IsFreeSearchEnabled() bool {
	return envFlag(freeSearchDDGEnabledEnv, false) || envFlag(freeSearchBingEnabledEnv, false)
}

// IsPaidSearchEnabled 检查是否至少配置一个付费搜索 API Key。
// 对齐 Python: is_paid_search_enabled() (web_tools.py line 452)
func IsPaidSearchEnabled() bool {
	for _, key := range paidSearchAPIKeyEnvs {
		if strings.TrimSpace(os.Getenv(key)) != "" {
			return true
		}
	}
	return false
}

// ResetFreeSearchRuntimeFlags 重置免费搜索运行时标志为禁用。
//
// 每次启动时调用，确保进程以禁用状态开始，后续 .env 加载或 UI 操作会覆盖。
// 对应 Python: reset_free_search_runtime_flags()
func ResetFreeSearchRuntimeFlags() {
	_ = os.Setenv(freeSearchDDGEnabledEnv, "false")
	_ = os.Setenv(freeSearchBingEnabledEnv, "false")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// normalizeTools 将 ToolCard 列表和 Tool 实例列表统一规范化，
// 合并为 ToolCard 列表和 Tool 实例列表，同时过滤被禁用的 free_search 工具。
// 对齐 Python: _normalize_tools(tools: List[Tool | ToolCard])
func normalizeTools(toolCards []*tool.ToolCard, toolInstances []tool.Tool) (normalizedCards []*tool.ToolCard, mergedInstances []tool.Tool) {
	// 纯 ToolCard 直接加入 normalizedCards
	for _, tc := range toolCards {
		if isDisabledFreeSearchTool(tc) {
			logger.Debug(logComponent).Str("tool_name", tc.GetName()).Msg("跳过被禁用的 free_search 工具")
			continue
		}
		normalizedCards = append(normalizedCards, tc)
	}
	// Tool 实例：card 加入 normalizedCards，实例加入 mergedInstances
	for _, t := range toolInstances {
		card := t.Card()
		if isDisabledFreeSearchTool(card) {
			logger.Debug(logComponent).Str("tool_name", card.GetName()).Msg("跳过被禁用的 free_search 工具")
			continue
		}
		normalizedCards = append(normalizedCards, card)
		mergedInstances = append(mergedInstances, t)
	}
	return
}

// isDisabledFreeSearchTool 检查工具是否为被禁用的 free_search 工具。
// 对齐 Python: _is_disabled_free_search_tool(tool)
func isDisabledFreeSearchTool(card *tool.ToolCard) bool {
	if card == nil {
		return false
	}
	if card.GetName() != "free_search" {
		return false
	}
	return !IsFreeSearchEnabled()
}

// envFlag 解析布尔型环境变量值，保留空值时的默认值。
// 对齐 Python: _env_flag(name, default) (web_tools.py line 436)
func envFlag(name string, defaultVal bool) bool {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv(name)))
	if raw == "" {
		return defaultVal
	}
	return raw == "1" || raw == "true" || raw == "yes" || raw == "on" || raw == "enabled"
}

// registerToolInstances 将 Tool 实例注册到全局资源管理器，
// 使 ToolCard 变为可执行。同 ID 已存在时仅追加 tag。
//
// 对应 Python: _register_tool_instances(tool_instances, tag=tag)
// 注意：Python 用 Runner.resource_mgr.get_tool(tool.card.id) 返回单个，
// Go 用 GetTool([]string{toolID}) 返回切片。
// Python 检查 existing_tool is not tool（同一指针），
// Go 无法做接口指针比较，改为检查 ID 存在即视为已注册。
func registerToolInstances(toolInstances []tool.Tool, tag string) error {
	rm := runner.GetResourceMgr()
	if rm == nil {
		return fmt.Errorf("全局资源管理器未初始化")
	}

	for _, t := range toolInstances {
		card := t.Card()
		toolID := card.GetID()
		if toolID == "" {
			toolID = card.GetName()
		}

		// 检查是否已注册
		existing, err := rm.GetTool([]string{toolID})
		if err == nil && len(existing) > 0 {
			// 同 ID 已注册，仅追加 tag
			_, tagErr := rm.AddResourceTag(toolID, []resourcesmanager.Tag{tag})
			if tagErr != nil {
				logger.Warn(logComponent).Str("tool_id", toolID).Err(tagErr).Msg("添加资源标签失败")
			}
			continue
		}

		// 注册新 Tool 实例
		if addErr := rm.AddTool(t, resourcesmanager.WithTag(resourcesmanager.Tag(tag))); addErr != nil {
			return fmt.Errorf("注册工具失败: tool_id=%s, err=%w", toolID, addErr)
		}

		logger.Info(logComponent).
			Str("tool_id", toolID).
			Str("tag", tag).
			Msg("工具实例已注册到资源管理器")
	}
	return nil
}

// injectGeneralPurposeSubagent 当 addGeneralPurposeAgent=True 时注入通用子 Agent。
// 已存在名为 general-purpose 的子 Agent 时不重复注入。
// 从调用方的 agentRails 中过滤掉 SubagentRail，确保有 SysOperationRail。
//
// 对应 Python: _inject_general_purpose_subagent()
func injectGeneralPurposeSubagent(
	subagents []hschema.SubagentSpec,
	addGeneralPurposeAgent bool,
	resolvedLanguage string,
	agentRails []agentinterfaces.AgentRail,
	systemPrompt string,
	toolCards []*tool.ToolCard,
	toolInstances []tool.Tool,
	mcps []*mcptypes.McpServerConfig,
	model *llm.Model,
	skills []string,
) []hschema.SubagentSpec {
	effectiveSubagents := make([]hschema.SubagentSpec, len(subagents))
	copy(effectiveSubagents, subagents)

	if !addGeneralPurposeAgent {
		return effectiveSubagents
	}

	// 检查是否已存在 general-purpose 子 Agent
	hasGP := false
	for _, s := range effectiveSubagents {
		if subCfg, ok := s.(*hschema.SubAgentConfig); ok && subCfg.AgentCard != nil && subCfg.AgentCard.GetName() == "general-purpose" {
			hasGP = true
			break
		}
	}
	if hasGP {
		logger.Debug(logComponent).Msg("general-purpose 子 Agent 已存在，跳过注入")
		return effectiveSubagents
	}

	// 获取描述
	desc, ok := hpromptstools.GeneralPurposeAgentDesc[resolvedLanguage]
	if !ok {
		desc = hpromptstools.GeneralPurposeAgentDesc["cn"]
	}

	// 构建 gp_rails：过滤掉 SubagentRail，确保有 SysOperationRail
	gpRails := make([]agentinterfaces.AgentRail, 0, len(agentRails))
	hasSysOpRail := false
	for _, r := range agentRails {
		// ⤵️ SubagentRail 类型断言待回填：SubagentRail 尚未实现，暂不过滤
		if _, ok := r.(*rails.SysOperationRail); ok {
			hasSysOpRail = true
		}
		gpRails = append(gpRails, r)
	}
	// 确保 gpRails 中有 SysOperationRail
	if !hasSysOpRail {
		gpRails = append(gpRails, rails.NewSysOperationRail())
	}

	// 注入到列表头部
	// 对齐 Python: SubAgentConfig(tools=list(tools or []))
	// toolCards 和 toolInstances 原样透传，后续 normalizeTools 统一拆分
	gpConfig := hschema.SubAgentConfig{
		AgentCard: agentschema.NewAgentCard(
			agentschema.WithAgentName("general-purpose"),
			agentschema.WithAgentDescription(desc),
		),
		SystemPrompt:      systemPrompt,
		Tools:             toolCards,
		ToolInstances:     toolInstances,
		Mcps:              mcps,
		Model:             model,
		Rails:             gpRails,
		Skills:            skills,
		RestrictToWorkDir: false,
	}

	effectiveSubagents = append([]hschema.SubagentSpec{&gpConfig}, effectiveSubagents...)
	logger.Info(logComponent).Str("language", resolvedLanguage).Msg("已注入 general-purpose 子 Agent")
	return effectiveSubagents
}

// buildWorkspace 构建 Workspace 实例。
// 传入 nil 时创建默认 Workspace(root_path="./")，传入已有实例时直接返回。
//
// 对齐 Python: factory.py L260-265 workspace 构建
func buildWorkspace(ws *workspace.Workspace, language string) *workspace.Workspace {
	if ws != nil {
		return ws
	}
	// Python: workspace = Workspace(root_path="./", language=language)
	return workspace.NewWorkspace("./", language)
}

// buildSysOperation 构建 SysOperation 实例。
// 调用方未提供时，自动创建默认 SysOperationCard（LocalWorkConfig 模式）并注册到 resource_mgr。
//
// 对齐 Python: factory.py L267-281 sys_operation 构建
func buildSysOperation(card *agentschema.AgentCard, sysOp sysop.SysOperation, restrictToWorkDir bool) (sysop.SysOperation, error) {
	if sysOp != nil {
		return sysOp, nil
	}

	// 构建 SysOperationCard
	cardName := "deep_agent"
	cardID := ""
	if card != nil {
		cardName = card.GetName()
		cardID = card.GetID()
	}
	sysopID := fmt.Sprintf("%s_%s", cardName, cardID)

	sysopCard := sysop.NewSysOperationCard(
		sysop.WithSysOpMode(sysop.OperationModeLocal),
		sysop.WithSysOpWorkConfig(&sysop.LocalWorkConfig{
			RestrictToSandbox: restrictToWorkDir,
		}),
	)
	sysopCard.ID = sysopID

	logger.Info(logComponent).
		Str("sysop_id", sysopID).
		Str("mode", sysop.OperationModeLocal.String()).
		Bool("restrict_to_work_dir", restrictToWorkDir).
		Msg("已创建默认 SysOperationCard")

	// 注册到全局资源管理器
	// 对齐 Python: Runner.resource_mgr.add_sys_operation(sysop_card)
	// Go 签名：AddSysOperation(id, instance)
	localSysOp := sysop.NewLocalSysOperation(sysopCard)
	rm := runner.GetResourceMgr()
	if rm != nil {
		if addErr := rm.AddSysOperation(sysopID, localSysOp); addErr != nil {
			logger.Error(logComponent).
				Str("event_type", "SYS_OPERATION_REGISTER_ERROR").
				Str("sysop_id", sysopID).
				Err(addErr).
				Msg("add_sys_operation 失败")
		} else {
			logger.Info(logComponent).
				Str("sysop_id", sysopID).
				Msg("SysOperation 已注册到资源管理器")
		}
	} else {
		logger.Warn(logComponent).Msg("全局资源管理器未初始化，跳过 SysOperation 注册")
	}

	return localSysOp, nil
}

// alreadyProvided 检查调用方是否已显式提供了指定类型的 Rail。
// 使用 reflect.TypeOf 精确类型匹配，不匹配子类。
//
// 对应 Python: _already_provided(rail_cls) — Python 使用 issubclass 支持子类匹配，
// Go 端当前使用精确匹配，后续需要可升级为接口断言。
func alreadyProvided(rails []agentinterfaces.AgentRail, target agentinterfaces.AgentRail) bool {
	targetType := reflect.TypeOf(target)
	if targetType == nil {
		return false
	}
	for _, r := range rails {
		if reflect.TypeOf(r) == targetType {
			return true
		}
	}
	return false
}

// collectDisabledSkillsFromState 从每个 skills_dir 读取 skills_state.json，
// 收集 enabled=false 的技能名称。结果按字母排序。
//
// 对应 Python: _collect_disabled_skills_from_state(skills_dirs)
func collectDisabledSkillsFromState(skillsDirs []string) []string {
	disabled := make(map[string]struct{})
	for _, dir := range skillsDirs {
		statePath := filepath.Join(dir, "skills_state.json")
		data, err := os.ReadFile(statePath)
		if err != nil {
			continue
		}
		var parsed map[string]any
		if err := json.Unmarshal(data, &parsed); err != nil {
			logger.Warn(logComponent).Str("path", statePath).Err(err).Msg("解析 skills_state.json 失败")
			continue
		}
		configs, ok := parsed["skill_configs"].(map[string]any)
		if !ok {
			continue
		}
		for name, cfg := range configs {
			cfgMap, ok := cfg.(map[string]any)
			if !ok {
				continue
			}
			if enabled, ok := cfgMap["enabled"].(bool); ok && !enabled {
				disabled[name] = struct{}{}
			}
		}
	}
	result := make([]string, 0, len(disabled))
	for name := range disabled {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

// addDefaultRails 自动添加调用方未显式提供的默认 Rail。
//
// 对齐 Python: factory.py L358-367 default_rails 自动添加
// ⤵️ 9.8-9.24 回填：SecurityRail/SkillUseRail/SubagentRail/TaskPlanningRail 具体实例化
func addDefaultRails(
	agent *DeepAgent,
	params hconfig.CreateDeepAgentParams,
	config *hschema.DeepAgentConfig,
	effectiveSubagents []hschema.SubagentSpec,
	ws *workspace.Workspace,
	resolvedLanguage string,
) {
	userProvidedTypes := make(map[reflect.Type]bool)
	for _, r := range params.Rails {
		rt := reflect.TypeOf(r)
		if rt != nil {
			userProvidedTypes[rt] = true
		}
	}

	// SecurityRail — 始终添加
	// ⤵️ 9.8-9.24 回填：SecurityRail 具体实例化
	if !alreadyProvidedByType(userProvidedTypes, nil) {
		agent.AddRail(agentinterfaces.NewBaseRail())
		logger.Debug(logComponent).Msg("已添加默认 SecurityRail 占位，⤵️ 9.8-9.24 回填")
	}

	// AskUserRail — 始终添加（拦截 ask_user 工具，实现 HITL 用户交互）
	if !alreadyProvidedByType(userProvidedTypes, reflect.TypeOf(&interrupt.AskUserRail{})) {
		agent.AddRail(interrupt.NewAskUserRail())
		logger.Debug(logComponent).Msg("已添加 AskUserRail")
	}

	// SysOperationRail — 始终添加（系统操作工具注册）
	if !alreadyProvidedByType(userProvidedTypes, reflect.TypeOf(&rails.SysOperationRail{})) {
		sysOpRail := rails.NewSysOperationRail()
		agent.AddRail(sysOpRail)
		logger.Debug(logComponent).Msg("已添加 SysOperationRail")
	}

	// TaskPlanningRail — 仅当 enable_task_planning=True 时添加
	if params.EnableTaskPlanning && !alreadyProvidedByType(userProvidedTypes, nil) {
		modelSelMap := make(map[*llm.Model]string, len(params.ModelSelection))
		for _, entry := range params.ModelSelection {
			if entry.Model != nil {
				modelSelMap[entry.Model] = entry.ModeName
			}
		}
		taskPlanningRail := rails.NewTaskPlanningRail(
			rails.WithModelSelection(modelSelMap),
			rails.WithLanguage(resolvedLanguage),
		)
		agent.AddRail(taskPlanningRail)
		logger.Debug(logComponent).
			Int("model_selection_count", len(modelSelMap)).
			Msg("已添加 TaskPlanningRail")
	}

	// SkillUseRail — 仅当有 skills 或启用了 skill_discovery 时添加
	if (len(params.Skills) > 0 || config.EnableSkillDiscovery) &&
		!alreadyProvidedByType(userProvidedTypes, nil) {
		// ⤵️ 9.8-9.24 回填：SkillUseRail 具体实例化（含 skills_dir + disabled_skills）
		agent.AddRail(agentinterfaces.NewBaseRail())
		// 收集被禁用的技能名称（逻辑保留，后续 SkillUseRail 使用）
		disabledSkills := collectDisabledSkillsFromState(params.Skills)
		logger.Debug(logComponent).
			Int("skills_count", len(params.Skills)).
			Int("disabled_count", len(disabledSkills)).
			Msg("已添加默认 SkillUseRail 占位，⤵️ 9.8-9.24 回填")
	}

	// SubagentRail — 仅当有 subagents 时添加
	if len(effectiveSubagents) > 0 && !alreadyProvidedByType(userProvidedTypes, nil) {
		// ⤵️ 9.8-9.24 回填：SubagentRail 具体实例化（含 enable_async_subagent）
		agent.AddRail(agentinterfaces.NewBaseRail())
		logger.Debug(logComponent).
			Bool("enable_async_subagent", params.EnableAsyncSubagent).
			Int("subagent_count", len(effectiveSubagents)).
			Msg("已添加默认 SubagentRail 占位，⤵️ 9.8-9.24 回填")
	}
}

// alreadyProvidedByType 检查用户提供的 Rail 类型映射中是否包含指定类型。
// 当 target 为具体 reflect.Type 时执行精确匹配；当 target 为 nil 时按字符串名称回退。
func alreadyProvidedByType(typeMap map[reflect.Type]bool, target reflect.Type) bool {
	if target != nil {
		return typeMap[target]
	}
	// 字符串名称回退：当前未实现
	return false
}

// buildCreateParamsFromSubagentKwargs 将 SubagentCreateParams 转换为 hconfig.CreateDeepAgentParams。
// 用于 CreateSubagent 的 default 工厂分支调用 CreateDeepAgent。
//
// 对齐 Python: DeepAgent.create_subagent() 中 create_deep_agent(**create_kwargs)
func buildCreateParamsFromSubagentKwargs(kwargs *hschema.SubagentCreateParams) hconfig.CreateDeepAgentParams {
	if kwargs == nil {
		return hconfig.CreateDeepAgentParams{}
	}
	return hconfig.CreateDeepAgentParams{
		Model:                  kwargs.Model,
		Card:                   kwargs.Card,
		SystemPrompt:           kwargs.SystemPrompt,
		ToolCards:              kwargs.Tools,         // ToolCard 列表，注册到 AbilityManager 提供 schema
		ToolInstances:          kwargs.ToolInstances, // Tool 实例列表，注册到 resource_mgr
		Mcps:                   kwargs.Mcps,
		Rails:                  kwargs.Rails,
		EnableTaskLoop:         kwargs.EnableTaskLoop,
		MaxIterations:          kwargs.MaxIterations,
		Workspace:              kwargs.Workspace,
		Skills:                 kwargs.Skills,
		Backend:                kwargs.Backend,
		SysOperation:           kwargs.SysOperation,
		Language:               kwargs.Language,
		PromptMode:             kwargs.PromptMode,
		EnableTaskPlanning:     kwargs.EnablePlanMode,
		RestrictToWorkDir:      kwargs.RestrictToWorkDir,
		EnableAsyncSubagent:    kwargs.EnableAsyncSubagent,
		AddGeneralPurposeAgent: kwargs.AddGeneralPurposeAgent,
	}
}
