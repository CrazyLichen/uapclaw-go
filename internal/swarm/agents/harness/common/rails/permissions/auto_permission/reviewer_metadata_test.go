package auto_permission

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions"
)

// ──────────────────────────── 辅助 ────────────────────────────

// metaFacts 构造 reviewer_metadata 测试用的 facts。
func metaFacts() permissions.ToolDecisionFacts {
	return permissions.ToolDecisionFacts{
		Capability: permissions.ToolCapability{
			ToolName: "read_file",
			Category: "path",
			RiskTier: "low",
		},
		WorkspaceRoot: "/ws",
	}
}

// ──────────────────────────── 语言注入 ────────────────────────────

// TestLocalized 验证双语选择。
func TestLocalized(t *testing.T) {
	pair := [2]string{"中文", "English"}
	if got := localized(pair, "en"); got != "English" {
		t.Errorf("en -> %q", got)
	}
	if got := localized(pair, "zh"); got != "中文" {
		t.Errorf("zh -> %q", got)
	}
}

// ──────────────────────────── 用例：truncateUI ───────────────────────────────

// TestTruncateUI 验证词边界截断。
func TestTruncateUI(t *testing.T) {
	cases := []struct {
		in        any
		maxLength int
		want      string
	}{
		{"short", 10, "short"},
		{"this is a long sentence", 12, "this is..."},
		{"abcdefghij", 3, "abc"},
		{"  trimmed  ", 20, "trimmed"},
		{nil, 10, ""},
		{42, 10, "42"},
	}
	for _, c := range cases {
		if got := truncateUI(c.in, c.maxLength); got != c.want {
			t.Errorf("truncateUI(%v,%d) = %q, want %q", c.in, c.maxLength, got, c.want)
		}
	}
}

// ──────────────────────────── 用例：uiText ───────────────────────────────

// TestUIText_按序取首个非空 验证候选键顺序。
func TestUIText_按序取首个非空(t *testing.T) {
	metadata := map[string]any{
		"a": "",
		"b": "value",
		"c": "other",
	}
	if got := uiText(metadata, []string{"a", "b", "c"}, "", 100); got != "value" {
		t.Errorf("got %q", got)
	}
	// 全空 → 默认值
	if got := uiText(metadata, []string{"missing"}, "default", 100); got != "default" {
		t.Errorf("got %q", got)
	}
}

// TestUIText_脱敏 验证敏感值被遮挡。
func TestUIText_脱敏(t *testing.T) {
	metadata := map[string]any{"reason": "api_key=secret123"}
	got := uiText(metadata, []string{"reason"}, "", 200)
	if strings.Contains(got, "secret123") {
		t.Errorf("should redact secret, got %q", got)
	}
}

// ──────────────────────────── 用例：reviewerStatus ───────────────────────────────

// TestReviewerStatus_各分支 验证状态归一。
func TestReviewerStatus_各分支(t *testing.T) {
	cases := []struct {
		name     string
		metadata map[string]any
		want     string
	}{
		{"final_reviewer_status", map[string]any{"final_reviewer_status": "approved"}, "approved"},
		{"reviewer_status", map[string]any{"reviewer_status": "denied"}, "denied"},
		{"outcome_deny", map[string]any{"reviewer_outcome": "deny"}, "denied"},
		{"outcome_denied", map[string]any{"reviewer_outcome": "DENIED"}, "denied"},
		{"lifecycle", map[string]any{"reviewer_lifecycle": "running"}, "running"},
		{"fallback", map[string]any{"fallback_reason": "x"}, "fallback"},
		{"默认manual", map[string]any{}, "manual"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := reviewerStatus(c.metadata); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

// ──────────────────────────── 用例：hostDisplayReason ───────────────────────────────

// TestHostDisplayReason_各分支 验证 16 个 key 的映射。
func TestHostDisplayReason_各分支(t *testing.T) {
	cases := []struct {
		name     string
		reason   string
		metadata map[string]any
		status   string
		wantKey  string
	}{
		{"执行provider", "x", map[string]any{"manual_reason_code": "execution_provider_contract_unverified"}, "manual", "execution_provider_contract_unverified"},
		{"用户拒绝", "x", map[string]any{"manual_reason_code": "user_rejected"}, "manual", "user_rejected"},
		{"超时状态", "x", map[string]any{}, "timed_out", "reviewer_timeout"},
		{"超时兜底", "x", map[string]any{"fallback_reason": "reviewer_timeout"}, "manual", "reviewer_timeout"},
		{"结论越界", "x", map[string]any{"manual_reason_code": "reviewer_outcome_not_allowed"}, "manual", "reviewer_outcome_not_allowed"},
		{"客户端异常", "x", map[string]any{"fallback_reason": "client_exception"}, "manual", "client_exception"},
		{"响应格式错", "x", map[string]any{"fallback_reason": "invalid_json"}, "manual", "reviewer_response_invalid"},
		{"载荷缺失", "x", map[string]any{"fallback_reason": "reviewer_payload_missing"}, "manual", "reviewer_input_unavailable"},
		{"脱敏不完整", "x", map[string]any{"fallback_reason": "reviewer_redaction_incomplete"}, "manual", "reviewer_redaction_incomplete"},
		{"载荷过大", "x", map[string]any{"fallback_reason": "reviewer_payload_too_large"}, "manual", "reviewer_payload_too_large"},
		{"兜底", "x", map[string]any{"fallback_reason": "other"}, "manual", "reviewer_fallback"},
		{"确定性放行", "x", map[string]any{"decision_source": "deterministic_scope"}, "manual", "deterministic_allow"},
		{"人工审批", "x", map[string]any{"decision_source": "manual_approval"}, "approved", "manual_approval"},
		{"策略放行", "policy_allow", map[string]any{"decision_source": "policy_evaluator"}, "approved", "policy_allow"},
		{"需人工", "x", map[string]any{}, "manual", "manual_required"},
		{"已拒绝", "x", map[string]any{}, "denied", "denied"},
		{"已完成", "x", map[string]any{}, "completed", "completed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := hostDisplayReason(c.reason, c.metadata, c.status, "zh")
			want := displayMessages[c.wantKey][0]
			if got != want {
				t.Errorf("got %q, want %q (key=%s)", got, want, c.wantKey)
			}
		})
	}
}

// TestHostDisplayReason_英文 验证英文文案。
func TestHostDisplayReason_英文(t *testing.T) {
	got := hostDisplayReason("x", map[string]any{}, "manual", "en")
	want := displayMessages["manual_required"][1]
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// ──────────────────────────── 用例：reviewerActionSummary ───────────────────────────────

// TestReviewerActionSummary 验证动作摘要格式。
func TestReviewerActionSummary(t *testing.T) {
	got := reviewerActionSummary(metaFacts())
	if got != "read_file (path, low risk)" {
		t.Errorf("got %q", got)
	}
}

// ──────────────────────────── 用例：reviewerUIMetadata ───────────────────────────────

// TestReviewerUIMetadata_补齐字段 验证输出字段齐全。
func TestReviewerUIMetadata_补齐字段(t *testing.T) {
	got := reviewerUIMetadata(metaFacts(), "reason", map[string]any{})

	for _, key := range []string{
		"action_summary", "decision_source", "evidence_summary",
		"final_reviewer_status", "reviewer_status", "risk_level",
	} {
		if _, exists := got[key]; !exists {
			t.Errorf("%s should be set", key)
		}
	}
	if got["risk_level"] != "low" {
		t.Errorf("risk_level = %v", got["risk_level"])
	}
}

// TestReviewerUIMetadata_剔除敏感字段 验证黑名单生效。
func TestReviewerUIMetadata_剔除敏感字段(t *testing.T) {
	got := reviewerUIMetadata(metaFacts(), "reason", map[string]any{
		"user_authorization":   "secret",
		"decision_digest":      "hash",
		"latency_ms":           100,
		"reviewer_raw_outcome": "allow_once",
		"search_query_preview": "q",
	})
	for _, key := range []string{
		"user_authorization", "decision_digest", "latency_ms",
		"reviewer_raw_outcome", "search_query_preview",
	} {
		if _, exists := got[key]; exists {
			t.Errorf("%s should be removed", key)
		}
	}
}

// TestReviewerUIMetadata_人工状态补提示 验证 manual 状态的 hint。
func TestReviewerUIMetadata_人工状态补提示(t *testing.T) {
	got := reviewerUIMetadata(metaFacts(), "reason", map[string]any{
		"final_reviewer_status": "manual",
	})
	if got["manual_reason_code"] == nil {
		t.Error("manual_reason_code should be set")
	}
	if got["user_review_hint"] == nil {
		t.Error("user_review_hint should be set")
	}
	// 语言取决于真实配置，断言中英任一（避免测试依赖环境）
	hint, _ := got["user_review_hint"].(string)
	if hint != manualHints["default"][0] && hint != manualHints["default"][1] {
		t.Errorf("hint = %v", got["user_review_hint"])
	}
}

// TestReviewerUIMetadata_非人工状态清提示 验证非 manual 状态剔除提示字段。
func TestReviewerUIMetadata_非人工状态清提示(t *testing.T) {
	got := reviewerUIMetadata(metaFacts(), "reason", map[string]any{
		"final_reviewer_status": "approved",
		"manual_reason_code":    "leftover",
		"user_review_hint":      "leftover",
	})
	for _, key := range []string{
		"manual_reason_code", "manual_reason_summary",
		"preserve_reviewer_user_review_hint", "user_review_hint",
	} {
		if _, exists := got[key]; exists {
			t.Errorf("%s should be removed for non-manual status", key)
		}
	}
}

// TestReviewerUIMetadata_降级放行用宿主摘要 验证 downgraded_allow 分支。
func TestReviewerUIMetadata_降级放行用宿主摘要(t *testing.T) {
	got := reviewerUIMetadata(metaFacts(), "reason", map[string]any{
		"final_reviewer_status":      "manual",
		"host_manual_reason_summary": "宿主摘要",
		"reviewer_reason_code":       "reviewer_outcome_not_allowed",
	})
	if got["evidence_summary"] != "宿主摘要" {
		t.Errorf("evidence_summary = %v", got["evidence_summary"])
	}
	if got["manual_reason_summary"] != "宿主摘要" {
		t.Errorf("manual_reason_summary = %v", got["manual_reason_summary"])
	}
}

// TestReviewerUIMetadata_保留审查提示 验证 preserve 标记生效。
func TestReviewerUIMetadata_保留审查提示(t *testing.T) {
	got := reviewerUIMetadata(metaFacts(), "reason", map[string]any{
		"final_reviewer_status":              "manual",
		"preserve_reviewer_user_review_hint": true,
		"user_review_hint":                   "保留我",
	})
	if got["user_review_hint"] != "保留我" {
		t.Errorf("hint should be preserved, got %v", got["user_review_hint"])
	}
}

// ──────────────────────────── 用例：withHostOwnedManualReviewDisplay ───────────────────────────────

// TestWithHostOwnedManualReviewDisplay_非人工路由 验证直返副本。
func TestWithHostOwnedManualReviewDisplay_非人工路由(t *testing.T) {
	route := &permissions.DecisionRoute{Level: "ask", Reason: "r", Source: permissions.SemanticReviewSource}
	metadata := map[string]any{"k": "v"}
	got, summary := withHostOwnedManualReviewDisplay(metaFacts(), route, metadata)
	if summary != nil {
		t.Errorf("summary should be nil, got %v", *summary)
	}
	if got["k"] != "v" {
		t.Errorf("got %v", got)
	}
}

// TestWithHostOwnedManualReviewDisplay_nil路由 验证 nil 安全。
func TestWithHostOwnedManualReviewDisplay_nil路由(t *testing.T) {
	got, summary := withHostOwnedManualReviewDisplay(metaFacts(), nil, map[string]any{})
	if summary != nil {
		t.Error("summary should be nil")
	}
	if got == nil {
		t.Error("should return a copy")
	}
}

// TestWithHostOwnedManualReviewDisplay_代码解析错误 验证专用文案。
func TestWithHostOwnedManualReviewDisplay_代码解析错误(t *testing.T) {
	route := &permissions.DecisionRoute{
		Level: "ask", Reason: "code_parse_error", Source: permissions.ManualReviewSource,
	}
	got, summary := withHostOwnedManualReviewDisplay(metaFacts(), route, map[string]any{})
	if summary == nil {
		t.Fatal("summary should be set")
	}
	if !strings.Contains(*summary, "代码无法解析") &&
		!strings.Contains(*summary, "could not be parsed") {
		t.Errorf("summary = %q", *summary)
	}
	if got["host_manual_reason_code"] != "code_parse_error" {
		t.Errorf("reason code = %v", got["host_manual_reason_code"])
	}
	if got["preserve_reviewer_user_review_hint"] != true {
		t.Error("preserve flag should be true")
	}
}

// TestWithHostOwnedManualReviewDisplay_载荷缺失 验证第二类专用文案。
func TestWithHostOwnedManualReviewDisplay_载荷缺失(t *testing.T) {
	route := &permissions.DecisionRoute{
		Level: "ask", Reason: "code_payload_missing", Source: permissions.ManualReviewSource,
	}
	_, summary := withHostOwnedManualReviewDisplay(metaFacts(), route, map[string]any{})
	if summary == nil || (!strings.Contains(*summary, "可执行代码内容缺失") &&
		!strings.Contains(*summary, "executable code payload is missing")) {
		t.Errorf("summary = %v", summary)
	}
}

// TestWithHostOwnedManualReviewDisplay_browser覆盖 验证 browser 特殊原因码。
func TestWithHostOwnedManualReviewDisplay_browser覆盖(t *testing.T) {
	facts := metaFacts()
	facts.Capability.Category = "browser"
	route := &permissions.DecisionRoute{
		Level: "ask", Reason: "other_reason", Source: permissions.ManualReviewSource,
	}
	metadata := map[string]any{"manual_reason_code": "browser_network_guard_unverified"}
	got, _ := withHostOwnedManualReviewDisplay(facts, route, metadata)
	if got["host_manual_reason_code"] != "browser_network_guard_unverified" {
		t.Errorf("got %v", got["host_manual_reason_code"])
	}
}

// ──────────────────────────── 辅助函数 ────────────────────────────

// TestDecisionRouteNoAutoAllowReason 验证 property 等价实现。
func TestDecisionRouteNoAutoAllowReason(t *testing.T) {
	if got := decisionRouteNoAutoAllowReason(nil); got != "" {
		t.Errorf("nil -> %q", got)
	}
	manual := &permissions.DecisionRoute{Reason: "r", Source: permissions.ManualReviewSource}
	if got := decisionRouteNoAutoAllowReason(manual); got != "r" {
		t.Errorf("manual -> %q", got)
	}
	semantic := &permissions.DecisionRoute{Reason: "r", Source: permissions.SemanticReviewSource}
	if got := decisionRouteNoAutoAllowReason(semantic); got != "" {
		t.Errorf("semantic -> %q", got)
	}
}

// TestLocalizedHostDisplayReason 验证语言回落。
func TestLocalizedHostDisplayReason(t *testing.T) {
	got := localizedHostDisplayReason("x", map[string]any{}, "manual")
	if got != displayMessages["manual_required"][0] && got != displayMessages["manual_required"][1] {
		t.Errorf("got %q", got)
	}
}

// TestHasKey 验证集合判定。
func TestHasKey(t *testing.T) {
	set := map[string]struct{}{"a": {}}
	if !hasKey(set, "a") {
		t.Error("should contain a")
	}
	if hasKey(set, "b") {
		t.Error("should not contain b")
	}
}

// ──────────────────────────── V-9 回填验证 ────────────────────────────

// TestNormalizeRuntimePermissionResult_用户拒绝转denied 验证 V-9 回填。
func TestNormalizeRuntimePermissionResult_用户拒绝转denied(t *testing.T) {
	result := map[string]any{"feedback": "[permission_rejected] user said no"}
	got := normalizeRuntimePermissionResult(
		bridgeInvocation("bash", nil), result, metaFacts(), manualApprovalDecisionSource)

	payload, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("got %T, want map", got)
	}
	if payload["status"] != "denied" {
		t.Errorf("status = %v", payload["status"])
	}
	if payload["reason"] != "user_rejected" {
		t.Errorf("reason = %v", payload["reason"])
	}
	if payload["permission_decision"] != "deny" {
		t.Errorf("permission_decision = %v", payload["permission_decision"])
	}
	if payload["reviewer_metadata"] == nil {
		t.Error("reviewer_metadata should be set")
	}
}

// TestNormalizeRuntimePermissionResult_非人工审批来源直返 验证来源校验。
func TestNormalizeRuntimePermissionResult_非人工审批来源直返(t *testing.T) {
	result := map[string]any{"feedback": "[permission_rejected] x"}
	got := normalizeRuntimePermissionResult(
		bridgeInvocation("bash", nil), result, metaFacts(), "auto_reviewer")
	mapping, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("got %T", got)
	}
	if mapping["status"] != nil {
		t.Errorf("should return original result, got %v", mapping)
	}
}

// TestNormalizeRuntimePermissionResult_非用户拒绝直返 验证类别校验。
func TestNormalizeRuntimePermissionResult_非用户拒绝直返(t *testing.T) {
	result := map[string]any{"feedback": "[permission_denied] x"}
	got := normalizeRuntimePermissionResult(
		bridgeInvocation("bash", nil), result, metaFacts(), manualApprovalDecisionSource)
	mapping, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("got %T", got)
	}
	// denied 不走 user_rejection 转换
	if mapping["reason"] != nil {
		t.Errorf("should return original, got %v", mapping)
	}
}

// TestTruncateUI_中文不切坏 验证 rune 语义（对齐 Python 字符切片）。
func TestTruncateUI_中文不切坏(t *testing.T) {
	text := strings.Repeat("中", 20)
	got := truncateUI(text, 10)
	if strings.ContainsRune(got, '\uFFFD') {
		t.Errorf("切坏多字节字符：%q", got)
	}
	if !utf8.ValidString(got) {
		t.Errorf("输出不是合法 UTF-8：%q", got)
	}
	if utf8.RuneCountInString(got) != 10 {
		t.Errorf("字符数 = %d, want 10（got %q）", utf8.RuneCountInString(got), got)
	}
}
