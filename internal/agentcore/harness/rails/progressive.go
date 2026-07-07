package rails

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/tool_discovery"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ProgressiveToolRail 渐进式工具发现和可调用工具过滤 Rail。
//
// 在大工具集场景下，通过 search_tools/load_tools 元工具实现渐进式工具暴露：
//   - BeforeInvoke：缓存全量工具清单 + 初始化会话可见工具
//   - BeforeModelCall：注入导航节+规则节到系统提示词，并过滤 inputs.Tools 为仅可见工具
//
// 对齐 Python: ProgressiveToolRail (openjiuwen/harness/rails/progressive_tool_rail.py)
type ProgressiveToolRail struct {
	DeepAgentRail
	// config DeepAgent 运行时配置
	config *schema.DeepAgentConfig
	// defaultVisible 默认可见工具名称集合
	defaultVisible map[string]struct{}
	// alwaysVisible 始终可见工具名称集合
	alwaysVisible map[string]struct{}
	// maxLoadedTools 最大加载工具数
	maxLoadedTools int
	// metaToolNames 元工具名称集合（search_tools, load_tools）
	metaToolNames map[string]struct{}
	// ownedToolNames 本 Rail 注册到 ability_manager 的工具名称集合
	ownedToolNames map[string]struct{}
	// ownedToolIDs 本 Rail 注册到 resource_mgr 的工具 ID 集合
	ownedToolIDs map[string]struct{}
	// cachedAllTools 缓存的全量工具清单
	cachedAllTools []cschema.ToolInfoInterface
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// progressiveToolRailPriority ProgressiveToolRail 优先级
	progressiveToolRailPriority = 90

	// visibleToolsKey 会话状态键：可见工具名称列表
	visibleToolsKey = "__progressive_visible_tool_names__"
	// discoveryTraceKey 会话状态键：工具发现轨迹
	discoveryTraceKey = "__progressive_tool_discovery_trace__"

	// 搜索评分权重
	scoreExactMatch    = 100
	scoreNameContains  = 40
	scoreDescContains  = 25
	scoreHaystackMatch = 10
	scoreTokenMatch    = 3

	// 导航工具摘要最大长度
	navigationSummaryMaxLen = 160
)

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译时验证 ProgressiveToolRail 满足 AgentRail 接口
var _ agentinterfaces.AgentRail = (*ProgressiveToolRail)(nil)

// progressiveLogComponent 日志组件标识
var progressiveLogComponent = logger.ComponentAgentCore

// navigationBaselineTools 导航中始终包含的基础工具名集合
var navigationBaselineTools = map[string]struct{}{
	"code": {}, "read_file": {}, "bash": {}, "list_skill": {}, "pdf": {}, "xlsx": {},
}

// toolGroupOrder 工具分组排序权重
var toolGroupOrder = map[string]int{
	"skill":       0,
	"runtime":     1,
	"document":    2,
	"spreadsheet": 3,
	"general":     9,
}

// toolGroupCN 工具分组中文映射
var toolGroupCN = map[string]string{
	"skill":       "技能",
	"runtime":     "运行时",
	"document":    "文档",
	"spreadsheet": "表格",
	"general":     "通用",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewProgressiveToolRail 创建 ProgressiveToolRail 实例。
//
// 对齐 Python: ProgressiveToolRail.__init__(config)
func NewProgressiveToolRail(config *schema.DeepAgentConfig) *ProgressiveToolRail {
	r := &ProgressiveToolRail{
		DeepAgentRail:  *NewDeepAgentRail(),
		config:         config,
		defaultVisible: toSet(config.ProgressiveToolDefaultVisibleTools),
		alwaysVisible:  toSet(config.ProgressiveToolAlwaysVisibleTools),
		maxLoadedTools: config.EffectiveProgressiveToolMaxLoadedTools(),
		metaToolNames:  make(map[string]struct{}),
		ownedToolNames: make(map[string]struct{}),
		ownedToolIDs:   make(map[string]struct{}),
		cachedAllTools: nil,
	}
	r.WithPriority(progressiveToolRailPriority)
	return r
}

// Init 注册渐进式元工具到 resource_mgr 和 ability_manager。
//
// 对齐 Python: ProgressiveToolRail.init(agent)
func (r *ProgressiveToolRail) Init(agent agentinterfaces.BaseAgent) error {
	language := r.config.EffectiveLanguage()
	agentID := ""
	if card := agent.Card(); card != nil {
		agentID = card.ID
	}

	// 步骤 1：创建元工具
	searchTool := tool_discovery.NewSearchToolsTool(
		r.searchTools,
		r.appendTrace,
		language,
		agentID,
	)
	loadTool := tool_discovery.NewLoadToolsTool(
		r.loadTools,
		language,
		agentID,
	)
	tools := []tool.Tool{searchTool, loadTool}

	// 步骤 2：收集元工具名称
	r.metaToolNames = make(map[string]struct{})
	for _, t := range tools {
		r.metaToolNames[t.Card().Name] = struct{}{}
	}

	// 步骤 3：注册到 resource_mgr
	resourceMgr := runner.GetResourceMgr()
	for _, t := range tools {
		func(t tool.Tool) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Warn(progressiveLogComponent).
						Str("event_type", "progressive_rail_init").
						Str("tool_id", t.Card().ID).
						Str("tool_name", t.Card().Name).
						Msgf("注册工具到 resource_mgr 失败: %v", rec)
				}
			}()
			existing, err := resourceMgr.GetTool([]string{t.Card().ID})
			if err != nil || len(existing) == 0 {
				if addErr := resourceMgr.AddTool(t); addErr != nil {
					logger.Warn(progressiveLogComponent).
						Str("event_type", "progressive_rail_init").
						Str("tool_id", t.Card().ID).
						Err(addErr).
						Msg("注册工具到 resource_mgr 失败")
				} else {
					r.ownedToolIDs[t.Card().ID] = struct{}{}
				}
			}
		}(t)
	}

	// 步骤 4：注册到 ability_manager
	am := agent.AbilityManager()
	if am != nil {
		for _, t := range tools {
			func(t tool.Tool) {
				defer func() {
					if rec := recover(); rec != nil {
						logger.Warn(progressiveLogComponent).
							Str("event_type", "progressive_rail_init").
							Str("tool_name", t.Card().Name).
							Msgf("注册工具到 ability_manager 失败: %v", rec)
					}
				}()
				result := am.Add(t.Card())
				if result.Added {
					r.ownedToolNames[t.Card().Name] = struct{}{}
				}
			}(t)
		}
	}

	logger.Info(progressiveLogComponent).
		Str("event_type", "progressive_rail_init").
		Strs("meta_tool_names", setToSortedSlice(r.metaToolNames)).
		Msg("ProgressiveToolRail 初始化完成")

	return nil
}

// Uninit 移除本 Rail 注册的元工具。
//
// 对齐 Python: ProgressiveToolRail.uninit(agent)
func (r *ProgressiveToolRail) Uninit(agent agentinterfaces.BaseAgent) error {
	am := agent.AbilityManager()
	if am != nil {
		for name := range r.ownedToolNames {
			func(name string) {
				defer func() {
					if rec := recover(); rec != nil {
						logger.Warn(progressiveLogComponent).
							Str("event_type", "progressive_rail_uninit").
							Str("tool_name", name).
							Msgf("从 ability_manager 移除工具失败: %v", rec)
					}
				}()
				am.Remove(name)
			}(name)
		}
	}

	r.ownedToolNames = make(map[string]struct{})
	r.ownedToolIDs = make(map[string]struct{})
	r.metaToolNames = make(map[string]struct{})
	r.cachedAllTools = nil

	logger.Info(progressiveLogComponent).
		Str("event_type", "progressive_rail_uninit").
		Msg("ProgressiveToolRail 注销完成")

	return nil
}

// BeforeInvoke 缓存全量工具清单并初始化会话可见工具。
//
// 对齐 Python: ProgressiveToolRail.before_invoke(ctx)
func (r *ProgressiveToolRail) BeforeInvoke(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	// 步骤 1：缓存全量工具清单
	infos, err := r.listToolInfos(ctx, cbc.Agent())
	if err != nil {
		logger.Warn(progressiveLogComponent).
			Str("event_type", "progressive_before_invoke").
			Err(err).
			Msg("获取全量工具清单失败")
	}
	r.cachedAllTools = infos

	// 步骤 2：初始化会话可见工具
	sess := cbc.Session()
	r.initVisibleTools(sess, r.defaultVisible)

	logger.Info(progressiveLogComponent).
		Str("event_type", "progressive_before_invoke").
		Int("total_tools", len(infos)).
		Msg("ProgressiveToolRail BeforeInvoke 完成")

	return nil
}

// BeforeModelCall 注入导航+规则节到系统提示词，并过滤可调用工具。
//
// 对齐 Python: ProgressiveToolRail.before_model_call(ctx)
func (r *ProgressiveToolRail) BeforeModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	sess := cbc.Session()

	// 步骤 1：获取 SystemPromptBuilder 并注入导航+规则节
	builder := r.getPromptBuilder(cbc)

	navigationSection := r.buildNavigationSection(ctx, sess)
	rulesSection := r.buildProgressiveToolRulesSection()

	builder.AddSection(navigationSection)
	builder.AddSection(rulesSection)

	// 步骤 2：过滤 inputs.Tools
	inputs := cbc.Inputs()
	modelInputs, ok := inputs.(*agentinterfaces.ModelCallInputs)
	if !ok {
		logger.Info(progressiveLogComponent).
			Str("event_type", "progressive_before_model_call").
			Str("inputs_kind", inputs.EventKind()).
			Msg("inputs 不是 ModelCallInputs，跳过工具过滤")
		return nil
	}

	tools := modelInputs.Tools
	if tools == nil {
		logger.Info(progressiveLogComponent).
			Str("event_type", "progressive_before_model_call").
			Msg("inputs.Tools 为 nil，跳过过滤")
		return nil
	}

	// 步骤 3：计算可见工具集合
	sessionVisible := toSet(r.getVisibleTools(sess))
	baselineVisible := r.alwaysVisible
	metaVisible := r.metaToolNames

	// 步骤 4：过滤
	var filtered []cschema.ToolInfoInterface
	for _, t := range tools {
		name := t.GetName()
		if name == "" {
			continue
		}
		if _, ok := metaVisible[name]; ok {
			filtered = append(filtered, t)
			continue
		}
		if _, ok := baselineVisible[name]; ok {
			filtered = append(filtered, t)
			continue
		}
		if _, ok := sessionVisible[name]; ok {
			filtered = append(filtered, t)
			continue
		}
	}

	logger.Info(progressiveLogComponent).
		Str("event_type", "progressive_before_model_call").
		Int("before_count", len(tools)).
		Int("after_count", len(filtered)).
		Msg("ProgressiveToolRail BeforeModelCall 工具过滤完成")

	modelInputs.Tools = filtered
	return nil
}

// GetCallbacks 覆盖基类回调映射，增加 BeforeInvoke + BeforeModelCall。
//
// 对齐 Python: ProgressiveToolRail 隐式覆盖 before_invoke + before_model_call
func (r *ProgressiveToolRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	callbacks := r.DeepAgentRail.GetCallbacks()

	callbacks[agentinterfaces.CallbackBeforeInvoke] = func(ctx context.Context, railCtx any) error {
		return r.BeforeInvoke(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackBeforeModelCall] = func(ctx context.Context, railCtx any) error {
		return r.BeforeModelCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}

	return callbacks
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// searchTools 搜索缓存工具清单，按名称/描述/参数文本模糊匹配。
//
// 对齐 Python: ProgressiveToolRail._search_tools(query, limit, detail_level)
func (r *ProgressiveToolRail) searchTools(_ context.Context, query string, limit int, detailLevel int) ([]map[string]any, error) {
	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return nil, nil
	}

	allTools := r.getRealToolInfos()
	type scoredItem struct {
		score int
		tool  cschema.ToolInfoInterface
	}
	var scored []scoredItem

	for _, t := range allTools {
		name := t.GetName()
		description := t.GetDescription()
		parameters := t.GetParameters()

		haystack := strings.Join([]string{
			strings.ToLower(name),
			strings.ToLower(description),
			strings.ToLower(parametersToText(parameters)),
		}, " ")

		score := 0
		lowerName := strings.ToLower(name)
		lowerDesc := strings.ToLower(description)

		if query == lowerName {
			score += scoreExactMatch
		}
		if strings.Contains(lowerName, query) {
			score += scoreNameContains
		}
		if strings.Contains(lowerDesc, query) {
			score += scoreDescContains
		}
		if strings.Contains(haystack, query) {
			score += scoreHaystackMatch
		}

		for _, token := range strings.Fields(query) {
			if token != "" && strings.Contains(haystack, token) {
				score += scoreTokenMatch
			}
		}

		if score > 0 {
			scored = append(scored, scoredItem{score: score, tool: t})
		}
	}

	// 按 score 降序、name 升序排序
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].tool.GetName() < scored[j].tool.GetName()
	})

	effectiveLimit := limit
	if effectiveLimit < 1 {
		effectiveLimit = 1
	}
	if effectiveLimit > len(scored) {
		effectiveLimit = len(scored)
	}

	matched := make([]scoredItem, effectiveLimit)
	copy(matched, scored[:effectiveLimit])

	results := make([]map[string]any, len(matched))
	for i, item := range matched {
		results[i] = buildToolSummary(item.tool, detailLevel)
	}

	return results, nil
}

// loadTools 将指定工具标记为当前会话可调用。
//
// 对齐 Python: ProgressiveToolRail._load_tools(session, tool_names, replace)
func (r *ProgressiveToolRail) loadTools(_ context.Context, sess sessioninterfaces.SessionFacade, toolNames []string, replace bool) (map[string]any, error) {
	if sess == nil {
		return map[string]any{
			"loaded_tools":  []string{},
			"visible_tools": []string{},
			"skipped_tools": toolNames,
			"message":       "session is required for load_tools",
		}, nil
	}

	allTools := r.getRealToolInfos()
	availableNames := make(map[string]struct{}, len(allTools))
	for _, t := range allTools {
		availableNames[t.GetName()] = struct{}{}
	}

	// 步骤 1：校验请求的工具名称
	requested := make([]string, 0, len(toolNames))
	for _, name := range toolNames {
		trimmed := strings.TrimSpace(name)
		if trimmed != "" {
			requested = append(requested, trimmed)
		}
	}

	var validNames []string
	var skippedNames []string
	for _, name := range requested {
		if _, ok := r.alwaysVisible[name]; ok {
			validNames = append(validNames, name)
			continue
		}
		if _, ok := availableNames[name]; ok {
			validNames = append(validNames, name)
		} else {
			skippedNames = append(skippedNames, name)
		}
	}

	// 步骤 2：计算新的可见集合
	currentVisible := r.getVisibleTools(sess)

	var nextVisible []string
	if replace {
		nextVisible = dedupPreserveOrder(validNames)
	} else {
		nextVisible = dedupPreserveOrder(append(currentVisible, validNames...))
	}

	// 步骤 3：超限截断
	if len(nextVisible) > r.maxLoadedTools {
		overflow := nextVisible[r.maxLoadedTools:]
		skippedNames = append(skippedNames, overflow...)
		nextVisible = nextVisible[:r.maxLoadedTools]
	}

	r.setVisibleTools(sess, nextVisible)

	// 步骤 4：记录轨迹
	r.appendTrace(sess, map[string]any{
		"action":         "load_tools",
		"requested":      requested,
		"loaded":         validNames,
		"visible_before": currentVisible,
		"visible_after":  nextVisible,
		"skipped":        skippedNames,
		"replace":        replace,
	})

	// 步骤 5：返回结果
	visibleStr := "(none)"
	if len(nextVisible) > 0 {
		visibleStr = strings.Join(nextVisible, ", ")
	}
	return map[string]any{
		"loaded_tools":  validNames,
		"visible_tools": nextVisible,
		"skipped_tools": skippedNames,
		"message":       fmt.Sprintf("loaded %d tool(s), visible now: %s", len(validNames), visibleStr),
	}, nil
}

// listToolInfos 获取 Agent 上注册的所有工具信息。
//
// 对齐 Python: ProgressiveToolRail._list_tool_infos(agent)
func (r *ProgressiveToolRail) listToolInfos(ctx context.Context, agent agentinterfaces.BaseAgent) ([]cschema.ToolInfoInterface, error) {
	am := agent.AbilityManager()
	if am == nil {
		return nil, nil
	}
	infos, err := am.ListToolInfo(ctx, nil)
	if err != nil {
		logger.Warn(progressiveLogComponent).
			Str("event_type", "progressive_list_tool_infos").
			Err(err).
			Msg("获取工具信息列表失败")
		return nil, err
	}
	return infos, nil
}

// getRealToolInfos 返回缓存中非元工具的工具清单。
//
// 对齐 Python: ProgressiveToolRail._get_real_tool_infos()
func (r *ProgressiveToolRail) getRealToolInfos() []cschema.ToolInfoInterface {
	var result []cschema.ToolInfoInterface
	for _, t := range r.cachedAllTools {
		if _, ok := r.metaToolNames[t.GetName()]; !ok {
			result = append(result, t)
		}
	}
	return result
}

// buildNavigationSection 构建工具导航节。
//
// 对齐 Python: ProgressiveToolRail._build_navigation_section(session)
func (r *ProgressiveToolRail) buildNavigationSection(_ context.Context, sess sessioninterfaces.SessionFacade) saprompt.PromptSection {
	language := r.config.EffectiveLanguage()
	entriesCN := r.buildNavigationEntries(sess, "cn")
	entriesEN := r.buildNavigationEntries(sess, "en")

	// 构建中英双语导航节
	sectionCN := sections.BuildNavigationSection(entriesCN, "cn")
	sectionEN := sections.BuildNavigationSection(entriesEN, "en")

	// 合并为双语言 PromptSection
	merged := saprompt.PromptSection{
		Name:     sections.SectionToolNavigation,
		Priority: 70,
		Content:  make(map[string]string),
	}
	for k, v := range sectionCN.Content {
		merged.Content[k] = v
	}
	for k, v := range sectionEN.Content {
		merged.Content[k] = v
	}
	if language == "en" {
		if _, ok := merged.Content["en"]; !ok {
			merged.Content["en"] = ""
		}
	} else {
		if _, ok := merged.Content["cn"]; !ok {
			merged.Content["cn"] = ""
		}
	}

	return merged
}

// buildProgressiveToolRulesSection 构建渐进式工具规则节。
//
// 对齐 Python: ProgressiveToolRail._build_progressive_tool_rules_section()
func (r *ProgressiveToolRail) buildProgressiveToolRulesSection() saprompt.PromptSection {
	language := r.config.EffectiveLanguage()
	sectionCN := sections.BuildProgressiveToolRulesSection("cn")
	sectionEN := sections.BuildProgressiveToolRulesSection("en")

	merged := saprompt.PromptSection{
		Name:     sections.SectionProgressiveToolRules,
		Priority: 75,
		Content:  make(map[string]string),
	}
	for k, v := range sectionCN.Content {
		merged.Content[k] = v
	}
	for k, v := range sectionEN.Content {
		merged.Content[k] = v
	}
	if language == "en" {
		if _, ok := merged.Content["en"]; !ok {
			merged.Content["en"] = ""
		}
	} else {
		if _, ok := merged.Content["cn"]; !ok {
			merged.Content["cn"] = ""
		}
	}

	return merged
}

// buildNavigationEntries 构建导航条目列表。
//
// 对齐 Python: ProgressiveToolRail._build_navigation_entries(session, language)
func (r *ProgressiveToolRail) buildNavigationEntries(sess sessioninterfaces.SessionFacade, language string) []string {
	allTools := r.getRealToolInfos()
	loaded := toSet(r.getVisibleTools(sess))
	baseline := make(map[string]struct{})
	for k := range r.alwaysVisible {
		baseline[k] = struct{}{}
	}
	for k := range r.defaultVisible {
		baseline[k] = struct{}{}
	}

	var entries []string
	seen := make(map[string]struct{})

	// 按 group rank + name 排序
	sortedTools := make([]cschema.ToolInfoInterface, len(allTools))
	copy(sortedTools, allTools)
	sort.SliceStable(sortedTools, func(i, j int) bool {
		ri := toolGroupRank(sortedTools[i])
		rj := toolGroupRank(sortedTools[j])
		if ri != rj {
			return ri < rj
		}
		return sortedTools[i].GetName() < sortedTools[j].GetName()
	})

	for _, t := range sortedTools {
		name := t.GetName()
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		// 决定是否包含
		if _, ok := baseline[name]; !ok {
			if _, ok2 := loaded[name]; !ok2 {
				if _, ok3 := navigationBaselineTools[name]; !ok3 {
					continue
				}
			}
		}
		seen[name] = struct{}{}

		summary := toolSummaryForNavigation(t)
		group := toolGroupForNavigation(t)

		var status string
		var groupLabel string
		if language == "en" {
			if _, ok := loaded[name]; ok {
				status = "callable"
			} else if _, ok2 := r.alwaysVisible[name]; ok2 {
				status = "callable"
			} else {
				status = "navigation-only"
			}
			groupLabel = group
		} else {
			if _, ok := loaded[name]; ok {
				status = "可调用"
			} else if _, ok2 := r.alwaysVisible[name]; ok2 {
				status = "可调用"
			} else {
				status = "仅导航"
			}
			groupLabel = toolGroupToCN(group)
		}

		entries = append(entries, sections.BuildNavigationEntry(name, groupLabel, status, summary, language))
	}

	return entries
}

// getVisibleTools 读取当前会话可见工具名称列表。
//
// 对齐 Python: ProgressiveToolRail._get_visible_tools(session)
func (r *ProgressiveToolRail) getVisibleTools(sess sessioninterfaces.SessionFacade) []string {
	if sess == nil {
		return nil
	}
	val, err := sess.GetState(state.StringKey(visibleToolsKey))
	if err != nil {
		return nil
	}
	if val == nil {
		return nil
	}
	// 期望 []any 或 []string
	switch v := val.(type) {
	case []string:
		result := make([]string, 0, len(v))
		for _, s := range v {
			trimmed := strings.TrimSpace(s)
			if trimmed != "" {
				result = append(result, trimmed)
			}
		}
		return result
	case []any:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				trimmed := strings.TrimSpace(s)
				if trimmed != "" {
					result = append(result, trimmed)
				}
			}
		}
		return result
	default:
		return nil
	}
}

// setVisibleTools 持久化当前会话可见工具名称列表。
//
// 对齐 Python: ProgressiveToolRail._set_visible_tools(session, names)
func (r *ProgressiveToolRail) setVisibleTools(sess sessioninterfaces.SessionFacade, names []string) {
	if sess == nil {
		return
	}
	normalized := dedupPreserveOrder(names)
	sess.UpdateState(map[string]any{visibleToolsKey: normalized})
}

// initVisibleTools 初始化会话可见工具状态（仅执行一次）。
//
// 对齐 Python: ProgressiveToolRail._init_visible_tools(session, default_visible_tools)
func (r *ProgressiveToolRail) initVisibleTools(sess sessioninterfaces.SessionFacade, defaultVisible map[string]struct{}) {
	if sess == nil {
		return
	}

	// 已初始化则跳过
	val, err := sess.GetState(state.StringKey(visibleToolsKey))
	if err == nil && val != nil {
		switch v := val.(type) {
		case []string:
			if len(v) >= 0 {
				return
			}
		case []any:
			if v != nil {
				return
			}
		}
	}

	// 合并 always_visible + default_visible
	var initial []string
	for k := range r.alwaysVisible {
		initial = append(initial, k)
	}
	for k := range defaultVisible {
		initial = append(initial, k)
	}
	initial = dedupPreserveOrder(initial)

	sess.UpdateState(map[string]any{
		visibleToolsKey:   initial,
		discoveryTraceKey: []any{},
	})
}

// appendTrace 追加工具发现轨迹到会话状态。
//
// 对齐 Python: ProgressiveToolRail._append_trace(session, event)
func (r *ProgressiveToolRail) appendTrace(sess sessioninterfaces.SessionFacade, event map[string]any) {
	if sess == nil {
		return
	}
	val, err := sess.GetState(state.StringKey(discoveryTraceKey))
	var trace []any
	if err == nil && val != nil {
		if t, ok := val.([]any); ok {
			trace = t
		}
	}
	trace = append(trace, event)
	sess.UpdateState(map[string]any{discoveryTraceKey: trace})
}

// getPromptBuilder 获取 SystemPromptBuilder。
//
// 对齐 Python: ProgressiveToolRail._get_prompt_builder(ctx)
func (r *ProgressiveToolRail) getPromptBuilder(cbc *agentinterfaces.AgentCallbackContext) saprompt.SystemPromptBuilderInterface {
	agent := cbc.Agent()
	if agent == nil {
		logger.Error(progressiveLogComponent).
			Str("event_type", "progressive_get_prompt_builder").
			Msg("ProgressiveToolRail 要求 cbc.Agent() 不为 nil")
		panic("ProgressiveToolRail requires ctx.agent to exist")
	}
	builder := agent.SystemPromptBuilder()
	if builder == nil {
		logger.Error(progressiveLogComponent).
			Str("event_type", "progressive_get_prompt_builder").
			Msg("ProgressiveToolRail 要求 agent.SystemPromptBuilder() 不为 nil")
		panic("ProgressiveToolRail requires agent.SystemPromptBuilder() to exist")
	}
	return builder
}

// toSet 将字符串切片转为 set
func toSet(names []string) map[string]struct{} {
	s := make(map[string]struct{}, len(names))
	for _, name := range names {
		s[name] = struct{}{}
	}
	return s
}

// setToSortedSlice 将 set 转为排序后的切片（用于日志）
func setToSortedSlice(s map[string]struct{}) []string {
	result := make([]string, 0, len(s))
	for k := range s {
		result = append(result, k)
	}
	sort.Strings(result)
	return result
}

// dedupPreserveOrder 去重并保持顺序
func dedupPreserveOrder(names []string) []string {
	seen := make(map[string]struct{}, len(names))
	result := make([]string, 0, len(names))
	for _, name := range names {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

// toolSummaryForNavigation 返回工具的导航摘要行。
//
// 对齐 Python: ProgressiveToolRail._tool_summary_for_navigation(tool)
func toolSummaryForNavigation(t cschema.ToolInfoInterface) string {
	description := strings.TrimSpace(t.GetDescription())
	if description == "" {
		return "No summary available."
	}
	line := description
	if idx := strings.IndexByte(line, '\n'); idx >= 0 {
		line = line[:idx]
	}
	line = strings.TrimSpace(line)
	if len(line) > navigationSummaryMaxLen {
		line = line[:navigationSummaryMaxLen]
	}
	return line
}

// toolGroupForNavigation 推断工具的导航分组。
//
// 对齐 Python: ProgressiveToolRail._tool_group_for_navigation(tool)
func toolGroupForNavigation(t cschema.ToolInfoInterface) string {
	name := strings.ToLower(t.GetName())
	desc := strings.ToLower(t.GetDescription())

	if containsAny(name, "read", "write", "edit", "file", "bash", "code") {
		return "runtime"
	}
	if containsAny(name, "pdf", "invoice", "document") {
		return "document"
	}
	if containsAny(name, "xlsx", "excel", "sheet", "spreadsheet") {
		return "spreadsheet"
	}
	if strings.Contains(name, "skill") {
		return "skill"
	}
	if containsAny(desc, "pdf", "invoice", "document") {
		return "document"
	}
	if containsAny(desc, "xlsx", "excel", "spreadsheet") {
		return "spreadsheet"
	}
	return "general"
}

// toolGroupToCN 将分组标签翻译为中文。
//
// 对齐 Python: ProgressiveToolRail._tool_group_to_cn(group)
func toolGroupToCN(group string) string {
	if cn, ok := toolGroupCN[group]; ok {
		return cn
	}
	return "通用"
}

// toolGroupRank 返回工具分组的排序权重。
//
// 对齐 Python: ProgressiveToolRail._tool_group_rank(tool)
func toolGroupRank(t cschema.ToolInfoInterface) int {
	group := toolGroupForNavigation(t)
	if rank, ok := toolGroupOrder[group]; ok {
		return rank
	}
	return 99
}

// containsAny 检查字符串是否包含任一关键词
func containsAny(s string, keywords ...string) bool {
	for _, kw := range keywords {
		if strings.Contains(s, kw) {
			return true
		}
	}
	return false
}

// parametersToText 将参数摘要和原始 schema 拼接为可搜索文本。
//
// 对齐 Python: ProgressiveToolRail._parameters_to_text(parameters)
func parametersToText(parameters map[string]any) string {
	summary := parametersSummary(parameters)
	raw := safeSerializeParameters(parameters)
	return summary + " " + raw
}

// parametersSummary 构建参数的简短文本摘要。
//
// 对齐 Python: ProgressiveToolRail._parameters_summary(parameters)
func parametersSummary(parameters map[string]any) string {
	if parameters == nil {
		return "no parameters"
	}
	props, ok := parameters["properties"]
	if ok {
		if propsMap, ok2 := props.(map[string]any); ok2 && len(propsMap) > 0 {
			keys := make([]string, 0, len(propsMap))
			for k := range propsMap {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			return "fields: " + strings.Join(keys, ", ")
		}
	}
	if len(parameters) > 0 {
		keys := make([]string, 0, len(parameters))
		for k := range parameters {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return "schema keys: " + strings.Join(keys, ", ")
	}
	return "empty schema"
}

// safeSerializeParameters 安全地将参数 schema 序列化为字符串。
//
// 对齐 Python: ProgressiveToolRail._safe_serialize_parameters(parameters)
func safeSerializeParameters(parameters map[string]any) string {
	if parameters == nil {
		return ""
	}
	return fmt.Sprintf("%v", parameters)
}

// buildToolSummary 构建结构化工具摘要。
//
// 对齐 Python: ProgressiveToolRail._build_tool_summary(tool, detail_level)
func buildToolSummary(t cschema.ToolInfoInterface, detailLevel int) map[string]any {
	name := t.GetName()
	description := t.GetDescription()
	parameters := t.GetParameters()

	payload := map[string]any{
		"name":        name,
		"description": description,
	}

	if detailLevel >= 2 {
		payload["parameter_summary"] = parametersSummary(parameters)
	}

	if detailLevel >= 3 {
		payload["parameters"] = parameters
	}

	return payload
}
