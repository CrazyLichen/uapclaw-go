package auto_permission

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"unsafe"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions"
)

// ──────────────────────────── 用例：trustedReadonlyBinding ───────────────────────────────

// TestTrustedReadonlyBinding_非toolSearch直接false 验证 D3 范围收窄。
func TestTrustedReadonlyBinding_非toolSearch直接false(t *testing.T) {
	for _, name := range []string{"list_skill", "get_current_goal", "recall_compressed_context", "bash", ""} {
		invocation := ToolInvocation{ToolName: name}
		if trustedReadonlyBinding(invocation, "s1", nil) {
			t.Errorf("%q should not use fast path（D3 只覆盖 tool_search）", name)
		}
	}
}

// TestTrustedReadonlyBinding_无ctx或session 验证基础 fail-closed。
func TestTrustedReadonlyBinding_无ctx或session(t *testing.T) {
	invocation := ToolInvocation{ToolName: toolSearchName}
	if trustedReadonlyBinding(invocation, "", nil) {
		t.Error("empty session should fail")
	}
	if trustedReadonlyBinding(invocation, "s1", nil) {
		t.Error("nil ctx should fail")
	}
}

// TestTrustedReadonlyBinding_owner类型不符 验证类型不符 fail-closed。
func TestTrustedReadonlyBinding_owner类型不符(t *testing.T) {
	invocation := ToolInvocation{ToolName: toolSearchName, Ctx: newBindCbc(nil)}
	if trustedReadonlyBinding(invocation, "s1", "not an agent") {
		t.Error("wrong owner type should fail")
	}
}

// ──────────────────────────── 登记表 ────────────────────────────

// TestSetReadonlyToolTypes 验证登记与读取。
func TestSetReadonlyToolTypes(t *testing.T) {
	defer SetReadonlyToolTypes(nil)
	SetReadonlyToolTypes(nil)
	if got := registeredReadonlyToolType(toolSearchName); got != nil {
		t.Errorf("got %v", got)
	}
	typ := reflect.TypeOf((*fakeReadonlyTool)(nil))
	SetReadonlyToolTypes(map[string]reflect.Type{toolSearchName: typ, "bad": nil})
	if got := registeredReadonlyToolType(toolSearchName); got != typ {
		t.Errorf("got %v, want %v", got, typ)
	}
	if got := registeredReadonlyToolType("bad"); got != nil {
		t.Errorf("nil type should be skipped, got %v", got)
	}
}

// TestToolSearchToolType 验证返回登记类型。
func TestToolSearchToolType(t *testing.T) {
	defer SetReadonlyToolTypes(nil)
	SetReadonlyToolTypes(nil)
	if got := toolSearchToolType(); got != nil {
		t.Errorf("unregistered -> %v, want nil", got)
	}
}

// ──────────────────────────── 辅助 ────────────────────────────

// TestBoundSessionIDOf 验证反射读取。
func TestBoundSessionIDOf(t *testing.T) {
	type withBound struct{ boundSessionID string }
	if got := boundSessionIDOf(&withBound{boundSessionID: "s1"}); got != "s1" {
		t.Errorf("got %q", got)
	}
	if got := boundSessionIDOf(withBound{boundSessionID: "s2"}); got != "s2" {
		t.Errorf("value form -> %q", got)
	}
	if got := boundSessionIDOf("string"); got != "" {
		t.Errorf("non-struct -> %q", got)
	}
	if got := boundSessionIDOf(nil); got != "" {
		t.Errorf("nil -> %q", got)
	}
}

// TestProgressiveToolRailType 验证具体类型常量。
func TestProgressiveToolRailType(t *testing.T) {
	if progressiveToolRailType != reflect.TypeOf((*rails.ProgressiveToolRail)(nil)) {
		t.Error("type mismatch")
	}
}

// TestSetReadonlyToolTypes_并发安全 验证并发读写不 panic。
func TestSetReadonlyToolTypes_并发安全(t *testing.T) {
	defer SetReadonlyToolTypes(nil)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			SetReadonlyToolTypes(map[string]reflect.Type{toolSearchName: progressiveToolRailType})
			_ = registeredReadonlyToolType(toolSearchName)
		}()
	}
	wg.Wait()
}

// ──────────────────────────── 测试辅助 ────────────────────────────

// fakeReadonlyTool 测试用工具替身，实现 tool.Tool 全部方法。
type fakeReadonlyTool struct {
	// card 注册的卡片（ResolveToolBinding 要求指针同一）
	card *tool.ToolCard
}

// Card 实现 tool.Tool
func (f *fakeReadonlyTool) Card() *tool.ToolCard {
	return f.card
}

// Invoke 实现 tool.Tool
func (f *fakeReadonlyTool) Invoke(context.Context, map[string]any, ...tool.ToolOption) (map[string]any, error) {
	return nil, nil
}

// Stream 实现 tool.Tool
func (f *fakeReadonlyTool) Stream(context.Context, map[string]any, ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, nil
}

// GetCallTimeout 实现 tool.Tool
func (f *fakeReadonlyTool) GetCallTimeout(map[string]any, float64) float64 { return 0 }

// newBindCbc 构造带 session 的回调上下文。
func newBindCbc(sess sessioninterfaces.SessionFacade) *agentinterfaces.AgentCallbackContext {
	inputs := &agentinterfaces.ToolCallInputs{ToolName: toolSearchName}
	return agentinterfaces.NewAgentCallbackContext(nil, inputs, sess)
}

// 确保 permissions 包被引用（避免测试文件 import 被裁剪）
var _ = permissions.AutoDecisionAllowLevel

// ──────────────────────────── 真实 DeepAgent 成功路径 ────────────────────────────

// newBoundDeepAgent 构造绑定了指定会话的 DeepAgent。
func newBoundDeepAgent(t *testing.T, sessionID string) *harness.DeepAgent {
	t.Helper()
	agent := harness.NewDeepAgent(&agentschema.AgentCard{})
	// boundSessionID 无导出 setter（生产走 PrepareInteractionTaskLoop），
	// 测试用 unsafe 绕过反射的只读限制设置该字段
	value := reflect.ValueOf(agent).Elem().FieldByName("boundSessionID")
	if !value.IsValid() || value.Kind() != reflect.String {
		t.Fatal("boundSessionID field not found")
	}
	reflect.NewAt(value.Type(), unsafe.Pointer(value.UnsafeAddr())).Elem().SetString(sessionID)
	return agent
}

// TestTrustedToolSearchBinding_成功路径 验证 D5：真实构造 + MarkTrusted 后通过。
func TestTrustedToolSearchBinding_成功路径(t *testing.T) {
	sess := session.NewSession()
	sessionID := sess.GetSessionID()

	agent := newBoundDeepAgent(t, sessionID)
	progressive := rails.NewProgressiveToolRail(nil)
	agent.AddRail(progressive)

	// 装配期登记 ToolSearchTool 的具体类型
	defer SetReadonlyToolTypes(nil)
	SetReadonlyToolTypes(map[string]reflect.Type{
		toolSearchName: reflect.TypeOf((*fakeReadonlyTool)(nil)),
	})

	// ability_manager 必须注册该工具；用注入点让 ResolveToolBinding 找到实例
	card := tool.NewToolCardWithID("card-ts-1", toolSearchName, "search", nil, nil)
	toolInstance := &fakeReadonlyTool{card: card}
	agent.AbilityManager().Add(card)
	SetResolveToolByIDFunc(func(id string) tool.Tool {
		if id == "card-ts-1" {
			return toolInstance
		}
		return nil
	})

	inputs := &agentinterfaces.ToolCallInputs{
		ToolName: toolSearchName,
		ToolArgs: map[string]any{"query": "find file tools"},
	}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, sess)
	invocation := ToolInvocation{Ctx: cbc, ToolName: toolSearchName, ToolArgs: inputs.ToolArgs}

	got := trustedReadonlyBinding(invocation, sessionID, agent)
	// callbackAgent 是 DeepAgent 自身（不是 reactAgent），桥接应成立
	if !got {
		t.Error("trusted ProgressiveToolRail should enable fast path（D5）")
	}
}

// TestTrustedToolSearchBinding_未MarkTrusted 验证 D5 反例。
func TestTrustedToolSearchBinding_未MarkTrusted(t *testing.T) {
	sess := session.NewSession()
	sessionID := sess.GetSessionID()
	agent := newBoundDeepAgent(t, sessionID)
	// 绕过构造函数直接造 rail → 未 MarkTrusted
	progressive := &rails.ProgressiveToolRail{}
	agent.AddRail(progressive)

	cbc := agentinterfaces.NewAgentCallbackContext(agent,
		&agentinterfaces.ToolCallInputs{ToolName: toolSearchName}, sess)
	invocation := ToolInvocation{Ctx: cbc, ToolName: toolSearchName}

	if trustedReadonlyBinding(invocation, sessionID, agent) {
		t.Error("untrusted rail should fail-closed（D5 反例）")
	}
}

// TestSessionObservationBinding_会话不一致 验证会话校验。
func TestSessionObservationBinding_会话不一致(t *testing.T) {
	sess := session.NewSession()
	agent := newBoundDeepAgent(t, "other-session")
	progressive := rails.NewProgressiveToolRail(nil)
	agent.AddRail(progressive)
	cbc := agentinterfaces.NewAgentCallbackContext(agent,
		&agentinterfaces.ToolCallInputs{ToolName: toolSearchName}, sess)
	invocation := ToolInvocation{Ctx: cbc, ToolName: toolSearchName}
	if sessionObservationBinding(invocation, sess.GetSessionID(), agent) {
		t.Error("mismatched bound session should fail")
	}
}

// TestSessionObservationBinding_终止阶段 验证 phase 校验。
func TestSessionObservationBinding_终止阶段(t *testing.T) {
	sess := session.NewSession()
	sessionID := sess.GetSessionID()
	agent := newBoundDeepAgent(t, sessionID)
	progressive := rails.NewProgressiveToolRail(nil)
	agent.AddRail(progressive)
	// 用 unsafe 设置非导出 phase 字段为 TERMINATED
	phaseValue := reflect.ValueOf(agent).Elem().FieldByName("phase")
	if phaseValue.IsValid() && phaseValue.CanAddr() {
		reflect.NewAt(phaseValue.Type(), unsafe.Pointer(phaseValue.UnsafeAddr())).
			Elem().Set(reflect.ValueOf(hschema.InteractionPhaseTerminated))
	}
	cbc := agentinterfaces.NewAgentCallbackContext(agent,
		&agentinterfaces.ToolCallInputs{ToolName: toolSearchName}, sess)
	invocation := ToolInvocation{Ctx: cbc, ToolName: toolSearchName}
	if sessionObservationBinding(invocation, sessionID, agent) {
		t.Error("terminated phase should fail")
	}
}

// TestSessionObservationBinding_多rail 验证「恰好一个」约束。
func TestSessionObservationBinding_多rail(t *testing.T) {
	sess := session.NewSession()
	sessionID := sess.GetSessionID()
	agent := newBoundDeepAgent(t, sessionID)
	agent.AddRail(rails.NewProgressiveToolRail(nil))
	agent.AddRail(rails.NewProgressiveToolRail(nil))
	cbc := agentinterfaces.NewAgentCallbackContext(agent,
		&agentinterfaces.ToolCallInputs{ToolName: toolSearchName}, sess)
	invocation := ToolInvocation{Ctx: cbc, ToolName: toolSearchName}
	if sessionObservationBinding(invocation, sessionID, agent) {
		t.Error("multiple rails should fail")
	}
}

// TestSessionObservationBinding_无rail 验证缺失 rail fail-closed。
func TestSessionObservationBinding_无rail(t *testing.T) {
	sess := session.NewSession()
	sessionID := sess.GetSessionID()
	agent := newBoundDeepAgent(t, sessionID)
	cbc := agentinterfaces.NewAgentCallbackContext(agent,
		&agentinterfaces.ToolCallInputs{ToolName: toolSearchName}, sess)
	invocation := ToolInvocation{Ctx: cbc, ToolName: toolSearchName}
	if sessionObservationBinding(invocation, sessionID, agent) {
		t.Error("no rail should fail")
	}
}

// TestDeepAgentBridgeMatches_桥接失败 验证能力管理器不同的反例。
func TestDeepAgentBridgeMatches_桥接失败(t *testing.T) {
	agent := newBoundDeepAgent(t, "s1")
	// callbackAgent 是另一个 agent → 既非自身也非 reactAgent
	other := harness.NewDeepAgent(&agentschema.AgentCard{})
	if deepAgentBridgeMatches(agent, other) {
		t.Error("different agent should not bridge")
	}
}

// TestBoundSessionIDOf_无字段 验证缺字段回落空串。
func TestBoundSessionIDOf_无字段(t *testing.T) {
	type noField struct{ X int }
	if got := boundSessionIDOf(&noField{}); got != "" {
		t.Errorf("got %q", got)
	}
}
