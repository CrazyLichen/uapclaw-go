package auto_permission

import (
	"context"
	"reflect"
	"testing"
	"unsafe"

	"github.com/uapclaw/uapclaw-go/internal/common/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool"
	session "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interfaces"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness"
	agentcron "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/tools/cron"
	commrails "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails"
	cronbridge "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools/cron"
	swarmtools "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools"
)

// newV6Harness 构造 owner DeepAgent + ToolRetrievalPromptRail + 真注册表。
// callback agent 直接用 owner（deepAgentBridgeMatches 身份分支 1）。
func newV6Harness(t *testing.T, sessionID string) (*harness.DeepAgent, *commrails.ToolRetrievalPromptRail, *swarmtools.ToolRetrievalToolkit) {
	t.Helper()
	// sessionID 为空时用真实会话 id（toolkit.session_scope 必须与绑定会话一致，
	// 对齐 Python toolkit._session_scope == session_id 校验）。
	if sessionID == "" {
		sessionID = session.NewSession().GetSessionID()
	}
	owner := harness.NewDeepAgent(agentschema.NewAgentCard(
		agentschema.WithAgentID("v6-owner"),
		agentschema.WithAgentName("v6-test"),
	))
	bindV6Session(t, owner, sessionID)
	am := owner.AbilityManager()
	provider := func() []schema.ToolInfoInterface { return nil }
	toolkit := swarmtools.NewToolRetrievalToolkit(provider, sessionID, nil, nil, "cn")
	rail := commrails.NewToolRetrievalPromptRail(toolkit, map[string]any{})
	if err := owner.RegisterRail(context.Background(), rail); err != nil {
		t.Fatalf("RegisterRail 失败: %v", err)
	}
	for _, toolInstance := range toolkit.GetTools() {
		_ = am.AddAbility(toolInstance.Card(), toolInstance)
		// ResolveToolBinding 的实例解析注入（包既有测试同款）
		cardID := toolInstance.Card().ID
		registered := toolInstance
		SetResolveToolByIDFunc(func(id string) tool.Tool {
			if id == cardID {
				return registered
			}
			return nil
		})
	}
	return owner, rail, toolkit
}

// bindV6Session 设置 boundSessionID（无导出 setter，测试用 unsafe，与包既有
// 测试 newBoundDeepAgent 同款）。
func bindV6Session(t *testing.T, agent *harness.DeepAgent, sessionID string) {
	t.Helper()
	value := reflect.ValueOf(agent).Elem().FieldByName("boundSessionID")
	if !value.IsValid() || value.Kind() != reflect.String {
		t.Fatal("boundSessionID field not found")
	}
	reflect.NewAt(value.Type(), unsafe.Pointer(value.UnsafeAddr())).Elem().SetString(sessionID)
}

// newV6Cbc 带 session 的回调上下文（callback agent = owner 自身）。
func newV6Cbc(owner *harness.DeepAgent, sess sessioninterfaces.SessionFacade) *agentinterfaces.AgentCallbackContext {
	inputs := &agentinterfaces.ToolCallInputs{ToolName: toolSearchName}
	return agentinterfaces.NewAgentCallbackContext(owner, inputs, sess)
}

// TestTrustedReadonlyBinding_ToolRetrievalPath 对齐 Python :176-196 正例。
func TestTrustedReadonlyBinding_ToolRetrievalPath(t *testing.T) {
	sess := session.NewSession()
	owner, _, _ := newV6Harness(t, sess.GetSessionID())
	cbc := agentinterfaces.NewAgentCallbackContext(owner, nil, sess)
	invocation := ToolInvocation{
		Ctx:      cbc,
		ToolName: "tool_search",
		ToolArgs: map[string]any{"operation": "list", "category": "cron"},
	}
	if !trustedReadonlyBinding(invocation, sess.GetSessionID(), owner) {
		t.Fatal("tool_retrieval 归属的 tool_search 应命中快速路径")
	}
}

// TestTrustedReadonlyBinding_FailClosed 对齐 Python fail-closed 反例。
func TestTrustedReadonlyBinding_FailClosed(t *testing.T) {
	sess := session.NewSession()
	owner, _, _ := newV6Harness(t, sess.GetSessionID())
	cbc := agentinterfaces.NewAgentCallbackContext(owner, nil, sess)
	invocation := ToolInvocation{Ctx: cbc, ToolName: "tool_search", ToolArgs: map[string]any{"operation": "list", "category": "cron"}}

	// ① session 不一致
	if trustedReadonlyBinding(invocation, "other-session", owner) {
		t.Fatal("session 不一致应 fail-closed")
	}
	// ② toolkit session_scope 不一致（rail 的 toolkit 是另一 scope）
	otherScopeToolkit := swarmtools.NewToolRetrievalToolkit(
		func() []schema.ToolInfoInterface { return nil }, "another-scope", nil, nil, "cn")
	otherRail := commrails.NewToolRetrievalPromptRail(otherScopeToolkit, map[string]any{})
	otherOwner := harness.NewDeepAgent(agentschema.NewAgentCard(
		agentschema.WithAgentID("v6-owner-2"),
		agentschema.WithAgentName("v6-test-2"),
	))
	if err := otherOwner.RegisterRail(context.Background(), otherRail); err != nil {
		t.Fatalf("RegisterRail 失败: %v", err)
	}
	for _, toolInstance := range otherScopeToolkit.GetTools() {
		_ = otherOwner.AbilityManager().AddAbility(toolInstance.Card(), toolInstance)
	}
	if trustedReadonlyBinding(invocation, "sess-v6", otherOwner) {
		t.Fatal("toolkit scope 不一致应 fail-closed")
	}
	// ③ 前置件未落地：heartbeat_* / skill_index 恒 false
	if trustedReadonlyBinding(ToolInvocation{Ctx: cbc, ToolName: "heartbeat_create_job"}, sess.GetSessionID(), owner) {
		t.Fatal("heartbeat 应 fail-closed（⤴️9.11c）")
	}
	if trustedReadonlyBinding(ToolInvocation{Ctx: cbc, ToolName: "skill_index"}, sess.GetSessionID(), owner) {
		t.Fatal("skill_index 应 fail-closed（⤴️16.4）")
	}
	// ④ 缺卡（tool_search 未注册进该 owner 的 AbilityManager）
	bareOwner := harness.NewDeepAgent(agentschema.NewAgentCard(
		agentschema.WithAgentID("v6-owner-3"),
		agentschema.WithAgentName("v6-test-3"),
	))
	_, bareRail, _ := newV6Harness(t, "sess-v6")
	if err := bareOwner.RegisterRail(context.Background(), bareRail); err != nil {
		t.Fatalf("RegisterRail 失败: %v", err)
	}
	if trustedReadonlyBinding(invocation, "sess-v6", bareOwner) {
		t.Fatal("tool_search 卡未注册应 fail-closed")
	}
}

// TestCronReadonlyBinding 对齐 Python :286-305：受限视图 + 绑定会话。
func TestCronReadonlyBinding(t *testing.T) {
	owner := harness.NewDeepAgent(agentschema.NewAgentCard(
		agentschema.WithAgentID("v6-cron-owner"),
		agentschema.WithAgentName("v6-cron"),
	))
	am := owner.AbilityManager()
	sess := session.NewSession()

	// 装配：受限 backend + 绑定上下文的 cron 卡（factory 装配期登记属性）
	inner := &stubV6CronBackend{}
	restricted := cronbridge.NewRestrictedCronBackend(inner, false, false)
	cronCtx := &agentcron.CronToolContext{ChannelID: "feishu", SessionID: sess.GetSessionID()}
	card := tool.NewToolCardWithID("cron_list_jobs", "cron_list_jobs", "列出", nil, nil)
	card.Properties = map[string]any{"cron_backend": restricted, "cron_context": cronCtx}
	_ = am.AddAbility(card, echoV6Tool{card: card})

	cbc := agentinterfaces.NewAgentCallbackContext(owner, nil, sess)
	invocation := ToolInvocation{Ctx: cbc, ToolName: "cron_list_jobs"}
	if !trustedReadonlyBinding(invocation, sess.GetSessionID(), owner) {
		t.Fatal("受限 backend + 绑定会话应命中")
	}
	// 绑定会话不一致
	if trustedReadonlyBinding(invocation, "other-session-id", owner) {
		t.Fatal("绑定会话不一致应 fail-closed")
	}
	// 无 backend 属性的卡
	card2 := tool.NewToolCardWithID("cron_get_job", "cron_get_job", "查询", nil, nil)
	_ = am.AddAbility(card2, echoV6Tool{card: card2})
	if trustedReadonlyBinding(ToolInvocation{Ctx: cbc, ToolName: "cron_get_job"}, sess.GetSessionID(), owner) {
		t.Fatal("无装配登记应 fail-closed")
	}
	// 未注册工具
	if trustedReadonlyBinding(ToolInvocation{Ctx: cbc, ToolName: "cron_preview_job"}, sess.GetSessionID(), owner) {
		t.Fatal("未注册应 fail-closed")
	}
	// 全开受限视图 + 绑定会话：同样命中（对齐 Python：restricted 判定后
	// 挖内层，非受限路径亦可）
	plain := cronbridge.NewRestrictedCronBackend(inner, true, true)
	card3 := tool.NewToolCardWithID("cron_toggle_job", "cron_toggle_job", "启停", nil, nil)
	card3.Properties = map[string]any{"cron_backend": plain, "cron_context": cronCtx}
	_ = am.AddAbility(card3, echoV6Tool{card: card3})
	if !trustedReadonlyBinding(ToolInvocation{Ctx: cbc, ToolName: "cron_toggle_job"}, sess.GetSessionID(), owner) {
		t.Fatal("全开受限视图 + 绑定会话应命中")
	}
}

// ──────────────────────────── 测试基建 ────────────────────────────

// stubV6CronBackend 空实现内层（仅供身份链校验）。
type stubV6CronBackend struct{ agentcron.CronToolBackend }

// echoV6Tool 带 card 的最小工具。
type echoV6Tool struct{ card *tool.ToolCard }

func (s echoV6Tool) Card() *tool.ToolCard { return s.card }
func (s echoV6Tool) Invoke(_ context.Context, inputs map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	return map[string]any{"success": true, "echo": inputs}, nil
}
func (s echoV6Tool) Stream(context.Context, map[string]any, ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}
func (s echoV6Tool) GetCallTimeout(_ map[string]any, defaultTimeout float64) float64 { return defaultTimeout }
