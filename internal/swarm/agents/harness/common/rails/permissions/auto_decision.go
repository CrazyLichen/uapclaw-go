// 自动权限审查层的确定性判定助手。
//
// 对应 Python 代码：permissions/auto_decision.py（639 行 / 8 导出 + 6 私有）
//
// 职责：在规则层说 ASK 之后、LLM 审查之前，用确定性逻辑挡掉一批明显安全
// 或明显危险的调用——
//   - 硬拒绝（search_skill 敏感查询、非 https scheme、URI 硬阻断）
//   - 领域固定路由（mcp / browser / cron / memory / skill 各自的 ASK 原因）
//   - 封闭内部动作的 ALLOW（internal_readonly / session_status / memory_read 等）
//   - 低风险路径读的 ALLOW
//   - 事后兜底：高弹性/高副作用动作不允许变成最终 ALLOW
//
// 结构差异（9.90b 已定，本文件沿用）：Python 的 facts.tool_name / facts.tool_category
// 在 Go 侧位于 facts.Capability.ToolName / facts.Capability.Category。

package permissions

import (
	"math"
	"regexp"
	"strings"
	"unicode/utf8"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BrowserRuntimeSecurityProfile 浏览器运行时的安全档位（最小面）。
//
// Python: browser_runtime_security_profile（Any | None，只读 network_guard_enforced）
type BrowserRuntimeSecurityProfile struct {
	// NetworkGuardEnforced 浏览器运行时是否强制执行网络护栏
	NetworkGuardEnforced bool
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// AutoDecisionAllowLevel allow 档位
	AutoDecisionAllowLevel = "allow"
	// AutoDecisionAskLevel ask 档位
	AutoDecisionAskLevel = "ask"
	// AutoDecisionDenyLevel deny 档位
	AutoDecisionDenyLevel = "deny"
	// SearchSkillTool 搜索技能工具名
	SearchSkillTool = "search_skill"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// searchSkillSensitiveQueryPattern search_skill 查询里的密钥特征。
	//
	// Python: SEARCH_SKILL_SENSITIVE_QUERY_PATTERN
	searchSkillSensitiveQueryPattern = regexp.MustCompile(
		`(?i)(?:api[_-]?key|private[_-]?key|access[_-]?token|secret[_-]?key|password|token)\b(?:\s*[:=]\s*|\s+)\S+`)

	// egressSecretKeyPattern 出网载荷里的密钥型键名。
	//
	// Python: EGRESS_SECRET_KEY_PATTERN
	egressSecretKeyPattern = regexp.MustCompile(
		`(?i)(?:^|[._-])(?:access[_-]?token|api[_-]?key|authorization|` +
			`credentials?|password|private[_-]?key|secret|token)(?:$|[._-])`)

	// egressSecretValuePattern 出网载荷里的密钥型字面量。
	//
	// Python: EGRESS_SECRET_VALUE_PATTERN
	egressSecretValuePattern = regexp.MustCompile(
		`(?i)(?:AKIA[A-Z0-9]{12,}|sk-[A-Za-z0-9_-]{8,}|` +
			`gh[pousr]_[A-Za-z0-9_]{8,}|(?:access[_-]?token|api[_-]?key|` +
			`authorization|bearer|credentials?|password|private[_-]?key|secret|token)` +
			`\s*[:=]\s*\S+)`)

	// genericMCPUploadKeys 通用 MCP 上传字段名。
	//
	// Python: GENERIC_MCP_UPLOAD_KEYS
	genericMCPUploadKeys = map[string]struct{}{
		"abs_file_path":      {},
		"abs_file_path_list": {},
		"abs_file_paths":     {},
		"attachment":         {},
		"attachments":        {},
		"file":               {},
		"file_path":          {},
		"file_path_list":     {},
		"file_paths":         {},
		"files":              {},
	}

	// highEffectSideEffects 高影响副作用集合。
	//
	// Python: HIGH_EFFECT_SIDE_EFFECTS
	highEffectSideEffects = map[string]struct{}{
		"browser_action": {},
		"delegation":     {},
		"device_action":  {},
		"dynamic_exec":   {},
		"external_send":  {},
		"memory_write":   {},
		"schedule":       {},
		"skill_change":   {},
	}

	// recallMemoryIDPattern recall_compressed_context 的 memory_id 白名单形态。
	recallMemoryIDPattern = regexp.MustCompile(`^[A-Za-z0-9_.:-]+$`)

	// memoryPathPattern memory_read 允许的日期文件名形态。
	memoryPathPattern = regexp.MustCompile(`^memory/\d{4}-\d{2}-\d{2}\.md$`)

	// browserRecallHandlePattern browser_session_recall 的句柄形态（32 位十六进制）。
	browserRecallHandlePattern = regexp.MustCompile(`^[0-9a-fA-F]{32}$`)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// DeterministicGuardRoute 对不可覆盖的发现返回硬拒绝路由。
//
// Python: deterministic_guard_route(facts)
func DeterministicGuardRoute(facts ToolDecisionFacts) *DecisionRoute {
	if facts.Capability.ToolName == SearchSkillTool {
		return evaluateSearchSkillEgressGuard(facts)
	}
	return nil
}

// GenericMCPEgressEvidence 返回通用 MCP 审查器证据（已脱敏的风险标签）。
//
// Python: generic_mcp_egress_evidence(facts)
// 结果按首次出现顺序去重。
func GenericMCPEgressEvidence(facts ToolDecisionFacts) []string {
	network := InspectNetworkScope(facts)
	evidence := []string{}
	if network.UploadLike ||
		hasSideEffect(facts.Capability.StaticSideEffects, "external_send") ||
		hasGenericMCPUploadField(facts.UntrustedArgs) {
		evidence = append(evidence, "generic_mcp_upload_like_payload")
	}
	for _, uri := range network.AbsoluteURIs {
		switch {
		case AbsoluteURIHardBlockReason(uri) != "":
			evidence = append(evidence, AbsoluteURIHardBlockReason(uri))
		case uri.ParseFailureReason != "":
			evidence = append(evidence, uri.ParseFailureReason)
		case uri.FileResolutionReason != "":
			evidence = append(evidence, uri.FileResolutionReason)
		case uri.Scheme == "http":
			evidence = append(evidence, "network_scheme_not_https")
		case uri.Scheme == "file":
			evidence = append(evidence, "generic_mcp_file_uri")
		case uri.Scheme == "ftp":
			evidence = append(evidence, "generic_mcp_uri_provider_support_unproven")
		case uri.Scheme != "https":
			evidence = append(evidence, "generic_mcp_uri_scheme_unsupported")
		}
	}
	if hasSecretLikeEgressPayload(facts.UntrustedArgs) {
		evidence = append(evidence, "generic_mcp_sensitive_egress")
	}
	return dedupeStrings(evidence)
}

// DeterministicDomainRoute 返回仓库内固定的领域路由（无插件注册表）。
//
// Python: deterministic_domain_route(facts, *, original_user_intent,
// recent_url_sources=(), browser_runtime_security_profile=None)
func DeterministicDomainRoute(
	facts ToolDecisionFacts,
	originalUserIntent *OriginalUserIntentEvidence,
	recentURLSources []RecentURLSource,
	browserProfile *BrowserRuntimeSecurityProfile,
) *DecisionRoute {
	category := facts.Capability.Category
	operation := domainOperation(facts)
	network := InspectNetworkScope(facts)

	switch category {
	case "mcp":
		return &DecisionRoute{Level: AutoDecisionAskLevel, Reason: "domain_policy_generic_mcp_manual", Source: "manual_only"}

	case "browser":
		for _, uri := range network.AbsoluteURIs {
			if reason := AbsoluteURIHardBlockReason(uri); reason != "" {
				return &DecisionRoute{Level: AutoDecisionDenyLevel, Reason: reason, Source: "hard_guard"}
			}
		}
		if containsString(network.Schemes, "http") {
			return &DecisionRoute{Level: AutoDecisionDenyLevel, Reason: "network_scheme_not_https", Source: "hard_guard"}
		}
		if anyURIParseFailure(network.AbsoluteURIs) {
			return &DecisionRoute{Level: AutoDecisionAskLevel, Reason: "browser_uri_parse_failure", Source: "manual_only"}
		}
		if anyURIFileResolution(network.AbsoluteURIs) {
			reason := "browser_file_uri_unresolved"
			for _, uri := range network.AbsoluteURIs {
				if uri.FileResolutionReason == "file_uri_nonlocal_host" {
					reason = "browser_file_uri_nonlocal"
					break
				}
			}
			return &DecisionRoute{Level: AutoDecisionAskLevel, Reason: reason, Source: "manual_only"}
		}
		if containsString(network.Schemes, "ftp") {
			return &DecisionRoute{Level: AutoDecisionAskLevel, Reason: "browser_uri_provider_support_unproven", Source: "manual_only"}
		}
		for _, scheme := range network.Schemes {
			switch scheme {
			case "file", "ftp", "http", "https":
			default:
				return &DecisionRoute{Level: AutoDecisionAskLevel, Reason: "browser_uri_scheme_unsupported", Source: "manual_only"}
			}
		}
		if len(network.AbsoluteURIs) > 1 {
			return &DecisionRoute{Level: AutoDecisionAskLevel, Reason: "browser_uri_target_ambiguous", Source: "manual_only"}
		}
		browserReasons := map[string]string{
			"credential_form": "domain_policy_browser_credential_form",
			"upload":          "domain_policy_browser_file_transfer",
			"external_submit": "domain_policy_browser_external_submit",
			"control_cancel":  "domain_policy_browser_cancel_control_not_model_authorized",
			"route_mutation":  "domain_policy_browser_route_mutation_not_model_authorized",
			"inspect":         "domain_policy_browser_readonly",
			"click":           "domain_policy_browser_interactive",
			"type":            "domain_policy_browser_interactive",
			"download":        "domain_policy_browser_interactive",
		}
		if operation == "navigate" {
			if len(network.AbsoluteURIs) > 0 && network.AbsoluteURIs[0].Scheme == "file" {
				return &DecisionRoute{Level: AutoDecisionAskLevel, Reason: "domain_policy_browser_local_file_uri", Source: "semantic_reviewer"}
			}
			urlRoute := EvaluateReviewableURLScope(facts, originalUserIntent, recentURLSources)
			if urlRoute.HardBlock {
				return &DecisionRoute{Level: AutoDecisionDenyLevel, Reason: urlRoute.Reason, Source: "hard_guard"}
			}
			if !urlRoute.Accepted {
				return &DecisionRoute{Level: AutoDecisionAskLevel, Reason: urlRoute.Reason, Source: "manual_only"}
			}
			guardVerified := browserProfile != nil && browserProfile.NetworkGuardEnforced
			if urlRoute.SemanticReviewRequired && !guardVerified {
				return &DecisionRoute{Level: AutoDecisionAskLevel, Reason: "browser_network_guard_unverified", Source: "manual_only"}
			}
			return &DecisionRoute{
				Level:  AutoDecisionAskLevel,
				Reason: "domain_policy_browser_navigation_url_reviewable",
				Source: "semantic_reviewer",
			}
		}
		if reason, ok := browserReasons[operation]; ok {
			return &DecisionRoute{Level: AutoDecisionAskLevel, Reason: reason, Source: "semantic_reviewer"}
		}
		return &DecisionRoute{Level: AutoDecisionAskLevel, Reason: "domain_policy_browser_unknown_payload", Source: "manual_only"}

	case "cron":
		cronReasons := map[string]string{
			"preview": "domain_policy_cron_preview",
			"create":  "domain_policy_cron_mutation_manual",
			"update":  "domain_policy_cron_mutation_manual",
			"run_now": "domain_policy_cron_mutation_manual",
			"delete":  "domain_policy_cron_destructive",
			"toggle":  "domain_policy_cron_destructive",
		}
		if reason, ok := cronReasons[operation]; ok {
			return &DecisionRoute{Level: AutoDecisionAskLevel, Reason: reason, Source: "semantic_reviewer"}
		}
		return &DecisionRoute{Level: AutoDecisionAskLevel, Reason: "domain_policy_cron_unknown_payload", Source: "manual_only"}

	case "memory":
		if facts.Capability.OperationFamily != "memory_write" {
			return nil
		}
		namespace := textArg(facts.UntrustedArgs, "namespace", "space", "profile")
		scope := textArg(facts.UntrustedArgs, "scope", "visibility", "target_scope")
		retention := textArg(facts.UntrustedArgs, "retention", "ttl", "expires_in", "expires")
		missing := ""
		switch {
		case namespace == "":
			missing = "domain_policy_memory_namespace_missing"
		case scope == "":
			missing = "domain_policy_memory_scope_missing"
		case retention == "":
			missing = "domain_policy_memory_retention_missing"
		}
		reason := missing
		source := "manual_only"
		if reason == "" {
			reason = "domain_policy_memory_bounded_write"
			source = "semantic_reviewer"
		}
		return &DecisionRoute{Level: AutoDecisionAskLevel, Reason: reason, Source: source}

	case "skill":
		switch operation {
		case "read", "search", "list":
			return &DecisionRoute{Level: AutoDecisionAskLevel, Reason: "domain_policy_skill_readonly", Source: "semantic_reviewer"}
		case "install", "update", "uninstall":
			return &DecisionRoute{Level: AutoDecisionAskLevel, Reason: "domain_policy_skill_change", Source: "semantic_reviewer"}
		default:
			return &DecisionRoute{Level: AutoDecisionAskLevel, Reason: "domain_policy_skill_unknown_payload", Source: "manual_only"}
		}
	}
	return nil
}

// ToolSearchArgsValid 校验工具搜索参数（SDK 搜索与目录契约分开）。
//
// Python: tool_search_args_valid(args, *, progressive)
func ToolSearchArgsValid(args map[string]any, progressive bool) bool {
	if args == nil {
		return false
	}
	if !progressive {
		return catalogueArgsValid("tool_search", args)
	}
	limit, hasLimit := args["limit"]
	if !hasLimit {
		limit = nil
	}
	return keysSubsetOf(args, "query", "limit") &&
		boundedText(args["query"], 4096, true) &&
		(limit == nil || (isExactInt(limit) && limit.(int) >= 1 && limit.(int) <= 20))
}

// SessionObservationArgsValid 校验五个封闭会话观察契约，其余返回 not-ok。
//
// Python: session_observation_args_valid(name, args) -> bool | None
func SessionObservationArgsValid(name string, args map[string]any) (bool, bool) {
	switch name {
	case "tool_search":
		return ToolSearchArgsValid(args, true) || ToolSearchArgsValid(args, false), true
	case "skill_index":
		return catalogueArgsValid(name, args), true
	case "list_skill":
		query := any("")
		if raw, exists := args["query"]; exists {
			query = raw
		}
		return keysSubsetOf(args, "query") && boundedText(query, 4096, true), true
	case "get_current_goal":
		return len(args) == 0, true
	case "recall_compressed_context":
		memoryID, _ := args["memory_id"].(string)
		return keysSubsetOf(args, "query", "memory_id") &&
			boundedText(args["query"], 4096, false) &&
			boundedText(memoryID, 128, true) &&
			(memoryID == "" || recallMemoryIDPattern.MatchString(memoryID)) &&
			memoryID != "." && memoryID != "..", true
	default:
		return false, false
	}
}

// TerminalLowRiskRoute 返回封闭的低风险路径读 ALLOW 路由（在基础 ASK 之前）。
//
// Python: terminal_low_risk_route(facts)
func TerminalLowRiskRoute(facts ToolDecisionFacts) *DecisionRoute {
	if DeterministicGuardRoute(facts) != nil {
		return nil
	}
	if len(facts.ExternalPaths) > 0 || len(facts.WritePaths) > 0 || !facts.AccessesKnown {
		return nil
	}
	if facts.Capability.ToolName == "lsp" {
		return nil
	}
	if facts.Capability.Category == "path" {
		if facts.Capability.RiskTier == "low" && len(facts.Capability.StaticSideEffects) == 0 {
			return &DecisionRoute{Level: AutoDecisionAllowLevel, Reason: "terminal_low_risk_allow", Source: "hard_guard"}
		}
	}
	return nil
}

// TerminalInternalRoute 返回封闭的 Host 规范动作 ALLOW 路由。
//
// Python: terminal_internal_route(facts, *, subagent_runtime_control_verified=False,
// readonly_binding_verified=False)
func TerminalInternalRoute(
	facts ToolDecisionFacts,
	subagentRuntimeControlVerified bool,
	readonlyBindingVerified bool,
) *DecisionRoute {
	observationArgsValid, hasObservation := false, false
	if facts.Capability.OperationFamily == "internal_readonly" {
		observationArgsValid, hasObservation = SessionObservationArgsValid(
			facts.Capability.ToolName, facts.UntrustedArgs)
	}
	if hasObservation && !observationArgsValid {
		return nil
	}
	network := internalNetworkScope(facts)
	if facts.Capability.OperationFamily == "internal_readonly" && !readonlyBindingVerified {
		return nil
	}
	if facts.Capability.FactsSource != "host_static" ||
		facts.Capability.AliasConflict ||
		!facts.ArgumentsValidObject {
		return nil
	}
	if facts.Capability.OperationFamily == "subagent_runtime_control" && !subagentRuntimeControlVerified {
		return nil
	}
	if !hasObservation && !closedInternalArgsValid(facts, network) {
		return nil
	}
	if facts.Capability.RiskTier != "low" || facts.Capability.HighFlex {
		return nil
	}
	if len(facts.ExternalPaths) > 0 || len(facts.WritePaths) > 0 || facts.Command != "" {
		return nil
	}
	if len(network.AbsoluteURIs) > 0 {
		return nil
	}
	allowedEffects := map[string][]string{
		"internal_readonly":        {},
		"browser_session_observe":  {"session_cache_write"},
		"browser_session_recall":   {},
		"memory_read":              {},
		"memory_search":            {},
		"session_status":           {},
		"skill_read":               {},
		"subagent_runtime_control": {"delegation"},
		"task_state":               {},
	}
	expectedEffects, ok := allowedEffects[facts.Capability.OperationFamily]
	if !ok {
		return nil
	}
	if !sameEffectSet(facts.Capability.StaticSideEffects, expectedEffects) {
		return nil
	}
	return &DecisionRoute{Level: AutoDecisionAllowLevel, Reason: "canonical_internal_action_allow", Source: "internal"}
}

// PostPolicyRoute 阻止高弹性/高影响动作变成最终 ALLOW。
//
// Python: post_policy_route(facts, *, policy_level)
func PostPolicyRoute(facts ToolDecisionFacts, policyLevel string) *DecisionRoute {
	if policyLevel != AutoDecisionAllowLevel {
		return nil
	}
	for effect := range highEffectSideEffects {
		if _, exists := facts.Capability.StaticSideEffects[effect]; exists {
			return &DecisionRoute{Level: AutoDecisionAskLevel, Reason: "high_effect_tool", Source: "post_policy"}
		}
	}
	if facts.Capability.HighFlex {
		return &DecisionRoute{Level: AutoDecisionAskLevel, Reason: "high_flex_tool", Source: "post_policy"}
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// hasGenericMCPUploadField 递归判断参数里是否有非空的上传字段。
//
// Python: _has_generic_mcp_upload_field(value)
func hasGenericMCPUploadField(value any) bool {
	if mapping, ok := value.(map[string]any); ok {
		for key, nested := range mapping {
			if _, isUploadKey := genericMCPUploadKeys[strings.ToLower(strings.TrimSpace(key))]; isUploadKey {
				if hasTruthyEgressValue(nested) {
					return true
				}
			}
			if hasGenericMCPUploadField(nested) {
				return true
			}
		}
		return false
	}
	if list, ok := value.([]any); ok {
		for _, item := range list {
			if hasGenericMCPUploadField(item) {
				return true
			}
		}
	}
	return false
}

// hasSecretLikeEgressPayload 递归判断载荷里是否有密钥型键或字面量。
//
// Python: _has_secret_like_egress_payload(value)
func hasSecretLikeEgressPayload(value any) bool {
	if mapping, ok := value.(map[string]any); ok {
		for key, nested := range mapping {
			if egressSecretKeyPattern.MatchString(key) && hasTruthyEgressValue(nested) {
				return true
			}
			if hasSecretLikeEgressPayload(nested) {
				return true
			}
		}
		return false
	}
	if text, ok := value.(string); ok {
		return egressSecretValuePattern.MatchString(text)
	}
	if list, ok := value.([]any); ok {
		for _, item := range list {
			if hasSecretLikeEgressPayload(item) {
				return true
			}
		}
	}
	return false
}

// hasTruthyEgressValue 判断出网值是否「有内容」。
//
// Python: _has_truthy_egress_value(value)
func hasTruthyEgressValue(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case bool:
		return typed
	case string:
		return strings.TrimSpace(typed) != ""
	case []any:
		return len(typed) > 0
	case map[string]any:
		return len(typed) > 0
	default:
		return true
	}
}

// evaluateSearchSkillEgressGuard search_skill 查询的密钥特征硬拒绝。
//
// Python: _evaluate_search_skill_egress_guard(facts)
func evaluateSearchSkillEgressGuard(facts ToolDecisionFacts) *DecisionRoute {
	query, ok := facts.UntrustedArgs["query"].(string)
	if ok && searchSkillSensitiveQueryPattern.MatchString(query) {
		return &DecisionRoute{
			Level:  AutoDecisionDenyLevel,
			Reason: "search_skill_sensitive_query",
			Source: "deterministic_guard",
		}
	}
	return nil
}

// domainOperation 只对固定路由使用的工具/操作名做分类。
//
// Python: _domain_operation(facts)
func domainOperation(facts ToolDecisionFacts) string {
	raw := textArg(facts.UntrustedArgs, "operation", "action", "mode", "op")
	operation := strings.ToLower(strings.ReplaceAll(raw, "-", "_"))
	if raw == "" {
		operation = strings.ToLower(strings.ReplaceAll(facts.Capability.ToolName, "-", "_"))
	}
	category := facts.Capability.Category

	switch category {
	case "browser":
		toolName := facts.Capability.ToolName
		if toolName == "browser_page_action" && containsString([]string{
			"read_text", "find", "snapshot", "tabs", "wait"}, operation) {
			return "inspect"
		}
		if toolName == "browser_page_action" && operation == "scroll" {
			return "click"
		}
		if strings.Contains(operation, "cancel") {
			return "control_cancel"
		}
		if strings.Contains(operation, "route") || strings.Contains(operation, "unroute") {
			return "route_mutation"
		}
		if strings.Contains(operation, "upload") {
			return "upload"
		}
		if strings.Contains(operation, "download") {
			return "download"
		}
		for _, token := range []string{"snapshot", "screenshot", "inspect", "probe", "recall", "health", "wait_for"} {
			if strings.Contains(operation, token) {
				return "inspect"
			}
		}
		for _, token := range []string{"navigate", "open", "goto"} {
			if strings.Contains(operation, token) {
				return "navigate"
			}
		}
		for _, token := range []string{"click", "press", "hover", "select"} {
			if strings.Contains(operation, token) {
				return "click"
			}
		}
		for _, token := range []string{"type", "fill", "input"} {
			if strings.Contains(operation, token) {
				return "type"
			}
		}
		if containsString([]string{"post", "submit", "external_submit"}, operation) {
			return "external_submit"
		}
		return "unknown"

	case "cron":
		for _, pair := range [][2]string{
			{"preview", "preview"},
			{"delete", "delete"},
			{"remove", "delete"},
			{"toggle", "toggle"},
			{"enable", "toggle"},
			{"disable", "toggle"},
			{"update", "update"},
			{"edit", "update"},
			{"create", "create"},
			{"add", "create"},
			{"run_now", "run_now"},
		} {
			if strings.Contains(operation, pair[0]) {
				return pair[1]
			}
		}
		return "unknown"

	case "memory":
		family := facts.Capability.OperationFamily
		if family == "memory_read" {
			return "read"
		}
		if family == "memory_search" {
			return "search"
		}
		if strings.Contains(operation, "edit") || strings.Contains(operation, "update") {
			return "edit"
		}
		for _, token := range []string{"write", "remember", "add", "conclude"} {
			if strings.Contains(operation, token) {
				return "write"
			}
		}
		return "unknown"

	case "skill":
		family := facts.Capability.OperationFamily
		if family == "skill_read" {
			return "read"
		}
		if family == "skill_discovery" {
			return "search"
		}
		for _, token := range []string{"uninstall", "install", "update", "search", "read", "list"} {
			if strings.Contains(operation, token) {
				return token
			}
		}
		return "unknown"
	}
	return facts.Capability.OperationFamily
}

// textArg 按顺序取第一个非空字符串参数。
//
// Python: _text_arg(args, *keys)
func textArg(args map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := args[key].(string); ok {
			if trimmed := strings.TrimSpace(value); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

// boundedText 是否为有界且无 NUL 的字符串。
//
// Python: _bounded_text(value, limit, *, empty=False)
// 长度按字符数判定（utf8.RuneCountInString），与 Python 的 len() 一致。
func boundedText(value any, limit int, allowEmpty bool) bool {
	text, ok := value.(string)
	if !ok {
		return false
	}
	// Python 的 len() 是字符数，Go 的 len() 是字节数——用 rune 数对齐
	if utf8.RuneCountInString(text) > limit {
		return false
	}
	if strings.ContainsRune(text, 0) {
		return false
	}
	return allowEmpty || strings.TrimSpace(text) != ""
}

// catalogueArgsValid 校验目录类发现工具的封闭参数契约。
//
// Python: _catalogue_args_valid(name, args)
func catalogueArgsValid(name string, args map[string]any) bool {
	if name == "tool_search" {
		if !keysSubsetOf(args, "operation", "category", "tool", "tools", "query", "cursor", "call_goal") {
			return false
		}
		operation := ""
		if raw, ok := args["operation"].(string); ok {
			operation = raw
		}
		if !boundedText(operation, 32, true) {
			return false
		}
		lowered := strings.ToLower(strings.TrimSpace(operation))
		switch lowered {
		case "", "list", "search", "get_schema":
		default:
			return false
		}
		for _, pair := range [][2]any{
			{"category", 4096}, {"tool", 128}, {"cursor", 128}, {"call_goal", 4096},
		} {
			key := pair[0].(string)
			if _, exists := args[key]; exists && !boundedText(args[key], pair[1].(int), true) {
				return false
			}
		}
		tools, hasTools := args["tools"]
		if !hasTools {
			tools = []any{}
		}
		toolList, ok := tools.([]any)
		if !ok || len(toolList) > 8 {
			return false
		}
		for _, item := range toolList {
			if !boundedText(item, 128, false) {
				return false
			}
		}
		query := any("")
		if raw, exists := args["query"]; exists {
			query = raw
		}
		if !boundedText(query, 4096, true) && !boundedTextList(query, 512) {
			return false
		}
		if lowered != "get_schema" {
			return true
		}
		toolName := ""
		if raw, ok := args["tool"].(string); ok {
			toolName = strings.TrimSpace(raw)
		}
		return toolName != "" || len(toolList) > 0
	}

	if !keysSubsetOf(args, "operation", "category", "query", "cursor") {
		return false
	}
	operation, _ := args["operation"].(string)
	if operation != "list" && operation != "search" {
		return false
	}
	for _, pair := range [][2]any{{"category", 4096}, {"cursor", 128}} {
		key := pair[0].(string)
		if _, exists := args[key]; exists && !boundedText(args[key], pair[1].(int), false) {
			return false
		}
	}
	query := any([]any{})
	if raw, exists := args["query"]; exists {
		query = raw
	}
	if !boundedText(query, 512, false) && !boundedTextList(query, 512) {
		return false
	}
	if operation == "list" {
		return true
	}
	if _, exists := args["cursor"]; exists {
		return true
	}
	queryText, _ := query.(string)
	return strings.TrimSpace(queryText) != ""
}

// boundedTextList 是否为长度≤8 且每项有界的字符串列表。
func boundedTextList(value any, limit int) bool {
	list, ok := value.([]any)
	if !ok || len(list) > 8 {
		return false
	}
	for _, item := range list {
		if !boundedText(item, limit, false) {
			return false
		}
	}
	return true
}

// closedInternalArgsValid 校验封闭内部动作的参数契约。
//
// Python: _closed_internal_args_valid(facts, network)
func closedInternalArgsValid(facts ToolDecisionFacts, network NetworkScopeFacts) bool {
	family := facts.Capability.OperationFamily
	args := facts.UntrustedArgs

	switch family {
	case "internal_readonly":
		name := facts.Capability.ToolName
		if name == "cron_list_jobs" {
			return len(args) == 0
		}
		if name == "heartbeat_list_jobs" {
			if !keysSubsetOf(args, "scope") {
				return false
			}
			scope, exists := args["scope"]
			if !exists || scope == nil {
				return true
			}
			text, _ := scope.(string)
			return text == "current"
		}
		if name == "convert_timestamp_to_utc8_time" {
			if len(args) != 1 {
				return false
			}
			value, exists := args["timestamp"]
			if !exists {
				return false
			}
			switch typed := value.(type) {
			case int:
				return true
			case float64:
				return !math.IsInf(typed, 0) && !math.IsNaN(typed)
			default:
				return false
			}
		}
		field := "terminal_id"
		if strings.HasPrefix(name, "cron_") || strings.HasPrefix(name, "heartbeat_") {
			field = "job_id"
		}
		preview := strings.HasSuffix(name, "preview_job")
		allowedKeys := []string{field}
		if preview {
			allowedKeys = append(allowedKeys, "count")
		}
		if !keysSubsetOf(args, allowedKeys...) {
			return false
		}
		fieldValue, ok := args[field].(string)
		if !ok || strings.TrimSpace(fieldValue) == "" {
			return false
		}
		if count, hasCount := args["count"]; hasCount && !isExactInt(count) {
			return false
		}
		return true

	case "session_status":
		schemas := map[string][]string{
			"session_list":         {"query", "limit", "offset"},
			"session_message_list": {"target_session_id", "limit", "offset"},
			"session_read":         {"target_session_id", "cursor", "limit", "max_output_chars"},
		}
		allowed, ok := schemas[facts.Capability.ToolName]
		if !ok || !keysSubsetOf(args, allowed...) {
			return false
		}
		if facts.Capability.ToolName == "session_read" {
			if _, exists := args["target_session_id"]; !exists {
				return false
			}
		}
		for key, value := range args {
			switch key {
			case "limit", "offset", "max_output_chars":
				if !isExactInt(value) {
					return false
				}
				number := value.(int)
				minValue := 1
				if key == "offset" {
					minValue = 0
				}
				if number < minValue {
					return false
				}
			default:
				text, isString := value.(string)
				if !isString {
					return false
				}
				if key != "query" && strings.TrimSpace(text) == "" {
					return false
				}
			}
		}
		return true

	case "skill_read":
		name, _ := args["skill_name"].(string)
		path, hasPath := args["relative_file_path"]
		if !hasPath {
			path = nil
		}
		if strings.TrimSpace(name) == "" || strings.Contains(name, "/") || strings.Contains(name, `\`) {
			return false
		}
		if path == nil {
			return true
		}
		text, isString := path.(string)
		return isString && (text == "" || text == "SKILL.md")

	case "memory_read":
		path, ok := args["path"].(string)
		if !ok {
			return false
		}
		if path == "memory/USER.md" || path == "memory/MEMORY.md" {
			return true
		}
		return memoryPathPattern.MatchString(path)

	case "browser_session_observe", "browser_session_recall":
		if domainOperation(facts) != "inspect" || len(network.AbsoluteURIs) > 0 {
			return false
		}
		if family == "browser_session_recall" {
			handle, ok := args["handle"].(string)
			return ok && browserRecallHandlePattern.MatchString(handle)
		}
	}
	return true
}

// internalNetworkScope 在校验封闭观察参数后再检查 URI。
//
// Python: _internal_network_scope(facts)
// 这些执行器把 query 当作本地文本（即使内容是 URL 或 shell 语法），
// 因此要先剔除这些不透明字段再扫描 URI。
func internalNetworkScope(facts ToolDecisionFacts) NetworkScopeFacts {
	var opaqueFields []string
	switch facts.Capability.OperationFamily {
	case "internal_readonly":
		table := map[string][]string{
			"tool_search":               {"query", "call_goal"},
			"skill_index":               {"query"},
			"list_skill":                {"query"},
			"recall_compressed_context": {"query"},
		}
		opaqueFields = table[facts.Capability.ToolName]
	case "subagent_runtime_control":
		table := map[string]string{
			"subagent_spawn":      "task_description",
			"subagent_send_input": "query",
		}
		if field, ok := table[facts.Capability.ToolName]; ok {
			opaqueFields = []string{field}
		}
	}
	if len(opaqueFields) == 0 {
		return InspectNetworkScope(facts)
	}
	filtered := make(map[string]any, len(facts.UntrustedArgs))
	for key, value := range facts.UntrustedArgs {
		skip := false
		for _, field := range opaqueFields {
			if key == field {
				skip = true
				break
			}
		}
		if !skip {
			filtered[key] = value
		}
	}
	filteredFacts := facts
	filteredFacts.UntrustedArgs = filtered
	return InspectNetworkScope(filteredFacts)
}

// hasSideEffect 副作用集合是否含指定项。
func hasSideEffect(effects map[string]struct{}, name string) bool {
	if effects == nil {
		return false
	}
	_, exists := effects[name]
	return exists
}

// sameEffectSet 副作用集合是否与期望列表完全一致。
func sameEffectSet(effects map[string]struct{}, expected []string) bool {
	if len(effects) != len(expected) {
		return false
	}
	for _, name := range expected {
		if _, exists := effects[name]; !exists {
			return false
		}
	}
	return true
}

// keysSubsetOf map 的键是否全部落在允许集内。
func keysSubsetOf(args map[string]any, allowed ...string) bool {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		allowedSet[key] = struct{}{}
	}
	for key := range args {
		if _, ok := allowedSet[key]; !ok {
			return false
		}
	}
	return true
}

// isExactInt 是否为精确 int（排除 bool 与其他数值类型）。
//
// Python 用 `type(value) is int` 排除 bool 子类；Go 的 any 断言天然区分。
func isExactInt(value any) bool {
	_, ok := value.(int)
	return ok
}

// anyURIParseFailure 是否有 URI 解析失败。
func anyURIParseFailure(uris []AbsoluteURIFacts) bool {
	for _, uri := range uris {
		if uri.ParseFailureReason != "" {
			return true
		}
	}
	return false
}

// anyURIFileResolution 是否有 file URI 解析失败。
func anyURIFileResolution(uris []AbsoluteURIFacts) bool {
	for _, uri := range uris {
		if uri.FileResolutionReason != "" {
			return true
		}
	}
	return false
}

// dedupeStrings 按首次出现顺序去重。
func dedupeStrings(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		if _, exists := seen[item]; exists {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}
