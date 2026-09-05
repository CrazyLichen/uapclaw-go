package skills

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	tool "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	sections "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	codetool "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/code"
	filesystemtool "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/filesystem"
	shelltool "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/shell"
	skilltools "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/skills"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	skillpkg "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/skills"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SkillUseRail 技能使用护栏，管理 skill 提示词注入和工具注册。
// 对齐 Python: SkillUseRail (openjiuwen/harness/rails/skills/skill_use_rail.py)
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

	// ── 运行时状态 ──
	// skills 当前已加载技能列表
	skills []*skillpkg.Skill
	// systemPromptBuilder 系统提示词构建器引用（Init 中获取）
	systemPromptBuilder saprompt.SystemPromptBuilderInterface

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

// SkillUseRailOption SkillUseRail 构造选项
type SkillUseRailOption func(*SkillUseRail)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// SkillModeAll 将所有技能注入系统提示词
	SkillModeAll = "all"
	// SkillModeAutoList 添加 list_skill 工具让模型自主查看技能
	SkillModeAutoList = "auto_list"

	// skillUseRailPriority SkillUseRail 优先级
	// 对齐 Python: SkillUseRail.priority = 100
	skillUseRailPriority = 100
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ValidSkillModes 有效技能模式集合
var ValidSkillModes = map[string]struct{}{
	SkillModeAll:      {},
	SkillModeAutoList: {},
}

// 编译时接口检查
var _ agentinterfaces.AgentRail = (*SkillUseRail)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSkillUseRail 创建 SkillUseRail 实例。
// 对齐 Python: SkillUseRail.__init__()
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
			"Unsupported skill_mode: %s. Expected one of [all, auto_list]",
			r.skillMode,
		))
	}

	return r
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
// 对齐 Python: SkillUseRail.skills_meta property
func (r *SkillUseRail) SkillsMeta() []*skillpkg.Skill {
	result := make([]*skillpkg.Skill, len(r.skills))
	for i, s := range r.skills {
		cp := *s // 值拷贝 Skill 结构体
		result[i] = &cp
	}
	return result
}

// ReloadSkills 重新加载技能 + 演化经验。
// 对齐 Python: SkillUseRail.reload_skills()
func (r *SkillUseRail) ReloadSkills(ctx context.Context) error {
	if err := r.prepareSkills(); err != nil {
		return err
	}
	r.fetchEvolutionTexts(ctx)
	return nil
}

// ClearSkills 清空缓存。
// 对齐 Python: SkillUseRail.clear_skills()
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

// Init 注册工具到 ResourceMgr + AbilityManager。
// 对齐 Python: SkillUseRail.init() L237-306
func (r *SkillUseRail) Init(agent agentinterfaces.BaseAgent) error {
	r.systemPromptBuilder = agent.SystemPromptBuilder()

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

	var tools []tool.Tool

	// 对齐 Python L246-253: 始终注册 SkillTool
	tools = append(tools, skilltools.NewSkillTool(
		op,
		func() []*skillpkg.Skill { return r.skills },
		language,
		agentID,
	))

	// 对齐 Python L255-271: includeTools 时注册 ReadFileTool/CodeTool/BashTool
	if r.includeTools {
		enableImageMultimodal := true
		if r.enableImageMultimodal != nil {
			enableImageMultimodal = *r.enableImageMultimodal
		}
		tools = append(tools,
			filesystemtool.NewReadFileTool(op, language, agentID, enableImageMultimodal),
			codetool.NewCodeTool(op, language, agentID),
			shelltool.NewBashTool(op, language, agentID, shelltool.NewPermissionConfig(shelltool.PermissionModeAuto, nil, nil)),
		)
	}

	// 对齐 Python L273-281: auto_list 模式注册 ListSkillTool
	if r.skillMode == SkillModeAutoList {
		tools = append(tools, skilltools.NewListSkillTool(
			func() []*skillpkg.Skill { return r.skills },
			r.listSkillModel,
			language,
			agentID,
		))
	}

	// 对齐 Python L283-294: 幂等注册到 ResourceMgr
	resourceMgr := runner.GetResourceMgr()
	for _, t := range tools {
		toolID := t.Card().ID
		if resourceMgr != nil && toolID != "" {
			existing, err := resourceMgr.GetTool([]string{toolID})
			if err == nil && len(existing) > 0 {
				_, _ = resourceMgr.RemoveTool([]string{toolID})
			}
			if err := resourceMgr.AddTool(t); err != nil {
				logger.Warn(logger.ComponentAgentCore).
					Str("tool_id", toolID).
					Err(err).
					Msg("failed to add tool resource to resource_mgr")
			}
			r.ownedToolIDs[toolID] = struct{}{}
		}
	}

	// 对齐 Python L296-306: 注册到 AbilityManager
	am := agent.AbilityManager()
	if am != nil {
		for _, t := range tools {
			result := am.Add(t.Card())
			if result.Added {
				r.ownedToolNames[t.Card().Name] = struct{}{}
			} else {
				logger.Warn(logger.ComponentAgentCore).
					Str("tool_name", t.Card().Name).
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

// Uninit 从 AbilityManager + ResourceMgr 注销工具。
// 对齐 Python: SkillUseRail.uninit() L308-321
func (r *SkillUseRail) Uninit(agent agentinterfaces.BaseAgent) error {
	am := agent.AbilityManager()
	if am != nil {
		for toolName := range r.ownedToolNames {
			am.Remove(toolName)
		}
	}

	r.ownedToolNames = make(map[string]struct{})
	r.ownedToolIDs = make(map[string]struct{})

	return nil
}

// BeforeInvoke 调用 refreshSkillPrompt。
// 对齐 Python: SkillUseRail.before_invoke()
func (r *SkillUseRail) BeforeInvoke(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	r.refreshSkillPrompt(context.Background())
	return nil
}

// BeforeModelCall 构建 skills section 并注入 systemPromptBuilder。
// 对齐 Python: SkillUseRail.before_model_call() L359-372
func (r *SkillUseRail) BeforeModelCall(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	if r.systemPromptBuilder == nil {
		return nil
	}
	section := r.buildSkillsSection()
	if section != nil {
		r.systemPromptBuilder.AddSection(*section)
	} else {
		r.systemPromptBuilder.RemoveSection(sections.SectionSkills)
	}
	return nil
}

// AfterInvoke 空操作。
// 对齐 Python: SkillUseRail.after_invoke()
func (r *SkillUseRail) AfterInvoke(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// LoadSkillsFromDir 类方法：静态加载技能。
// 对齐 Python: SkillUseRail.load_skills_from_dir()
func LoadSkillsFromDir(ctx context.Context, skillsDir []string) ([]*skillpkg.Skill, error) {
	roots := normalizeSkillDirs(skillsDir)
	if len(roots) == 0 {
		return nil, errors.New("skills_dir is empty")
	}

	skillMap := make(map[string]*skillpkg.Skill)
	loader := NewSkillUseRail(skillsDir, WithSkillMode(SkillModeAll), WithIncludeTools(false))

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
			skill, err := loader.loadSkill(filepath.Join(root, entry.Name()), fileInfo.ModTime())
			if err != nil {
				continue
			}

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

	result := make([]*skillpkg.Skill, 0, len(skillMap))
	for _, skill := range skillMap {
		result = append(result, skill)
	}
	return result, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// refreshSkillPrompt 重新加载技能 + 演化经验。
// 对齐 Python: SkillUseRail.refresh_skill_prompt()
func (r *SkillUseRail) refreshSkillPrompt(ctx context.Context) {
	_ = r.prepareSkills()
	r.fetchEvolutionTexts(ctx)
}

// prepareSkills 增量刷新 + 过滤。
// 对齐 Python: SkillUseRail._prepare_skills()
func (r *SkillUseRail) prepareSkills() error {
	if !r.enableCache {
		r.skillCache = make(map[string]*skillpkg.Skill)
		r.skillUpdateAt = make(map[string]time.Time)
		r.skillOrder = make([]string, 0)
	}

	if err := r.refreshSkillsIncrementally(); err != nil {
		return err
	}
	r.skills = r.filterSkills(r.collectSkillsInOrder())
	return nil
}

// refreshSkillsIncrementally 遍历 skillsDir，mtime 增量比对。
// 对齐 Python: SkillUseRail._refresh_skills_incrementally() L123-175
func (r *SkillUseRail) refreshSkillsIncrementally() error {
	roots := r.normalizeSkillDirs()
	if len(roots) == 0 {
		return errors.New("skills_dir is empty")
	}

	discoveredKeys := make(map[string]struct{})
	var orderedKeys []string

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

			key, _ := filepath.Abs(filepath.Join(root, entry.Name()))
			updateAt := fileInfo.ModTime()

			discoveredKeys[key] = struct{}{}
			orderedKeys = append(orderedKeys, key)

			cachedSkill := r.skillCache[key]
			cachedUpdateAt := r.skillUpdateAt[key]

			if cachedSkill == nil || !cachedUpdateAt.Equal(updateAt) {
				skill, err := r.loadSkill(filepath.Join(root, entry.Name()), updateAt)
				if err == nil {
					r.skillCache[key] = skill
					r.skillUpdateAt[key] = updateAt
				}
			}
		}
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
// 对齐 Python: SkillUseRail._load_skill()
func (r *SkillUseRail) loadSkill(dir string, modTime time.Time) (*skillpkg.Skill, error) {
	skillMDPath := filepath.Join(dir, "SKILL.md")

	description := ""
	desc, err := r.loadDescription(skillMDPath)
	if err != nil {
		logger.Warn(logger.ComponentAgentCore).
			Str("path", skillMDPath).
			Err(err).
			Msg("Failed to load description")
		description = fmt.Sprintf("Skill located in %s", dir)
	} else {
		description = desc
	}

	skill := skillpkg.NewSkill(filepath.Base(dir), description, dir)
	// 对齐 Python: setattr(skill, "update_at", update_at)
	skill.UpdateAt = modTime

	return skill, nil
}

// loadYAML 从文件读取 YAML front matter。
// 对齐 Python: SkillUseRail._load_yaml()
func (r *SkillUseRail) loadYAML(path string) (map[string]any, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("读取文件失败 %s: %w", path, err)
	}

	text := string(data)
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
// 对齐 Python: SkillUseRail._load_description()
func (r *SkillUseRail) loadDescription(path string) (string, error) {
	yamlData, _, err := r.loadYAML(path)
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
// 对齐 Python: SkillUseRail._collect_skills_in_order()
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
				Msg("duplicate skill name detected, keep first loaded skill")
			continue
		}
		seenNames[skill.Name] = struct{}{}
		collected = append(collected, skill)
	}

	return collected
}

// filterSkills enabled/disabled 过滤。
// 对齐 Python: SkillUseRail._filter_skills()
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
// 对齐 Python: SkillUseRail._fetch_evolution_texts() L333-346
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
// 对齐 Python: SkillUseRail._get_skill_description() L348-354
func (r *SkillUseRail) getSkillDescription(skill *skillpkg.Skill) string {
	desc := skill.Description
	if evoText, ok := r.evolutionTexts[skill.Name]; ok && evoText != "" {
		desc = desc + "\n  演进经验:\n" + evoText
	}
	return desc
}

// buildSkillsSection 构建 PromptSection。
// 对齐 Python: SkillUseRail._build_skills_section()
func (r *SkillUseRail) buildSkillsSection() *saprompt.PromptSection {
	if r.skillMode == SkillModeAll {
		return r.buildAllModeSection()
	}
	// auto_list 模式
	section := sections.BuildSkillsSection("auto_list", "", r.language())
	return &section
}

// buildAllModeSection 构建 all 模式 PromptSection。
// 对齐 Python: SkillUseRail._build_skills_section() all 分支
func (r *SkillUseRail) buildAllModeSection() *saprompt.PromptSection {
	var bodyLines []string
	for idx, skill := range r.skills {
		bodyLines = append(bodyLines, sections.BuildSkillLine(
			idx,
			skill.Name,
			r.getSkillDescription(skill),
		))
	}
	skillLines := sections.BuildSkillLines(bodyLines)
	section := sections.BuildSkillsSection("all", skillLines, r.language())
	return &section
}

// language 获取当前语言
func (r *SkillUseRail) language() string {
	if r.systemPromptBuilder != nil {
		return r.systemPromptBuilder.Language()
	}
	return "cn"
}

// skillMDPath 返回 SKILL.md 路径。
// 对齐 Python: SkillUseRail._skill_md_path()
func skillMDPath(skill *skillpkg.Skill) string {
	return filepath.Join(skill.Directory, "SKILL.md")
}

// normalizeNameList 规范化名称列表（支持逗号/分号分隔）。
// 对齐 Python: SkillUseRail._normalize_name_list()，但仅接受 []string
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
// 对齐 Python: SkillUseRail._normalize_name_set()
func normalizeNameSet(names []string) map[string]struct{} {
	normalized := normalizeNameList(names)
	result := make(map[string]struct{}, len(normalized))
	for _, name := range normalized {
		result[name] = struct{}{}
	}
	return result
}

// parseSkillDirs 解析分号/逗号分隔字符串。
// 对齐 Python: SkillUseRail._parse_skill_dirs()
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
// 对齐 Python: SkillUseRail._normalize_skill_dirs()
func (r *SkillUseRail) normalizeSkillDirs() []string {
	return normalizeSkillDirs(r.skillsDir)
}

// normalizeSkillDirs 将 skillsDir 规范化为绝对路径列表。
// 对齐 Python: SkillUseRail._normalize_skill_dirs()
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
		abs, err := filepath.Abs(raw)
		if err != nil {
			abs = raw
		}
		normalized = append(normalized, abs)
	}

	return normalized
}
