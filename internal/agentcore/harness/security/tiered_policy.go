package security

import (
	"fmt"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/dlclark/regexp2"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/resources"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security/toolguard"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/utils"
)

// ──────────────────────────── 结构体 ────────────────────────────

// tieredInvocationContext 分层调用上下文。
//
// Python: _TieredInvocationContext (tiered_policy.py L53-61)
type tieredInvocationContext struct {
	// mode 权限模式（normal 或 strict）
	Mode string
	// builtinRules 内置规则列表
	BuiltinRules []map[string]any
	// rules 用户规则列表
	Rules []map[string]any
	// approvalOverrides 审批覆盖列表
	ApprovalOverrides []map[string]any
	// baselineLevel 基线权限级别
	BaselineLevel PermissionLevel
	// baselineRule 基线规则标识
	BaselineRule string
	// defaultsCfg 默认配置
	DefaultsCfg map[string]any
}

// subcommandResult 子命令评估结果
type subcommandResult struct {
	Command     string
	Permission  PermissionLevel
	MatchedRule string
}

// paramRuleHit 参数规则命中
type paramRuleHit struct {
	Level PermissionLevel
	Label string
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// mr 模块名前缀
	// Python: _MR = "tiered_policy" (tiered_policy.py L49)
	mr = "tiered_policy"

	// approvalOverridesPrefix 审批覆盖前缀
	// Python: _APPROVAL_OVERRIDES_PREFIX (tiered_policy.py L50)
	approvalOverridesPrefix = mr + ":approval_overrides"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// shellOperatorsRE Shell 操作符正则：检测命令链/注入元字符。
//
// 如果命令匹配 allow 模式但同时包含这些操作符，权限从 ALLOW → ASK 作为安全网。
//
// Python: _SHELL_OPERATORS_RE (checker.py L36-40)
var shellOperatorsRE = regexp.MustCompile(
	`[;&|` + "`" + `<>]` +
		`|\$[({]` +
		`|\r?\n`)

// strictOrder 权限严格度排序（DENY < ASK < ALLOW）
// Python: _STRICT_ORDER (tiered_policy.py L28)
var strictOrder = map[PermissionLevel]int{
	PermissionLevelDeny:  0,
	PermissionLevelAsk:   1,
	PermissionLevelAllow: 2,
}

// shellTools Shell 工具集合
// Python: _SHELL_TOOLS (tiered_policy.py L31)
var shellTools = map[string]bool{
	"bash": true, "mcp_exec_command": true, "create_terminal": true,
	"powershell": true, "core.powershell": true,
}

// categoryShell shell 语义类别的名字。内置规则用 `tools: [shell]` 写法，
// 需展开为具体 shell 工具集合后匹配。
//
// Python: _CATEGORY_SHELL (tool_categories.py)
const categoryShell = "shell"

// pathTools 路径工具集合
// Python: _PATH_TOOLS (tiered_policy.py L32-38)
var pathTools = map[string]bool{
	"read_file": true, "write_file": true, "edit_file": true,
	"read_text_file": true, "write_text_file": true,
	"write": true, "read": true,
	"glob_file_search": true, "glob": true, "list_dir": true, "list_files": true,
	"grep": true, "search_replace": true,
}

// PathTools 路径类工具集合，供 fileguard 子包引用。
//
// 与内部 pathTools 同源，导出仅供跨包子包使用（Go 无法跨包访问非导出符号）。
//
// Python: _PATH_TOOLS (tool_policy.py)
var PathTools = pathTools

// WritePathTools 写类路径工具集合，供 fileguard 子包引用。
//
// Python: _WRITE_PATH_TOOLS (file_guard.py / path_extract.py)
var WritePathTools = map[string]bool{
	"write_file": true, "edit_file": true, "write_text_file": true,
	"write": true, "search_replace": true, "apply_patch": true,
}

// ShellTools 导出 shell 工具集合，供 fileguard / netguard 子包判断工具类别。
var ShellTools = shellTools

// IterPathStrings 遍历一次调用里所有像路径的字符串参数。
//
// Python: _iter_path_strings(tool_name, tool_args)
func IterPathStrings(toolName string, toolArgs map[string]any) []string {
	return iterPathStrings(toolName, toolArgs)
}

// CommandText 取工具参数里的命令行文本（command 优先，其次 cmd）。
//
// Python: _command_text(tool_args)
func CommandText(toolArgs map[string]any) string {
	return commandText(toolArgs)
}

// WithShellCommand 替换 toolArgs 中的 command/cmd 为给定命令文本。
//
// Python: _with_shell_command(tool_args, command)
func WithShellCommand(toolArgs map[string]any, command string) map[string]any {
	return withShellCommand(toolArgs, command)
}

// iterPathStrings 遍历工具参数中的路径字符串。
//
// 顺序说明：Python 的 dict 保插入序，Go 的 map 无序，因此抽取顺序可能
// 与 Python 不同。file_guard 的 reason 取首个命中路径，跨工具参数多路径
// 场景下文案可能与 Python 不一致（判定结果本身不受影响）。
//
// Python: _iter_path_strings(tool_name, tool_args) (tool_policy.py L369-377)
func iterPathStrings(toolName string, toolArgs map[string]any) []string {
	var out []string
	for k, v := range toolArgs {
		s, ok := v.(string)
		if !ok || strings.TrimSpace(s) == "" {
			continue
		}
		if toolArgValueLooksLikePath(k, s) {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out
}

// toolArgValueLooksLikePath 判断参数值是否纳入路径类 pattern 匹配。
//
// Python: _tool_arg_value_looks_like_path(arg_key, value) (tool_policy.py L360-367)
func toolArgValueLooksLikePath(argKey, value string) bool {
	if pathArgKeys[argKey] {
		return true
	}
	if strings.Contains(value, "/") || strings.Contains(value, `\`) {
		return true
	}
	if len(value) > 1 && value[1] == ':' {
		return true
	}
	return false
}

// networkTools 网络工具集合
// Python: _NETWORK_TOOLS (tiered_policy.py L39)
var networkTools = map[string]bool{
	"mcp_fetch_webpage": true, "mcp_free_search": true, "mcp_paid_search": true,
}

// pathArgKeys 路径参数键集合
// Python: _PATH_ARG_KEYS (tiered_policy.py L41-44)
var pathArgKeys = map[string]bool{
	"path": true, "file_path": true, "target_file": true, "file": true,
	"old_path": true, "new_path": true, "source_path": true, "dest_path": true,
	"directory": true, "dir": true,
}

// builtinRulesCache 内置规则缓存
// Python: _BUILTIN_RULES_CACHE (tiered_policy.py L47)
// 三元组: (路径字符串, mtime, 规则列表)
var builtinRulesCache struct {
	path   string
	mtime  float64
	rules  []map[string]any
	loaded bool
}

// builtinRulesCacheMu 保护 builtinRulesCache 的并发访问
var builtinRulesCacheMu sync.Mutex

// ──────────────────────────── 导出函数 ────────────────────────────

// EvaluateTieredPolicy 返回 (最终权限, matched_rule 摘要)。
//
// 优先级链：
// 1. 整工具 deny 优先于参数级放行
// 2. Shell 工具: ParseShellForPermission → shellAstFloor
// 3. Shell "simple" 含子命令: 逐个 evaluateSingleInvocation → aggregateSubcommandResults
// 4. evaluateSingleInvocation: builtin-param-rules(DENY短路) → user-param-rules(DENY短路) → approval-overrides(ALLOW) → builtin-hits → user-hits → baseline → defaults → fallback-ASK
// 5. applyShellAstFloor 应用 Shell AST 权限下限
// 6. MaybeEscalateShellOperators 可能升级 Shell 操作符判定
//
// Python: evaluate_tiered_policy(permission_config, tool_name, tool_args) (tiered_policy.py L508-581)
func EvaluateTieredPolicy(permissionConfig map[string]any, toolName string, toolArgs map[string]any) (PermissionLevel, string) {
	mode := parsePermissionMode(permissionConfig)

	toolsCfg := mapAnyField(permissionConfig, "tools")
	defaultsCfg := mapAnyField(permissionConfig, "defaults")
	rules := listAnyField(permissionConfig, "rules")
	approvalOverrides := listAnyField(permissionConfig, "approval_overrides")

	// 内置规则必须已在 config["rules"] 里（layer=builtin），由
	// PreparePermissionsForEngine 在 ingest 阶段注入。判定路径不再 load YAML。
	// Python: builtin_rules = [r for r in dict_rules if r.get("layer") == "builtin"]
	//         user_rules    = [r for r in dict_rules if r.get("layer") != "builtin"]
	builtinRules, userRules := splitByBuiltinLayer(rules)

	bl, blRule := baselineLevel(toolsCfg, toolName)
	if bl == PermissionLevelDeny {
		if blRule == "" {
			blRule = mr + ":tools.deny"
		}
		return PermissionLevelDeny, blRule
	}

	// shell_guard 两个宿主开关：unknown_structure / interpreter_sink。
	// Python: check_unknown, check_sink = _read_shell_guard(permission_config)
	checkUnknown, checkSink := readShellGuard(permissionConfig)

	// 命令脱壳：原始与脱壳两份并存（不是替换）。
	// Python: original_cmd / canon_cmd / canon_args
	originalCmd := commandText(toolArgs)
	canonCmd := toolguard.CanonicalizeShellCommandForPermission(originalCmd)
	argsDiffer := canonCmd != originalCmd
	var canonArgs map[string]any
	if argsDiffer {
		canonArgs = withShellCommand(toolArgs, canonCmd)
	}

	// shell AST 用脱壳后的命令解析。
	// Python: shell_parse = parse_shell_for_permission(canon_cmd)
	var shellParse *ShellAstParseResult
	isShell := toolCategory(toolName) == "shell"
	if isShell {
		shellParse = ParseShellForPermission(canonCmd)
	}
	shellFloor, shellFloorRule := shellAstFloorFor(shellParse, checkUnknown)

	invCtx := &tieredInvocationContext{
		Mode:              mode,
		BuiltinRules:      builtinRules,
		Rules:             userRules,
		ApprovalOverrides: approvalOverrides,
		BaselineLevel:     bl,
		BaselineRule:      blRule,
		DefaultsCfg:       defaultsCfg,
	}

	// override 命中：先查原始，空了再查脱壳。
	// Python: override_hits = ... ; if not override_hits and canon_args is not tool_args: ...
	overrideHits := collectApprovalOverrideHits(approvalOverrides, toolName, toolArgs)
	if len(overrideHits) == 0 && argsDiffer {
		overrideHits = collectApprovalOverrideHits(approvalOverrides, toolName, canonArgs)
	}

	if isShell && shellParse != nil && shellParse.Kind == ShellAstKindSimple {
		var subcommandResults []subcommandResult
		for _, subcmd := range shellParse.Subcommands {
			if subcmd.Text == "" {
				continue
			}
			// 子命令以原始 tool_args 作基底（Python 用的是 tool_args，不是 canon_args）
			subArgs := withShellCommand(toolArgs, subcmd.Text)
			result := evaluateSingleInvocation(toolName, subArgs, invCtx)
			subcommandResults = append(subcommandResults, result)
			if result.Permission == PermissionLevelDeny {
				break
			}
		}
		aggregated := aggregateSubcommandResults(subcommandResults)
		permission, matchedRule := aggregated.Permission, aggregated.MatchedRule

		// 整串参数规则复核：原始与脱壳各评一次，取最严。
		// Python: orig_full / canon_full → full_candidates → strictest
		var fullCandidates []subcommandResult
		if origFull := evaluateParamRulesOnly(toolName, toolArgs, invCtx); origFull != nil {
			fullCandidates = append(fullCandidates, *origFull)
		}
		if argsDiffer {
			if canonFull := evaluateParamRulesOnly(toolName, canonArgs, invCtx); canonFull != nil {
				fullCandidates = append(fullCandidates, *canonFull)
			}
		}
		if len(fullCandidates) > 0 {
			fullLevel := fullCandidates[0].Permission
			fullRule := fullCandidates[0].MatchedRule
			for _, cand := range fullCandidates[1:] {
				merged := Strictest(fullLevel, cand.Permission)
				// Python: next(rule for lvl, rule in full_candidates if lvl == full_level)
				// —— 取第一个达到 fullLevel 的候选规则
				if merged == cand.Permission && merged != fullLevel {
					fullRule = cand.MatchedRule
				}
				fullLevel = merged
			}
			permission = Strictest(permission, fullLevel)
			if permission == fullLevel {
				matchedRule = fullRule
			}
		}

		// interpreter_sink 守卫：管道末段是解释器（curl x | bash）。
		// Python: if check_sink and _has_interpreter_sink(shell_parse)
		if checkSink && hasInterpreterSink(shellParse) {
			permission = Strictest(permission, PermissionLevelAsk)
			if permission == PermissionLevelAsk {
				sinkRule := mr + ":shell_guard:interpreter_sink"
				if matchedRule != "" {
					matchedRule = sinkRule + "|" + matchedRule
				} else {
					matchedRule = sinkRule
				}
			}
		}

		permission, matchedRule = applyShellAstFloor(permission, matchedRule, shellFloor, shellFloorRule)
		return allowOverridesUnlessDenied(permission, matchedRule, overrideHits)
	}

	// 非 shell-simple：shell 类用脱壳后的 args，其余用原始 args。
	// Python: eval_args = canon_args if _tool_category(...) == "shell" else tool_args
	evalArgs := toolArgs
	if isShell && argsDiffer {
		evalArgs = canonArgs
	}
	result := evaluateSingleInvocation(toolName, evalArgs, invCtx)
	permission, matchedRule := applyShellAstFloor(result.Permission, result.MatchedRule, shellFloor, shellFloorRule)
	return allowOverridesUnlessDenied(permission, matchedRule, overrideHits)
}

// Strictest 返回最严格的权限级别。
//
// Python: strictest(*levels) (tiered_policy.py L118-121)
func Strictest(levels ...PermissionLevel) PermissionLevel {
	if len(levels) == 0 {
		return PermissionLevelAsk
	}
	result := levels[0]
	for _, l := range levels[1:] {
		if strictOrder[l] < strictOrder[result] {
			result = l
		}
	}
	return result
}

// SeverityToDecision 将严重级别映射为权限决策。
//
// Python: severity_to_decision(severity, permission_mode) (tiered_policy.py L124-138)
func SeverityToDecision(severity, permissionMode string) PermissionLevel {
	sev := strings.ToUpper(strings.TrimSpace(severity))
	mode := strings.ToLower(strings.TrimSpace(permissionMode))
	if mode != "normal" && mode != "strict" {
		mode = "normal"
	}

	switch sev {
	case "LOW":
		return PermissionLevelAllow
	case "MEDIUM":
		if mode == "strict" {
			return PermissionLevelAsk
		}
		return PermissionLevelAllow
	case "HIGH":
		return PermissionLevelAsk
	case "CRITICAL":
		if mode == "strict" {
			return PermissionLevelDeny
		}
		return PermissionLevelAsk
	default:
		logger.Warn(logComponent).
			Str("severity", severity).
			Msg("permission.tiered_policy.unknown_severity")
		return PermissionLevelAsk
	}
}

// MaybeEscalateShellOperators 命令含链式/注入元字符时 ALLOW→ASK。
// approval_overrides 豁免。
//
// Python: maybe_escalate_shell_operators(tool_name, tool_args, permission) (tiered_policy.py L584-599)
func MaybeEscalateShellOperators(toolName string, toolArgs map[string]any, permission PermissionLevel, matchedRule string) PermissionLevel {
	if !shellTools[toolName] {
		return permission
	}
	if permission != PermissionLevelAllow {
		return permission
	}
	// approval_overrides 豁免
	if MatchedRuleUsesApprovalOverride(matchedRule) {
		return permission
	}
	cmd := commandText(toolArgs)
	if cmd != "" && shellOperatorsRE.MatchString(cmd) {
		return PermissionLevelAsk
	}
	return permission
}

// MatchedRuleUsesApprovalOverride 当前结果是否来自 approval_overrides。
//
// Python: matched_rule_uses_approval_override(matched_rule) (tiered_policy.py L602-606)
func MatchedRuleUsesApprovalOverride(matchedRule string) bool {
	return strings.HasPrefix(matchedRule, approvalOverridesPrefix)
}

// ruleTargetsTool 判定规则是否作用于该工具。
//
// 两条路径（对齐 Python _rule_targets_tool）：
//  1. 工具名直接出现在 rules[].tools 里
//  2. rules[].tools 含类别名 "shell"，且该工具属 shell 工具集合
//
// 第 2 条是内置规则的用法：builtin_rules.yaml 里写的是 `tools: [shell]`，
// 并非具体工具名，需要按语义类别展开。
//
// Python: _rule_targets_tool(tool_name, r_tools, permission_config) (tool_policy.py L148-158)
func ruleTargetsTool(toolName string, rTools []string) bool {
	trimmed := make([]string, 0, len(rTools))
	for _, t := range rTools {
		if v := strings.TrimSpace(t); v != "" {
			trimmed = append(trimmed, v)
		}
	}
	if containsString(trimmed, toolName) {
		return true
	}
	return containsString(trimmed, categoryShell) && shellTools[toolName]
}

// RuleToolsCategoryConsistent 检查规则中 tools 是否同类。
//
// Python: rule_tools_category_consistent(tools) (tiered_policy.py L151-160)
func RuleToolsCategoryConsistent(tools []string) bool {
	cats := make(map[string]bool)
	for _, t := range tools {
		c := toolCategory(t)
		if c == "" {
			return false
		}
		cats[c] = true
		if len(cats) > 1 {
			return false
		}
	}
	return len(cats) > 0
}

// GetBuiltinSecurityRules 获取内置安全规则列表（进程内按路径+mtime 缓存）。
//
// 优先从文件系统读取（支持 mtime 自动重载），回退到嵌入资源。
//
// Python: get_builtin_security_rules() (tiered_policy.py L64-90)
func GetBuiltinSecurityRules() []map[string]any {
	builtinRulesCacheMu.Lock()
	defer builtinRulesCacheMu.Unlock()

	// 尝试从文件系统读取（对齐 Python: 从包内 resources/builtin_rules.yaml 读取）
	yamlPath := resources.ResolveBuiltinRulesYAMLPath()
	if yamlPath != "" {
		mtime := resources.GetFileModTime(yamlPath)
		// 缓存命中：路径和 mtime 未变
		if builtinRulesCache.loaded && builtinRulesCache.path == yamlPath && builtinRulesCache.mtime == mtime {
			return builtinRulesCache.rules
		}

		// mtime 变化或首次加载：从文件系统读取
		parsed, err := resources.ParseBuiltinRulesFromFile(yamlPath)
		if err != nil {
			logger.Warn(logComponent).
				Str("path", yamlPath).
				Err(err).
				Msg("permission.tiered_policy.builtin_rules_file_read_failed")
		} else {
			rules := convertBuiltinRules(parsed)
			builtinRulesCache.path = yamlPath
			builtinRulesCache.mtime = mtime
			builtinRulesCache.rules = rules
			builtinRulesCache.loaded = true
			return rules
		}
	}

	// 回退到嵌入资源
	if builtinRulesCache.loaded {
		return builtinRulesCache.rules
	}

	parsed, err := resources.ParseBuiltinRules()
	if err != nil {
		logger.Warn(logComponent).
			Err(err).
			Msg("permission.tiered_policy.builtin_rules_missing")
		builtinRulesCache.loaded = true
		return nil
	}

	rules := convertBuiltinRules(parsed)
	builtinRulesCache.rules = rules
	builtinRulesCache.loaded = true
	return rules
}

// TieredPolicyRuleMatches 单条 rule 是否对本次调用匹配。
//
// Python: tiered_policy_rule_matches(tool_name, pattern, tool_args, rule_tools) (tiered_policy.py L325-345)
func TieredPolicyRuleMatches(toolName string, pattern string, toolArgs map[string]any, ruleTools []string) bool {
	if len(ruleTools) == 0 {
		return false
	}
	cat := toolCategory(ruleTools[0])
	switch cat {
	case "shell":
		return shellPatternMatches(pattern, commandText(toolArgs))
	case "path":
		for _, val := range iterPathStrings(toolName, toolArgs) {
			if pathPatternMatches(pattern, val) {
				return true
			}
		}
		return false
	case "network":
		// 产品设计：网络类暂仅整工具；参数规则不匹配
		return false
	default:
		return false
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// splitByBuiltinLayer 把规则列表按 layer=="builtin" 切成内置与用户两份。
//
// Python: builtin_rules = [r for r in dict_rules if r.get("layer") == "builtin"]
func splitByBuiltinLayer(rules []map[string]any) (builtin, user []map[string]any) {
	for _, r := range rules {
		if layer, _ := r["layer"].(string); layer == "builtin" {
			builtin = append(builtin, r)
		} else {
			user = append(user, r)
		}
	}
	return builtin, user
}

// getBuiltinSecurityRules 别名（内部用）
func getBuiltinSecurityRules() []map[string]any {
	return GetBuiltinSecurityRules()
}

// convertBuiltinRules 将 BuiltinRules 转换为 []map[string]any
func convertBuiltinRules(parsed *resources.BuiltinRules) []map[string]any {
	rules := make([]map[string]any, 0, len(parsed.Rules))
	for _, r := range parsed.Rules {
		tools := make([]string, len(r.TargetTools))
		copy(tools, r.TargetTools)
		entry := map[string]any{
			"id":         r.ID,
			"tools":      tools,
			"match_type": r.MatchType,
			"pattern":    r.Pattern,
			"severity":   r.Severity,
		}
		if r.Action != "" {
			entry["action"] = r.Action
		}
		rules = append(rules, entry)
	}
	return rules
}

// parsePermissionMode 从配置中解析权限模式
func parsePermissionMode(config map[string]any) string {
	mode := strings.ToLower(strings.TrimSpace(utils.StrVal(config["permission_mode"])))
	if mode != "normal" && mode != "strict" {
		mode = "normal"
	}
	return mode
}

// toolCategory 获取工具类别。
//
// 首行显式处理类别名本身（"shell"），对齐 Python _tool_category 的
// `if tool_name == _CATEGORY_SHELL or is_shell_tool(...)`——内置规则用
// `tools: [shell]` 这种语义类别写法，不走具体工具名。
//
// Python: _tool_category(tool_name) (tool_policy.py L116-130)
func toolCategory(toolName string) string {
	if toolName == categoryShell || shellTools[toolName] {
		return "shell"
	}
	if pathTools[toolName] {
		return "path"
	}
	if networkTools[toolName] {
		return "network"
	}
	return ""
}

// baselineLevel 获取整工具基线权限
// Python: _baseline_level(tools_cfg, tool_name) (tiered_policy.py L348-375)
func baselineLevel(toolsCfg map[string]any, toolName string) (PermissionLevel, string) {
	if toolsCfg == nil {
		return PermissionLevelNone, "" // 无配置，对齐 Python baseline_level is None
	}
	raw, ok := toolsCfg[toolName]
	if !ok {
		return PermissionLevelNone, "" // 无配置，对齐 Python baseline_level is None
	}

	switch v := raw.(type) {
	case string:
		level, err := ParsePermissionLevel(v)
		if err != nil {
			logger.Warn(logComponent).
				Str("tool", toolName).
				Str("value", v).
				Msg("permission.tiered_policy.invalid_tool_level")
			return PermissionLevelAllow, ""
		}
		return level, fmt.Sprintf("tools.%s", toolName)
	case map[string]any:
		// dict 类型，取 * 键
		asterisk, ok := v["*"].(string)
		if !ok {
			logger.Warn(logComponent).
				Str("tool", toolName).
				Msg("permission.tiered_policy.tools_dict_non_scalar: using=asterisk_only")
			return PermissionLevelAllow, ""
		}
		level, err := ParsePermissionLevel(asterisk)
		if err != nil {
			return PermissionLevelAllow, ""
		}
		return level, fmt.Sprintf("tools.%s.*", toolName)
	default:
		logger.Warn(logComponent).
			Str("tool", toolName).
			Msg("permission.tiered_policy.invalid_tool_baseline: reason=non_scalar_level")
		return PermissionLevelAllow, ""
	}
}

// parseLevel 解析权限级别字符串
// Python: _parse_level(value) (tiered_policy.py L113-115)
func parseLevel(value string) PermissionLevel {
	level, err := ParsePermissionLevel(strings.TrimSpace(value))
	if err != nil {
		return PermissionLevelAsk
	}
	return level
}

// compiledShellRegexes 编译 shell 正则；整段失败时按 "|" 拆段逐个尝试。
//
// 保留拆段降级是因为 YAML 双引号落盘后 `C:\Users` 可能变成非法 `\U`，
// 旧版 `posix|win` 整段会编译失败。这与 regexp2 无关，是 YAML 转义问题。
//
// Python: _compiled_shell_regexes(expr, flags)
func compiledShellRegexes(expr, flags string) []*regexp2.Regexp {
	if re, err := regexp2.Compile(flags+expr, 0); err == nil {
		return []*regexp2.Regexp{re}
	}
	var out []*regexp2.Regexp
	if strings.Contains(expr, "|") {
		for _, part := range strings.Split(expr, "|") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if re, err := regexp2.Compile(flags+part, 0); err == nil {
				out = append(out, re)
			}
		}
	}
	if len(out) == 0 {
		logger.Warn(logComponent).
			Str("expr", expr).
			Msg("permission.tiered_policy.invalid_shell_regex")
	}
	return out
}

// maxShellRegexDepth 引号内递归匹配的最大深度。
//
// Python: _MAX_SHELL_REGEX_DEPTH = 4
const maxShellRegexDepth = 4

// shellQuotedSpanRe 匹配完整的一对单/双引号（含转义）。
//
// Python: _SHELL_QUOTED_SPAN_RE
var shellQuotedSpanRe = regexp2.MustCompile(`"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'`, 0)

// shellEvalPrefixRe 判定引号前是否为「求值载荷」前缀（shell -c / python -c / ssh 远程）。
//
// Python: _SHELL_EVAL_PREFIX_RE / _CODE_EVAL_PREFIX_RE / _REMOTE_PAYLOAD_PREFIX_RE
// 三条正则在 Go 侧合并为一条等价集合（见 9.90b Task 3b 的偏差登记）。
var shellEvalPrefixRe = regexp2.MustCompile(
	`(?ix)(?:^|(?<=[;&|()]))\s*`+
		`(?:`+
		`(?:sudo|doas|pkexec|timeout|su|env|command|nice|nohup|xargs|time)(?:\s+\S+)*\s+`+
		`|ssh(?:\.exe)?(?:\s+\S+)*\s+`+
		`|docker(?:\.exe)?\s+(?:exec|run)(?:\s+\S+)*\s+`+
		`)*`+
		`(?:[^\s;&|()]*[\\/])?`+
		`(?:bash|sh|zsh|dash|ash|fish|pwsh|powershell|cmd|python(?:3|w)?|py|ruby|perl)(?:\.exe)?`+
		`(?:\s+-{1,2}[A-Za-z][A-Za-z-]*)*`+
		`\s+-[A-Za-z]*[ce][A-Za-z]*\s+$`, 0)

// substDollarRe 匹配 $(...) 命令替换。
//
// Python: _SUBST_DOLLAR_RE
var substDollarRe = regexp2.MustCompile(`\$\((.*)\)`, regexp2.Singleline)

// substBacktickRe 匹配 `...` 命令替换。
//
// Python: _SUBST_BACKTICK_RE
var substBacktickRe = regexp2.MustCompile("`([^`]*)`", 0)

// quotedSpanEntry 一个引号区间及其原文。
type quotedSpanEntry struct {
	start int
	end   int
	text  string
}

// spanEntries 返回命令里所有引号区间（含原文）。
func spanEntries(command string) []quotedSpanEntry {
	m, _ := shellQuotedSpanRe.FindStringMatch(command)
	var out []quotedSpanEntry
	for m != nil {
		out = append(out, quotedSpanEntry{start: m.Index, end: m.Index + m.Length, text: m.String()})
		m, _ = shellQuotedSpanRe.FindNextMatch(m)
	}
	return out
}

// shellQuotedSpans 返回命令里所有引号区间的 [start, end) 列表。
func shellQuotedSpans(command string) [][2]int {
	entries := spanEntries(command)
	out := make([][2]int, 0, len(entries))
	for _, e := range entries {
		out = append(out, [2]int{e.start, e.end})
	}
	return out
}

// indexInAnySpan 判定下标是否严格落在某个区间内（不含端点）。
func indexInAnySpan(index int, spans [][2]int) bool {
	for _, sp := range spans {
		if index > sp[0] && index < sp[1] {
			return true
		}
	}
	return false
}

// quoteSpanIsEvalPayload 判定引号前是否紧跟求值载荷前缀。
//
// ⚠️ quoteStart 是 **rune 索引**（来自 regexp2 的 Match.Index），不能直接
// 用于字节切片——含非 ASCII 的命令会错位。先转 []rune 再切。
//
// Python: _quote_span_is_eval_payload(command, quote_start)
func quoteSpanIsEvalPayload(command string, quoteStart int) bool {
	runes := []rune(command)
	if quoteStart <= 0 || quoteStart > len(runes) {
		return false
	}
	prefix := string(runes[:quoteStart])
	ok, _ := shellEvalPrefixRe.MatchString(prefix)
	return ok
}

// doubleQuoteHasCommandSubstitution 双引号内是否含命令替换（单引号不展开）。
//
// Python: _double_quote_has_command_substitution(span_text)
func doubleQuoteHasCommandSubstitution(spanText string) bool {
	if !strings.HasPrefix(spanText, `"`) || len(spanText) < 2 {
		return false
	}
	inner := spanText[1 : len(spanText)-1]
	return strings.Contains(inner, "$(") || strings.Contains(inner, "`")
}

// commandSubstitutionBodies 提取 $(...) 与 `...` 的内部命令。
//
// Python: _command_substitution_bodies(text)
func commandSubstitutionBodies(text string) []string {
	var out []string
	if m, _ := substDollarRe.FindStringMatch(text); m != nil {
		for m != nil {
			if g := m.GroupByNumber(1); g != nil && strings.TrimSpace(g.String()) != "" {
				out = append(out, g.String())
			}
			m, _ = substDollarRe.FindNextMatch(m)
		}
	}
	if m, _ := substBacktickRe.FindStringMatch(text); m != nil {
		for m != nil {
			if g := m.GroupByNumber(1); g != nil && strings.TrimSpace(g.String()) != "" {
				out = append(out, g.String())
			}
			m, _ = substBacktickRe.FindNextMatch(m)
		}
	}
	return out
}

// regexMatchesShellText 判定正则在命令文本中命中，且命中位置不在「非求值引号」内。
//
// 三层判定（对齐 Python _regex_matches_shell_text）：
//  1. 原文与反斜杠归一化文本都试
//  2. 命中位置若落在引号内，且该引号不是求值载荷 → 跳过（`echo "rm -rf /"` 不判违规）
//  3. 若引号是求值载荷（bash -c / python -c / ssh 远程载荷），递归检查引号内文本
//
// Python: _regex_matches_shell_text(compiled, command, *, depth=0)
func regexMatchesShellText(re *regexp2.Regexp, command string, depth ...int) bool {
	d := 0
	if len(depth) > 0 {
		d = depth[0]
	}
	if d > maxShellRegexDepth || command == "" {
		return false
	}

	candidates := []string{command}
	if normalized := strings.ReplaceAll(command, `\`, "/"); normalized != command {
		candidates = append(candidates, normalized)
	}

	for _, text := range candidates {
		spans := shellQuotedSpans(text)
		if m, _ := re.FindStringMatch(text); m != nil {
			for m != nil {
				if !indexInAnySpan(m.Index, spans) {
					return true
				}
				m, _ = re.FindNextMatch(m)
			}
		}
		// 引号内是否为求值载荷
		for _, sp := range spanEntries(text) {
			if !quoteSpanIsEvalPayload(text, sp.start) && !doubleQuoteHasCommandSubstitution(sp.text) {
				continue
			}
			inner := strings.TrimSuffix(strings.TrimPrefix(sp.text, string(sp.text[0])), string(sp.text[0]))
			if inner != "" && regexMatchesShellText(re, inner, d+1) {
				return true
			}
			if doubleQuoteHasCommandSubstitution(sp.text) {
				for _, body := range commandSubstitutionBodies(inner) {
					if regexMatchesShellText(re, body, d+1) {
						return true
					}
				}
			}
		}
	}
	return false
}

// shellPatternMatches Shell 模式匹配
// Python: _shell_pattern_matches(pattern, command) (tiered_policy.py L167-205)
func shellPatternMatches(pattern, command string) bool {
	if pattern == "" || command == "" {
		return false
	}
	p := strings.TrimSpace(pattern)

	// Python: flags = re.IGNORECASE if sys.platform == "win32" else 0
	regexFlags := ""
	if runtime.GOOS == "windows" {
		regexFlags = "(?i)"
	}

	if strings.HasPrefix(strings.ToLower(p), "re:") {
		expr := strings.TrimSpace(p[3:])
		for _, re := range compiledShellRegexes(expr, regexFlags) {
			if regexMatchesShellText(re, command) {
				return true
			}
		}
		return false
	}

	// 通配符
	if strings.ContainsAny(p, "*?[") {
		return MatchWildcard(p, command)
	}
	return command == p
}

// pathPatternMatches 路径模式匹配
// Python: _path_pattern_matches(pattern, value) (tiered_policy.py L208-220)
func pathPatternMatches(pattern, value string) bool {
	if pattern == "" || value == "" {
		return false
	}
	p := strings.TrimSpace(pattern)

	// Python: flags = re.IGNORECASE if sys.platform == "win32" else 0
	regexFlags := ""
	if runtime.GOOS == "windows" {
		regexFlags = "(?i)"
	}

	if strings.HasPrefix(strings.ToLower(p), "re:") {
		expr := strings.TrimSpace(p[3:])
		re, err := regexp.Compile(regexFlags + expr)
		if err != nil {
			logger.Warn(logComponent).
				Str("expr", expr).
				Msg("permission.tiered_policy.invalid_path_regex")
			return false
		}
		return re.MatchString(strings.ReplaceAll(value, `\`, "/"))
	}
	return tieredPathMatcher.MatchPath(p, value)
}

// collectParamRuleHits 收集参数级规则命中列表。
// Python: _collect_param_rule_hits(rules, tool_name, tool_args, mode, label_ns) (tiered_policy.py L242-284)
func collectParamRuleHits(rules []map[string]any, toolName string, toolArgs map[string]any, mode, labelNS string) []paramRuleHit {
	var hits []paramRuleHit
	for _, rule := range rules {
		rTools := ruleToolsListFromAny(rule["tools"])
		if !ruleTargetsTool(toolName, rTools) {
			continue
		}
		rToolsStr := make([]string, 0, len(rTools))
		for _, x := range rTools {
			x = strings.TrimSpace(x)
			if x != "" {
				rToolsStr = append(rToolsStr, x)
			}
		}
		if !RuleToolsCategoryConsistent(rToolsStr) {
			logger.Warn(logComponent).
				Str("id", utils.StrVal(rule["id"])).
				Strs("tools", rToolsStr).
				Msg("permission.tiered_policy.rule_skipped: reason=inconsistent_tool_category")
			continue
		}

		pattern, _ := rule["pattern"].(string)
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}

		if !TieredPolicyRuleMatches(toolName, pattern, toolArgs, rToolsStr) {
			continue
		}

		// 确定 level
		var dec PermissionLevel
		action, _ := rule["action"].(string)
		if strings.TrimSpace(action) != "" {
			dec = parseLevel(action)
		} else {
			sev, _ := rule["severity"].(string)
			if sev == "" {
				sev = "HIGH"
			}
			dec = SeverityToDecision(sev, mode)
		}

		rid, _ := rule["id"].(string)
		var label string
		if rid != "" {
			label = fmt.Sprintf("%s[%s]", labelNS, rid)
		} else {
			label = fmt.Sprintf("%s[?]", labelNS)
		}
		hits = append(hits, paramRuleHit{Level: dec, Label: label})
	}
	return hits
}

// collectApprovalOverrideHits 收集审批覆盖命中列表。
// Python: _collect_approval_override_hits(rules, tool_name, tool_args) (tiered_policy.py L287-322)
func collectApprovalOverrideHits(rules []map[string]any, toolName string, toolArgs map[string]any) []string {
	var hits []string
	for _, rule := range rules {
		action := strings.ToLower(strings.TrimSpace(utils.StrVal(rule["action"])))
		if action != "allow" {
			continue
		}
		rTools := ruleToolsListFromAny(rule["tools"])
		if !ruleTargetsTool(toolName, rTools) {
			continue
		}
		rToolsStr := make([]string, 0, len(rTools))
		for _, x := range rTools {
			x = strings.TrimSpace(x)
			if x != "" {
				rToolsStr = append(rToolsStr, x)
			}
		}
		if !RuleToolsCategoryConsistent(rToolsStr) {
			logger.Warn(logComponent).
				Str("id", utils.StrVal(rule["id"])).
				Strs("tools", rToolsStr).
				Msg("permission.tiered_policy.override_skipped: reason=inconsistent_tool_category")
			continue
		}

		pattern, _ := rule["pattern"].(string)
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}

		if !TieredPolicyRuleMatches(toolName, pattern, toolArgs, rToolsStr) {
			continue
		}

		rid, _ := rule["id"].(string)
		var label string
		if rid != "" {
			label = fmt.Sprintf("approval_overrides[%s]", rid)
		} else {
			label = "approval_overrides[?]"
		}
		hits = append(hits, label)
	}
	return hits
}

// evaluateSingleInvocation 评估单次调用的权限。
// Python: _evaluate_single_invocation(tool_name, tool_args, ctx) (tiered_policy.py L436-485)
func evaluateSingleInvocation(toolName string, toolArgs map[string]any, ctx *tieredInvocationContext) subcommandResult {
	// 1. 内置参数规则
	builtinHits := collectParamRuleHits(ctx.BuiltinRules, toolName, toolArgs, ctx.Mode, "builtin")
	if hasDenyHit(builtinHits) {
		level, rule := finalizeHits(builtinHits, "builtin")
		return subcommandResult{Permission: level, MatchedRule: rule}
	}

	// 2. 用户参数规则
	userHits := collectParamRuleHits(ctx.Rules, toolName, toolArgs, ctx.Mode, "rules")
	if hasDenyHit(userHits) {
		level, rule := finalizeHits(userHits, "rules")
		return subcommandResult{Permission: level, MatchedRule: rule}
	}

	// 3. 审批覆盖
	overrideHits := collectApprovalOverrideHits(ctx.ApprovalOverrides, toolName, toolArgs)
	if len(overrideHits) > 0 {
		sort.Strings(overrideHits)
		// 去重
		unique := dedupStrings(overrideHits)
		rule := approvalOverridesPrefix + ":" + strings.Join(unique, "+")
		return subcommandResult{Permission: PermissionLevelAllow, MatchedRule: rule}
	}

	// 4. 有内置命中 → 取最严格
	if len(builtinHits) > 0 {
		level, rule := finalizeHits(builtinHits, "builtin")
		return subcommandResult{Permission: level, MatchedRule: rule}
	}

	// 5. 有用户命中 → 取最严格
	if len(userHits) > 0 {
		level, rule := finalizeHits(userHits, "rules")
		return subcommandResult{Permission: level, MatchedRule: rule}
	}

	// 6. 基线
	// Python: if ctx.baseline_level is not None → 使用基线
	// BaselineLevel != PermissionLevelNone 表示 tools 配置中存在该工具名
	if ctx.BaselineLevel != PermissionLevelNone {
		return subcommandResult{Permission: ctx.BaselineLevel, MatchedRule: ctx.BaselineRule}
	}

	// 7. 默认配置
	if ctx.DefaultsCfg != nil {
		if defaultVal, ok := ctx.DefaultsCfg["*"].(string); ok {
			dl := parseLevel(defaultVal)
			return subcommandResult{Permission: dl, MatchedRule: mr + ":defaults.*"}
		}
	}

	// 8. Fallback 兜底回退
	return subcommandResult{Permission: PermissionLevelAsk, MatchedRule: mr + ":fallback(no_config)"}
}

// shellAstFloor Shell AST 地板权限。
// Python: _shell_ast_floor(shell_parse) (tiered_policy.py L388-408)
func shellAstFloorFor(shellParse *ShellAstParseResult, unknownStructure bool) (PermissionLevel, string) {
	if shellParse == nil || !unknownStructure {
		return PermissionLevelAllow, "" // 无 floor
	}
	flags := &shellParse.Flags

	if shellParse.Kind == ShellAstKindTooComplex {
		reason := shellParse.Reason
		if reason == "" {
			reason = "unsupported_complex_structure"
		}
		return PermissionLevelAsk, fmt.Sprintf("%s:shell_ast:too_complex:%s", mr, reason)
	}

	if shellParse.Kind == ShellAstKindParseUnavailable && flags.HasRiskyStructure() {
		reason := shellParse.Reason
		if reason == "" {
			reason = "conservative_fallback"
		}
		return PermissionLevelAsk, fmt.Sprintf("%s:shell_ast:parse_unavailable:%s", mr, reason)
	}

	return PermissionLevelAllow, "" // 无 floor
}

// interpreterSinkNames 管道末段若为这些解释器，视为远程载荷注入。
//
// Python: _INTERPRETER_SINK_NAMES
var interpreterSinkNames = map[string]struct{}{
	"bash": {}, "sh": {}, "zsh": {}, "dash": {}, "ash": {}, "fish": {},
	"python": {}, "python3": {}, "pythonw": {}, "py": {},
	"node": {}, "nodejs": {},
	"ruby": {}, "perl": {},
	"pwsh": {}, "powershell": {},
	"cmd":               {},
	"iex":               {},
	"invoke-expression": {},
	"invoke-command":    {},
}

// readShellGuard 读取 shell_guard 的两个宿主开关，非 dict 时都默认 true。
//
// Python: _read_shell_guard(permission_config)
func readShellGuard(permissionConfig map[string]any) (unknownStructure, interpreterSink bool) {
	raw, ok := permissionConfig["shell_guard"].(map[string]any)
	if !ok {
		return true, true
	}
	return asShellGuardFlag(raw["unknown_structure"], true),
		asShellGuardFlag(raw["interpreter_sink"], true)
}

// asShellGuardFlag 读一个布尔开关，非 bool 时用默认值。
//
// Python: _as_shell_guard_flag(value, default=True)
func asShellGuardFlag(v any, def bool) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return def
}

// hasInterpreterSink 判定管道末段是否为解释器（curl x | bash 这类注入）。
//
// Python: _has_interpreter_sink(shell_parse)
func hasInterpreterSink(shellParse *ShellAstParseResult) bool {
	if shellParse == nil || len(shellParse.Subcommands) < 2 {
		return false
	}
	if !hasPipeline(shellParse) {
		return false
	}
	for i, sub := range shellParse.Subcommands {
		if i == 0 {
			continue
		}
		if _, hit := interpreterSinkNames[sinkBasename(sub)]; hit {
			return true
		}
	}
	return false
}

// hasPipeline 判定解析结果是否含管道。
//
// flags.Pipeline 之外还看子命令的 ParentOperators——tree-sitter 在
// 某些结构下只标后者。
//
// Python: _has_pipeline(shell_parse)
func hasPipeline(shellParse *ShellAstParseResult) bool {
	if shellParse.Flags.Pipeline {
		return true
	}
	for _, sub := range shellParse.Subcommands {
		for _, op := range sub.ParentOperators {
			if strings.Contains(op, "|") {
				return true
			}
		}
	}
	return false
}

// sinkBasename 取子命令的可执行名（小写、去路径、去 .exe）。
//
// Python: _sink_basename(subcommand)
func sinkBasename(sub ShellSubcommand) string {
	token := ""
	if len(sub.Argv) > 0 {
		token = sub.Argv[0]
	} else if text := strings.TrimSpace(sub.Text); text != "" {
		token = strings.Fields(text)[0]
	}
	token = strings.ReplaceAll(token, `\`, "/")
	if idx := strings.LastIndex(token, "/"); idx >= 0 {
		token = token[idx+1:]
	}
	base := strings.ToLower(token)
	return strings.TrimSuffix(base, ".exe")
}

// evaluateParamRulesOnly 只评参数规则（内置 + 用户），不看 baseline/defaults。
// 返回 nil 表示无命中。
//
// Python: _evaluate_param_rules_only(tool_name, tool_args, ctx)
func evaluateParamRulesOnly(toolName string, toolArgs map[string]any, ctx *tieredInvocationContext) *subcommandResult {
	builtinHits := collectParamRuleHits(ctx.BuiltinRules, toolName, toolArgs, ctx.Mode, "builtin")
	if hasDenyHit(builtinHits) {
		level, rule := finalizeHits(builtinHits, "builtin")
		return &subcommandResult{Permission: level, MatchedRule: rule}
	}
	userHits := collectParamRuleHits(ctx.Rules, toolName, toolArgs, ctx.Mode, "rules")
	if hasDenyHit(userHits) {
		level, rule := finalizeHits(userHits, "rules")
		return &subcommandResult{Permission: level, MatchedRule: rule}
	}
	if len(builtinHits) > 0 {
		level, rule := finalizeHits(builtinHits, "builtin")
		return &subcommandResult{Permission: level, MatchedRule: rule}
	}
	if len(userHits) > 0 {
		level, rule := finalizeHits(userHits, "rules")
		return &subcommandResult{Permission: level, MatchedRule: rule}
	}
	return nil
}

// allowOverridesUnlessDenied 整命令级的 override 兜底：可放宽 ASK，不可放宽 DENY。
//
// Python: _allow_overrides_unless_denied(permission, matched_rule, override_hits)
func allowOverridesUnlessDenied(permission PermissionLevel, matchedRule string, overrideHits []string) (PermissionLevel, string) {
	if permission == PermissionLevelDeny || len(overrideHits) == 0 {
		return permission, matchedRule
	}
	sorted := dedupStrings(append([]string(nil), overrideHits...))
	sort.Strings(sorted)
	return PermissionLevelAllow, approvalOverridesPrefix + ":" + strings.Join(sorted, "+")
}

// applyShellAstFloor 应用 Shell AST 地板权限。
// Python: _apply_shell_ast_floor(permission, matched_rule, shell_floor, shell_floor_rule) (tiered_policy.py L411-424)
func applyShellAstFloor(permission PermissionLevel, matchedRule string, shellFloor PermissionLevel, shellFloorRule string) (PermissionLevel, string) {
	if shellFloor == PermissionLevelAllow && shellFloorRule == "" {
		return permission, matchedRule
	}
	final := Strictest(permission, shellFloor)
	if final == permission {
		return permission, matchedRule
	}
	if matchedRule != "" && shellFloorRule != "" {
		return final, shellFloorRule + "|" + matchedRule
	}
	if shellFloorRule != "" {
		return final, shellFloorRule
	}
	return final, matchedRule
}

// aggregateSubcommandResults 聚合子命令结果。
// Python: _aggregate_subcommand_results(results) (tiered_policy.py L488-505)
func aggregateSubcommandResults(results []subcommandResult) subcommandResult {
	if len(results) == 0 {
		return subcommandResult{Permission: PermissionLevelAsk, MatchedRule: mr + ":shell_subcommands:fallback"}
	}
	if len(results) == 1 {
		return results[0]
	}

	final := results[0].Permission
	for _, r := range results[1:] {
		if strictOrder[r.Permission] < strictOrder[final] {
			final = r.Permission
		}
	}

	// 收集最严格级别的 contributing
	var contributing []string
	for _, r := range results {
		if r.Permission == final {
			contributing = append(contributing, r.Command+"=>"+r.MatchedRule)
		}
	}
	sort.Strings(contributing)
	if len(contributing) == 0 {
		return subcommandResult{Permission: final, MatchedRule: mr + ":shell_subcommands"}
	}
	return subcommandResult{Permission: final, MatchedRule: mr + ":shell_subcommands:" + strings.Join(contributing, "+")}
}

// hasDenyHit 是否有 DENY 命中
func hasDenyHit(hits []paramRuleHit) bool {
	for _, h := range hits {
		if h.Level == PermissionLevelDeny {
			return true
		}
	}
	return false
}

// finalizeHits 确定命中结果
// Python: _finalize_hits(hits, prefix) (tiered_policy.py L378-385)
func finalizeHits(hits []paramRuleHit, prefix string) (PermissionLevel, string) {
	if hasDenyHit(hits) {
		var contributing []string
		for _, h := range hits {
			if h.Level == PermissionLevelDeny {
				contributing = append(contributing, h.Label)
			}
		}
		sort.Strings(contributing)
		deduped := dedupStrings(contributing)
		return PermissionLevelDeny, fmt.Sprintf("%s:%s:deny:%s", mr, prefix, strings.Join(deduped, "+"))
	}

	// 取最严格
	final := hits[0].Level
	for _, h := range hits[1:] {
		if strictOrder[h.Level] < strictOrder[final] {
			final = h.Level
		}
	}

	var contributing []string
	for _, h := range hits {
		if h.Level == final {
			contributing = append(contributing, h.Label)
		}
	}
	sort.Strings(contributing)
	deduped := dedupStrings(contributing)

	if len(deduped) > 0 {
		return final, fmt.Sprintf("%s:%s:%s", mr, prefix, strings.Join(deduped, "+"))
	}
	return final, fmt.Sprintf("%s:%s", mr, prefix)
}

// withShellCommand 替换 toolArgs 中的 command/cmd 为子命令文本
// Python: _with_shell_command(tool_args, command) (tiered_policy.py L427-433)
func withShellCommand(toolArgs map[string]any, command string) map[string]any {
	subArgs := make(map[string]any, len(toolArgs))
	for k, v := range toolArgs {
		subArgs[k] = v
	}
	// Python: if "command" in sub_args or "cmd" not in sub_args:
	if _, hasCommand := toolArgs["command"]; hasCommand {
		subArgs["command"] = command
	} else if _, hasCmd := toolArgs["cmd"]; !hasCmd {
		subArgs["command"] = command
	}
	if _, hasCmd := toolArgs["cmd"]; hasCmd {
		subArgs["cmd"] = command
	}
	return subArgs
}

// mapAnyField 从 map[string]any 中获取子 map
func mapAnyField(m map[string]any, key string) map[string]any {
	if m == nil {
		return nil
	}
	v, ok := m[key]
	if !ok {
		return nil
	}
	sub, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	return sub
}

// listAnyField 从 map[string]any 中获取 []map[string]any
func listAnyField(m map[string]any, key string) []map[string]any {
	if m == nil {
		return nil
	}
	v, ok := m[key]
	if !ok {
		return nil
	}
	switch list := v.(type) {
	case []any:
		result := make([]map[string]any, 0, len(list))
		for _, item := range list {
			if sub, ok := item.(map[string]any); ok {
				result = append(result, sub)
			}
		}
		return result
	case []map[string]any:
		return list
	default:
		return nil
	}
}

// ruleToolsListFromAny 从 any 类型中提取工具列表
func ruleToolsListFromAny(v any) []string {
	switch val := v.(type) {
	case string:
		return []string{strings.TrimSpace(val)}
	case []any:
		result := make([]string, 0, len(val))
		for _, item := range val {
			if s, ok := item.(string); ok {
				s = strings.TrimSpace(s)
				if s != "" {
					result = append(result, s)
				}
			}
		}
		return result
	case []string:
		return val
	default:
		return nil
	}
}

// containsString 检查字符串切片是否包含指定值
func containsString(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}

// dedupStrings 字符串切片去重（已排序）
func dedupStrings(sorted []string) []string {
	if len(sorted) <= 1 {
		return sorted
	}
	result := []string{sorted[0]}
	for i := 1; i < len(sorted); i++ {
		if sorted[i] != sorted[i-1] {
			result = append(result, sorted[i])
		}
	}
	return result
}
