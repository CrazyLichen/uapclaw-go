// Package tools 内 SkillRetrievalToolkit——JiuwenSwarm 对 Symphony DCI
// 兼容技能发现的适配层：会话本地 skill_index 工具的装配与 session_profile
// 供体。Python: skill_retrieval_toolkits.py（651 行）。
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/tools/skill_retrieval_toolkits.py
package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/symphony/discovery"
	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	skillruntime "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// candidateBudgetRatio 产品级不变量：完整元数据仅在严格低于上下文
	// 窗口 1% 时全量注入（Python _CANDIDATE_BUDGET_RATIO）。
	candidateBudgetRatio = 0.01
	// sessionProfileVersion sidecar schema 版本（Python _SESSION_PROFILE_VERSION）
	sessionProfileVersion = 1
)

// trueValues 环境开关真值集合（Python _TRUE_VALUES）
var trueValues = map[string]bool{"1": true, "true": true, "yes": true, "on": true, "enabled": true}

// ──────────────────────────── 结构体 ────────────────────────────

// SkillDirectoriesProvider 目录提供回调（Python SkillDirectoriesProvider 的
// callable 形态；静态序列由调用方包装成函数）
type SkillDirectoriesProvider func() []string

// DisabledSkillsProvider 禁用名单提供回调
type DisabledSkillsProvider func() []string

// SkillSourcesProvider 来源映射提供回调
type SkillSourcesProvider func() map[string]string

// SkillRetrievalToolkit 会话本地结构化技能索引工具包。
// Python: SkillRetrievalToolkit（skill_retrieval_toolkits.py:277-470）。
type SkillRetrievalToolkit struct {
	toolName string

	skillDirectories      SkillDirectoriesProvider
	disabledSkills        DisabledSkillsProvider
	sourceByName          SkillSourcesProvider
	visibleSkillNames     discovery.VisibleSkillNames
	indexSkillDirectories SkillDirectoriesProvider
	sessionScope          string
	configBase            map[string]any

	indexEnabled             bool
	candidateScale           string
	estimatedCandidateTokens int
	candidateBudgetTokens    int
	effectiveStrategy        string
	settings                 discovery.DiscoverySettings

	frozenPromptSnapshot discovery.SkillPromptSnapshot
	frozenSelectionCards map[string]map[string]string

	environment     *discovery.SkillFS
	indexToolkit    *discovery.InstalledSkillsDirectoryToolkit
	autoBuildResult map[string]any
}

// ToolkitOptions 构造选项（对齐 Python __init__ 关键字参数）。
type ToolkitOptions struct {
	// SkillDirectories live 扫描目录（必填）
	SkillDirectories SkillDirectoriesProvider
	// DisabledSkills 禁用名单（可 nil）
	DisabledSkills DisabledSkillsProvider
	// SourceByName 来源映射（可 nil）
	SourceByName SkillSourcesProvider
	// VisibleSkillNames 可见名单（可 nil）
	VisibleSkillNames discovery.VisibleSkillNames
	// IndexSkillDirectories 共享 taxonomy 构建目录（nil=currentRecords）
	IndexSkillDirectories SkillDirectoriesProvider
	// SessionScope 会话标识（缺省 "default"）
	SessionScope string
	// ConfigBase 应用配置（深拷贝持有）
	ConfigBase map[string]any
	// Settings 显式设置（nil=BuildDiscoverySettings）
	Settings *discovery.DiscoverySettings
	// ArtifactRoot 显式产物根（空=SkillRetrievalArtifactRoot）
	ArtifactRoot string
	// AutoBuildIndex 是否允许自动后台构建（D-16-4-5：⤴️16.5 仅登记日志）
	AutoBuildIndex bool
	// FrozenProfile sidecar 恢复的 profile（可 nil）
	FrozenProfile map[string]any
}

// ──────────────────────────── 导出函数 ────────────────────────────

// SettingsFromProfile 从 profile 恢复设置（canonical 校验）。
// 对齐 _settings_from_profile（skill_retrieval_toolkits.py:37-62）：
// 逐键类型与值必须与 LoadDiscoverySettings 结果一致，否则 nil。
func SettingsFromProfile(profile map[string]any) *discovery.DiscoverySettings {
	if profile == nil {
		return nil
	}
	raw, ok := profile["settings"].(map[string]any)
	if !ok {
		return nil
	}
	values := map[string]any{}
	for k, v := range raw {
		values[k] = v
	}
	restored := discovery.LoadDiscoverySettings(values)
	expected := settingsAsDict(restored)
	for key, value := range expected {
		got, present := values[key]
		if !present || !sameScalarType(got, value) || !scalarEqual(got, value) {
			return nil
		}
	}
	return &restored
}

// SnapshotFromProfile 从 profile 恢复冻结提示词快照。
// 对齐 _snapshot_from_profile（skill_retrieval_toolkits.py:65-119）。
func SnapshotFromProfile(profile map[string]any) *discovery.SkillPromptSnapshot {
	if profile == nil {
		return nil
	}
	raw, ok := profile["prompt_snapshot"].(map[string]any)
	if !ok {
		return nil
	}
	rawEntries, _ := raw["entries"].([]any)
	rawBranches, hasBranches := raw["branches"].([]any)
	if !hasBranches {
		rawBranches = []any{}
	}
	if rawEntries == nil {
		return nil
	}
	mode := stringOrPy(raw["mode"])
	indexState := stringOrPy(raw["index_state"])
	totalCount, err := intOfPy(raw["total_count"])
	if err != nil {
		return nil
	}
	estimatedTokens, err := intOfPy(raw["estimated_candidate_tokens"])
	if err != nil {
		return nil
	}
	budgetTokens, err := intOfPy(raw["candidate_budget_tokens"])
	if err != nil {
		return nil
	}
	omittedCount := 0
	if raw["omitted_branch_count"] != nil {
		omittedCount, err = intOfPy(raw["omitted_branch_count"])
		if err != nil {
			return nil
		}
	}
	switch mode {
	case "small", "large-flat", "indexed", "indexed-stale":
	default:
		return nil
	}
	switch indexState {
	case "missing", "fresh", "stale":
	default:
		return nil
	}
	if totalCount < 0 || estimatedTokens < 0 || omittedCount < 0 || budgetTokens < 1 {
		return nil
	}
	snapshot := discovery.SkillPromptSnapshot{
		Mode:                     mode,
		TotalCount:               totalCount,
		EstimatedCandidateTokens: estimatedTokens,
		CandidateBudgetTokens:    budgetTokens,
		IndexState:               indexState,
		OmittedBranchCount:       omittedCount,
	}
	for _, e := range rawEntries {
		entry, ok := e.(map[string]any)
		if !ok {
			continue
		}
		workerID := stringOrPy(entry["worker_id"])
		if workerID == "" {
			continue
		}
		snapshot.Entries = append(snapshot.Entries, discovery.SkillPromptEntry{
			WorkerID:    workerID,
			Description: stringOrPy(entry["description"]),
			Source:      stringOrPy(entry["source"]),
		})
	}
	for _, b := range rawBranches {
		branch, ok := b.(map[string]any)
		if !ok {
			continue
		}
		path := stringOrPy(branch["path"])
		if path == "" {
			continue
		}
		snapshot.Branches = append(snapshot.Branches, discovery.SkillPromptBranch{
			Path:        path,
			Label:       stringOrPy(branch["label"]),
			Description: stringOrPy(branch["description"]),
		})
	}
	return &snapshot
}

// IsValidSkillRetrievalSessionProfile 持久化 v1 profile 的消费前校验。
// 对齐 is_valid_skill_retrieval_session_profile（skill_retrieval_toolkits.py:122-147）。
func IsValidSkillRetrievalSessionProfile(profile map[string]any) bool {
	if profile == nil {
		return false
	}
	if v, ok := profile["schema_version"].(int); !ok || v != sessionProfileVersion {
		if f, ok := profile["schema_version"].(float64); !ok || int(f) != sessionProfileVersion {
			return false
		}
	}
	if enabledFalse, ok := profile["enabled"].(bool); ok && !enabledFalse {
		return true
	}
	if _, ok := profile["index_enabled"].(bool); !ok {
		return false
	}
	if _, ok := profile["selection_cards"].(map[string]any); !ok {
		return false
	}
	if SettingsFromProfile(profile) == nil {
		return false
	}
	if SnapshotFromProfile(profile) == nil {
		return false
	}
	rawIndexSnapshot, present := profile["index_snapshot"]
	if !present || rawIndexSnapshot == nil {
		return stringOrPy(profile["pinned_index_revision"]) == ""
	}
	snapshot, err := discovery.SkillIndexSnapshotFromDict(asAnyMap(rawIndexSnapshot))
	if err != nil {
		return false
	}
	return snapshot.Fingerprint == stringOrPy(profile["pinned_index_revision"])
}

// SkillSourcesFromManager 把本地/插件技能来源投影进 core 记录。
// 对齐 skill_sources_from_manager（skill_retrieval_toolkits.py:161-177）：
// get_local_skills → get_installed_plugins，name+source（回落 marketplace）。
func SkillSourcesFromManager(manager *skillruntime.SkillManager) map[string]string {
	sources := map[string]string{}
	if manager == nil {
		return sources
	}
	for _, method := range []func() []map[string]any{manager.GetLocalSkills, manager.GetInstalledPlugins} {
		for _, item := range method() {
			name := strings.TrimSpace(stringOrPy(item["name"]))
			source := strings.TrimSpace(stringOrPy(orAny(item["source"], item["marketplace"])))
			if name != "" && source != "" {
				if _, exists := sources[name]; !exists {
					sources[name] = source
				}
			}
		}
	}
	return sources
}

// IsSkillRetrievalEnabled Symphony 平铺/索引发现路径总开关。
// 对齐 is_skill_retrieval_enabled（skill_retrieval_toolkits.py:201-209）：
// SYMPHONY_SKILL_RETRIEVAL_ENABLED env 覆盖 → symphony.skill_retrieval.enabled。
func IsSkillRetrievalEnabled(configBase map[string]any) bool {
	if override := envSwitch("SYMPHONY_SKILL_RETRIEVAL_ENABLED"); override != nil {
		return *override
	}
	return boolOfPy(retrievalConfig(configBase)["enabled"], false)
}

// IsSkillRetrievalIndexEnabled taxonomy 开关（现随检索总开关，兼容别名）。
// 对齐 is_skill_retrieval_index_enabled（skill_retrieval_toolkits.py:212-215）。
func IsSkillRetrievalIndexEnabled(configBase map[string]any) bool {
	return IsSkillRetrievalEnabled(configBase)
}

// BuildDiscoverySettings 应用侧公开的 discovery 设置。
// 对齐 build_discovery_settings（skill_retrieval_toolkits.py:218-237）：
// 占比强制 0.01（忽略 stale/用户配置），use_existing_index 随检索开关。
func BuildDiscoverySettings(configBase map[string]any) discovery.DiscoverySettings {
	cfg := configOf(configBase)
	settings := discovery.LoadDiscoverySettings(cfg)
	settings.CandidateBudgetRatio = candidateBudgetRatio
	settings.UseExistingIndex = IsSkillRetrievalEnabled(cfg)
	return settings
}

// BuildModelDiscoverySettings 从会话实际构建的模型解析设置。
// 对齐 build_model_discovery_settings（skill_retrieval_toolkits.py:240-275）。
func BuildModelDiscoverySettings(configBase map[string]any, modelName string, modelConfigObj, agentosCtxWindow any) discovery.DiscoverySettings {
	settings := BuildDiscoverySettings(configBase)
	if modelName == "" && modelConfigObj == nil && agentosCtxWindow == nil {
		return settings
	}
	cfg := configOf(configBase)
	react, _ := cfg["react"].(map[string]any)
	if react == nil {
		react = map[string]any{}
	}
	contextEngine, _ := react["context_engine_config"].(map[string]any)
	if contextEngine == nil {
		contextEngine = map[string]any{}
	}
	window := config.ResolveContextWindowTokens(modelName, contextEngine, modelConfigObj, agentosCtxWindow)
	if window < 1 {
		window = 1
	}
	settings.ContextWindowTokens = window
	return settings
}

// SkillRetrievalArtifactRoot 索引产物根三级解析。
// 对齐 skill_retrieval_artifact_root（skill_retrieval_toolkits.py:278-289）：
// symphony.skill_retrieval.artifact_root → SYMPHONY_SKILL_RETRIEVAL_ROOT
// → AgentWorkspaceDir()/symphony/skill_retrieval。
func SkillRetrievalArtifactRoot(configBase map[string]any) string {
	artifactRootCfg, _ := retrievalConfig(configBase)["artifact_root"].(string)
	raw := strings.TrimSpace(orStr(artifactRootCfg, os.Getenv("SYMPHONY_SKILL_RETRIEVAL_ROOT")))
	if raw != "" {
		abs, err := filepath.Abs(expandUser(raw))
		if err == nil {
			return abs
		}
		return raw
	}
	return filepath.Join(workspace.AgentWorkspaceDir(), "symphony", "skill_retrieval")
}

// ResolveSkillRetrievalStrategy 策略四态推导（纯函数）。
// 对齐 resolve_skill_retrieval_strategy（skill_retrieval_toolkits.py:622-634）。
func ResolveSkillRetrievalStrategy(enabled bool, candidateScale, layout, indexState string) string {
	if !enabled {
		return "legacy"
	}
	if candidateScale == "small" {
		return "small_full"
	}
	if layout != "tree" {
		return "large_flat"
	}
	if indexState == "fresh" {
		return "indexed"
	}
	return "indexed_stale"
}

// NewSkillRetrievalToolkit 构造（对齐 __init__ skill_retrieval_toolkits.py:288-470）。
func NewSkillRetrievalToolkit(opts ToolkitOptions) (*SkillRetrievalToolkit, error) {
	tk := &SkillRetrievalToolkit{
		toolName:              discovery.SkillIndexToolName,
		skillDirectories:      opts.SkillDirectories,
		disabledSkills:        opts.DisabledSkills,
		sourceByName:          opts.SourceByName,
		visibleSkillNames:     opts.VisibleSkillNames,
		indexSkillDirectories: opts.IndexSkillDirectories,
		sessionScope:          opts.SessionScope,
		autoBuildResult:       nil,
	}
	if tk.sessionScope == "" {
		tk.sessionScope = "default"
	}
	// 配置深拷贝：会话期配置不可变（Python deepcopy）
	if opts.ConfigBase != nil {
		tk.configBase = deepCopyMap(opts.ConfigBase)
	}

	// 步骤 1：配置化设置（:296-300）
	configuredSettings := discovery.DefaultDiscoverySettings()
	if opts.Settings != nil {
		configuredSettings = *opts.Settings
	} else {
		configuredSettings = BuildDiscoverySettings(tk.configBase)
	}
	configuredSettings.CandidateBudgetRatio = candidateBudgetRatio

	// 步骤 2：frozen_profile 防御校验（:301-305）
	frozenProfile := opts.FrozenProfile
	if frozenProfile != nil && !IsValidSkillRetrievalSessionProfile(frozenProfile) {
		invalidProfileWarn()
		frozenProfile = nil
	}

	// 步骤 3：三段恢复（:306-320）
	restoredSettings := SettingsFromProfile(frozenProfile)
	restoredSnapshot := SnapshotFromProfile(frozenProfile)
	var restoredIndexSnapshot *discovery.SkillIndexSnapshot
	if frozenProfile != nil {
		if raw, ok := frozenProfile["index_snapshot"]; ok && raw != nil {
			snapshot, err := discovery.SkillIndexSnapshotFromDict(asAnyMap(raw))
			if err == nil {
				restoredIndexSnapshot = snapshot
			}
		}
	}
	if restoredSettings != nil {
		configuredSettings = *restoredSettings
	}
	configuredSettings.CandidateBudgetRatio = candidateBudgetRatio

	// 步骤 4：index_enabled（:321-327）
	if frozenProfile != nil {
		tk.indexEnabled = boolOfPy(frozenProfile["enabled"], IsSkillRetrievalEnabled(tk.configBase))
	} else {
		tk.indexEnabled = IsSkillRetrievalEnabled(tk.configBase)
	}

	// 步骤 5：产物根（:328-334）
	resolvedArtifactRoot := strings.TrimSpace(opts.ArtifactRoot)
	if resolvedArtifactRoot == "" {
		resolvedArtifactRoot = SkillRetrievalArtifactRoot(opts.ConfigBase)
	}

	// 步骤 6：一次扫描喂饱阈值估算/附录/初始产物（:335-357）
	initialRecords, err := tk.currentRecords()
	if err != nil {
		return nil, err
	}
	flatSettings := configuredSettings
	flatSettings.UseExistingIndex = false
	probeEnvironment, err := discovery.NewSkillFS(sessionRecordsFn(initialRecords), discovery.SkillFSOptions{
		Settings:          flatSettings,
		VisibleSkillNames: tk.visibleSkillNames,
		ArtifactRoot:      resolvedArtifactRoot,
		IndexRoot:         resolvedArtifactRoot,
	})
	if err != nil {
		return nil, err
	}
	flatSnapshot := probeEnvironment.PromptSnapshot(false)
	baseSnapshot := flatSnapshot
	if restoredSnapshot != nil {
		baseSnapshot = *restoredSnapshot
	}
	// 冻结选择卡：profile 优先，回落现扫
	restoredCards, _ := frozenProfile["selection_cards"].(map[string]any)
	if restoredCards != nil {
		tk.frozenSelectionCards = map[string]map[string]string{}
		for key, value := range restoredCards {
			card, ok := value.(map[string]any)
			if !ok || key == "" {
				continue
			}
			name := stringOrPy(card["name"])
			if name == "" {
				name = key
			}
			tk.frozenSelectionCards[key] = map[string]string{
				"name":        name,
				"description": stringOrPy(card["description"]),
			}
		}
	} else {
		tk.frozenSelectionCards = probeEnvironment.SelectionCards(false)
	}
	if baseSnapshot.AllCandidatesIncluded() {
		tk.candidateScale = "small"
	} else {
		tk.candidateScale = "large"
	}
	tk.estimatedCandidateTokens = baseSnapshot.EstimatedCandidateTokens
	tk.candidateBudgetTokens = baseSnapshot.CandidateBudgetTokens

	// 步骤 7：consume_index——大会话且开关开才消费 taxonomy（:359-365）
	consumeIndex := tk.candidateScale == "large" && tk.indexEnabled
	tk.settings = configuredSettings
	tk.settings.UseExistingIndex = consumeIndex

	// 步骤 8：首刷用初始扫描，后续走 live provider（:366-372）
	initialAvailable := true
	sessionRecords := func() (discovery.SkillInventory, error) {
		if initialAvailable {
			initialAvailable = false
			return discovery.InventoryFromRecords(initialRecords), nil
		}
		return tk.currentRecordsInv()
	}

	// 步骤 9：环境装配（:374-386）
	envOpts := discovery.SkillFSOptions{
		Settings:          tk.settings,
		VisibleSkillNames: tk.visibleSkillNames,
		ArtifactRoot:      resolvedArtifactRoot,
		IndexRoot:         resolvedArtifactRoot,
		PinIndexRevision:  consumeIndex,
	}
	if consumeIndex && frozenProfile != nil {
		envOpts.SnapshotProvided = true
		envOpts.PinnedSnapshot = restoredIndexSnapshot
	}
	environment, err := discovery.NewSkillFS(sessionRecords, envOpts)
	if err != nil {
		return nil, err
	}
	tk.environment = environment

	// 步骤 10：旧 profile 分支朝向恢复（:387-396）
	environmentSnapshot := environment.PromptSnapshot(false)
	if restoredSnapshot != nil &&
		(restoredSnapshot.Mode == "indexed" || restoredSnapshot.Mode == "indexed-stale") {
		if len(restoredSnapshot.Branches) == 0 && len(environmentSnapshot.Branches) > 0 {
			restoredSnapshot.Branches = environmentSnapshot.Branches
			restoredSnapshot.OmittedBranchCount = environmentSnapshot.OmittedBranchCount
		}
	}
	if restoredSnapshot != nil {
		tk.frozenPromptSnapshot = *restoredSnapshot
	} else {
		tk.frozenPromptSnapshot = environmentSnapshot
	}

	// 步骤 11：有效策略（:397-409）
	restoredStrategy := ""
	if frozenProfile != nil {
		restoredStrategy = stringOrPy(frozenProfile["effective_strategy"])
	}
	switch restoredStrategy {
	case "small_full", "large_flat", "indexed", "indexed_stale":
		tk.effectiveStrategy = restoredStrategy
	default:
		tk.effectiveStrategy = ResolveSkillRetrievalStrategy(
			true, tk.candidateScale,
			environment.Artifact().Layout, environment.Artifact().IndexState)
	}

	// 步骤 12：索引工具挂载（:410-414）
	indexToolkit, err := discovery.NewInstalledSkillsDirectoryToolkit(
		environment, tk.sessionScope, tk.settings.IncrementalNoticeMaxChars)
	if err != nil {
		return nil, err
	}
	tk.indexToolkit = indexToolkit

	// 步骤 13：自动后台构建判定（:415-436，D-16-4-5 ⤴️16.5）
	tk.maybeAutoBuild(opts.AutoBuildIndex, configuredSettings, initialRecords, resolvedArtifactRoot)
	return tk, nil
}

// ──────────────────────────── 导出方法 ────────────────────────────

// Environment 状态环境（提示 rail 共用）。
// 对齐 environment property。
func (t *SkillRetrievalToolkit) Environment() *discovery.SkillFS { return t.environment }

// Settings 当前生效设置。
func (t *SkillRetrievalToolkit) Settings() discovery.DiscoverySettings { return t.settings }

// SessionScope 会话标识。
func (t *SkillRetrievalToolkit) SessionScope() string { return t.sessionScope }

// IndexEnabled 索引消费开关。
func (t *SkillRetrievalToolkit) IndexEnabled() bool { return t.indexEnabled }

// CandidateScale 候选规模（small|large）。
func (t *SkillRetrievalToolkit) CandidateScale() string { return t.candidateScale }

// EstimatedCandidateTokens 估 token。
func (t *SkillRetrievalToolkit) EstimatedCandidateTokens() int { return t.estimatedCandidateTokens }

// CandidateBudgetTokens 预算 token。
func (t *SkillRetrievalToolkit) CandidateBudgetTokens() int { return t.candidateBudgetTokens }

// EffectiveStrategy 有效策略四态。
func (t *SkillRetrievalToolkit) EffectiveStrategy() string { return t.effectiveStrategy }

// FrozenPromptSnapshot 构造期捕获的元数据快照。
func (t *SkillRetrievalToolkit) FrozenPromptSnapshot() discovery.SkillPromptSnapshot {
	return t.frozenPromptSnapshot
}

// FrozenSelectionCards 冻结选择卡副本。
func (t *SkillRetrievalToolkit) FrozenSelectionCards() map[string]map[string]string {
	out := map[string]map[string]string{}
	for k, v := range t.frozenSelectionCards {
		out[k] = v
	}
	return out
}

// AutoBuildResult 后台构建结果（D-16-4-5：⤴️16.5 前恒 nil）。
func (t *SkillRetrievalToolkit) AutoBuildResult() map[string]any { return t.autoBuildResult }

// SessionProfile 有界冻结 profile（状态查询与持久化供体）。
// 对齐 session_profile（skill_retrieval_toolkits.py:538-560）。
func (t *SkillRetrievalToolkit) SessionProfile() map[string]any {
	artifact := t.environment.Artifact()
	revision := ""
	if pinned := t.environment.PinnedIndexSnapshot(); pinned != nil {
		revision = pinned.Fingerprint
	}
	return map[string]any{
		"schema_version":             sessionProfileVersion,
		"session_id":                 t.sessionScope,
		"index_enabled":              t.indexEnabled,
		"candidate_scale":            t.candidateScale,
		"estimated_candidate_tokens": t.estimatedCandidateTokens,
		"candidate_budget_tokens":    t.candidateBudgetTokens,
		"effective_strategy":         t.effectiveStrategy,
		"layout":                     artifact.Layout,
		"index_state":                t.frozenPromptSnapshot.IndexState,
		"pinned_index_revision":      revision,
		"searchable_count":           len(t.environment.SelectionCards(false)),
		"settings":                   settingsAsDict(t.settings),
		"selection_cards":            t.FrozenSelectionCards(),
		"prompt_snapshot":            snapshotAsDict(t.frozenPromptSnapshot),
		"index_snapshot":             indexSnapshotAsAny(t.environment.PinnedIndexSnapshot()),
	}
}

// CurrentRecords live 扫描记录。
func (t *SkillRetrievalToolkit) CurrentRecords() ([]discovery.SkillRecord, error) {
	return t.currentRecords()
}

// CurrentIndexRecords 共享 taxonomy 许可的稳定清单。
func (t *SkillRetrievalToolkit) CurrentIndexRecords() ([]discovery.SkillRecord, error) {
	if t.indexSkillDirectories == nil {
		return t.currentRecords()
	}
	return t.scanRecords(t.indexSkillDirectories)
}

// SkillIndex 经与 Tool 相同的结构化目录实现执行。
func (t *SkillRetrievalToolkit) SkillIndex(arguments map[string]any) (discovery.SkillDCICommandResult, error) {
	return t.indexToolkit.SkillIndex(arguments)
}

// GetTools 工具暴露列表。
func (t *SkillRetrievalToolkit) GetTools() []discovery.ToolEntry {
	return t.indexToolkit.GetTools()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// currentRecords live 目录扫描（_scan_records 对 live 目录的特化）。
func (t *SkillRetrievalToolkit) currentRecords() ([]discovery.SkillRecord, error) {
	return t.scanRecords(t.skillDirectories)
}

// currentRecordsInv RecordsProvider 形态（SkillFS 回调）。
func (t *SkillRetrievalToolkit) currentRecordsInv() (discovery.SkillInventory, error) {
	records, err := t.currentRecords()
	if err != nil {
		return discovery.SkillInventory{}, err
	}
	return discovery.InventoryFromRecords(records), nil
}

// scanRecords 目录+禁用+来源三回调展开后扫描。
// 对齐 _scan_records（skill_retrieval_toolkits.py:563-590）。
func (t *SkillRetrievalToolkit) scanRecords(directoriesProvider SkillDirectoriesProvider) ([]discovery.SkillRecord, error) {
	var directories []string
	if directoriesProvider != nil {
		directories = directoriesProvider()
	}
	var disabled []string
	if t.disabledSkills != nil {
		disabled = t.disabledSkills()
	}
	var sources map[string]string
	if t.sourceByName != nil {
		sources = t.sourceByName()
	}
	if sources == nil {
		sources = map[string]string{}
	}
	inv, err := discovery.ScanSkillDirectories(directories, disabled, sources)
	if err != nil {
		return nil, err
	}
	return inv.Items, nil
}

// maybeAutoBuild 自动构建判定（对齐 :415-436；构建本体 ⤴️16.5）。
func (t *SkillRetrievalToolkit) maybeAutoBuild(autoBuildIndex bool, configuredSettings discovery.DiscoverySettings, initialRecords []discovery.SkillRecord, artifactRoot string) {
	shouldAutoBuild := autoBuildIndex && IsSkillRetrievalEnabled(t.configBase)
	shouldAutoBuild = shouldAutoBuild && t.indexEnabled
	if !shouldAutoBuild || t.candidateScale != "large" {
		return
	}
	// MCP 捆绑技能参与 1% 判定与 live SkillFS，但不进稳定全局清单：
	// 共享索引新鲜度只对专用本地清单判定（Python 注释语义）。
	var indexRecords []discovery.SkillRecord
	if t.indexSkillDirectories == nil {
		indexRecords = initialRecords
	} else {
		indexRecords, _ = t.CurrentIndexRecords()
	}
	indexProbe, err := discovery.NewSkillFS(sessionRecordsFn(indexRecords), discovery.SkillFSOptions{
		Settings:          func() discovery.DiscoverySettings { s := configuredSettings; s.UseExistingIndex = true; return s }(),
		VisibleSkillNames: nil,
		ArtifactRoot:      artifactRoot,
		IndexRoot:         artifactRoot,
		PinIndexRevision:  true,
	})
	if err != nil {
		autoBuildWarn(err)
		return
	}
	state := indexProbe.Artifact().IndexState
	if state == "missing" || state == "stale" {
		t.startBackgroundIndexBuild(artifactRoot)
	}
}

// startBackgroundIndexBuild 后台构建（Python _start_background_index_build；
// SkillTaxonomyRuntime ⤴️16.5——当前恒走 import 失败等价分支：日志告警，
// 会话保持冻结降级，auto_build_result 保持 nil）。
func (t *SkillRetrievalToolkit) startBackgroundIndexBuild(artifactRoot string) {
	autoBuildWarn(nil)
}

// sessionRecordsFn 静态记录集 → RecordsProvider。
func sessionRecordsFn(records []discovery.SkillRecord) discovery.RecordsProvider {
	return func() (discovery.SkillInventory, error) {
		return discovery.InventoryFromRecords(records), nil
	}
}

// invalidProfileWarn 非法 profile 告警（对齐 :303-305 日志）。
func invalidProfileWarn() {
	loggerWarn("Ignoring invalid persisted Skill retrieval profile")
}

// autoBuildWarn 构建失败告警（对齐 :620-625 日志文案）。
func autoBuildWarn(err error) {
	loggerWarn("Unable to start the enabled Skill taxonomy build; the session remains on its frozen fallback")
}

// loggerWarn 工具级告警日志。
func loggerWarn(message string) {
	logger.Info(logger.ComponentAgentServer).Msg(message)
}

// retrievalConfig symphony.skill_retrieval 子映射。
func retrievalConfig(configBase map[string]any) map[string]any {
	cfg := configOf(configBase)
	symphony, _ := cfg["symphony"].(map[string]any)
	if symphony == nil {
		return map[string]any{}
	}
	retrieval, _ := symphony["skill_retrieval"].(map[string]any)
	if retrieval == nil {
		return map[string]any{}
	}
	return retrieval
}

// configOf 配置回落。Python get_config() 全局兜底在 Go 侧无全局单例：
// 调用方（DeepAdapter/工厂）恒传 configBase（对齐 Python 全部调用点形态），
// nil 回落空表（开关默认关，与 Python get_config() 异常空表分支一致）。
func configOf(configBase map[string]any) map[string]any {
	if configBase != nil {
		return configBase
	}
	return map[string]any{}
}

// envSwitch 环境开关三态（Python _env_switch）。
func envSwitch(name string) *bool {
	value, present := os.LookupEnv(name)
	if !present || strings.TrimSpace(value) == "" {
		return nil
	}
	result := trueValues[strings.ToLower(strings.TrimSpace(value))]
	return &result
}

// expandUser ~ 展开（对齐 Path.expanduser）。
func expandUser(path string) string {
	if strings.HasPrefix(path, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[1:])
		}
	}
	return path
}

// deepCopyMap 配置深拷贝（JSON 往返，与 Python deepcopy 等价的值语义）。
func deepCopyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	data, err := json.Marshal(in)
	if err != nil {
		return in
	}
	out := map[string]any{}
	if err := json.Unmarshal(data, &out); err != nil {
		return in
	}
	return out
}

// settingsAsDict DiscoverySettings → snake_case dict（对齐 dataclasses.asdict）。
func settingsAsDict(s discovery.DiscoverySettings) map[string]any {
	preferred := make([]any, 0, len(s.PromptPreferredSkills))
	for _, v := range s.PromptPreferredSkills {
		preferred = append(preferred, v)
	}
	return map[string]any{
		"candidate_budget_ratio":       s.CandidateBudgetRatio,
		"context_window_tokens":        s.ContextWindowTokens,
		"max_output_chars":             s.MaxOutputChars,
		"max_results":                  s.MaxResults,
		"max_list_entries":             s.MaxListEntries,
		"incremental_notice_max_chars": s.IncrementalNoticeMaxChars,
		"use_existing_index":           s.UseExistingIndex,
		"prompt_preferred_skills":      preferred,
	}
}

// snapshotAsDict SkillPromptSnapshot → snake_case dict。
func snapshotAsDict(s discovery.SkillPromptSnapshot) map[string]any {
	entries := make([]any, 0, len(s.Entries))
	for _, e := range s.Entries {
		entries = append(entries, map[string]any{
			"worker_id": e.WorkerID, "description": e.Description, "source": e.Source,
		})
	}
	branches := make([]any, 0, len(s.Branches))
	for _, b := range s.Branches {
		branches = append(branches, map[string]any{
			"path": b.Path, "label": b.Label, "description": b.Description,
		})
	}
	return map[string]any{
		"mode": s.Mode, "total_count": s.TotalCount, "entries": entries,
		"estimated_candidate_tokens": s.EstimatedCandidateTokens,
		"candidate_budget_tokens":    s.CandidateBudgetTokens,
		"index_state":                s.IndexState, "branches": branches,
		"omitted_branch_count": s.OmittedBranchCount,
	}
}

// indexSnapshotAsAny 索引快照三态序列化。
func indexSnapshotAsAny(s *discovery.SkillIndexSnapshot) any {
	if s == nil {
		return nil
	}
	return s.ToDict()
}

// sameScalarType 同型判定（Python type(a) is not type(b)）。[Go 适配]
// JSON 往返把全部数字化为 float64（Python json 保留 int），数值类合并一档，
// 其余类型严格同型。
func sameScalarType(a, b any) bool {
	isNum := func(v any) bool {
		switch v.(type) {
		case int, int64, float64:
			return true
		}
		return false
	}
	switch a.(type) {
	case bool:
		_, ok := b.(bool)
		return ok
	case int, int64, float64:
		return isNum(b)
	case string:
		_, ok := b.(string)
		return ok
	case []any:
		_, ok := b.([]any)
		return ok
	case map[string]any:
		_, ok := b.(map[string]any)
		return ok
	}
	return false
}

// scalarEqual 标量/列表/映射值等价。
func scalarEqual(a, b any) bool {
	switch av := a.(type) {
	case bool, int, int64, float64, string:
		return fmt.Sprint(a) == fmt.Sprint(b)
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !scalarEqual(av[i], bv[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for k, v := range av {
			other, ok := bv[k]
			if !ok || !scalarEqual(v, other) {
				return false
			}
		}
		return true
	}
	return false
}

// stringOrPy 字符串化（None→""）。
func stringOrPy(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

// orStr 首个非空字符串。
func orStr(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// orAny 首个非 nil。
func orAny(values ...any) any {
	for _, v := range values {
		if v != nil {
			return v
		}
	}
	return nil
}

// boolOfPy 布尔化回落。
func boolOfPy(value any, def bool) bool {
	if v, ok := value.(bool); ok {
		return v
	}
	return def
}

// intOfPy 整数化（Python int() 语义：int/float 整值/整数字符串）。
func intOfPy(value any) (int, error) {
	switch v := value.(type) {
	case int:
		return v, nil
	case int64:
		return int(v), nil
	case float64:
		if v != float64(int64(v)) {
			return 0, fmt.Errorf("not integral: %v", v)
		}
		return int(v), nil
	case string:
		var out int
		if _, err := fmt.Sscanf(strings.TrimSpace(v), "%d", &out); err != nil {
			return 0, err
		}
		return out, nil
	}
	return 0, fmt.Errorf("not an int: %T", value)
}

// asAnyMap any → map[string]any（nil 安全）。
func asAnyMap(value any) map[string]any {
	if m, ok := value.(map[string]any); ok {
		return m
	}
	return nil
}
