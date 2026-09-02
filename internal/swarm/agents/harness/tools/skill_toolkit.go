package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	tool "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
	skillpkg "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SkillToolkit 把 SkillManager 暴露成模型友好的工具集合。
// 对应 Python: SkillToolkit (jiuwenswarm/agents/harness/common/tools/skill_toolkits.py)
type SkillToolkit struct {
	// manager 技能管理器实例
	manager *skillpkg.SkillManager
}

// SkillSearchItem 搜索结果归一化项，替代 map[string]any 的动态结构。
// 对应 Python: SkillToolkit._normalize_search_item 返回的字典
type SkillSearchItem struct {
	// Name 技能名称
	Name string `json:"name"`
	// Description 技能描述
	Description string `json:"description"`
	// Source 来源标识
	Source string `json:"source"`
	// Identifier 来源无关的统一标识符
	Identifier string `json:"identifier"`
	// Installed 是否已安装
	Installed bool `json:"installed"`
	// Version 版本号
	Version string `json:"version"`
	// Author 作者
	Author string `json:"author"`
	// Score 评分（SkillNet 的 stars，其他来源为 nil）
	Score *int `json:"score"`
}

// InstalledItem 已安装技能展示信息，替代 map[string]any 的动态结构。
// 对应 Python: SkillToolkit._build_installed_item 返回的字典
type InstalledItem struct {
	// Name 技能名称
	Name string `json:"name"`
	// Description 技能描述
	Description string `json:"description"`
	// Source 来源标识
	Source string `json:"source"`
	// Identifier 来源无关的统一标识符
	Identifier string `json:"identifier"`
	// Installed 是否已安装
	Installed bool `json:"installed"`
	// Version 版本号
	Version string `json:"version"`
	// Author 作者
	Author string `json:"author"`
	// Score 评分（已安装项通常为 nil）
	Score *int `json:"score"`
	// SkillDir 技能目录路径
	SkillDir string `json:"skill_dir"`
	// SkillFile SKILL.md 文件路径
	SkillFile string `json:"skill_file"`
}

// ListInstalledResult 列出已安装技能的结果
type ListInstalledResult struct {
	// Success 是否成功
	Success bool `json:"success"`
	// Items 已安装技能列表
	Items []*InstalledItem `json:"items"`
	// Detail 详细信息
	Detail string `json:"detail"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// autoSource 自动来源
	autoSource = "auto"
	// defaultSource 默认来源
	defaultSource = "skillnet"
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentServer
)

// ──────────────────────────── 全局变量 ────────────────────────────

// supportedSources 支持的来源列表
var supportedSources = map[string]bool{
	"skillnet":      true,
	"clawhub":       true,
	"teamskillshub": true,
}

// installSourceByTarget 根据标识符形态推断安装来源
// 对应 Python: _INSTALL_SOURCE_BY_TARGET
var installSourceByTarget = []struct {
	pattern *regexp.Regexp
	source  string
}{
	{regexp.MustCompile(`^https?://`), "skillnet"},
	{regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`), "clawhub"},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ToMap 转换为 map[string]any，供 MapFunction 输出使用。
func (s *SkillSearchItem) ToMap() map[string]any {
	return map[string]any{
		"name":        s.Name,
		"description": s.Description,
		"source":      s.Source,
		"identifier":  s.Identifier,
		"installed":   s.Installed,
		"version":     s.Version,
		"author":      s.Author,
		"score":       s.Score,
	}
}

// ToMap 转换为 map[string]any，供 MapFunction 输出使用。
func (it *InstalledItem) ToMap() map[string]any {
	return map[string]any{
		"name":        it.Name,
		"description": it.Description,
		"source":      it.Source,
		"identifier":  it.Identifier,
		"installed":   it.Installed,
		"version":     it.Version,
		"author":      it.Author,
		"score":       it.Score,
		"skill_dir":   it.SkillDir,
		"skill_file":  it.SkillFile,
	}
}

// ToMap 转换为 map[string]any，供 MapFunction 输出使用。
func (r *ListInstalledResult) ToMap() map[string]any {
	items := make([]map[string]any, len(r.Items))
	for i, item := range r.Items {
		items[i] = item.ToMap()
	}
	return map[string]any{"success": r.Success, "items": items, "detail": r.Detail}
}

// NewSkillToolkit 创建 SkillToolkit 实例。
// 对应 Python: SkillToolkit.__init__(manager)
func NewSkillToolkit(manager *skillpkg.SkillManager) *SkillToolkit {
	return &SkillToolkit{manager: manager}
}

// GetTools 返回技能管理工具列表，供 agent 注册。
// 对应 Python: SkillToolkit.get_tools()
func (tk *SkillToolkit) GetTools() []tool.Tool {
	return []tool.Tool{
		tk.newSearchSkillTool(),
		tk.newInstallSkillTool(),
		tk.newUninstallSkillTool(),
	}
}

// SearchSkill 搜索技能，统一查询 SkillNet、ClawHub、TeamSkillsHub。
// 对应 Python: SkillToolkit.search_skill(query, source, limit)
func (tk *SkillToolkit) SearchSkill(ctx context.Context, inputs map[string]any) (result map[string]any, err error) {
	// 顶层 panic 恢复（对齐 Python: except Exception）
	defer func() {
		if r := recover(); r != nil {
			logger.Error(logComponent).Any("panic", r).Msg("search_skill panicked")
			result = map[string]any{"success": false, "source": toString(inputs["source"]), "items": []any{}, "detail": fmt.Sprintf("internal error: %v", r)}
			err = nil
		}
	}()

	query := strings.TrimSpace(toString(inputs["query"]))
	source := strings.TrimSpace(toString(inputs["source"]))
	if source == "" {
		source = defaultSource
	}
	limit := safeInt(inputs["limit"], 10)

	logger.Info(logComponent).
		Str("query", query).
		Str("source", source).
		Int("limit", limit).
		Msg("SkillToolkit: search_skill 调用")

	normalizedSource, err := normalizeSource(source)
	if err != nil {
		return map[string]any{
			"success": false, "source": source, "items": []any{}, "detail": err.Error(),
		}, nil
	}

	if query == "" {
		return map[string]any{
			"success": false, "source": normalizedSource, "items": []any{}, "detail": "query is required",
		}, nil
	}

	installedNames := tk.getInstalledNames()

	// 确定搜索来源列表
	var sources []string
	if normalizedSource == autoSource {
		// 对齐 Python: sorted(_SUPPORTED_SOURCES)，动态从 supportedSources 构建
		for src := range supportedSources {
			sources = append(sources, src)
		}
		sort.Strings(sources)
	} else {
		sources = []string{normalizedSource}
	}

	var searchItems []*SkillSearchItem
	var errors []string
	anySuccess := false

	for _, currentSource := range sources {
		params := map[string]any{"q": query, "limit": limit}
		if currentSource == "skillnet" {
			params["mode"] = "vector"
		}

		var payload map[string]any
		switch currentSource {
		case "skillnet":
			payload, _ = tk.manager.HandleSkillsSkillnetSearch(ctx, params)
		case "clawhub":
			payload, _ = tk.manager.HandleSkillsClawhubSearch(ctx, params)
		case "teamskillshub":
			payload, _ = tk.manager.HandleSkillsTeamSkillsHubSearch(ctx, params)
		default:
			continue
		}

		summary := summarizeSearchPayload(currentSource, query, payload)
		logger.Info(logComponent).
			Str("source", currentSource).
			Interface("summary", summary).
			Msg("SkillToolkit: 搜索结果摘要")

		if !toBool(payload["success"]) {
			detail := strings.TrimSpace(toString(payload["detail"]))
			if detail == "" {
				detail = currentSource + " search failed"
			}
			errors = append(errors, currentSource+": "+detail)
			continue
		}
		anySuccess = true

		if skills, ok := toSliceOfAny(payload["skills"]); ok {
			for _, rawItem := range skills {
				if m, ok := rawItem.(map[string]any); ok {
					searchItems = append(searchItems, normalizeSearchItem(m, currentSource, installedNames))
				}
			}
		}
	}

	detail := strings.Join(errors, "; ")
	if len(searchItems) == 0 {
		noResultDetail := fmt.Sprintf(
			"No skills found from %s for query %q. Underlying search returned success but an empty skills list.",
			normalizedSource, query,
		)
		if anySuccess && detail != "" {
			detail = noResultDetail + " Partial source errors: " + detail
		} else if detail == "" {
			detail = noResultDetail
		}
	}

	// 截断到 limit
	if len(searchItems) > limit {
		searchItems = searchItems[:limit]
	}

	// 转换为 map 输出
	items := make([]map[string]any, len(searchItems))
	for i, si := range searchItems {
		items[i] = si.ToMap()
	}

	return map[string]any{
		"success":       anySuccess,
		"source":        normalizedSource,
		"items":         items,
		"detail":        detail,
		"query_summary": fmt.Sprintf("search query=%q source=%s limit=%d", query, normalizedSource, limit),
	}, nil
}

// InstallSkill 安装技能，需要显式指定来源。
// 对应 Python: SkillToolkit.install_skill(identifier, source, timeout_sec)
func (tk *SkillToolkit) InstallSkill(ctx context.Context, inputs map[string]any) (result map[string]any, err error) {
	// 顶层 panic 恢复（对齐 Python: except Exception）
	defer func() {
		if r := recover(); r != nil {
			logger.Error(logComponent).Any("panic", r).Msg("install_skill panicked")
			result = map[string]any{"success": false, "source": toString(inputs["source"]), "installed": false, "detail": fmt.Sprintf("internal error: %v", r)}
			err = nil
		}
	}()

	identifier := strings.TrimSpace(toString(inputs["identifier"]))
	source := strings.TrimSpace(toString(inputs["source"]))
	timeoutSec := safeInt(inputs["timeout_sec"], 60)

	logger.Info(logComponent).
		Str("identifier", identifier).
		Str("source", source).
		Int("timeout_sec", timeoutSec).
		Msg("SkillToolkit: install_skill 调用")

	if identifier == "" {
		return map[string]any{
			"success": false, "source": source, "installed": false,
			"detail": "identifier is required",
		}, nil
	}
	if source == "" {
		return map[string]any{
			"success": false, "source": source, "installed": false,
			"detail": "source is required and must be one of: 'skillnet', 'clawhub', 'teamskillshub'",
		}, nil
	}

	normalizedSource, err := normalizeSource(source)
	if err != nil {
		return map[string]any{
			"success": false, "source": source, "installed": false, "detail": err.Error(),
		}, nil
	}
	if normalizedSource == autoSource {
		return map[string]any{
			"success": false, "source": normalizedSource, "installed": false,
			"detail": "source must be explicitly set to 'skillnet', 'clawhub', or 'teamskillshub'",
		}, nil
	}

	// 检查是否已安装
	existingItem := tk.findInstalledByTarget(identifier, normalizedSource)
	if existingItem != nil {
		detail := fmt.Sprintf(
			"Skill `%s` is already installed. Skipping duplicate installation.",
			existingItem.Name,
		)
		return map[string]any{
			"success":           true,
			"source":            normalizedSource,
			"installed":         true,
			"already_installed": true,
			"name":              existingItem.Name,
			"description":       existingItem.Description,
			"identifier":        existingItem.Identifier,
			"skill_file":        existingItem.SkillFile,
			"detail":            detail,
		}, nil
	}

	// 执行安装
	var payload map[string]any
	switch normalizedSource {
	case "skillnet":
		payload = tk.installSkillnetSyncWait(ctx, identifier, timeoutSec)
	case "teamskillshub":
		installParams := map[string]any{"asset_id": identifier, "force": false}
		payload, _ = tk.manager.HandleSkillsTeamSkillsHubInstall(ctx, installParams)
	default: // clawhub 来源
		payload, _ = tk.manager.HandleSkillsClawhubDownload(ctx, map[string]any{
			"slug": identifier, "force": false,
		})
	}

	if !toBool(payload["success"]) {
		detail := strings.TrimSpace(toString(payload["detail"]))
		if detail == "" {
			detail = "skill installation failed"
		}
		return map[string]any{
			"success": false, "source": normalizedSource, "installed": false, "detail": detail,
		}, nil
	}

	// 构建安装结果
	skill, _ := payload["skill"].(map[string]any)
	name := strings.TrimSpace(toString(skill["name"]))
	if name == "" {
		// 对齐 Python: Path(target).name if resolved_source == "skillnet" else target
		if normalizedSource == "skillnet" {
			name = path.Base(identifier)
		} else {
			name = identifier
		}
	}

	installedItem := tk.buildInstalledItem(name, normalizedSource)
	desc := strings.TrimSpace(installedItem.Description)
	if desc == "" {
		desc = "No description provided."
	}
	detail := fmt.Sprintf("Skill installed successfully. Available now: - `%s`: %s", installedItem.Name, desc)
	if installedItem.SkillFile != "" {
		detail = detail + " Read SKILL.md before use."
	}

	logger.Info(logComponent).
		Str("name", installedItem.Name).
		Str("source", normalizedSource).
		Str("skill_dir", installedItem.SkillDir).
		Msg("SkillToolkit: install_skill 成功")

	return map[string]any{
		"success":     true,
		"source":      normalizedSource,
		"installed":   true,
		"name":        installedItem.Name,
		"description": installedItem.Description,
		"identifier":  installedItem.Identifier,
		"skill_file":  installedItem.SkillFile,
		"detail":      detail,
	}, nil
}

// UninstallSkill 卸载技能。
// 对应 Python: SkillToolkit.uninstall_skill(name)
func (tk *SkillToolkit) UninstallSkill(ctx context.Context, inputs map[string]any) (result map[string]any, err error) {
	// 顶层 panic 恢复（对齐 Python: except Exception）
	defer func() {
		if r := recover(); r != nil {
			logger.Error(logComponent).Any("panic", r).Msg("uninstall_skill panicked")
			result = map[string]any{"success": false, "removed": false, "name": toString(inputs["name"]), "detail": fmt.Sprintf("internal error: %v", r)}
			err = nil
		}
	}()

	skillName := strings.TrimSpace(toString(inputs["name"]))

	logger.Info(logComponent).
		Str("name", skillName).
		Msg("SkillToolkit: uninstall_skill 调用")

	if skillName == "" {
		return map[string]any{
			"success": false, "removed": false, "detail": "name is required",
		}, nil
	}

	// 检查是否为内置技能
	if tk.manager.IsBuiltinSkill(skillName) {
		return map[string]any{
			"success": false, "removed": false, "name": skillName,
			"detail": "Built-in skills cannot be uninstalled.",
		}, nil
	}

	// 检查是否已安装
	installedResult := tk.listInstalledSkills(ctx)
	if !installedResult.Success {
		return map[string]any{
			"success": false, "removed": false, "name": skillName,
			"detail": strings.TrimSpace(installedResult.Detail),
		}, nil
	}

	var foundSource string
	for _, item := range installedResult.Items {
		if item.Name == skillName {
			foundSource = item.Source
			break
		}
	}

	if foundSource == "" {
		return map[string]any{
			"success": false, "removed": false, "name": skillName,
			"detail": fmt.Sprintf("Skill `%s` is not installed.", skillName),
		}, nil
	}

	// 执行卸载
	payload, err := tk.manager.HandleSkillsUninstall(ctx, map[string]any{"name": skillName})
	if err != nil {
		logger.Error(logComponent).Str("name", skillName).Err(err).Msg("uninstall_skill failed")
		return map[string]any{
			"success": false, "removed": false, "name": skillName,
			"detail": err.Error(),
		}, nil
	}

	if !toBool(payload["success"]) {
		return map[string]any{
			"success": false, "removed": false, "name": skillName,
			"detail": strings.TrimSpace(toString(payload["detail"])),
		}, nil
	}

	return map[string]any{
		"success": true, "removed": true, "name": skillName,
		"source": foundSource,
		"detail": fmt.Sprintf("Skill `%s` uninstalled successfully.", skillName),
	}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// normalizeSource 规范化来源字符串。
// 对应 Python: SkillToolkit._normalize_source(source)
func normalizeSource(source string) (string, error) {
	value := strings.TrimSpace(strings.ToLower(source))
	if value == "" {
		value = defaultSource
	}
	if supportedSources[value] || value == autoSource {
		return value, nil
	}
	return "", fmt.Errorf("unsupported source: %s", source)
}

// detectSource 根据标识符形态推断来源。
// 对应 Python: SkillToolkit._detect_source(target)
func detectSource(target string) (string, error) {
	raw := strings.TrimSpace(target)
	if raw == "" {
		return "", fmt.Errorf("identifier 不能为空")
	}
	for _, rule := range installSourceByTarget {
		if rule.pattern.MatchString(raw) {
			return rule.source, nil
		}
	}
	return "", fmt.Errorf("无法从 identifier 推断来源: %s", target)
}

// safeInt 安全地转换为整数，失败时返回默认值。
// 对应 Python: SkillToolkit._safe_int(value, default)
func safeInt(value any, defaultVal int) int {
	switch v := value.(type) {
	case int:
		if v < 1 {
			return defaultVal
		}
		return v
	case float64:
		result := int(v)
		if result < 1 {
			return defaultVal
		}
		return result
	case string:
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err != nil || n < 1 {
			return defaultVal
		}
		return n
	default:
		return defaultVal
	}
}

// getInstalledNames 返回已安装技能名称集合。
// 对应 Python: SkillToolkit._get_installed_names()
func (tk *SkillToolkit) getInstalledNames() map[string]bool {
	names := make(map[string]bool)
	for _, m := range tk.manager.GetInstalledPlugins() {
		name := strings.TrimSpace(toString(m["name"]))
		if name != "" {
			names[name] = true
		}
	}
	return names
}

// findInstalledByTarget 按统一 identifier 反查是否已安装。
// 对应 Python: SkillToolkit._find_installed_by_target(identifier, source)
func (tk *SkillToolkit) findInstalledByTarget(identifier, source string) *InstalledItem {
	target := strings.TrimSpace(identifier)
	if target == "" {
		return nil
	}

	// 在 local_skills 中查找
	for _, m := range tk.manager.GetLocalSkills() {
		name := strings.TrimSpace(toString(m["name"]))
		origin := strings.TrimSpace(toString(m["origin"]))
		localSource := strings.TrimSpace(toString(m["source"]))

		if source == "skillnet" && localSource == "skillnet" && origin == target {
			return tk.buildInstalledItem(name, "skillnet")
		}
		if source == "clawhub" && localSource == "clawhub" {
			if origin == "clawhub:"+target || origin == target || name == target {
				return tk.buildInstalledItem(name, "clawhub")
			}
		}
		if source == "teamskillshub" && localSource == "teamskillshub" {
			if origin == "teamskillshub:"+target || origin == target || name == target {
				return tk.buildInstalledItem(name, "teamskillshub")
			}
		}
	}

	// 在 installed_plugins 中查找
	for _, p := range tk.manager.GetInstalledPlugins() {
		name := strings.TrimSpace(toString(p["name"]))
		marketplace := strings.TrimSpace(toString(p["marketplace"]))
		pluginSource := strings.TrimSpace(toString(p["source"]))
		normalizedSource := pluginSource
		if normalizedSource == "" {
			normalizedSource = marketplace
		}

		if source == "clawhub" && normalizedSource == "clawhub" && name == target {
			return tk.buildInstalledItem(name, "clawhub")
		}
		if source == "skillnet" && normalizedSource == "skillnet" && name == target {
			return tk.buildInstalledItem(name, "skillnet")
		}
		if source == "teamskillshub" && normalizedSource == "teamskillshub" && name == target {
			return tk.buildInstalledItem(name, "teamskillshub")
		}
	}

	return nil
}

// buildInstalledItem 补齐已安装技能的展示信息。
// 对应 Python: SkillToolkit._build_installed_item(name, source)
func (tk *SkillToolkit) buildInstalledItem(name, source string) *InstalledItem {
	meta := tk.manager.GetSkillMeta(name)
	if meta == nil {
		meta = make(map[string]any)
	}
	description := strings.TrimSpace(toString(meta["description"]))
	skillDir := toString(meta["skill_dir"])
	skillFile := toString(meta["skill_file"])

	// 从 local_skills 中查找 origin 作为 identifier
	identifier := ""
	for _, m := range tk.manager.GetLocalSkills() {
		if toString(m["name"]) == name {
			identifier = strings.TrimSpace(toString(m["origin"]))
			break
		}
	}

	// 清理来源前缀
	if source == "clawhub" && strings.HasPrefix(identifier, "clawhub:") {
		identifier = strings.TrimSpace(identifier[8:])
	}
	if source == "teamskillshub" && strings.HasPrefix(identifier, "teamskillshub:") {
		identifier = strings.TrimSpace(identifier[14:])
	}
	if identifier == "" {
		identifier = name
	}

	return &InstalledItem{
		Name:        name,
		Description: description,
		Source:      source,
		Identifier:  identifier,
		Installed:   true,
		Version:     toString(meta["version"]),
		Author:      toString(meta["author"]),
		Score:       nil,
		SkillDir:    skillDir,
		SkillFile:   skillFile,
	}
}

// normalizeSearchItem 将不同来源的搜索结果归一化。
// 对应 Python: SkillToolkit._normalize_search_item(item, source, installed_names)
func normalizeSearchItem(item map[string]any, source string, installedNames map[string]bool) *SkillSearchItem {
	var name, description, identifier, version, author string
	var score *int

	switch source {
	case "skillnet":
		name = strings.TrimSpace(toString(item["skill_name"]))
		description = strings.TrimSpace(toString(item["skill_description"]))
		identifier = strings.TrimSpace(toString(item["skill_url"]))
		version = ""
		author = strings.TrimSpace(toString(item["author"]))
		score = toIntPtr(item["stars"])
	case "teamskillshub":
		assetID := strings.TrimSpace(toString(item["asset_id"]))
		name = strings.TrimSpace(toString(item["display_name"]))
		if name == "" {
			name = strings.TrimSpace(toString(item["name"]))
		}
		if name == "" {
			name = assetID
		}
		description = strings.TrimSpace(toString(item["summary"]))
		identifier = assetID
		version = strings.TrimSpace(toString(item["version"]))
		author = ""
		score = nil
	default: // clawhub 来源
		name = strings.TrimSpace(toString(item["display_name"]))
		if name == "" {
			name = strings.TrimSpace(toString(item["slug"]))
		}
		description = strings.TrimSpace(toString(item["summary"]))
		identifier = strings.TrimSpace(toString(item["slug"]))
		version = strings.TrimSpace(toString(item["version"]))
		author = ""
		score = nil
	}

	return &SkillSearchItem{
		Name:        name,
		Description: description,
		Source:      source,
		Identifier:  identifier,
		Installed:   installedNames[name],
		Version:     version,
		Author:      author,
		Score:       score,
	}
}

// summarizeSearchPayload 提取搜索结果摘要，便于日志排查。
// 对应 Python: SkillToolkit._summarize_search_payload(source, query, payload)
func summarizeSearchPayload(source, query string, payload map[string]any) map[string]any {
	skills, _ := toSliceOfAny(payload["skills"])
	var first map[string]any
	if len(skills) > 0 {
		if m, ok := skills[0].(map[string]any); ok {
			first = m
		}
	}
	if first == nil {
		first = make(map[string]any)
	}
	return map[string]any{
		"source":  source,
		"query":   query,
		"success": toBool(payload["success"]),
		"count":   len(skills),
		"detail":  strings.TrimSpace(toString(payload["detail"])),
		"sample": map[string]any{
			"skill_name": toString(first["skill_name"]),
			"skill_url":  toString(first["skill_url"]),
			"asset_id":   toString(first["asset_id"]),
			"summary":    toString(first["skill_description"]),
		},
	}
}

// listInstalledSkills 列出已安装技能，供 toolkit 内部逻辑复用。
// 对应 Python: SkillToolkit._list_installed_skills()
func (tk *SkillToolkit) listInstalledSkills(ctx context.Context) *ListInstalledResult {
	logger.Info(logComponent).Msg("SkillToolkit: list_installed_skills 调用")

	payload, _ := tk.manager.HandleSkillsInstalled(ctx, map[string]any{})

	var pluginItems []*InstalledItem
	if plugins, ok := toSliceOfAny(payload["plugins"]); ok {
		for _, plugin := range plugins {
			if p, ok := plugin.(map[string]any); ok {
				name := strings.TrimSpace(toString(p["plugin_name"]))
				source := strings.TrimSpace(toString(p["marketplace"]))
				if source == "" {
					source = "local"
				}
				if name != "" {
					pluginItems = append(pluginItems, tk.buildInstalledItem(name, source))
				}
			}
		}
	}

	var items []*InstalledItem
	seen := make(map[string]bool)
	for _, item := range pluginItems {
		if item.Name == "" || seen[item.Name] {
			continue
		}
		seen[item.Name] = true
		items = append(items, item)
	}

	// 补充 local_skills 中未在 plugins 中出现的技能
	for _, m := range tk.manager.GetLocalSkills() {
		name := strings.TrimSpace(toString(m["name"]))
		if name == "" || seen[name] {
			continue
		}
		source := strings.TrimSpace(toString(m["source"]))
		if source == "" {
			source = "local"
		}
		seen[name] = true
		items = append(items, tk.buildInstalledItem(name, source))
	}

	return &ListInstalledResult{Success: true, Items: items, Detail: ""}
}

// installSkillnetSyncWait 在单次 tool 调用内轮询 SkillNet 安装状态，直到完成或超时。
// 对应 Python: SkillToolkit._install_skillnet_sync_wait(identifier, timeout_sec)
func (tk *SkillToolkit) installSkillnetSyncWait(ctx context.Context, identifier string, timeoutSec int) map[string]any {
	// 1. 发起安装
	payload, _ := tk.manager.HandleSkillsSkillnetInstall(ctx, map[string]any{"url": identifier, "force": false})
	if !toBool(payload["success"]) {
		return payload
	}
	if !toBool(payload["pending"]) {
		return payload
	}

	// 2. 提取 install_id
	installID := strings.TrimSpace(toString(payload["install_id"]))
	if installID == "" {
		return map[string]any{"success": false, "detail": "missing install_id from skillnet install"}
	}

	// 3. 轮询安装状态
	pollCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-pollCtx.Done():
			return map[string]any{
				"success": false,
				"detail":  fmt.Sprintf("skill installation timed out after %d seconds", timeoutSec),
			}
		case <-ticker.C:
			statusPayload, _ := tk.manager.HandleSkillsSkillnetInstallStatus(
				pollCtx, map[string]any{"install_id": installID},
			)
			if toString(statusPayload["status"]) != "pending" {
				if !toBool(statusPayload["success"]) {
					return statusPayload
				}
				return map[string]any{"success": true, "skill": statusPayload["skill"]}
			}
		}
	}
}

// newSearchSkillTool 创建 search_skill 工具
func (tk *SkillToolkit) newSearchSkillTool() *tool.MapFunction {
	card := tool.NewToolCard(
		"search_skill",
		"Search installable skills from SkillNet, ClawHub, and TeamSkillsHub. Use the returned identifier with install_skill (SkillNet URL, ClawHub slug, or TeamSkillsHub asset_id when source is teamskillshub).",
		[]*schema.Param{
			{
				Name:        "query",
				Description: "Search query for the skill.",
				Type:        schema.ParamTypeString,
				Required:    true,
			},
			{
				Name:        "source",
				Description: "Skill source to search. Defaults to skillnet. Use auto to search SkillNet, ClawHub, and TeamSkillsHub (teamskillshub).",
				Type:        schema.ParamTypeString,
				Required:    false,
				Default:     "skillnet",
				Enum:        []any{"auto", "skillnet", "clawhub", "teamskillshub"},
			},
			{
				Name:        "limit",
				Description: "Maximum number of skills to return.",
				Type:        schema.ParamTypeInteger,
				Required:    false,
				Default:     10,
			},
		},
		nil,
	)

	mf, _ := tool.NewMapFunction(card, tk.SearchSkill, nil)
	return mf
}

// newInstallSkillTool 创建 install_skill 工具
func (tk *SkillToolkit) newInstallSkillTool() *tool.MapFunction {
	card := tool.NewToolCard(
		"install_skill",
		"Install a skill using the identifier returned by search_skill. Returns the installed skill summary and where to read SKILL.md.",
		[]*schema.Param{
			{
				Name:        "identifier",
				Description: "Source-agnostic identifier returned by search_skill.",
				Type:        schema.ParamTypeString,
				Required:    true,
			},
			{
				Name:        "source",
				Description: "Explicit source matching search_skill items. Use teamskillshub for Team Skills Hub.",
				Type:        schema.ParamTypeString,
				Required:    true,
				Enum:        []any{"skillnet", "clawhub", "teamskillshub"},
			},
			{
				Name:        "timeout_sec",
				Description: "Installation timeout in seconds.",
				Type:        schema.ParamTypeInteger,
				Required:    false,
				Default:     60,
			},
		},
		nil,
	)

	mf, _ := tool.NewMapFunction(card, tk.InstallSkill, nil)
	return mf
}

// newUninstallSkillTool 创建 uninstall_skill 工具
func (tk *SkillToolkit) newUninstallSkillTool() *tool.MapFunction {
	card := tool.NewToolCard(
		"uninstall_skill",
		"Uninstall an installed skill by name.",
		[]*schema.Param{
			{
				Name:        "name",
				Description: "Installed skill name to remove.",
				Type:        schema.ParamTypeString,
				Required:    true,
			},
		},
		nil,
	)

	mf, _ := tool.NewMapFunction(card, tk.UninstallSkill, nil)
	return mf
}

// toString 安全地将 any 转为 string
func toString(v any) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case fmt.Stringer:
		return val.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}

// toBool 安全地将 any 转为 bool
func toBool(v any) bool {
	if v == nil {
		return false
	}
	switch val := v.(type) {
	case bool:
		return val
	case string:
		return val == "true" || val == "1"
	default:
		return false
	}
}

// toIntPtr 安全地将 any 转为 *int，用于 JSON 反序列化后的数值字段（float64）
func toIntPtr(v any) *int {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case float64:
		i := int(val)
		return &i
	case int:
		return &val
	case json.Number:
		if i, err := val.Int64(); err == nil {
			ii := int(i)
			return &ii
		}
	}
	return nil
}

// toSliceOfAny 安全地将 any 转为 []any
func toSliceOfAny(v any) ([]any, bool) {
	if v == nil {
		return nil, false
	}
	switch val := v.(type) {
	case []any:
		return val, true
	case []map[string]any:
		result := make([]any, len(val))
		for i, m := range val {
			result[i] = m
		}
		return result, true
	default:
		return nil, false
	}
}
