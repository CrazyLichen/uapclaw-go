package skill

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
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
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// errNotImplemented 后续补充方法未实现错误
	errNotImplemented = errors.New("功能尚未实现")
	// skillnetDownloadTimeout SkillNet 下载超时秒数
	skillnetDownloadTimeout = envInt(skillnetDownloadTimeoutEnv, 60)
	// skillnetMaxRetries SkillNet 最大重试次数
	skillnetMaxRetries = envInt(skillnetMaxRetriesEnv, 3)
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
	os.MkdirAll(sm.skillsDir, 0o755)

	sm.state = sm.loadState()
	return sm
}

// SetSkillnetInstallCompleteHook 设置安装成功落盘后回调（通常为重载 Agent 实例）
// 对应 Python: SkillManager.set_skillnet_install_complete_hook(hook)
func (sm *SkillManager) SetSkillnetInstallCompleteHook(hook func(ctx context.Context) error) {
	sm.skillnetInstallCompleteHook = hook
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

	rawPlugins := sm.getInstalledPlugins()
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

	os.MkdirAll(filepath.Dir(evoPath), 0o755)
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
		os.RemoveAll(dest)
	}
	if err := copyDir(pluginSrc, dest); err != nil {
		return map[string]any{"success": false, "detail": fmt.Sprintf("安装失败: %s", err)}, nil
	}

	// 解析元数据并记录
	meta := sm.parseSkillMD(sm.tryFindSkillFile(dest))
	commitHash := sm.gitGetCommit(repoDir)
	sm.addInstalledPlugin(map[string]any{
		"name":         safePlugin,
		"marketplace":  safeMarket,
		"version":      toString(meta["version"]),
		"commit":       commitHash,
		"source":       safeMarket,
		"installed_at": time.Now().UTC().Format(time.RFC3339),
	})
	sm.refreshAgentDataIndexes()
	sm.saveState()

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
	sm.addInstalledPlugin(map[string]any{
		"name":         safeName,
		"marketplace":  "builtin",
		"version":      toString(meta["version"]),
		"commit":       "",
		"source":       "builtin",
		"installed_at": time.Now().UTC().Format(time.RFC3339),
	})
	sm.refreshAgentDataIndexes()
	sm.saveState()
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

	if err := os.RemoveAll(dest); err != nil {
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
		os.RemoveAll(dest)
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
		"enabled": true,
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

	return map[string]any{"success": true, "name": name, "enabled": enabled}, nil
}

// HandleSkillsSkillnetSearch 在线搜索 SkillNet 技能
// 对应 Python: SkillManager.handle_skills_skillnet_search(params)
func (sm *SkillManager) HandleSkillsSkillnetSearch(ctx context.Context, params map[string]any) (map[string]any, error) {
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
	skillURL := trimSpace(toString(params["url"]))
	if skillURL == "" {
		return map[string]any{"success": false, "detail": "缺少参数: url"}, nil
	}

	installID := generateUUID()
	sm.mu.Lock()
	sm.skillnetInstallJobs[installID] = map[string]any{"status": "pending"}
	sm.mu.Unlock()

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

// HandleSkillsSkillnetEvaluate 使用 SkillNet 评估
// 对应 Python: SkillManager.handle_skills_skillnet_evaluate(params)
func (sm *SkillManager) HandleSkillsSkillnetEvaluate(ctx context.Context, params map[string]any) (map[string]any, error) {
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

	sm.setClawhubToken(token)
	sm.saveState()

	return map[string]any{
		"success": true,
		"token":   maskClawhubToken(token),
	}, nil
}

// HandleSkillsClawhubSearch 从 ClawHub 搜索技能
// 对应 Python: SkillManager.handle_skills_clawhub_search(params)
func (sm *SkillManager) HandleSkillsClawhubSearch(ctx context.Context, params map[string]any) (map[string]any, error) {
	return map[string]any{"success": false, "detail": errNotImplemented.Error()}, errNotImplemented
}

// HandleSkillsClawhubDownload 从 ClawHub 下载技能
// 对应 Python: SkillManager.handle_skills_clawhub_download(params)
func (sm *SkillManager) HandleSkillsClawhubDownload(ctx context.Context, params map[string]any) (map[string]any, error) {
	return map[string]any{"success": false, "detail": errNotImplemented.Error()}, errNotImplemented
}

// HandleSkillsTeamSkillsHubInfo 查询 Team Skills Hub 技能版本详情
// 对应 Python: SkillManager.handle_skills_team_skills_hub_info(params)
func (sm *SkillManager) HandleSkillsTeamSkillsHubInfo(ctx context.Context, params map[string]any) (map[string]any, error) {
	return map[string]any{"success": false, "detail": errNotImplemented.Error()}, errNotImplemented
}

// HandleSkillsTeamSkillsHubInit 初始化 TeamSkills 模板目录
// 对应 Python: SkillManager.handle_skills_team_skills_hub_init(params)
func (sm *SkillManager) HandleSkillsTeamSkillsHubInit(ctx context.Context, params map[string]any) (map[string]any, error) {
	return map[string]any{"success": false, "detail": errNotImplemented.Error()}, errNotImplemented
}

// HandleSkillsTeamSkillsHubValidate 校验 TeamSkills 目录结构与 SKILL.md 内容
// 对应 Python: SkillManager.handle_skills_team_skills_hub_validate(params)
func (sm *SkillManager) HandleSkillsTeamSkillsHubValidate(ctx context.Context, params map[string]any) (map[string]any, error) {
	return map[string]any{"success": false, "detail": errNotImplemented.Error()}, errNotImplemented
}

// HandleSkillsTeamSkillsHubPack 将 TeamSkills 目录打包为 zip
// 对应 Python: SkillManager.handle_skills_team_skills_hub_pack(params)
func (sm *SkillManager) HandleSkillsTeamSkillsHubPack(ctx context.Context, params map[string]any) (map[string]any, error) {
	return map[string]any{"success": false, "detail": errNotImplemented.Error()}, errNotImplemented
}

// HandleSkillsTeamSkillsHubSearch 从 Team Skills Hub 搜索技能
// 对应 Python: SkillManager.handle_skills_team_skills_hub_search(params)
func (sm *SkillManager) HandleSkillsTeamSkillsHubSearch(ctx context.Context, params map[string]any) (map[string]any, error) {
	return map[string]any{"success": false, "detail": errNotImplemented.Error()}, errNotImplemented
}

// HandleSkillsTeamSkillsHubInstall 从 Team Skills Hub 安装技能
// 对应 Python: SkillManager.handle_skills_team_skills_hub_install(params)
func (sm *SkillManager) HandleSkillsTeamSkillsHubInstall(ctx context.Context, params map[string]any) (map[string]any, error) {
	return map[string]any{"success": false, "detail": errNotImplemented.Error()}, errNotImplemented
}

// HandleSkillsTeamSkillsHubPublish 发布 TeamSkills
// 对应 Python: SkillManager.handle_skills_team_skills_hub_publish(params)
func (sm *SkillManager) HandleSkillsTeamSkillsHubPublish(ctx context.Context, params map[string]any) (map[string]any, error) {
	return map[string]any{"success": false, "detail": errNotImplemented.Error()}, errNotImplemented
}

// HandleSkillsTeamSkillsHubDelete 删除 TeamSkills
// 对应 Python: SkillManager.handle_skills_team_skills_hub_delete(params)
func (sm *SkillManager) HandleSkillsTeamSkillsHubDelete(ctx context.Context, params map[string]any) (map[string]any, error) {
	return map[string]any{"success": false, "detail": errNotImplemented.Error()}, errNotImplemented
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

// ──────────────────────────── 非导出函数 ────────────────────────────

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
	os.MkdirAll(filepath.Dir(sm.stateFile), 0o755)
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

// getInstalledPlugins 获取已安装插件列表
// 对应 Python: SkillManager._get_installed_plugins()
func (sm *SkillManager) getInstalledPlugins() []map[string]any {
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

// addInstalledPlugin 添加已安装插件记录
// 对应 Python: SkillManager._add_installed_plugin(plugin)
func (sm *SkillManager) addInstalledPlugin(plugin map[string]any) {
	plugins := sm.getInstalledPlugins()
	// 如果已存在同名插件，替换
	name := toString(plugin["name"])
	for i, p := range plugins {
		if toString(p["name"]) == name {
			plugins[i] = plugin
			sm.state["installed_plugins"] = mapSliceToAny(plugins)
			return
		}
	}
	plugins = append(plugins, plugin)
	sm.state["installed_plugins"] = mapSliceToAny(plugins)
}

// removeInstalledPlugin 移除已安装插件记录
func (sm *SkillManager) removeInstalledPlugin(name string) {
	plugins := sm.getInstalledPlugins()
	var filtered []map[string]any
	for _, p := range plugins {
		if toString(p["name"]) != name {
			filtered = append(filtered, p)
		}
	}
	sm.state["installed_plugins"] = mapSliceToAny(filtered)
}

// addLocalSkill 添加本地技能记录
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
	return p
}

// getClawhubToken 获取 ClawHub token
// 对应 Python: SkillManager._get_clawhub_token()
func (sm *SkillManager) getClawhubToken() string {
	return toString(sm.state[clawhubTokenKey])
}

// setClawhubToken 设置 ClawHub token
// 对应 Python: SkillManager._set_clawhub_token(token)
func (sm *SkillManager) setClawhubToken(token string) {
	sm.state[clawhubTokenKey] = token
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
func (sm *SkillManager) resolveLocalSkillDir(skillName string) string {
	candidate := filepath.Join(sm.skillsDir, skillName)
	if dirExists(candidate) {
		return candidate
	}
	return ""
}

// resolveSkillSource 确定技能来源
// 对应 Python: SkillManager._resolve_skill_source(skill_name)
func (sm *SkillManager) resolveSkillSource(skillName string) string {
	plugins := sm.getInstalledPlugins()
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
				meta["source"] = sm.resolveSkillSource(name)
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
			sm.gitClone(ctx, url, repoDir)
		}
	}
	return nil
}

// refreshAgentDataIndexes 刷新 Agent 数据索引
// 对应 Python: SkillManager._refresh_agent_data_indexes()
func (sm *SkillManager) refreshAgentDataIndexes() {
	// 后续补充：触发 Agent 重新加载技能索引
	logger.Debug(logComponent).Msg("刷新 Agent 数据索引（当前为空操作）")
}

// gitClone 执行 git clone
func (sm *SkillManager) gitClone(ctx context.Context, url, dir string) error {
	// 后续补充：实际 git clone 实现
	logger.Warn(logComponent).Str("url", url).Msg("git clone 尚未实现")
	return errNotImplemented
}

// gitPull 执行 git pull
func (sm *SkillManager) gitPull(ctx context.Context, dir string) {
	// 后续补充：实际 git pull 实现
	logger.Warn(logComponent).Str("dir", dir).Msg("git pull 尚未实现")
}

// gitGetCommit 获取当前 commit hash
func (sm *SkillManager) gitGetCommit(dir string) string {
	// 后续补充：实际 git rev-parse HEAD 实现
	return ""
}

// safePathName 校验路径名称安全性
// 对应 Python: _safe_path_name(value, label)
func safePathName(value any, label string) (string, error) {
	raw := trimSpace(toString(value))
	if raw == "" {
		return "", fmt.Errorf("invalid %s name", label)
	}
	if raw == "." || raw == ".." {
		return "", fmt.Errorf("invalid %s name: %s", label, raw)
	}
	if strings.Contains(raw, "/") || strings.Contains(raw, "\\") {
		return "", fmt.Errorf("invalid %s name: %s", label, raw)
	}
	if filepath.IsAbs(raw) {
		return "", fmt.Errorf("invalid %s name: %s", label, raw)
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
	// 简单实现：使用时间戳 + 随机数
	return fmt.Sprintf("%x", time.Now().UnixNano())
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

// parseYAMLFrontmatter 解析 YAML frontmatter（最小实现）
func parseYAMLFrontmatter(text string) map[string]any {
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
