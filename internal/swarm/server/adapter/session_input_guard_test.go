package adapter

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness"
	hschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/schema"
)

// TestSDKInputMode steer/follow_up/nil 三态。
func TestSDKInputMode(t *testing.T) {
	if m := SDKInputMode(map[string]any{"input_mode": "steer"}); m == nil || *m != hschema.InputDispatchModeSteer {
		t.Errorf("steer 应解析，实际 %v", m)
	}
	if m := SDKInputMode(map[string]any{"input_mode": "follow_up"}); m == nil || *m != hschema.InputDispatchModeFollowUp {
		t.Errorf("follow_up 应解析，实际 %v", m)
	}
	if m := SDKInputMode(map[string]any{}); m != nil {
		t.Errorf("无标记应 nil，实际 %v", m)
	}
}

// TestVisibleResponseText 空响应与纯文本。
func TestVisibleResponseText(t *testing.T) {
	if got := visibleResponseText(nil); got != "" {
		t.Errorf("nil 应空串，实际 %q", got)
	}
	msg := schema.NewAssistantMessage("回复正文")
	if got := visibleResponseText(msg); got != "回复正文" {
		t.Errorf("应取正文，实际 %q", got)
	}
}

// TestCrossSessionModelMessages 工具结果对形态：call_id 稳定、元数据齐全。
func TestCrossSessionModelMessages(t *testing.T) {
	messages := CrossSessionModelMessages("跨会话正文", map[string]any{
		"message_id": "m-1", "chain_id": "c1", "hop_count": float64(2),
	})
	if len(messages) != 2 {
		t.Fatalf("应两条（assistant+tool），实际 %d", len(messages))
	}
	assistant, ok := messages[0].(*schema.AssistantMessage)
	if !ok || len(assistant.ToolCalls) != 1 {
		t.Fatalf("首条应为带 tool_calls 的 assistant，实际 %#v", messages[0])
	}
	call := assistant.ToolCalls[0]
	if !strings.HasPrefix(call.ID, "sm_") || call.Name != "session_message_received" {
		t.Errorf("call 形态不符: %+v", call)
	}
	if len(call.ID) != len("sm_")+24 {
		t.Errorf("call_id 应为 sha256 前 24 hex，实际 %s", call.ID)
	}
	tool, ok := messages[1].(*schema.ToolMessage)
	if !ok || tool.ToolCallID != call.ID || tool.Content.String() != "跨会话正文" {
		t.Errorf("tool 对不符: %#v", tool)
	}
	if assistant.Metadata["host_generated"] != true {
		t.Errorf("应带 host_generated 元数据")
	}
	// 同一 message_id → 同一 call_id（幂等）
	again := CrossSessionModelMessages("x", map[string]any{"message_id": "m-1"})
	againAssistant := again[0].(*schema.AssistantMessage)
	if againAssistant.ToolCalls[0].ID != call.ID {
		t.Error("同 identity 应产出同 call_id")
	}
}

// TestAgentDeliveryReference 引用裁剪与白名单键。
func TestAgentDeliveryReference(t *testing.T) {
	long := strings.Repeat("x", 300)
	ref := agentDeliveryReference(map[string]any{
		"message_id": long, "hop_count": 3, "unknown_key": "dropped",
	})
	if len(ref["cross_session"].(map[string]any)["message_id"].(string)) != 256 {
		t.Error("超长应截到 256")
	}
	if _, exists := ref["cross_session"].(map[string]any)["unknown_key"]; exists {
		t.Error("白名单外键应丢弃")
	}
	if ref["authority"] != "tool_result" || ref["content_omitted_from_system"] != true {
		t.Errorf("权限声明不符: %v", ref)
	}
}

// TestSessionInputGuard_准入窗口 初始关、before_model_call 开、无工具调用收尾关。
func TestSessionInputGuard_准入窗口(t *testing.T) {
	agent := newGuardTestAgent()
	guard := NewSessionInputGuard(agent)
	if guard.Accepting() {
		t.Fatal("初始应 accepting=false（对齐 Python __init__）")
	}
	cbc := guardModelCallCBC(agent, 1)
	if err := guard.beforeModelCall(context.Background(), cbc); err != nil {
		t.Fatal(err)
	}
	if !guard.Accepting() {
		t.Error("未配置 max_iterations 应恒开")
	}
	if err := guard.afterModelCall(cbc); err != nil {
		t.Fatal(err)
	}
	if guard.Accepting() {
		t.Error("无工具调用的回复收尾后应关窗")
	}
}

// TestSessionInputGuard_工具窗口 工具期间保持开窗，最后一个收尾关。
func TestSessionInputGuard_工具窗口(t *testing.T) {
	agent := newGuardTestAgent()
	guard := NewSessionInputGuard(agent)
	cbc := guardModelCallCBC(agent, 1)
	_ = guard.beforeModelCall(context.Background(), cbc)
	_ = guard.beforeToolCall()
	if !guard.Accepting() {
		t.Error("工具执行期间应开窗")
	}
	_ = guard.beforeToolCall()
	_ = guard.afterToolCall()
	if !guard.Accepting() {
		t.Error("仍有在飞工具应保持开窗")
	}
	_ = guard.afterToolCall()
	if guard.Accepting() {
		t.Error("最后一个工具收尾应关窗")
	}
}

// TestSessionInputGuard_异常与收尾 异常关窗；after_invoke 清交付状态。
func TestSessionInputGuard_异常与收尾(t *testing.T) {
	agent := newGuardTestAgent()
	guard := NewSessionInputGuard(agent)
	cbc := guardModelCallCBC(agent, 1)
	_ = guard.beforeModelCall(context.Background(), cbc)
	_ = guard.onException()
	if guard.Accepting() {
		t.Error("异常应关窗")
	}
	// after_invoke：非 interrupt 结果清态
	invoke := agentinterfaces.NewAgentCallbackContext(agent,
		&agentinterfaces.InvokeInputs{Result: map[string]any{"result_type": "ok"}}, nil)
	if err := guard.afterInvoke(invoke); err != nil {
		t.Fatal(err)
	}
	// interrupt 结果不清态：交付义务保留
	invoke2 := agentinterfaces.NewAgentCallbackContext(agent,
		&agentinterfaces.InvokeInputs{Result: map[string]any{"result_type": "interrupt"}}, nil)
	if err := guard.afterInvoke(invoke2); err != nil {
		t.Fatal(err)
	}
}

// TestSessionInputGuard_PublishInputReceived_无会话流 无 sessionRef 时边界失败 + DeliveryUnknown。
func TestSessionInputGuard_PublishInputReceived_无会话流(t *testing.T) {
	agent := newGuardTestAgent()
	guard := NewSessionInputGuard(agent)
	entry := hschema.NewQueuedSessionInput("q", "req-1")
	err := guard.PublishInputReceived(context.Background(), entry)
	var unknown *SessionInputDeliveryUnknown
	if !errors.As(err, &unknown) {
		t.Fatalf("应返回 DeliveryUnknown，实际 %v", err)
	}
	if entry.BoundaryError == nil {
		t.Error("条目应记录边界错误")
	}
	select {
	case <-entry.BoundaryReady:
	default:
		t.Error("边界信号应已关闭")
	}
}

// TestSessionInputGuard_代次边界 before_steering_drain 置位、consume 一次性。
func TestSessionInputGuard_代次边界(t *testing.T) {
	agent := newGuardTestAgent()
	guard := NewSessionInputGuard(agent)
	if guard.ConsumeGenerationBoundary() {
		t.Error("初始应无边界")
	}
	// pending>0 → 置位
	drainCBC := agentinterfaces.NewAgentCallbackContext(nil,
		&agentinterfaces.SteeringDrainInputs{Pending: 2}, nil)
	if err := guard.beforeSteeringDrain(drainCBC); err != nil {
		t.Fatal(err)
	}
	if !guard.ConsumeGenerationBoundary() {
		t.Error("应消费到边界")
	}
	if guard.ConsumeGenerationBoundary() {
		t.Error("边界应一次性")
	}
}

// TestGetCallbacks_守卫声明全钩子 防 GetCallbacks 名字错位回归。
func TestGetCallbacks_守卫声明全钩子(t *testing.T) {
	agent := newGuardTestAgent()
	guard := NewSessionInputGuard(agent)
	cbs := guard.GetCallbacks()
	for _, ev := range []agentinterfaces.AgentCallbackEvent{
		agentinterfaces.CallbackOnUserMessage, agentinterfaces.CallbackBeforeInvoke,
		agentinterfaces.CallbackBeforeModelCall, agentinterfaces.CallbackAfterModelCall,
		agentinterfaces.CallbackBeforeToolCall, agentinterfaces.CallbackAfterToolCall,
		agentinterfaces.CallbackAfterInvoke, agentinterfaces.CallbackBeforeSteeringDrain,
	} {
		if _, ok := cbs[ev]; !ok {
			t.Errorf("缺少 %s 回调", ev)
		}
	}
	boundary := NewSessionInputBoundaryGuard(NewSessionInputLedger())
	if _, ok := boundary.GetCallbacks()[agentinterfaces.CallbackOnUserMessage]; !ok {
		t.Error("边界守卫应声明 on_user_message")
	}
	if boundary.Priority() != 1000 {
		t.Errorf("边界守卫优先级应 1000，实际 %d", boundary.Priority())
	}
}

// ──────────────────────────── 测试构造助手 ────────────────────────────

// newGuardTestAgent 构造最小 DeepAgent（守卫状态机用）。
func newGuardTestAgent() *harness.DeepAgent {
	return harness.NewDeepAgent(guardTestCard())
}

// guardModelCallCBC 构造 BeforeModelCall/AfterModelCall 共用的回调上下文。
func guardModelCallCBC(agent *harness.DeepAgent, iteration int) *agentinterfaces.AgentCallbackContext {
	cbc := agentinterfaces.NewAgentCallbackContext(agent,
		&agentinterfaces.ModelCallInputs{
			Response:       schema.NewAssistantMessage("可见回复"),
			ReactIteration: iteration,
		}, nil)
	return cbc
}

// guardInvokeCBC 构造 AfterInvoke 回调上下文。
func guardInvokeCBC() *agentinterfaces.AgentCallbackContext {
	return agentinterfaces.NewAgentCallbackContext(nil,
		&agentinterfaces.InvokeInputs{}, nil)
}

// guardTestCard 守卫测试用的最小 AgentCard。
func guardTestCard() *agentschema.AgentCard {
	return agentschema.NewAgentCard()
}

// TestStreamCompletionState 中断态判定（_stream_completion_state 对齐）。
func TestStreamCompletionState(t *testing.T) {
	agent := newGuardTestAgent()
	d := NewDeepAdapter()
	d.instance = agent
	// loop session 缺席 → had_interaction 分支（Python 一比一）
	if got := d.streamCompletionState(true); got != "suspended" {
		t.Errorf("缺席+had_interaction 应 suspended，实际 %s", got)
	}
	if got := d.streamCompletionState(false); got != "completed" {
		t.Errorf("缺席无交互应 completed，实际 %s", got)
	}
	// 有中断态（ask_user 挂起）→ suspended
	loopSess := agent.LoopSession()
	if loopSess == nil {
		t.Skip("测试 agent 无 loop session，跳过状态分支")
	}
	loopSess.UpdateState(map[string]any{agentschema.InterruptionKey: &agentschema.ToolInterruptionState{
		BaseInterruptionState: agentschema.BaseInterruptionState{Iteration: 1},
		InterruptedTools: map[string]*agentschema.ToolInterruptEntry{
			"call-1": {ToolCall: &schema.ToolCall{ID: "call-1", Name: "ask_user"}},
		},
	}})
	if got := d.streamCompletionState(false); got != "suspended" {
		t.Errorf("interrupted_tools 非空应 suspended，实际 %s", got)
	}
	// 清态后 → completed
	loopSess.UpdateState(map[string]any{agentschema.InterruptionKey: nil})
	if got := d.streamCompletionState(false); got != "completed" {
		t.Errorf("清态后应 completed，实际 %s", got)
	}
}
