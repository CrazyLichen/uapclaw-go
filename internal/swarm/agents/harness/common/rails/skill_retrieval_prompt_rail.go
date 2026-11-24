// Package rails 内 SkillRetrievalPromptRail——Symphony 托管运行时技能发现的
// 提示词边界：让 Symphony 成为唯一技能发现面，同时保留 SkillTool。
// Python: skill_retrieval_prompt_rail.py（582 行）。
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/rails/skill_retrieval_prompt_rail.py
package rails

import (
	"context"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/symphony/discovery"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	schema "github.com/uapclaw/uapclaw-go/internal/common/schema"
	cb "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/runner/callback"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/prompts"
	sections "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/prompts/sections"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails"
	toolkits "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// skillRetrievalRailPriority 优先级 = SkillUseRail(95) - 1：
	// 先隐藏原生节，再等 SkillUseRail 为本次模型调用刷新（Python priority）。
	skillRetrievalRailPriority = 94
	// skillRetrievalSectionName 注入节名
	skillRetrievalSectionName = "skill_retrieval"
	// skillRetrievalCandidateSectionName 候选附录节名
	skillRetrievalCandidateSectionName = "skill_retrieval.session_candidates"
	// skillRetrievalCandidateSectionPriority 附录保持在可复用的 system/tool
	// 前缀之后；内容在 init 期一次性冻结（Python CANDIDATE_SECTION_PRIORITY）
	skillRetrievalCandidateSectionPriority = 10_000
	// runtimeSkillAttachmentSection 运行期技能附件节
	runtimeSkillAttachmentSection = "skills.runtime_changes"
	// skillIndexToolName 工具名（Python _SKILL_INDEX_TOOL_NAME）
	skillIndexToolName = "skill_index"
)

// legacyListSkillToolNames legacy 列技能入口（Python _LEGACY_LIST_SKILL_TOOL_NAMES）
var legacyListSkillToolNames = map[string]bool{"list_skill": true, "list_skills": true}

// symphonyRuntimeToolNames Symphony 运行时工具白名单
// （Python _SYMPHONY_RUNTIME_TOOL_NAMES：skill_index + 编排兼容面）。
var symphonyRuntimeToolNames = map[string]bool{
	"skill_index":            true,
	"symphony_compose_score": true,
	"symphony_read_score":    true,
	"symphony_refresh_score": true,
	"symphony_compose_graph": true,
	"symphony_read_graph":    true,
	"symphony_refresh_graph": true,
}

// ──────────────────────────── 结构体 ────────────────────────────

// SkillRetrievalPromptRail 提示词边界 rail。
// Python: SkillRetrievalPromptRail(DeepAgentRail)。
type SkillRetrievalPromptRail struct {
	rails.DeepAgentRail

	sessionScope              string
	sourceToolkit             *toolkits.SkillRetrievalToolkit
	incrementalNoticeMaxChars int
	discoverySettings         discovery.DiscoverySettings
	promptSkillFS             *discovery.SkillFS
	configBase                map[string]any
	sessionEnabled            bool
	agent                     agentinterfaces.BaseAgent
	systemPromptBuilder       saprompt.SystemPromptBuilderInterface
	hiddenLegacyAbilities     map[string]schema.Ability
	hiddenSkillsSection       *saprompt.PromptSection
	frozenPromptSnapshot      *discovery.SkillPromptSnapshot
}

// 编译期验证 rail 满足 AgentRail 接口。
var _ agentinterfaces.AgentRail = (*SkillRetrievalPromptRail)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// RailOptions 构造选项（对齐 __init__ 关键字参数）。
type RailOptions struct {
	// Toolkit 来源工具包（可 nil）
	Toolkit *toolkits.SkillRetrievalToolkit
	// SessionScope 会话标识
	SessionScope string
	// IncrementalNoticeMaxChars 通知上限（nil 用 settings 值）
	IncrementalNoticeMaxChars *int
	// DiscoverySettings 显式设置（可 nil）
	DiscoverySettings *discovery.DiscoverySettings
	// Environment 显式环境（可 nil）
	Environment *discovery.SkillFS
	// ConfigBase 应用配置
	ConfigBase map[string]any
}

// NewSkillRetrievalPromptRail 构造（对齐 __init__ :70-113）。
func NewSkillRetrievalPromptRail(opts RailOptions) (*SkillRetrievalPromptRail, error) {
	settings := discovery.DiscoverySettings{}
	switch {
	case opts.DiscoverySettings != nil:
		settings = *opts.DiscoverySettings
	case opts.Toolkit != nil:
		settings = opts.Toolkit.Settings()
	default:
		settings = toolkits.BuildDiscoverySettings(opts.ConfigBase)
	}
	noticeMaxChars := settings.IncrementalNoticeMaxChars
	if opts.IncrementalNoticeMaxChars != nil {
		noticeMaxChars = *opts.IncrementalNoticeMaxChars
	}
	if noticeMaxChars <= 0 {
		return nil, ErrInvalidNoticeMaxChars
	}
	r := &SkillRetrievalPromptRail{
		sessionScope:              opts.SessionScope,
		sourceToolkit:             opts.Toolkit,
		incrementalNoticeMaxChars: noticeMaxChars,
		discoverySettings:         settings,
		promptSkillFS:             opts.Environment,
		configBase:                opts.ConfigBase,
		sessionEnabled:            toolkits.IsSkillRetrievalEnabled(opts.ConfigBase),
		hiddenLegacyAbilities:     map[string]schema.Ability{},
	}
	if r.sessionScope == "" {
		r.sessionScope = "default"
	}
	if r.promptSkillFS == nil && opts.Toolkit != nil {
		r.promptSkillFS = opts.Toolkit.Environment()
	}
	r.WithPriority(skillRetrievalRailPriority)
	return r, nil
}

// ErrInvalidNoticeMaxChars notice 上限非法。
var ErrInvalidNoticeMaxChars = errInvalidNoticeMaxChars{}

type errInvalidNoticeMaxChars struct{}

func (errInvalidNoticeMaxChars) Error() string {
	return "incremental_notice_max_chars must be positive"
}

// Init 注册期初始化：冻结快照 + notice 基线（对齐 init :115-140）。
func (r *SkillRetrievalPromptRail) Init(_ context.Context, agent agentinterfaces.BaseAgent) error {
	if agent != nil {
		r.agent = agent
		r.systemPromptBuilder = agent.SystemPromptBuilder()
	}
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				r.frozenPromptSnapshot = r.emptyPromptSnapshot()
				logger.Warn(logger.ComponentAgentServer).
					Msg("Unable to initialize Symphony Skill prompt/delta baseline")
			}
		}()
		toolkit := r.toolkit()
		var selectionCards map[string]map[string]string
		if toolkit != nil {
			selectionCards = toolkit.FrozenSelectionCards()
		} else {
			selectionCards = r.scanSelectionCards()
		}
		if toolkit != nil {
			snapshot := toolkit.FrozenPromptSnapshot()
			r.frozenPromptSnapshot = &snapshot
		} else if r.promptSkillFS != nil {
			snapshot := r.promptSkillFS.PromptSnapshot(true)
			r.frozenPromptSnapshot = &snapshot
		} else {
			r.frozenPromptSnapshot = r.emptyPromptSnapshot()
		}
		_ = discovery.InitializeIncrementalSkillNoticeState(r.sessionScope, selectionCards)
	}()
	return nil
}

// Uninit 卸载：恢复 legacy + 原生节并移除注入节（对齐 uninit :142-153）。
func (r *SkillRetrievalPromptRail) Uninit(agent agentinterfaces.BaseAgent) error {
	r.restoreLegacyListSkill(agent)
	r.restoreNativeSkillsSection()
	if r.systemPromptBuilder != nil {
		r.systemPromptBuilder.RemoveSection(skillRetrievalSectionName)
		r.systemPromptBuilder.RemoveSection(skillRetrievalCandidateSectionName)
	}
	r.systemPromptBuilder = nil
	r.promptSkillFS = nil
	r.sourceToolkit = nil
	r.frozenPromptSnapshot = nil
	r.agent = nil
	return nil
}

// BeforeInvoke 在 progressive 工具冻结目录前隐藏 legacy 入口
// （对齐 before_invoke :146-157）。
func (r *SkillRetrievalPromptRail) BeforeInvoke(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if r.agent == nil && cbc != nil {
		r.agent = cbc.Agent()
	}
	abilityManager := r.abilityManager()
	if r.sessionEnabled && abilityManager != nil &&
		abilityManager.Get(skillIndexToolName) != nil {
		r.hideLegacyListSkill()
	}
	return nil
}

// BeforeModelCall 模型工具列表就绪后同步提示词（对齐 before_model_call :160-163）。
func (r *SkillRetrievalPromptRail) BeforeModelCall(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	return r.syncPromptAttachment(cbc)
}

// AfterModelCall 空实现（对齐 after_model_call :289-291）。
func (r *SkillRetrievalPromptRail) AfterModelCall(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// OnModelException 空实现（对齐 on_model_exception :293-295）。
func (r *SkillRetrievalPromptRail) OnModelException(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// AfterToolCall 工具调用后消费漂移通知（对齐 after_tool_call :297-326）。
func (r *SkillRetrievalPromptRail) AfterToolCall(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if !r.sessionEnabled || cbc.Exception() != nil {
		return nil
	}
	inputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if !ok || inputs == nil {
		return nil
	}
	toolName := strings.TrimSpace(inputs.ToolName)
	if toolName == "" && inputs.ToolCall != nil {
		toolName = strings.TrimSpace(inputs.ToolCall.Name)
	}
	if !symphonyRuntimeToolNames[toolName] {
		return nil
	}
	// 会话自有命令工具已在结果内刷新 SkillFS 并消费共享 notice 游标，
	// 这里跳过避免二次扫描（Python :277-279 注释语义）。
	if toolName == skillIndexToolName {
		return nil
	}
	cards := r.scanSelectionCards()
	reminder, err := discovery.ConsumeIncrementalSkillReminder(
		r.sessionScope, cards, r.incrementalNoticeMaxChars)
	if err != nil {
		logger.Warn(logger.ComponentAgentServer).
			Msg("Unable to consume Symphony Skill delta")
		return nil
	}
	if reminder != "" {
		appendToolResultReminder(inputs, reminder)
	}
	return nil
}

// Toolkit 来源工具包访问器（Python rail._source_toolkit 的 Go 出口；
// V-6 skill 类绑定归属校验用）。
func (r *SkillRetrievalPromptRail) Toolkit() *toolkits.SkillRetrievalToolkit {
	return r.sourceToolkit
}

// Environment 环境访问器（Python rail._prompt_skillfs 的 Go 出口）。
func (r *SkillRetrievalPromptRail) Environment() *discovery.SkillFS {
	return r.promptSkillFS
}

// GetCallbacks 注册 BeforeInvoke + BeforeModelCall + AfterToolCall。
// 对齐 DeepAgentRail 基类回调映射扩展（SkillUseRail 同款）。
func (r *SkillRetrievalPromptRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	callbacks := r.DeepAgentRail.GetCallbacks()
	callbacks[agentinterfaces.CallbackBeforeInvoke] = func(ctx context.Context, railCtx any) error {
		return r.BeforeInvoke(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackBeforeModelCall] = func(ctx context.Context, railCtx any) error {
		return r.BeforeModelCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackAfterToolCall] = func(ctx context.Context, railCtx any) error {
		return r.AfterToolCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	return callbacks
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// syncPromptAttachment 把冻结清单保持在系统提示词尾部
// （对齐 _sync_prompt_attachment :165-219）。
func (r *SkillRetrievalPromptRail) syncPromptAttachment(cbc *agentinterfaces.AgentCallbackContext) error {
	if cbc != nil && cbc.Agent() != nil {
		r.agent = cbc.Agent()
		if r.systemPromptBuilder == nil {
			r.systemPromptBuilder = r.agent.SystemPromptBuilder()
		}
	}
	if !r.sessionEnabled || !r.hasSkillIndex(cbc) {
		r.disableAgenticPrompt(cbc)
		return nil
	}
	if r.systemPromptBuilder == nil {
		return nil
	}
	language := r.systemPromptBuilder.Language()
	if language == "" {
		language = "cn"
	}
	r.hideLegacyListSkill()
	filterLegacyListSkillFromModelInputs(cbc)
	r.hideNativeSkillsSection()
	snapshot := func() (snapshot discovery.SkillPromptSnapshot) {
		defer func() {
			if rec := recover(); rec != nil {
				logger.Warn(logger.ComponentAgentServer).
					Msg("Unable to build the Symphony Skill prompt snapshot")
				snapshot = *r.emptyPromptSnapshot()
			}
		}()
		return *r.promptSnapshot()
	}()
	candidateAppendix := buildCandidateAppendix(language, snapshot)
	r.systemPromptBuilder.RemoveSection(skillRetrievalSectionName)
	r.addPromptBuilderSection(language, candidateAppendix)
	return nil
}

// addPromptBuilderSection 注入候选附录节（对齐 _add_prompt_builder_section :221-233）。
func (r *SkillRetrievalPromptRail) addPromptBuilderSection(language, candidateAppendix string) {
	if r.systemPromptBuilder == nil {
		return
	}
	r.systemPromptBuilder.AddSection(saprompt.PromptSection{
		Name:     skillRetrievalCandidateSectionName,
		Content:  map[string]string{language: candidateAppendix},
		Priority: skillRetrievalCandidateSectionPriority,
	})
}

// disableAgenticPrompt 关闭路径：恢复 legacy 并移除注入节
// （对齐 _disable_agentic_prompt :253-262）。
func (r *SkillRetrievalPromptRail) disableAgenticPrompt(cbc *agentinterfaces.AgentCallbackContext) {
	if cbc != nil {
		r.restoreLegacyListSkill(cbc.Agent())
	} else {
		r.restoreLegacyListSkill(nil)
	}
	r.restoreNativeSkillsSection()
	if r.systemPromptBuilder != nil {
		r.systemPromptBuilder.RemoveSection(skillRetrievalSectionName)
		r.systemPromptBuilder.RemoveSection(skillRetrievalCandidateSectionName)
	}
}

// scanSelectionCards 现扫选择卡（对齐 _scan_selection_cards :328-332）。
func (r *SkillRetrievalPromptRail) scanSelectionCards() map[string]map[string]string {
	if r.promptSkillFS == nil {
		return map[string]map[string]string{}
	}
	return r.promptSkillFS.SelectionCards(true)
}

// appendToolResultReminder 就地追加 reminder 到工具结果
// （对齐 _append_tool_result_reminder :334-355：output→result 已知键优先
// → system_reminder 键兜底，防御异构结果形状）。
func appendToolResultReminder(inputs *agentinterfaces.ToolCallInputs, reminder string) {
	switch result := inputs.ToolResult.(type) {
	case map[string]any:
		updated := map[string]any{}
		for k, v := range result {
			updated[k] = v
		}
		for _, key := range []string{"output", "result"} {
			if value, ok := updated[key].(string); ok {
				if value != "" {
					updated[key] = value + "\n\n" + reminder
				} else {
					updated[key] = reminder
				}
				inputs.ToolResult = updated
				return
			}
		}
		updated["system_reminder"] = reminder
		inputs.ToolResult = updated
	case string:
		if result != "" {
			inputs.ToolResult = result + "\n\n" + reminder
		} else {
			inputs.ToolResult = reminder
		}
	}
}

// hideLegacyListSkill 移除 legacy 列技能能力（对齐 _hide_legacy_list_skill :357-379）。
func (r *SkillRetrievalPromptRail) hideLegacyListSkill() {
	abilityManager := r.abilityManager()
	if abilityManager == nil {
		return
	}
	for name := range legacyListSkillToolNames {
		if _, hidden := r.hiddenLegacyAbilities[name]; hidden {
			continue
		}
		card := abilityManager.Get(name)
		if card == nil {
			continue
		}
		removed := abilityManager.Remove(name)
		if removed != nil {
			r.hiddenLegacyAbilities[name] = removed
		}
	}
}

// restoreLegacyListSkill 恢复被隐藏的 legacy 能力（对齐 _restore_legacy_list_skill :381-410）。
func (r *SkillRetrievalPromptRail) restoreLegacyListSkill(agent agentinterfaces.BaseAgent) []schema.Ability {
	if agent != nil {
		r.agent = agent
	}
	abilityManager := r.abilityManager()
	if abilityManager == nil || len(r.hiddenLegacyAbilities) == 0 {
		return nil
	}
	var restored []schema.Ability
	for _, name := range sortedAbilityNames(r.hiddenLegacyAbilities) {
		card := r.hiddenLegacyAbilities[name]
		current := abilityManager.Get(name)
		if current == nil {
			abilityManager.Add(card)
			current = card
		}
		restored = append(restored, current)
		delete(r.hiddenLegacyAbilities, name)
	}
	return restored
}

// hideNativeSkillsSection 暂存并移除原生 SKILLS 节（对齐 _hide_native_skills_section :440-449）。
func (r *SkillRetrievalPromptRail) hideNativeSkillsSection() {
	if r.systemPromptBuilder == nil {
		return
	}
	if r.hiddenSkillsSection == nil {
		r.hiddenSkillsSection = r.systemPromptBuilder.GetSection(sections.SectionSkills)
	}
	r.systemPromptBuilder.RemoveSection(sections.SectionSkills)
}

// restoreNativeSkillsSection 恢复原生 SKILLS 节（对齐 _restore_native_skills_section :451-459）。
func (r *SkillRetrievalPromptRail) restoreNativeSkillsSection() {
	if r.systemPromptBuilder == nil || r.hiddenSkillsSection == nil {
		return
	}
	if !r.systemPromptBuilder.HasSection(sections.SectionSkills) {
		r.systemPromptBuilder.AddSection(*r.hiddenSkillsSection)
	}
	r.hiddenSkillsSection = nil
}

// promptSnapshot 冻结快照（对齐 _prompt_snapshot :461-473）。
func (r *SkillRetrievalPromptRail) promptSnapshot() *discovery.SkillPromptSnapshot {
	if r.frozenPromptSnapshot == nil {
		toolkit := r.toolkit()
		if toolkit != nil {
			snapshot := toolkit.FrozenPromptSnapshot()
			r.frozenPromptSnapshot = &snapshot
		} else if r.promptSkillFS != nil {
			snapshot := r.promptSkillFS.PromptSnapshot(true)
			r.frozenPromptSnapshot = &snapshot
		} else {
			r.frozenPromptSnapshot = r.emptyPromptSnapshot()
		}
	}
	return r.frozenPromptSnapshot
}

// toolkit 来源工具包（对齐 _toolkit :475-486）。
func (r *SkillRetrievalPromptRail) toolkit() *toolkits.SkillRetrievalToolkit {
	if r.promptSkillFS == nil {
		return nil
	}
	return r.sourceToolkit
}

// emptyPromptSnapshot 空快照兜底（对齐 _empty_prompt_snapshot :488-497）。
func (r *SkillRetrievalPromptRail) emptyPromptSnapshot() *discovery.SkillPromptSnapshot {
	return &discovery.SkillPromptSnapshot{
		Mode:                     "small",
		TotalCount:               0,
		EstimatedCandidateTokens: 0,
		CandidateBudgetTokens:    r.discoverySettings.CandidateBudgetTokens(),
		IndexState:               "missing",
	}
}

// abilityManager agent 能力管理器访问。
func (r *SkillRetrievalPromptRail) abilityManager() agentinterfaces.AbilityManagerInterface {
	if r.agent == nil {
		return nil
	}
	return r.agent.AbilityManager()
}

// hasSkillIndex 本次模型调用的工具列表是否含 skill_index（对齐 _has_skill_index :555-566）。
func (r *SkillRetrievalPromptRail) hasSkillIndex(cbc *agentinterfaces.AgentCallbackContext) bool {
	tools := modelInputsTools(cbc)
	if len(tools) == 0 {
		return false
	}
	for _, t := range tools {
		if modelToolName(t) == skillIndexToolName {
			return true
		}
	}
	return false
}

// buildCandidateAppendix 渲染候选附录（对齐 _build_candidate_appendix :499-553）。
func buildCandidateAppendix(language string, snapshot discovery.SkillPromptSnapshot) string {
	english := strings.HasPrefix(strings.ToLower(language), "en")
	complete := snapshot.AllCandidatesIncluded()
	var lines []string
	if english {
		lines = append(lines, "## Installed Skills")
		lines = append(lines, "",
			"Each Skill is an installed package with SKILL.md instructions and optional supporting files. "+
				"Browse or search as needed for the task. Use descriptions for initial selection; "+
				"read instructions when details are needed or before execution. "+
				"Stop discovery when you have enough information.")
	} else {
		lines = append(lines, "## 已安装 Skill")
		lines = append(lines, "",
			"每个 Skill 是已安装的技能包，包含 SKILL.md 使用说明及可选的辅助文件。"+
				"按任务需要自行导航或搜索，用描述初筛；需要细节或实际执行前再读说明，信息足够即可停止发现。")
	}

	if len(snapshot.Branches) > 0 {
		if english {
			lines = append(lines, "", "Categories (virtual groups):")
		} else {
			lines = append(lines, "", "分类（虚拟目录）：")
		}
		lines = append(lines, renderPromptBranches(snapshot.Branches)...)
		if snapshot.OmittedBranchCount > 0 {
			// 被裁分支数逐字渲染（Python f-string {snapshot.omitted_branch_count}）
			if english {
				lines = append(lines, "- … "+itoa(snapshot.OmittedBranchCount)+" more categories.")
			} else {
				lines = append(lines, "- … 另有 "+itoa(snapshot.OmittedBranchCount)+" 个分类。")
			}
		}
	}

	if len(snapshot.Entries) > 0 {
		if english {
			if complete {
				lines = append(lines, "", "Skills:")
			} else {
				lines = append(lines, "", "Examples:")
			}
		} else {
			if complete {
				lines = append(lines, "", "Skill：")
			} else {
				lines = append(lines, "", "示例：")
			}
		}
		lines = append(lines, renderPromptEntries(snapshot.Entries)...)
	} else if len(snapshot.Branches) == 0 {
		if english {
			if complete {
				lines = append(lines, "", "No available Skills.")
			} else {
				lines = append(lines, "", "No examples shown; use skill_index to find installed Skills.")
			}
		} else {
			if complete {
				lines = append(lines, "", "当前没有可用 Skill。")
			} else {
				lines = append(lines, "", "未展示示例；可使用 skill_index 检索已安装 Skill。")
			}
		}
	}
	return strings.Join(lines, "\n")
}

// renderPromptEntries 条目行渲染（对齐 _render_prompt_entries :565-577）。
func renderPromptEntries(entries []discovery.SkillPromptEntry) []string {
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, "- `"+compactPromptText(entry.WorkerID)+"`: "+
			compactPromptText(entry.Description))
	}
	return out
}

// renderPromptBranches 分支行渲染（对齐 _render_prompt_branches :579-586）。
func renderPromptBranches(branches []discovery.SkillPromptBranch) []string {
	out := make([]string, 0, len(branches))
	for _, branch := range branches {
		out = append(out, "- `"+compactPromptText(branch.Label)+"`: "+
			compactPromptText(compactBranchDescription(branch.Description, branch.Label)))
	}
	return out
}

// compactBranchDescription 分支描述压缩（对齐 _compact_branch_description :588-609）。
func compactBranchDescription(description, fallback string) string {
	primary := ""
	selectWhen := ""
	for _, paragraph := range strings.Split(description, "\n\n") {
		var semanticLines []string
		for _, rawLine := range strings.Split(paragraph, "\n") {
			line := strings.TrimSpace(rawLine)
			lowered := strings.ToLower(line)
			if strings.HasPrefix(lowered, "select when:") {
				if selectWhen == "" {
					selectWhen = line
				}
				continue
			}
			if strings.HasPrefix(lowered, "covers ") ||
				strings.HasPrefix(lowered, "representative ") ||
				strings.HasPrefix(lowered, "don't select when:") {
				continue
			}
			if line != "" {
				semanticLines = append(semanticLines, line)
			}
		}
		if len(semanticLines) > 0 && primary == "" {
			primary = strings.Join(semanticLines, " ")
		}
	}
	joined := strings.TrimSpace(primary + " " + selectWhen)
	if joined == "" {
		joined = fallback
	}
	return compactPromptText(joined)
}

// compactPromptText 反引号转义 + 空白折叠（对齐 _compact_prompt_text :611-613）。
func compactPromptText(value string) string {
	value = strings.ReplaceAll(value, "`", "\\`")
	return strings.Join(strings.Fields(value), " ")
}

// filterLegacyListSkillFromModelInputs 模型输入过滤 legacy 工具
// （对齐 _filter_legacy_list_skill_from_model_inputs :517-537）。
func filterLegacyListSkillFromModelInputs(cbc *agentinterfaces.AgentCallbackContext) {
	inputs := modelInputsOf(cbc)
	if inputs == nil || len(inputs.Tools) == 0 {
		return
	}
	filtered := make([]schema.ToolInfoInterface, 0, len(inputs.Tools))
	for _, t := range inputs.Tools {
		if name := modelToolName(t); !legacyListSkillToolNames[name] {
			filtered = append(filtered, t)
		}
	}
	if len(filtered) != len(inputs.Tools) {
		inputs.Tools = filtered
	}
}

// restoreLegacyInModelInputs 恢复的 legacy 卡片暴露进本次模型调用
// （对齐 _restore_legacy_list_skill_in_model_inputs :412-438）。
func (r *SkillRetrievalPromptRail) restoreLegacyInModelInputs(cbc *agentinterfaces.AgentCallbackContext, restoredCards []schema.Ability) {
	inputs := modelInputsOf(cbc)
	if len(restoredCards) == 0 || inputs == nil {
		return
	}
	restored := append([]schema.ToolInfoInterface{}, inputs.Tools...)
	originalCount := len(restored)
	visibleNames := map[string]bool{}
	for _, t := range restored {
		visibleNames[modelToolName(t)] = true
	}
	for _, card := range restoredCards {
		name := modelToolName(card)
		if visibleNames[name] {
			continue
		}
		provider, ok := card.(interface {
			ToolInfo() schema.ToolInfoInterface
		})
		if !ok {
			continue
		}
		restored = append(restored, provider.ToolInfo())
	}
	if len(restored) > originalCount {
		inputs.Tools = restored
	}
}

// modelInputsOf 取模型调用输入（类型守卫）。
func modelInputsOf(cbc *agentinterfaces.AgentCallbackContext) *agentinterfaces.ModelCallInputs {
	if cbc == nil {
		return nil
	}
	inputs, ok := cbc.Inputs().(*agentinterfaces.ModelCallInputs)
	if !ok {
		return nil
	}
	return inputs
}

// modelInputsTools 工具列表安全访问。
func modelInputsTools(cbc *agentinterfaces.AgentCallbackContext) []schema.ToolInfoInterface {
	inputs := modelInputsOf(cbc)
	if inputs == nil {
		return nil
	}
	return inputs.Tools
}

// modelToolName 工具名提取（对齐 _model_tool_name :539-553）。
func modelToolName(t any) string {
	switch v := t.(type) {
	case schema.ToolInfoInterface:
		return v.GetName()
	case map[string]any:
		if fn, ok := v["function"].(map[string]any); ok {
			name, _ := fn["name"].(string)
			return name
		}
		name, _ := v["name"].(string)
		return name
	}
	return ""
}

// sortedAbilityNames 稳定排序能力名（Go map 遍历无序，恢复顺序确定性）。
func sortedAbilityNames(m map[string]schema.Ability) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && names[j] < names[j-1]; j-- {
			names[j], names[j-1] = names[j-1], names[j]
		}
	}
	return names
}

// itoa 整数转字符串（避免引入 strconv 仅为此处）。
func itoa(v int) string {
	digits := "0123456789"
	if v == 0 {
		return "0"
	}
	negative := v < 0
	buf := []byte{}
	for v > 0 {
		buf = append([]byte{digits[v%10]}, buf...)
		v /= 10
	}
	if negative {
		return "-" + string(buf)
	}
	return string(buf)
}
