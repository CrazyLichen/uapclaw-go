package auto_permission

import (
	"testing"

	hsecurity "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions"
)

// ──────────────────────────── 用例：ReviewerRouteAuditExtra ───────────────────────────────

// TestReviewerRouteAuditExtra_基本 验证路由溯源投影。
func TestReviewerRouteAuditExtra_基本(t *testing.T) {
	route := &permissions.DecisionRoute{
		Level:  "ask",
		Reason: "semantic_review_required",
		Source: "semantic_reviewer",
	}
	got := ReviewerRouteAuditExtra(route, "", nil)
	if got["host_route_reason"] != "semantic_review_required" {
		t.Errorf("host_route_reason = %v", got["host_route_reason"])
	}
	if got["host_route_source"] != "semantic_reviewer" {
		t.Errorf("host_route_source = %v", got["host_route_source"])
	}
	if _, exists := got["policy_level"]; exists {
		t.Error("empty policy_level should be omitted")
	}
	if _, exists := got["reviewer_called"]; exists {
		t.Error("nil reviewer_called should be omitted")
	}
}

// TestReviewerRouteAuditExtra_可选字段 验证两个可选字段。
func TestReviewerRouteAuditExtra_可选字段(t *testing.T) {
	route := &permissions.DecisionRoute{Reason: "r", Source: "s"}
	called := true
	got := ReviewerRouteAuditExtra(route, "ask", &called)
	if got["policy_level"] != "ask" {
		t.Errorf("policy_level = %v", got["policy_level"])
	}
	if got["reviewer_called"] != true {
		t.Errorf("reviewer_called = %v", got["reviewer_called"])
	}
}

// TestReviewerRouteAuditExtra_false也保留 验证 false 不被省略。
func TestReviewerRouteAuditExtra_false也保留(t *testing.T) {
	called := false
	got := ReviewerRouteAuditExtra(&permissions.DecisionRoute{}, "", &called)
	value, exists := got["reviewer_called"]
	if !exists {
		t.Fatal("false reviewer_called should be kept")
	}
	if value != false {
		t.Errorf("got %v", value)
	}
}

// TestReviewerRouteAuditExtra_nil路由 验证 nil 安全。
func TestReviewerRouteAuditExtra_nil路由(t *testing.T) {
	got := ReviewerRouteAuditExtra(nil, "ask", nil)
	if _, exists := got["host_route_reason"]; exists {
		t.Error("nil route should not produce reason")
	}
	if got["policy_level"] != "ask" {
		t.Errorf("policy_level = %v", got["policy_level"])
	}
}

// ──────────────────────────── 用例：ParseConfirmationPayload ───────────────────────────────

// TestParseConfirmationPayload_map形态 验证直拷贝。
func TestParseConfirmationPayload_map形态(t *testing.T) {
	source := map[string]any{"approved": true}
	got := ParseConfirmationPayload(source)
	if got["approved"] != true {
		t.Errorf("got %v", got)
	}
	// 必须是副本
	got["approved"] = false
	if source["approved"] != true {
		t.Error("should be a copy")
	}
}

// TestParseConfirmationPayload_JSON字符串 验证解析。
func TestParseConfirmationPayload_JSON字符串(t *testing.T) {
	got := ParseConfirmationPayload(`{"approved":true,"feedback":"ok"}`)
	if got["approved"] != true || got["feedback"] != "ok" {
		t.Errorf("got %v", got)
	}
	// 非对象
	if got := ParseConfirmationPayload(`[1,2]`); got != nil {
		t.Errorf("array -> %v", got)
	}
	// 非法 JSON
	if got := ParseConfirmationPayload(`{bad}`); got != nil {
		t.Errorf("invalid -> %v", got)
	}
}

// TestParseConfirmationPayload_对象形态 验证 PermissionConfirmResponse 投影。
func TestParseConfirmationPayload_对象形态(t *testing.T) {
	response := hsecurity.PermissionConfirmResponse{
		Approved:     true,
		Feedback:     "ok",
		AutoConfirm:  false,
		PersistScope: "",
	}
	got := ParseConfirmationPayload(response)
	if got["approved"] != true {
		t.Errorf("approved = %v", got["approved"])
	}
	if got["feedback"] != "ok" {
		t.Errorf("feedback = %v", got["feedback"])
	}
	if got["persist_allow"] != false {
		t.Errorf("persist_allow = %v", got["persist_allow"])
	}
}

// TestParseConfirmationPayload_指针形态 验证指针输入。
func TestParseConfirmationPayload_指针形态(t *testing.T) {
	response := &hsecurity.PermissionConfirmResponse{Approved: true}
	got := ParseConfirmationPayload(response)
	if got["approved"] != true {
		t.Errorf("got %v", got)
	}
	// nil 指针
	var nilResponse *hsecurity.PermissionConfirmResponse
	if got := ParseConfirmationPayload(nilResponse); got != nil {
		t.Errorf("nil -> %v", got)
	}
}

// TestParseConfirmationPayload_不支持类型 验证返回 nil。
func TestParseConfirmationPayload_不支持类型(t *testing.T) {
	for _, value := range []any{nil, 42, []any{1}, struct{}{}} {
		if got := ParseConfirmationPayload(value); got != nil {
			t.Errorf("%v -> %v, want nil", value, got)
		}
	}
}

// ──────────────────────────── 用例：IsAllowOnceConfirmation ───────────────────────────────

// TestIsAllowOnceConfirmation_正常单次放行 验证基础通过。
func TestIsAllowOnceConfirmation_正常单次放行(t *testing.T) {
	if !IsAllowOnceConfirmation(map[string]any{"approved": true}) {
		t.Error("plain approval should be allow-once")
	}
}

// TestIsAllowOnceConfirmation_不可解析 验证空载荷拒绝。
func TestIsAllowOnceConfirmation_不可解析(t *testing.T) {
	for _, value := range []any{nil, 42, "not json", map[string]any{}} {
		if IsAllowOnceConfirmation(value) {
			t.Errorf("%v should not be allow-once", value)
		}
	}
}

// TestIsAllowOnceConfirmation_approved非严格true 验证严格判等。
func TestIsAllowOnceConfirmation_approved非严格true(t *testing.T) {
	// 非 bool 的 truthy 值不算
	for _, value := range []any{"true", 1, map[string]any{}} {
		if IsAllowOnceConfirmation(map[string]any{"approved": value}) {
			t.Errorf("approved=%v (%T) should not count", value, value)
		}
	}
	// approved=false
	if IsAllowOnceConfirmation(map[string]any{"approved": false}) {
		t.Error("approved=false should not be allow-once")
	}
}

// TestIsAllowOnceConfirmation_持久化意图 验证 auto_confirm / persist_allow 排除。
func TestIsAllowOnceConfirmation_持久化意图(t *testing.T) {
	if IsAllowOnceConfirmation(map[string]any{"approved": true, "auto_confirm": true}) {
		t.Error("auto_confirm should exclude allow-once")
	}
	if IsAllowOnceConfirmation(map[string]any{"approved": true, "persist_allow": true}) {
		t.Error("persist_allow should exclude allow-once")
	}
}

// TestIsAllowOnceConfirmation_退役任务级键 验证三个退役键都被拒绝。
func TestIsAllowOnceConfirmation_退役任务级键(t *testing.T) {
	for _, key := range []string{"task_grant", "task_grant_proposal_id", "task_grant_request_id"} {
		payload := map[string]any{"approved": true, key: "anything"}
		if IsAllowOnceConfirmation(payload) {
			t.Errorf("retired key %q should exclude allow-once（安全要点）", key)
		}
	}
}

// TestIsAllowOnceConfirmation_审查器覆盖令牌 验证令牌排除。
func TestIsAllowOnceConfirmation_审查器覆盖令牌(t *testing.T) {
	payload := map[string]any{"approved": true, "reviewer_override_token": "tok"}
	if IsAllowOnceConfirmation(payload) {
		t.Error("reviewer_override_token should exclude allow-once")
	}
}

// TestIsAllowOnceConfirmation_对象形态 验证从 PermissionConfirmResponse 判定。
func TestIsAllowOnceConfirmation_对象形态(t *testing.T) {
	// 纯批准 → allow-once
	if !IsAllowOnceConfirmation(hsecurity.PermissionConfirmResponse{Approved: true}) {
		t.Error("plain response should be allow-once")
	}
	// AutoConfirm → 排除
	if IsAllowOnceConfirmation(hsecurity.PermissionConfirmResponse{
		Approved: true, AutoConfirm: true,
	}) {
		t.Error("AutoConfirm response should not be allow-once")
	}
	// PersistScope 不参与判定（Python 的 PermissionConfirmResponse 无
	// persist_allow 属性，getattr 恒取默认 False）→ 仍算 allow-once
	if !IsAllowOnceConfirmation(hsecurity.PermissionConfirmResponse{
		Approved: true, PersistScope: "session",
	}) {
		t.Error("PersistScope should not affect allow-once（对齐 Python）")
	}
}

// ──────────────────────────── 用例：IsRejectionConfirmation ───────────────────────────────

// TestIsRejectionConfirmation 验证拒绝判定。
func TestIsRejectionConfirmation(t *testing.T) {
	if !IsRejectionConfirmation(map[string]any{"approved": false}) {
		t.Error("approved=false should be rejection")
	}
	if IsRejectionConfirmation(map[string]any{"approved": true}) {
		t.Error("approved=true should not be rejection")
	}
	// 非严格 false
	if IsRejectionConfirmation(map[string]any{"approved": 0}) {
		t.Error("approved=0 should not count")
	}
	// 不可解析
	if IsRejectionConfirmation(nil) {
		t.Error("nil should not be rejection")
	}
	// 对象形态
	if !IsRejectionConfirmation(hsecurity.PermissionConfirmResponse{Approved: false}) {
		t.Error("response approved=false should be rejection")
	}
}

// ──────────────────────────── 辅助 ────────────────────────────

// TestIsStrictTrue 验证严格 true 判定。
func TestIsStrictTrue(t *testing.T) {
	if !isStrictTrue(true) {
		t.Error("true should be strict true")
	}
	for _, value := range []any{false, "true", 1, nil} {
		if isStrictTrue(value) {
			t.Errorf("%v (%T) should not be strict true", value, value)
		}
	}
}

// TestContainsRetiredTaskScopeConfirmation 验证三个退役键识别。
func TestContainsRetiredTaskScopeConfirmation(t *testing.T) {
	for _, key := range []string{"task_grant", "task_grant_proposal_id", "task_grant_request_id"} {
		if !containsRetiredTaskScopeConfirmation(map[string]any{key: nil}) {
			t.Errorf("%q should be detected", key)
		}
	}
	if containsRetiredTaskScopeConfirmation(map[string]any{"other": 1}) {
		t.Error("unknown key should not be detected")
	}
	if containsRetiredTaskScopeConfirmation(map[string]any{}) {
		t.Error("empty should not be detected")
	}
}
