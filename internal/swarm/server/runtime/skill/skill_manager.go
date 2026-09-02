package skill

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SkillManager 技能管理器，对应 skills.* 请求方法
// 对应 Python: jiuwenswarm/server/runtime/skill/skill_manager.py SkillManager
type SkillManager struct {
	// mu 状态读写锁
	mu sync.RWMutex
	// agentRoot Agent 根目录
	agentRoot string
	// skillsDir 技能目录
	skillsDir string
	// marketplaceDir marketplace 目录
	marketplaceDir string
	// stateFile 状态文件路径
	stateFile string
	// state 内存状态
	state map[string]any
	// skillnetInstallJobs SkillNet 异步安装任务
	skillnetInstallJobs map[string]map[string]any
	// skillnetInstallCompleteHook 安装成功落盘后回调
	skillnetInstallCompleteHook func(ctx context.Context) error
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// evolutionFilename 演化记录文件名
	evolutionFilename = "evolutions.json"
	// skillnetDownloadTimeoutEnv SkillNet 下载超时环境变量
	skillnetDownloadTimeoutEnv = "SKILLNET_DOWNLOAD_TIMEOUT"
	// skillnetMaxRetriesEnv SkillNet 最大重试环境变量
	skillnetMaxRetriesEnv = "SKILLNET_MAX_RETRIES"
	// clawhubTokenKey ClawHub token 在 state 中的键
	clawhubTokenKey = "clawhub_token"
	// teamSkillsHubBaseURLEnv TeamSkillsHub 基础 URL 环境变量
	teamSkillsHubBaseURLEnv = "TEAM_SKILLS_HUB_BASE_URL"
	// teamSkillsHubTimeoutEnv TeamSkillsHub 超时环境变量
	teamSkillsHubTimeoutEnv = "TEAM_SKILLS_HUB_TIMEOUT"
	// teamSkillsHubAllowedHostsEnv TeamSkillsHub 下载白名单环境变量
	teamSkillsHubAllowedHostsEnv = "TEAM_SKILLS_HUB_ALLOWED_DOWNLOAD_HOSTS"
	// teamSkillsHubDefaultBaseURL TeamSkillsHub 默认基础 URL
	teamSkillsHubDefaultBaseURL = "https://teamskills.openjiuwen.com"
	// teamSkillsHubDefaultTimeout TeamSkillsHub 默认超时秒数
	teamSkillsHubDefaultTimeout = 60
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// errNotImplemented 后续补充方法未实现错误
	errNotImplemented = errors.New("功能尚未实现")
	// skillnetDownloadTimeout SkillNet 下载超时秒数
	skillnetDownloadTimeout = envInt(skillnetDownloadTimeoutEnv, 60)
	// skillnetMaxRetries SkillNet 最大重试次数
	skillnetMaxRetries = envInt(skillnetMaxRetriesEnv, 3)
	// teamSkillsHubAllowedHostDefaults TeamSkillsHub 下载白名单默认值
	teamSkillsHubAllowedHostDefaults = []string{
		"openjiuwen-market.obs.*.myhuaweicloud.com",
		"127.0.0.1",
		"localhost",
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSkillManager 创建新的 SkillManager 实例
// 对应 Python: SkillManager.__init__(workspace_dir)
func NewSkillManager(workspaceDir string) *SkillManager {
	sm := &SkillManager{
		skillnetInstallJobs: make(map[string]map[string]any),
	}

	if workspaceDir != "" {
		sm.agentRoot = workspaceDir
		sm.skillsDir = filepath.Join(workspaceDir, "skills")
		sm.marketplaceDir = filepath.Join(sm.skillsDir, "_marketplace")
		sm.stateFile = filepath.Join(sm.skillsDir, "skills_state.json")
	} else {
		sm.agentRoot = workspace.AgentRootDir()
		sm.skillsDir = workspace.AgentSkillsDir()
		sm.marketplaceDir = filepath.Join(sm.skillsDir, "_marketplace")
		sm.stateFile = GetStateFile()
	}

	// 确保技能目录存在
	if err := os.MkdirAll(sm.skillsDir, 0o755); err != nil {
		logger.Warn(logComponent).Err(err).Str("path", sm.skillsDir).Msg("创建技能目录失败")
	}

	sm.state = sm.loadState()
	return sm
}

// SetSkillnetInstallCompleteHook 设置安装成功落盘后回调（通常为重载 Agent 实例）
// 对应 Python: SkillManager.set_skillnet_install_complete_hook(hook)
func (sm *SkillManager) SetSkillnetInstallCompleteHook(hook func(ctx context.Context) error) {
	sm.skillnetInstallCompleteHook = hook
}

// HasPendingSkillnetInstall 检查是否有 pending 的 SkillNet 安装任务。
//
// 对齐 Python: SkillManager 中检查 _skillnet_install_jobs 是否有 pending 状态的任务
func (sm *SkillManager) HasPendingSkillnetInstall() bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	for _, job := range sm.skillnetInstallJobs {
		if toString(job["status"]) == "pending" {
			return true
		}
	}
	return false
}

// HandleSkillsList 返回所有可用 skill（本地 + marketplace 中未安装的）
// 对应 Python: SkillManager.handle_skills_list(params)
func (sm *SkillManager) HandleSkillsList(ctx context.Context, params map[string]any) (map[string]any, error) {
	refreshMarketplaces := toBool(params["refresh_marketplaces"])
	if refreshMarketplaces {
		if err := sm.syncMarketplaceRepos(ctx); err != nil {
			logger.Warn(logComponent).Err(err).Msg("同步 marketplace 仓库失败")
		}
	}

	local := sm.scanLocalSkills()
	builtin := sm.scanBuiltinSkills()
	marketplace := sm.scanMarketplaceSkills()

	out := map[string]any{
		"skills": append(append(local, builtin...), marketplace...),
	}

	if toBool(params["with_installed"]) {
		installed, _ := sm.HandleSkillsInstalled(ctx, params)
		if plugins, ok := installed["plugins"]; ok {
			out["plugins"] = plugins
		} else {
			out["plugins"] = []any{}
		}
	}
	return out, nil
}

// HandleSkillsInstalled 返回已安装的 marketplace 插件列表
// 对应 Python: SkillManager.handle_skills_installed(params)
func (sm *SkillManager) HandleSkillsInstalled(ctx context.Context, params map[string]any) (map[string]any, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	rawPlugins := sm.GetInstalledPlugins()
	plugins := make([]map[string]any, 0, len(rawPlugins))
	for _, p := range rawPlugins {
		p = sm.normalizePlugin(p)
		name := toString(p["name"])
		marketplace := toString(p["marketplace"])
		spec := name
		if marketplace != "" {
			spec = name + "@" + marketplace
		}
		plugin := map[string]any{
			"plugin_name":  name,
			"marketplace":  marketplace,
			"spec":         spec,
			"version":      toString(p["version"]),
			"installed_at": toString(p["installed_at"]),
			"git_commit":   toString(p["commit"]),
			"enabled":      GetSkillEnabled(sm.state, name),
			"skills":       []string{name},
		}
		plugins = append(plugins, plugin)
	}
	return map[string]any{"plugins": plugins}, nil
}

// HandleSkillsGet 获取单个 skill 详情
// 对应 Python: SkillManager.handle_skills_get(params)
func (sm *SkillManager) HandleSkillsGet(ctx context.Context, params map[string]any) (map[string]any, error) {
	name := toString(params["name"])
	if name == "" {
		return nil, fmt.Errorf("缺少参数: name")
	}

	sm.mu.RLock()
	defer sm.mu.RUnlock()

	// 在本地 skills 目录中查找
	meta, err := sm.findSkillInDir(sm.skillsDir, name, "")
	if err == nil {
		return meta, nil
	}

	// 在 marketplace 目录中查找
	if dirExists(sm.marketplaceDir) {
		entries, _ := os.ReadDir(sm.marketplaceDir)
		for _, repoEntry := range entries {
			if !repoEntry.IsDir() {
				continue
			}
			repoDir := filepath.Join(sm.marketplaceDir, repoEntry.Name())
			meta, err = sm.findSkillInDir(repoDir, name, repoEntry.Name())
			if err == nil {
				return meta, nil
			}
		}
	}

	return nil, fmt.Errorf("未找到 skill: %s", name)
}

// SkillsDir 返回技能目录路径
func (sm *SkillManager) SkillsDir() string {
	return sm.skillsDir
}

// HandleSkillsToggle 切换已安装本地 skill 的 enabled 状态
// 对应 Python: SkillManager.handle_skills_toggle(params)
func (sm *SkillManager) HandleSkillsToggle(ctx context.Context, params map[string]any) (map[string]any, error) {
	name := toString(params["name"])
	enabledVal, hasEnabled := params["enabled"]

	if name == "" {
		return map[string]any{"success": false, "detail": "缺少参数: name"}, nil
	}
	if !hasEnabled {
		return map[string]any{"success": false, "detail": "缺少参数: enabled (bool)"}, nil
	}
	enabled := toBool(enabledVal)

	safeName, err := safePathName(name, "skill")
	if err != nil {
		logRejectedName("skills.toggle", "skill", name, err)
		return map[string]any{"success": false, "detail": err.Error()}, nil
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	SetSkillEnabled(sm.state, safeName, enabled)
	sm.saveState()

	return map[string]any{
		"success": true,
		"name":    safeName,
		"enabled": enabled,
		"config":  map[string]any{"enabled": enabled},
		"detail":  "配置已更新；下次 reload / rebuild / 新会话后执行面生效。",
	}, nil
}

// HandleSkillsEvolutionStatus 检查某个 skill 是否存在 evolutions.json
// 对应 Python: SkillManager.handle_skills_evolution_status(params)
func (sm *SkillManager) HandleSkillsEvolutionStatus(ctx context.Context, params map[string]any) (map[string]any, error) {
	name := trimSpace(toString(params["name"]))
	if name == "" {
		return nil, fmt.Errorf("缺少参数: name")
	}
	safeName, err := safePathName(name, "skill")
	if err != nil {
		logRejectedName("skills.evolution.status", "skill", name, err)
		return nil, fmt.Errorf("%s", err.Error())
	}

	sm.mu.RLock()
	defer sm.mu.RUnlock()

	evoPath := sm.getSkillEvolutionPath(safeName)
	exists := evoPath != "" && fileExists(evoPath)
	return map[string]any{"name": safeName, "exists": exists}, nil
}

// HandleSkillsEvolutionGet 获取某个 skill 的 evolutions.json 内容
// 对应 Python: SkillManager.handle_skills_evolution_get(params)
func (sm *SkillManager) HandleSkillsEvolutionGet(ctx context.Context, params map[string]any) (map[string]any, error) {
	name := trimSpace(toString(params["name"]))
	if name == "" {
		return nil, fmt.Errorf("缺少参数: name")
	}
	safeName, err := safePathName(name, "skill")
	if err != nil {
		logRejectedName("skills.evolution.get", "skill", name, err)
		return nil, fmt.Errorf("%s", err.Error())
	}

	sm.mu.RLock()
	defer sm.mu.RUnlock()

	evoPath := sm.getSkillEvolutionPath(safeName)
	if evoPath == "" || !fileExists(evoPath) {
		return map[string]any{
			"name":       safeName,
			"exists":     false,
			"valid":      true,
			"skill_id":   safeName,
			"version":    "1.0.0",
			"updated_at": "",
			"entries":    []any{},
		}, nil
	}

	data, err := os.ReadFile(evoPath)
	if err != nil {
		logger.Warn(logComponent).Str("skill", safeName).Err(err).Msg("读取 evolutions.json 失败")
		return map[string]any{
			"name":       safeName,
			"exists":     true,
			"valid":      false,
			"detail":     "evolutions.json 格式错误或读取失败",
			"skill_id":   safeName,
			"version":    "1.0.0",
			"updated_at": "",
			"entries":    []any{},
		}, nil
	}

	var evoData map[string]any
	if err := json.Unmarshal(data, &evoData); err != nil {
		logger.Warn(logComponent).Str("skill", safeName).Err(err).Msg("解析 evolutions.json 失败")
		return map[string]any{
			"name":       safeName,
			"exists":     true,
			"valid":      false,
			"detail":     "evolutions.json 格式错误或读取失败",
			"skill_id":   safeName,
			"version":    "1.0.0",
			"updated_at": "",
			"entries":    []any{},
		}, nil
	}

	result := map[string]any{
		"name":   safeName,
		"exists": true,
		"valid":  true,
	}
	for k, v := range evoData {
		result[k] = v
	}
	return result, nil
}

// HandleSkillsEvolutionSave 保存某个 skill 的 evolutions.json 条目列表
// 对应 Python: SkillManager.handle_skills_evolution_save(params)
func (sm *SkillManager) HandleSkillsEvolutionSave(ctx context.Context, params map[string]any) (map[string]any, error) {
	name := trimSpace(toString(params["name"]))
	if name == "" {
		return nil, fmt.Errorf("缺少参数: name")
	}
	safeName, err := safePathName(name, "skill")
	if err != nil {
		logRejectedName("skills.evolution.save", "skill", name, err)
		return nil, fmt.Errorf("%s", err.Error())
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.resolveLocalSkillDir(safeName) == "" {
		return nil, fmt.Errorf("未找到 skill: %s", safeName)
	}

	entriesVal, ok := params["entries"]
	if !ok {
		return nil, fmt.Errorf("参数 entries 必须是数组")
	}
	entries, ok := toSliceOfAny(entriesVal)
	if !ok {
		return nil, fmt.Errorf("参数 entries 必须是数组")
	}

	normalizedEntries := make([]map[string]any, 0, len(entries))
	for idx, item := range entries {
		itemMap, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("entries[%d] 必须是对象", idx)
		}
		entryID := trimSpace(toString(itemMap["id"]))
		if entryID == "" {
			return nil, fmt.Errorf("entries[%d].id 不能为空", idx)
		}
		change, _ := itemMap["change"].(map[string]any)
		if change == nil {
			return nil, fmt.Errorf("entries[%d].change.content 必须是字符串", idx)
		}
		content, _ := change["content"].(string)
		if content == "" {
			return nil, fmt.Errorf("entries[%d].change.content 必须是字符串", idx)
		}
		normalizedEntries = append(normalizedEntries, itemMap)
	}

	evoPath := sm.getSkillEvolutionPath(safeName)
	if evoPath == "" {
		return nil, fmt.Errorf("未找到 skill: %s", safeName)
	}

	// 读取已有文件或创建新的
	evoFile := map[string]any{
		"skill_id":   safeName,
		"version":    "1.0.0",
		"entries":    []any{},
		"updated_at": time.Now().UTC().Format(time.RFC3339),
	}
	if fileExists(evoPath) {
		data, err := os.ReadFile(evoPath)
		if err == nil {
			var existing map[string]any
			if json.Unmarshal(data, &existing) == nil {
				evoFile = existing
			}
		}
	}

	evoFile["entries"] = normalizedEntries
	evoFile["updated_at"] = time.Now().UTC().Format(time.RFC3339)
	if evoFile["skill_id"] == nil || evoFile["skill_id"] == "" {
		evoFile["skill_id"] = safeName
	}

	if err := os.MkdirAll(filepath.Dir(evoPath), 0o755); err != nil {
		return nil, fmt.Errorf("创建目录失败: %w", err)
	}
	data, _ := json.MarshalIndent(evoFile, "", "  ")
	if err := os.WriteFile(evoPath, data, 0o644); err != nil {
		return nil, fmt.Errorf("写入 evolutions.json 失败: %w", err)
	}

	return map[string]any{
		"success":     true,
		"name":        safeName,
		"entry_count": len(normalizedEntries),
		"updated_at":  evoFile["updated_at"],
	}, nil
}

// HandleSkillsMarketplaceList 列出已配置的 marketplace 源
// 对应 Python: SkillManager.handle_skills_marketplace_list(params)
func (sm *SkillManager) HandleSkillsMarketplaceList(ctx context.Context, params map[string]any) (map[string]any, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	marketplaces := sm.getMarketplaces()
	result := make([]map[string]any, 0, len(marketplaces))
	for _, m := range marketplaces {
		item := map[string]any{
			"name":    toString(m["name"]),
			"url":     toString(m["url"]),
			"enabled": toBoolWithDefault(m["enabled"], true),
		}
		if v, ok := m["install_location"]; ok {
			item["install_location"] = v
		}
		if v, ok := m["last_updated"]; ok {
			item["last_updated"] = v
		}
		result = append(result, item)
	}
	return map[string]any{"marketplaces": result}, nil
}

// HandleSkillsInstall 安装 marketplace 中的 skill
// 对应 Python: SkillManager.handle_skills_install(params)
func (sm *SkillManager) HandleSkillsInstall(ctx context.Context, params map[string]any) (map[string]any, error) {
	spec := toString(params["spec"])
	force := toBool(params["force"])

	if spec == "" {
		return map[string]any{"success": false, "detail": "缺少参数: spec"}, nil
	}

	if !strings.Contains(spec, "@") {
		safeName, err := safePathName(spec, "skill")
		if err != nil {
			logRejectedName("skills.install", "skill", spec, err)
			return map[string]any{"success": false, "detail": err.Error()}, nil
		}
		builtinDir := getBuiltinSkillsDir()
		builtinPath := filepath.Join(builtinDir, safeName)
		if dirExists(builtinPath) {
			return sm.HandleSkillsInstallBuiltin(ctx, map[string]any{"name": safeName})
		}
		return map[string]any{"success": false, "detail": "spec 格式应为 skill@marketplace，内置技能可直接使用名称安装"}, nil
	}

	lastAt := strings.LastIndex(spec, "@")
	pluginName := spec[:lastAt]
	marketplaceName := spec[lastAt+1:]

	if pluginName == "" || marketplaceName == "" {
		return map[string]any{"success": false, "detail": "plugin 或 marketplace 名称为空"}, nil
	}

	safePlugin, err := safePathName(pluginName, "plugin")
	if err != nil {
		logRejectedName("skills.install", "plugin/marketplace", spec, err)
		return map[string]any{"success": false, "detail": err.Error()}, nil
	}
	safeMarket, err := safePathName(marketplaceName, "marketplace")
	if err != nil {
		logRejectedName("skills.install", "plugin/marketplace", spec, err)
		return map[string]any{"success": false, "detail": err.Error()}, nil
	}

	if safeMarket == "builtin" {
		return sm.HandleSkillsInstallBuiltin(ctx, map[string]any{"name": safePlugin})
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	// 查找 marketplace 配置
	var marketplace map[string]any
	for _, m := range sm.getMarketplaces() {
		if toString(m["name"]) == safeMarket {
			marketplace = m
			break
		}
	}
	if marketplace == nil {
		return map[string]any{"success": false, "detail": fmt.Sprintf("未找到 marketplace: %s", safeMarket)}, nil
	}

	gitURL := toString(marketplace["url"])
	if gitURL == "" {
		return map[string]any{"success": false, "detail": fmt.Sprintf("marketplace %s 缺少 url", safeMarket)}, nil
	}

	// marketplace 安装逻辑（git clone + copy）
	repoDir := filepath.Join(sm.marketplaceDir, safeMarket)
	if !dirExists(repoDir) {
		if err := sm.gitClone(ctx, gitURL, repoDir); err != nil {
			return map[string]any{"success": false, "detail": fmt.Sprintf("git clone 失败: %s", gitURL)}, nil
		}
	} else {
		sm.gitPull(ctx, repoDir)
	}

	// 在仓库中查找 plugin 目录
	pluginSrc := filepath.Join(repoDir, "skills", safePlugin)
	if !dirExists(pluginSrc) {
		pluginSrc = repoDir // 兼容单 skill 模式
	}
	if !dirExists(pluginSrc) {
		return map[string]any{"success": false, "detail": fmt.Sprintf("在 marketplace 仓库中未找到 plugin: %s", safePlugin)}, nil
	}

	mdPath := sm.tryFindSkillFile(pluginSrc)
	if mdPath == "" {
		return map[string]any{"success": false, "detail": fmt.Sprintf("plugin %s 缺少 SKILL.md", safePlugin)}, nil
	}

	// 复制到本地 skills 目录
	dest := filepath.Join(sm.skillsDir, safePlugin)
	if dirExists(dest) {
		if !force {
			return map[string]any{"success": false, "detail": fmt.Sprintf("skill %s 已存在", safePlugin)}, nil
		}
		if err := safeRmtree(dest); err != nil {
			logger.Warn(logComponent).Err(err).Str("path", dest).Msg("移除已存在技能目录失败")
		}
	}
	if err := copyDir(pluginSrc, dest); err != nil {
		return map[string]any{"success": false, "detail": fmt.Sprintf("安装失败: %s", err)}, nil
	}

	// 解析元数据并记录
	meta := sm.parseSkillMD(sm.tryFindSkillFile(dest))
	commitHash := sm.gitGetCommit(repoDir)
	sm.AddInstalledPlugin(map[string]any{
		"name":         safePlugin,
		"marketplace":  safeMarket,
		"version":      toString(meta["version"]),
		"commit":       commitHash,
		"source":       safeMarket,
		"installed_at": time.Now().UTC().Format(time.RFC3339),
	})
	sm.refreshAgentDataIndexes()
	// saveState 已在 AddInstalledPlugin 内调用，此处不再冗余（对齐 Python: _add_installed_plugin 自身 _save_state）

	return map[string]any{"success": true}, nil
}

// HandleSkillsInstallBuiltin 安装内置技能
// 对应 Python: SkillManager.handle_skills_install_builtin(params)
func (sm *SkillManager) HandleSkillsInstallBuiltin(ctx context.Context, params map[string]any) (map[string]any, error) {
	name := toString(params["name"])
	if name == "" {
		return map[string]any{"success": false, "detail": "缺少参数: name"}, nil
	}
	safeName, err := safePathName(name, "skill")
	if err != nil {
		logRejectedName("skills.install_builtin", "skill", name, err)
		return map[string]any{"success": false, "detail": err.Error()}, nil
	}

	builtinDir := getBuiltinSkillsDir()
	if !dirExists(builtinDir) {
		return map[string]any{"success": false, "detail": "内置技能目录不存在"}, nil
	}

	src := filepath.Join(builtinDir, safeName)
	if !dirExists(src) {
		return map[string]any{"success": false, "detail": fmt.Sprintf("未找到内置技能: %s", safeName)}, nil
	}

	dest := filepath.Join(sm.skillsDir, safeName)
	if dirExists(dest) {
		return map[string]any{"success": false, "detail": fmt.Sprintf("技能 %s 已经安装", safeName)}, nil
	}

	if err := copyDir(src, dest); err != nil {
		logger.Error(logComponent).Err(err).Msg("安装内置技能失败")
		return map[string]any{"success": false, "detail": fmt.Sprintf("安装失败: %s", err)}, nil
	}

	meta := sm.parseSkillMD(sm.tryFindSkillFile(dest))
	sm.mu.Lock()
	sm.AddInstalledPlugin(map[string]any{
		"name":         safeName,
		"marketplace":  "builtin",
		"version":      toString(meta["version"]),
		"commit":       "",
		"source":       "builtin",
		"installed_at": time.Now().UTC().Format(time.RFC3339),
	})
	sm.refreshAgentDataIndexes()
	// saveState 已在 AddInstalledPlugin 内调用，此处不再冗余
	sm.mu.Unlock()

	return map[string]any{"success": true}, nil
}

// HandleSkillsUninstall 卸载技能
// 对应 Python: SkillManager.handle_skills_uninstall(params)
func (sm *SkillManager) HandleSkillsUninstall(ctx context.Context, params map[string]any) (map[string]any, error) {
	name := toString(params["name"])
	if name == "" {
		return map[string]any{"success": false, "detail": "缺少参数: name"}, nil
	}
	safeName, err := safePathName(name, "skill")
	if err != nil {
		logRejectedName("skills.uninstall", "skill", name, err)
		return map[string]any{"success": false, "detail": err.Error()}, nil
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	dest := filepath.Join(sm.skillsDir, safeName)
	if !dirExists(dest) {
		return map[string]any{"success": false, "detail": fmt.Sprintf("未找到技能: %s", safeName)}, nil
	}

	// 内置技能保护（对齐 Python: 检查 builtin 目录，不允许删除内置技能）
	builtinDir := sm.getBuiltinSkillsDir()
	if builtinDir != "" {
		isBuiltin, builtinPath := sm.isBuiltinSkill(safeName, builtinDir)
		if isBuiltin {
			destAbs, _ := filepath.Abs(dest)
			builtinAbs, _ := filepath.Abs(builtinPath)
			if destAbs == builtinAbs {
				return map[string]any{"success": false, "detail": "内置技能不允许删除"}, nil
			}
		}
	}

	if err := safeRmtree(dest); err != nil {
		return map[string]any{"success": false, "detail": fmt.Sprintf("卸载失败: %s", err)}, nil
	}

	sm.removeInstalledPlugin(safeName)
	sm.refreshAgentDataIndexes()
	sm.saveState()

	return map[string]any{"success": true, "name": safeName}, nil
}

// HandleSkillsImportLocal 导入本地技能
// 对应 Python: SkillManager.handle_skills_import_local(params)
func (sm *SkillManager) HandleSkillsImportLocal(ctx context.Context, params map[string]any) (map[string]any, error) {
	path := toString(params["path"])
	if path == "" {
		return map[string]any{"success": false, "detail": "缺少参数: path"}, nil
	}

	// URL 检测分支（对齐 Python: import_local 支持远程 URL 下载）
	if isHTTPDownloadTarget(path) {
		checksumSHA256 := toString(params["checksum_sha256"])
		result, err := importSkillFromRemoteArchive(ctx, sm, path, toBool(params["force"]), checksumSHA256)
		if err != nil {
			return map[string]any{"success": false, "detail": fmt.Sprintf("远程下载失败: %s", err.Error())}, nil
		}
		return result, nil
	}

	name := toString(params["name"])
	force := toBool(params["force"])

	absPath, err := filepath.Abs(path)
	if err != nil {
		return map[string]any{"success": false, "detail": fmt.Sprintf("路径无效: %s", path)}, nil
	}

	if !dirExists(absPath) {
		return map[string]any{"success": false, "detail": fmt.Sprintf("目录不存在: %s", absPath)}, nil
	}

	mdPath := sm.tryFindSkillFile(absPath)
	if mdPath == "" {
		return map[string]any{"success": false, "detail": "目录中未找到 SKILL.md"}, nil
	}

	meta := sm.parseSkillMD(mdPath)
	skillName := name
	if skillName == "" {
		skillName = toString(meta["name"])
	}
	if skillName == "" {
		skillName = filepath.Base(absPath)
	}

	safeSkillName, err := safePathName(skillName, "skill")
	if err != nil {
		return map[string]any{"success": false, "detail": err.Error()}, nil
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	dest := filepath.Join(sm.skillsDir, safeSkillName)
	if dirExists(dest) {
		if !force {
			return map[string]any{"success": false, "detail": fmt.Sprintf("技能 %s 已存在", safeSkillName)}, nil
		}
		if err := safeRmtree(dest); err != nil {
			logger.Warn(logComponent).Err(err).Str("path", dest).Msg("移除已存在技能目录失败")
		}
	}

	if err := copyDir(absPath, dest); err != nil {
		return map[string]any{"success": false, "detail": fmt.Sprintf("导入失败: %s", err)}, nil
	}

	sm.addLocalSkill(map[string]any{
		"name":         safeSkillName,
		"origin":       absPath,
		"source":       "local",
		"installed_at": time.Now().UTC().Format(time.RFC3339),
	})
	sm.refreshAgentDataIndexes()
	sm.saveState()

	return map[string]any{"success": true, "name": safeSkillName}, nil
}

// HandleSkillsMarketplaceAdd 添加 marketplace
// 对应 Python: SkillManager.handle_skills_marketplace_add(params)
func (sm *SkillManager) HandleSkillsMarketplaceAdd(ctx context.Context, params map[string]any) (map[string]any, error) {
	name := toString(params["name"])
	url := toString(params["url"])
	if name == "" || url == "" {
		return map[string]any{"success": false, "detail": "缺少参数: name 或 url"}, nil
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	marketplaces := sm.getMarketplaces()
	for _, m := range marketplaces {
		if toString(m["name"]) == name {
			return map[string]any{"success": false, "detail": fmt.Sprintf("marketplace %s 已存在", name)}, nil
		}
	}

	marketplaces = append(marketplaces, map[string]any{
		"name":    name,
		"url":     url,
		"enabled": false, // 新增源默认禁用（对齐 Python: enabled=False，避免未经确认就触发远程同步）
	})
	// 转为 []any 以保持 state 的 JSON 兼容性
	anyList := make([]any, len(marketplaces))
	for i, m := range marketplaces {
		anyList[i] = m
	}
	sm.state["marketplaces"] = anyList
	sm.saveState()

	return map[string]any{"success": true, "name": name}, nil
}

// HandleSkillsMarketplaceRemove 移除 marketplace
// 对应 Python: SkillManager.handle_skills_marketplace_remove(params)
func (sm *SkillManager) HandleSkillsMarketplaceRemove(ctx context.Context, params map[string]any) (map[string]any, error) {
	name := toString(params["name"])
	if name == "" {
		return map[string]any{"success": false, "detail": "缺少参数: name"}, nil
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	marketplaces := sm.getMarketplaces()
	var filtered []map[string]any
	found := false
	for _, m := range marketplaces {
		if toString(m["name"]) == name {
			found = true
			continue
		}
		filtered = append(filtered, m)
	}
	if !found {
		return map[string]any{"success": false, "detail": fmt.Sprintf("未找到 marketplace: %s", name)}, nil
	}

	anyList := make([]any, len(filtered))
	for i, m := range filtered {
		anyList[i] = m
	}
	sm.state["marketplaces"] = anyList
	sm.saveState()

	// 删除本地缓存目录（对齐 Python: safeRmtree(repo_dir)）
	repoDir := filepath.Join(sm.marketplaceDir, name)
	if dirExists(repoDir) {
		_ = safeRmtree(repoDir)
	}

	return map[string]any{"success": true, "name": name}, nil
}

// HandleSkillsMarketplaceToggle 切换 marketplace 的 enabled 状态
// 对应 Python: SkillManager.handle_skills_marketplace_toggle(params)
func (sm *SkillManager) HandleSkillsMarketplaceToggle(ctx context.Context, params map[string]any) (map[string]any, error) {
	name := toString(params["name"])
	enabled := toBool(params["enabled"])
	if name == "" {
		return map[string]any{"success": false, "detail": "缺少参数: name"}, nil
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	marketplaces := sm.getMarketplaces()
	found := false
	for _, m := range marketplaces {
		if toString(m["name"]) == name {
			m["enabled"] = enabled
			found = true
			break
		}
	}
	if !found {
		return map[string]any{"success": false, "detail": fmt.Sprintf("未找到 marketplace: %s", name)}, nil
	}

	anyList := make([]any, len(marketplaces))
	for i, m := range marketplaces {
		anyList[i] = m
	}
	sm.state["marketplaces"] = anyList
	sm.saveState()

	// 启用/禁用时处理本地缓存
	repoDir := filepath.Join(sm.marketplaceDir, name)
	if enabled {
		// 启用时：同步远程仓库（对齐 Python: git pull 或 git clone）
		if dirExists(repoDir) {
			_ = gitPull(ctx, repoDir)
		} else {
			// 查找 marketplace URL
			var repoURL string
			for _, m := range marketplaces {
				if toString(m["name"]) == name {
					repoURL = toString(m["url"])
					break
				}
			}
			if repoURL != "" {
				_ = gitClone(ctx, repoURL, repoDir)
			}
		}
	} else {
		// 禁用时：删除本地缓存（对齐 Python: safeRmtree）
		if dirExists(repoDir) {
			_ = safeRmtree(repoDir)
		}
	}

	return map[string]any{"success": true, "name": name, "enabled": enabled}, nil
}

// HandleSkillsSkillnetSearch 在线搜索 SkillNet 技能
// 对应 Python: SkillManager.handle_skills_skillnet_search(params)
func (sm *SkillManager) HandleSkillsSkillnetSearch(ctx context.Context, params map[string]any) (map[string]any, error) {
	// ⤵️ 回填: SkillNet 搜索尚未实现
	// 代理环境变量上下文（对齐 Python: _skillnet_network_context）
	restore := skillnetNetworkContext()
	defer restore()

	query := trimSpace(toString(params["q"]))
	if query == "" {
		return map[string]any{"success": false, "detail": "缺少参数: q"}, nil
	}

	// SkillNet 需要外部 API，后续补充
	return map[string]any{"success": false, "detail": errNotImplemented.Error()}, errNotImplemented
}

// HandleSkillsSkillnetInstall 从 SkillNet URL 异步安装
// 对应 Python: SkillManager.handle_skills_skillnet_install(params)
func (sm *SkillManager) HandleSkillsSkillnetInstall(ctx context.Context, params map[string]any) (map[string]any, error) {
	// 代理环境变量上下文（对齐 Python: _skillnet_network_context）
	restore := skillnetNetworkContext()
	defer restore()

	skillURL := trimSpace(toString(params["url"]))
	if skillURL == "" {
		return map[string]any{"success": false, "detail": "缺少参数: url"}, nil
	}

	installID := generateUUID()
	sm.mu.Lock()
	sm.skillnetInstallJobs[installID] = map[string]any{"status": "pending"}
	sm.mu.Unlock()

	// 异步设置 job 为 failed（对齐 Python: SkillNet 安装尚未实现）
	go func() {
		time.Sleep(100 * time.Millisecond) // 确保调用方获取到 pending 状态
		job := sm.GetInstallJob(installID)
		if job != nil {
			job["status"] = "failed"
			job["detail"] = "SkillNet 安装尚未实现 ⤵️"
			sm.SetSkillnetInstallJob(installID, job)
		}
	}()

	return map[string]any{
		"success":    true,
		"pending":    true,
		"install_id": installID,
	}, nil
}

// HandleSkillsSkillnetInstallStatus 查询 SkillNet 异步安装状态
// 对应 Python: SkillManager.handle_skills_skillnet_install_status(params)
func (sm *SkillManager) HandleSkillsSkillnetInstallStatus(ctx context.Context, params map[string]any) (map[string]any, error) {
	installID := trimSpace(toString(params["install_id"]))
	if installID == "" {
		return map[string]any{"success": false, "detail": "缺少参数: install_id"}, nil
	}

	sm.mu.RLock()
	job, ok := sm.skillnetInstallJobs[installID]
	sm.mu.RUnlock()

	if !ok {
		return map[string]any{
			"success":    false,
			"detail":     "安装会话已过期，请重新点击安装。",
			"detail_key": "skills.skillNet.errors.sessionExpired",
		}, nil
	}

	status := toString(job["status"])
	switch status {
	case "pending":
		return map[string]any{"success": true, "status": "pending"}, nil
	case "failed":
		out := map[string]any{
			"success": false,
			"status":  "failed",
			"detail":  toStringWithDefault(job["detail"], "安装失败"),
		}
		if v, ok := job["detail_key"]; ok {
			out["detail_key"] = v
		}
		if v, ok := job["detail_params"]; ok {
			out["detail_params"] = v
		}
		return out, nil
	default: // "done"
		return map[string]any{
			"success": true,
			"status":  "done",
			"skill":   job["skill"],
		}, nil
	}
}

// SetSkillnetInstallJob 设置 SkillNet 安装任务（测试辅助方法）
func (sm *SkillManager) SetSkillnetInstallJob(installID string, job map[string]any) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.skillnetInstallJobs[installID] = job
}

// GetInstallJob 获取指定安装任务（测试辅助方法）
func (sm *SkillManager) GetInstallJob(installID string) map[string]any {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	job, ok := sm.skillnetInstallJobs[installID]
	if !ok {
		return nil
	}
	// 返回副本以避免外部修改
	cp := make(map[string]any, len(job))
	for k, v := range job {
		cp[k] = v
	}
	return cp
}

// GetInstallJobIDs 获取所有安装任务 ID（测试辅助方法）
func (sm *SkillManager) GetInstallJobIDs() []string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	ids := make([]string, 0, len(sm.skillnetInstallJobs))
	for id := range sm.skillnetInstallJobs {
		ids = append(ids, id)
	}
	return ids
}

// HandleSkillsSkillnetEvaluate 使用 SkillNet 评估
// 对应 Python: SkillManager.handle_skills_skillnet_evaluate(params)
func (sm *SkillManager) HandleSkillsSkillnetEvaluate(ctx context.Context, params map[string]any) (map[string]any, error) {
	// ⤵️ 回填: SkillNet 评估尚未实现
	// 代理环境变量上下文（对齐 Python: _skillnet_network_context）
	restore := skillnetNetworkContext()
	defer restore()

	skillURL := trimSpace(toString(params["url"]))
	if skillURL == "" {
		return map[string]any{"success": false, "detail": "缺少参数: url"}, nil
	}
	return map[string]any{"success": false, "detail": errNotImplemented.Error()}, errNotImplemented
}

// HandleSkillsClawhubGetToken 获取 ClawHub CLI token（已掩码）
// 对应 Python: SkillManager.handle_skills_clawhub_get_token(params)
func (sm *SkillManager) HandleSkillsClawhubGetToken(ctx context.Context, params map[string]any) (map[string]any, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	token := sm.getClawhubToken()
	return map[string]any{
		"success":   true,
		"token":     maskClawhubToken(token),
		"has_token": token != "",
	}, nil
}

// HandleSkillsClawhubSetToken 设置 ClawHub CLI token
// 对应 Python: SkillManager.handle_skills_clawhub_set_token(params)
func (sm *SkillManager) HandleSkillsClawhubSetToken(ctx context.Context, params map[string]any) (map[string]any, error) {
	token := trimSpace(toString(params["token"]))

	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.SetClawhubToken(token)
	sm.saveState()

	return map[string]any{
		"success": true,
		"token":   maskClawhubToken(token),
	}, nil
}

// HandleSkillsClawhubSearch 从 ClawHub 搜索技能
// 对应 Python: SkillManager.handle_skills_clawhub_search(params)
func (sm *SkillManager) HandleSkillsClawhubSearch(ctx context.Context, params map[string]any) (map[string]any, error) {
	query := trimSpace(toString(params["q"]))
	if query == "" {
		return map[string]any{"success": false, "detail": "缺少参数: q"}, nil
	}

	token := sm.getClawhubToken()
	if token == "" {
		return map[string]any{"success": false, "detail": "ClawHub token 未配置", "detail_key": "skills.clawhub.errors.noToken"}, nil
	}

	limit := 10
	if v, ok := params["limit"]; ok {
		if n, err := strconv.Atoi(toString(v)); err == nil && n > 0 {
			limit = n
		}
	}

	// 构建请求 URL（支持环境变量覆盖）
	clawhubBaseURL := envString("CLAWHUB_BASE_URL", "https://clawhub.ai")
	reqURL, _ := url.Parse(clawhubBaseURL + "/api/v1/search")
	q := reqURL.Query()
	q.Set("q", query)
	q.Set("limit", strconv.Itoa(limit))
	reqURL.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return map[string]any{"success": false, "detail": err.Error()}, nil
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return map[string]any{"success": false, "detail": "网络请求失败: " + err.Error()}, nil
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return map[string]any{"success": false, "detail": fmt.Sprintf("ClawHub API 返回状态码: %d", resp.StatusCode)}, nil
	}

	var data map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return map[string]any{"success": false, "detail": "JSON 解析失败: " + err.Error()}, nil
	}

	// 映射结果字段
	rawResults, ok := toSliceOfAny(data["results"])
	if !ok {
		return map[string]any{"success": true, "query": query, "count": 0, "skills": []map[string]any{}}, nil
	}

	var skills []map[string]any
	for _, item := range rawResults {
		if m, ok := item.(map[string]any); ok {
			skills = append(skills, map[string]any{
				"slug":         toString(m["slug"]),
				"display_name": toString(m["displayName"]),
				"summary":      toString(m["summary"]),
				"version":      toString(m["version"]),
				"updated_at":   m["updatedAt"],
			})
		}
	}

	return map[string]any{"success": true, "query": query, "count": len(skills), "skills": skills}, nil
}

// HandleSkillsClawhubDownload 从 ClawHub 下载并安装技能
// 对应 Python: SkillManager.handle_skills_clawhub_download(params)
func (sm *SkillManager) HandleSkillsClawhubDownload(ctx context.Context, params map[string]any) (map[string]any, error) {
	slug, err := safePathName(params["slug"], "skill")
	if err != nil {
		return map[string]any{"success": false, "detail": err.Error()}, nil
	}

	token := sm.getClawhubToken()
	if token == "" {
		return map[string]any{"success": false, "detail": "ClawHub token 未配置", "detail_key": "skills.clawhub.errors.noToken"}, nil
	}

	force := toBoolWithDefault(params["force"], false)
	destDir := filepath.Join(sm.skillsDir, slug)
	if dirExists(destDir) && !force {
		return map[string]any{"success": false, "detail": fmt.Sprintf("技能 %s 已存在，使用 force=true 覆盖", slug)}, nil
	}

	// 构建请求 URL（支持环境变量覆盖）
	clawhubBaseURL := envString("CLAWHUB_BASE_URL", "https://clawhub.ai")
	reqURL, _ := url.Parse(clawhubBaseURL + "/api/v1/download")
	q := reqURL.Query()
	q.Set("slug", slug)
	if v := toString(params["version"]); v != "" {
		q.Set("version", v)
	}
	if v := toString(params["tag"]); v != "" {
		q.Set("tag", v)
	}
	reqURL.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return map[string]any{"success": false, "detail": err.Error()}, nil
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return map[string]any{"success": false, "detail": "网络请求失败: " + err.Error()}, nil
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return map[string]any{"success": false, "detail": fmt.Sprintf("ClawHub API 返回状态码: %d", resp.StatusCode)}, nil
	}

	// 读取 ZIP 内容
	zipBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return map[string]any{"success": false, "detail": "读取响应失败: " + err.Error()}, nil
	}

	// 解压到临时目录
	tmpDir, err := os.MkdirTemp("", "jiuwenswarm_clawhub_")
	if err != nil {
		return map[string]any{"success": false, "detail": "创建临时目录失败: " + err.Error()}, nil
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	if err := safeExtractZIPBytesToDir(zipBytes, tmpDir); err != nil {
		return map[string]any{"success": false, "detail": "ZIP 解压失败: " + err.Error()}, nil
	}

	// 定位 SKILL.md（递归搜索，对齐 Python _locate_skill_dir）
	skillDir := sm.locateSkillDir(tmpDir)
	if skillDir == "" {
		return map[string]any{"success": false, "detail": "ZIP 中未找到 SKILL.md"}, nil
	}
	skillFile := sm.tryFindSkillFile(skillDir)
	meta := sm.parseSkillMD(skillFile)
	if meta == nil {
		return map[string]any{"success": false, "detail": "SKILL.md 解析失败"}, nil
	}
	skillName := toString(meta["name"])
	if skillName == "" {
		skillName = slug
	}

	// 安装到 skillsDir
	finalDest := filepath.Join(sm.skillsDir, skillName)
	if dirExists(finalDest) {
		if force {
			_ = safeRmtree(finalDest)
		} else {
			return map[string]any{"success": false, "detail": fmt.Sprintf("技能 %s 已存在", skillName)}, nil
		}
	}
	// 定位 SKILL.md 所在的目录（可能是子目录）
	skillSrcDir := skillDir
	if err := copyDir(skillSrcDir, finalDest); err != nil {
		return map[string]any{"success": false, "detail": "安装失败: " + err.Error()}, nil
	}

	// 记录安装信息
	sm.mu.Lock()
	sm.addLocalSkill(map[string]any{
		"name":         skillName,
		"origin":       "clawhub:" + slug,
		"source":       "clawhub",
		"installed_at": time.Now().Format(time.RFC3339),
	})
	sm.AddInstalledPlugin(map[string]any{
		"name":         skillName,
		"marketplace":  "clawhub",
		"source":       "clawhub",
		"version":      toString(meta["version"]), // 从 SKILL.md meta 获取
		"commit":       "",                        // 对齐 Python 默认空
		"installed_at": time.Now().Format(time.RFC3339),
	})
	// saveState 已在 AddInstalledPlugin 内调用，此处不再冗余
	sm.mu.Unlock()

	sm.refreshAgentDataIndexes()

	return map[string]any{
		"success": true,
		"skill":   map[string]any{"name": skillName, "source": "clawhub"},
	}, nil
}

// HandleSkillsTeamSkillsHubInfo 查询 Team Skills Hub 技能版本详情
// 对应 Python: SkillManager.handle_skills_team_skills_hub_info(params)
func (sm *SkillManager) HandleSkillsTeamSkillsHubInfo(ctx context.Context, params map[string]any) (map[string]any, error) {
	assetID := trimSpace(toString(params["asset_id"]))
	if assetID == "" {
		return map[string]any{"success": false, "detail": "缺少参数: asset_id"}, nil
	}
	baseURL := trimSpace(toString(params["market_url"]))
	version := trimSpace(toString(params["version"]))

	queryParams := url.Values{}
	if version != "" {
		queryParams.Set("version", version)
	}

	data, err := sm.teamSkillsHubHTTPGet(ctx, "/api/v1/artifacts/"+assetID, queryParams, 0, baseURL)
	if err != nil {
		return map[string]any{"success": false, "detail": err.Error()}, nil
	}
	return map[string]any{"success": true, "data": data}, nil
}

// HandleSkillsTeamSkillsHubInit 初始化 TeamSkills 模板目录
// 对应 Python: SkillManager.handle_skills_team_skills_hub_init(params)
func (sm *SkillManager) HandleSkillsTeamSkillsHubInit(ctx context.Context, params map[string]any) (map[string]any, error) {
	name := trimSpace(toString(params["name"]))
	if name == "" {
		return map[string]any{"success": false, "detail": "缺少参数: name"}, nil
	}
	output := trimSpace(toString(params["output"]))
	var dirPath string
	if output != "" {
		dirPath = filepath.Join(output, name)
	} else {
		dirPath = filepath.Join(sm.skillsDir, name)
	}

	if dirExists(dirPath) {
		return map[string]any{"success": false, "detail": fmt.Sprintf("目录 %s 已存在", dirPath)}, nil
	}

	// 创建目录结构
	if err := os.MkdirAll(filepath.Join(dirPath, "tools"), 0o755); err != nil {
		return map[string]any{"success": false, "detail": "创建目录失败: " + err.Error()}, nil
	}
	if err := os.MkdirAll(filepath.Join(dirPath, "data"), 0o755); err != nil {
		return map[string]any{"success": false, "detail": "创建目录失败: " + err.Error()}, nil
	}

	// 写入 SKILL.md 骨架
	skillContent := fmt.Sprintf("---\nname: %s\ndescription: \"\"\nversion: \"1.0.0\"\n---\n", name)
	if err := os.WriteFile(filepath.Join(dirPath, "SKILL.md"), []byte(skillContent), 0o644); err != nil {
		return map[string]any{"success": false, "detail": "写入 SKILL.md 失败: " + err.Error()}, nil
	}

	return map[string]any{"success": true, "path": dirPath}, nil
}

// HandleSkillsTeamSkillsHubValidate 校验 TeamSkills 目录结构与 SKILL.md 内容
// 对应 Python: SkillManager.handle_skills_team_skills_hub_validate(params)
func (sm *SkillManager) HandleSkillsTeamSkillsHubValidate(ctx context.Context, params map[string]any) (map[string]any, error) {
	dirPath := trimSpace(toString(params["path"]))
	if dirPath == "" {
		return map[string]any{"success": false, "detail": "缺少参数: path"}, nil
	}

	var errs []string
	if !dirExists(dirPath) {
		return map[string]any{"success": false, "detail": "目录不存在"}, nil
	}

	skillFile := sm.tryFindSkillFile(dirPath)
	if skillFile == "" {
		return map[string]any{"success": true, "valid": false, "errors": []string{"缺少 SKILL.md 文件"}}, nil
	}

	meta := sm.parseSkillMD(skillFile)
	if meta == nil {
		return map[string]any{"success": true, "valid": false, "errors": []string{"SKILL.md 解析失败"}}, nil
	}

	if toString(meta["name"]) == "" {
		errs = append(errs, "SKILL.md 缺少 name 字段")
	}
	if toString(meta["description"]) == "" {
		errs = append(errs, "SKILL.md 缺少 description 字段")
	}

	// skill_type 判断 + teamskills roles 校验（对齐 Python: handle_skills_team_skills_hub_validate）
	skillType := strings.ToLower(trimSpace(toString(params["skill_type"])))
	if skillType == "" {
		skillType = strings.ToLower(trimSpace(toString(params["plugin_type"])))
	}
	if skillType == "" {
		skillType = strings.ToLower(trimSpace(toString(params["type"])))
	}
	if skillType != "teamskills" && skillType != "skill" {
		// 推断 skill_type（对齐 Python: kind=="team-skill" → teamskills）
		if strings.ToLower(trimSpace(toString(meta["kind"]))) == "team-skill" {
			skillType = "teamskills"
		} else {
			skillType = "skill"
		}
	}

	if skillType == "teamskills" {
		// roles 完整校验（对齐 Python: frontmatter roles 必须是非空列表，至少 2 项有效 id）
		roles, _ := meta["roles"].([]any)
		if len(roles) == 0 {
			errs = append(errs, "frontmatter `roles` must be a non-empty list")
		} else {
			var roleIDs []string
			for i, role := range roles {
				roleMap, ok := role.(map[string]any)
				if !ok {
					errs = append(errs, fmt.Sprintf("roles[%d] must be an object", i))
					continue
				}
				roleID := trimSpace(toString(roleMap["id"]))
				if roleID == "" {
					errs = append(errs, fmt.Sprintf("roles[%d] missing required field `id`", i))
					continue
				}
				roleIDs = append(roleIDs, roleID)
			}
			if len(roleIDs) < 2 {
				errs = append(errs, "frontmatter `roles` must list at least 2 entries with valid `id`")
			} else {
				// 检查重复 id
				seen := make(map[string]bool)
				for _, id := range roleIDs {
					if seen[id] {
						errs = append(errs, "frontmatter `roles` must not repeat the same `id`")
						break
					}
					seen[id] = true
				}
			}
		}
	}

	if len(errs) > 0 {
		detail := "校验失败"
		if skillType == "teamskills" {
			detail = "TeamSkills roles 校验失败"
		}
		return map[string]any{"success": false, "detail": detail, "errors": errs}, nil
	}
	return map[string]any{
		"success":    true,
		"path":       dirPath,
		"skill_file": skillFile,
		"skill_type": skillType,
		"name":       trimSpace(toString(meta["name"])),
		"warnings":   []string{},
	}, nil
}

// HandleSkillsTeamSkillsHubPack 将 TeamSkills 目录打包为 zip
// 对应 Python: SkillManager.handle_skills_team_skills_hub_pack(params)
func (sm *SkillManager) HandleSkillsTeamSkillsHubPack(ctx context.Context, params map[string]any) (map[string]any, error) {
	dirPath := trimSpace(toString(params["path"]))
	if dirPath == "" {
		return map[string]any{"success": false, "detail": "缺少参数: path"}, nil
	}
	if !dirExists(dirPath) {
		return map[string]any{"success": false, "detail": "目录不存在"}, nil
	}

	output := trimSpace(toString(params["output"]))
	if output == "" {
		output = dirPath + ".zip"
	}

	// 排除的目录
	excludeDirs := map[string]bool{".git": true, "__pycache__": true, "node_modules": true}

	outFile, err := os.Create(output)
	if err != nil {
		return map[string]any{"success": false, "detail": "创建 ZIP 文件失败: " + err.Error()}, nil
	}
	defer func() { _ = outFile.Close() }()

	zipWriter := zip.NewWriter(outFile)
	err = filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dirPath, path)
		if rel == "." {
			return nil
		}
		// 检查排除目录
		parts := strings.Split(rel, string(filepath.Separator))
		for _, p := range parts {
			if excludeDirs[p] {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		if info.IsDir() {
			return nil
		}
		w, err := zipWriter.Create(rel)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	})
	_ = zipWriter.Close()
	_ = outFile.Close()

	if err != nil {
		return map[string]any{"success": false, "detail": "打包失败: " + err.Error()}, nil
	}

	fi, _ := os.Stat(output)
	var size int64
	if fi != nil {
		size = fi.Size()
	}
	return map[string]any{"success": true, "zip_path": output, "size": size}, nil
}

// HandleSkillsTeamSkillsHubSearch 从 Team Skills Hub 搜索技能
// 对应 Python: SkillManager.handle_skills_team_skills_hub_search(params)
func (sm *SkillManager) HandleSkillsTeamSkillsHubSearch(ctx context.Context, params map[string]any) (map[string]any, error) {
	query := trimSpace(toString(params["q"]))
	baseURL := trimSpace(toString(params["market_url"]))

	// 构建查询参数
	queryParams := url.Values{}
	if query != "" {
		queryParams.Set("search_keyword", query)
	}
	if v := trimSpace(toString(params["search_asset_id"])); v != "" {
		queryParams.Set("asset_id", v)
	}
	if v := trimSpace(toString(params["search_asset_type"])); v != "" {
		queryParams.Set("asset_type", v)
	}
	if v := trimSpace(toString(params["search_publisher_id"])); v != "" {
		queryParams.Set("publisher_id", v)
	}
	if v := trimSpace(toString(params["skill_type"])); v != "" {
		queryParams.Set("plugin_type", v)
	} else if v := trimSpace(toString(params["plugin_type"])); v != "" {
		queryParams.Set("plugin_type", v)
	}
	if v := trimSpace(toString(params["author"])); v != "" {
		queryParams.Set("publisher_name", v)
	}
	if v := trimSpace(toString(params["order_by"])); v != "" {
		queryParams.Set("order_by", v)
	} else {
		queryParams.Set("order_by", "install_count")
	}
	if v, ok := params["desc"]; ok {
		queryParams.Set("desc", toString(v))
	} else {
		queryParams.Set("desc", "true")
	}

	// 分页参数
	pageSize := 20
	if v, ok := params["limit"]; ok {
		if n, err := strconv.Atoi(toString(v)); err == nil && n > 0 && n <= 100 {
			pageSize = n
		}
	} else if v, ok := params["page_size"]; ok {
		if n, err := strconv.Atoi(toString(v)); err == nil && n > 0 && n <= 100 {
			pageSize = n
		}
	}
	queryParams.Set("page_size", strconv.Itoa(pageSize))

	if v := trimSpace(toString(params["page"])); v != "" {
		queryParams.Set("page", v)
	} else {
		queryParams.Set("page", "1")
	}

	data, err := sm.teamSkillsHubHTTPGet(ctx, "/api/v1/plugins", queryParams, 0, baseURL)
	if err != nil {
		return map[string]any{"success": false, "detail": err.Error()}, nil
	}

	// 映射结果字段
	rawItems, ok := toSliceOfAny(data["items"])
	if !ok {
		return map[string]any{"success": true, "query": query, "count": 0, "skills": []map[string]any{}}, nil
	}

	var skills []map[string]any
	for _, item := range rawItems {
		if m, ok := item.(map[string]any); ok {
			assetID := toString(m["asset_id"])
			name := toString(m["name"])
			if name == "" {
				name = assetID
			}
			displayName := toString(m["display_name"])
			if displayName == "" {
				displayName = name
			}
			skills = append(skills, map[string]any{
				"asset_id":     assetID,
				"name":         name,
				"display_name": displayName,
				"summary":      toString(m["short_desc"]),
				"version":      toString(m["latest_version"]),
				"updated_at":   m["update_time"],
			})
		}
	}

	return map[string]any{"success": true, "query": query, "count": len(skills), "skills": skills}, nil
}

// HandleSkillsTeamSkillsHubInstall 从 Team Skills Hub 安装技能
// 对应 Python: SkillManager.handle_skills_team_skills_hub_install(params)
func (sm *SkillManager) HandleSkillsTeamSkillsHubInstall(ctx context.Context, params map[string]any) (map[string]any, error) {
	assetID := trimSpace(toString(params["asset_id"]))
	if assetID == "" {
		return map[string]any{"success": false, "detail": "缺少参数: asset_id"}, nil
	}

	baseURL := trimSpace(toString(params["market_url"]))
	force := toBoolWithDefault(params["force"], false)
	version := trimSpace(toString(params["version"]))
	output := trimSpace(toString(params["output"]))

	// 获取 artifact 元数据
	queryParams := url.Values{}
	if version != "" {
		queryParams.Set("version", version)
	}
	data, err := sm.teamSkillsHubHTTPGet(ctx, "/api/v1/artifacts/"+assetID, queryParams, 0, baseURL)
	if err != nil {
		return map[string]any{"success": false, "detail": "获取 artifact 元数据失败: " + err.Error()}, nil
	}

	downloadURL := toString(data["download_url"])
	checksumSHA256 := toString(data["checksum_sha256"])

	if downloadURL == "" {
		return map[string]any{"success": false, "detail": "artifact 元数据中缺少 download_url"}, nil
	}

	// 白名单校验
	if err := sm.assertTeamSkillsHubDownloadURLAllowed(downloadURL); err != nil {
		return map[string]any{"success": false, "detail": "下载 URL 校验失败: " + err.Error()}, nil
	}

	// 下载并校验
	zipBytes, err := sm.downloadZipAndVerify(ctx, downloadURL, checksumSHA256)
	if err != nil {
		return map[string]any{"success": false, "detail": "下载校验失败: " + err.Error()}, nil
	}

	// 解压到临时目录
	tmpDir, err := os.MkdirTemp("", "jiuwenswarm_team_skills_hub_")
	if err != nil {
		return map[string]any{"success": false, "detail": "创建临时目录失败: " + err.Error()}, nil
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	if err := safeExtractZIPBytesToDir(zipBytes, tmpDir); err != nil {
		return map[string]any{"success": false, "detail": "ZIP 解压失败: " + err.Error()}, nil
	}

	// 定位 SKILL.md（递归搜索，对齐 Python _locate_skill_dir）
	skillDir := sm.locateSkillDir(tmpDir)
	if skillDir == "" {
		return map[string]any{"success": false, "detail": "ZIP 中未找到 SKILL.md"}, nil
	}
	skillFile := sm.tryFindSkillFile(skillDir)
	meta := sm.parseSkillMD(skillFile)
	if meta == nil {
		return map[string]any{"success": false, "detail": "SKILL.md 解析失败"}, nil
	}
	skillName := toString(meta["name"])
	if skillName == "" {
		skillName = assetID
	}

	// 安装到目标目录
	var finalDest string
	if output != "" {
		finalDest = filepath.Join(output, skillName)
	} else {
		finalDest = filepath.Join(sm.skillsDir, skillName)
	}
	if dirExists(finalDest) && !force {
		return map[string]any{"success": false, "detail": fmt.Sprintf("技能 %s 已存在", skillName)}, nil
	}
	if dirExists(finalDest) {
		_ = safeRmtree(finalDest)
	}
	skillSrcDir := skillDir
	if err := copyDir(skillSrcDir, finalDest); err != nil {
		return map[string]any{"success": false, "detail": "安装失败: " + err.Error()}, nil
	}

	// 记录安装信息（非自定义 output 时）
	if output == "" {
		sm.mu.Lock()
		sm.addLocalSkill(map[string]any{
			"name":         skillName,
			"origin":       "teamskillshub:" + assetID,
			"source":       "teamskillshub",
			"installed_at": time.Now().Format(time.RFC3339),
		})
		sm.AddInstalledPlugin(map[string]any{
			"name":         skillName,
			"marketplace":  "teamskillshub",
			"version":      toString(meta["version"]), // 从 SKILL.md meta 获取
			"commit":       "",                        // TeamSkillsHub 安装无 git commit
			"source":       "teamskillshub",
			"installed_at": time.Now().Format(time.RFC3339),
		})
		// saveState 已在 AddInstalledPlugin 内调用，此处不再冗余
		sm.mu.Unlock()
		sm.refreshAgentDataIndexes()
	}

	return map[string]any{
		"success": true,
		"skill":   map[string]any{"name": skillName, "source": "teamskillshub", "asset_id": assetID, "path": finalDest},
	}, nil
}

// HandleSkillsTeamSkillsHubPublish 发布 TeamSkills
// 对应 Python: SkillManager.handle_skills_team_skills_hub_publish(params)
//
// 步骤（对齐 Python: _prepare_teamskills_publish_zip 规范化后上传）：
//  1. 从 path/file 构建 plugin.yaml + 规范化 ZIP
//  2. 计算 SHA256 校验和
//  3. 上传到 TeamSkills Hub
func (sm *SkillManager) HandleSkillsTeamSkillsHubPublish(ctx context.Context, params map[string]any) (map[string]any, error) {
	pathRaw := trimSpace(toString(params["path"]))
	fileRaw := trimSpace(toString(params["file"]))
	pluginVersion := trimSpace(toString(params["version"]))
	if pluginVersion == "" {
		pluginVersion = "1.0.0"
	}

	if pathRaw == "" && fileRaw == "" {
		return map[string]any{"success": false, "detail": "缺少参数: path 或 file"}, nil
	}

	// 构建规范化 ZIP（对齐 Python: _prepare_teamskills_publish_zip）
	zipData, checksumSHA256, err := buildTeamskillsPublishZipFromPath(pathRaw, fileRaw, pluginVersion)
	if err != nil {
		return map[string]any{"success": false, "detail": "构建发布 ZIP 失败: " + err.Error()}, nil
	}

	baseURL := trimSpace(toString(params["market_url"]))
	if baseURL == "" {
		baseURL = envString(teamSkillsHubBaseURLEnv, teamSkillsHubDefaultBaseURL)
	}

	// 构建 multipart/form-data 上传
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "teamskills_publish_normalized.zip")
	if err != nil {
		return map[string]any{"success": false, "detail": "构建上传请求失败: " + err.Error()}, nil
	}
	if _, err := part.Write(zipData); err != nil {
		return map[string]any{"success": false, "detail": "写入上传数据失败: " + err.Error()}, nil
	}
	_ = writer.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/v1/artifacts", body)
	if err != nil {
		return map[string]any{"success": false, "detail": "构建请求失败: " + err.Error()}, nil
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-Checksum-SHA256", checksumSHA256)

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return map[string]any{"success": false, "detail": "上传失败: " + err.Error()}, nil
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return map[string]any{"success": false, "detail": fmt.Sprintf("API 返回状态码: %d", resp.StatusCode)}, nil
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return map[string]any{"success": false, "detail": "JSON 解析失败: " + err.Error()}, nil
	}

	assetID := ""
	if data, ok := result["data"].(map[string]any); ok {
		assetID = toString(data["asset_id"])
	}
	return map[string]any{"success": true, "asset_id": assetID, "checksum_sha256": checksumSHA256}, nil
}

// HandleSkillsTeamSkillsHubDelete 删除 TeamSkills
// 对应 Python: SkillManager.handle_skills_team_skills_hub_delete(params)
func (sm *SkillManager) HandleSkillsTeamSkillsHubDelete(ctx context.Context, params map[string]any) (map[string]any, error) {
	assetID := trimSpace(toString(params["asset_id"]))
	if assetID == "" {
		return map[string]any{"success": false, "detail": "缺少参数: asset_id"}, nil
	}
	baseURL := trimSpace(toString(params["market_url"]))
	if baseURL == "" {
		baseURL = envString(teamSkillsHubBaseURLEnv, teamSkillsHubDefaultBaseURL)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, baseURL+"/api/v1/artifacts/"+assetID, nil)
	if err != nil {
		return map[string]any{"success": false, "detail": "构建请求失败: " + err.Error()}, nil
	}

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return map[string]any{"success": false, "detail": "删除失败: " + err.Error()}, nil
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return map[string]any{"success": false, "detail": fmt.Sprintf("API 返回状态码: %d", resp.StatusCode)}, nil
	}

	return map[string]any{"success": true}, nil
}

// HandlePluginsList 列出已安装的插件
// 对应 Python: SkillManager.handle_plugins_list(params) → 同 handle_skills_installed
func (sm *SkillManager) HandlePluginsList(ctx context.Context, params map[string]any) (map[string]any, error) {
	return sm.HandleSkillsInstalled(ctx, params)
}

// HandlePluginsInstall 安装插件
// 对应 Python: SkillManager.handle_plugins_install(params)
func (sm *SkillManager) HandlePluginsInstall(ctx context.Context, params map[string]any) (map[string]any, error) {
	return sm.HandleSkillsInstall(ctx, params)
}

// HandlePluginsUninstall 卸载插件
// 对应 Python: SkillManager.handle_plugins_uninstall(params)
func (sm *SkillManager) HandlePluginsUninstall(ctx context.Context, params map[string]any) (map[string]any, error) {
	return sm.HandleSkillsUninstall(ctx, params)
}

// HandlePluginsEnable 启用插件
// 对应 Python: SkillManager.handle_plugins_enable(params)
func (sm *SkillManager) HandlePluginsEnable(ctx context.Context, params map[string]any) (map[string]any, error) {
	name := toString(params["name"])
	if name == "" {
		return map[string]any{"success": false, "detail": "缺少参数: name"}, nil
	}
	return sm.HandleSkillsToggle(ctx, map[string]any{"name": name, "enabled": true})
}

// HandlePluginsDisable 禁用插件
// 对应 Python: SkillManager.handle_plugins_disable(params)
func (sm *SkillManager) HandlePluginsDisable(ctx context.Context, params map[string]any) (map[string]any, error) {
	name := toString(params["name"])
	if name == "" {
		return map[string]any{"success": false, "detail": "缺少参数: name"}, nil
	}
	return sm.HandleSkillsToggle(ctx, map[string]any{"name": name, "enabled": false})
}

// HandlePluginsReload 重载插件
// 对应 Python: SkillManager.handle_plugins_reload(params)
func (sm *SkillManager) HandlePluginsReload(ctx context.Context, params map[string]any) (map[string]any, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.refreshAgentDataIndexes()
	return map[string]any{"success": true}, nil
}

// GetInstalledPlugins 获取已安装插件列表
// 对应 Python: SkillManager._get_installed_plugins()
func (sm *SkillManager) GetInstalledPlugins() []map[string]any {
	raw, ok := sm.state["installed_plugins"]
	if !ok {
		return nil
	}
	list, ok := toSliceOfAny(raw)
	if !ok {
		return nil
	}
	var result []map[string]any
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			result = append(result, m)
		}
	}
	return result
}

// AddInstalledPlugin 添加已安装插件记录
// 对应 Python: SkillManager._add_installed_plugin(plugin)
func (sm *SkillManager) AddInstalledPlugin(plugin map[string]any) {
	// 对齐 Python: plugin = self._normalize_plugin(plugin) — 补全 enabled=True
	plugin = sm.normalizePlugin(plugin)
	plugins := sm.GetInstalledPlugins()
	// 如果已存在同名插件，替换
	name := toString(plugin["name"])
	for i, p := range plugins {
		if toString(p["name"]) == name {
			plugins[i] = plugin
			sm.state["installed_plugins"] = mapSliceToAny(plugins)
			// 对齐 Python: self._save_state()
			sm.saveState()
			return
		}
	}
	plugins = append(plugins, plugin)
	sm.state["installed_plugins"] = mapSliceToAny(plugins)
	// 对齐 Python: self._save_state()
	sm.saveState()
}

// AddLocalSkill 添加本地技能记录（外部接口，自带锁保护）
func (sm *SkillManager) AddLocalSkill(skill map[string]any) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.addLocalSkill(skill)
}

// GetLocalSkills 返回本地技能列表
// 对应 Python: SkillManager.get_local_skills()
func (sm *SkillManager) GetLocalSkills() []map[string]any {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	raw, ok := sm.state["local_skills"]
	if !ok {
		return []map[string]any{}
	}
	list, ok := toSliceOfAny(raw)
	if !ok {
		return []map[string]any{}
	}
	var result []map[string]any
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			result = append(result, m)
		}
	}
	return result
}

// SetClawhubToken 设置 ClawHub token
// 对应 Python: SkillManager._set_clawhub_token(token)
func (sm *SkillManager) SetClawhubToken(token string) {
	sm.state[clawhubTokenKey] = token
}

// GetSkillMeta 从本地技能目录读取解析后的 SKILL.md 元数据
// 对应 Python: SkillManager.get_skill_meta(skill_name)
func (sm *SkillManager) GetSkillMeta(name string) map[string]any {
	skillDir := sm.resolveLocalSkillDir(name)
	if skillDir == "" {
		return nil
	}
	skillFile := sm.tryFindSkillFile(skillDir)
	if skillFile == "" {
		return nil
	}
	meta := sm.parseSkillMD(skillFile)
	if meta == nil {
		return nil
	}
	meta["skill_dir"] = skillDir
	meta["skill_file"] = skillFile
	return meta
}

// IsBuiltinSkill 判断技能是否为内置技能
// 对应 Python: SkillManager.is_builtin_skill(skill_name)
// 比较用户 skills 目录中的技能与内置目录中的技能是否指向同一物理路径
func (sm *SkillManager) IsBuiltinSkill(name string) bool {
	if name == "" {
		return false
	}
	builtinDir := getBuiltinSkillsDir()
	if builtinDir == "" {
		return false
	}
	// 安全校验路径名称
	if _, err := safePathName(name, "skill"); err != nil {
		return false
	}
	// 用户 skills 目录下的技能路径
	userSkillPath := filepath.Join(sm.skillsDir, name)
	userInfo, err := os.Stat(userSkillPath)
	if err != nil {
		return false
	}
	// 内置目录下的技能路径
	builtinSkillPath := filepath.Join(builtinDir, name)
	builtinInfo, err := os.Stat(builtinSkillPath)
	if err != nil {
		return false
	}
	return os.SameFile(userInfo, builtinInfo)
}

// GetSkillEnabled 读取技能的 enabled 标志，默认为 true（向后兼容）。
// 对应 Python: SkillManager.get_skill_enabled(skill_name)
func (sm *SkillManager) GetSkillEnabled(name string) bool {
	return GetSkillEnabled(sm.state, name)
}

// SetSkillEnabled 将技能的 enabled 标志持久化到 state 中。
// 对应 Python: SkillManager.set_skill_enabled(skill_name, enabled)
func (sm *SkillManager) SetSkillEnabled(name string, enabled bool) {
	SetSkillEnabled(sm.state, name, enabled)
}

// ListDisabledSkills 从 skill_configs 中返回已禁用的技能名称列表（排序）。
// 对应 Python: SkillManager.list_disabled_skills()
func (sm *SkillManager) ListDisabledSkills() []string {
	return ListDisabledSkills(sm.state)
}

// ListExecutionDisabledSkills 返回当前已安装的已禁用技能名称列表。
// 对应 Python: SkillManager.list_execution_disabled_skills()
func (sm *SkillManager) ListExecutionDisabledSkills() []string {
	return ListExecutionDisabledSkills(sm.state)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getMirrorSkillsDirs 返回需要镜像同步的 skills 目录（对齐 Python: _get_mirror_skills_dirs）。
// Go 二进制等价 Python package 安装模式，始终返回空切片。
// ⤵️ 回填: 如未来需要源码开发模式支持，需补全 mirror 路径逻辑。
func (sm *SkillManager) getMirrorSkillsDirs() []string {
	return []string{} // Go 二进制 = package 安装模式，无 mirror 目录
}

// getBuiltinSkillsDir 返回内置技能目录。
func (sm *SkillManager) getBuiltinSkillsDir() string {
	// 对齐 Python: 检查 skills/builtin 目录
	builtinDir := filepath.Join(sm.skillsDir, "builtin")
	if dirExists(builtinDir) {
		return builtinDir
	}
	return ""
}

// isBuiltinSkill 检查技能是否为内置技能。
func (sm *SkillManager) isBuiltinSkill(skillName, builtinDir string) (bool, string) {
	entries, err := os.ReadDir(builtinDir)
	if err != nil {
		return false, ""
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillDir := filepath.Join(builtinDir, entry.Name(), "skill")
		skillMd := filepath.Join(skillDir, "SKILL.md")
		if _, err := os.Stat(skillMd); err == nil {
			// 解析 SKILL.md 检查技能名
			meta, _ := parseSKILLMd(skillMd)
			if meta != nil {
				if name, ok := meta["name"].(string); ok && name == skillName {
					return true, skillDir
				}
			}
		}
	}
	return false, ""
}

// parseSKILLMd 解析 SKILL.md 文件返回元数据（包级函数，用于内置技能检查等场景）。
func parseSKILLMd(path string) (map[string]any, error) {
	if !fileExists(path) {
		return nil, fmt.Errorf("文件不存在: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	content := string(data)
	meta := make(map[string]any)
	if strings.HasPrefix(content, "---") {
		end := strings.Index(content[3:], "---")
		if end >= 0 {
			frontmatter := content[3 : 3+end]
			meta = parseYAMLFrontmatter(frontmatter)
		}
	}
	return meta, nil
}

// loadState 从文件加载状态
// 对应 Python: SkillManager._load_state()
func (sm *SkillManager) loadState() map[string]any {
	data, err := os.ReadFile(sm.stateFile)
	if err != nil {
		return make(map[string]any)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		logger.Warn(logComponent).Err(err).Msg("解析技能状态文件失败，使用空状态")
		return make(map[string]any)
	}
	return state
}

// saveState 将状态保存到文件
// 对应 Python: SkillManager._save_state()
func (sm *SkillManager) saveState() {
	data, err := json.MarshalIndent(sm.state, "", "  ")
	if err != nil {
		logger.Error(logComponent).Err(err).Msg("序列化技能状态失败")
		return
	}
	if err := os.MkdirAll(filepath.Dir(sm.stateFile), 0o755); err != nil {
		logger.Error(logComponent).Err(err).Msg("创建技能状态目录失败")
		return
	}
	if err := os.WriteFile(sm.stateFile, data, 0o644); err != nil {
		logger.Error(logComponent).Err(err).Msg("保存技能状态文件失败")
	}
}

// getMarketplaces 获取 marketplace 配置列表
// 对应 Python: SkillManager._get_marketplaces()
func (sm *SkillManager) getMarketplaces() []map[string]any {
	raw, ok := sm.state["marketplaces"]
	if !ok {
		return nil
	}
	list, ok := toSliceOfAny(raw)
	if !ok {
		return nil
	}
	var result []map[string]any
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			result = append(result, m)
		}
	}
	return result
}

// removeInstalledPlugin 移除已安装插件记录
func (sm *SkillManager) removeInstalledPlugin(name string) {
	plugins := sm.GetInstalledPlugins()
	var filtered []map[string]any
	for _, p := range plugins {
		if toString(p["name"]) != name {
			filtered = append(filtered, p)
		}
	}
	sm.state["installed_plugins"] = mapSliceToAny(filtered)
}

// addLocalSkill 添加本地技能记录（内部方法，调用者需持有锁）
// 对应 Python: SkillManager._add_local_skill(skill)
func (sm *SkillManager) addLocalSkill(skill map[string]any) {
	raw, ok := sm.state["local_skills"]
	if !ok {
		raw = []any{}
	}
	list, ok := toSliceOfAny(raw)
	if !ok {
		list = []any{}
	}
	list = append(list, skill)
	sm.state["local_skills"] = list
}

// normalizePlugin 规范化插件记录
// 对应 Python: SkillManager._normalize_plugin(p)
func (sm *SkillManager) normalizePlugin(p map[string]any) map[string]any {
	// 对齐 Python _normalize_plugin：补全 enabled 字段
	if _, ok := p["enabled"]; !ok {
		p["enabled"] = true
	}
	return p
}

// getClawhubToken 获取 ClawHub token
// 对应 Python: SkillManager._get_clawhub_token()
func (sm *SkillManager) getClawhubToken() string {
	return toString(sm.state[clawhubTokenKey])
}

// maskClawhubToken 掩码 ClawHub token
// 对应 Python: SkillManager._mask_clawhub_token(token)
func maskClawhubToken(token string) string {
	if token == "" {
		return ""
	}
	if len(token) <= 8 {
		return strings.Repeat("*", len(token))
	}
	return token[:4] + strings.Repeat("*", len(token)-8) + token[len(token)-4:]
}

// getSkillEvolutionPath 获取技能的演化记录文件路径
// 对应 Python: SkillManager._get_skill_evolution_path(skill_name)
func (sm *SkillManager) getSkillEvolutionPath(skillName string) string {
	localDir := sm.resolveLocalSkillDir(skillName)
	if localDir == "" {
		return ""
	}
	return filepath.Join(localDir, evolutionFilename)
}

// resolveLocalSkillDir 查找本地技能目录
// 对应 Python: SkillManager._resolve_local_skill_dir(skill_name)
// resolveLocalSkillDir 解析本地技能目录。
// 对应 Python: SkillManager._resolve_local_skill_dir(skill_name)
// 先尝试 skillsDir/skillName 直接路径，若不存在则遍历子目录通过 SKILL.md 的 name 字段匹配。
func (sm *SkillManager) resolveLocalSkillDir(skillName string) string {
	// 直接路径匹配
	candidate := filepath.Join(sm.skillsDir, skillName)
	if dirExists(candidate) {
		return candidate
	}
	// 回退：遍历子目录，通过 SKILL.md 的 name 字段匹配（对齐 Python）
	entries, err := os.ReadDir(sm.skillsDir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), "_") {
			continue
		}
		childDir := filepath.Join(sm.skillsDir, entry.Name())
		mdPath := sm.tryFindSkillFile(childDir)
		if mdPath == "" {
			continue
		}
		meta := sm.parseSkillMD(mdPath)
		if meta != nil {
			if toString(meta["name"]) == skillName {
				return childDir
			}
		}
	}
	return ""
}

// resolveSkillSource 确定技能来源
// 对应 Python: SkillManager._resolve_skill_source(skill_name)
func (sm *SkillManager) resolveSkillSource(skillName string) string {
	plugins := sm.GetInstalledPlugins()
	for _, p := range plugins {
		if toString(p["name"]) == skillName {
			return toString(p["marketplace"])
		}
	}
	return "local"
}

// scanLocalSkills 扫描本地技能目录
// 对应 Python: SkillManager._scan_local_skills()
func (sm *SkillManager) scanLocalSkills() []map[string]any {
	var skills []map[string]any
	entries, err := os.ReadDir(sm.skillsDir)
	if err != nil {
		return skills
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "_") || !entry.IsDir() {
			continue
		}
		childPath := filepath.Join(sm.skillsDir, entry.Name())
		mdPath := sm.tryFindSkillFile(childPath)
		if mdPath == "" {
			continue
		}
		meta := sm.parseSkillMD(mdPath)
		if meta != nil {
			name := toString(meta["name"])
			if name != "" {
				meta["source"] = sm.resolveSkillSource(name)
				meta["enabled"] = GetSkillEnabled(sm.state, name)
			}
			skills = append(skills, meta)
		}
	}
	return skills
}

// scanBuiltinSkills 扫描内置技能目录
// 对应 Python: SkillManager._scan_builtin_skills()
func (sm *SkillManager) scanBuiltinSkills() []map[string]any {
	builtinDir := getBuiltinSkillsDir()
	if !dirExists(builtinDir) {
		return nil
	}
	var skills []map[string]any
	entries, err := os.ReadDir(builtinDir)
	if err != nil {
		return skills
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		childPath := filepath.Join(builtinDir, entry.Name())
		mdPath := sm.tryFindSkillFile(childPath)
		if mdPath == "" {
			continue
		}
		meta := sm.parseSkillMD(mdPath)
		if meta != nil {
			name := toString(meta["name"])
			meta["source"] = "builtin"
			meta["is_builtin"] = true
			meta["is_builtin_source"] = true
			if name != "" {
				meta["enabled"] = GetSkillEnabled(sm.state, name)
			}
			skills = append(skills, meta)
		}
	}
	return skills
}

// scanMarketplaceSkills 扫描 marketplace 技能
// 对应 Python: SkillManager._scan_marketplace_skills()
func (sm *SkillManager) scanMarketplaceSkills() []map[string]any {
	if !dirExists(sm.marketplaceDir) {
		return nil
	}
	var skills []map[string]any
	repoEntries, err := os.ReadDir(sm.marketplaceDir)
	if err != nil {
		return skills
	}
	for _, repoEntry := range repoEntries {
		if !repoEntry.IsDir() {
			continue
		}
		repoDir := filepath.Join(sm.marketplaceDir, repoEntry.Name())
		pluginEntries, err := os.ReadDir(repoDir)
		if err != nil {
			continue
		}
		for _, pluginEntry := range pluginEntries {
			if !pluginEntry.IsDir() {
				continue
			}
			pluginDir := filepath.Join(repoDir, pluginEntry.Name())
			mdPath := sm.tryFindSkillFile(pluginDir)
			if mdPath == "" {
				continue
			}
			meta := sm.parseSkillMD(mdPath)
			if meta != nil {
				meta["source"] = repoEntry.Name()
				meta["marketplace"] = repoEntry.Name()
				meta["is_builtin"] = false
				meta["is_builtin_source"] = false
				name := toString(meta["name"])
				if name != "" {
					meta["enabled"] = GetSkillEnabled(sm.state, name)
				}
				skills = append(skills, meta)
			}
		}
	}
	return skills
}

// tryFindSkillFile 在目录中查找 SKILL.md 文件
// 对应 Python: SkillManager._try_find_skill_file(directory)
func (sm *SkillManager) tryFindSkillFile(dir string) string {
	candidates := []string{"SKILL.md", "skill.md", "Skill.md"}
	for _, name := range candidates {
		path := filepath.Join(dir, name)
		if fileExists(path) {
			return path
		}
	}
	return ""
}

// locateSkillDir 定位包含 SKILL.md 的目录（优先当前目录，再向下递归）
// 对应 Python: SkillManager._locate_skill_dir(path)
func (sm *SkillManager) locateSkillDir(dir string) string {
	// 优先在当前目录直接查找
	if f := sm.tryFindSkillFile(dir); f != "" {
		return filepath.Dir(f)
	}
	// 递归搜索子目录（匹配 Python 的 rglob("SKILL.md")）
	var found string
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || found != "" {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		lower := strings.ToLower(d.Name())
		if lower == "skill.md" {
			found = filepath.Dir(path)
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

// parseSkillMD 解析 SKILL.md 文件的 frontmatter 和 body
// 对应 Python: SkillManager._parse_skill_md(path)
func (sm *SkillManager) parseSkillMD(path string) map[string]any {
	if path == "" || !fileExists(path) {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	content := string(data)

	// 解析 YAML frontmatter
	meta := make(map[string]any)
	if strings.HasPrefix(content, "---") {
		end := strings.Index(content[3:], "---")
		if end >= 0 {
			frontmatter := content[3 : 3+end]
			meta = parseYAMLFrontmatter(frontmatter)
			meta["body"] = strings.TrimSpace(content[3+end+3:])
			meta["path"] = path
		}
	}
	return meta
}

// findSkillInDir 在指定目录下查找技能详情
func (sm *SkillManager) findSkillInDir(dir, name, marketplaceName string) (map[string]any, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("未找到 skill: %s", name)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "_") || !entry.IsDir() {
			continue
		}
		childPath := filepath.Join(dir, entry.Name())
		mdPath := sm.tryFindSkillFile(childPath)
		if mdPath == "" {
			continue
		}
		meta := sm.parseSkillMD(mdPath)
		if meta != nil && toString(meta["name"]) == name {
			// 字段转换以符合前端期望
			if body, ok := meta["body"]; ok {
				meta["content"] = body
				delete(meta, "body")
			}
			if p, ok := meta["path"]; ok {
				meta["file_path"] = p
				delete(meta, "path")
			}
			if marketplaceName != "" {
				meta["source"] = marketplaceName
				meta["marketplace"] = marketplaceName
				meta["is_builtin"] = false
				meta["is_builtin_source"] = false
			} else {
				source := sm.resolveSkillSource(name)
				meta["source"] = source
				// 对齐 Python：根据 source 判断 is_builtin/is_builtin_source
				isBuiltin := source == "builtin"
				meta["is_builtin"] = isBuiltin
				meta["is_builtin_source"] = isBuiltin
			}
			meta["has_evolutions"] = fileExists(filepath.Join(childPath, evolutionFilename))
			meta["enabled"] = GetSkillEnabled(sm.state, name)
			return meta, nil
		}
	}
	return nil, fmt.Errorf("未找到 skill: %s", name)
}

// syncMarketplaceRepos 同步 marketplace 仓库
// 对应 Python: SkillManager._sync_marketplace_repos()
func (sm *SkillManager) syncMarketplaceRepos(ctx context.Context) error {
	for _, m := range sm.getMarketplaces() {
		if !toBoolWithDefault(m["enabled"], true) {
			continue
		}
		name := toString(m["name"])
		url := toString(m["url"])
		repoDir := filepath.Join(sm.marketplaceDir, name)
		if dirExists(repoDir) {
			sm.gitPull(ctx, repoDir)
		} else {
			if err := sm.gitClone(ctx, url, repoDir); err != nil {
				logger.Warn(logComponent).Err(err).Str("url", url).Str("dir", repoDir).Msg("同步 marketplace 仓库 git clone 失败")
			}
		}
	}
	return nil
}

// gitClone 执行 git clone（委托给 git_ops.go 中的包级函数）。
func (sm *SkillManager) gitClone(ctx context.Context, url, dir string) error {
	return gitClone(ctx, url, dir)
}

// gitPull 执行 git pull（委托给 git_ops.go 中的包级函数）。
func (sm *SkillManager) gitPull(ctx context.Context, dir string) {
	_ = gitPull(ctx, dir)
}

// gitGetCommit 获取当前 commit hash（委托给 git_ops.go 中的包级函数）。
func (sm *SkillManager) gitGetCommit(dir string) string {
	hash, _ := gitGetCommit(dir)
	return hash
}

// safePathName 校验路径名称安全性
// 对应 Python: _safe_path_name(value, label)
func safePathName(value any, label string) (string, error) {
	raw := trimSpace(toString(value))
	if raw == "" {
		return "", fmt.Errorf("无效的 %s 名称", label)
	}
	if raw == "." || raw == ".." {
		return "", fmt.Errorf("无效的 %s 名称: %s", label, raw)
	}
	if strings.Contains(raw, "/") || strings.Contains(raw, "\\") {
		return "", fmt.Errorf("无效的 %s 名称: %s", label, raw)
	}
	if filepath.IsAbs(raw) {
		return "", fmt.Errorf("无效的 %s 名称: %s", label, raw)
	}
	return raw, nil
}

// logRejectedName 记录被拒绝的无效名称
// 对应 Python: _log_rejected_name(operation, label, value, exc)
func logRejectedName(operation, label string, value any, exc error) {
	logger.Warn(logComponent).
		Str("operation", operation).
		Str("label", label).
		Str("value", toString(value)).
		Err(exc).
		Msg("拒绝了无效名称")
}

// getBuiltinSkillsDir 获取内置技能目录
// 对应 Python: get_builtin_skills_dir()
func getBuiltinSkillsDir() string {
	if dir := os.Getenv("BUILTIN_SKILLS_DIR"); dir != "" {
		return dir
	}
	// 后续补充：从 package root 解析 resources/agent/workspace/skills
	return ""
}

// dirExists 检查目录是否存在
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// fileExists 检查文件是否存在
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// copyDir 递归复制目录
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
}

// generateUUID 生成 UUID
func generateUUID() string {
	// 对齐 Python: uuid.uuid4()，使用 crypto/rand 生成标准 UUIDv4 格式
	var uuid [16]byte
	_, _ = rand.Read(uuid[:])
	// 设置版本 4 和变体位
	uuid[6] = (uuid[6] & 0x0f) | 0x40 // 版本4
	uuid[8] = (uuid[8] & 0x3f) | 0x80 // 变体10
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		uuid[0:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:16])
}

// envInt 从环境变量读取整数，带默认值
func envInt(key string, defaultVal int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := fmt.Sscanf(v, "%d", new(int)); err == nil && n == 1 {
			var result int
			fmt.Sscanf(v, "%d", &result)
			return result
		}
	}
	return defaultVal
}

// envString 从环境变量读取字符串，带默认值
func envString(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

// parseYAMLFrontmatter 解析 YAML frontmatter（使用 gopkg.in/yaml.v3 完整解析）。
// 对齐 Python: yaml.safe_load(frontmatter)，补全默认字段和 tags/allowed_tools 类型转换。
func parseYAMLFrontmatter(text string) map[string]any {
	result := make(map[string]any)
	// 使用 yaml.v3 解析（对齐 Python: yaml.safe_load）
	if err := yaml.Unmarshal([]byte(text), &result); err != nil {
		// 解析失败时回退到逐行解析
		return parseYAMLFrontmatterFallback(text)
	}
	// 补全默认字段（对齐 Python: tags 默认空列表, allowed_tools 默认空列表）
	if _, ok := result["tags"]; !ok {
		result["tags"] = []any{}
	}
	if _, ok := result["allowed_tools"]; !ok {
		result["allowed_tools"] = []any{}
	}
	return result
}

// parseYAMLFrontmatterFallback 逐行解析 YAML frontmatter（yaml.v3 解析失败时回退）
func parseYAMLFrontmatterFallback(text string) map[string]any {
	result := make(map[string]any)
	lines := strings.Split(strings.TrimSpace(text), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		colonIdx := strings.Index(line, ":")
		if colonIdx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:colonIdx])
		val := strings.TrimSpace(line[colonIdx+1:])
		// 去除引号
		if len(val) >= 2 && ((val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'')) {
			val = val[1 : len(val)-1]
		}
		result[key] = val
	}
	return result
}

// toBoolWithDefault 将 any 转为 bool，带默认值
func toBoolWithDefault(v any, defaultVal bool) bool {
	if v == nil {
		return defaultVal
	}
	switch val := v.(type) {
	case bool:
		return val
	case nil:
		return defaultVal
	default:
		return toBool(v)
	}
}

// toStringWithDefault 将 any 转为 string，带默认值
func toStringWithDefault(v any, defaultVal string) string {
	s := toString(v)
	if s == "" {
		return defaultVal
	}
	return s
}

// mapSliceToAny 将 []map[string]any 转为 []any，保持 JSON 兼容性
func mapSliceToAny(items []map[string]any) []any {
	result := make([]any, len(items))
	for i, m := range items {
		result[i] = m
	}
	return result
}

// safeExtractZIPBytesToDir 安全解压 ZIP 字节到目标目录（防 Zip Slip）
// 对应 Python: SkillManager._safe_extract_zip_bytes_to_dir(zip_bytes, dest_dir)
func safeExtractZIPBytesToDir(zipBytes []byte, destDir string) error {
	reader, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return fmt.Errorf("ZIP 读取失败: %w", err)
	}

	for _, f := range reader.File {
		// Zip Slip 防护：检查解压路径不超出目标目录
		targetPath := filepath.Join(destDir, f.Name)
		relPath, err := filepath.Rel(destDir, targetPath)
		if err != nil {
			return fmt.Errorf("路径解析失败: %w", err)
		}
		if strings.HasPrefix(relPath, "..") {
			return fmt.Errorf("zip slip 检测：路径 %q 超出目标目录", f.Name)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(targetPath, f.Mode()); err != nil {
				return err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}
		data, readErr := io.ReadAll(rc)
		_ = rc.Close()
		if readErr != nil {
			return readErr
		}
		if err := os.WriteFile(targetPath, data, f.Mode()); err != nil {
			return err
		}
	}
	return nil
}

// teamSkillsHubHTTPGet 向 TeamSkillsHub 发送 GET 请求
// 对应 Python: SkillManager._team_skills_hub_http_get_data(path, params, timeout, base_url)
func (sm *SkillManager) teamSkillsHubHTTPGet(ctx context.Context, path string, params url.Values, timeout int, baseURL string) (map[string]any, error) {
	if baseURL == "" {
		baseURL = envString(teamSkillsHubBaseURLEnv, teamSkillsHubDefaultBaseURL)
	}
	if timeout <= 0 {
		timeout = envInt(teamSkillsHubTimeoutEnv, teamSkillsHubDefaultTimeout)
	}

	reqURL, _ := url.Parse(baseURL + path)
	if params != nil {
		reqURL.RawQuery = params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("构建请求失败: %w", err)
	}

	client := &http.Client{Timeout: time.Duration(timeout) * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("网络请求失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API 返回状态码: %d", resp.StatusCode)
	}

	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("JSON 解析失败: %w", err)
	}

	// 检查业务状态码
	code, _ := payload["code"].(float64)
	if int(code) != 200 {
		return nil, fmt.Errorf("API 业务错误，code=%v, detail=%v", code, payload["detail"])
	}

	data, _ := payload["data"].(map[string]any)
	return data, nil
}

// assertTeamSkillsHubDownloadURLAllowed 校验下载 URL 主机名是否在白名单中
// 对应 Python: SkillManager._assert_team_skills_hub_download_url_allowed(download_url)
func (sm *SkillManager) assertTeamSkillsHubDownloadURLAllowed(downloadURL string) error {
	parsed, err := url.Parse(downloadURL)
	if err != nil {
		return fmt.Errorf("URL 解析失败: %w", err)
	}
	host := parsed.Hostname()

	// 获取白名单
	allowedHosts := teamSkillsHubAllowedHostDefaults
	if envHosts := os.Getenv(teamSkillsHubAllowedHostsEnv); envHosts != "" {
		allowedHosts = strings.Split(envHosts, ",")
	}

	for _, pattern := range allowedHosts {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		if matchHost(host, pattern) {
			return nil
		}
	}
	return fmt.Errorf("下载 URL 主机名 %q 不在白名单中", host)
}

// matchHost 检查主机名是否匹配模式（支持 * 单段通配，对齐 Python _team_skills_hub_host_matches_rule）
// 例如 "openjiuwen-market.obs.*.myhuaweicloud.com" 匹配 "openjiuwen-market.obs.cn-north-4.myhuaweicloud.com"
func matchHost(host, pattern string) bool {
	if pattern == "*" {
		return true
	}
	// 后缀匹配（对齐 Python: rule.startswith(".") → host.endswith(rule)）
	if strings.HasPrefix(pattern, ".") {
		return strings.HasSuffix(host, pattern)
	}
	// 前缀通配 *.example.com → 匹配 foo.example.com 和 example.com
	if strings.HasPrefix(pattern, "*.") {
		suffix := pattern[2:]
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	// 按段逐段匹配（对齐 Python：段数必须相同，* 匹配任意单段）
	hostParts := strings.Split(host, ".")
	patternParts := strings.Split(pattern, ".")
	if len(hostParts) != len(patternParts) {
		return false
	}
	for i := range hostParts {
		if patternParts[i] == "*" {
			continue
		}
		if hostParts[i] != patternParts[i] {
			return false
		}
	}
	return true
}

// downloadZipAndVerify 下载 ZIP 并校验完整性
// 对应 Python: SkillManager._download_zip_and_verify(download_url, checksum_sha256)
func (sm *SkillManager) downloadZipAndVerify(ctx context.Context, downloadURL, checksumSHA256 string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, fmt.Errorf("构建请求失败: %w", err)
	}

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("下载失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("下载返回状态码: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	// 校验：非空
	if len(data) == 0 {
		return nil, fmt.Errorf("下载内容为空")
	}

	// 校验：ZIP 魔数（PK）
	if len(data) < 2 || data[0] != 'P' || data[1] != 'K' {
		return nil, fmt.Errorf("下载内容不是有效的 ZIP 文件")
	}

	// 校验：SHA256（如果提供了 checksum）
	if checksumSHA256 != "" {
		hash := sha256.Sum256(data)
		actual := hex.EncodeToString(hash[:])
		if !strings.EqualFold(actual, checksumSHA256) {
			return nil, fmt.Errorf("SHA256 校验失败: 期望 %s, 实际 %s", checksumSHA256, actual)
		}
	}

	// 校验：ZIP 完整性
	if _, err := zip.NewReader(bytes.NewReader(data), int64(len(data))); err != nil {
		return nil, fmt.Errorf("ZIP 完整性校验失败: %w", err)
	}

	return data, nil
}
