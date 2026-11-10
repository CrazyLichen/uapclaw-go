package auto_permission

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions"
)

// TestDomainRouteMetadata_固定字段 验证 10 个字段。
func TestDomainRouteMetadata_固定字段(t *testing.T) {
	route := &permissions.DecisionRoute{Reason: "domain_deny", Source: "domain_policy"}
	got := domainRouteMetadata(execFacts(), route, domainFinalStatusDenied)

	expected := map[string]any{
		"decision_source":       hostRouteDecisionSource,
		"host_route_reason":     "domain_deny",
		"host_route_source":     "domain_policy",
		"final_reviewer_status": domainFinalStatusDenied,
		"hide_always_allow":     true,
		"manual_reason_code":    "domain_deny",
		"permission_status":     domainFinalStatusDenied,
		"reviewer_status":       domainFinalStatusDenied,
		"risk_tier":             "high",
	}
	for key, want := range expected {
		if got[key] != want {
			t.Errorf("%s = %v, want %v", key, got[key], want)
		}
	}
	if got["action_summary"] == nil {
		t.Error("action_summary should be set")
	}
}

// TestDomainRouteMetadata_nil路由 验证 nil 安全。
func TestDomainRouteMetadata_nil路由(t *testing.T) {
	got := domainRouteMetadata(execFacts(), nil, domainFinalStatusManual)
	if got["host_route_reason"] != "" {
		t.Errorf("host_route_reason = %v", got["host_route_reason"])
	}
}

// TestDenyDomainRoute 验证拒绝响应结构。
func TestDenyDomainRoute(t *testing.T) {
	rail := newBridgeRail(t)
	route := &permissions.DecisionRoute{Reason: "domain_deny", Source: "domain_policy"}
	got := rail.denyDomainRoute(execFacts(), route)

	if got["status"] != "denied" {
		t.Errorf("status = %v", got["status"])
	}
	if got["reason"] != "domain_deny" {
		t.Errorf("reason = %v", got["reason"])
	}
	if got["reviewer_metadata"] == nil {
		t.Error("reviewer_metadata should be set")
	}
}

// TestManualDomainRouteResponse 验证人工响应结构。
func TestManualDomainRouteResponse(t *testing.T) {
	rail := newBridgeRail(t)
	route := &permissions.DecisionRoute{Reason: "domain_manual", Source: "domain_policy"}
	got := rail.manualDomainRouteResponse(execFacts(), route)
	if got == nil {
		t.Fatal("response should not be nil")
	}
}

// TestDeny 验证通用拒绝响应。
func TestDeny(t *testing.T) {
	rail := newBridgeRail(t)
	got := rail.deny(execFacts(), "custom_reason", "custom_source")

	if got["status"] != "denied" {
		t.Errorf("status = %v", got["status"])
	}
	if got["reason"] != "custom_reason" {
		t.Errorf("reason = %v", got["reason"])
	}
	metadata, _ := got["reviewer_metadata"].(map[string]any)
	if metadata["decision_source"] != "custom_source" {
		t.Errorf("decision_source = %v", metadata["decision_source"])
	}
}

// TestCurrentSandboxProfile_nilSysOperation 验证返回缓存。
func TestCurrentSandboxProfile_nilSysOperation(t *testing.T) {
	rail := newBridgeRail(t)
	rail.sandbox = nil
	if got := rail.currentSandboxProfile(); got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

// TestCurrentSandboxProfile_有SysOperation 验证重建并缓存。
func TestCurrentSandboxProfile_有SysOperation(t *testing.T) {
	rail := newBridgeRail(t)
	rail.sysOperation = struct{}{}
	got := rail.currentSandboxProfile()
	if got == nil {
		t.Fatal("should build a profile")
	}
	if rail.sandbox != got {
		t.Error("should cache into r.sandbox")
	}
}

// TestEvaluateDomainRoute 验证调用不 panic。
func TestEvaluateDomainRoute(t *testing.T) {
	rail := newBridgeRail(t)
	_ = rail.evaluateDomainRoute(execFacts(), nil, nil)
}

// TestHandleDomainRoute_非审查器路由 验证落到人工。
func TestHandleDomainRoute_非审查器路由(t *testing.T) {
	rail := newBridgeRail(t)
	route := &permissions.DecisionRoute{
		Level:  "ask",
		Reason: "domain_manual",
		Source: permissions.ManualReviewSource,
	}
	result, _ := rail.handleDomainRoute(context.Background(), execFacts(),
		handleDomainRouteOptions{DomainRoute: route})
	if !result.Handled {
		t.Fatal("should be handled")
	}
	if result.DecisionSource != domainPolicyDecisionSource {
		t.Errorf("decision_source = %q", result.DecisionSource)
	}
}

// TestHandleDomainRoute_审查器路由 验证转发给 maybeRunReviewer。
func TestHandleDomainRoute_审查器路由(t *testing.T) {
	rail := newReviewerRail(t, `{"outcome":"deny","rationale":"bad","reason_code":"unsafe"}`)
	route := &permissions.DecisionRoute{
		Level:  "ask",
		Reason: "domain_review",
		Source: permissions.SemanticReviewSource,
	}
	result, _ := rail.handleDomainRoute(context.Background(), execFacts(),
		handleDomainRouteOptions{DomainRoute: route})
	if !result.Handled {
		t.Fatal("should be handled")
	}
	// 审查器给出 deny → decision_source 应为 auto_reviewer
	if result.DecisionSource != "auto_reviewer" {
		t.Errorf("decision_source = %q", result.DecisionSource)
	}
}

// TestRouteReason 验证 nil 安全。
func TestRouteReason(t *testing.T) {
	if got := routeReason(nil); got != "" {
		t.Errorf("nil -> %q", got)
	}
	if got := routeReason(&permissions.DecisionRoute{Reason: "r"}); got != "r" {
		t.Errorf("got %q", got)
	}
}
