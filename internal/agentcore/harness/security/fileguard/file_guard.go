package fileguard

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// FileGuardAxisDefaults 三轴的默认判定级别。
//
// Python: @dataclass(frozen=True) FileGuardAxisDefaults
type FileGuardAxisDefaults struct {
	// Read 读轴默认
	Read security.PermissionLevel
	// Write 写轴默认
	Write security.PermissionLevel
	// Exec 执行轴默认
	Exec security.PermissionLevel
}

// FileGuardPathRule 一条路径规则。
//
// Python: @dataclass(frozen=True) FileGuardPathRule
type FileGuardPathRule struct {
	// Path 归整后的路径（prefix 匹配）或 glob 模式
	Path string
	// Read 读轴级别
	Read security.PermissionLevel
	// Write 写轴级别
	Write security.PermissionLevel
	// Exec 执行轴级别
	Exec security.PermissionLevel
	// Match "prefix" 或 "glob"
	Match string
}

// EffectiveFileGuardConfig 加载期编译后的生效视图（不写回调用方 dict）。
//
// Python: @dataclass(frozen=True) EffectiveFileGuardConfig
type EffectiveFileGuardConfig struct {
	// Enabled 路径层是否启用
	Enabled bool
	// Mode "legacy" 或 "native"
	Mode string
	// Defaults 三轴默认
	Defaults FileGuardAxisDefaults
	// Paths 编译后的规则列表
	Paths []FileGuardPathRule
	// WorkspaceRoot 工作空间根（空串表示未解析）
	WorkspaceRoot string
}

// FileGuardChecker 路径防护判定器（Legacy / Native 共用入口）。
//
// Python: class FileGuardChecker
type FileGuardChecker struct {
	// effective 编译后的生效配置
	effective EffectiveFileGuardConfig
	// permissionConfig 原始权限配置（供 isShellTool 判断类别）
	permissionConfig map[string]any
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// modeLegacy 旧配置形态（无读写轴）
	modeLegacy = "legacy"
	// modeNative 新配置形态（三轴 + prefix/glob）
	modeNative = "native"
	// matchPrefix 前缀匹配
	matchPrefix = "prefix"
	// matchGlob glob 匹配
	matchGlob = "glob"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// levelOrder 严格度排序（数字越小越严）：deny > ask > allow
	levelOrder = map[security.PermissionLevel]int{
		security.PermissionLevelDeny:  0,
		security.PermissionLevelAsk:   1,
		security.PermissionLevelAllow: 2,
	}

	// pathClassTools 路径类工具（供 reportLegacyPathRulesAtLoad 用）
	pathClassTools = map[string]struct{}{
		"read_file": {}, "write_file": {}, "edit_file": {},
		"read_text_file": {}, "write_text_file": {},
		"write": {}, "read": {},
		"glob_file_search": {}, "glob": {}, "list_dir": {}, "list_files": {},
		"grep": {}, "search_replace": {},
	}

	// loadWarnedRuleIDsMu 加载期告警去重锁
	loadWarnedRuleIDsMu sync.Mutex
	// loadWarnedRuleIDs 已告警的规则 id
	loadWarnedRuleIDs = map[string]struct{}{}

	// winAbsReLegacy legacy 模式的 Windows 盘符正则
	winAbsReLegacy = regexp.MustCompile(`^[A-Za-z]:[\\/]`)

	// legacyPathAwareCommands legacy 模式的路径类命令集合。
	//
	// Python: file_guard.py 的 _PATH_AWARE_COMMANDS（26 项，比 path_extract.py
	// 的版本少 get-content/set-content 等 PowerShell 命令）
	legacyPathAwareCommands = map[string]struct{}{
		"cd": {}, "rm": {}, "cp": {}, "mv": {}, "mkdir": {}, "touch": {},
		"chmod": {}, "chown": {}, "cat": {}, "ls": {}, "dir": {}, "type": {},
		"del": {}, "rd": {}, "copy": {}, "move": {}, "md": {},
		"head": {}, "tail": {}, "more": {}, "less": {}, "vim": {}, "nano": {},
		"gedit": {}, "notepad": {},
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NormalizePathGuardConfig 把 permissions 与运行时 trusted_dirs 编译为生效配置。
//
// enabled 推导顺序（对齐 Python，不可调换）：
//  1. file_guard.enabled 显式给出 → 用之
//  2. external_directory 非空 或 trusted_dirs 非空 → true
//  3. 否则 false
//
// Python: normalize_path_guard_config(permissions, *, workspace_root=None, trusted_dirs=None)
func NormalizePathGuardConfig(permissions map[string]any, workspaceRoot string, trustedDirs []string) EffectiveFileGuardConfig {
	perms := permissions
	if perms == nil {
		perms = map[string]any{}
	}
	fg := mapOf(perms["file_guard"])
	ext := perms["external_directory"]
	hasExt := externalDirectoryNonEmpty(ext)
	hasTrusted := len(trustedDirs) > 0

	var enabled bool
	if explicit, ok := explicitEnabled(fg); ok {
		enabled = explicit
	} else if hasExt || hasTrusted {
		enabled = true
	}

	ws := ""
	if strings.TrimSpace(workspaceRoot) != "" {
		ws = posixNorm(workspaceRoot)
	}
	native := hasNativeFileGuard(fg, perms)

	if !enabled {
		mode := modeLegacy
		if native {
			mode = modeNative
		}
		return EffectiveFileGuardConfig{
			Enabled: false,
			Mode:    mode,
			Defaults: FileGuardAxisDefaults{
				Read:  security.PermissionLevelAsk,
				Write: security.PermissionLevelAsk,
				Exec:  security.PermissionLevelAsk,
			},
			Paths:         nil,
			WorkspaceRoot: ws,
		}
	}
	if native {
		return normalizeNative(fg, ext, ws, trustedDirs)
	}
	return normalizeLegacy(ext, ws, trustedDirs, pathListOf(fg["paths"]))
}

// BuildFileGuardChecker 编译配置；路径层关闭时返回 nil。
//
// Python: build_file_guard_checker(permissions, *, workspace_root=None, trusted_dirs=None)
func BuildFileGuardChecker(permissions map[string]any, workspaceRoot string, trustedDirs []string) *FileGuardChecker {
	perms := permissions
	if perms == nil {
		perms = map[string]any{}
	}
	ReportLegacyPathRulesAtLoad(perms)
	effective := NormalizePathGuardConfig(perms, workspaceRoot, trustedDirs)
	if !effective.Enabled {
		return nil
	}
	return &FileGuardChecker{effective: effective, permissionConfig: perms}
}

// ReportLegacyPathRulesAtLoad 加载期 WARN：tools 全为 path 类且 pattern 像
// 路径的 rules 宜迁到 file_guard.paths。返回被标记的规则 id 列表。
//
// Python: report_legacy_path_rules_at_load(permissions)
func ReportLegacyPathRulesAtLoad(permissions map[string]any) []string {
	rules, ok := permissions["rules"].([]any)
	if !ok {
		return nil
	}
	var flagged []string
	for _, item := range rules {
		rule, ok := item.(map[string]any)
		if !ok {
			continue
		}
		rawID, _ := rule["id"].(string)
		ruleID := strings.TrimSpace(rawID)

		toolsRaw, ok := rule["tools"].([]any)
		if !ok {
			continue
		}
		pattern, ok := rule["pattern"].(string)
		if !ok {
			continue
		}
		if len(toolsRaw) == 0 {
			continue
		}
		allPathTools := true
		toolsDesc := make([]string, 0, len(toolsRaw))
		for _, t := range toolsRaw {
			name, ok := t.(string)
			if !ok {
				allPathTools = false
				break
			}
			toolsDesc = append(toolsDesc, name)
			if _, hit := pathClassTools[name]; !hit {
				allPathTools = false
				break
			}
		}
		if !allPathTools || !looksLikePathPattern(pattern) {
			continue
		}
		flagged = append(flagged, ruleID)

		cacheKey := ruleID
		if cacheKey == "" {
			cacheKey = "__anon__:" + pattern
		}
		loadWarnedRuleIDsMu.Lock()
		_, warned := loadWarnedRuleIDs[cacheKey]
		if !warned {
			loadWarnedRuleIDs[cacheKey] = struct{}{}
		}
		loadWarnedRuleIDsMu.Unlock()
		if !warned {
			logger.Warn(logComponent).
				Str("id", ruleID).
				Strs("tools", toolsDesc).
				Str("pattern", pattern).
				Msg("file_guard.permissions.rules.path_class_deprecated hint=migrate_to_file_guard_paths")
		}
	}
	return flagged
}

// ExtractPathsLegacy legacy 模式的路径抽取：仅路径字符串，无 R/W/X。
//
// 供 Legacy 投影锁定现网行为，禁止与 Native 抽取混用。
//
// Python: extract_paths_legacy(tool_name, tool_args, workspace, permission_config=None)
func ExtractPathsLegacy(toolName string, toolArgs map[string]any, workspace string, permissionConfig map[string]any) []string {
	var paths []string
	if isShellTool(toolName, permissionConfig) {
		workdirRaw, _ := toolArgs["workdir"].(string)
		// Python: (workspace / str(workdir)).resolve()。pathlib 的 / 运算在右操作数
		// 为绝对路径时丢弃左侧，与 filepath.Join 不同，故需显式判断。
		workdirPath := workdirRaw
		if !pathIsAbs(workdirRaw) {
			workdirPath = filepath.Join(workspace, workdirRaw)
		}
		workdirResolved := resolveExtractPath(workdirPath)
		cmd := commandFromArgs(toolArgs)
		paths = extractPathsFromCommand(cmd, workdirResolved)
	} else if security.PathTools[toolName] {
		for _, s := range security.IterPathStrings(toolName, toolArgs) {
			raw := strings.TrimSpace(s)
			raw = strings.Trim(raw, `"`)
			raw = strings.Trim(raw, `'`)
			if raw == "" {
				continue
			}
			resolved := raw
			if !pathIsAbs(raw) {
				resolved = filepath.Join(workspace, raw)
			}
			paths = append(paths, resolveExtractPath(resolved))
		}
	}
	return paths
}

// Evaluate 评估路径层；无意见或全部 ALLOW 时返回 nil（不抬升 Pipeline A）。
//
// Python: FileGuardChecker.evaluate(tool_name, tool_args)
func (c *FileGuardChecker) Evaluate(toolName string, toolArgs map[string]any) *security.PermissionResult {
	if !c.effective.Enabled {
		return nil
	}
	workspace := c.effective.WorkspaceRoot
	if workspace == "" {
		logger.Debug(logComponent).Msg("file_guard.skip reason=no_workspace")
		return nil
	}

	accesses := c.collectAccesses(toolName, toolArgs)
	if len(accesses) == 0 {
		return nil
	}

	overall := security.PermissionLevelAllow
	hitExternal := make([]string, 0, len(accesses))
	matchedBits := make([]string, 0, len(accesses))

	for _, acc := range accesses {
		level, ruleID := c.resolveOne(acc.Path, acc.Action)
		overall = security.Strictest(overall, level)
		if level != security.PermissionLevelAllow {
			hitExternal = append(hitExternal, acc.Path)
			if ruleID != "" {
				matchedBits = append(matchedBits, ruleID)
			}
		}
	}
	if overall == security.PermissionLevelAllow {
		return nil
	}

	var reason, matched string
	if c.effective.Mode == modeLegacy {
		sample := accesses[0].Path
		if overall == security.PermissionLevelDeny {
			reason = "Access to paths outside workspace is denied: " + sample
		} else {
			reason = "Access to paths outside workspace requires approval: " + sample
		}
		matched = "external_directory.*"
	} else {
		hint := accesses[0].Path
		if len(hitExternal) > 0 {
			hint = hitExternal[0]
		}
		if overall == security.PermissionLevelDeny {
			reason = "file_guard denied: " + hint
		} else {
			reason = "file_guard requires approval: " + hint
		}
		if len(matchedBits) > 0 {
			matched = strings.Join(matchedBits, "|")
		} else {
			matched = "file_guard"
		}
	}

	// Python: external_paths=hit_external or None —— 空列表返回 nil 而非空切片
	var externalPaths []string
	if len(hitExternal) > 0 {
		externalPaths = hitExternal
	}

	return &security.PermissionResult{
		Permission:    overall,
		Reason:        reason,
		MatchedRule:   matched,
		ExternalPaths: externalPaths,
	}
}

// Enabled 路径层是否启用（实现 security.FileGuardEvaluator）。
//
// Python: FileGuardChecker.enabled（property）
func (c *FileGuardChecker) Enabled() bool { return c.effective.Enabled }

// Mode 判定模式（实现 security.FileGuardEvaluator）。
//
// Python: FileGuardChecker.mode（property）
func (c *FileGuardChecker) Mode() string { return c.effective.Mode }

// CollectAskAccesses 返回本次判定为 ASK 的访问，供「总是允许」按轴落盘。
//
// 路径为实际触达路径（不再上卷父目录）；仅含 ASK，不含 DENY/ALLOW。
// 去重键是 (path, action) 二元组，不是单纯 path。
//
// Python: FileGuardChecker.collect_ask_accesses(tool_name, tool_args)
func (c *FileGuardChecker) CollectAskAccesses(toolName string, toolArgs map[string]any) []security.PathAction {
	if !c.effective.Enabled || c.effective.WorkspaceRoot == "" {
		return nil
	}
	accesses := c.collectAccesses(toolName, toolArgs)
	out := make([]security.PathAction, 0, len(accesses))
	seen := make(map[string]struct{}, len(accesses))
	for _, acc := range accesses {
		level, _ := c.resolveOne(acc.Path, acc.Action)
		if level != security.PermissionLevelAsk {
			continue
		}
		key := acc.Path + "\x00" + string(acc.Action)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, security.PathAction{Path: acc.Path, Action: string(acc.Action)})
	}
	return out
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// collectAccesses 按模式抽取本次调用的路径访问。
func (c *FileGuardChecker) collectAccesses(toolName string, toolArgs map[string]any) []FileAccess {
	workspace := c.effective.WorkspaceRoot
	if workspace == "" {
		return nil
	}
	if c.effective.Mode == modeNative {
		return ExtractAccessesNative(toolName, toolArgs, workspace, c.permissionConfig)
	}
	paths := ExtractPathsLegacy(toolName, toolArgs, workspace, c.permissionConfig)
	action := toolDefaultAction(toolName, c.permissionConfig)
	out := make([]FileAccess, 0, len(paths))
	for _, p := range paths {
		out = append(out, FileAccess{Path: p, Action: action})
	}
	return out
}

// resolveOne 判定单个路径在给定轴上的级别，并返回命中的规则 id。
//
// 三处细节（对齐 Python _resolve_one）：
//  1. prefix 判定两层：字符串前缀 + containsPath（后者处理 Windows 盘符怪例）
//  2. 多个 prefix 命中时取**最长**前缀
//  3. glob 与 prefix 的级别一起取 min（严格者胜），但 rule_id 优先 prefix
//
// Python: FileGuardChecker._resolve_one(path, action)
func (c *FileGuardChecker) resolveOne(pathPosix string, action FileAction) (security.PermissionLevel, string) {
	var bestPrefixLen int
	var bestPrefixRule *FileGuardPathRule
	globHits := make([]security.PermissionLevel, 0, 4)

	for i := range c.effective.Paths {
		rule := &c.effective.Paths[i]
		if rule.Match == matchGlob {
			if matchGlobPattern(strings.ReplaceAll(rule.Path, `\`, "/"), pathPosix) {
				globHits = append(globHits, levelForAction(rule, action))
			}
			continue
		}
		prefix := strings.TrimSuffix(rule.Path, "/")
		if pathPosix == prefix || strings.HasPrefix(pathPosix, prefix+"/") {
			// also accept contains_path for Windows drive quirks
			if containsPath(prefix, pathPosix) || pathPosix == prefix || strings.HasPrefix(pathPosix, prefix+"/") {
				if bestPrefixRule == nil || len(prefix) > bestPrefixLen {
					bestPrefixLen = len(prefix)
					bestPrefixRule = rule
				}
			}
		}
	}

	candidates := make([]security.PermissionLevel, 0, 1+len(globHits))
	ruleID := ""
	if bestPrefixRule != nil {
		candidates = append(candidates, levelForAction(bestPrefixRule, action))
		ruleID = "file_guard:prefix:" + bestPrefixRule.Path
	}
	candidates = append(candidates, globHits...)
	if len(globHits) > 0 && ruleID == "" {
		ruleID = "file_guard:glob"
	}

	if len(candidates) > 0 {
		strictest := candidates[0]
		for _, lv := range candidates[1:] {
			if levelOrder[lv] < levelOrder[strictest] {
				strictest = lv
			}
		}
		return strictest, ruleID
	}

	// 未命中 paths → defaults
	switch action {
	case FileActionWrite:
		return c.effective.Defaults.Write, "file_guard:defaults"
	case FileActionExec:
		return c.effective.Defaults.Exec, "file_guard:defaults"
	}
	return c.effective.Defaults.Read, "file_guard:defaults"
}

// normalizeLegacy 编译 Legacy 模式的生效配置。
//
// 规则追加顺序（即优先级，resolveOne 取最长前缀）：
//  1. workspace 隐式全放行（三轴 allow）
//  2. 每个 trusted_dir（read/write allow、exec ask）
//  3. external_directory 具名键（allow → 三轴 allow 但 exec 为 ask；其他 → 三轴同 action）
//  4. file_guard.paths 中无 glob 的项（/add-dir 写入）
//
// Python: _normalize_legacy(*, ext, workspace_root, trusted_dirs, file_guard_paths=None)
func normalizeLegacy(ext any, workspaceRoot string, trustedDirs []string, fileGuardPaths []map[string]any) EffectiveFileGuardConfig {
	starAction := "ask"
	allowPrefixes := make([][2]string, 0, 4)
	switch v := ext.(type) {
	case string:
		starAction = v
	case map[string]any:
		if star, ok := v["*"].(string); ok {
			starAction = star
		}
		for key, action := range v {
			if key == "*" {
				continue
			}
			actionStr, ok := action.(string)
			if !ok {
				continue
			}
			if actionStr != "allow" && actionStr != "ask" && actionStr != "deny" {
				continue
			}
			allowPrefixes = append(allowPrefixes, [2]string{key, actionStr})
		}
	}
	// map 遍历无序，按 key 排序保证结果稳定
	sortPairs(allowPrefixes)

	defaults := axisFromStar(starAction)
	rules := make([]FileGuardPathRule, 0, 4+len(trustedDirs)+len(allowPrefixes))

	// 1. workspace 隐式全放行
	if workspaceRoot != "" {
		if rule := compilePathEntry(workspaceRoot, levelPtr("allow"), levelPtr("allow"), levelPtr("allow"), matchPrefix, security.PermissionLevelAsk); rule != nil {
			rules = append(rules, *rule)
		}
	}
	// 2. trusted_dirs
	for _, td := range trustedDirs {
		if rule := compilePathEntry(td, levelPtr("allow"), levelPtr("allow"), levelPtr("ask"), matchPrefix, security.PermissionLevelAsk); rule != nil {
			rules = append(rules, *rule)
		}
	}
	// 3. external_directory 具名键
	for _, item := range allowPrefixes {
		key, action := item[0], item[1]
		var rule *FileGuardPathRule
		if action == "allow" {
			rule = compilePathEntry(key, levelPtr("allow"), levelPtr("allow"), levelPtr("ask"), matchPrefix, security.PermissionLevelAsk)
		} else {
			rule = compilePathEntry(key, levelPtr(action), levelPtr(action), levelPtr(action), matchPrefix, security.PermissionLevelAsk)
		}
		if rule != nil {
			rules = append(rules, *rule)
		}
	}
	// 4. file_guard.paths 中无 glob 的项
	for _, item := range fileGuardPaths {
		if m, _ := item["match"].(string); m == matchGlob {
			continue
		}
		pathV, ok := item["path"].(string)
		if !ok || strings.TrimSpace(pathV) == "" {
			continue
		}
		readLevel := stringOrDefault(item["read"], "allow")
		writeLevel := stringOrDefault(item["write"], "allow")
		execLevel := stringOrDefault(item["exec"], "ask")
		if rule := compilePathEntry(pathV, levelPtr(readLevel), levelPtr(writeLevel), levelPtr(execLevel), matchPrefix, security.PermissionLevelAsk); rule != nil {
			rules = append(rules, *rule)
		}
	}

	return EffectiveFileGuardConfig{
		Enabled:       true,
		Mode:          modeLegacy,
		Defaults:      defaults,
		Paths:         rules,
		WorkspaceRoot: workspaceRoot,
	}
}

// normalizeNative 编译 Native 模式的生效配置。
//
// 规则追加顺序：
//  1. file_guard.paths 逐项（prefix 或 glob）
//  2. file_guard.workspace 绑定 workspaceRoot 的轴规则
//  3. 每个 trusted_dir（read/write allow、exec ask）
//  4. external_directory 具名键作迁移源（不覆盖已有同 path 的 prefix 规则）
//
// Python: _normalize_native(fg, *, ext, workspace_root, trusted_dirs)
func normalizeNative(fg map[string]any, ext any, workspaceRoot string, trustedDirs []string) EffectiveFileGuardConfig {
	rawDefaults := mapOf(fg["defaults"])
	var defaults FileGuardAxisDefaults
	if !hasAxisDict(rawDefaults) {
		switch v := ext.(type) {
		case map[string]any:
			star, _ := v["*"].(string)
			if star == "" {
				star = "ask"
			}
			defaults = axisFromStar(star)
		case string:
			defaults = axisFromStar(v)
		default:
			defaults = axisFromStar("ask")
		}
	} else {
		defaults = FileGuardAxisDefaults{
			Read:  parseFileGuardLevel(rawDefaults["read"], security.PermissionLevelAsk),
			Write: parseFileGuardLevel(rawDefaults["write"], security.PermissionLevelAsk),
			Exec:  parseFileGuardLevel(rawDefaults["exec"], security.PermissionLevelAsk),
		}
	}

	rules := make([]FileGuardPathRule, 0, 4)
	// 1. file_guard.paths
	for _, item := range pathListOf(fg["paths"]) {
		pathV, ok := item["path"].(string)
		if !ok || strings.TrimSpace(pathV) == "" {
			continue
		}
		match := matchPrefix
		if m, _ := item["match"].(string); m == matchGlob {
			match = matchGlob
		}
		rule := compilePathEntry(
			pathV,
			levelPtrOptional(item["read"]),
			levelPtrOptional(item["write"]),
			levelPtrOptional(item["exec"]),
			match,
			security.PermissionLevelAsk,
		)
		if rule != nil {
			rules = append(rules, *rule)
		}
	}
	// 2. file_guard.workspace 绑定 workspaceRoot
	if rule := compileWorkspaceAxisRule(fg["workspace"], workspaceRoot); rule != nil {
		rules = append(rules, *rule)
	}
	// 3. trusted_dirs
	for _, td := range trustedDirs {
		if rule := compilePathEntry(td, levelPtr("allow"), levelPtr("allow"), levelPtr("ask"), matchPrefix, security.PermissionLevelAsk); rule != nil {
			rules = append(rules, *rule)
		}
	}
	// 4. external_directory 迁移源
	if extMap, ok := ext.(map[string]any); ok {
		existing := make(map[string]struct{}, len(rules))
		for _, r := range rules {
			if r.Match == matchPrefix {
				existing[r.Path] = struct{}{}
			}
		}
		keys := make([]string, 0, len(extMap))
		for k := range extMap {
			keys = append(keys, k)
		}
		sortStrings(keys)
		for _, key := range keys {
			if key == "*" {
				continue
			}
			action, ok := extMap[key].(string)
			if !ok || (action != "allow" && action != "ask" && action != "deny") {
				continue
			}
			keyNorm := posixNormLoose(key)
			if _, dup := existing[keyNorm]; dup {
				continue
			}
			var rule *FileGuardPathRule
			if action == "allow" {
				rule = compilePathEntry(key, levelPtr("allow"), levelPtr("allow"), levelPtr("ask"), matchPrefix, security.PermissionLevelAsk)
			} else {
				rule = compilePathEntry(key, levelPtr(action), levelPtr(action), levelPtr(action), matchPrefix, security.PermissionLevelAsk)
			}
			if rule != nil {
				rules = append(rules, *rule)
				logger.Warn(logComponent).
					Str("key", key).
					Str("action", action).
					Msg("file_guard.migrate.external_directory_to_file_guard_paths")
			}
		}
	}

	return EffectiveFileGuardConfig{
		Enabled:       true,
		Mode:          modeNative,
		Defaults:      defaults,
		Paths:         rules,
		WorkspaceRoot: workspaceRoot,
	}
}

// compileWorkspaceAxisRule 把 file_guard.workspace 编译为绑定 workspaceRoot 的前缀规则。
// 无 workspaceRoot 时 WARN 并跳过（不应用该规则）。
//
// Python: _compile_workspace_axis_rule(workspace_cfg, *, workspace_root)
func compileWorkspaceAxisRule(workspaceCfg any, workspaceRoot string) *FileGuardPathRule {
	cfg, ok := workspaceCfg.(map[string]any)
	if !ok || !hasAxisDict(cfg) {
		return nil
	}
	if workspaceRoot == "" {
		logger.Warn(logComponent).
			Msg("file_guard.workspace.rule_skipped reason=no_workspace_root")
		return nil
	}
	return compilePathEntry(
		workspaceRoot,
		levelPtrOptional(cfg["read"]),
		levelPtrOptional(cfg["write"]),
		levelPtrOptional(cfg["exec"]),
		matchPrefix,
		security.PermissionLevelAsk,
	)
}

// compilePathEntry 编译一条路径规则。返回 nil 表示跳过。
//
// 过短前缀（归一化前后都不含 "/"）跳过两次，避免 "C:" 误匹配整盘。
//
// Python: _compile_path_entry(path_raw, *, read=None, write=None, exec_=None,
//
//	match="prefix", default_level=ASK)
func compilePathEntry(pathRaw string, read, write, execP *security.PermissionLevel, match string, defaultLevel security.PermissionLevel) *FileGuardPathRule {
	pathS := strings.TrimSpace(pathRaw)
	if pathS == "" || pathS == "*" {
		return nil
	}
	r := defaultLevel
	if read != nil {
		r = *read
	}
	w := defaultLevel
	if write != nil {
		w = *write
	}
	e := defaultLevel
	if execP != nil {
		e = *execP
	}
	r, w, e = applyImplications(r, w, e, pathS)

	if match == matchPrefix {
		// 第一次判断：原始串（过短前缀跳过，避免 "C:" 误匹配整盘）
		norm := strings.TrimSuffix(strings.ReplaceAll(pathS, `\`, "/"), "/")
		if !strings.Contains(norm, "/") {
			return nil
		}
		// 归一化
		pathS = posixNormLoose(pathS)
		// 第二次判断：归一化后
		if !strings.Contains(strings.TrimSuffix(pathS, "/"), "/") {
			return nil
		}
	} else {
		// glob 模式不走归一化（模式本身不是真实路径）
		pathS = strings.TrimSpace(pathRaw)
	}
	return &FileGuardPathRule{Path: pathS, Read: r, Write: w, Exec: e, Match: match}
}

// applyImplications 应用 Write⇒Read / Exec⇒Read 蕴含。
// 显式更严的 read（deny）优先于蕴含，并打 WARN。
//
// Python 原文（file_guard.py:110-131）：
//
//	if write == ALLOW or exec_ == ALLOW:
//	    if read == DENY:
//	        logger.warning("implication.conflict ... (explicit deny wins over Write/Exec⇒Read)")
//	        return read, write, exec_
//	    return ALLOW, write, exec_
//	return read, write, exec_
func applyImplications(read, write, execP security.PermissionLevel, pathLabel string) (security.PermissionLevel, security.PermissionLevel, security.PermissionLevel) {
	if write == security.PermissionLevelAllow || execP == security.PermissionLevelAllow {
		if read == security.PermissionLevelDeny {
			logger.Warn(logComponent).
				Str("path", pathLabel).
				Str("write", write.String()).
				Str("exec", execP.String()).
				Msg("file_guard.implication.conflict explicit_read_deny_wins")
			return read, write, execP
		}
		return security.PermissionLevelAllow, write, execP
	}
	return read, write, execP
}

// hasAxisDict 判定 map 是否含 read/write/exec 任一轴键。
//
// Python: _has_axis_dict(raw)
func hasAxisDict(v any) bool {
	m, ok := v.(map[string]any)
	if !ok {
		return false
	}
	for _, k := range []string{"read", "write", "exec"} {
		if _, has := m[k]; has {
			return true
		}
	}
	return false
}

// hasNativeFileGuard 判定是否进入 Native 语义。
//
// 五条规则（顺序不可调换，对齐 Python _has_native_file_guard）：
//  1. 显式 defaults 轴 → Native
//  2. 显式 workspace 轴 → Native
//  3. paths 非空且含 match: glob → Native
//  4. paths 非空、无 glob、且存在 external_directory → Legacy
//  5. paths 非空、无 glob、无 external_directory → Native
func hasNativeFileGuard(fg, permissions map[string]any) bool {
	if hasAxisDict(fg["defaults"]) || hasAxisDict(fg["workspace"]) {
		return true
	}
	paths := pathListOf(fg["paths"])
	if len(paths) == 0 {
		return false
	}
	for _, p := range paths {
		if m, ok := p["match"].(string); ok && m == matchGlob {
			return true
		}
	}
	if externalDirectoryNonEmpty(permissions["external_directory"]) {
		return false
	}
	return true
}

// explicitEnabled 读 file_guard.enabled；键不存在返回 (false, false)。
//
// Python: _explicit_enabled(fg) 返回 bool | None
func explicitEnabled(fg map[string]any) (bool, bool) {
	if fg == nil {
		return false, false
	}
	raw, has := fg["enabled"]
	if !has {
		return false, false
	}
	if b, ok := raw.(bool); ok {
		return b, true
	}
	// Python 的 bool(x) 语义：非零/非空为真
	return truthyAny(raw), true
}

// truthyAny Python 的 bool() 语义。
func truthyAny(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case int:
		return x != 0
	case int64:
		return x != 0
	case float64:
		return x != 0
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	}
	return true
}

// externalDirectoryNonEmpty 判定 external_directory 是否非空（dict 或 str）。
func externalDirectoryNonEmpty(v any) bool {
	switch x := v.(type) {
	case string:
		return x != ""
	case map[string]any:
		return len(x) > 0
	}
	return false
}

// posixNorm 归一化为绝对 posix 路径；失败回退到字符串替换。
//
// Python: _posix_norm(p)
func posixNorm(p string) string {
	expanded := expandRaw(p)
	if abs, err := filepath.Abs(expanded); err == nil {
		if resolved, err2 := filepath.EvalSymlinks(abs); err2 == nil {
			return filepath.ToSlash(resolved)
		}
		return filepath.ToSlash(abs)
	}
	norm := strings.TrimSuffix(strings.ReplaceAll(expanded, `\`, "/"), "/")
	if norm == "" {
		return strings.ReplaceAll(expanded, `\`, "/")
	}
	return norm
}

// posixNormLoose 尽力归一化为 posix 形式，不解析符号链接（用于去重比较与规则编译）。
func posixNormLoose(p string) string {
	expanded := expandRaw(p)
	if abs, err := filepath.Abs(expanded); err == nil {
		return filepath.ToSlash(abs)
	}
	norm := strings.TrimSuffix(strings.ReplaceAll(expanded, `\`, "/"), "/")
	if norm == "" {
		return strings.ReplaceAll(expanded, `\`, "/")
	}
	return norm
}

// expandRaw 展开环境变量与 ~。
//
// Python: os.path.expandvars(os.path.expanduser(raw.strip()))
func expandRaw(raw string) string {
	text := strings.TrimSpace(raw)
	text = os.ExpandEnv(text)
	return expandHomeToken(text)
}

// parseFileGuardLevel 解析级别字符串，非法值返回 default。
//
// Python: _parse_level(raw, default=ASK)
func parseFileGuardLevel(v any, def security.PermissionLevel) security.PermissionLevel {
	if lv, ok := v.(security.PermissionLevel); ok {
		return lv
	}
	s, ok := v.(string)
	if !ok {
		return def
	}
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "allow":
		return security.PermissionLevelAllow
	case "ask":
		return security.PermissionLevelAsk
	case "deny":
		return security.PermissionLevelDeny
	}
	return def
}

// axisFromStar 把统一 action 展开成三轴默认。
//
// Python: _axis_from_star(action)
func axisFromStar(action any) FileGuardAxisDefaults {
	lv := parseFileGuardLevel(action, security.PermissionLevelAsk)
	return FileGuardAxisDefaults{Read: lv, Write: lv, Exec: lv}
}

// levelPtr 把字符串级别转为指针（非法值回落 ask）。
func levelPtr(s string) *security.PermissionLevel {
	lv := parseFileGuardLevel(s, security.PermissionLevelAsk)
	return &lv
}

// levelPtrOptional 把 any 级别转为指针；nil 返回 nil（表示沿用 defaultLevel）。
func levelPtrOptional(v any) *security.PermissionLevel {
	if v == nil {
		return nil
	}
	lv := parseFileGuardLevel(v, security.PermissionLevelAsk)
	return &lv
}

// levelForAction 取规则在指定轴上的级别。
//
// Python: _level_for_action(rule, action)
func levelForAction(rule *FileGuardPathRule, action FileAction) security.PermissionLevel {
	switch action {
	case FileActionWrite:
		return rule.Write
	case FileActionExec:
		return rule.Exec
	}
	return rule.Read
}

// toolDefaultAction Legacy 模式下工具路径访问的默认轴。
//
// Legacy 不区分 exec；shell 与读取类都按 read 轴（与 ExternalDirectory
// 无轴一致，共用 defaults）。
//
// Python: _tool_default_action(tool_name, permission_config=None)
func toolDefaultAction(toolName string, permissionConfig map[string]any) FileAction {
	if security.WritePathTools[toolName] {
		return FileActionWrite
	}
	// shell 与其余都按 read 轴
	return FileActionRead
}

// matchGlobPattern 简单 glob：支持 ** / * / ?（路径用 posix 形式）。
//
// Python: _match_glob(pattern, path_posix)
func matchGlobPattern(pattern, pathPosix string) bool {
	var b strings.Builder
	i := 0
	for i < len(pattern) {
		switch {
		case strings.HasPrefix(pattern[i:], "**/"):
			b.WriteString(`(?:.*/)?`)
			i += 3
		case strings.HasPrefix(pattern[i:], "**"):
			b.WriteString(`.*`)
			i += 2
		case pattern[i] == '*':
			b.WriteString(`[^/]*`)
			i++
		case pattern[i] == '?':
			b.WriteString(`[^/]`)
			i++
		default:
			b.WriteString(regexp.QuoteMeta(string(pattern[i])))
			i++
		}
	}
	re, err := regexp.Compile("^(?:" + b.String() + ")$")
	if err != nil {
		return false
	}
	return re.MatchString(pathPosix)
}

// extractPathsFromCommand Legacy 模式的命令行路径抽取。
//
// Python: _extract_paths_from_command(command, workdir)
func extractPathsFromCommand(command, workdir string) []string {
	if command == "" {
		return nil
	}
	tokens := shlexSplit(command)
	if len(tokens) == 0 {
		return nil
	}
	cmd := strings.ToLower(tokens[0])
	// legacy 只认 pathAwareCommands 的子集（Python 的 _PATH_AWARE_COMMANDS）
	if _, hit := legacyPathAwareCommands[cmd]; !hit {
		return nil
	}
	base := resolveExtractPath(workdir)
	var paths []string
	for _, tok := range tokens[1:] {
		tok = strings.TrimSpace(tok)
		tok = strings.Trim(tok, `"`)
		tok = strings.Trim(tok, `'`)
		if tok == "" || strings.HasPrefix(tok, "-") {
			continue
		}
		if !looksLikePathLegacy(tok) {
			continue
		}
		resolved := tok
		if !pathIsAbs(tok) {
			resolved = filepath.Join(base, tok)
		}
		paths = append(paths, resolveExtractPath(resolved))
	}
	return paths
}

// looksLikePathLegacy legacy 模式的路径判定（比 path_extract 的版本更宽松：
// 不排除 cmd.exe 开关、不认 . / .. / ~）。
//
// Python: file_guard.py 的 _looks_like_path（与 path_extract.py 的同名函数不同）
func looksLikePathLegacy(token string) bool {
	if strings.HasPrefix(token, `\\`) || strings.HasPrefix(token, "./") || strings.HasPrefix(token, "../") {
		return true
	}
	if winAbsReLegacy.MatchString(token) {
		return true
	}
	return strings.Contains(token, `\`) || strings.Contains(token, "/")
}

// looksLikePathPattern 判定 pattern 是否像路径。
//
// Python: _looks_like_path_pattern(pattern)
func looksLikePathPattern(pattern string) bool {
	s := strings.TrimSpace(pattern)
	if s == "" {
		return false
	}
	if strings.Contains(s, "/") || strings.Contains(s, `\`) || strings.Contains(s, "**") {
		return true
	}
	if strings.HasPrefix(s, "~") || strings.HasPrefix(s, "$") || strings.HasPrefix(s, "%") {
		return true
	}
	if len(s) > 1 && s[1] == ':' {
		return true
	}
	return false
}

// containsPath 判定 prefix 是否包含 path（处理 Windows 盘符大小写与分隔符差异）。
//
// Python: toolguard/pattern_matchers.py 的 contains_path
func containsPath(prefix, path string) bool {
	normPrefix := strings.ReplaceAll(prefix, `\`, "/")
	normPath := strings.ReplaceAll(path, `\`, "/")
	// Windows 盘符大小写不敏感
	if len(normPrefix) > 1 && normPrefix[1] == ':' {
		normPrefix = strings.ToLower(normPrefix)
		normPath = strings.ToLower(normPath)
	}
	return normPath == normPrefix || strings.HasPrefix(normPath, normPrefix+"/")
}

// mapOf 把 any 转为 map[string]any，非 map 返回 nil。
func mapOf(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

// pathListOf 把 any 转为 []map[string]any，非列表返回 nil。
func pathListOf(v any) []map[string]any {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(list))
	for _, item := range list {
		if m, isMap := item.(map[string]any); isMap {
			out = append(out, m)
		}
	}
	return out
}

// stringOrDefault 取字符串值，非字符串或空串用默认值。
func stringOrDefault(v any, def string) string {
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return def
}

// sortPairs 按第一个元素排序 [][2]string。
func sortPairs(pairs [][2]string) {
	for i := 1; i < len(pairs); i++ {
		for j := i; j > 0 && pairs[j][0] < pairs[j-1][0]; j-- {
			pairs[j], pairs[j-1] = pairs[j-1], pairs[j]
		}
	}
}

// sortStrings 原地排序字符串切片。
func sortStrings(items []string) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j] < items[j-1]; j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}
