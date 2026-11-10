package auto_permission

import (
	"context"
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions"
)

// ──────────────────────────── 用例：allowOnceReviewerGateDecision ───────────────────────────────

// TestAllowOnceReviewerGateDecision_无审查器 验证返回 (false, nil)。
func TestAllowOnceReviewerGateDecision_无审查器(t *testing.T) {
	rail := newBridgeRail(t) // 无 auto_reviewer
	allow, response := rail.allowOnceReviewerGateDecision(
		execFacts(), permissions.AutoDecisionAskLevel, nil, nil)
	if allow || response != nil {
		t.Errorf("got (%v, %v), want (false, nil)", allow, response)
	}
}

// TestAllowOnceReviewerGateDecision_硬拦截 验证拒绝响应。
func TestAllowOnceReviewerGateDecision_硬拦截(t *testing.T) {
	rail := newReviewerRail(t, `{"outcome":"allow_once","rationale":"ok","reason_code":"safe"}`)
	allow, response := rail.allowOnceReviewerGateDecision(
		execFacts(), permissions.AutoDecisionDenyLevel, nil, nil)
	if !allow {
		t.Error("hard block should return allow=true（表示已给出结论）")
	}
	if response == nil {
		t.Error("hard block should return a denied response")
	}
}

// TestAllowOnceReviewerGateDecision_领域路由要求人工 验证交回人工路径。
func TestAllowOnceReviewerGateDecision_领域路由要求人工(t *testing.T) {
	rail := newReviewerRail(t, `{"outcome":"allow_once","rationale":"ok","reason_code":"safe"}`)
	domainRoute := &permissions.DecisionRoute{
		Level:  "ask",
		Reason: "domain_needs_manual",
		Source: permissions.ManualReviewSource,
	}
	allow, response := rail.allowOnceReviewerGateDecision(
		execFacts(), permissions.AutoDecisionAskLevel, nil, domainRoute)
	if allow || response != nil {
		t.Errorf("got (%v, %v), want (false, nil)", allow, response)
	}
}

// TestAllowOnceReviewerGateDecision_通过 验证闸门放行。
func TestAllowOnceReviewerGateDecision_通过(t *testing.T) {
	rail := newReviewerRail(t, `{"outcome":"allow_once","rationale":"ok","reason_code":"safe"}`)
	allow, response := rail.allowOnceReviewerGateDecision(
		execFacts(), permissions.AutoDecisionAskLevel, nil, nil)
	if !allow {
		t.Error("should pass the gate")
	}
	if response != nil {
		t.Errorf("response should be nil, got %v", response)
	}
}

// ──────────────────────────── 用例：sessionAutoConfirmEligible ───────────────────────────────

// TestSessionAutoConfirmEligible_无会话 验证 nil 会话直接 false。
func TestSessionAutoConfirmEligible_无会话(t *testing.T) {
	rail := newBridgeRail(t)
	invocation := ToolInvocation{ToolName: "bash", ToolArgs: map[string]any{}}
	if rail.sessionAutoConfirmEligible(context.Background(), execFacts(), invocation, nil) {
		t.Error("nil session should not be eligible")
	}
}

// TestSessionAutoConfirmEligible_外部发送副作用 验证 external_send 排除。
func TestSessionAutoConfirmEligible_外部发送副作用(t *testing.T) {
	rail := newBridgeRail(t)
	facts := execFacts()
	facts.Capability.StaticSideEffects = map[string]struct{}{externalSendSideEffect: {}}
	invocation := ToolInvocation{ToolName: "bash", ToolArgs: map[string]any{}}
	if rail.sessionAutoConfirmEligible(context.Background(), facts, invocation, nil) {
		t.Error("external_send should be excluded")
	}
}

// ──────────────────────────── 用例：isAutoConfirmed ───────────────────────────────

// TestIsAutoConfirmed 验证严格判定。
func TestIsAutoConfirmed(t *testing.T) {
	cases := []struct {
		name   string
		config map[string]any
		want   bool
	}{
		{"缺失", map[string]any{}, false},
		{"true", map[string]any{"k": true}, true},
		{"false", map[string]any{"k": false}, false},
		{"字符串true不算", map[string]any{"k": "true"}, false},
		{"数字1不算", map[string]any{"k": 1}, false},
		{"nil不算", map[string]any{"k": nil}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isAutoConfirmed(c.config, "k"); got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

// ──────────────────────────── 用例：consumeSessionAutoConfirm ───────────────────────────────

// TestConsumeSessionAutoConfirm_有用户输入 验证直接未处理。
func TestConsumeSessionAutoConfirm_有用户输入(t *testing.T) {
	rail := newBridgeRail(t)
	invocation := ToolInvocation{ToolName: "bash", ToolArgs: map[string]any{}}
	got := rail.consumeSessionAutoConfirm(context.Background(), execFacts(),
		invocation, map[string]any{"approved": true}, nil)
	if got.Handled {
		t.Error("user_input present should not be handled")
	}
}

// TestConsumeSessionAutoConfirm_无会话 验证未处理。
func TestConsumeSessionAutoConfirm_无会话(t *testing.T) {
	rail := newBridgeRail(t)
	invocation := ToolInvocation{ToolName: "bash", ToolArgs: map[string]any{}}
	got := rail.consumeSessionAutoConfirm(context.Background(), execFacts(),
		invocation, nil, nil)
	if got.Handled {
		t.Error("nil session should not be handled")
	}
}

// TestNotHandledResult 验证未处理结果。
func TestNotHandledResult(t *testing.T) {
	got := notHandledResult()
	if got.Handled {
		t.Error("should be unhandled")
	}
	if got.DecisionSource != "" {
		t.Errorf("decision_source = %q", got.DecisionSource)
	}
}

// ──────────────────────────── 带 session 的路径 ────────────────────────────

// fakeAutoConfirmRail 在 fakePermissionRail 基础上提供 AutoConfirmKey 能力。
type fakeAutoConfirmRail struct {
	*fakePermissionRail
	// key 返回的自动确认键（空串表示无键）
	key string
}

// AutoConfirmKey 实现自动确认键提供者。
func (f *fakeAutoConfirmRail) AutoConfirmKey(any) string { return f.key }

// newConsumeRail 构造带 session 与自动确认键的 rail。
func newConsumeRail(t *testing.T, key string, autoConfirmConfig map[string]any) (*AutoPermissionRail, *agentinterfaces.AgentCallbackContext) {
	t.Helper()
	rail, err := NewAutoPermissionRail(AutoPermissionRailOptions{
		BaseRail:         &fakeAutoConfirmRail{fakePermissionRail: &fakePermissionRail{}, key: key},
		PermissionConfig: map[string]any{},
	})
	if err != nil {
		t.Fatalf("construct rail: %v", err)
	}
	sess := session.NewSession()
	if autoConfirmConfig != nil {
		sess.UpdateState(map[string]any{
			saschema.InterruptAutoConfirmKey: autoConfirmConfig,
		})
	}
	inputs := &agentinterfaces.ToolCallInputs{
		ToolCall: &llmschema.ToolCall{ID: "c1", Name: "bash"},
		ToolName: "bash",
	}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, sess)
	return rail, cbc
}

// TestSessionAutoConfirmEligible_全条件满足 验证返回 true。
func TestSessionAutoConfirmEligible_全条件满足(t *testing.T) {
	rail, cbc := newConsumeRail(t, "key1", nil)
	invocation := ToolInvocation{
		Ctx:      cbc,
		ToolName: "bash",
		ToolCall: &llmschema.ToolCall{ID: "c1", Name: "bash"},
	}
	if !rail.sessionAutoConfirmEligible(context.Background(), execFacts(), invocation, nil) {
		t.Error("all conditions met should be eligible")
	}
}

// TestSessionAutoConfirmEligible_各排除条件 验证 9 个条件逐一否决。
func TestSessionAutoConfirmEligible_各排除条件(t *testing.T) {
	invocationOf := func(cbc *agentinterfaces.AgentCallbackContext) ToolInvocation {
		return ToolInvocation{
			Ctx:      cbc,
			ToolName: "bash",
			ToolCall: &llmschema.ToolCall{ID: "c1", Name: "bash"},
		}
	}

	t.Run("无自动确认键", func(t *testing.T) {
		rail, cbc := newConsumeRail(t, "", nil)
		if rail.sessionAutoConfirmEligible(context.Background(), execFacts(), invocationOf(cbc), nil) {
			t.Error("empty key should not be eligible")
		}
	})
	t.Run("参数非对象", func(t *testing.T) {
		rail, cbc := newConsumeRail(t, "key1", nil)
		facts := execFacts()
		facts.ArgumentsValidObject = false
		if rail.sessionAutoConfirmEligible(context.Background(), facts, invocationOf(cbc), nil) {
			t.Error("invalid arguments should not be eligible")
		}
	})
	t.Run("来源不可信", func(t *testing.T) {
		rail, cbc := newConsumeRail(t, "key1", nil)
		facts := execFacts()
		facts.Capability.FactsSource = "unverified"
		if rail.sessionAutoConfirmEligible(context.Background(), facts, invocationOf(cbc), nil) {
			t.Error("untrusted source should not be eligible")
		}
	})
	t.Run("别名冲突", func(t *testing.T) {
		rail, cbc := newConsumeRail(t, "key1", nil)
		facts := execFacts()
		facts.Capability.AliasConflict = true
		if rail.sessionAutoConfirmEligible(context.Background(), facts, invocationOf(cbc), nil) {
			t.Error("alias conflict should not be eligible")
		}
	})
	t.Run("访问点未知", func(t *testing.T) {
		rail, cbc := newConsumeRail(t, "key1", nil)
		facts := execFacts()
		facts.AccessesKnown = false
		if rail.sessionAutoConfirmEligible(context.Background(), facts, invocationOf(cbc), nil) {
			t.Error("unknown accesses should not be eligible")
		}
	})
	t.Run("领域路由要求人工", func(t *testing.T) {
		rail, cbc := newConsumeRail(t, "key1", nil)
		domainRoute := &permissions.DecisionRoute{Source: permissions.ManualReviewSource}
		if rail.sessionAutoConfirmEligible(context.Background(), execFacts(), invocationOf(cbc), domainRoute) {
			t.Error("manual domain route should not be eligible")
		}
	})
	t.Run("意图窗口阻断", func(t *testing.T) {
		rail, cbc := newConsumeRail(t, "key1", nil)
		decisionCtx, _ := permissions.BindRootDecisionContext(context.Background(),
			&permissions.RootDecisionContext{AutoReviewBlockReason: "too_long"})
		if rail.sessionAutoConfirmEligible(decisionCtx, execFacts(), invocationOf(cbc), nil) {
			t.Error("blocked intent window should not be eligible")
		}
	})
}

// TestConsumeSessionAutoConfirm_成功消费 验证完整成功路径。
func TestConsumeSessionAutoConfirm_成功消费(t *testing.T) {
	rail, cbc := newConsumeRail(t, "key1", map[string]any{"key1": true})
	invocation := ToolInvocation{
		Ctx:      cbc,
		ToolName: "bash",
		ToolCall: &llmschema.ToolCall{ID: "c1", Name: "bash"},
	}
	got := rail.consumeSessionAutoConfirm(context.Background(), execFacts(), invocation, nil, nil)
	if !got.Handled {
		t.Fatal("should be handled")
	}
	if got.DecisionSource != sessionAutoConfirmDecisionSource {
		t.Errorf("decision_source = %q", got.DecisionSource)
	}
}

// TestConsumeSessionAutoConfirm_配置未标记 验证未确认时不放行。
func TestConsumeSessionAutoConfirm_配置未标记(t *testing.T) {
	rail, cbc := newConsumeRail(t, "key1", map[string]any{"other": true})
	invocation := ToolInvocation{
		Ctx:      cbc,
		ToolName: "bash",
		ToolCall: &llmschema.ToolCall{ID: "c1", Name: "bash"},
	}
	got := rail.consumeSessionAutoConfirm(context.Background(), execFacts(), invocation, nil, nil)
	if got.Handled {
		t.Error("unmarked config should not be handled")
	}
}

// TestConsumeSessionAutoConfirm_无session状态 验证 session 无该状态时不放行。
func TestConsumeSessionAutoConfirm_无session状态(t *testing.T) {
	rail, cbc := newConsumeRail(t, "key1", nil)
	invocation := ToolInvocation{
		Ctx:      cbc,
		ToolName: "bash",
		ToolCall: &llmschema.ToolCall{ID: "c1", Name: "bash"},
	}
	got := rail.consumeSessionAutoConfirm(context.Background(), execFacts(), invocation, nil, nil)
	if got.Handled {
		t.Error("no session state should not be handled")
	}
}
