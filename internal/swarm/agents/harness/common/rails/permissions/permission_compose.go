// Global ⊕ User ⊕ Session 权限合成，产出 Engine 使用的生效配置。
//
// 对应 Python 代码：permissions/permission_compose.py（362 行 / 1 导出 + 10 私有）
//
// 合并语义（P1）：
//  1. 整工具 deny 优先——任一层 deny 该工具则最终 deny，不可被放宽
//  2. User allow 可放宽 Global ask；Session allow 可放宽 User/Global ask
//  3. Session 的 ask/deny 字段被丢弃（不构成授权）
//  4. Host 事实（shell 工具名单、package_builtin_rules）不允许 User/Session 覆盖
//  5. 任何异常 → fail-closed（默认 ask，所有护栏开启）
//
// 本文件替换 permissions_layers.go 中 9.90b 落地的最小实现（9.90d-2 Task 19）。

package permissions

import (
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security/fileguard"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security/netguard"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security/toolguard"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// composeLogComponent 日志组件
	composeLogComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// sessionDropKeys 会话层被丢弃的字段（不构成授权）。
	//
	// Python: _SESSION_DROP
	sessionDropKeys = map[string]struct{}{
		"deny_tools": {},
		"ask_tools":  {},
		"enabled":    {},
	}

	// composeToolLevels 合法工具档位集合。
	//
	// Python: _TOOL_LEVELS
	composeToolLevels = map[string]struct{}{
		"allow": {},
		"ask":   {},
		"deny":  {},
	}

	// netActions 合法网络动作集合。
	//
	// Python: _NET_ACTIONS
	netActions = map[string]struct{}{
		"allow": {},
		"deny":  {},
	}

	// falseStrings 被视为假的字符串。
	//
	// Python: _FALSE_STRINGS
	falseStrings = map[string]struct{}{
		"false": {},
		"0":     {},
		"no":    {},
		"off":   {},
	}

	// p1SeverityToAction P1 严重度→动作映射。
	//
	// Python: _P1_SEVERITY_TO_ACTION
	// 只填空缺 action 的 host 规则；内置规则已带 YAML 默认动作。
	p1SeverityToAction = map[string]string{
		"LOW":      "allow",
		"MEDIUM":   "allow",
		"HIGH":     "ask",
		"CRITICAL": "deny",
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ComposeHostEffectivePermissions 合成 Global ⊕ User ⊕ Session 生效配置。
//
// Python: compose_host_effective_permissions(*, global_permissions=None,
// user_permissions=None, session_permissions=None, session_id=None,
// agent_permissions=None, auto_mode=False)
//
// 偏差：省略 Python 的 agent_permissions 参数——P1 阶段它被显式忽略
// （Python 函数体首行 `_ = (session_id, agent_permissions)`），保留会成死参数。
// sessionID 同样被忽略，保留仅为对齐签名。
func ComposeHostEffectivePermissions(
	globalPermissions map[string]any,
	userPermissions map[string]any,
	sessionPermissions map[string]any,
	sessionID string,
	autoMode bool,
) (result map[string]any) {
	_ = sessionID
	global := asPermissionsSection(globalPermissions)
	user := asPermissionsSection(userPermissions)
	session := map[string]any{}
	for key, value := range asPermissionsSection(sessionPermissions) {
		if _, drop := sessionDropKeys[key]; drop {
			continue
		}
		session[key] = value
	}

	// 兜底：任何 panic 都 fail-closed，绝不放过未判定配置
	defer func() {
		if recovered := recover(); recovered != nil {
			enabled := false
			if raw, ok := global["enabled"].(bool); ok {
				enabled = raw
			}
			logger.Error(composeLogComponent).
				Interface("recovered", recovered).
				Msg("permissions.compose failed")
			result = failClosed(anyToStringValue(recovered), enabled)
		}
	}()

	if autoMode {
		// 归一化后的 engine 投影是权威的：旧列表镜像不能把 ASK 翻回 ALLOW
		global = NormalizePermissionsForRuntime(
			ComposeHostEffectivePermissions(global, nil, nil, "", false))
		for _, key := range []string{"allow_tools", "ask_tools", "deny_tools"} {
			delete(global, key)
		}
	}

	globalLevels := namedLevels(global, false)
	userLevels := namedLevels(user, false)
	sessionLevels := namedLevels(session, true)

	names := make(map[string]struct{}, len(globalLevels)+len(userLevels)+len(sessionLevels))
	for name := range globalLevels {
		names[name] = struct{}{}
	}
	for name := range userLevels {
		names[name] = struct{}{}
	}
	for name := range sessionLevels {
		names[name] = struct{}{}
	}
	sortedNames := sortedKeys(names)

	tools := make(map[string]any, len(sortedNames))
	denyTools := make([]string, 0, len(sortedNames))
	askTools := make([]string, 0, len(sortedNames))
	allowTools := make([]string, 0, len(sortedNames))
	for _, name := range sortedNames {
		level, ok := pickLevel(globalLevels[name], userLevels[name], sessionLevels[name])
		if !ok {
			continue
		}
		tools[name] = level
		switch level {
		case "deny":
			denyTools = append(denyTools, name)
		case "ask":
			askTools = append(askTools, name)
		default:
			allowTools = append(allowTools, name)
		}
	}

	out := deepCopyAnyMap(global)
	if out == nil {
		out = map[string]any{}
	}
	out["tools"] = tools
	out["deny_tools"] = denyTools
	out["ask_tools"] = askTools
	out["allow_tools"] = allowTools
	if _, exists := out["enabled"]; !exists {
		out["enabled"] = false
	}
	out["rules"] = applyProductP1RuleActions(mergeListOfDicts(
		global["rules"], user["rules"], session["rules"]))
	out["approval_overrides"] = mergeListOfDicts(
		global["approval_overrides"], user["approval_overrides"], session["approval_overrides"])

	out["file_guard"] = composeFileGuard(global, user, session)
	out["net_guard"] = composeNetGuard(global, user)
	out["shell_guard"] = composeShellGuard(global, user)

	// Host 事实：User / Session 不得覆盖 shell 工具名单 / 包内 builtin_rules
	delete(out, "categories")
	out["categories"] = map[string]any{"shell": ShellToolNames()}
	usePackage := packageBuiltinRulesEnabled(global)
	out["package_builtin_rules"] = usePackage
	if !usePackage {
		return out
	}
	injected := toolguard.InlinePackageCommandRules(out)
	injected = fileguard.MergePackageSensitivePaths(injected, nil)
	return netguard.MergePackageNetURLs(injected, nil)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// packageBuiltinRulesEnabled 是否启用包内内置规则（仅 Global 层事实）。
//
// Python: _package_builtin_rules_enabled(layer)
// 缺失或非法默认 True；字符串 "false"/"0"/"no"/"off" 视为关闭。
func packageBuiltinRulesEnabled(layer map[string]any) bool {
	value := layer["package_builtin_rules"]
	if enabled, ok := value.(bool); ok {
		return enabled
	}
	if text, ok := value.(string); ok {
		if _, isFalse := falseStrings[trimLowerText(text)]; isFalse {
			return false
		}
	}
	return true
}

// namedLevels 提取某层的「工具名 → 档位」映射。
//
// Python: _named_levels(layer, *, drop_tighten=False)
// drop_tighten 为真时只保留 allow（会话层不能收紧）。
func namedLevels(layer map[string]any, dropTighten bool) map[string]string {
	out := map[string]string{}
	if tools, ok := layer["tools"].(map[string]any); ok {
		for name, raw := range tools {
			trimmed := trimSpaceText(name)
			if trimmed == "" {
				continue
			}
			level := scalarLevel(raw)
			if level == "" {
				continue
			}
			if dropTighten && (level == "deny" || level == "ask") {
				continue
			}
			out[trimmed] = level
		}
	}
	type levelKey struct {
		key   string
		level string
	}
	mapping := []levelKey{
		{"deny_tools", "deny"},
		{"ask_tools", "ask"},
		{"allow_tools", "allow"},
	}
	if dropTighten {
		mapping = []levelKey{{"allow_tools", "allow"}}
	}
	for _, item := range mapping {
		for _, name := range strNames(layer[item.key]) {
			out[name] = item.level
		}
	}
	return out
}

// pickLevel 三层档位合成规则。
//
// Python: _pick_level(global_lv, user_lv, session_lv)
// 顺序即优先级：任一层 deny → deny；session/user allow 放宽 ask；
// 否则回落到最先出现的非空档位。
func pickLevel(globalLevel, userLevel, sessionLevel string) (string, bool) {
	for _, level := range []string{globalLevel, userLevel} {
		if level == "deny" {
			return "deny", true
		}
	}
	if sessionLevel == "allow" {
		return "allow", true
	}
	if userLevel == "allow" {
		return "allow", true
	}
	for _, level := range []string{globalLevel, userLevel} {
		if level == "ask" {
			return "ask", true
		}
	}
	for _, level := range []string{globalLevel, userLevel, sessionLevel} {
		if level == "allow" {
			return "allow", true
		}
	}
	for _, level := range []string{globalLevel, userLevel, sessionLevel} {
		if level != "" {
			return level, true
		}
	}
	return "", false
}

// mergeListOfDicts 把多层 list 里的 map 项依次深拷贝拼接。
//
// Python: _merge_list_of_dicts(*layers)
func mergeListOfDicts(layers ...any) []any {
	out := []any{}
	for _, layer := range layers {
		list, ok := layer.([]any)
		if !ok {
			continue
		}
		for _, item := range list {
			if mapping, isMap := item.(map[string]any); isMap {
				out = append(out, deepCopyAnyMap(mapping))
			}
		}
	}
	return out
}

// applyProductP1RuleActions 给缺 action 的规则按严重度补动作。
//
// Python: _apply_product_p1_rule_actions(rules)
// 已有非空 action 的规则原样保留；severity 映射不到时也不补。
func applyProductP1RuleActions(rules []any) []any {
	filled := make([]any, 0, len(rules))
	for _, rule := range rules {
		mapping, ok := rule.(map[string]any)
		if !ok {
			continue
		}
		item := deepCopyAnyMap(mapping)
		if action, isString := item["action"].(string); isString && trimSpaceText(action) != "" {
			filled = append(filled, item)
			continue
		}
		severity := upperTrimSpace(anyToStringValue(item["severity"]))
		if mapped, exists := p1SeverityToAction[severity]; exists {
			item["action"] = mapped
		}
		filled = append(filled, item)
	}
	return filled
}

// netAction 解析网络规则动作为 allow / deny。
//
// Python: _net_action(raw)
// map 入参先取 action，缺失再取 fetch。
func netAction(raw any) (string, bool) {
	if mapping, ok := raw.(map[string]any); ok {
		if action := mapping["action"]; action != nil {
			raw = action
		} else {
			raw = mapping["fetch"]
		}
	}
	text, ok := raw.(string)
	if !ok {
		return "", false
	}
	action := trimLowerText(text)
	if _, valid := netActions[action]; !valid {
		return "", false
	}
	return action, true
}

// mergeNetURLs 合并多层 URL 规则：deny 不可被后续 allow 放宽。
//
// Python: _merge_net_urls(*layers)
func mergeNetURLs(layers ...any) map[string]any {
	merged := map[string]any{}
	for _, layer := range layers {
		mapping, ok := layer.(map[string]any)
		if !ok {
			continue
		}
		for pattern, value := range mapping {
			key := trimSpaceText(pattern)
			if key == "" {
				continue
			}
			action, valid := netAction(value)
			if !valid {
				continue
			}
			if existing, exists := merged[key]; exists && existing == "deny" && action == "allow" {
				continue
			}
			if action == "deny" {
				merged[key] = "deny"
				continue
			}
			merged[key] = "allow"
		}
	}
	return merged
}

// asShellGuardFlag 取布尔开关，非布尔返回 not-ok。
//
// Python: _as_shell_guard_flag(value)
func asShellGuardFlag(value any) (bool, bool) {
	flag, ok := value.(bool)
	return flag, ok
}

// composeShellGuard 合成 shell 护栏（只看 Global ⊕ User，忽略 Session）。
//
// Python: _compose_shell_guard(global_perms, user_perms)
// 后层覆盖前层；缺失或非法一律 True（fail-closed）。
func composeShellGuard(globalPerms, userPerms map[string]any) map[string]any {
	globalGuard := asPermissionsSection(globalPerms["shell_guard"])
	userGuard := asPermissionsSection(userPerms["shell_guard"])

	flag := func(key string) bool {
		if value, ok := asShellGuardFlag(userGuard[key]); ok {
			return value
		}
		if value, ok := asShellGuardFlag(globalGuard[key]); ok {
			return value
		}
		return true
	}
	return map[string]any{
		"unknown_structure": flag("unknown_structure"),
		"interpreter_sink":  flag("interpreter_sink"),
	}
}

// composeNetGuard 合成网络护栏（只看 Global ⊕ User，忽略 Session）。
//
// Python: _compose_net_guard(global_perms, user_perms)
// User 不能关闭或放宽 Global 的 deny。
func composeNetGuard(globalPerms, userPerms map[string]any) map[string]any {
	globalGuard := asPermissionsSection(globalPerms["net_guard"])
	userGuard := asPermissionsSection(userPerms["net_guard"])

	enabled := true
	if raw, exists := globalGuard["enabled"]; exists {
		enabled = anyToBool(raw)
	}
	globalDefaults, globalOK := netAction(globalGuard["defaults"])
	userDefaults, userOK := netAction(userGuard["defaults"])

	var defaults string
	switch {
	case globalOK && globalDefaults == "deny" && userOK && userDefaults == "allow":
		defaults = "deny"
	case userOK:
		defaults = userDefaults
	case globalOK:
		defaults = globalDefaults
	default:
		defaults = "allow"
	}
	return map[string]any{
		"enabled":  enabled,
		"defaults": defaults,
		"urls":     mergeNetURLs(globalGuard["urls"], userGuard["urls"]),
	}
}

// composeFileGuard 合成路径护栏：Global 为基底，User/Session 只叠加 paths。
//
// Python: compose_host_effective_permissions 内的 file_guard 段
// User/Session 只能追加路径规则，不能改 defaults 或 enabled。
func composeFileGuard(globalPerms, userPerms, sessionPerms map[string]any) map[string]any {
	guard := map[string]any{}
	if base, ok := globalPerms["file_guard"].(map[string]any); ok {
		guard = deepCopyAnyMap(base)
		if guard == nil {
			guard = map[string]any{}
		}
	}
	for _, layer := range []map[string]any{userPerms, sessionPerms} {
		overlay, ok := layer["file_guard"].(map[string]any)
		if !ok {
			continue
		}
		extra, ok := overlay["paths"].([]any)
		if !ok {
			continue
		}
		paths, ok := guard["paths"].([]any)
		if !ok {
			paths = []any{}
		}
		for _, item := range extra {
			if mapping, isMap := item.(map[string]any); isMap {
				paths = append(paths, deepCopyAnyMap(mapping))
			}
		}
		guard["paths"] = paths
	}
	return guard
}

// failClosed 构造 fail-closed 配置：默认 ask、所有护栏开启。
//
// Python: _fail_closed(reason, enabled)
func failClosed(reason string, enabled bool) map[string]any {
	logger.Error(composeLogComponent).
		Str("reason", reason).
		Msg("permissions.compose fail-closed")
	return map[string]any{
		"enabled":               enabled,
		"schema":                "tiered_policy",
		"permission_mode":       "normal",
		"defaults":              map[string]any{"*": "ask"},
		"tools":                 map[string]any{},
		"rules":                 []any{},
		"approval_overrides":    []any{},
		"categories":            map[string]any{"shell": ShellToolNames()},
		"package_builtin_rules": true,
		"file_guard": map[string]any{
			"enabled":  true,
			"defaults": map[string]any{"read": "ask", "write": "ask", "exec": "ask"},
			"paths":    []any{},
		},
		"shell_guard": map[string]any{"unknown_structure": true, "interpreter_sink": true},
	}
}

// trimSpaceText 去首尾空白。
func trimSpaceText(text string) string { return strings.TrimSpace(text) }

// trimLowerText 去首尾空白并转小写。
func trimLowerText(text string) string { return strings.ToLower(strings.TrimSpace(text)) }

// upperTrimSpace 去首尾空白并转大写。
func upperTrimSpace(text string) string { return strings.ToUpper(strings.TrimSpace(text)) }

// sortedKeys 返回 map 键的有序切片。
func sortedKeys(source map[string]struct{}) []string {
	out := make([]string, 0, len(source))
	for key := range source {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
