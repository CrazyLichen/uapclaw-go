package security

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PatternMatcher 通配符模式匹配器（仅支持 wildcard 模式 * 和 ?）。
//
// 对齐 Python: PatternMatcher (patterns.py L162-173)
type PatternMatcher struct{}

// PathMatcher 路径匹配器。
//
// 对齐 Python: PathMatcher (patterns.py L176-203)
type PathMatcher struct {
	pm PatternMatcher
}

// URLMatcher URL 匹配器。
//
// 对齐 Python: URLMatcher (patterns.py L206-234)
type URLMatcher struct {
	pm PatternMatcher
}

// CommandMatcher 命令匹配器（wildcard 模式，全串锚定防注入）。
//
// 对齐 Python: CommandMatcher (patterns.py L237-250)
type CommandMatcher struct {
	pm PatternMatcher
}

// approvalOverrideSignature 审批覆盖签名，用于去重判断。
//
// 对齐 Python: _ApprovalOverrideSignature (patterns.py L106-115)
type approvalOverrideSignature struct {
	// ToolName 目标工具名
	ToolName string
	// Tools 规则中的工具列表
	Tools []string
	// MatchType 匹配类型
	MatchType string
	// ExistingMatchType 已有条目的匹配类型
	ExistingMatchType string
	// Pattern 匹配模式
	Pattern string
	// ExistingPattern 已有条目的匹配模式
	ExistingPattern string
	// ExistingAction 已有条目的动作
	ExistingAction string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// wildcardChars 限制性字符类：仅允许命令参数和路径常见字符，
	// 排除 ; | & ` < > $ 等 shell 元字符防注入。
	// 对齐 Python: _WILDCARD_CHARS (patterns.py L118-119)
	wildcardChars = `[-a-zA-Z0-9 \._/:"']*`
)

// ──────────────────────────── 全局变量 ────────────────────────────

// 全局 PathMatcher 实例，供 tiered_policy 使用
// 对齐 Python: _TIERED_PATH_MATCHER = PathMatcher() (tiered_policy.py L26)
var tieredPathMatcher = &PathMatcher{pm: PatternMatcher{}}

var patternsLogComponent = logger.ComponentAgentCore

// shellApprovalTools Shell 审批工具集合
// 对齐 Python: _SHELL_APPROVAL_TOOLS (patterns.py L92)
var shellApprovalTools = map[string]bool{
	"bash": true, "mcp_exec_command": true, "create_terminal": true,
}

// pathApprovalTools 路径审批工具集合
// 对齐 Python: _PATH_APPROVAL_TOOLS (patterns.py L93-99)
var pathApprovalTools = map[string]bool{
	"read_file": true, "write_file": true, "edit_file": true,
	"read_text_file": true, "write_text_file": true,
	"write": true, "read": true,
	"glob_file_search": true, "glob": true, "list_dir": true, "list_files": true,
	"grep": true, "search_replace": true,
}

// pathApprovalKeys 路径审批参数键
// 对齐 Python: _PATH_APPROVAL_KEYS (patterns.py L100-103)
var pathApprovalKeys = map[string]bool{
	"path": true, "file_path": true, "target_file": true, "file": true,
	"old_path": true, "new_path": true, "source_path": true, "dest_path": true,
	"directory": true, "dir": true,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// MatchWildcard 通配符匹配。
//
// - * → 限制性字符类*（排除 shell 元字符，防命令拼接）
// - ? → 限制性字符类（恰好一个）
// - 正则元字符转义
// - " *" 结尾 → ( 字符类*)? 使 "ls *" 可匹配 "ls" 或 "ls -la"
// - 全串匹配防止 "git status; rm -rf /" 匹配 "git status *"
//
// 对齐 Python: match_wildcard(value, pattern) (patterns.py L122-157)
func MatchWildcard(value, pattern string) bool {
	if pattern == "" || value == "" {
		return false
	}
	val := strings.ReplaceAll(value, `\`, "/")
	pat := strings.ReplaceAll(pattern, `\`, "/")

	// 1. 转义正则特殊字符（* 和 ? 保留，后续单独处理）
	toEscape := ".+^${}()|[]\\"
	escaped := escapeRegexChars(pat, toEscape)

	// 2. 先替换 ?（必须在 * 之前，否则会误替换 ")? " 中的 ?）
	escaped = strings.ReplaceAll(escaped, "?", wildcardChars[:len(wildcardChars)-1]) // 去掉末尾 *

	// 3. * → 限制性字符类*
	if strings.HasSuffix(escaped, " *") {
		escaped = escaped[:len(escaped)-2] + "( " + wildcardChars + ")?"
	} else {
		escaped = strings.ReplaceAll(escaped, "*", wildcardChars)
	}

	// 4. 全串匹配
	re, err := regexp.Compile("^" + escaped + "$")
	if err != nil {
		return false
	}
	return re.MatchString(val)
}

// BuildCommandAllowPattern 构建匹配完整命令的通配符模式。
//
// 对齐 Python: build_command_allow_pattern(cmd) (patterns.py L253-261)
// "start chrome" → "start chrome *"
func BuildCommandAllowPattern(cmd string) string {
	return strings.TrimSpace(cmd) + " *"
}

// ContainsPath 子路径是否在父路径下（含路径穿越防护）。
//
// 对齐 Python: contains_path(parent, child) (patterns.py L264-271)
func ContainsPath(parent, child string) bool {
	parentPath := filepath.Clean(parent)
	childPath := filepath.Clean(child)

	// 解析为绝对路径
	absParent, err := filepath.Abs(parentPath)
	if err != nil {
		return false
	}
	absChild, err := filepath.Abs(childPath)
	if err != nil {
		return false
	}

	// 计算相对路径
	rel, err := filepath.Rel(absParent, absChild)
	if err != nil {
		return false
	}

	// 如果相对路径以 .. 开头，说明 child 不在 parent 下
	return !strings.HasPrefix(rel, "..") && rel != ".."
}

// WritePermissionsSectionToAgentConfigYAML 将 permissions 整段写入 agent YAML
// （保留其它顶层键；文件不存在则新建仅含 permissions 的根）。
//
// 对齐 Python: write_permissions_section_to_agent_config_yaml(config_yaml_path, permissions) (patterns.py L383-412)
func WritePermissionsSectionToAgentConfigYAML(configYAMLPath string, permissions map[string]any) bool {
	cfgPath := resolveAgentConfigYAMLPath(configYAMLPath)
	if cfgPath == "" {
		logger.Warn(patternsLogComponent).Msg("permission.write_yaml.abort: no_config_yaml_path")
		return false
	}

	var data map[string]any
	raw, err := os.ReadFile(cfgPath)
	if err == nil && len(raw) > 0 {
		if err := yaml.Unmarshal(raw, &data); err != nil {
			data = make(map[string]any)
		}
	} else {
		data = make(map[string]any)
	}

	data["permissions"] = deepCopyMap(permissions)

	out, err := yaml.Marshal(data)
	if err != nil {
		logger.Error(patternsLogComponent).Err(err).Str("path", cfgPath).Msg("permission.write_yaml.failed: marshal error")
		return false
	}
	if err := os.WriteFile(cfgPath, out, 0644); err != nil {
		logger.Error(patternsLogComponent).Err(err).Str("path", cfgPath).Msg("permission.write_yaml.failed: write error")
		return false
	}

	logger.Info(patternsLogComponent).Str("path", cfgPath).Msg("permission.write_yaml.ok")
	return true
}

// MergeExternalDirectoryAllowIntoPermissions 在 permissions 副本上合并外部目录白名单。
// 返回 (merged, wrote_any)。
//
// 对齐 Python: merge_external_directory_allow_into_permissions(permissions, paths) (patterns.py L415-439)
func MergeExternalDirectoryAllowIntoPermissions(permissions map[string]any, paths []string) (map[string]any, bool) {
	if len(paths) == 0 {
		return deepCopyMap(permissions), false
	}
	perms := deepCopyMap(permissions)

	extCfg, ok := perms["external_directory"].(map[string]any)
	if !ok {
		extCfg = map[string]any{"*": "ask"}
		perms["external_directory"] = extCfg
	}

	wrote := false
	for _, pathStr := range paths {
		pathNorm := strings.ReplaceAll(pathStr, `\`, "/")
		pathNorm = strings.TrimRight(pathNorm, "/")
		parentDir := filepath.Dir(pathNorm)
		parentNorm := strings.ReplaceAll(parentDir, `\`, "/")
		key := parentNorm
		if key == "" || key == "." {
			key = pathNorm
		}
		existing, exists := extCfg[key]
		if !exists || existing != "allow" {
			extCfg[key] = "allow"
			wrote = true
			logger.Info(patternsLogComponent).
				Str("path", key).
				Msg("permission.merge.external: action=allow")
		}
	}
	return perms, wrote
}

// MergePermissionAllowRuleIntoPermissions 在 permissions 副本上合并「始终允许」规则。
// 返回 (merged, applied)。applied 为假表示未写入任何变更。
//
// 对齐 Python: merge_permission_allow_rule_into_permissions(permissions, tool_name, tool_args) (patterns.py L442-493)
func MergePermissionAllowRuleIntoPermissions(permissions map[string]any, toolName string, toolArgs map[string]any) (map[string]any, bool) {
	perms := deepCopyMap(permissions)

	currentPermission, _ := EvaluateTieredPolicy(perms, toolName, toolArgs)
	if currentPermission != PermissionLevelAsk {
		logger.Warn(patternsLogComponent).
			Str("tool", toolName).
			Str("current", currentPermission.String()).
			Msg("permission.merge.skip: current_permission_not_ask")
		return perms, false
	}

	// Shell 工具获取 shell_ast_result
	var shellAstResult *ShellAstParseResult
	if shellApprovalTools[toolName] {
		cmd := commandText(toolArgs)
		shellAstResult = ParseShellForPermission(cmd)
	}

	suggestions := BuildPermissionSuggestions(toolName, toolArgs, shellAstResult)
	if !persistTieredApprovalOverrideSuggestions(perms, suggestions) {
		if !shellApprovalTools[toolName] && !pathApprovalTools[toolName] {
			if persistTieredToolAllow(perms, toolName) {
				logger.Info(patternsLogComponent).
					Str("tool", toolName).
					Msg("permission.merge.ok: target=tools")
				return perms, true
			}
		}
		logger.Warn(patternsLogComponent).
			Str("tool", toolName).
			Msg("permission.merge.skip: no_safe_suggestion")
		return perms, false
	}
	logger.Info(patternsLogComponent).
		Str("tool", toolName).
		Msg("permission.merge.ok: target=approval_overrides")
	return perms, true
}

// PersistCliTrustedDirectory CLI add_dir：全局信任目录子树。
//
// 对齐 Python: persist_cli_trusted_directory(raw_path, config_yaml_path, bootstrap_permissions) (patterns.py L496-616)
func PersistCliTrustedDirectory(rawPath string, configYAMLPath string, bootstrapPermissions map[string]any) map[string]any {
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" {
		return map[string]any{"ok": false, "error": "path is empty"}
	}

	resolved, err := filepath.Abs(rawPath)
	if err != nil {
		return map[string]any{"ok": false, "error": fmt.Sprintf("invalid path: %s", err)}
	}
	dirNorm := filepath.ToSlash(resolved)
	dirNorm = strings.TrimRight(dirNorm, "/")
	if dirNorm == "" {
		return map[string]any{"ok": false, "error": "path resolves to empty"}
	}

	cfgPath := resolveAgentConfigYAMLPath(configYAMLPath)
	if cfgPath == "" {
		return map[string]any{"ok": false, "error": "no agent config yaml path"}
	}

	var data map[string]any
	raw, err := os.ReadFile(cfgPath)
	if err == nil && len(raw) > 0 {
		_ = yaml.Unmarshal(raw, &data)
	}
	if data == nil {
		if bootstrapPermissions == nil || len(bootstrapPermissions) == 0 {
			logger.Warn(patternsLogComponent).Str("path", cfgPath).Msg("permission.persist.abort: new_yaml_requires_fallback_permissions")
			return map[string]any{"ok": false, "error": "cannot bootstrap yaml (missing file; pass bootstrap_permissions with non-empty permissions dict)"}
		}
		data = map[string]any{"permissions": deepCopyMap(bootstrapPermissions)}
	}

	permissions, ok := data["permissions"].(map[string]any)
	if !ok {
		permissions = make(map[string]any)
		data["permissions"] = permissions
	}

	extCfg, ok := permissions["external_directory"].(map[string]any)
	if !ok {
		extCfg = map[string]any{"*": "ask"}
		permissions["external_directory"] = extCfg
	}
	extCfg[dirNorm] = "allow"
	logger.Info(patternsLogComponent).
		Str("path", dirNorm).
		Msg("permission.persist.cli_add_dir.external.write: action=allow")

	// 路径正则：re:^dir(?:$|/)
	pathPattern := "re:^" + regexp.QuoteMeta(dirNorm) + `(?:$|/)`

	// Shell 正则：re:.*dir.*
	shellPattern := "re:.*" + regexp.QuoteMeta(dirNorm) + ".*"

	suffix := fmt.Sprintf("%x", sha256.Sum256([]byte(dirNorm)))[:16]
	pathOverrideID := "cli_trusted_path_" + suffix
	shellOverrideID := "cli_trusted_shell_" + suffix

	overrides, ok := permissions["approval_overrides"].([]any)
	if !ok {
		overrides = []any{}
		permissions["approval_overrides"] = overrides
	}

	if !hasOverrideID(overrides, pathOverrideID) {
		pathTools := sortedPathTools()
		overrides = append(overrides, map[string]any{
			"id":         pathOverrideID,
			"tools":      pathTools,
			"match_type": "path",
			"pattern":    pathPattern,
			"action":     "allow",
		})
		permissions["approval_overrides"] = overrides
		logger.Info(patternsLogComponent).
			Str("id", pathOverrideID).
			Msg("permission.persist.cli_add_dir.override.write: target=path")
	}

	if !hasOverrideID(overrides, shellOverrideID) {
		shellTools := sortedShellTools()
		overrides = append(overrides, map[string]any{
			"id":         shellOverrideID,
			"tools":      shellTools,
			"match_type": "command",
			"pattern":    shellPattern,
			"action":     "allow",
		})
		permissions["approval_overrides"] = overrides
		logger.Info(patternsLogComponent).
			Str("id", shellOverrideID).
			Msg("permission.persist.cli_add_dir.override.write: target=shell")
	}

	out, err := yaml.Marshal(data)
	if err != nil {
		logger.Error(patternsLogComponent).Err(err).Msg("permission.persist.cli_add_dir.failed: marshal error")
		return map[string]any{"ok": false, "error": err.Error()}
	}
	if err := os.WriteFile(cfgPath, out, 0644); err != nil {
		logger.Error(patternsLogComponent).Err(err).Msg("permission.persist.cli_add_dir.failed: write error")
		return map[string]any{"ok": false, "error": err.Error()}
	}

	return map[string]any{
		"ok":               true,
		"normalized":       dirNorm,
		"path_pattern":     pathPattern,
		"shell_pattern":    shellPattern,
		"tiered_overrides": true,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// Match 实现 PatternMatcher 的静态匹配方法。
//
// 对齐 Python: PatternMatcher.match(pattern, value) (patterns.py L166-169)
func (pm *PatternMatcher) Match(pattern, value string) bool {
	if pattern == "" || value == "" {
		return false
	}
	return MatchWildcard(value, pattern)
}

// MatchAny 匹配任意一个模式。
//
// 对齐 Python: PatternMatcher.match_any(patterns, value) (patterns.py L171-173)
func (pm *PatternMatcher) MatchAny(patterns []string, value string) bool {
	for _, p := range patterns {
		if pm.Match(p, value) {
			return true
		}
	}
	return false
}

// MatchPath 匹配文件路径（规范化分隔符后再比较）。
//
// 对齐 Python: PathMatcher.match_path(pattern, path) (patterns.py L182-200)
func (pm *PathMatcher) MatchPath(pattern, path string) bool {
	normalizedPath := strings.ReplaceAll(path, `\`, "/")
	normalizedPattern := strings.ReplaceAll(pattern, `\`, "/")

	if pm.pm.Match(normalizedPattern, normalizedPath) {
		return true
	}

	// 尝试匹配父目录层级
	pathObj := filepath.Clean(path)
	for {
		parent := filepath.Dir(pathObj)
		if parent == pathObj {
			break
		}
		parentStr := strings.ReplaceAll(parent, `\`, "/")
		if pm.pm.Match(normalizedPattern, parentStr) {
			return true
		}
		if pm.pm.Match(normalizedPattern, parentStr+"/") {
			return true
		}
		if pm.pm.Match(normalizedPattern, parentStr+"/*") {
			return true
		}
		pathObj = parent
	}
	return false
}

// MatchPathAny 匹配任意一个路径模式。
func (pm *PathMatcher) MatchPathAny(patterns []string, path string) bool {
	for _, p := range patterns {
		if pm.MatchPath(p, path) {
			return true
		}
	}
	return false
}

// MatchURL 匹配 URL（支持 hostname、netloc、full URL）。
//
// 对齐 Python: URLMatcher.match_url(pattern, url) (patterns.py L212-231)
func (pm *URLMatcher) MatchURL(pattern, url string) bool {
	if url == "" {
		return false
	}
	if pm.pm.Match(pattern, url) {
		return true
	}

	// 简易 URL 解析
	scheme, host, path := parseURL(url)
	if host != "" {
		if pm.pm.Match(pattern, host) {
			return true
		}
		netloc := host
		if scheme != "" {
			if pm.pm.Match(pattern, scheme+"://"+netloc) {
				return true
			}
			if pm.pm.Match(pattern, scheme+"://"+netloc+"/*") {
				return true
			}
		}
	}
	_ = path
	return false
}

// MatchURLAny 匹配任意一个 URL 模式。
func (pm *URLMatcher) MatchURLAny(patterns []string, url string) bool {
	for _, p := range patterns {
		if pm.MatchURL(p, url) {
			return true
		}
	}
	return false
}

// MatchCommand 匹配命令字符串（wildcard 模式，全串锚定）。
//
// 对齐 Python: CommandMatcher.match_command(pattern, command) (patterns.py L243-247)
func (pm *CommandMatcher) MatchCommand(pattern, command string) bool {
	if command == "" {
		return false
	}
	return pm.pm.Match(pattern, command)
}

// MatchCommandAny 匹配任意一个命令模式。
func (pm *CommandMatcher) MatchCommandAny(patterns []string, command string) bool {
	for _, p := range patterns {
		if pm.MatchCommand(p, command) {
			return true
		}
	}
	return false
}

// escapeRegexChars 转义正则特殊字符（保留 skip 中的字符）
func escapeRegexChars(s, toEscape string) string {
	var b strings.Builder
	for _, c := range s {
		if strings.ContainsRune(toEscape, c) {
			b.WriteByte('\\')
		}
		b.WriteRune(c)
	}
	return b.String()
}

// parseURL 简易 URL 解析（不依赖 net/url 包避免额外复杂度）
func parseURL(urlStr string) (scheme, host, path string) {
	// 提取 scheme
	idx := strings.Index(urlStr, "://")
	if idx >= 0 {
		scheme = urlStr[:idx]
		urlStr = urlStr[idx+3:]
	}
	// 提取 host 和 path
	slashIdx := strings.Index(urlStr, "/")
	if slashIdx >= 0 {
		host = urlStr[:slashIdx]
		path = urlStr[slashIdx:]
	} else {
		host = urlStr
	}
	// 去掉 host 中的 port
	colonIdx := strings.LastIndex(host, ":")
	if colonIdx >= 0 && colonIdx > strings.LastIndex(host, "]") {
		// 有端口号，host 保持原样（含端口）
	}
	return
}

// resolveAgentConfigYAMLPath 解析落盘用的 agent 配置文件路径。
//
// 对齐 Python: _resolve_agent_config_yaml_path(explicit) (patterns.py L38-55)
func resolveAgentConfigYAMLPath(explicit string) string {
	if explicit == "" {
		return ""
	}
	p := explicit
	info, err := os.Stat(p)
	if err == nil && !info.IsDir() {
		return p
	}
	// 父目录存在也可
	dir := filepath.Dir(p)
	if d, err := os.Stat(dir); err == nil && d.IsDir() {
		return p
	}
	return ""
}

// deepCopyMap 深拷贝 map[string]any
func deepCopyMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	result := make(map[string]any, len(m))
	for k, v := range m {
		switch val := v.(type) {
		case map[string]any:
			result[k] = deepCopyMap(val)
		case []any:
			result[k] = deepCopySlice(val)
		default:
			result[k] = v
		}
	}
	return result
}

// deepCopySlice 深拷贝 []any
func deepCopySlice(s []any) []any {
	if s == nil {
		return nil
	}
	result := make([]any, len(s))
	for i, v := range s {
		switch val := v.(type) {
		case map[string]any:
			result[i] = deepCopyMap(val)
		case []any:
			result[i] = deepCopySlice(val)
		default:
			result[i] = v
		}
	}
	return result
}

// persistTieredApprovalOverrideSuggestions 持久化审批覆盖建议。
//
// 对齐 Python: _persist_tiered_approval_override_suggestions(permissions, suggestions) (patterns.py L277-299)
func persistTieredApprovalOverrideSuggestions(permissions map[string]any, suggestions []PermissionSuggestion) bool {
	if len(suggestions) == 0 {
		return false
	}
	overrides, ok := permissions["approval_overrides"].([]any)
	if !ok {
		overrides = []any{}
		permissions["approval_overrides"] = overrides
	}

	persistedAny := false
	for _, suggestion := range suggestions {
		for _, toolName := range suggestion.Tools {
			if ensureSingleAllowOverride(overrides, toolName, suggestion.MatchType, suggestion.Pattern, suggestion.Action) {
				persistedAny = true
			}
		}
	}
	if persistedAny {
		permissions["approval_overrides"] = overrides
	}
	return persistedAny
}

// ensureSingleAllowOverride 确保单个 allow override 条目存在。
//
// 对齐 Python: _ensure_single_allow_override(overrides, ...) (patterns.py L302-345)
func ensureSingleAllowOverride(overrides []any, toolName, matchType, pattern, action string) bool {
	for _, existing := range overrides {
		m, ok := existing.(map[string]any)
		if !ok {
			continue
		}
		tools := ruleToolsList(m)
		existingMatchType, _ := m["match_type"].(string)
		existingPattern, _ := m["pattern"].(string)
		existingAction := strings.TrimSpace(strings.ToLower(strVal(m["action"])))

		sig := approvalOverrideSignature{
			ToolName:          toolName,
			Tools:             tools,
			MatchType:         matchType,
			ExistingMatchType: existingMatchType,
			Pattern:           pattern,
			ExistingPattern:   existingPattern,
			ExistingAction:    existingAction,
		}
		if isSameAllowOverride(sig) {
			logger.Info(patternsLogComponent).
				Str("tool", toolName).
				Str("match_type", matchType).
				Str("pattern", pattern).
				Msg("permission.persist.skip: approval_override_exists")
			return true
		}
	}

	overrides = append(overrides, map[string]any{
		"id":         buildApprovalOverrideID(toolName, matchType, pattern),
		"tools":      []string{toolName},
		"match_type": matchType,
		"pattern":    pattern,
		"action":     action,
	})
	return true
}

// isSameAllowOverride 判断是否为相同的 allow override。
//
// 对齐 Python: _is_same_allow_override(signature) (patterns.py L348-355)
func isSameAllowOverride(sig approvalOverrideSignature) bool {
	found := false
	for _, t := range sig.Tools {
		if t == sig.ToolName {
			found = true
			break
		}
	}
	if !found {
		return false
	}
	if sig.ExistingMatchType != sig.MatchType {
		return false
	}
	if sig.ExistingPattern != sig.Pattern {
		return false
	}
	return sig.ExistingAction == "allow"
}

// buildApprovalOverrideID 构建 override 条目 ID。
//
// 对齐 Python: _build_approval_override_id(tool_name, match_type, pattern) (patterns.py L358-363)
func buildApprovalOverrideID(toolName, matchType, pattern string) string {
	raw := fmt.Sprintf("user_allow_%s_%s_%s", toolName, matchType, pattern)
	// 非字母数字替换为 _
	re := regexp.MustCompile(`[^a-zA-Z0-9]+`)
	collapsed := strings.Trim(re.ReplaceAllString(raw, "_"), "_")
	collapsed = strings.ToLower(collapsed)
	if collapsed == "" {
		return "user_allow_override"
	}
	if len(collapsed) > 120 {
		collapsed = collapsed[:120]
	}
	return collapsed
}

// persistTieredToolAllow 持久化整工具 allow。
//
// 对齐 Python: _persist_tiered_tool_allow(permissions, tool_name) (patterns.py L366-380)
func persistTieredToolAllow(permissions map[string]any, toolName string) bool {
	if toolName == "" {
		return false
	}
	tools, ok := permissions["tools"].(map[string]any)
	if !ok {
		tools = make(map[string]any)
		permissions["tools"] = tools
	}
	if tools[toolName] == "allow" {
		return false
	}
	tools[toolName] = "allow"
	return true
}

// hasOverrideID 检查 overrides 列表中是否已有指定 ID
func hasOverrideID(overrides []any, oid string) bool {
	for _, r := range overrides {
		if m, ok := r.(map[string]any); ok {
			if m["id"] == oid {
				return true
			}
		}
	}
	return false
}

// sortedPathTools 返回排序后的 path 工具列表
func sortedPathTools() []string {
	tools := make([]string, 0, len(pathApprovalTools))
	for t := range pathApprovalTools {
		tools = append(tools, t)
	}
	sortStrings(tools)
	return tools
}

// sortedShellTools 返回排序后的 shell 工具列表
func sortedShellTools() []string {
	tools := make([]string, 0, len(shellApprovalTools))
	for t := range shellApprovalTools {
		tools = append(tools, t)
	}
	sortStrings(tools)
	return tools
}

// sortStrings 对字符串切片排序
func sortStrings(s []string) {
	for i := 0; i < len(s)-1; i++ {
		for j := i + 1; j < len(s); j++ {
			if s[i] > s[j] {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
}

// commandText 从 toolArgs 中提取命令文本。
//
// 对齐 Python: _command_text(tool_args) (tiered_policy.py L163-164)
func commandText(toolArgs map[string]any) string {
	cmd, _ := toolArgs["command"].(string)
	if cmd == "" {
		cmd, _ = toolArgs["cmd"].(string)
	}
	return strings.TrimSpace(cmd)
}

// ruleToolsList 从规则 map 中提取 tools 列表
func ruleToolsList(rule map[string]any) []string {
	raw := rule["tools"]
	switch v := raw.(type) {
	case string:
		return []string{strings.TrimSpace(v)}
	case []any:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				s = strings.TrimSpace(s)
				if s != "" {
					result = append(result, s)
				}
			}
		}
		return result
	case []string:
		return v
	default:
		return nil
	}
}

// strVal 安全获取 string 值
func strVal(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
