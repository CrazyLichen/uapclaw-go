package auto_permission

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions"
)

// codeFacts 构造 code 路由用例的 facts。
func codeFacts() permissions.ToolDecisionFacts {
	return permissions.ToolDecisionFacts{
		Capability: permissions.ToolCapability{
			ToolName:        enterPlanModeTool,
			Category:        "control",
			RiskTier:        "low",
			FactsSource:     "host_static",
			OperationFamily: "mode_control",
		},
		WorkspaceRoot:        "/ws",
		ArgumentsValidObject: true,
		AccessesKnown:        true,
	}
}

// ──────────────────────────── 用例：TrustedCodeRoute fail-closed ───────────────────────────────

// TestTrustedCodeRoute_三个控制工具无绑定时拒绝 验证 D4 基础。
func TestTrustedCodeRoute_三个控制工具无绑定时拒绝(t *testing.T) {
	defer SetCodeControlToolTypes(nil)
	SetCodeControlToolTypes(nil)
	for _, name := range []string{enterPlanModeTool, exitPlanModeTool, switchModeTool} {
		invocation := ToolInvocation{ToolName: name}
		if got := TrustedCodeRoute(invocation, codeFacts(), false, nil); got != "" {
			t.Errorf("%s: got %q, want empty（无真实绑定应 fail-closed）", name, got)
		}
	}
}

// TestTrustedCodeRoute_LSP返回false 验证 V-7。
func TestTrustedCodeRoute_LSP返回false(t *testing.T) {
	invocation := ToolInvocation{ToolName: lspTool}
	if got := TrustedCodeRoute(invocation, codeFacts(), false, nil); got != "" {
		t.Errorf("LSP route 未实现（spec V-7），必须返回空串，got %q", got)
	}
}

// TestTrustedCodeRoute_路径需审批时拒绝 验证 path_requires_approval 前置。
func TestTrustedCodeRoute_路径需审批时拒绝(t *testing.T) {
	invocation := ToolInvocation{ToolName: enterPlanModeTool}
	if got := TrustedCodeRoute(invocation, codeFacts(), true, nil); got != "" {
		t.Errorf("got %q", got)
	}
}

// TestTrustedCodeRoute_非闭集工具 验证未知工具名。
func TestTrustedCodeRoute_非闭集工具(t *testing.T) {
	for _, name := range []string{"bash", "read_file", ""} {
		invocation := ToolInvocation{ToolName: name}
		if got := TrustedCodeRoute(invocation, codeFacts(), false, nil); got != "" {
			t.Errorf("%q -> %q", name, got)
		}
	}
}

// TestTrustedCodeRoute_参数非对象 验证 arguments_valid_object 前置。
func TestTrustedCodeRoute_参数非对象(t *testing.T) {
	facts := codeFacts()
	facts.ArgumentsValidObject = false
	invocation := ToolInvocation{ToolName: enterPlanModeTool}
	if got := TrustedCodeRoute(invocation, facts, false, nil); got != "" {
		t.Errorf("got %q", got)
	}
}

// TestTrustedCodeRoute_owner类型不符 验证 owner 校验。
func TestTrustedCodeRoute_owner类型不符(t *testing.T) {
	sess := session.NewSession()
	cbc := agentinterfaces.NewAgentCallbackContext(
		harness.NewDeepAgent(&agentschema.AgentCard{}),
		&agentinterfaces.ToolCallInputs{ToolName: enterPlanModeTool}, sess)
	invocation := ToolInvocation{Ctx: cbc, ToolName: enterPlanModeTool}
	if got := TrustedCodeRoute(invocation, codeFacts(), false, "wrong owner"); got != "" {
		t.Errorf("got %q", got)
	}
}

// ──────────────────────────── 完整成功路径 ────────────────────────────

// TestTrustedCodeRoute_完整成功路径 验证 D4：真实 AgentModeRail + 登记后通过。
func TestTrustedCodeRoute_完整成功路径(t *testing.T) {
	sess := session.NewSession()
	agent := harness.NewDeepAgent(&agentschema.AgentCard{})
	agent.SetUapswarmAdapterMode(codeAdapterMode)
	modeRail := rails.NewAgentModeRail(nil)
	agent.AddRail(modeRail)

	defer SetCodeControlToolTypes(nil)
	SetCodeControlToolTypes(map[string]reflect.Type{
		enterPlanModeTool: reflect.TypeOf((*fakeCodeControlTool)(nil)),
	})

	card := tool.NewToolCardWithID("card-code-1", enterPlanModeTool, "plan", nil, nil)
	toolInstance := &fakeCodeControlTool{card: card}
	agent.AbilityManager().Add(card)
	SetResolveToolByIDFunc(func(id string) tool.Tool {
		if id == "card-code-1" {
			return toolInstance
		}
		return nil
	})

	inputs := &agentinterfaces.ToolCallInputs{
		ToolName: enterPlanModeTool,
		ToolArgs: map[string]any{},
	}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, sess)
	invocation := ToolInvocation{Ctx: cbc, ToolName: enterPlanModeTool, ToolArgs: inputs.ToolArgs}

	got := TrustedCodeRoute(invocation, codeFacts(), false, agent)
	if got != codePlanControlRoute {
		t.Errorf("got %q, want %q", got, codePlanControlRoute)
	}
}

// TestTrustedCodeRoute_适配器模式不符 验证 code 模式校验。
func TestTrustedCodeRoute_适配器模式不符(t *testing.T) {
	sess := session.NewSession()
	agent := harness.NewDeepAgent(&agentschema.AgentCard{})
	agent.AddRail(rails.NewAgentModeRail(nil))
	// 不设置 code 模式
	cbc := agentinterfaces.NewAgentCallbackContext(agent,
		&agentinterfaces.ToolCallInputs{ToolName: enterPlanModeTool}, sess)
	invocation := ToolInvocation{Ctx: cbc, ToolName: enterPlanModeTool}
	if got := TrustedCodeRoute(invocation, codeFacts(), false, agent); got != "" {
		t.Errorf("got %q", got)
	}
}

// TestTrustedCodeRoute_未MarkTrusted 验证 V-4 反例。
func TestTrustedCodeRoute_未MarkTrusted(t *testing.T) {
	sess := session.NewSession()
	agent := harness.NewDeepAgent(&agentschema.AgentCard{})
	agent.SetUapswarmAdapterMode(codeAdapterMode)
	agent.AddRail(&rails.AgentModeRail{}) // 绕过构造函数 → 未 MarkTrusted

	cbc := agentinterfaces.NewAgentCallbackContext(agent,
		&agentinterfaces.ToolCallInputs{ToolName: enterPlanModeTool}, sess)
	invocation := ToolInvocation{Ctx: cbc, ToolName: enterPlanModeTool}
	if got := TrustedCodeRoute(invocation, codeFacts(), false, agent); got != "" {
		t.Errorf("untrusted rail should fail, got %q", got)
	}
}

// ──────────────────────────── 辅助 ────────────────────────────

// TestSetCodeControlToolTypes 验证登记与读取。
func TestSetCodeControlToolTypes(t *testing.T) {
	defer SetCodeControlToolTypes(nil)
	SetCodeControlToolTypes(nil)
	if got := registeredCodeControlToolType(enterPlanModeTool); got != nil {
		t.Errorf("got %v", got)
	}
	typ := reflect.TypeOf((*fakeCodeControlTool)(nil))
	SetCodeControlToolTypes(map[string]reflect.Type{
		enterPlanModeTool: typ,
		"bad":             nil,
	})
	if got := registeredCodeControlToolType(enterPlanModeTool); got != typ {
		t.Errorf("got %v", got)
	}
	if got := registeredCodeControlToolType("bad"); got != nil {
		t.Errorf("nil type should be skipped, got %v", got)
	}
}

// TestSetCodeControlToolTypes_并发安全 验证并发读写。
func TestSetCodeControlToolTypes_并发安全(t *testing.T) {
	defer SetCodeControlToolTypes(nil)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			SetCodeControlToolTypes(map[string]reflect.Type{
				switchModeTool: reflect.TypeOf((*fakeCodeControlTool)(nil)),
			})
			_ = registeredCodeControlToolType(switchModeTool)
		}()
	}
	wg.Wait()
}

// TestRegisterLSPReadSemantics_桩 验证 no-op 不 panic。
func TestRegisterLSPReadSemantics_桩(t *testing.T) {
	RegisterLSPReadSemantics()
}

// TestAgentModeRailType 验证类型常量。
func TestAgentModeRailType(t *testing.T) {
	if agentModeRailType != reflect.TypeOf((*rails.AgentModeRail)(nil)) {
		t.Error("type mismatch")
	}
}

// ──────────────────────────── 测试替身 ────────────────────────────

// fakeCodeControlTool 控制工具替身，实现 tool.Tool 全部方法。
type fakeCodeControlTool struct {
	// card 注册的卡片（ResolveToolBinding 要求指针同一）
	card *tool.ToolCard
}

// Card 实现 tool.Tool
func (f *fakeCodeControlTool) Card() *tool.ToolCard { return f.card }

// Invoke 实现 tool.Tool
func (f *fakeCodeControlTool) Invoke(context.Context, map[string]any, ...tool.ToolOption) (map[string]any, error) {
	return nil, nil
}

// Stream 实现 tool.Tool
func (f *fakeCodeControlTool) Stream(context.Context, map[string]any, ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, nil
}

// GetCallTimeout 实现 tool.Tool
func (f *fakeCodeControlTool) GetCallTimeout(map[string]any, float64) float64 { return 0 }

// TestTrustedCodeRoute_多个AgentModeRail 验证「恰好一个」约束。
func TestTrustedCodeRoute_多个AgentModeRail(t *testing.T) {
	sess := session.NewSession()
	agent := harness.NewDeepAgent(&agentschema.AgentCard{})
	agent.SetUapswarmAdapterMode(codeAdapterMode)
	agent.AddRail(rails.NewAgentModeRail(nil))
	agent.AddRail(rails.NewAgentModeRail(nil))

	cbc := agentinterfaces.NewAgentCallbackContext(agent,
		&agentinterfaces.ToolCallInputs{ToolName: enterPlanModeTool}, sess)
	invocation := ToolInvocation{Ctx: cbc, ToolName: enterPlanModeTool}
	if got := TrustedCodeRoute(invocation, codeFacts(), false, agent); got != "" {
		t.Errorf("multiple rails should fail, got %q", got)
	}
}

// TestTrustedCodeRoute_未登记工具类型 验证 V-18 的 fail-closed。
func TestTrustedCodeRoute_未登记工具类型(t *testing.T) {
	defer SetCodeControlToolTypes(nil)
	SetCodeControlToolTypes(nil)
	sess := session.NewSession()
	agent := harness.NewDeepAgent(&agentschema.AgentCard{})
	agent.SetUapswarmAdapterMode(codeAdapterMode)
	agent.AddRail(rails.NewAgentModeRail(nil))

	cbc := agentinterfaces.NewAgentCallbackContext(agent,
		&agentinterfaces.ToolCallInputs{ToolName: switchModeTool}, sess)
	invocation := ToolInvocation{Ctx: cbc, ToolName: switchModeTool}
	if got := TrustedCodeRoute(invocation, codeFacts(), false, agent); got != "" {
		t.Errorf("unregistered type should fail, got %q", got)
	}
}

// TestCodeOwnerBridgeValid_nilCtx 验证 nil 上下文安全。
func TestCodeOwnerBridgeValid_nilCtx(t *testing.T) {
	if codeOwnerBridgeValid(ToolInvocation{}, nil) {
		t.Error("nil ctx should fail")
	}
}

// TestCodeControlBindingValid_nilCtx 验证 nil 上下文安全。
func TestCodeControlBindingValid_nilCtx(t *testing.T) {
	if codeControlBindingValid(ToolInvocation{}) {
		t.Error("nil ctx should fail")
	}
}
