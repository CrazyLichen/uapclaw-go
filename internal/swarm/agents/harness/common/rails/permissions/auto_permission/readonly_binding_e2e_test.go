package auto_permission

import (
	"context"
	"reflect"
	"testing"
	"unsafe"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// TestVerify_只读快速路径端到端 验证从 fail-closed 变为 allow 的完整链路。
//
// 链路：ProgressiveToolRail.Init 构造 tool_search
//
//	→ SetToolInstanceObserver 自动登记类型
//	→ trustedReadonlyBinding 校验 rail.IsTrusted() + ResolveToolBinding
//	→ 返回 true（启用快速路径）
func TestVerify_只读快速路径端到端(t *testing.T) {
	// 清理其他用例可能留下的全局注入（SetResolveToolByIDFunc 是包级变量）
	ResetResolveToolByIDFunc()

	// ── 步骤 1：真实装配，触发自动登记 ──
	agent := harness.NewDeepAgent(&agentschema.AgentCard{})
	rail := rails.NewProgressiveToolRail(nil)
	agent.AddRail(rail) // 先注册进图，Init 才会在图中可见
	if err := rail.Init(context.Background(), agent); err != nil {
		t.Fatalf("Init: %v", err)
	}

	registeredType := toolSearchToolType()
	if registeredType == nil {
		t.Fatal("【失败】tool_search 类型未自动登记，快速路径仍 fail-closed")
	}
	t.Logf("✓ 自动登记生效：tool_search 类型 = %v", registeredType)

	// ── 步骤 2：从图里取回真实工具实例，注册成能力卡 ──
	found := agent.FindRailsByType(reflect.TypeOf((*rails.ProgressiveToolRail)(nil)))
	if len(found) != 1 {
		t.Fatalf("ProgressiveToolRail 应恰好 1 个，实际 %d", len(found))
	}
	progressive := found[0].(*rails.ProgressiveToolRail)
	if !progressive.IsTrusted() {
		t.Fatal("【失败】ProgressiveToolRail 未 MarkTrusted")
	}
	t.Log("✓ ProgressiveToolRail 已 MarkTrusted")

	// Init 已把 tool_search 注册到 AbilityManager + ResourceMgr，
	// 直接从中取回实例做卡片同一性校验
	am := agent.AbilityManager()
	card := am.Get(toolSearchName)
	if card == nil {
		t.Fatal("【失败】tool_search 未注册到 AbilityManager")
	}
	t.Logf("✓ tool_search 已注册，卡片 ID = %s", card.AbilityID())

	// ── 步骤 3：绑定会话标识，构造调用上下文 ──
	sess := session.NewSession()
	sessionID := sess.GetSessionID()
	setBoundSessionID(t, agent, sessionID)

	inputs := &agentinterfaces.ToolCallInputs{
		ToolName: toolSearchName,
		ToolArgs: map[string]any{"query": "find file tools"},
	}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, sess)
	invocation := ToolInvocation{
		Ctx:      cbc,
		ToolName: toolSearchName,
		ToolArgs: inputs.ToolArgs,
	}

	// ── 步骤 4：断言快速路径放行 ──
	got := trustedReadonlyBinding(invocation, sessionID, agent)
	if !got {
		// 逐项诊断哪一步没通过
		t.Logf("诊断: IsTrusted=%v", progressive.IsTrusted())
		t.Logf("诊断: rails=%d", len(agent.FindRailsByType(progressiveToolRailType)))
		t.Logf("诊断: registeredType=%v", registeredType)
		if card != nil {
			t.Logf("诊断: ResolveToolBinding=%v",
				ResolveToolBinding(am, toolSearchName, registeredType) != nil)
		}
		t.Fatal("【失败】只读快速路径未放行")
	}
	t.Log("✓✓ 只读快速路径已放行（从 fail-closed 变为 allow）")

	// ── 步骤 5：反证——伪造 rail 必须仍拒绝 ──
	forged := harness.NewDeepAgent(&agentschema.AgentCard{})
	forged.AddRail(&rails.ProgressiveToolRail{})
	setBoundSessionID(t, forged, sessionID)
	forgedCbc := agentinterfaces.NewAgentCallbackContext(forged, inputs, sess)
	if trustedReadonlyBinding(
		ToolInvocation{Ctx: forgedCbc, ToolName: toolSearchName, ToolArgs: inputs.ToolArgs},
		sessionID, forged) {
		t.Fatal("【失败】伪造 rail 不应放行")
	}
	t.Log("✓ 反证通过：伪造 rail（未 MarkTrusted）仍 fail-closed")
}

// setBoundSessionID 用 unsafe 设置私有字段（生产走 PrepareInteractionTaskLoop）。
func setBoundSessionID(t *testing.T, agent *harness.DeepAgent, sessionID string) {
	t.Helper()
	value := reflect.ValueOf(agent).Elem().FieldByName("boundSessionID")
	if !value.IsValid() {
		t.Fatal("boundSessionID 字段不存在")
	}
	reflect.NewAt(value.Type(), unsafe.Pointer(value.UnsafeAddr())).Elem().SetString(sessionID)
}
