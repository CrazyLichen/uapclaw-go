// 任务级自动权限模式的运行时配置归一化。
//
// 对应 Python 代码：permissions/auto_config.py（315 行 / 5 导出 + 15 私有）
//
// 职责：把用户写的 permissions 配置（可能含 mode=auto）归一成运行时安全形态——
//  1. 补全 auto 段的默认选项（超时/输出格式/审计开关/受限写入上限）
//  2. 把 defaults["*"] 与各工具档位收窄到 auto 模式允许的取值（高风险工具强制 ask）
//  3. 同名工具的多条规则按「更严格者胜」合并，结构化规则（paths/commands/patterns）逐项合并
//
// 本文件只做归一化，不产生任何审批决策。

package permissions

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/permission_mode"
	"github.com/uapclaw/uapclaw-go/internal/common/permission_tools"
	swarmschema "github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// autoConfigLogComponent 日志组件
	autoConfigLogComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// structuredPermissionRuleMapKeys 需要逐项合并的结构化规则键。
	//
	// Python: _STRUCTURED_PERMISSION_RULE_MAP_KEYS = frozenset({"paths","commands","patterns"})
	structuredPermissionRuleMapKeys = map[string]struct{}{
		"paths":    {},
		"commands": {},
		"patterns": {},
	}

	// AutoPermissionDefaultOptions auto 段默认选项。
	//
	// Python: AUTO_PERMISSION_DEFAULT_OPTIONS
	// 注意 reviewer_output_format 的默认值是 "json_object"（不是 "json"）。
	AutoPermissionDefaultOptions = map[string]any{
		"reviewer_timeout_ms":          60000,
		"reviewer_output_format":       "json_object",
		"persistent_audit_enabled":     false,
		"bounded_write_max_files":      3,
		"bounded_write_excluded_paths": JiuwenclawProtectedWritePaths,
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ResolveDeclaredAutoWorkspace 返回唯一确定的显式工作区，冲突时报错。
//
// Python: resolve_declared_auto_workspace(params, metadata=None)
// 返回 nil 表示未声明；params.workspace_dir 与 project_dir 不一致时报
// auto_permission_workspace_conflict:new_session_required。
func ResolveDeclaredAutoWorkspace(
	params map[string]any,
	metadata map[string]any,
) (*string, error) {
	canonical := func(value any) (*string, error) {
		raw := strings.TrimSpace(anyToStringValue(value))
		if raw == "" {
			return nil, nil
		}
		resolved, err := resolveUserPathStrictFalse(raw)
		if err != nil {
			return nil, &AutoPermissionConfigError{"auto_permission_workspace_invalid"}
		}
		return &resolved, nil
	}

	workspace, err := canonical(mapGet(params, "workspace_dir"))
	if err != nil {
		return nil, err
	}
	projectRaw := mapGet(params, "project_dir")
	if projectRaw == nil {
		projectRaw = mapGet(metadata, "project_dir")
	}
	project, err := canonical(projectRaw)
	if err != nil {
		return nil, err
	}
	if workspace != nil && project != nil && *workspace != *project {
		return nil, &AutoPermissionConfigError{"auto_permission_workspace_conflict:new_session_required"}
	}
	if workspace != nil {
		return workspace, nil
	}
	return project, nil
}

// SupportsPhaseAutoRoot 该请求是否属于受支持的独立 root。
//
// Python: supports_phase_auto_root(params)
// 条件：非 team 模式 且 mode/work_mode 组合支持单 Agent 权限档位。
func SupportsPhaseAutoRoot(params map[string]any) bool {
	if mapGet(params, "team") != nil && anyToBool(mapGet(params, "team")) {
		return false
	}
	mode := mapGet(params, "mode")
	if mode == nil || anyToStringValue(mode) == "" {
		mode = "agent"
	}
	return swarmschema.SupportsSingleAgentPermissionProfile(mode, nil, mapGet(params, "work_mode"))
}

// NormalizePermissionsForRuntime 返回运行时安全的 permissions 配置，不改动入参。
//
// Python: normalize_permissions_for_runtime(permission_config)
// 非 auto 模式原样深拷贝返回；auto 模式下补全 auto 段、收窄 defaults["*"]、归一 tools。
func NormalizePermissionsForRuntime(permissionConfig map[string]any) map[string]any {
	normalized := deepCopyAnyMap(permissionConfig)
	if normalized == nil {
		normalized = map[string]any{}
	}
	if !permission_mode.IsAutoPermissionMode(permissionConfig) {
		return normalized
	}

	normalized["mode"] = permission_mode.AutoPermissionMode
	normalized["auto"] = NormalizeAutoPermissionOptions(mapGet(normalized, "auto"))

	defaults, ok := mapGet(normalized, "defaults").(map[string]any)
	if !ok {
		defaults = map[string]any{}
	} else {
		defaults = deepCopyAnyMap(defaults)
	}
	defaults["*"] = normalizeAutoDefaultLevel(mapGet(defaults, "*"))
	normalized["defaults"] = defaults

	if tools, ok := mapGet(normalized, "tools").(map[string]any); ok {
		normalized["tools"] = normalizeToolsForAutoMode(tools)
	}
	return normalized
}

// NormalizeAutoPermissionOptions 返回运行时安全的 permissions.auto 段。
//
// Python: normalize_auto_permission_options(raw_options)
// 按默认值的类型分派归一策略：布尔直转、整数走正整数校验、
// reviewer_output_format 走枚举白名单、bounded_write_excluded_paths 与保护路径合并。
func NormalizeAutoPermissionOptions(rawOptions any) map[string]any {
	options := map[string]any{}
	if raw, ok := rawOptions.(map[string]any); ok {
		options = deepCopyAnyMap(raw)
	}
	normalized := deepCopyAnyMap(AutoPermissionDefaultOptions)
	if normalized == nil {
		normalized = map[string]any{}
	}
	for key, defaultValue := range AutoPermissionDefaultOptions {
		rawValue, exists := options[key]
		if !exists {
			continue
		}
		switch typed := defaultValue.(type) {
		case bool:
			normalized[key] = anyToBool(rawValue)
		case int:
			normalized[key] = positiveInt(rawValue, typed)
		default:
			if key == "reviewer_output_format" {
				normalized[key] = NormalizeReviewerOutputFormat(rawValue)
			} else if key == "bounded_write_excluded_paths" {
				normalized[key] = MergeProtectedWritePaths(
					anyToStringSlice(defaultValue),
					normalizeStringTuple(rawValue, nil),
				)
			} else {
				normalized[key] = rawValue
			}
		}
	}
	return normalized
}

// NormalizeReviewerOutputFormat 归一隔离审查器的 Chat JSON 模式设置。
//
// Python: normalize_reviewer_output_format(value)
// 只接受 "json_object" / "prompt"，其余回落 "json_object"。
func NormalizeReviewerOutputFormat(value any) string {
	text, ok := value.(string)
	if ok && (text == "json_object" || text == "prompt") {
		return text
	}
	return "json_object"
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// normalizeToolsForAutoMode 归一 tools 段，同名工具的多条规则按更严格者合并。
//
// Python: _normalize_tools_for_auto_mode(tools)
func normalizeToolsForAutoMode(tools map[string]any) map[string]any {
	normalized := make(map[string]any, len(tools))
	for rawToolName, rawLevel := range tools {
		toolName := permission_tools.NormalizePermissionToolName(rawToolName)
		if toolName == "" {
			continue
		}
		entry := normalizeToolEntryForAutoMode(toolName, rawLevel)
		if existing, exists := normalized[toolName]; exists {
			entry = mergeToolEntriesForAutoMode(toolName, existing, entry)
		}
		normalized[toolName] = entry
	}
	return normalized
}

// normalizeAutoDefaultLevel 归一 defaults["*"]：只允许 deny 或 ask。
//
// Python: _normalize_auto_default_level(value)
// auto 模式下默认档位不能是 allow——否则等于关掉审查。
func normalizeAutoDefaultLevel(value any) string {
	level := normalizeLevel(value)
	if level == "deny" {
		return "deny"
	}
	return "ask"
}

// normalizeToolEntryForAutoMode 归一单个工具的规则条目。
//
// Python: _normalize_tool_entry_for_auto_mode(tool_name, entry)
// 非 map 走档位归一；map 则对四个档位键（* / default / level / permission）分别归一。
func normalizeToolEntryForAutoMode(toolName string, entry any) any {
	mapping, ok := entry.(map[string]any)
	if !ok {
		return normalizeToolLevelForAutoMode(toolName, entry)
	}
	normalizedEntry := deepCopyAnyMap(mapping)
	for _, key := range []string{"*", "default", "level", "permission"} {
		if raw, exists := normalizedEntry[key]; exists {
			normalizedEntry[key] = normalizeToolLevelForAutoMode(toolName, raw)
		}
	}
	return normalizedEntry
}

// normalizeToolLevelForAutoMode 归一工具档位：allow 但属高风险工具时强制 ask。
//
// Python: _normalize_tool_level_for_auto_mode(tool_name, value)
func normalizeToolLevelForAutoMode(toolName string, value any) string {
	level := normalizeLevel(value)
	if level == "allow" && toolRequiresAskInAuto(toolName) {
		return "ask"
	}
	return level
}

// mergeToolEntriesForAutoMode 合并同名工具的两条规则。
//
// Python: _merge_tool_entries_for_auto_mode(tool_name, existing, candidate)
// 两侧都不是 map 时直接返回更严格的档位；否则合并结构化规则并把 "*" 设为严格档位。
func mergeToolEntriesForAutoMode(toolName string, existing, candidate any) any {
	strictLevel := permission_tools.StricterPermissionLevel(
		toolEntryDefaultLevel(toolName, existing),
		toolEntryDefaultLevel(toolName, candidate),
	)
	_, existingIsMap := existing.(map[string]any)
	_, candidateIsMap := candidate.(map[string]any)
	if !existingIsMap && !candidateIsMap {
		return strictLevel
	}
	merged := mergeStructuredToolRules(existing, candidate)
	merged["*"] = strictLevel
	return merged
}

// toolEntryDefaultLevel 取条目当前生效的档位。
//
// Python: _tool_entry_default_level(tool_name, entry)
// map 按 * / default / level / permission 顺序取第一个存在的键；都没有时视为 ask。
func toolEntryDefaultLevel(toolName string, entry any) string {
	mapping, ok := entry.(map[string]any)
	if !ok {
		return normalizeToolLevelForAutoMode(toolName, entry)
	}
	for _, key := range []string{"*", "default", "level", "permission"} {
		if raw, exists := mapping[key]; exists {
			return normalizeToolLevelForAutoMode(toolName, raw)
		}
	}
	return "ask"
}

// mergeStructuredToolRules 合并两侧的结构化规则（跳过四个档位键）。
//
// Python: _merge_structured_tool_rules(left, right)
// 结构化规则键（paths/commands/patterns）逐项按严格者合并；
// 其余 map 值递归合并；标量直接取右侧。
func mergeStructuredToolRules(left, right any) map[string]any {
	merged := map[string]any{}
	for _, entry := range []any{left, right} {
		mapping, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		for key, value := range mapping {
			if isToolLevelKey(key) {
				continue
			}
			existing, exists := merged[key]
			if !exists {
				merged[key] = deepCopyAnyValue(value)
				continue
			}
			if _, structured := structuredPermissionRuleMapKeys[key]; structured {
				merged[key] = mergeStructuredRuleValues(key, existing, value)
				continue
			}
			existingMap, existingIsMap := existing.(map[string]any)
			valueMap, valueIsMap := value.(map[string]any)
			if existingIsMap && valueIsMap {
				merged[key] = mergeStructuredRuleMap(key, existingMap, valueMap)
				continue
			}
			merged[key] = deepCopyAnyValue(value)
		}
	}
	return merged
}

// mergeStructuredRuleValues 合并一个结构化规则键下的两侧取值。
//
// Python: _merge_structured_rule_values(rule_key, left, right)
// 两侧都解析不出规则 map 时直接取右侧深拷贝。
func mergeStructuredRuleValues(ruleKey string, left, right any) any {
	leftRules := structuredRuleMapFromValue(left)
	rightRules := structuredRuleMapFromValue(right)
	if len(leftRules) == 0 && len(rightRules) == 0 {
		return deepCopyAnyValue(right)
	}
	return mergeStructuredRuleMap(ruleKey, leftRules, rightRules)
}

// mergeStructuredRuleMap 逐项合并规则 map：同键按更严格档位合并。
//
// Python: _merge_structured_rule_map(rule_key, left, right)
func mergeStructuredRuleMap(ruleKey string, left, right map[string]any) map[string]any {
	merged := deepCopyAnyMap(left)
	if merged == nil {
		merged = map[string]any{}
	}
	for itemKey, itemValue := range right {
		existing, exists := merged[itemKey]
		if exists {
			if _, structured := structuredPermissionRuleMapKeys[ruleKey]; structured {
				merged[itemKey] = permission_tools.StricterPermissionLevel(
					normalizeLevel(existing),
					normalizeLevel(itemValue),
				)
				continue
			}
		}
		merged[itemKey] = deepCopyAnyValue(itemValue)
	}
	return merged
}

// structuredRuleMapFromValue 把结构化规则的任意形态解析成 pattern→level 映射。
//
// Python: _structured_rule_map_from_value(value)
// 支持两种形态：map（键即 pattern）、list/tuple（元素为字符串或含
// pattern/path/command + level/permission/action 的 map）。
func structuredRuleMapFromValue(value any) map[string]any {
	if mapping, ok := value.(map[string]any); ok {
		rules := make(map[string]any, len(mapping))
		for pattern, level := range mapping {
			rules[pattern] = normalizeLevel(level)
		}
		return rules
	}
	list, ok := value.([]any)
	if !ok {
		return map[string]any{}
	}
	rules := map[string]any{}
	for _, item := range list {
		if text, isString := item.(string); isString {
			existing, exists := rules[text]
			base := "allow"
			if exists {
				base = normalizeLevel(existing)
			}
			rules[text] = permission_tools.StricterPermissionLevel(base, "deny")
			continue
		}
		itemMap, isMap := item.(map[string]any)
		if !isMap {
			continue
		}
		pattern := itemMap["pattern"]
		if pattern == nil {
			pattern = itemMap["path"]
		}
		if pattern == nil {
			pattern = itemMap["command"]
		}
		if pattern == nil {
			continue
		}
		level, hasLevel := structuredRuleItemLevel(itemMap)
		if !hasLevel {
			continue
		}
		key := anyToStringValue(pattern)
		base := "allow"
		if existing, exists := rules[key]; exists {
			base = normalizeLevel(existing)
		}
		rules[key] = permission_tools.StricterPermissionLevel(base, level)
	}
	return rules
}

// structuredRuleItemLevel 取结构化规则项的档位。
//
// Python: _structured_rule_item_level(item)
// 按 level / permission / action 顺序取第一个存在的键。
func structuredRuleItemLevel(item map[string]any) (string, bool) {
	for _, key := range []string{"level", "permission", "action"} {
		if raw, exists := item[key]; exists {
			return normalizeLevel(raw), true
		}
	}
	return "", false
}

// normalizeLevel 归一权限档位：只接受 allow / ask / deny，其余回落 ask。
//
// Python: _normalize_level(value)
// 入参是 map 时先取 ["*"]；空值视为 ask。
func normalizeLevel(value any) string {
	if mapping, ok := value.(map[string]any); ok {
		value = mapping["*"]
	}
	level := strings.ToLower(strings.TrimSpace(anyToStringValue(value)))
	if level == "" {
		level = "ask"
	}
	switch level {
	case "allow", "ask", "deny":
		return level
	default:
		return "ask"
	}
}

// positiveInt 解析正整数，失败或非正时返回默认值。
//
// Python: _positive_int(value, default_value)
func positiveInt(value any, defaultValue int) int {
	parsed, ok := anyToInt(value)
	if !ok {
		return defaultValue
	}
	if parsed > 0 {
		return parsed
	}
	return defaultValue
}

// normalizeStringTuple 把任意值归一为去空白后的字符串切片。
//
// Python: _normalize_string_tuple(value, default_value)
// 非 list 输入返回默认值（nil 表示空）。
func normalizeStringTuple(value any, defaultValue []string) []string {
	list, ok := value.([]any)
	if !ok {
		return defaultValue
	}
	normalized := make([]string, 0, len(list))
	for _, item := range list {
		text := strings.TrimSpace(anyToStringValue(item))
		if text != "" {
			normalized = append(normalized, text)
		}
	}
	return normalized
}

// toolRequiresAskInAuto 该工具在 auto 模式下是否必须 ask。
//
// Python: _tool_requires_ask_in_auto(tool_name)
// 高灵活度（high_flex）或高风险等级（risk_tier == "high"）的工具强制人工确认。
func toolRequiresAskInAuto(toolName string) bool {
	capability := ClassifyTool(toolName)
	return capability.HighFlex || capability.RiskTier == "high"
}

// isToolLevelKey 是否工具档位键（合并结构化规则时跳过）。
func isToolLevelKey(key string) bool {
	switch key {
	case "*", "default", "level", "permission":
		return true
	default:
		return false
	}
}

// resolveUserPathStrictFalse 展开 ~ 并取绝对路径（Path.expanduser().resolve(strict=False)）。
func resolveUserPathStrictFalse(raw string) (string, error) {
	expanded := expandHomePath(raw)
	abs, err := filepath.Abs(expanded)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

// expandHomePath 展开 ~ 前缀为用户主目录（Path.expanduser 等价）。
func expandHomePath(raw string) string {
	if raw != "~" && !strings.HasPrefix(raw, "~/") && !strings.HasPrefix(raw, `~\`) {
		return raw
	}
	home := userHomeDir()
	if home == "" {
		return raw
	}
	rest := strings.TrimPrefix(raw, "~")
	rest = strings.TrimPrefix(rest, "/")
	rest = strings.TrimPrefix(rest, `\`)
	if rest == "" {
		return home
	}
	return filepath.Join(home, rest)
}

// AutoPermissionConfigError 自动权限配置错误。
//
// Python 侧抛 ValueError；Go 用带错误码的类型表达，调用方可按 Code 分支。
type AutoPermissionConfigError struct {
	// Code 错误码
	Code string
}

// Error 实现 error 接口。
func (e *AutoPermissionConfigError) Error() string {
	if e == nil {
		return ""
	}
	return e.Code
}

// anyToBool Python bool(value) 等价：None/零值/空容器为假，其余为真。
func anyToBool(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case bool:
		return typed
	case string:
		return typed != ""
	case int:
		return typed != 0
	case int64:
		return typed != 0
	case float64:
		return typed != 0
	case []any:
		return len(typed) > 0
	case map[string]any:
		return len(typed) > 0
	default:
		return true
	}
}

// anyToInt Python int(value) 等价：字符串/浮点可转，其余失败。
func anyToInt(value any) (int, bool) {
	switch typed := value.(type) {
	case nil:
		return 0, false
	case bool:
		if typed {
			return 1, true
		}
		return 0, true
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

// anyToStringSlice 把 []string 或 []any 归一为 []string。
func anyToStringSlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		out := make([]string, len(typed))
		copy(out, typed)
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, anyToStringValue(item))
		}
		return out
	default:
		return nil
	}
}

// mapGet 安全取 map 值（nil map 返回 nil）。
func mapGet(source map[string]any, key string) any {
	if source == nil {
		return nil
	}
	return source[key]
}

// DeepCopyPermissionConfig 深拷贝一份权限配置（供子包复用）。
//
// 等价于 Python 的 copy.deepcopy(permission_config)。
func DeepCopyPermissionConfig(config map[string]any) map[string]any {
	if config == nil {
		return map[string]any{}
	}
	return deepCopyAnyMap(config)
}

// AutoOptionTimeoutMs 从归一后的 auto 选项取审查器超时（毫秒）。
//
// Python: int(auto_options.get("reviewer_timeout_ms", 60000))
func AutoOptionTimeoutMs(autoOptions map[string]any) int {
	timeout, ok := anyToInt(autoOptions["reviewer_timeout_ms"])
	if !ok || timeout <= 0 {
		return 60000
	}
	return timeout
}

// AutoOptionOutputFormat 从归一后的 auto 选项取审查器输出格式。
//
// Python: auto_options["reviewer_output_format"]
func AutoOptionOutputFormat(autoOptions map[string]any) string {
	return NormalizeReviewerOutputFormat(autoOptions["reviewer_output_format"])
}
