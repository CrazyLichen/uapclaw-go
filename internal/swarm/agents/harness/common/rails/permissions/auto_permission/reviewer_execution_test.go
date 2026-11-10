package auto_permission

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions"
)

// execFacts 构造执行编排用例的 facts。
func execFacts() permissions.ToolDecisionFacts {
	return permissions.ToolDecisionFacts{
		Capability: permissions.ToolCapability{
			ToolName: "bash",
			Category: "shell",
			RiskTier: "high",
			// 结构性人工原因的其余前置：来源可信、操作族已知
			// （否则 structuralManualReason 会返回 facts_source_unverified / unknown_operation，
			// 路由落到 manual_only，allow_once 会被策略拒绝）
			FactsSource:     "host_static",
			OperationFamily: "shell_command",
		},
		WorkspaceRoot: "/ws",
		UntrustedArgs: map[string]any{"command": "ls"},
		// 结构性人工原因的前置：参数是合法对象、访问点已知
		// （否则 structuralManualReason 会返回 arguments_not_object / core_accesses_unknown）
		ArgumentsValidObject: true,
		AccessesKnown:        true,
	}
}

// execOpts 构造默认执行选项。
func execOpts(policyLevel string) reviewerExecutionOptions {
	return reviewerExecutionOptions{
		PolicyLevel:  policyLevel,
		PolicyReason: "test_reason",
		PolicySource: defaultPolicySource,
		SessionID:    "s1",
		RequestID:    "r1",
		ToolCallID:   "c1",
		ChannelKind:  "web",
	}
}

// TestMaybeRunReviewer_硬拦截 验证 hard_guard 直接拒绝。
func TestMaybeRunReviewer_硬拦截(t *testing.T) {
	rail := newBridgeRail(t)
	opts := execOpts(permissions.AutoDecisionDenyLevel)
	result, _ := rail.maybeRunReviewer(context.Background(), execFacts(), opts)

	if !result.Handled {
		t.Fatal("hard block should be handled")
	}
	if result.DecisionSource != "hard_guard" {
		t.Errorf("decision_source = %q", result.DecisionSource)
	}
	if result.Result == nil {
		t.Error("should return a denied response")
	}
}

// TestMaybeRunReviewer_failClosed转人工 验证策略不可用转人工。
func TestMaybeRunReviewer_failClosed转人工(t *testing.T) {
	rail := newBridgeRail(t)
	opts := execOpts(permissions.AutoDecisionAskLevel)
	opts.PolicySource = policySourceFailClosed
	opts.PolicyReason = "policy_unavailable"
	result, _ := rail.maybeRunReviewer(context.Background(), execFacts(), opts)

	if !result.Handled {
		t.Fatal("fail_closed should be handled")
	}
	if result.DecisionSource != "policy_evaluator" {
		t.Errorf("decision_source = %q", result.DecisionSource)
	}
}

// TestMaybeRunReviewer_审查器不可用转人工 验证 nil reviewer 兜底。
func TestMaybeRunReviewer_审查器不可用转人工(t *testing.T) {
	rail := newBridgeRail(t)
	opts := execOpts(permissions.AutoDecisionAskLevel)
	result, _ := rail.maybeRunReviewer(context.Background(), execFacts(), opts)

	if !result.Handled {
		t.Fatal("reviewer unavailable should be handled")
	}
	if result.DecisionSource != reviewerUnavailableDecisionSource {
		t.Errorf("decision_source = %q", result.DecisionSource)
	}
}

// TestMaybeRunReviewer_审查器不可用且用户放行 验证 allow_once 通过。
func TestMaybeRunReviewer_审查器不可用且用户放行(t *testing.T) {
	rail := newBridgeRail(t)
	opts := execOpts(permissions.AutoDecisionAskLevel)
	opts.UserInput = map[string]any{"approved": true}
	result, _ := rail.maybeRunReviewer(context.Background(), execFacts(), opts)

	if !result.Handled {
		t.Fatal("should be handled")
	}
	if result.DecisionSource != manualApprovalDecisionSource {
		t.Errorf("decision_source = %q", result.DecisionSource)
	}
	if result.Result != nil {
		t.Errorf("result should be nil, got %v", result.Result)
	}
}

// TestMaybeRunReviewer_确定性放行 验证 deterministic 分支。
func TestMaybeRunReviewer_确定性放行(t *testing.T) {
	rail := newBridgeRail(t)
	facts := execFacts()
	facts.Capability.ToolName = "web_fetch"
	facts.Capability.Category = "network"
	// allow 档位 + 无原始意图 → 路由可能进入确定性放行或审查器
	opts := execOpts(permissions.AutoDecisionAllowLevel)
	result, _ := rail.maybeRunReviewer(context.Background(), facts, opts)
	if result == nil {
		t.Fatal("should return a result")
	}
}

// TestMaybeRunReviewer_默认值填充 验证空选项的默认值。
func TestMaybeRunReviewer_默认值填充(t *testing.T) {
	rail := newBridgeRail(t)
	// 全空选项：PolicySource / GuardResult / ChannelKind 都走默认
	result, _ := rail.maybeRunReviewer(context.Background(), execFacts(), reviewerExecutionOptions{
		PolicyLevel: permissions.AutoDecisionAskLevel,
	})
	if result == nil {
		t.Fatal("should return a result with defaults")
	}
}

// TestResolveManualHandoff_failClosed 验证优先级最高。
func TestResolveManualHandoff_failClosed(t *testing.T) {
	got := resolveManualHandoff(context.Background(),
		permissions.DecisionRoute{}, policySourceFailClosed, "unavailable")
	if got == nil || got.Source != "policy_evaluator" {
		t.Errorf("got %+v", got)
	}
}

// TestResolveManualHandoff_执行器契约 验证第二优先级。
func TestResolveManualHandoff_执行器契约(t *testing.T) {
	route := permissions.DecisionRoute{
		Reason: permissions.ExecutionProviderContractUnverified,
		Source: permissions.ManualReviewSource,
	}
	got := resolveManualHandoff(context.Background(), route, defaultPolicySource, "")
	if got == nil || got.Source != executionProviderContractDecisionSource {
		t.Errorf("got %+v", got)
	}
}

// TestResolveManualHandoff_无需转交 验证返回 nil。
func TestResolveManualHandoff_无需转交(t *testing.T) {
	got := resolveManualHandoff(context.Background(),
		permissions.DecisionRoute{Source: permissions.SemanticReviewSource},
		defaultPolicySource, "")
	if got != nil {
		t.Errorf("got %+v, want nil", got)
	}
}

// ──────────────────────────── 审查器分支（fake client） ────────────────────────────

// fakeReviewerClient 返回固定 JSON 的审查器客户端替身。
type fakeReviewerClient struct {
	// response 返回的 JSON 文本
	response string
	// err 注入错误
	err error
}

// Assess 实现 ReviewerClient
func (f *fakeReviewerClient) Assess(context.Context, *permissions.ReviewerActionView) (any, error) {
	return f.response, f.err
}

// newReviewerRail 构造带 fake 审查器的 rail。
func newReviewerRail(t *testing.T, response string) *AutoPermissionRail {
	t.Helper()
	rail, err := NewAutoPermissionRail(AutoPermissionRailOptions{
		BaseRail:         &fakePermissionRail{},
		PermissionConfig: map[string]any{},
		AutoReviewer: &permissions.AutoReviewer{
			Client:         &fakeReviewerClient{response: response},
			TimeoutSeconds: 5,
		},
	})
	if err != nil {
		t.Fatalf("construct rail: %v", err)
	}
	return rail
}

// TestMaybeRunReviewer_审查器放行 验证 allow_once 结局。
func TestMaybeRunReviewer_审查器放行(t *testing.T) {
	rail := newReviewerRail(t,
		`{"outcome":"allow_once","rationale":"safe command","reason_code":"safe_readonly"}`)
	result, _ := rail.maybeRunReviewer(context.Background(), execFacts(),
		execOpts(permissions.AutoDecisionAskLevel))

	if !result.Handled {
		t.Fatal("should be handled")
	}
	if result.DecisionSource != "auto_reviewer" {
		t.Errorf("decision_source = %q", result.DecisionSource)
	}
	if result.Result != nil {
		t.Errorf("allow_once should return nil result, got %v", result.Result)
	}
}

// TestMaybeRunReviewer_审查器放行且用户确认 验证 decision_source 变 manual_approval。
func TestMaybeRunReviewer_审查器放行且用户确认(t *testing.T) {
	rail := newReviewerRail(t,
		`{"outcome":"allow_once","rationale":"ok","reason_code":"safe"}`)
	opts := execOpts(permissions.AutoDecisionAskLevel)
	opts.UserInput = map[string]any{"approved": true}
	result, _ := rail.maybeRunReviewer(context.Background(), execFacts(), opts)

	if result.DecisionSource != manualApprovalDecisionSource {
		t.Errorf("decision_source = %q, want manual_approval", result.DecisionSource)
	}
}

// TestMaybeRunReviewer_审查器拒绝 验证 deny 结局。
func TestMaybeRunReviewer_审查器拒绝(t *testing.T) {
	rail := newReviewerRail(t,
		`{"outcome":"deny","rationale":"destructive","reason_code":"dangerous"}`)
	result, _ := rail.maybeRunReviewer(context.Background(), execFacts(),
		execOpts(permissions.AutoDecisionAskLevel))

	if !result.Handled {
		t.Fatal("should be handled")
	}
	if result.DecisionSource != "auto_reviewer" {
		t.Errorf("decision_source = %q", result.DecisionSource)
	}
	if result.Result == nil {
		t.Error("deny should return a response")
	}
}

// TestMaybeRunReviewer_审查器转人工 验证 manual 结局。
func TestMaybeRunReviewer_审查器转人工(t *testing.T) {
	rail := newReviewerRail(t,
		`{"outcome":"manual","rationale":"unclear","reason_code":"needs_review"}`)
	result, _ := rail.maybeRunReviewer(context.Background(), execFacts(),
		execOpts(permissions.AutoDecisionAskLevel))

	if !result.Handled {
		t.Fatal("should be handled")
	}
	if result.Result == nil {
		t.Error("manual should return a prompt response")
	}
}

// TestMaybeRunReviewer_审查器转人工且用户确认 验证用户放行分支。
func TestMaybeRunReviewer_审查器转人工且用户确认(t *testing.T) {
	rail := newReviewerRail(t,
		`{"outcome":"manual","rationale":"unclear","reason_code":"needs_review"}`)
	opts := execOpts(permissions.AutoDecisionAskLevel)
	opts.UserInput = map[string]any{"approved": true}
	result, _ := rail.maybeRunReviewer(context.Background(), execFacts(), opts)

	if result.DecisionSource != manualApprovalDecisionSource {
		t.Errorf("decision_source = %q", result.DecisionSource)
	}
	if result.Result != nil {
		t.Errorf("result should be nil, got %v", result.Result)
	}
}

// TestMaybeRunReviewer_审查器异常 验证 client 报错走 fallback。
func TestMaybeRunReviewer_审查器异常(t *testing.T) {
	rail, err := NewAutoPermissionRail(AutoPermissionRailOptions{
		BaseRail:         &fakePermissionRail{},
		PermissionConfig: map[string]any{},
		AutoReviewer: &permissions.AutoReviewer{
			Client:         &fakeReviewerClient{err: context.DeadlineExceeded},
			TimeoutSeconds: 5,
		},
	})
	if err != nil {
		t.Fatalf("construct rail: %v", err)
	}
	result, _ := rail.maybeRunReviewer(context.Background(), execFacts(),
		execOpts(permissions.AutoDecisionAskLevel))
	if !result.Handled {
		t.Fatal("timeout should still be handled")
	}
}

// TestMaybeRunReviewer_sendFile无审查器也被契约拦截 验证拦截优先级。
func TestMaybeRunReviewer_sendFile无审查器也被契约拦截(t *testing.T) {
	rail := newBridgeRail(t) // 无审查器
	facts := execFacts()
	facts.Capability.ToolName = sendFileToolNameForGrant
	facts.ReadPaths = nil
	opts := execOpts(permissions.AutoDecisionAskLevel)
	opts.UserInput = map[string]any{"approved": true}

	result, _ := rail.maybeRunReviewer(context.Background(), facts, opts)
	if result.DecisionSource != executionProviderContractDecisionSource {
		t.Errorf("decision_source = %q", result.DecisionSource)
	}
}

// TestMaybeRunReviewer_sendFile被契约检查提前拦截 验证执行器契约优先于授权签发。
//
// send_file_to_user 的 NoAutoAllowReason 恒为 execution_provider_contract_unverified
// （宿主无法验证其执行 provider），因此在第 ③ 步就转人工，走不到第 ⑨ 步的
// 授权签发。这是上游设计，不是缺陷。
func TestMaybeRunReviewer_sendFile被契约检查提前拦截(t *testing.T) {
	rail := newReviewerRail(t,
		`{"outcome":"manual","rationale":"unclear","reason_code":"needs_review"}`)
	facts := execFacts()
	facts.Capability.ToolName = sendFileToolNameForGrant
	facts.ReadPaths = nil
	opts := execOpts(permissions.AutoDecisionAskLevel)
	opts.UserInput = map[string]any{"approved": true}

	result, _ := rail.maybeRunReviewer(context.Background(), facts, opts)
	if result.DecisionSource != executionProviderContractDecisionSource {
		t.Errorf("decision_source = %q, want execution_provider_contract", result.DecisionSource)
	}
}

// TestResolveManualHandoff_意图契约 验证第三优先级（意图窗口超限）。
func TestResolveManualHandoff_意图契约(t *testing.T) {
	decisionCtx, _ := permissions.BindRootDecisionContext(
		context.Background(),
		&permissions.RootDecisionContext{AutoReviewBlockReason: "intent_window_exceeded"},
	)
	got := resolveManualHandoff(decisionCtx,
		permissions.DecisionRoute{Source: permissions.SemanticReviewSource},
		defaultPolicySource, "")
	if got == nil {
		t.Fatal("should return intent_contract handoff")
	}
	if got.Source != intentContractDecisionSource {
		t.Errorf("source = %q", got.Source)
	}
	if got.Reason != "intent_window_exceeded" {
		t.Errorf("reason = %q", got.Reason)
	}
}

// TestResolveManualHandoff_空阻断原因 验证阻断原因为空时不转人工。
func TestResolveManualHandoff_空阻断原因(t *testing.T) {
	decisionCtx, _ := permissions.BindRootDecisionContext(
		context.Background(), &permissions.RootDecisionContext{})
	got := resolveManualHandoff(decisionCtx,
		permissions.DecisionRoute{Source: permissions.SemanticReviewSource},
		defaultPolicySource, "")
	if got != nil {
		t.Errorf("got %+v, want nil", got)
	}
}
