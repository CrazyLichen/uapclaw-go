package skills

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"
	"unicode"

	"gopkg.in/yaml.v3"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	tool "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hprompts "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	sections "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	codetool "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/code"
	filesystemtool "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/filesystem"
	shelltool "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/shell"
	skilltools "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/skills"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	skillpkg "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/skills"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SkillUseRail 技能使用护栏，管理 skill 提示词注入和工具注册。
// Python: SkillUseRail (openjiuwen/harness/rails/skills/skill_use_rail.py)
type SkillUseRail struct {
	rails.DeepAgentRail

	// ── 配置字段（构造时设置） ──
	// skillsDir 技能根目录（支持多个）
	skillsDir []string
	// skillMode 技能暴露模式："all" 或 "auto_list"
	skillMode string
	// listSkillModel list_skill 工具使用的 LLM 模型（可选）
	listSkillModel *llm.Model
	// enableCache 是否缓存已加载技能
	enableCache bool
	// includeTools 是否注册 read_file / code / bash 工具
	includeTools bool
	// enableImageMultimodal 是否启用图片多模态读取（includeTools=true 时使用）
	enableImageMultimodal *bool
	// enabledSkills 白名单
	enabledSkills map[string]struct{}
	// disabledSkills 黑名单
	disabledSkills map[string]struct{}
	// evolutionStore 可选演进存储
	evolutionStore *checkpointing.EvolutionStore
	// maxSkills 技能提示词最大技能数（0 表示不限）
	maxSkills int
	// maxTotalChars 技能提示词字符总预算（0 表示不限）
	maxTotalChars int
	// currentQuery 当前轮用户查询（用于技能预算的关键词排序）
	currentQuery string
	// lastSignature 上次技能目录签名（增量刷新判断）
	// Python: self._skills_snapshot_signature
	lastSignature []string
	// signatureSet 是否已生成过签名（对齐 Python 初值 None：首轮必刷新）
	signatureSet bool

	// ── 运行时状态 ──
	// skills 当前已加载技能列表
	skills []*skillpkg.Skill
	// systemPromptBuilder 系统提示词构建器引用（Init 中获取）
	systemPromptBuilder saprompt.SystemPromptBuilderInterface
	// attachmentManagerFor 会话级附件管理器获取函数（Init 中从 agent 取）
	// Python: self.attachment_manager（DeepAgent 共享给内层 ReActAgent 与各 rail）
	attachmentManagerFor func() *hprompts.Manager

	// ── 缓存 ──
	// skillCache 增量缓存 absPath → Skill
	skillCache map[string]*skillpkg.Skill
	// skillUpdateAt 增量缓存 absPath → mtime
	skillUpdateAt map[string]time.Time
	// skillOrder 有序 absPath 列表
	skillOrder []string
	// evolutionTexts 演化经验文本 skillName → text
	evolutionTexts map[string]string

	// ── 工具跟踪 ──
	// ownedToolNames 已注册到 AbilityManager 的工具名称
	ownedToolNames map[string]struct{}
	// ownedToolIDs 已注册到 ResourceMgr 的工具 ID
	ownedToolIDs map[string]struct{}
}

// skillBaselineState 会话技能基线持久化结构
// Python: _save_session_baseline 写入的 dict (:733-750)
type skillBaselineState struct {
	// SchemaVersion 结构版本，不匹配则整份忽略
	SchemaVersion int `json:"schema_version"`
	// BaselineSkills 首次快照时的技能列表
	BaselineSkills []skillBaselineEntry `json:"baseline_skills"`
}

// skillBaselineEntry 单条基线技能
type skillBaselineEntry struct {
	// Name 技能名
	Name string `json:"name"`
	// Description 技能描述
	Description string `json:"description"`
	// Directory 技能目录绝对路径（skill_tool 读文件必需）
	Directory string `json:"directory"`
}

// skillDirEntry 技能目录发现结果
// Python: _discover_skill_dirs 返回的 (skill_dir, skill_md_mtime) 对
type skillDirEntry struct {
	// Dir 技能目录路径
	Dir string
	// SkillMD 该目录下 SKILL.md 的修改时间
	SkillMD time.Time
}

// SkillUseRailOption SkillUseRail 构造选项
type SkillUseRailOption func(*SkillUseRail)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// SkillModeAll 将所有技能注入系统提示词
	SkillModeAll = "all"
	// SkillModeAutoList 添加 list_skill 工具让模型自主查看技能
	SkillModeAutoList = "auto_list"

	// skillUseRailPriority SkillUseRail 优先级
	// Python: SkillUseRail.priority = 95（必须晚于真正持有 read_file/code/bash 的 rail 初始化，
	// 否则 init 的 owner 检查没有意义）(skill_use_rail.py:57-61)
	skillUseRailPriority = 95

	// skillSessionStateKey 会话技能基线的 session state 键
	// Python: SkillUseRail._SESSION_STATE_KEY (skill_use_rail.py:65)
	skillSessionStateKey = "skill_use"
	// skillSessionStateSchemaVersion 基线结构版本
	// Python: SkillUseRail._SESSION_STATE_SCHEMA_VERSION (:66)
	skillSessionStateSchemaVersion = 1
	// runtimeSkillAttachmentSection 运行时技能变更附件节名
	// Python: SkillUseRail._RUNTIME_ATTACHMENT_SECTION (:67)
	runtimeSkillAttachmentSection = "skills.runtime_changes"
	// runtimeSkillAttachmentSource 附件来源标识
	runtimeSkillAttachmentSource = "skill_use_rail"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ValidSkillModes 有效技能模式集合
var ValidSkillModes = map[string]struct{}{
	SkillModeAll:      {},
	SkillModeAutoList: {},
}

// skillScanSkipDirs 技能库扫描跳过的目录名（分组目录仍会下钻，这些不）。
// Python: _SKILL_SCAN_SKIP_DIRS (skill_use_rail.py:40-48)
//
// 与 skills 工具侧 skillTreeSkipDirs 是两套独立常量——上游同样是两份
// （脚本注释："two sets are deliberately independent even though they currently agree"）。
var skillScanSkipDirs = map[string]struct{}{
	"output":       {},
	"temp":         {},
	"assets":       {},
	"node_modules": {},
}

// 编译时接口检查
// _ 编译时验证 SkillUseRail 满足 AgentRail 接口
var _ agentinterfaces.AgentRail = (*SkillUseRail)(nil)

// skillStopWords 关键词提取的停用词（与上游 _STOP_WORDS 一致）。
// Python: SkillUseRail._STOP_WORDS (L636-646)
var skillStopWords = map[string]bool{
	"the": true, "a": true, "an": true, "is": true, "are": true, "was": true,
	"were": true, "be": true, "been": true, "being": true, "to": true, "of": true,
	"and": true, "or": true, "in": true, "on": true, "at": true, "by": true,
	"for": true, "with": true, "as": true, "this": true, "that": true,
	"these": true, "those": true, "it": true, "its": true, "from": true,
	"have": true, "has": true, "had": true, "do": true, "does": true, "did": true,
	"will": true, "would": true, "could": true, "should": true, "may": true,
	"might": true, "can": true, "shall": true, "you": true, "your": true,
	"we": true, "our": true, "i": true, "my": true, "he": true, "she": true,
	"they": true, "them": true, "their": true, "what": true, "which": true,
	"who": true, "when": true, "where": true, "why": true, "how": true,
	"all": true, "each": true, "every": true, "both": true, "few": true,
	"more": true, "most": true, "other": true, "some": true, "such": true,
	"no": true, "nor": true, "not": true, "only": true, "own": true,
	"same": true, "so": true,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSkillUseRail 创建 SkillUseRail 实例。
// Python: SkillUseRail.__init__()
func NewSkillUseRail(skillsDir []string, opts ...SkillUseRailOption) *SkillUseRail {
	r := &SkillUseRail{
		DeepAgentRail:  *rails.NewDeepAgentRail(),
		skillsDir:      skillsDir,
		skillMode:      SkillModeAutoList,
		enableCache:    true,
		includeTools:   true,
		skillCache:     make(map[string]*skillpkg.Skill),
		skillUpdateAt:  make(map[string]time.Time),
		skillOrder:     make([]string, 0),
		evolutionTexts: make(map[string]string),
		ownedToolNames: make(map[string]struct{}),
		ownedToolIDs:   make(map[string]struct{}),
		enabledSkills:  make(map[string]struct{}),
		disabledSkills: make(map[string]struct{}),
	}
	r.WithPriority(skillUseRailPriority)

	for _, opt := range opts {
		opt(r)
	}

	// 校验 skillMode 有效性
	if _, ok := ValidSkillModes[r.skillMode]; !ok {
		panic(fmt.Sprintf(
			"不支持的 skill_mode: %s，应为 [all, auto_list] 之一",
			r.skillMode,
		))
	}

	return r
}

// WithSkillBudget 设置技能提示词预算。
// Python: SkillUseRail(max_skills=..., max_total_chars=...)
func WithSkillBudget(maxSkills, maxTotalChars int) SkillUseRailOption {
	return func(r *SkillUseRail) {
		r.maxSkills = maxSkills
		r.maxTotalChars = maxTotalChars
	}
}

// WithSkillMode 设置技能模式
func WithSkillMode(mode string) SkillUseRailOption {
	return func(r *SkillUseRail) { r.skillMode = mode }
}

// WithListSkillModel 设置 list_skill 使用的 LLM 模型
func WithListSkillModel(model *llm.Model) SkillUseRailOption {
	return func(r *SkillUseRail) { r.listSkillModel = model }
}

// WithEnableCache 设置是否缓存已加载技能
func WithEnableCache(enabled bool) SkillUseRailOption {
	return func(r *SkillUseRail) { r.enableCache = enabled }
}

// WithIncludeTools 设置是否注册 read_file / code / bash 工具
func WithIncludeTools(enabled bool) SkillUseRailOption {
	return func(r *SkillUseRail) { r.includeTools = enabled }
}

// WithEnabledSkills 设置白名单
func WithEnabledSkills(names []string) SkillUseRailOption {
	return func(r *SkillUseRail) { r.enabledSkills = normalizeNameSet(names) }
}

// WithDisabledSkills 设置黑名单
func WithDisabledSkills(names []string) SkillUseRailOption {
	return func(r *SkillUseRail) { r.disabledSkills = normalizeNameSet(names) }
}

// WithEvolutionStore 设置演进存储
func WithEvolutionStore(store *checkpointing.EvolutionStore) SkillUseRailOption {
	return func(r *SkillUseRail) { r.evolutionStore = store }
}

// WithEnableImageMultimodal 设置图片多模态读取
func WithEnableImageMultimodal(enabled bool) SkillUseRailOption {
	return func(r *SkillUseRail) { r.enableImageMultimodal = &enabled }
}

// SkillsMeta 返回当前技能列表深拷贝。
// Python: SkillUseRail.skills_meta property
func (r *SkillUseRail) SkillsMeta() []*skillpkg.Skill {
	result := make([]*skillpkg.Skill, len(r.skills))
	for i, s := range r.skills {
		cp := *s // 值拷贝 Skill 结构体
		result[i] = &cp
	}
	return result
}

// ReloadSkills 重新加载技能 + 演化经验。
// Python: SkillUseRail.reload_skills()
func (r *SkillUseRail) ReloadSkills(ctx context.Context) error {
	if err := r.prepareSkills(ctx); err != nil {
		return err
	}
	r.fetchEvolutionTexts(ctx)
	return nil
}

// SetSkillsDir 更新技能扫描根（热刷新用）。
// Python: refresh_skill_rails 直接赋值 self._skill_rail.skills_dir (interface_deep.py:11214-11216)
func (r *SkillUseRail) SetSkillsDir(dirs []string) {
	r.skillsDir = dirs
}

// SetDisabledSkills 更新禁用名单（热刷新用）。
// Python: refresh_skill_rails 赋值 disabled_skills (interface_deep.py:11217-11218)
func (r *SkillUseRail) SetDisabledSkills(names []string) {
	r.disabledSkills = normalizeNameSet(names)
}

// ClearSkills 清空缓存。
// Python: SkillUseRail.clear_skills()
func (r *SkillUseRail) ClearSkills() {
	r.skillCache = make(map[string]*skillpkg.Skill)
	r.skillUpdateAt = make(map[string]time.Time)
	r.skillOrder = make([]string, 0)
	r.skills = nil
}

// Priority 返回优先级。
// 覆盖 BaseRail.Priority() 以返回 skillUseRailPriority
func (r *SkillUseRail) Priority() int {
	return skillUseRailPriority
}

// GetSkillsForSession 返回会话可见技能视图：基线 ∪ 当前新发现（按 name 去重）。
// Python: get_skills_for_session (:673-687)
func (r *SkillUseRail) GetSkillsForSession(sess sessioninterfaces.SessionFacade) []*skillpkg.Skill {
	if _, ok := r.loadSessionState(sess); !ok {
		out := make([]*skillpkg.Skill, len(r.skills))
		copy(out, r.skills)
		return out
	}
	baseline := r.loadSessionBaseline(sess)
	known := make(map[string]struct{}, len(baseline))
	merged := make([]*skillpkg.Skill, 0, len(baseline)+len(r.skills))
	for _, skill := range baseline {
		known[skill.Name] = struct{}{}
		merged = append(merged, skill)
	}
	for _, skill := range r.skills {
		if skill == nil {
			continue
		}
		if _, ok := known[skill.Name]; ok {
			continue
		}
		merged = append(merged, skill)
	}
	return merged
}

// ClearSessionBaseline 清除会话基线，下次同步时按当前集合重拍。
// Python: JiuWenSwarmDeepAdapter._clear_skill_session_baseline (interface_deep.py:11261-11274)
func (r *SkillUseRail) ClearSessionBaseline(sess sessioninterfaces.SessionFacade) {
	if isNilSession(sess) {
		return
	}
	sess.UpdateState(map[string]any{skillSessionStateKey: nil})
}

// Init 注册工具到 ResourceMgr + AbilityManager。
// Python: SkillUseRail.init() L237-306
func (r *SkillUseRail) Init(_ context.Context, agent agentinterfaces.BaseAgent) error {
	r.systemPromptBuilder = agent.SystemPromptBuilder()
	// Python: self.attachment_manager = getattr(agent, "prompt_attachment_manager", None)
	if provider, ok := agent.(interface{ PromptAttachments() *hprompts.Manager }); ok {
		r.attachmentManagerFor = provider.PromptAttachments
	}

	var language string
	if r.systemPromptBuilder != nil {
		language = r.systemPromptBuilder.Language()
	} else {
		language = "cn"
	}

	var agentID string
	if card := agent.Card(); card != nil {
		agentID = card.ID
	}

	op := r.SysOperation()

	// Python: enable_read_image_multimodal = build_read_image_multimodal_resolver(agent)
	enableImageMultimodal := true
	if r.enableImageMultimodal != nil {
		enableImageMultimodal = *r.enableImageMultimodal
	}

	var tools []tool.Tool

	// Python: L246-253: 始终注册 SkillTool
	tools = append(tools, skilltools.NewSkillTool(
		op,
		func(sess sessioninterfaces.SessionFacade) []*skillpkg.Skill { return r.GetSkillsForSession(sess) },
		language,
		agentID,
		enableImageMultimodal,
	))

	// Python: L255-271: includeTools 时注册 ReadFileTool/CodeTool/BashTool
	if r.includeTools {
		tools = append(tools,
			filesystemtool.NewReadFileTool(op, language, agentID, enableImageMultimodal),
			codetool.NewCodeTool(op, language, agentID),
			shelltool.NewBashTool(op, language, agentID, shelltool.NewPermissionConfig(shelltool.PermissionModeAuto, nil, nil)),
		)
	}

	// Python: L273-281: auto_list 模式注册 ListSkillTool
	if r.skillMode == SkillModeAutoList {
		tools = append(tools, skilltools.NewListSkillTool(
			func(sess sessioninterfaces.SessionFacade) []*skillpkg.Skill { return r.GetSkillsForSession(sess) },
			r.listSkillModel,
			language,
			agentID,
		))
	}

	// Python: L296-306 单入口注册（add_ability），已由其他 rail 拥有的能力不抢注册——
	// 对方的实例携带本 rail 无法复现的策略（只读模式、bash deny 规则、多模态覆盖）
	am := agent.AbilityManager()
	resourceMgr := runner.GetResourceMgr()
	for _, t := range tools {
		name := t.Card().Name
		if am != nil && am.Get(name) != nil {
			logger.Debug(logger.ComponentAgentCore).
				Str("tool_name", name).
				Msg("ability already owned by another rail, skip fallback registration")
			continue
		}
		toolID := t.Card().ID
		if resourceMgr != nil && toolID != "" {
			if err := resourceMgr.AddTool(t); err != nil {
				// 同 ID 可能已被占用（同 agent 重复 Init / 全局注册表残留）：
				// 记录告警但不阻断能力注册，否则工具对模型不可见。
				logger.Warn(logger.ComponentAgentCore).
					Str("tool_id", toolID).
					Err(err).
					Msg("failed to add tool resource to resource_mgr")
			} else {
				r.ownedToolIDs[toolID] = struct{}{}
			}
		}
		if am != nil {
			if result := am.Add(t.Card()); result.Added {
				r.ownedToolNames[name] = struct{}{}
			} else {
				logger.Warn(logger.ComponentAgentCore).
					Str("tool_name", name).
					Msg("failed to add tool card to ability_manager")
			}
		}
	}

	logger.Info(logger.ComponentAgentCore).
		Str("skill_mode", r.skillMode).
		Bool("include_tools", r.includeTools).
		Int("tool_count", len(tools)).
		Msg("SkillUseRail init success")

	return nil
}

// Uninit 从 AbilityManager 注销工具。
// Python: SkillUseRail.uninit() L308-321
func (r *SkillUseRail) Uninit(agent agentinterfaces.BaseAgent) error {
	am := agent.AbilityManager()
	if am != nil {
		for toolName := range r.ownedToolNames {
			removed := am.Remove(toolName)
			if removed == nil {
				logger.Warn(logger.ComponentAgentCore).
					Str("tool_name", toolName).
					Msg("failed to unregister tool from AbilityManager, tool not found")
			}
		}
	}

	r.ownedToolNames = make(map[string]struct{})
	r.ownedToolIDs = make(map[string]struct{})

	return nil
}

// BeforeInvoke 刷新技能并落盘基线。
// Python: SkillUseRail.before_invoke (:437-442)
func (r *SkillUseRail) BeforeInvoke(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	r.refreshSkillPrompt(ctx)
	var sess sessioninterfaces.SessionFacade
	if cbc != nil {
		sess = cbc.Session()
	}
	// Python: _ensure_session_baseline 无返回值（写 state 不会失败）
	r.ensureSessionBaseline(sess)
	return r.syncSkillPromptAndAttachment(ctx, cbc)
}

// BeforeModelCall 用会话基线渲染稳定 skills 节并刷新变更附件。
// Python: SkillUseRail.before_model_call (:475-485)
func (r *SkillUseRail) BeforeModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	// 记录当前查询：技能预算按与查询的关键词重叠度排序。
	// 上游从**本次模型调用的 messages** 里倒序找最后一条 UserMessage（follow-up /
	// steering / 附件注入后 messages 尾部才是真实查询，inputs.Query 可能是旧值）。
	// Python: SkillUseRail._extract_query_from_messages() L657-669
	if cbc != nil {
		if inputs, ok := cbc.Inputs().(*agentinterfaces.ModelCallInputs); ok && inputs != nil {
			r.currentQuery = extractQueryFromMessages(inputs.Messages)
		}
	}
	if r.systemPromptBuilder == nil {
		return nil
	}
	return r.syncSkillPromptAndAttachment(ctx, cbc)
}

// AfterInvoke 空操作。
// Python: SkillUseRail.after_invoke()
func (r *SkillUseRail) AfterInvoke(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// GetCallbacks 覆盖基类回调映射，增加 BeforeInvoke + BeforeModelCall。
// Python: SkillUseRail 隐式覆盖 before_invoke/before_model_call
func (r *SkillUseRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	callbacks := r.DeepAgentRail.GetCallbacks()

	callbacks[agentinterfaces.CallbackBeforeInvoke] = func(ctx context.Context, railCtx any) error {
		return r.BeforeInvoke(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackBeforeModelCall] = func(ctx context.Context, railCtx any) error {
		return r.BeforeModelCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}

	return callbacks
}

// LoadSkillsFromDir 类方法：静态加载技能。
// Python: SkillUseRail.load_skills_from_dir()
func LoadSkillsFromDir(ctx context.Context, skillsDir []string) ([]*skillpkg.Skill, error) {
	roots := normalizeSkillDirs(skillsDir)
	if len(roots) == 0 {
		return nil, errors.New("skills_dir is empty")
	}

	skillMap := make(map[string]*skillpkg.Skill)
	loader := NewSkillUseRail(skillsDir, WithSkillMode(SkillModeAll), WithIncludeTools(false))

	// 逐个 skills 根目录扫描，不存在的目录跳过
	for _, root := range roots {
		if _, err := os.Stat(root); os.IsNotExist(err) {
			logger.Debug(logger.ComponentAgentCore).
				Str("skills_dir", root).
				Msg("skills_dir does not exist, skipping")
			continue
		}
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			logger.Debug(logger.ComponentAgentCore).
				Str("skills_dir", root).
				Msg("skills_dir is not a directory, skipping")
			continue
		}

		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].Name() < entries[j].Name()
		})

		// 根目录下每个子目录视为一个技能，必须含 SKILL.md
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			skillMDPath := filepath.Join(root, entry.Name(), "SKILL.md")
			if _, err := os.Stat(skillMDPath); os.IsNotExist(err) {
				continue
			}
			fileInfo, err := os.Stat(skillMDPath)
			if err != nil {
				continue
			}
			skill, err := loader.loadSkill(ctx, filepath.Join(root, entry.Name()), fileInfo.ModTime())
			if err != nil {
				continue
			}

			// 同名技能保留先加载的，后者只告警跳过
			if _, exists := skillMap[skill.Name]; exists {
				prevDir := skillMap[skill.Name].Directory
				logger.Warn(logger.ComponentAgentCore).
					Str("skill_name", skill.Name).
					Str("keep_dir", prevDir).
					Str("skip_dir", skill.Directory).
					Msg("duplicate skill name detected")
				continue
			}
			skillMap[skill.Name] = skill
		}
	}

	// 汇总结果：去重用的 map 转成切片返回
	result := make([]*skillpkg.Skill, 0, len(skillMap))
	for _, skill := range skillMap {
		result = append(result, skill)
	}
	return result, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// refreshSkillPrompt 重新加载技能 + 演化经验。
// Python: SkillUseRail.refresh_skill_prompt()
func (r *SkillUseRail) refreshSkillPrompt(ctx context.Context) {
	_ = r.prepareSkills(ctx)
	r.fetchEvolutionTexts(ctx)
	// Python: refresh_skill_prompt 末尾重算签名 (:430-435)
	r.lastSignature = r.buildSkillsSnapshotSignature()
	r.signatureSet = true
}

// syncSkillPromptAndAttachment 刷新稳定 skills 节与变更附件。
// Python: _sync_skill_prompt_and_attachment (:486-508)
func (r *SkillUseRail) syncSkillPromptAndAttachment(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if r.systemPromptBuilder == nil {
		return nil
	}
	var sess sessioninterfaces.SessionFacade
	if cbc != nil {
		sess = cbc.Session()
	}
	r.refreshSkillPromptIfChanged(ctx)
	// 进化记录可能在不改 SKILL.md 的情况下变化，需与技能快照解耦刷新。
	// 部分 task_loop 路径不经 BeforeInvoke，故此处也要保证基线存在。
	r.ensureSessionBaseline(sess)
	var baseline []*skillpkg.Skill
	if _, ok := r.loadSessionState(sess); ok {
		baseline = r.loadSessionBaseline(sess)
	} else {
		baseline = r.skills
	}
	r.fetchEvolutionTextsFor(ctx, append(append([]*skillpkg.Skill{}, baseline...), r.skills...))

	section := r.buildSkillsSectionFor(baseline)
	if section == nil {
		r.systemPromptBuilder.RemoveSection(sections.SectionSkills)
	} else {
		r.systemPromptBuilder.AddSection(*section)
	}
	return r.updateRuntimeSkillAttachment(cbc, baseline)
}

// refreshSkillPromptIfChanged 目录或 mtime 签名变化时才重建技能集合。
// Python: _refresh_skill_prompt_if_changed (:510-516)
func (r *SkillUseRail) refreshSkillPromptIfChanged(ctx context.Context) {
	if r.skillsSignatureEquals() {
		return
	}
	r.refreshSkillPrompt(ctx)
}

// skillsSignatureEquals 比较当前磁盘签名与上次快照。
// Python: _build_skills_snapshot_signature (:518-524)
func (r *SkillUseRail) skillsSignatureEquals() bool {
	if !r.signatureSet {
		// Python: 初值 None，任何签名都不等于 None → 首轮必刷新
		return false
	}
	sig := r.buildSkillsSnapshotSignature()
	if len(sig) != len(r.lastSignature) {
		return false
	}
	for i := range sig {
		if sig[i] != r.lastSignature[i] {
			return false
		}
	}
	return true
}

// buildSkillsSnapshotSignature 生成 (绝对路径, mtime) 序列签名。
// Python: _build_skills_snapshot_signature (:518-524)
func (r *SkillUseRail) buildSkillsSnapshotSignature() []string {
	roots := r.normalizeSkillDirs()
	discovered := discoverSkillDirs(roots)
	sig := make([]string, 0, len(discovered))
	for _, entry := range discovered {
		// 签名取 SKILL.md 的 mtime：目录 mtime 不随文件内容变化，
		// 演进写回只改 SKILL.md 时目录签名会漏检。
		// Python: _build_skills_snapshot_signature (:518-524) 用的是 _discover_skill_dirs 的 SKILL.md mtime
		sig = append(sig, entry.Dir+"@"+entry.SkillMD.UTC().Format(time.RFC3339Nano))
	}
	sort.Strings(sig)
	return sig
}

// discoverSkillDirs 递归发现技能目录，返回目录与 SKILL.md mtime。
// 规则与上游一致：一个分支上遇到第一个 SKILL.md 即认定该目录是技能并停止下钻
// （其内部子技能由 skill_tool 的 discovered_skill_names 披露，不注册为顶层技能）；
// 跳过 "." 开头的目录与 _SKILL_SCAN_SKIP_DIRS；用 visited 防止符号链接成环。
// Python: SkillUseRail._discover_skill_dirs (:1043-1125)
func discoverSkillDirs(roots []string) []skillDirEntry {
	found := make([]skillDirEntry, 0, len(roots))
	visited := make(map[string]struct{})

	var walk func(dir string)
	walk = func(dir string) {
		key := dir
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			key = resolved
		}
		if _, ok := visited[key]; ok {
			return
		}
		visited[key] = struct{}{}

		// 显式根自身也可能就是一个技能目录
		skillMDPath := filepath.Join(dir, "SKILL.md")
		if info, err := os.Stat(skillMDPath); err == nil && !info.IsDir() {
			found = append(found, skillDirEntry{Dir: dir, SkillMD: info.ModTime()})
			return
		}

		entries, err := os.ReadDir(dir)
		if err != nil {
			logger.Debug(logger.ComponentAgentCore).Str("dir", dir).Err(err).Msg("cannot list skill directory")
			return
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			name := entry.Name()
			if strings.HasPrefix(name, ".") {
				continue
			}
			if _, skip := skillScanSkipDirs[name]; skip {
				continue
			}
			walk(filepath.Join(dir, name))
		}
	}

	for _, root := range roots {
		info, err := os.Stat(root)
		if err != nil {
			logger.Debug(logger.ComponentAgentCore).Str("skills_dir", root).Msg("skills_dir does not exist, skipping")
			continue
		}
		if !info.IsDir() {
			logger.Debug(logger.ComponentAgentCore).Str("skills_dir", root).Msg("skills_dir is not a directory, skipping")
			continue
		}
		walk(root)
	}
	return found
}

// fetchEvolutionTextsFor 按给定技能列表抓取演化经验文本（同名去重）。
// Python: _fetch_evolution_texts (:444-462)
func (r *SkillUseRail) fetchEvolutionTextsFor(ctx context.Context, skills []*skillpkg.Skill) {
	if r.evolutionStore == nil {
		return
	}
	seen := map[string]struct{}{}
	for _, skill := range skills {
		if skill == nil {
			continue
		}
		if _, ok := seen[skill.Name]; ok {
			continue
		}
		seen[skill.Name] = struct{}{}
		text := r.evolutionStore.FormatDescExperienceText(ctx, skill.Name, 5)
		r.evolutionTexts[skill.Name] = text
	}
}

// ensureSessionBaseline 首次使用时落盘会话基线。
// Python: _ensure_session_baseline (:692-698)
func (r *SkillUseRail) ensureSessionBaseline(sess sessioninterfaces.SessionFacade) {
	if isNilSession(sess) {
		return
	}
	if _, ok := r.loadSessionState(sess); ok {
		return
	}
	r.saveSessionBaseline(sess, r.skills)
}

// loadSessionBaseline 读取会话基线；无有效 state 时返回空切片。
// Python: _load_session_baseline (:700-721)
func (r *SkillUseRail) loadSessionBaseline(sess sessioninterfaces.SessionFacade) []*skillpkg.Skill {
	stateMap, ok := r.loadSessionState(sess)
	if !ok {
		return nil
	}
	raw, exists := stateMap["baseline_skills"]
	if !exists {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		logger.Warn(logger.ComponentAgentCore).Msg("invalid persisted baseline_skills, ignoring")
		return nil
	}
	skills := make([]*skillpkg.Skill, 0, len(items))
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := stringField(entry, "name")
		directory := stringField(entry, "directory")
		if name == "" || directory == "" {
			continue
		}
		skills = append(skills, &skillpkg.Skill{
			Name:        name,
			Description: stringField(entry, "description"),
			Directory:   directory,
		})
	}
	return skills
}

// loadSessionState 读取并校验 skill_use state。
// Python: _load_session_state (:722-731)
func (r *SkillUseRail) loadSessionState(sess sessioninterfaces.SessionFacade) (map[string]any, bool) {
	if isNilSession(sess) {
		return nil, false
	}
	raw, err := sess.GetState(state.StringKey(skillSessionStateKey))
	if err != nil || raw == nil {
		return nil, false
	}
	stateMap, ok := raw.(map[string]any)
	if !ok {
		return nil, false
	}
	if schemaVersion(stateMap["schema_version"]) != skillSessionStateSchemaVersion {
		logger.Warn(logger.ComponentAgentCore).
			Any("schema_version", stateMap["schema_version"]).
			Msg("unsupported persisted skill state schema, ignoring")
		return nil, false
	}
	return stateMap, true
}

// saveSessionBaseline 把技能列表写入会话基线。
// Python: _save_session_baseline (:733-750)
func (r *SkillUseRail) saveSessionBaseline(sess sessioninterfaces.SessionFacade, skills []*skillpkg.Skill) {
	if isNilSession(sess) {
		return
	}
	entries := make([]any, 0, len(skills))
	for _, skill := range skills {
		if skill == nil {
			continue
		}
		entries = append(entries, map[string]any{
			"name":        skill.Name,
			"description": skill.Description,
			"directory":   skill.Directory,
		})
	}
	sess.UpdateState(map[string]any{
		skillSessionStateKey: map[string]any{
			"schema_version":  skillSessionStateSchemaVersion,
			"baseline_skills": entries,
		},
	})
}

// isNilSession 判断会话是否为空（含 typed-nil：接口内装 nil 指针时 != nil 仍成立）。
func isNilSession(sess sessioninterfaces.SessionFacade) bool {
	if sess == nil {
		return true
	}
	v := reflect.ValueOf(sess)
	// SessionFacade 的实现都是指针类型，只有指针需要判 nil
	return v.Kind() == reflect.Pointer && v.IsNil()
}

// stringField 读取 map 的字符串字段，缺失或非字符串返回空串。
func stringField(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// schemaVersion 归一化版本号：落盘回读可能是 int 或 float64。
func schemaVersion(v any) int {
	switch typed := v.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	}
	return -1
}

// updateRuntimeSkillAttachment 把技能新增/移除/进化经验写入变更附件。
// Python: _update_runtime_skill_attachment (:752-781)
func (r *SkillUseRail) updateRuntimeSkillAttachment(cbc *agentinterfaces.AgentCallbackContext, baseline []*skillpkg.Skill) error {
	if r.attachmentManagerFor == nil || cbc == nil {
		return nil
	}
	mgr := r.attachmentManagerFor()
	if mgr == nil || cbc.Session() == nil {
		return nil
	}
	sessionID := cbc.Session().GetSessionID()
	if sessionID == "" {
		return nil
	}

	baselineByName := make(map[string]struct{}, len(baseline))
	for _, skill := range baseline {
		baselineByName[skill.Name] = struct{}{}
	}
	currentByName := make(map[string]struct{}, len(r.skills))
	var additions []*skillpkg.Skill
	for _, skill := range r.skills {
		if skill == nil {
			continue
		}
		currentByName[skill.Name] = struct{}{}
		if _, ok := baselineByName[skill.Name]; !ok {
			additions = append(additions, skill)
		}
	}
	var removals []*skillpkg.Skill
	for _, skill := range baseline {
		if _, ok := currentByName[skill.Name]; !ok {
			removals = append(removals, skill)
		}
	}

	content := r.buildRuntimeSkillChangeContent(additions, removals, baseline)
	if content == "" {
		if _, err := mgr.ClearSection(sessionID, runtimeSkillAttachmentSection); err != nil {
			return err
		}
		return nil
	}
	_, err := mgr.AddSection(sessionID, runtimeSkillAttachmentSection, content, hprompts.AttachmentSkill, runtimeSkillAttachmentSource)
	return err
}

// buildRuntimeSkillChangeContent 渲染技能变更 + 进化经验，全空返回空串。
// Python: _build_runtime_skill_change_content (:783-849)
func (r *SkillUseRail) buildRuntimeSkillChangeContent(additions, removals, baseline []*skillpkg.Skill) string {
	isEnglish := strings.HasPrefix(strings.ToLower(r.language()), "en")

	var evolutionSkills []*skillpkg.Skill
	seen := map[string]struct{}{}
	for _, skill := range append(append([]*skillpkg.Skill{}, baseline...), additions...) {
		if skill == nil {
			continue
		}
		if _, ok := seen[skill.Name]; ok {
			continue
		}
		if strings.TrimSpace(r.evolutionTexts[skill.Name]) == "" {
			continue
		}
		seen[skill.Name] = struct{}{}
		evolutionSkills = append(evolutionSkills, skill)
	}

	if len(additions) == 0 && len(removals) == 0 && len(evolutionSkills) == 0 {
		return ""
	}

	var lines []string
	if isEnglish {
		lines = append(lines, "Skill environment status update. Invoke relevant skills only when needed for the current task.")
		if len(additions) > 0 {
			lines = append(lines, "Newly available skills:")
			for idx, skill := range additions {
				lines = append(lines, sections.BuildSkillLine(idx, skill.Name, skill.Description, ""))
			}
		}
		if len(removals) > 0 {
			lines = append(lines, "Unavailable skills (removed from the environment):")
			for _, skill := range removals {
				lines = append(lines, "- "+skill.Name)
			}
		}
		if len(evolutionSkills) > 0 {
			lines = append(lines, "Skill evolution experience reference:")
			for _, skill := range evolutionSkills {
				lines = append(lines, "[Skill: "+skill.Name+"]")
				lines = append(lines, strings.TrimSpace(r.evolutionTexts[skill.Name]))
			}
		}
		return strings.Join(lines, "\n")
	}

	lines = append(lines, "Skill 环境状态更新。请根据当前任务需要，按需调用相关 Skill。")
	if len(additions) > 0 {
		lines = append(lines, "新增可用 Skill：")
		for idx, skill := range additions {
			lines = append(lines, sections.BuildSkillLine(idx, skill.Name, skill.Description, ""))
		}
	}
	if len(removals) > 0 {
		lines = append(lines, "已移除、当前不可用的 Skill：")
		for _, skill := range removals {
			lines = append(lines, "- "+skill.Name)
		}
	}
	if len(evolutionSkills) > 0 {
		lines = append(lines, "Skill 演进经验参考：")
		for _, skill := range evolutionSkills {
			lines = append(lines, "[Skill: "+skill.Name+"]")
			lines = append(lines, strings.TrimSpace(r.evolutionTexts[skill.Name]))
		}
	}
	return strings.Join(lines, "\n")
}

// prepareSkills 增量刷新 + 过滤。
// Python: SkillUseRail._prepare_skills()
func (r *SkillUseRail) prepareSkills(ctx context.Context) error {
	if !r.enableCache {
		r.skillCache = make(map[string]*skillpkg.Skill)
		r.skillUpdateAt = make(map[string]time.Time)
		r.skillOrder = make([]string, 0)
	}

	if err := r.refreshSkillsIncrementally(ctx); err != nil {
		return err
	}
	r.skills = r.filterSkills(r.collectSkillsInOrder())
	return nil
}

// refreshSkillsIncrementally 递归发现技能目录，按 SKILL.md mtime 增量比对。
// Python: SkillUseRail._refresh_skills_incrementally() (:232-260)
func (r *SkillUseRail) refreshSkillsIncrementally(ctx context.Context) error {
	roots := r.normalizeSkillDirs()
	if len(roots) == 0 {
		return errors.New("skills_dir is empty")
	}

	discovered := discoverSkillDirs(roots)
	discoveredKeys := make(map[string]struct{}, len(discovered))
	orderedKeys := make([]string, 0, len(discovered))

	for _, entry := range discovered {
		key, err := filepath.Abs(entry.Dir)
		if err != nil {
			key = entry.Dir
		}

		discoveredKeys[key] = struct{}{}
		orderedKeys = append(orderedKeys, key)

		if cachedSkill, ok := r.skillCache[key]; ok && r.skillUpdateAt[key].Equal(entry.SkillMD) {
			_ = cachedSkill
			continue
		}
		skill, err := r.loadSkill(ctx, entry.Dir, entry.SkillMD)
		if err != nil {
			continue
		}
		r.skillCache[key] = skill
		r.skillUpdateAt[key] = entry.SkillMD
	}

	// 清理已消失的技能
	for key := range r.skillCache {
		if _, ok := discoveredKeys[key]; !ok {
			delete(r.skillCache, key)
			delete(r.skillUpdateAt, key)
		}
	}

	// 按发现顺序排列
	r.skillOrder = make([]string, 0)
	for _, key := range orderedKeys {
		if _, ok := r.skillCache[key]; ok {
			r.skillOrder = append(r.skillOrder, key)
		}
	}

	return nil
}

// loadSkill 加载单个 SKILL.md。
// Python: SkillUseRail._load_skill()
func (r *SkillUseRail) loadSkill(ctx context.Context, dir string, modTime time.Time) (*skillpkg.Skill, error) {
	skillMDPath := filepath.Join(dir, "SKILL.md")

	description := ""
	desc, err := r.loadDescription(ctx, skillMDPath)
	if err != nil {
		logger.Warn(logger.ComponentAgentCore).
			Str("path", skillMDPath).
			Err(err).
			Msg("failed to load description")
	} else {
		description = desc
	}
	// Python: description or f"Skill located in {skill_dir}"，空串也走 fallback
	if description == "" {
		description = fmt.Sprintf("Skill located in %s", dir)
	}

	skill := skillpkg.NewSkill(filepath.Base(dir), description, dir)
	// Python: setattr(skill, "update_at", update_at)
	skill.UpdateAt = modTime

	return skill, nil
}

// loadYAML 从文件读取 YAML front matter。
// Python: SkillUseRail._load_yaml()
func (r *SkillUseRail) loadYAML(ctx context.Context, path string) (map[string]any, string, error) {
	var text string

	// 优先使用 SysOperation.Fs().ReadFile()
	if r.SysOperation() != nil {
		fsOp := r.SysOperation().Fs()
		if fsOp != nil {
			readRes, err := fsOp.ReadFile(ctx, path)
			if err != nil {
				return nil, "", fmt.Errorf("读取文件失败 %s: %w", path, err)
			}
			if readRes == nil || readRes.Data == nil {
				return nil, "", fmt.Errorf("读取文件失败 %s: 返回结果为空", path)
			}
			text = readRes.Data.Content
		}
	}
	// SysOperation 不可用时回退到 os.ReadFile（测试场景）
	if text == "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, "", fmt.Errorf("读取文件失败 %s: %w", path, err)
		}
		text = string(data)
	}
	if strings.HasPrefix(text, "---") {
		parts := strings.SplitN(text, "---", 3)
		if len(parts) >= 3 {
			yamlBlock := parts[1]
			body := strings.TrimLeft(parts[2], "\n\r")

			var yamlData map[string]any
			if err := yaml.Unmarshal([]byte(yamlBlock), &yamlData); err != nil {
				return nil, text, nil
			}
			return yamlData, body, nil
		}
	}
	return nil, text, nil
}

// loadDescription 从 SKILL.md 的 YAML front matter 提取 description 字段。
// Python: SkillUseRail._load_description()
func (r *SkillUseRail) loadDescription(ctx context.Context, path string) (string, error) {
	yamlData, _, err := r.loadYAML(ctx, path)
	if err != nil {
		return "", err
	}
	if yamlData == nil {
		return "", errors.New("SKILL.md 文件不包含 YAML front matter")
	}
	descVal, ok := yamlData["description"]
	if !ok {
		return "", errors.New("SKILL.md 文件不包含 description 字段")
	}
	desc, ok := descVal.(string)
	if !ok {
		return "", fmt.Errorf("SKILL.md description 字段类型错误，期望 string，实际 %T", descVal)
	}
	return desc, nil
}

// collectSkillsInOrder 按序收集 + 去重。
// Python: SkillUseRail._collect_skills_in_order()
func (r *SkillUseRail) collectSkillsInOrder() []*skillpkg.Skill {
	var collected []*skillpkg.Skill
	seenNames := make(map[string]struct{})

	for _, key := range r.skillOrder {
		skill, ok := r.skillCache[key]
		if !ok {
			continue
		}
		if _, seen := seenNames[skill.Name]; seen {
			logger.Warn(logger.ComponentAgentCore).
				Str("skill_name", skill.Name).
				Str("directory", skill.Directory).
				Msg("duplicate skill name detected, keeping first loaded skill")
			continue
		}
		seenNames[skill.Name] = struct{}{}
		collected = append(collected, skill)
	}

	return collected
}

// filterSkills enabled/disabled 过滤。
// Python: SkillUseRail._filter_skills()
func (r *SkillUseRail) filterSkills(skills []*skillpkg.Skill) []*skillpkg.Skill {
	var filtered []*skillpkg.Skill

	for _, skill := range skills {
		if len(r.enabledSkills) > 0 {
			if _, ok := r.enabledSkills[skill.Name]; !ok {
				continue
			}
		}
		if _, ok := r.disabledSkills[skill.Name]; ok {
			continue
		}
		filtered = append(filtered, skill)
	}

	return filtered
}

// fetchEvolutionTexts 从 EvolutionStore 读取演化经验文本。
// Python: SkillUseRail._fetch_evolution_texts() L333-346
func (r *SkillUseRail) fetchEvolutionTexts(ctx context.Context) {
	if r.evolutionStore == nil {
		return
	}
	for _, skill := range r.skills {
		text := r.evolutionStore.FormatDescExperienceText(ctx, skill.Name, 5)
		r.evolutionTexts[skill.Name] = text
	}
}

// getSkillDescription 返回附加演化经验文本的描述。
// Python: SkillUseRail._get_skill_description() L348-354
func (r *SkillUseRail) getSkillDescription(skill *skillpkg.Skill) string {
	desc := skill.Description
	if evoText, ok := r.evolutionTexts[skill.Name]; ok && evoText != "" {
		desc = desc + "\n  演进经验:\n" + evoText
	}
	return desc
}

// buildSkillsSectionFor 按给定技能集合构建 PromptSection。
// Python: _build_skills_section (:526-558)
func (r *SkillUseRail) buildSkillsSectionFor(skills []*skillpkg.Skill) *saprompt.PromptSection {
	if r.skillMode == SkillModeAll {
		return r.buildAllModeSectionFor(skills)
	}
	// auto_list 模式：提示词只给一句指引，不列技能（上游忽略 skill_lines）
	return sections.BuildSkillsSection("auto_list", "", r.language())
}

// buildAllModeSectionFor 用给定集合构建 all 模式 PromptSection。
// Python: _build_skills_section all 分支 (:526-558)
func (r *SkillUseRail) buildAllModeSectionFor(skills []*skillpkg.Skill) *saprompt.PromptSection {
	// 预算裁剪：按与当前查询的关键词重叠度排序后整块丢弃低排名技能
	budgeted := applySkillBudget(skills, r.currentQuery, r.maxSkills, r.maxTotalChars)
	var bodyLines []string
	for idx, skill := range budgeted {
		bodyLines = append(bodyLines, sections.BuildSkillLine(
			idx,
			skill.Name,
			r.getSkillDescription(skill),
			"", // Python: skill_md_path 已注释掉 (# No longer needed with SkillTool)
		))
	}
	skillLines := sections.BuildSkillLines(bodyLines)
	return sections.BuildSkillsSection("all", skillLines, r.language())
}

// language 获取当前语言
func (r *SkillUseRail) language() string {
	if r.systemPromptBuilder != nil {
		return r.systemPromptBuilder.Language()
	}
	return "cn"
}

// skillMDPath 返回 SKILL.md 路径。
// Python: SkillUseRail._skill_md_path()
func skillMDPath(skill *skillpkg.Skill) string {
	return filepath.Join(skill.Directory, "SKILL.md")
}

// normalizeNameList 规范化名称列表（支持逗号/分号分隔）。
// Python: SkillUseRail._normalize_name_list()，但仅接受 []string
// （字符串输入场景由 parseSkillDirs 单独处理）。
func normalizeNameList(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	var result []string
	for _, item := range names {
		text := strings.TrimSpace(item)
		if text == "" {
			continue
		}
		normalized := strings.ReplaceAll(text, ";", ",")
		for _, part := range strings.Split(normalized, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				result = append(result, part)
			}
		}
	}
	return result
}

// normalizeNameSet 规范化名称集合。
// Python: SkillUseRail._normalize_name_set()
func normalizeNameSet(names []string) map[string]struct{} {
	normalized := normalizeNameList(names)
	result := make(map[string]struct{}, len(normalized))
	for _, name := range normalized {
		result[name] = struct{}{}
	}
	return result
}

// parseSkillDirs 解析分号/逗号分隔字符串。
// Python: SkillUseRail._parse_skill_dirs()
func parseSkillDirs(raw string) []string {
	if raw == "" || strings.TrimSpace(raw) == "" {
		return nil
	}
	normalized := strings.ReplaceAll(raw, ",", ";")
	var result []string
	for _, item := range strings.Split(normalized, ";") {
		item = strings.TrimSpace(item)
		if item != "" {
			result = append(result, item)
		}
	}
	return result
}

// normalizeSkillDirs 将 skillsDir 规范化为绝对路径列表。
// Python: SkillUseRail._normalize_skill_dirs()
func (r *SkillUseRail) normalizeSkillDirs() []string {
	return normalizeSkillDirs(r.skillsDir)
}

// normalizeSkillDirs 将 skillsDir 规范化为绝对路径列表。
// Python: SkillUseRail._normalize_skill_dirs()
func normalizeSkillDirs(skillsDir []string) []string {
	var rawDirs []string
	for _, item := range skillsDir {
		parsed := parseSkillDirs(item)
		if len(parsed) > 0 {
			rawDirs = append(rawDirs, parsed...)
		} else if strings.TrimSpace(item) != "" {
			rawDirs = append(rawDirs, strings.TrimSpace(item))
		}
	}

	var normalized []string
	for _, raw := range rawDirs {
		if raw == "" || strings.TrimSpace(raw) == "" {
			continue
		}
		expanded := pathutil.ExpandHome(raw)
		abs, err := filepath.Abs(expanded)
		if err != nil {
			abs = expanded
		}
		normalized = append(normalized, abs)
	}

	return normalized
}

// applySkillBudget 按与当前 query 的关键词重叠度排序，并按数量/字符预算整块丢弃技能。
// 丢弃以「整个技能」为粒度，绝不截断到半截文本——半截说明会让模型误用技能。
// Python: SkillUseRail._apply_skill_budget()
func applySkillBudget(skills []*skillpkg.Skill, query string, maxSkills, maxTotalChars int) []*skillpkg.Skill {
	if len(skills) == 0 {
		return skills
	}
	if maxSkills <= 0 && maxTotalChars <= 0 {
		return skills
	}

	// 有 query 时按关键词重叠度排序（同分按技能名，保证确定性）；无 query 保持原序。
	ranked := make([]*skillpkg.Skill, len(skills))
	copy(ranked, skills)
	if query != "" {
		keywords := extractSkillKeywords(query)
		type scored struct {
			score int
			skill *skillpkg.Skill
		}
		items := make([]scored, 0, len(ranked))
		for _, skill := range ranked {
			if skill == nil {
				continue
			}
			text := strings.ToLower(skill.Name + " " + skill.Description)
			score := 0
			for kw := range keywords {
				if strings.Contains(text, kw) {
					score++
				}
			}
			items = append(items, scored{score: score, skill: skill})
		}
		sort.SliceStable(items, func(i, j int) bool {
			if items[i].score != items[j].score {
				return items[i].score > items[j].score
			}
			return items[i].skill.Name < items[j].skill.Name
		})
		ranked = ranked[:0]
		for _, it := range items {
			ranked = append(ranked, it.skill)
		}
	}

	// 数量硬上限
	if maxSkills > 0 && len(ranked) > maxSkills {
		dropped := ranked[maxSkills:]
		ranked = ranked[:maxSkills]
		names := make([]string, 0, len(dropped))
		for _, s := range dropped {
			names = append(names, s.Name)
		}
		logger.Info(logger.ComponentAgentCore).
			Int("dropped", len(dropped)).
			Int("max_skills", maxSkills).
			Str("names", strings.Join(names, ", ")).
			Msg("[SkillUseRail] skills dropped due to max_skills")
	}

	// 字符软上限：只算 description，整块丢弃，且至少保留 1 个（最相关的那个即使
	// 单独超预算也保留），保证技能小节不会变成空列表。
	// Python: SkillUseRail._apply_skill_budget() L599-634
	if maxTotalChars > 0 {
		totalChars := 0
		for _, s := range ranked {
			totalChars += len([]rune(s.Description))
		}
		for len(ranked) > 1 && totalChars > maxTotalChars {
			last := ranked[len(ranked)-1]
			ranked = ranked[:len(ranked)-1]
			totalChars -= len([]rune(last.Description))
			logger.Info(logger.ComponentAgentCore).
				Str("skill", last.Name).
				Int("max_total_chars", maxTotalChars).
				Int("remaining_chars", totalChars).
				Msg("[SkillUseRail] skill dropped to stay under max_total_chars")
		}
		if len(ranked) > 0 && totalChars > maxTotalChars {
			logger.Warn(logger.ComponentAgentCore).
				Str("skill", ranked[0].Name).
				Int("max_total_chars", maxTotalChars).
				Int("total_chars", totalChars).
				Msg("[SkillUseRail] single skill exceeds max_total_chars, keeping it to avoid empty section")
		}
	}
	return ranked
}

// extractSkillKeywords 提取小写字母数字关键词（去停用词、长度 > 2）。
// Python: SkillUseRail._extract_keywords() (L646-654)：按空白切分后丢弃非
// 字母数字字符，故 "pdf-reader" → "pdfreader"。
func extractSkillKeywords(text string) map[string]bool {
	keywords := make(map[string]bool)
	for _, token := range strings.Fields(strings.ToLower(text)) {
		cleaned := strings.Map(func(r rune) rune {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				return r
			}
			return -1
		}, token)
		if cleaned == "" || skillStopWords[cleaned] || len([]rune(cleaned)) <= 2 {
			continue
		}
		keywords[cleaned] = true
	}
	return keywords
}

// extractQueryFromMessages 从本次模型调用的消息里倒序取最后一条非空用户消息。
// Python: SkillUseRail._extract_query_from_messages() (L657-669)
func extractQueryFromMessages(messages []llmschema.BaseMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if _, ok := messages[i].(*llmschema.UserMessage); !ok {
			continue
		}
		if text := strings.TrimSpace(messages[i].GetContent().Text()); text != "" {
			return text
		}
	}
	return ""
}
