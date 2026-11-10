package permissions

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"

	harnesssecurity "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security/fileguard"
	configutil "github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/utils"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ExactAllowOptions 一次精确放行落盘的参数。
//
// Python: persist_exact_permission_allow_rule 的形参与关键字参数
// （permissions_persist.py L195-206）
type ExactAllowOptions struct {
	// ToolName 工具名
	ToolName string
	// ToolArgs 工具参数（dict 或 JSON 字符串）
	ToolArgs map[string]any
	// AskAccesses 审批时确认的访问集合
	AskAccesses [][2]string
	// SessionID 会话标识，SessionOnly 时必填
	SessionID string
	// WorkspaceRoot 工作区根，空则取默认工作区
	WorkspaceRoot string
	// SessionOnly 仅写 session 层（false 写 user 层）
	SessionOnly bool
	// AutoMode 自动审查模式
	AutoMode bool
	// ValidateOnly 只校验不写盘
	ValidateOnly bool
	// RaiseOnInvalid 无效时返回错误（false 则静默返回 nil）
	RaiseOnInvalid bool
	// ExpectedGlobalPermissions 期望的全局配置快照，非空时比对
	ExpectedGlobalPermissions map[string]any
}

// ──────────────────────────── 全局变量 ────────────────────────────

// ErrPermissionApprovalInvalidated 一次审批已不满足当前宿主安全检查。
//
// Python: class PermissionApprovalInvalidated(ValueError)（permissions_persist.py L151-152）
// Go 用哨兵错误 + fmt.Errorf("%w: reason") 包装原因。
var ErrPermissionApprovalInvalidated = errors.New("permission approval invalidated")

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildCommandAllowPattern 构建匹配完整命令的通配符模式。
//
// Python: build_command_allow_pattern (permissions_persist.py L99-101)
func BuildCommandAllowPattern(cmd string) string {
	return strings.TrimSpace(cmd) + " *"
}

// MergeFileGuardPathIntoPermissions 写入 / 更新一条 file_guard.paths。
//
// Python: _merge_file_guard_path_into_permissions (permissions_persist.py L81-96)
//
// Python 用 clear()+update() 原地改同一 dict 供调用方 dump；
// Go 的 map 引用语义一致，先清空再回填即可。
func MergeFileGuardPathIntoPermissions(
	permissions map[string]any,
	pathNorm string,
	read string,
	write string,
	execAction string,
) bool {
	merged, wrote := harnesssecurity.MergeFileGuardPathRule(
		permissions, pathNorm, read, write, execAction, "prefix",
	)
	for key := range permissions {
		delete(permissions, key)
	}
	for key, value := range merged {
		permissions[key] = value
	}
	return wrote
}

// NormalizeExactAccesses 归一化精确访问集合。
//
// Python: _normalize_exact_accesses (permissions_persist.py L300-320)
//
// 返回 (normalized, ok)：ok=false 表示输入非法，调用方应整体拒绝
// （注意：不是跳过非法项，而是整批失败）。
func NormalizeExactAccesses(accesses [][2]string) ([][2]string, bool) {
	normalized := make([][2]string, 0, len(accesses))
	seen := make(map[[2]string]struct{}, len(accesses))
	for _, access := range accesses {
		pathNorm := strings.ReplaceAll(access[0], "\\", "/")
		pathNorm = strings.TrimRight(pathNorm, "/")
		actionNorm := strings.ToLower(strings.TrimSpace(access[1]))
		if pathNorm == "" {
			return nil, false
		}
		switch actionNorm {
		case "read", "write", "exec":
		default:
			return nil, false
		}
		item := [2]string{pathNorm, actionNorm}
		if _, dup := seen[item]; dup {
			continue
		}
		seen[item] = struct{}{}
		normalized = append(normalized, item)
	}
	return normalized, true
}

// CheckApprovalAccesses 用 Core 的完整策略与宿主访问投影校验一次审批。
//
// Python: _check_approval_accesses (permissions_persist.py L155-193)
//
// 八步顺序（对齐 Python，不可调换）：
//  1. 构造 PermissionEngine（workspace_root=root，trusted_dirs 为空）
//  2. current_native_path_access 取投影访问
//  3. evaluate_global_policy_directly（access 为 nil 时才含外部目录）
//  4. build_file_guard_checker
//  5. 有 access 且 checker 非空 → checker.evaluate，与 level 取 strictest
//  6. checker.collect_ask_accesses
//  7. send_file 单独走 SendFilePathGuardEvaluator（unevaluable/deny 都抛错）
//  8. 最终 level 为 deny 时抛错
//
// 返回 (level, asks, err)。err 非 nil 时一定是 ErrPermissionApprovalInvalidated。
func CheckApprovalAccesses(
	permissions map[string]any,
	name string,
	args map[string]any,
	root string,
) (string, map[[2]string]struct{}, error) {
	// 1. 构造引擎
	engine := harnesssecurity.NewPermissionEngine(permissions, nil, "", root)

	// 2. 投影访问
	access := CurrentNativePathAccess(name, args)

	// 3. 全局策略（access 存在时不含外部目录）
	includeExternalDirectory := access == nil
	level, _, err := engine.EvaluateGlobalPolicyDirectly(name, args, includeExternalDirectory)
	if err != nil {
		return "", nil, fmt.Errorf("%w: %s", ErrPermissionApprovalInvalidated, "policy_evaluate_failed")
	}

	// 4. 构造路径判定器
	checker := fileguard.BuildFileGuardChecker(engine.Config(), root, nil)

	guardTool, guardArgs := name, args
	if access != nil {
		guardTool, guardArgs = access.GuardTool(), access.GuardArgs()
	}

	// 5. 路径判定与 strictest 合并
	if access != nil && checker != nil {
		if pathResult := checker.Evaluate(guardTool, guardArgs); pathResult != nil {
			level = harnesssecurity.Strictest(level, pathResult.Permission)
		}
	}

	// 6. 收集需要询问的访问
	asks := make(map[[2]string]struct{})
	if checker != nil {
		for _, ask := range checker.CollectAskAccesses(guardTool, guardArgs) {
			asks[[2]string{ask.Path, ask.Action}] = struct{}{}
		}
	}

	// 7. send_file 专用守卫
	if name == SendFileToolName {
		result := NewSendFilePathGuardEvaluator().Evaluate(
			engine.Config(), root, nil, args["abs_file_path_list"],
		)
		if result.Status == "unevaluable" || result.Level == "deny" {
			return "", nil, fmt.Errorf("%w: %s", ErrPermissionApprovalInvalidated, result.Reason)
		}
		for _, ask := range result.AskAccesses {
			asks[[2]string{ask.Path, ask.Action}] = struct{}{}
		}
	}

	// 8. 最终拒绝
	levelStr := level.String()
	if levelStr == "deny" {
		return "", nil, fmt.Errorf("%w: %s", ErrPermissionApprovalInvalidated, "current_policy_denied")
	}
	return levelStr, asks, nil
}

// PersistExactPermissionAllowRule 校验一次审批并保存标准的 User 或 Session 增量。
//
// Python: persist_exact_permission_allow_rule (permissions_persist.py L195-299)
//
// 异常三态（对齐 Python 的两层 except）：
//   - ErrPermissionApprovalInvalidated：RaiseOnInvalid 为真则原样返回，否则静默 nil
//   - 其他错误：包装成 approval_validation_failed，同上处理
func PersistExactPermissionAllowRule(opts ExactAllowOptions) error {
	err := persistExactPermissionAllowRule(opts)
	if err == nil {
		return nil
	}
	if !opts.RaiseOnInvalid {
		return nil
	}
	if errors.Is(err, ErrPermissionApprovalInvalidated) {
		return err
	}
	return fmt.Errorf("%w: %s", ErrPermissionApprovalInvalidated, "approval_validation_failed")
}

// PersistPermissionAllowRule 用户选择「总是允许」时，将 allow 规则写入 config.yaml 的 permissions 段。
// Python: persist_permission_allow_rule(tool_name, tool_args) (permissions_persist.py L132-150)
func PersistPermissionAllowRule(toolName string, toolArgs map[string]any) bool {
	toolArgs = normalizeToolArgs(toolArgs)

	persistLock.Lock()
	defer persistLock.Unlock()

	data := readYAMLData()
	permissions, _ := data["permissions"].(map[string]any)
	if permissions == nil {
		logger.Warn(logComponent).
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
	schemaKey := strings.TrimSpace(strings.ToLower(utils.StrVal(permissions["schema"])))
	if schemaKey == "" {
		schemaKey = strings.TrimSpace(strings.ToLower(utils.StrVal(permissions["version"])))
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
// Python: _has_override_id(overrides, oid) (permissions_persist.py L73-74)
func hasOverrideID(overrides []map[string]any, oid string) bool {
	for _, item := range overrides {
		if utils.StrVal(item["id"]) == oid {
			return true
		}
	}
	return false
}

// appendOverrideIfMissing 若 overrides 中不存在指定 ID，则追加一条 override 到 permissions["approval_overrides"]。
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
// 对应 Python: Path(raw_path.strip()).expanduser().resolve(strict=False).as_posix().rstrip("/")
func expandAndResolvePath(rawPath string) string {
	cleaned := strings.TrimSpace(rawPath)
	if cleaned == "" {
		return ""
	}
	// expanduser + resolve(strict=False)：展开 ~、绝对化并解析已存在前缀的软链
	// （不要求目标存在）。统一走公共助手，避免各包重复实现产生语义分叉。
	resolved := pathutil.ResolveSymlinksLenient(cleaned)
	// POSIX 格式：反斜杠转正斜杠
	posix := filepath.ToSlash(resolved)
	// 去除尾部斜杠
	posix = strings.TrimRight(posix, "/")
	return posix
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

// persistExactPermissionAllowRule 的 14 步主体。
//
// Python: persist_exact_permission_allow_rule (permissions_persist.py L195-299)
// 步骤编号与 Python 行序一一对应，顺序不可调换。
func persistExactPermissionAllowRule(opts ExactAllowOptions) error {
	// ── 1-3. 参数归一与校验 ──
	name := strings.TrimSpace(opts.ToolName)
	args := normalizeToolArgs(opts.ToolArgs)
	accesses, ok := NormalizeExactAccesses(opts.AskAccesses)
	if name == "" || !ok {
		return fmt.Errorf("%w: %s", ErrPermissionApprovalInvalidated, "invalid_approval_scope")
	}
	if opts.SessionOnly && opts.SessionID == "" {
		return fmt.Errorf("%w: %s", ErrPermissionApprovalInvalidated, "invalid_approval_scope")
	}
	root := opts.WorkspaceRoot
	if root == "" {
		root = workspace.WorkspaceDir()
	}

	// ── 4. 存储锁 + 三层读取 ──
	unlock, err := PermissionStorageLock(opts.SessionID)
	if err != nil {
		return err
	}
	defer unlock()

	globalPerms, user, session, err := ReadPermissionLayersLocked(opts.SessionID, nil)
	if err != nil {
		return err
	}

	// ── 5. installed_policy_changed 检查 ──
	// Python:
	//   global_perms = resolve_env_vars(global_perms)
	//   if not validate_only and global_perms != expected: raise
	if opts.ExpectedGlobalPermissions != nil {
		if resolved, ok := configutil.ResolveEnvVars(globalPerms, nil).(map[string]any); ok {
			globalPerms = resolved
		}
		if !opts.ValidateOnly && !reflect.DeepEqual(globalPerms, opts.ExpectedGlobalPermissions) {
			return fmt.Errorf("%w: %s", ErrPermissionApprovalInvalidated, "installed_policy_changed")
		}
	}

	// ── 6. 组装 compose 闭包 ──
	compose := func(userLayer, sessionLayer map[string]any) map[string]any {
		return ComposeHostEffectivePermissions(
			globalPerms, userLayer, sessionLayer, opts.SessionID, opts.AutoMode,
		)
	}
	effective := compose(user, session)

	// ── 7. 当前访问必须被审批覆盖 ──
	currentLevel, currentAsks, err := CheckApprovalAccesses(effective, name, args, root)
	if err != nil {
		return err
	}
	if !isSubsetOf(currentAsks, accesses) {
		return fmt.Errorf("%w: %s", ErrPermissionApprovalInvalidated, "approval_accesses_changed")
	}

	// ── 8. 只校验模式提前返回 ──
	if opts.ValidateOnly {
		return nil
	}

	// ── 9. durable 检查：user 侧授权必须在本 session 已有授权之外仍成立 ──
	// Python: baseline = effective if session_only else compose(user, {})
	baseline := effective
	if !opts.SessionOnly {
		baseline = compose(user, map[string]any{})
	}
	if !opts.AutoMode && !opts.SessionOnly && currentLevel == "allow" {
		// Python: 当前已 ALLOW 时只需确认 durable 侧也 ALLOW 且无 asks，不写盘
		durableLevel, durableAsks, err := CheckApprovalAccesses(baseline, name, args, root)
		if err != nil {
			return err
		}
		if durableLevel == "allow" && len(durableAsks) == 0 {
			return nil
		}
	}
	_, targetAsks, err := CheckApprovalAccesses(baseline, name, args, root)
	if err != nil {
		return err
	}
	if !isSubsetOf(targetAsks, accesses) {
		return fmt.Errorf("%w: %s", ErrPermissionApprovalInvalidated, "approval_target_scope_changed")
	}

	// ── 10. 目标层增量 ──
	current := user
	if opts.SessionOnly {
		current = session
	}
	updated := shallowCopyPermissions(current)

	level, _ := harnesssecurity.EvaluateTieredPolicy(baseline, name, args)
	if level == harnesssecurity.PermissionLevelAsk {
		merged, applied := harnesssecurity.MergePermissionAllowRuleIntoPermissions(baseline, name, args)
		if !applied {
			return fmt.Errorf("%w: %s", ErrPermissionApprovalInvalidated, "approval_rule_unavailable")
		}
		// tools[name] 变了才把工具名加进 allow_tools
		if !reflect.DeepEqual(nestedTools(merged, name), nestedTools(baseline, name)) {
			allow := toStringList(updated["allow_tools"])
			if !containsString(allow, name) {
				allow = append(allow, name)
			}
			updated["allow_tools"] = toAnyList(allow)
		}
		// 新增的 approval_overrides 追加
		previous := toAnyList(baseline["approval_overrides"])
		additions := make([]any, 0)
		for _, item := range toAnyList(merged["approval_overrides"]) {
			if !anyListContains(previous, item) {
				additions = append(additions, item)
			}
		}
		if len(additions) > 0 {
			existing := toAnyList(updated["approval_overrides"])
			updated["approval_overrides"] = append(existing, additions...)
		}
	}

	// ── 11. 路径授权 ──
	if len(targetAsks) > 0 {
		accessesList := make([]harnesssecurity.PathAction, 0, len(targetAsks))
		for key := range targetAsks {
			accessesList = append(accessesList, harnesssecurity.PathAction{Path: key[0], Action: key[1]})
		}
		// Python: sorted(target_asks)
		sort.Slice(accessesList, func(i, j int) bool {
			if accessesList[i].Path != accessesList[j].Path {
				return accessesList[i].Path < accessesList[j].Path
			}
			return accessesList[i].Action < accessesList[j].Action
		})
		next, applied := harnesssecurity.MergeFileGuardAccessAllows(updated, accessesList)
		if !applied {
			return fmt.Errorf("%w: %s", ErrPermissionApprovalInvalidated, "approval_paths_unavailable")
		}
		updated = next
	}

	// ── 12. 最终校验：写完必须真能 ALLOW ──
	// Python: proposed = compose(user, updated) if session_only else compose(updated, {})
	proposed := compose(updated, map[string]any{})
	if opts.SessionOnly {
		proposed = compose(user, updated)
	}
	proposedLevel, proposedAsks, err := CheckApprovalAccesses(proposed, name, args, root)
	if err != nil {
		return err
	}
	if proposedLevel != "allow" || len(proposedAsks) > 0 {
		return fmt.Errorf("%w: %s", ErrPermissionApprovalInvalidated, "approval_not_covered")
	}

	// ── 13. 幂等返回 ──
	if reflect.DeepEqual(updated, current) {
		return nil
	}

	// ── 14. 写盘 ──
	if opts.SessionOnly {
		if !SaveSessionPermissionsLocked(opts.SessionID, updated) {
			logger.Error(logComponent).Msg("permission storage failed")
		}
		return nil
	}
	if !SaveUserPermissionsLocked(updated) {
		logger.Error(logComponent).Msg("permission storage failed")
	}
	return nil
}

// isSubsetOf 判断 asks 是否为 accesses 的子集。
//
// Python: current_asks.issubset(set(accesses))
func isSubsetOf(asks map[[2]string]struct{}, accesses [][2]string) bool {
	set := make(map[[2]string]struct{}, len(accesses))
	for _, access := range accesses {
		set[access] = struct{}{}
	}
	for ask := range asks {
		if _, ok := set[ask]; !ok {
			return false
		}
	}
	return true
}

// shallowCopyPermissions 浅拷贝一层 permissions 字典。
//
// Python 用 deepcopy，但 Go 侧增量只改顶层键（allow_tools / approval_overrides），
// 浅拷贝足够且避免把大对象整体复制。
func shallowCopyPermissions(source map[string]any) map[string]any {
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

// nestedTools 取 permissions["tools"][name]。
func nestedTools(permissions map[string]any, name string) any {
	tools, _ := permissions["tools"].(map[string]any)
	if tools == nil {
		return nil
	}
	return tools[name]
}

// toStringList 把值转成字符串列表。
func toStringList(value any) []string {
	items, ok := value.([]any)
	if !ok {
		if typed, isStrings := value.([]string); isStrings {
			return typed
		}
		return nil
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		if s, isString := item.(string); isString {
			result = append(result, s)
		}
	}
	return result
}

// toAnyList 把值转成 any 列表。
func toAnyList(value any) []any {
	if items, ok := value.([]any); ok {
		return items
	}
	if typed, ok := value.([]string); ok {
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index] = item
		}
		return result
	}
	return nil
}

// anyListContains 判断 any 列表是否含指定项（深比较）。
func anyListContains(items []any, target any) bool {
	for _, item := range items {
		if reflect.DeepEqual(item, target) {
			return true
		}
	}
	return false
}
