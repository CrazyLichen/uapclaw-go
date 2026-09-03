package security

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/resources"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// tieredInvocationContext 分层调用上下文。
//
// 对齐 Python: _TieredInvocationContext (tiered_policy.py L53-61)
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

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// mr 模块名前缀
	// 对齐 Python: _MR = "tiered_policy" (tiered_policy.py L49)
	mr = "tiered_policy"

	// approvalOverridesPrefix 审批覆盖前缀
	// 对齐 Python: _APPROVAL_OVERRIDES_PREFIX (tiered_policy.py L50)
	approvalOverridesPrefix = mr + ":approval_overrides"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// strictOrder 权限严格度排序（DENY < ASK < ALLOW）
// 对齐 Python: _STRICT_ORDER (tiered_policy.py L28)
var strictOrder = map[PermissionLevel]int{
	PermissionLevelDeny:  0,
	PermissionLevelAsk:   1,
	PermissionLevelAllow: 2,
}

// shellTools Shell 工具集合
// 对齐 Python: _SHELL_TOOLS (tiered_policy.py L31)
var shellTools = map[string]bool{
	"bash": true, "mcp_exec_command": true, "create_terminal": true,
}

// pathTools 路径工具集合
// 对齐 Python: _PATH_TOOLS (tiered_policy.py L32-38)
var pathTools = map[string]bool{
	"read_file": true, "write_file": true, "edit_file": true,
	"read_text_file": true, "write_text_file": true,
	"write": true, "read": true,
	"glob_file_search": true, "glob": true, "list_dir": true, "list_files": true,
	"grep": true, "search_replace": true,
}

// networkTools 网络工具集合
// 对齐 Python: _NETWORK_TOOLS (tiered_policy.py L39)
var networkTools = map[string]bool{
	"mcp_fetch_webpage": true, "mcp_free_search": true, "mcp_paid_search": true,
}

// pathArgKeys 路径参数键集合
// 对齐 Python: _PATH_ARG_KEYS (tiered_policy.py L41-44)
var pathArgKeys = map[string]bool{
	"path": true, "file_path": true, "target_file": true, "file": true,
	"old_path": true, "new_path": true, "source_path": true, "dest_path": true,
	"directory": true, "dir": true,
}

// tieredPolicyLogComponent 日志组件
var tieredPolicyLogComponent = logger.ComponentAgentCore

// builtinRulesCache 内置规则缓存
// 对齐 Python: _BUILTIN_RULES_CACHE (tiered_policy.py L47)
var builtinRulesCache []map[string]any

// builtinRulesCacheLoaded 是否已加载内置规则
var builtinRulesCacheLoaded bool

// ──────────────────────────── 导出函数 ────────────────────────────

// EvaluateTieredPolicy 返回 (最终权限, matched_rule 摘要)。
//
// 优先级链：
// 1. 整工具 deny 优先于参数级放行
// 2. Shell 工具: ParseShellForPermission → shellAstFloor
// 3. Shell "simple" 含子命令: 逐个 evaluateSingleInvocation → aggregateSubcommandResults
// 4. evaluateSingleInvocation: builtin-param-rules(DENY短路) → user-param-rules(DENY短路) → approval-overrides(ALLOW) → builtin-hits → user-hits → baseline → defaults → fallback-ASK
// 5. applyShellAstFloor
// 6. MaybeEscalateShellOperators
//
// 对齐 Python: evaluate_tiered_policy(permission_config, tool_name, tool_args) (tiered_policy.py L508-581)
func EvaluateTieredPolicy(permissionConfig map[string]any, toolName string, toolArgs map[string]any) (PermissionLevel, string) {
	mode := parsePermissionMode(permissionConfig)

	toolsCfg := mapAnyField(permissionConfig, "tools")
	defaultsCfg := mapAnyField(permissionConfig, "defaults")
	rules := listAnyField(permissionConfig, "rules")
	approvalOverrides := listAnyField(permissionConfig, "approval_overrides")

	bl, blRule := baselineLevel(toolsCfg, toolName)
	if bl == PermissionLevelDeny {
		return PermissionLevelDeny, blRule
	}

	// Shell AST 解析
	var shellParse *ShellAstParseResult
	if toolCategory(toolName) == "shell" {
		shellParse = ParseShellForPermission(commandText(toolArgs))
	}
	shellFloor, shellFloorRule := shellAstFloor(shellParse)

	builtinRules := getBuiltinSecurityRules()
	invCtx := &tieredInvocationContext{
		Mode:              mode,
		BuiltinRules:      builtinRules,
		Rules:             rules,
		ApprovalOverrides: approvalOverrides,
		BaselineLevel:     bl,
		BaselineRule:      blRule,
		DefaultsCfg:       defaultsCfg,
	}

	// Shell "simple" 含子命令：逐个评估
	if toolCategory(toolName) == "shell" && shellParse != nil && shellParse.Kind == ShellAstKindSimple {
		var subcommandResults []subcommandResult
		for _, subcmd := range shellParse.Subcommands {
			if subcmd.Text == "" {
				continue
			}
			subArgs := withShellCommand(toolArgs, subcmd.Text)
			result := evaluateSingleInvocation(toolName, subArgs, invCtx)
			subcommandResults = append(subcommandResults, result)
			if result.Permission == PermissionLevelDeny {
				break
			}
		}

		aggregated := aggregateSubcommandResults(subcommandResults)
		return applyShellAstFloor(aggregated.Permission, aggregated.MatchedRule, shellFloor, shellFloorRule)
	}

	// 非工具或非 simple Shell：直接评估单次调用
	result := evaluateSingleInvocation(toolName, toolArgs, invCtx)
	return applyShellAstFloor(result.Permission, result.MatchedRule, shellFloor, shellFloorRule)
}

// Strictest 返回最严格的权限级别。
//
// 对齐 Python: strictest(*levels) (tiered_policy.py L118-121)
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
// 对齐 Python: severity_to_decision(severity, permission_mode) (tiered_policy.py L124-138)
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
		logger.Warn(tieredPolicyLogComponent).
			Str("severity", severity).
			Msg("permission.tiered_policy.unknown_severity")
		return PermissionLevelAsk
	}
}

// MaybeEscalateShellOperators 命令含链式/注入元字符时 ALLOW→ASK。
// approval_overrides 豁免。
//
// 对齐 Python: maybe_escalate_shell_operators(tool_name, tool_args, permission) (tiered_policy.py L584-599)
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
// 对齐 Python: matched_rule_uses_approval_override(matched_rule) (tiered_policy.py L602-606)
func MatchedRuleUsesApprovalOverride(matchedRule string) bool {
	return strings.HasPrefix(matchedRule, approvalOverridesPrefix)
}

// RuleToolsCategoryConsistent 检查规则中 tools 是否同类。
//
// 对齐 Python: rule_tools_category_consistent(tools) (tiered_policy.py L151-160)
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

// GetBuiltinSecurityRules 获取内置安全规则列表（进程内缓存）。
//
// 对齐 Python: get_builtin_security_rules() (tiered_policy.py L88-110)
func GetBuiltinSecurityRules() []map[string]any {
	if builtinRulesCacheLoaded {
		return builtinRulesCache
	}

	parsed, err := resources.ParseBuiltinRules()
	if err != nil {
		logger.Warn(tieredPolicyLogComponent).
			Err(err).
			Msg("permission.tiered_policy.builtin_rules_missing")
		builtinRulesCacheLoaded = true
		return nil
	}

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

	builtinRulesCache = rules
	builtinRulesCacheLoaded = true
	return rules
}

// TieredPolicyRuleMatches 单条 rule 是否对本次调用匹配。
//
// 对齐 Python: tiered_policy_rule_matches(tool_name, pattern, tool_args, rule_tools) (tiered_policy.py L325-345)
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

// subcommandResult 子命令评估结果
type subcommandResult struct {
	Command     string
	Permission  PermissionLevel
	MatchedRule string
}

// getBuiltinSecurityRules 别名（内部用）
func getBuiltinSecurityRules() []map[string]any {
	return GetBuiltinSecurityRules()
}

// parsePermissionMode 从配置中解析权限模式
func parsePermissionMode(config map[string]any) string {
	mode := strings.ToLower(strings.TrimSpace(strVal(config["permission_mode"])))
	if mode != "normal" && mode != "strict" {
		mode = "normal"
	}
	return mode
}

// toolCategory 获取工具类别
// 对齐 Python: _tool_category(tool_name) (tiered_policy.py L141-148)
func toolCategory(toolName string) string {
	if shellTools[toolName] {
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
// 对齐 Python: _baseline_level(tools_cfg, tool_name) (tiered_policy.py L348-375)
func baselineLevel(toolsCfg map[string]any, toolName string) (PermissionLevel, string) {
	if toolsCfg == nil {
		return PermissionLevelAllow, "" // 无配置时不短路
	}
	raw, ok := toolsCfg[toolName]
	if !ok {
		return PermissionLevelAllow, "" // 无配置时不短路
	}

	switch v := raw.(type) {
	case string:
		level, err := ParsePermissionLevel(v)
		if err != nil {
			logger.Warn(tieredPolicyLogComponent).
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
			logger.Warn(tieredPolicyLogComponent).
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
		logger.Warn(tieredPolicyLogComponent).
			Str("tool", toolName).
			Msg("permission.tiered_policy.invalid_tool_baseline: reason=non_scalar_level")
		return PermissionLevelAllow, ""
	}
}

// parseLevel 解析权限级别字符串
// 对齐 Python: _parse_level(value) (tiered_policy.py L113-115)
func parseLevel(value string) PermissionLevel {
	level, err := ParsePermissionLevel(strings.TrimSpace(value))
	if err != nil {
		return PermissionLevelAsk
	}
	return level
}

// shellPatternMatches Shell 模式匹配
// 对齐 Python: _shell_pattern_matches(pattern, command) (tiered_policy.py L167-205)
func shellPatternMatches(pattern, command string) bool {
	if pattern == "" || command == "" {
		return false
	}
	p := strings.TrimSpace(pattern)
	if strings.HasPrefix(strings.ToLower(p), "re:") {
		expr := strings.TrimSpace(p[3:])
		norm := strings.ReplaceAll(command, `\`, "/")

		// 尝试主表达式
		re, err := regexp.Compile(expr)
		if err != nil {
			// 尝试分管道分割（如 YAML 双引号落盘后非法正则）
			if strings.Contains(expr, "|") {
				for _, part := range strings.Split(expr, "|") {
					part = strings.TrimSpace(part)
					if part == "" {
						continue
					}
					subRe, subErr := regexp.Compile(part)
					if subErr != nil {
						continue
					}
					if subRe.MatchString(command) || (norm != command && subRe.MatchString(norm)) {
						return true
					}
				}
			}
			logger.Warn(tieredPolicyLogComponent).
				Str("expr", expr).
				Msg("permission.tiered_policy.invalid_shell_regex")
			return false
		}

		if re.MatchString(command) {
			return true
		}
		if norm != command && re.MatchString(norm) {
			return true
		}
		return false
	}

	// 通配符
	if strings.ContainsAny(p, "*?[") {
		return MatchWildcard(command, p)
	}
	return command == p
}

// pathPatternMatches 路径模式匹配
// 对齐 Python: _path_pattern_matches(pattern, value) (tiered_policy.py L208-220)
func pathPatternMatches(pattern, value string) bool {
	if pattern == "" || value == "" {
		return false
	}
	p := strings.TrimSpace(pattern)
	if strings.HasPrefix(strings.ToLower(p), "re:") {
		expr := strings.TrimSpace(p[3:])
		re, err := regexp.Compile(expr)
		if err != nil {
			logger.Warn(tieredPolicyLogComponent).
				Str("expr", expr).
				Msg("permission.tiered_policy.invalid_path_regex")
			return false
		}
		return re.MatchString(strings.ReplaceAll(value, `\`, "/"))
	}
	return tieredPathMatcher.MatchPath(p, value)
}

// collectParamRuleHits 收集参数级规则命中列表。
// 对齐 Python: _collect_param_rule_hits(rules, tool_name, tool_args, mode, label_ns) (tiered_policy.py L242-284)
func collectParamRuleHits(rules []map[string]any, toolName string, toolArgs map[string]any, mode, labelNS string) []paramRuleHit {
	var hits []paramRuleHit
	for _, rule := range rules {
		rTools := ruleToolsListFromAny(rule["tools"])
		if !containsString(rTools, toolName) {
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
			logger.Warn(tieredPolicyLogComponent).
				Str("id", strVal(rule["id"])).
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
// 对齐 Python: _collect_approval_override_hits(rules, tool_name, tool_args) (tiered_policy.py L287-322)
func collectApprovalOverrideHits(rules []map[string]any, toolName string, toolArgs map[string]any) []string {
	var hits []string
	for _, rule := range rules {
		action := strings.ToLower(strings.TrimSpace(strVal(rule["action"])))
		if action != "allow" {
			continue
		}
		rTools := ruleToolsListFromAny(rule["tools"])
		if !containsString(rTools, toolName) {
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
			logger.Warn(tieredPolicyLogComponent).
				Str("id", strVal(rule["id"])).
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
// 对齐 Python: _evaluate_single_invocation(tool_name, tool_args, ctx) (tiered_policy.py L436-485)
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
	if ctx.BaselineLevel != PermissionLevelAllow || ctx.BaselineRule != "" {
		// 只要有 tools 配置（即使为 allow），就用基线
		if ctx.BaselineRule != "" {
			return subcommandResult{Permission: ctx.BaselineLevel, MatchedRule: ctx.BaselineRule}
		}
	}

	// 7. 默认配置
	if ctx.DefaultsCfg != nil {
		if defaultVal, ok := ctx.DefaultsCfg["*"].(string); ok {
			dl := parseLevel(defaultVal)
			return subcommandResult{Permission: dl, MatchedRule: mr + ":defaults.*"}
		}
	}

	// 8. Fallback
	return subcommandResult{Permission: PermissionLevelAsk, MatchedRule: mr + ":fallback(no_config)"}
}

// shellAstFloor Shell AST 地板权限。
// 对齐 Python: _shell_ast_floor(shell_parse) (tiered_policy.py L388-408)
func shellAstFloor(shellParse *ShellAstParseResult) (PermissionLevel, string) {
	if shellParse == nil {
		return PermissionLevelAllow, "" // nil 表示无 floor
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

	// 特定结构升级
	if flags.InputRedirection || flags.OutputRedirection ||
		flags.CommandSubstitution || flags.ProcessSubstitution ||
		flags.Heredoc {
		return PermissionLevelAsk, mr + ":shell_ast:structure_guard"
	}

	return PermissionLevelAllow, "" // 无 floor
}

// applyShellAstFloor 应用 Shell AST 地板权限。
// 对齐 Python: _apply_shell_ast_floor(permission, matched_rule, shell_floor, shell_floor_rule) (tiered_policy.py L411-424)
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
// 对齐 Python: _aggregate_subcommand_results(results) (tiered_policy.py L488-505)
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

// paramRuleHit 参数规则命中
type paramRuleHit struct {
	Level PermissionLevel
	Label string
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
// 对齐 Python: _finalize_hits(hits, prefix) (tiered_policy.py L378-385)
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
// 对齐 Python: _with_shell_command(tool_args, command) (tiered_policy.py L427-433)
func withShellCommand(toolArgs map[string]any, command string) map[string]any {
	subArgs := make(map[string]any, len(toolArgs))
	for k, v := range toolArgs {
		subArgs[k] = v
	}
	subArgs["command"] = command
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
