package permissions

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	harnesssecurity "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// persistLogComponent 日志组件
var persistLogComponent = logger.ComponentPermissions

// ──────────────────────────── 导出函数 ────────────────────────────

// PersistPermissionAllowRule 用户选择「总是允许」时，将 allow 规则写入 config.yaml 的 permissions 段。
//
// Python: persist_permission_allow_rule(tool_name, tool_args) (permissions_persist.py L132-150)
func PersistPermissionAllowRule(toolName string, toolArgs map[string]any) bool {
	toolArgs = normalizeToolArgs(toolArgs)

	persistLock.Lock()
	defer persistLock.Unlock()

	data := readYAMLData()
	permissions, _ := data["permissions"].(map[string]any)
	if permissions == nil {
		logger.Warn(persistLogComponent).
			Str("tool_name", toolName).
			Msg("[PermissionPersist] persist_permission_allow_rule.abort reason=no_permissions_section")
		return false
	}

	merged, ok := harnesssecurity.MergePermissionAllowRuleIntoPermissions(permissions, toolName, toolArgs)
	if !ok {
		return false
	}
	data["permissions"] = merged
	_ = writeYAMLData(data)
	return true
}

// PersistExternalDirectoryAllow 用户选择「总是允许」外部路径时，写入 external_directory 配置。
//
// Python: persist_external_directory_allow(paths) (permissions_persist.py L153-166)
func PersistExternalDirectoryAllow(paths []string) bool {
	if len(paths) == 0 {
		return false
	}

	persistLock.Lock()
	defer persistLock.Unlock()

	data := readYAMLData()
	permissions, _ := data["permissions"].(map[string]any)
	if permissions == nil {
		permissions = map[string]any{}
	}
	merged, ok := harnesssecurity.MergeExternalDirectoryAllowIntoPermissions(permissions, paths)
	if !ok {
		return false
	}
	data["permissions"] = merged
	_ = writeYAMLData(data)
	return true
}

// PersistCliTrustedDirectoryWithOverrides CLI command.add_dir：全局信任目录子树（包含覆盖规则）。
// 写入 permissions.external_directory 和 permissions.approval_overrides。
//
// Python: persist_cli_trusted_directory_with_overrides(raw_path) (permissions_persist.py L201-277)
func PersistCliTrustedDirectoryWithOverrides(rawPath string) map[string]any {
	if strings.TrimSpace(rawPath) == "" {
		return map[string]any{"ok": false, "error": "path is empty"}
	}

	// 解析路径：expanduser + resolve
	dirNorm := expandAndResolvePath(rawPath)
	if dirNorm == "" {
		return map[string]any{"ok": false, "error": "path resolves to empty"}
	}

	persistLock.Lock()
	defer persistLock.Unlock()

	data := readYAMLData()
	permissions := ensurePermissionsDict(data)
	extCfg := ensureExternalDirectoryDict(permissions)
	extCfg[dirNorm] = "allow"

	// 构建 regex 模式
	pathPattern := "re:^" + regexp.QuoteMeta(dirNorm) + "(?:$|/)"
	shellPattern := "re:" + ".*" + regexp.QuoteMeta(dirNorm) + ".*"

	// 是否写 approval_overrides
	schemaKey := strings.TrimSpace(strings.ToLower(strVal(permissions["schema"])))
	if schemaKey == "" {
		schemaKey = strings.TrimSpace(strings.ToLower(strVal(permissions["version"])))
	}
	tiered := schemaKey == "tiered_policy" || schemaKey == "v_cc" || schemaKey == "v4.2" || schemaKey == ""

	suffix := fmt.Sprintf("%x", sha256.Sum256([]byte(dirNorm)))[:16]
	pathOverrideID := "cli_trusted_path_" + suffix
	shellOverrideID := "cli_trusted_shell_" + suffix

	if tiered {
		overrides := ensureApprovalOverridesList(permissions)

		pathTools := sortedSet([]string{
			"read_file", "write_file", "edit_file",
			"read_text_file", "write_text_file",
			"write", "read",
			"glob_file_search", "glob", "list_dir", "list_files",
			"grep", "search_replace",
		})
		appendOverrideIfMissing(permissions, overrides, pathOverrideID, pathTools, "path", pathPattern, "allow", "cli_add_dir")

		shellTools := sortedSet([]string{"bash", "mcp_exec_command", "create_terminal"})
		appendOverrideIfMissing(permissions, overrides, shellOverrideID, shellTools, "command", shellPattern, "allow", "cli_add_dir")
	}

	_ = writeYAMLData(data)
	return map[string]any{
		"ok":               true,
		"normalized":       dirNorm,
		"path_pattern":     pathPattern,
		"shell_pattern":    shellPattern,
		"tiered_overrides": tiered,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// normalizeToolArgs 将工具入参规范化为 map[string]any。
// dict 原样返回，string/bytes 尝试 JSON 解析，其它返回空 map。
//
// Python: _normalize_tool_args(tool_args) (permissions_persist.py L106-129)
func normalizeToolArgs(toolArgs any) map[string]any {
	if toolArgs == nil {
		return map[string]any{}
	}
	if m, ok := toolArgs.(map[string]any); ok {
		return m
	}
	if b, ok := toolArgs.([]byte); ok {
		var parsed map[string]any
		if err := json.Unmarshal(b, &parsed); err != nil {
			return map[string]any{}
		}
		return parsed
	}
	if s, ok := toolArgs.(string); ok {
		s = strings.TrimSpace(s)
		if s == "" {
			return map[string]any{}
		}
		var parsed map[string]any
		if err := json.Unmarshal([]byte(s), &parsed); err != nil {
			return map[string]any{}
		}
		return parsed
	}
	return map[string]any{}
}

// ensurePermissionsDict 确保 data["permissions"] 存在且为 map[string]any。
//
// Python: _ensure_permissions_dict(data) (permissions_persist.py L43-53)
func ensurePermissionsDict(data map[string]any) map[string]any {
	if data == nil {
		return map[string]any{}
	}
	permissions, _ := data["permissions"].(map[string]any)
	if permissions == nil {
		permissions = map[string]any{}
		data["permissions"] = permissions
	}
	return permissions
}

// ensureExternalDirectoryDict 确保 permissions["external_directory"] 存在且为 map[string]any。
//
// Python: _ensure_external_directory_dict(permissions) (permissions_persist.py L56-61)
func ensureExternalDirectoryDict(permissions map[string]any) map[string]any {
	extCfg, _ := permissions["external_directory"].(map[string]any)
	if extCfg == nil {
		extCfg = map[string]any{"*": "ask"}
		permissions["external_directory"] = extCfg
	}
	return extCfg
}

// ensureApprovalOverridesList 确保 permissions["approval_overrides"] 存在且为 []map[string]any。
//
// Python: _ensure_approval_overrides_list(permissions) (permissions_persist.py L64-70)
func ensureApprovalOverridesList(permissions map[string]any) []map[string]any {
	raw, _ := permissions["approval_overrides"].([]any)
	if raw == nil {
		raw = []any{}
		permissions["approval_overrides"] = raw
	}
	// 仅保留 dict 项
	filtered := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if m, ok := item.(map[string]any); ok {
			filtered = append(filtered, m)
		}
	}
	return filtered
}

// hasOverrideID 检查 overrides 中是否已存在指定 ID。
//
// Python: _has_override_id(overrides, oid) (permissions_persist.py L73-74)
func hasOverrideID(overrides []map[string]any, oid string) bool {
	for _, item := range overrides {
		if strVal(item["id"]) == oid {
			return true
		}
	}
	return false
}

// appendOverrideIfMissing 若 overrides 中不存在指定 ID，则追加一条 override 到 permissions["approval_overrides"]。
//
// Python: _append_override_if_missing(overrides, ...) (permissions_persist.py L77-98)
func appendOverrideIfMissing(permissions map[string]any, overrides []map[string]any, oid string, tools []string, matchType string, pattern string, action string, source string) {
	if hasOverrideID(overrides, oid) {
		return
	}
	override := map[string]any{
		"id":         oid,
		"tools":      tools,
		"match_type": matchType,
		"pattern":    pattern,
		"action":     action,
		"source":     source,
	}
	// 追加到 permissions["approval_overrides"] 原始切片
	ovRaw, _ := permissions["approval_overrides"].([]any)
	if ovRaw == nil {
		ovRaw = []any{}
	}
	ovRaw = append(ovRaw, override)
	permissions["approval_overrides"] = ovRaw
}

// expandAndResolvePath 展开路径（expanduser + resolve），返回 POSIX 格式且去除尾部斜杠。
//
// 对应 Python: Path(raw_path.strip()).expanduser().resolve(strict=False).as_posix().rstrip("/")
func expandAndResolvePath(rawPath string) string {
	cleaned := strings.TrimSpace(rawPath)
	if cleaned == "" {
		return ""
	}
	// expanduser: 处理 ~
	expanded := expandUserPath(cleaned)
	// resolve: 转绝对路径（不要求文件存在）
	abs, err := filepath.Abs(expanded)
	if err != nil {
		return ""
	}
	// POSIX 格式：反斜杠转正斜杠
	posix := filepath.ToSlash(abs)
	// 去除尾部斜杠
	posix = strings.TrimRight(posix, "/")
	return posix
}

// expandUserPath 展开 ~ 为用户主目录。
func expandUserPath(path string) string {
	if strings.HasPrefix(path, "~") {
		homeDir, err := os.UserHomeDir()
		if err != nil || homeDir == "" {
			return path
		}
		return homeDir + path[1:]
	}
	return path
}

// sortedSet 对字符串切片去重排序。
func sortedSet(items []string) []string {
	seen := make(map[string]bool, len(items))
	result := make([]string, 0, len(items))
	for _, item := range items {
		if !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}
	sort.Strings(result)
	return result
}
