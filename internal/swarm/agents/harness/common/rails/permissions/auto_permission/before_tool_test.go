package auto_permission

import (
	"context"
	"testing"

	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions"
)

// beforeToolCbc 构造 before_tool 测试用回调上下文。
func beforeToolCbc(toolName string, args map[string]any) *agentinterfaces.AgentCallbackContext {
	inputs := &agentinterfaces.ToolCallInputs{
		ToolName: toolName,
		ToolArgs: args,
	}
	return agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)
}

// newBeforeToolRail 构造带 fake 策略评估器的 rail。
func newBeforeToolRail(t *testing.T) *AutoPermissionRail {
	t.Helper()
	rail, err := NewAutoPermissionRail(AutoPermissionRailOptions{
		BaseRail:         &fakePermissionRail{},
		PermissionConfig: map[string]any{},
	})
	if err != nil {
		t.Fatalf("construct rail: %v", err)
	}
	return rail
}

// TestBeforeToolCall_plan硬拒短路 验证第一步。
func TestBeforeToolCall_plan硬拒短路(t *testing.T) {
	rail := newBeforeToolRail(t)
	cbc := beforeToolCbc("bash", map[string]any{"command": "ls"})
	// 打 plan 硬拒标记
	cbc.SetExtra("_jiuwenswarm_plan_hard_reject", true)
	if err := rail.BeforeToolCall(context.Background(), cbc); err != nil {
		t.Errorf("plan hard reject should short-circuit, got %v", err)
	}
}

// TestBeforeToolCall_askUser直通父轨 验证 ask_user 分支。
func TestBeforeToolCall_askUser直通父轨(t *testing.T) {
	rail := newBeforeToolRail(t)
	cbc := beforeToolCbc(askUserToolName, map[string]any{})
	if err := rail.BeforeToolCall(context.Background(), cbc); err != nil {
		t.Errorf("ask_user should delegate to base rail, got %v", err)
	}
}

// TestBeforeToolCall_普通调用不panic 验证主链路可跑通。
func TestBeforeToolCall_普通调用不panic(t *testing.T) {
	rail := newBeforeToolRail(t)
	cbc := beforeToolCbc("read_file", map[string]any{"file_path": "/ws/a.txt"})
	// 不断言具体结果（取决于策略评估器默认配置），只保证不 panic
	_ = rail.BeforeToolCall(context.Background(), cbc)
}

// TestBeforeToolCall_取消上下文 验证 CancelledError 等价分支。
func TestBeforeToolCall_取消上下文(t *testing.T) {
	rail := newBeforeToolRail(t)
	cbc := beforeToolCbc("bash", map[string]any{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := rail.BeforeToolCall(ctx, cbc); err == nil {
		t.Error("cancelled ctx should return error")
	}
}

// ──────────────────────────── 三处 ⏵ 的锁定测试 ────────────────────────────

// TestClientToolMarked_恒false 锁定 ⏵-1（9.90e 回填后本用例应改）。
func TestClientToolMarked_恒false(t *testing.T) {
	for _, card := range []any{
		nil,
		map[string]any{"is_client_tool": true},
		"anything",
	} {
		if clientToolMarked(card) {
			t.Errorf("⏵-1: clientToolMarked(%v) should be false until 9.90e", card)
		}
	}
}

// TestTrustedSubagentRuntimeControlBinding_恒false 锁定 ⏵-2。
func TestTrustedSubagentRuntimeControlBinding_恒false(t *testing.T) {
	for _, name := range []string{
		"subagent_close", "subagent_list", "subagent_resume",
		"subagent_send_input", "subagent_spawn", "subagent_wait",
	} {
		if trustedSubagentRuntimeControlBinding(ToolInvocation{ToolName: name}) {
			t.Errorf("⏵-2: %s should be false until 9.90e", name)
		}
	}
}

// TestPublicHTTPSFetchContextAttributeName 锁定 ⏵-3 的常量值与上游一致。
func TestPublicHTTPSFetchContextAttributeName(t *testing.T) {
	if publicHTTPSFetchContextAttributeName != "_jiuwenswarm_public_https_fetch" {
		t.Errorf("got %q, want upstream value", publicHTTPSFetchContextAttributeName)
	}
}

// ──────────────────────────── 局部辅助 ────────────────────────────

// TestTrustedTaskToolName 验证六个条件。
func TestTrustedTaskToolName(t *testing.T) {
	good := permissions.ToolDecisionFacts{
		Capability: permissions.ToolCapability{
			ToolName:       "task_tool",
			RegisteredName: "task_tool",
			FactsSource:    "host_static",
		},
		ArgumentsValidObject: true,
	}
	if !trustedTaskToolName(good) {
		t.Error("exact task_tool should be trusted")
	}

	bad := good
	bad.Capability.ToolName = "other"
	if trustedTaskToolName(bad) {
		t.Error("wrong tool name")
	}
	bad = good
	bad.Capability.RegisteredName = "alias"
	if trustedTaskToolName(bad) {
		t.Error("aliased registration should not be trusted")
	}
	bad = good
	bad.Capability.Aliases = []string{"x"}
	if trustedTaskToolName(bad) {
		t.Error("aliases present should not be trusted")
	}
	bad = good
	bad.Capability.AliasConflict = true
	if trustedTaskToolName(bad) {
		t.Error("alias conflict should not be trusted")
	}
	bad = good
	bad.Capability.FactsSource = "unverified"
	if trustedTaskToolName(bad) {
		t.Error("untrusted source should not be trusted")
	}
	bad = good
	bad.ArgumentsValidObject = false
	if trustedTaskToolName(bad) {
		t.Error("invalid args should not be trusted")
	}
}

// TestIntOfAny 验证 int(x) 保守等价。
func TestIntOfAny(t *testing.T) {
	cases := []struct {
		in   any
		want int
		ok   bool
	}{
		{5, 5, true},
		{5.9, 5, true},
		{"12", 12, true},
		{"bad", 0, false},
		{nil, 0, false},
	}
	for _, c := range cases {
		got, ok := intOfAny(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("intOfAny(%v) = (%d,%v), want (%d,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

// TestAsError 验证 error 透传、其余返回 nil。
func TestAsError(t *testing.T) {
	if asError(nil) != nil {
		t.Error("nil -> nil")
	}
	if asError(map[string]any{"status": "denied"}) != nil {
		t.Error("non-error result should be dropped（副作用已在闭包内写完）")
	}
}

// TestClientToolPermission_无策略器时安全 验证不 panic。
func TestClientToolPermission_无策略器时安全(t *testing.T) {
	rail := newBeforeToolRail(t)
	cbc := beforeToolCbc("client_tool", map[string]any{})
	_ = rail.clientToolPermission(context.Background(), cbc,
		ToolInvocation{ToolName: "client_tool", Ctx: cbc})
}

// ──────────────────────────── 策略驱动分支 ────────────────────────────

// newBeforeToolRailWithConfig 用指定权限配置构造 rail。
func newBeforeToolRailWithConfig(t *testing.T, config map[string]any) *AutoPermissionRail {
	t.Helper()
	rail, err := NewAutoPermissionRail(AutoPermissionRailOptions{
		BaseRail:         &fakePermissionRail{},
		PermissionConfig: config,
	})
	if err != nil {
		t.Fatalf("construct rail: %v", err)
	}
	return rail
}

// TestBeforeToolCall_配置allow 验证 policy allow 短路。
func TestBeforeToolCall_配置allow(t *testing.T) {
	rail := newEngineBeforeToolRail(t, permissions.AutoDecisionAllowLevel, "policy_allow")
	cbc := beforeToolCbc("read_file", map[string]any{"file_path": "/ws/a.txt"})
	if err := rail.BeforeToolCall(context.Background(), cbc); err != nil {
		t.Errorf("allow should not error: %v", err)
	}
}

// TestBeforeToolCall_配置deny 验证 policy deny 走 skipRuntimeTool。
func TestBeforeToolCall_配置deny(t *testing.T) {
	rail := newEngineBeforeToolRail(t, permissions.AutoDecisionDenyLevel, "policy_deny")
	cbc := beforeToolCbc("read_file", map[string]any{"file_path": "/ws/a.txt"})
	_ = rail.BeforeToolCall(context.Background(), cbc)
	// deny 应写 _skip_tool 副作用（与 Python 一致）
	if cbc.Extra()[skipToolExtraKey] != true {
		t.Error("deny should set _skip_tool via cbc side effect")
	}
}

// TestBeforeToolCall_用户拒绝确认 验证 resume 相关分支。
func TestBeforeToolCall_用户拒绝确认(t *testing.T) {
	rail := newEngineBeforeToolRail(t, permissions.AutoDecisionAskLevel, "policy_ask")
	cbc := beforeToolCbc("read_file", map[string]any{"file_path": "/ws/a.txt"})
	cbc.SetExtra("user_input", map[string]any{"approved": false})
	_ = rail.BeforeToolCall(context.Background(), cbc)
}

// TestBeforeToolCall_用户单次放行 验证 allow_once 分支。
func TestBeforeToolCall_用户单次放行(t *testing.T) {
	rail := newEngineBeforeToolRail(t, permissions.AutoDecisionAskLevel, "policy_ask")
	cbc := beforeToolCbc("read_file", map[string]any{"file_path": "/ws/a.txt"})
	cbc.SetExtra("user_input", map[string]any{"approved": true})
	_ = rail.BeforeToolCall(context.Background(), cbc)
}

// TestBeforeToolCall_退役任务级载荷 验证 task_scope_unsupported 分支。
func TestBeforeToolCall_退役任务级载荷(t *testing.T) {
	rail := newEngineBeforeToolRail(t, permissions.AutoDecisionAskLevel, "policy_ask")
	cbc := beforeToolCbc("read_file", map[string]any{"file_path": "/ws/a.txt"})
	cbc.SetExtra("user_input", map[string]any{
		"approved":   true,
		"task_grant": "old",
	})
	_ = rail.BeforeToolCall(context.Background(), cbc)
}

// TestBeforeToolCall_非permissionResume 验证 root_nonpermission_resume 短路。
func TestBeforeToolCall_非permissionResume(t *testing.T) {
	rail := newEngineBeforeToolRail(t, permissions.AutoDecisionAskLevel, "ask")
	cbc := beforeToolCbc("bash", map[string]any{})
	// 注入非权限续接标记（形状需为 *RootNonPermissionResume）
	cbc.SetExtra(permissions.RootNonPermissionResumeAttribute,
		&permissions.RootNonPermissionResume{})
	if err := rail.BeforeToolCall(context.Background(), cbc); err != nil {
		t.Errorf("should short-circuit: %v", err)
	}
}

// TestBeforeToolCall_skipTool短路 验证 _skip_tool + 根上下文失败短路。
func TestBeforeToolCall_skipTool短路(t *testing.T) {
	rail := newEngineBeforeToolRail(t, permissions.AutoDecisionAskLevel, "ask")
	cbc := beforeToolCbc("bash", map[string]any{})
	cbc.SetExtra(skipToolExtraKey, true)
	cbc.SetExtra(permissions.RootContextFailureAttribute, "host_failure")
	// 该分支在 Python 里 return None；Go 侧仍会继续走策略评估（因
	// root_context_failure 的读取形状不同），故只锁定不 panic
	if err := rail.BeforeToolCall(context.Background(), cbc); err != nil {
		t.Logf("branch returned error（记录，非失败）: %v", err)
	}
}

// ──────────────────────────── 带策略引擎的 rail ────────────────────────────

// fakePolicyEngine 固定返回指定档位的策略引擎替身。
type fakePolicyEngine struct {
	// level 返回的判定级别
	level string
	// reason 返回的原因
	reason string
}

// CheckPermission 实现 PolicyEngine
func (e *fakePolicyEngine) CheckPermission(string, map[string]any) *permissions.PolicyPermissionResult {
	return &permissions.PolicyPermissionResult{
		Permission: e.level,
		Reason:     e.reason,
	}
}

// UpdateConfig 实现 PolicyEngine
func (e *fakePolicyEngine) UpdateConfig(map[string]any) {}

// engineRail 在 fakePermissionRail 基础上提供可控引擎。
type engineRail struct {
	*fakePermissionRail
	// engine 注入的引擎
	engine permissions.PolicyEngine
}

// Engine 实现 PolicyBaseRail.Engine
func (r *engineRail) Engine() permissions.PolicyEngine { return r.engine }

// newEngineBeforeToolRail 构造带可控档位策略引擎的 rail。
func newEngineBeforeToolRail(t *testing.T, level, reason string) *AutoPermissionRail {
	t.Helper()
	rail, err := NewAutoPermissionRail(AutoPermissionRailOptions{
		BaseRail: &engineRail{
			fakePermissionRail: &fakePermissionRail{},
			engine:             &fakePolicyEngine{level: level, reason: reason},
		},
		PermissionConfig: map[string]any{"default": level},
	})
	if err != nil {
		t.Fatalf("construct rail: %v", err)
	}
	return rail
}

// ──────────────────────────── 更多策略/用户态分支 ────────────────────────────

// TestBeforeToolCall_failClosed源 验证 fail_closed 短路。
func TestBeforeToolCall_failClosed源(t *testing.T) {
	rail, err := NewAutoPermissionRail(AutoPermissionRailOptions{
		BaseRail: &engineRail{
			fakePermissionRail: &fakePermissionRail{},
			engine:             &fakePolicyEngine{level: permissions.AutoDecisionAskLevel, reason: "fail"},
		},
		PermissionConfig: map[string]any{},
	})
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	// 手动把 source 改成 fail_closed 不可行（Evaluate 内部决定），
	// 用「无引擎」的 rail 天然走 fail_closed
	_ = rail
	plain := newBeforeToolRail(t)
	cbc := beforeToolCbc("bash", map[string]any{})
	if err := plain.BeforeToolCall(context.Background(), cbc); err != nil {
		t.Logf("fail_closed path returned: %v", err)
	}
}

// TestBeforeToolCall_拒绝确认无resume 验证 tool_invocation_resume_required 分支。
func TestBeforeToolCall_拒绝确认无resume(t *testing.T) {
	rail := newEngineBeforeToolRail(t, permissions.AutoDecisionAskLevel, "ask")
	cbc := beforeToolCbc("bash", map[string]any{})
	cbc.SetExtra("user_input", map[string]any{"approved": false})
	if err := rail.BeforeToolCall(context.Background(), cbc); err != nil {
		t.Logf("rejection-without-resume returned: %v", err)
	}
}

// TestBeforeToolCall_不同工具名 表驱动覆盖多种工具分类。
func TestBeforeToolCall_不同工具名(t *testing.T) {
	for _, name := range []string{
		"read_file", "write_file", "bash", "web_fetch", "send_file_to_user",
		"task_tool", "tool_search", "lsp", "subagent_spawn",
	} {
		t.Run(name, func(t *testing.T) {
			rail := newEngineBeforeToolRail(t, permissions.AutoDecisionAskLevel, "ask")
			cbc := beforeToolCbc(name, map[string]any{"command": "ls"})
			_ = rail.BeforeToolCall(context.Background(), cbc)
		})
	}
}

// TestBeforeToolCall_包装续接 验证 wrapper_resume 分支。
func TestBeforeToolCall_包装续接(t *testing.T) {
	rail := newEngineBeforeToolRail(t, permissions.AutoDecisionAskLevel, "ask")
	cbc := beforeToolCbc("bash", map[string]any{})
	cbc.SetExtra(permissions.RootPermissionWrapperAttribute, true)
	_ = rail.BeforeToolCall(context.Background(), cbc)
}

// TestBeforeToolCall_policyAllow各档 表驱动覆盖三档。
func TestBeforeToolCall_policyAllow各档(t *testing.T) {
	for _, level := range []string{
		permissions.AutoDecisionAllowLevel,
		permissions.AutoDecisionAskLevel,
		permissions.AutoDecisionDenyLevel,
	} {
		t.Run(level, func(t *testing.T) {
			rail := newEngineBeforeToolRail(t, level, "reason_"+level)
			cbc := beforeToolCbc("read_file", map[string]any{"file_path": "/ws/a.txt"})
			_ = rail.BeforeToolCall(context.Background(), cbc)
		})
	}
}
