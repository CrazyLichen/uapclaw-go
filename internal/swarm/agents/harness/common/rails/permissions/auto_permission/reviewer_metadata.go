// 审查器审计与 UI 元数据的宿主侧紧凑投影。
//
// 对应 Python 代码：_auto_permission/reviewer_metadata.py（401 行 / 11 函数）
//
// 职责：把审查器产出的原始字段归一成 audit / Web / TUI 消费的稳定形状——
//   - 补齐 action_summary / evidence_summary / reviewer_status / risk_level
//   - 剔除不该外泄的字段（user_authorization / decision_digest / latency_ms ...）
//   - 按状态决定人工提示文案（manual_reason_code / user_review_hint）
//   - 中英双语展示文案
//
// ## 显示语言读取：与 Python 一致
//
// Python 用 `get_config().get("preferred_language")`；Go 侧无全局单例，
// 但可用 `configutil.New("").Load()` 每次加载配置——与同仓库
// `team_manager_spec.go` 的 `configPreferredLanguage` 同一做法。

package auto_permission

import (
	"strings"
	"unicode/utf8"

	configutil "github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// reviewerUIStatusManual 人工待定状态
	reviewerUIStatusManual = "manual"
	// reviewerUIStatusFallback 审查兜底状态
	reviewerUIStatusFallback = "fallback"
	// reviewerUIStatusTimedOut 审查超时状态
	reviewerUIStatusTimedOut = "timed_out"
	// reviewerUIStatusAborted 审查中断状态
	reviewerUIStatusAborted = "aborted"
	// reviewerUIStatusApproved 已批准状态
	reviewerUIStatusApproved = "approved"
	// reviewerUIStatusDenied 已拒绝状态
	reviewerUIStatusDenied = "denied"
	// reviewerUIStatusBlocked 已阻断状态
	reviewerUIStatusBlocked = "blocked"
	// reviewerUIStatusDeterministicAllow 确定性放行状态
	reviewerUIStatusDeterministicAllow = "deterministic_allow"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// manualStatuses 需要人工介入的状态集合。
	//
	// Python: _MANUAL_STATUSES
	manualStatuses = map[string]struct{}{
		reviewerUIStatusManual:   {},
		reviewerUIStatusFallback: {},
		reviewerUIStatusTimedOut: {},
		reviewerUIStatusAborted:  {},
	}

	// reviewerResponseErrors 审查器响应格式类错误码集合。
	//
	// Python: _REVIEWER_RESPONSE_ERRORS
	reviewerResponseErrors = map[string]struct{}{
		"invalid_json":         {},
		"forbidden_field":      {},
		"missing_field":        {},
		"unknown_field":        {},
		"invalid_field":        {},
		"unknown_outcome":      {},
		"missing_manual_field": {},
	}

	// displayMessages 展示文案（中文, 英文）双语对。
	//
	// Python: _DISPLAY_MESSAGES
	// 逐字复制上游文案，禁止翻译或改写。
	displayMessages = map[string][2]string{
		"policy_allow": {
			"当前权限规则已允许此操作，无需再次审批。",
			"Your permission settings already allow this action. No approval needed.",
		},
		"completed": {
			"宿主权限检查已完成。",
			"Host permission checks completed.",
		},
		"denied": {
			"宿主权限检查拒绝了此操作。",
			"Host permission checks denied this operation.",
		},
		"deterministic_allow": {
			"宿主权限规则已确认该操作满足自动执行条件。",
			"Host permission rules verified that this operation meets automatic execution conditions.",
		},
		"execution_provider_contract_unverified": {
			"Host 无法验证当前工具的执行 provider 合同，因此需要人工确认；人工允许不会改变工具的实际执行环境。",
			"The Host cannot verify this tool's execution-provider contract, so manual " +
				"confirmation is required. Manual approval does not change the tool's actual " +
				"execution environment.",
		},
		"manual_approval": {
			"用户已通过人工审批允许此操作。",
			"The user allowed this operation through manual approval.",
		},
		"manual_required": {
			"当前权限证据不足以自动批准，请人工核对工具参数和风险。",
			"Current permission evidence is insufficient for automatic approval; review " +
				"the tool parameters and risks manually.",
		},
		"reviewer_fallback": {
			"智能审批未能完成判断，已转人工审批。",
			"Smart approval could not complete the decision; manual review is required.",
		},
		"reviewer_input_unavailable": {
			"调用信息缺失或无效，未调用审查模型，已转人工审批。",
			"Call information is missing or invalid; the reviewer model was not called. Manual review is required.",
		},
		"reviewer_response_invalid": {
			"智能审批结果格式不符合要求，已转人工审批。",
			"The smart approval response has an invalid format; manual review is required.",
		},
		"client_exception": {
			"智能审批服务异常，未取得有效结论，已转人工审批。",
			"The smart approval service failed without a valid decision; manual review is required.",
		},
		"reviewer_payload_too_large": {
			"完整审查正文超出字节预算，未调用审查模型，已转人工审批。",
			"The complete review payload exceeds the byte budget; " +
				"the reviewer model was not called. Manual review is required.",
		},
		"reviewer_redaction_incomplete": {
			"秘密值遮挡后无法完整核验调用结构，未调用审查模型，已转人工审批。",
			"Secret redaction prevents a complete review of the call structure; " +
				"the reviewer model was not called. Manual review is required.",
		},
		"reviewer_outcome_not_allowed": {
			"自动审批给出的结论超出宿主策略允许范围，已转人工审批。",
			"AutoReviewer returned an outcome outside the host policy allowance; manual review is required.",
		},
		"reviewer_timeout": {
			"自动审批审查超时，未能形成可信结论。",
			"AutoReviewer timed out before producing a trusted decision.",
		},
		"user_rejected": {
			"用户拒绝了本次工具调用。",
			"The user rejected this tool call.",
		},
	}

	// manualHints 人工复核提示（中文, 英文）双语对。
	//
	// Python: _MANUAL_HINTS
	manualHints = map[string][2]string{
		"default": {
			"请核对工具参数是否符合用户意图，再决定是否允许。",
			"Verify that the tool parameters match the user's intent before approving.",
		},
		"fallback": {
			"自动审批无法完成判断，已转人工审批。",
			"AutoReviewer could not complete the decision; review it manually.",
		},
		"timed_out": {
			"自动审批审查超时，请人工核对工具参数和风险后决定。",
			"Review the tool parameters and risks manually because AutoReviewer timed out.",
		},
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// configuredReviewerDisplayLanguage 读取配置里的展示语言（默认 "zh"）。
//
// Python: _configured_reviewer_display_language()
//
//	try:
//	    config = get_config()
//	    if isinstance(config, Mapping):
//	        return "en" if str(config.get("preferred_language") or "").strip().lower() == "en" else "zh"
//	except Exception:
//	    return "zh"
//	return "zh"
//
// Go 侧无全局 config 单例，改为每次加载配置文件——与同仓库
// `team_manager_spec.go` 的 configPreferredLanguage 同一做法。
// 任一环节失败一律回落 "zh"（对齐 Python 的 except 分支）。
func configuredReviewerDisplayLanguage() string {
	loaded, err := configutil.New("", configutil.WithNormalize(configutil.NormalizeConfig))
	if err != nil {
		return "zh"
	}
	cfgMap, err := loaded.Load()
	if err != nil {
		return "zh"
	}
	if language, ok := cfgMap["preferred_language"].(string); ok {
		if strings.ToLower(strings.TrimSpace(language)) == "en" {
			return "en"
		}
	}
	return "zh"
}

// localized 从双语对里取对应语言文案。
//
// Python: _localized(pair, language)
func localized(pair [2]string, language string) string {
	if language == "en" {
		return pair[1]
	}
	return pair[0]
}

// truncateUI 按词边界截断并补省略号。
//
// Python: _truncate(value, *, max_length)
func truncateUI(value any, maxLength int) string {
	text := strings.TrimSpace(anyToStringValue(value))
	// Python 的 len() / 切片都是字符语义，Go 需用 rune 数避免切坏多字节字符
	runes := []rune(text)
	if len(runes) <= maxLength {
		return text
	}
	if maxLength <= 3 {
		return string(runes[:maxLength])
	}
	candidate := strings.TrimRight(string(runes[:maxLength-3]), " \t")
	byteBoundary := strings.LastIndex(candidate, " ")
	if tabBoundary := strings.LastIndex(candidate, "\t"); tabBoundary > byteBoundary {
		byteBoundary = tabBoundary
	}
	// boundary 需换算成字符位置再与 maxLength/2 比较（Python 是字符索引）
	if byteBoundary >= 0 {
		runeBoundary := utf8.RuneCountInString(candidate[:byteBoundary])
		if runeBoundary >= maxLength/2 {
			candidate = strings.TrimRight(candidate[:byteBoundary], " \t")
		}
	}
	return candidate + "..."
}

// uiText 按候选键取第一个非空值，脱敏后截断。
//
// Python: _text(metadata, *keys, default="", max_length=240)
func uiText(metadata map[string]any, keys []string, defaultValue string, maxLength int) string {
	value := any(defaultValue)
	for _, key := range keys {
		candidate, exists := metadata[key]
		if !exists || candidate == nil {
			continue
		}
		if text, isString := candidate.(string); isString && text == "" {
			continue
		}
		value = candidate
		break
	}
	redacted := permissions.RedactText(value,
		permissions.WithRedactTextMaxLength(maxLength*2))
	return truncateUI(redacted, maxLength)
}

// reviewerStatus 归一审查器状态。
//
// Python: _reviewer_status(metadata)
func reviewerStatus(metadata map[string]any) string {
	status := uiText(metadata, []string{"final_reviewer_status", "reviewer_status"},
		"", ReviewerUIShortTextMaxLength)
	if status != "" {
		return status
	}
	outcome := strings.ToLower(uiText(metadata, []string{"reviewer_outcome"}, "",
		ReviewerUIShortTextMaxLength))
	if outcome == "deny" || outcome == "denied" {
		return reviewerUIStatusDenied
	}
	lifecycle := uiText(metadata, []string{"reviewer_lifecycle"}, "", ReviewerUIShortTextMaxLength)
	if lifecycle != "" {
		return lifecycle
	}
	if uiText(metadata, []string{"reviewer_fallback_reason", "fallback_reason"}, "",
		ReviewerUIShortTextMaxLength) != "" {
		return reviewerUIStatusFallback
	}
	return reviewerUIStatusManual
}

// hostDisplayReason 按状态选择展示原因文案。
//
// Python: _host_display_reason(reason, metadata, *, status, language)
func hostDisplayReason(reason string, metadata map[string]any, status, language string) string {
	reasonCode := uiText(metadata, []string{"manual_reason_code", "reviewer_reason_code"},
		reason, ReviewerUIShortTextMaxLength)
	fallback := uiText(metadata, []string{"fallback_reason", "reviewer_fallback_reason"},
		"", ReviewerUIShortTextMaxLength)
	source := uiText(metadata, []string{"decision_source"}, "", ReviewerUIShortTextMaxLength)

	var key string
	switch {
	case reasonCode == "execution_provider_contract_unverified" || reasonCode == "user_rejected":
		key = reasonCode
	case status == reviewerUIStatusTimedOut || fallback == "reviewer_timeout":
		key = "reviewer_timeout"
	case reasonCode == "reviewer_outcome_not_allowed":
		key = "reviewer_outcome_not_allowed"
	case fallback == "client_exception":
		key = "client_exception"
	case hasKey(reviewerResponseErrors, fallback):
		key = "reviewer_response_invalid"
	case fallback == "reviewer_payload_missing" || fallback == "reviewer_payload_invalid":
		key = "reviewer_input_unavailable"
	case fallback == "reviewer_redaction_incomplete":
		key = "reviewer_redaction_incomplete"
	case fallback == "reviewer_payload_too_large":
		key = "reviewer_payload_too_large"
	case fallback != "" || reasonCode == "reviewer_fallback":
		key = "reviewer_fallback"
	case strings.HasPrefix(source, "deterministic_"):
		key = "deterministic_allow"
	case source == manualApprovalDecisionSource &&
		(status == reviewerUIStatusApproved || status == reviewerUIStatusDeterministicAllow):
		key = "manual_approval"
	case source == "policy_evaluator" && reason == "policy_allow" && status == reviewerUIStatusApproved:
		key = "policy_allow"
	case hasKey(manualStatuses, status):
		key = "manual_required"
	case status == reviewerUIStatusDenied || status == reviewerUIStatusBlocked:
		key = "denied"
	default:
		key = "completed"
	}
	return localized(displayMessages[key], language)
}

// reviewerActionSummary 描述动作，不重新解析路径/URL/shell 语法。
//
// Python: _reviewer_action_summary(facts)
func reviewerActionSummary(facts permissions.ToolDecisionFacts) string {
	return facts.Capability.ToolName + " (" + facts.Capability.Category + ", " +
		facts.Capability.RiskTier + " risk)"
}

// reviewerUIMetadata 返回 audit / Web / TUI 消费的归一化决策字段。
//
// Python: _reviewer_ui_metadata(facts, *, reason, metadata)
func reviewerUIMetadata(
	facts permissions.ToolDecisionFacts,
	reason string,
	metadata map[string]any,
) map[string]any {
	merged := make(map[string]any, len(metadata)+8)
	for key, value := range metadata {
		merged[key] = value
	}
	status := reviewerStatus(merged)
	language := configuredReviewerDisplayLanguage()

	defaultSource := "auto_reviewer"
	if status == reviewerUIStatusFallback || status == reviewerUIStatusTimedOut {
		defaultSource = "fallback"
	}
	source := uiText(merged, []string{"decision_source"}, defaultSource, ReviewerUIShortTextMaxLength)

	naturalLanguage := source == "auto_reviewer" &&
		status != reviewerUIStatusFallback &&
		status != reviewerUIStatusTimedOut &&
		status != reviewerUIStatusAborted &&
		uiText(merged, []string{"fallback_reason", "reviewer_fallback_reason"}, "",
			ReviewerUIShortTextMaxLength) == "" &&
		!hasKey(map[string]struct{}{
			"reviewer_fallback":            {},
			"reviewer_outcome_not_allowed": {},
		}, uiText(merged, []string{"reviewer_reason_code", "reason_code"}, "",
			ReviewerUIShortTextMaxLength))

	hostSummary := uiText(merged, []string{"host_manual_reason_summary"}, "",
		ReviewerUITextMaxLength)
	downgradedAllow := hasKey(manualStatuses, status) && hostSummary != "" &&
		uiText(merged, []string{"reviewer_reason_code", "reason_code"}, "",
			ReviewerUIShortTextMaxLength) == "reviewer_outcome_not_allowed"

	var evidence string
	switch {
	case downgradedAllow:
		evidence = hostSummary
	case naturalLanguage:
		evidence = uiText(merged,
			[]string{"evidence_summary", "reviewer_reason_summary"}, reason, ReviewerUITextMaxLength)
	default:
		evidence = hostDisplayReason(reason, merged, status, language)
	}

	actionSummary := uiText(merged, []string{"action_summary"}, reviewerActionSummary(facts),
		ReviewerUIShortTextMaxLength)

	merged["action_summary"] = truncateUI(
		common.SanitizeReviewUIValue(actionSummary, facts.WorkspaceRoot),
		ReviewerUIShortTextMaxLength)
	merged["decision_source"] = source
	merged["evidence_summary"] = evidence
	merged["final_reviewer_status"] = status
	merged["reviewer_status"] = status

	riskLevel := uiText(merged, []string{"risk_level"}, "", ReviewerUIShortTextMaxLength)
	if riskLevel == "" {
		riskLevel = strings.TrimSpace(facts.Capability.RiskTier)
		if riskLevel == "" {
			riskLevel = "unknown"
		}
	}
	merged["risk_level"] = riskLevel

	delete(merged, "user_authorization")
	for _, key := range []string{
		"decision_digest", "latency_ms", "reviewer_raw_outcome", "reviewer_raw_status",
		"reviewer_user_review_hint", "search_query_digest", "search_query_preview",
		"search_source_tool",
	} {
		delete(merged, key)
	}

	if hasKey(manualStatuses, status) {
		merged["manual_reason_code"] = uiText(merged,
			[]string{"manual_reason_code", "reviewer_reason_code"}, reason, ReviewerUIShortTextMaxLength)
		switch {
		case downgradedAllow:
			merged["manual_reason_summary"] = hostSummary
		case naturalLanguage:
			merged["manual_reason_summary"] = uiText(merged,
				[]string{"manual_reason_summary", "reviewer_reason_summary"},
				reason, ReviewerUITextMaxLength)
		default:
			merged["manual_reason_summary"] = evidence
		}
		if preserved, _ := merged["preserve_reviewer_user_review_hint"].(bool); !preserved {
			hint, exists := manualHints[status]
			if !exists {
				hint = manualHints["default"]
			}
			merged["user_review_hint"] = localized(hint, language)
		}
	} else {
		for _, key := range []string{
			"manual_reason_code", "manual_reason_summary",
			"preserve_reviewer_user_review_hint", "reviewer_user_review_hint", "user_review_hint",
		} {
			delete(merged, key)
		}
	}
	return merged
}

// withHostOwnedManualReviewDisplay 当宿主路由本身要求人工时替换模型措辞。
//
// Python: _with_host_owned_manual_review_display(facts, route, metadata)
// 返回 (合并后的元数据, 摘要指针)；摘要为 nil 表示路由不要求人工。
func withHostOwnedManualReviewDisplay(
	facts permissions.ToolDecisionFacts,
	route *permissions.DecisionRoute,
	metadata map[string]any,
) (map[string]any, *string) {
	copied := make(map[string]any, len(metadata))
	for key, value := range metadata {
		copied[key] = value
	}
	if route == nil || route.Source != permissions.ManualReviewSource {
		return copied, nil
	}
	language := configuredReviewerDisplayLanguage()
	noAutoAllowReason := decisionRouteNoAutoAllowReason(route)

	var summary string
	switch noAutoAllowReason {
	case "code_parse_error":
		summary = localized([2]string{
			"代码无法解析，静态读写和导入检查未执行，因此需要人工审批。",
			"The code could not be parsed, so static checks did not run; manual approval is required.",
		}, language)
	case "code_payload_missing":
		summary = localized([2]string{
			"可执行代码内容缺失或为空，因此需要人工审批。",
			"The executable code payload is missing or empty; manual approval is required.",
		}, language)
	default:
		summary = localized(displayMessages["manual_required"], language)
	}

	reasonCode := noAutoAllowReason
	if facts.Capability.Category == "browser" &&
		(uiText(metadata, []string{"manual_reason_code", "reviewer_reason_code"}, "",
			ReviewerUIShortTextMaxLength) == "browser_network_guard_unverified" ||
			reasonCode == "browser_network_guard_unverified") {
		reasonCode = "browser_network_guard_unverified"
	}

	merged := make(map[string]any, len(metadata)+6)
	for key, value := range metadata {
		merged[key] = value
	}
	merged["evidence_summary"] = summary
	merged["host_manual_reason_code"] = reasonCode
	merged["host_manual_reason_summary"] = summary
	merged["manual_reason_summary"] = summary
	merged["preserve_reviewer_user_review_hint"] = true
	merged["user_review_hint"] = localized(manualHints["default"], language)
	return merged, &summary
}

// localizedHostDisplayReason 用当前语言生成宿主展示原因。
//
// Python: _localized_host_display_reason(reason, metadata, *, reviewer_status)
func localizedHostDisplayReason(reason string, metadata map[string]any, reviewerStatusValue string) string {
	return hostDisplayReason(reason, metadata, reviewerStatusValue, configuredReviewerDisplayLanguage())
}

// decisionRouteNoAutoAllowReason 路由要求人工时返回其 reason。
//
// Python: DecisionRoute.no_auto_allow_reason property
func decisionRouteNoAutoAllowReason(route *permissions.DecisionRoute) string {
	if route == nil || route.Source != permissions.ManualReviewSource {
		return ""
	}
	return route.Reason
}

// hasKey 集合是否包含指定键。
func hasKey(set map[string]struct{}, key string) bool {
	_, exists := set[key]
	return exists
}
