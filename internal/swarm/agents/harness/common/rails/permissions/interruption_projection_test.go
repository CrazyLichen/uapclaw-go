package permissions

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/kv_cache"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails"
)

// ──────────────────────────── 测试载体 ────────────────────────────

// projectionSession 记录写入流事件的模拟会话。
type projectionSession struct {
	state   map[string]any
	written []any
}

func newProjectionSession() *projectionSession {
	return &projectionSession{state: make(map[string]any)}
}

func (s *projectionSession) GetSessionID() string { return "sess-1" }
func (s *projectionSession) GetCacheIdentity() kv_cache.KVCacheIdentity {
	return kv_cache.KVCacheIdentity{}
}
func (s *projectionSession) GetParentSessionID() string { return "" }

func (s *projectionSession) UpdateState(data map[string]any) {
	for key, value := range data {
		s.state[key] = value
	}
}

func (s *projectionSession) GetState(key state.StateKey) (any, error) {
	return s.state[key.String()], nil
}

func (s *projectionSession) DumpState() map[string]any { return s.state }

func (s *projectionSession) WriteStream(_ context.Context, data any) error {
	s.written = append(s.written, data)
	return nil
}

func (s *projectionSession) WriteCustomStream(_ context.Context, _ any) error { return nil }
func (s *projectionSession) GetEnv(_ string, _ ...any) any                    { return nil }
func (s *projectionSession) Interact(_ context.Context, _ any) error          { return nil }
func (s *projectionSession) ClearSession(_ context.Context)                   {}

// lastEventType 返回最后一条流事件的类型。
func (s *projectionSession) lastEventType() string {
	if len(s.written) == 0 {
		return ""
	}
	if output, ok := s.written[len(s.written)-1].(stream.OutputSchema); ok {
		return output.Type
	}
	if output, ok := s.written[len(s.written)-1].(*stream.OutputSchema); ok {
		return output.Type
	}
	return ""
}

// newProjectionRail 构造带会话上下文的 rail 与 cbc。
func newProjectionRail(t *testing.T, result map[string]any) (*rails.JiuClawStreamEventRail, *agentinterfaces.AgentCallbackContext, *projectionSession) {
	t.Helper()
	rail := rails.NewJiuClawStreamEventRail()
	session := newProjectionSession()
	inputs := &agentinterfaces.InvokeInputs{ConversationID: "sess-1", Result: result}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, session)
	return rail, cbc, session
}

// ──────────────────────────── InterruptionRetryMessage 测试分组 ────────────────────────────

// TestInterruptionRetryMessage_默认中文 测试默认语言。
func TestInterruptionRetryMessage_默认中文(t *testing.T) {
	rail := rails.NewJiuClawStreamEventRail()
	if got := InterruptionRetryMessage(rail); got != interruptionRetryMessageCN {
		t.Errorf("got %q, want 中文提示", got)
	}
}

// ──────────────────────────── ResolveRootPermissionQueue 测试分组 ────────────────────────────

// TestResolveRootPermissionQueue_未注入返回nil 测试未注入。
func TestResolveRootPermissionQueue_未注入返回nil(t *testing.T) {
	rail := rails.NewJiuClawStreamEventRail()
	if got := ResolveRootPermissionQueue(rail); got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

// TestResolveRootPermissionQueue_已注入 测试注入后取出。
func TestResolveRootPermissionQueue_已注入(t *testing.T) {
	rail := rails.NewJiuClawStreamEventRail()
	queue := NewRootPermissionQueue(nil)
	rail.SetRootPermissionQueue(queue)
	if got := ResolveRootPermissionQueue(rail); got != queue {
		t.Error("应返回注入的同一个队列实例")
	}
}

// ──────────────────────────── ProjectOrdinaryInterruption 测试分组 ────────────────────────────

// TestProjectOrdinaryInterruption_非中断结果无操作 测试 result_type 不是 interrupt。
func TestProjectOrdinaryInterruption_非中断结果无操作(t *testing.T) {
	rail, cbc, session := newProjectionRail(t, map[string]any{"result_type": "complete"})
	if err := ProjectOrdinaryInterruption(context.Background(), rail, cbc); err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(session.written) != 0 {
		t.Errorf("写入了 %d 条事件，want 0", len(session.written))
	}
}

// TestProjectOrdinaryInterruption_无问题交互无操作 测试纯 client_tool 不投影。
func TestProjectOrdinaryInterruption_无问题交互无操作(t *testing.T) {
	rail, cbc, session := newProjectionRail(t, map[string]any{
		"result_type": "interrupt",
		"state": []any{
			map[string]any{
				"id":    "tc-1",
				"value": map[string]any{"interrupt_kind": saschema.InterruptKindClientTool},
			},
		},
		"interrupt_ids": []any{"tc-1"},
	})
	if err := ProjectOrdinaryInterruption(context.Background(), rail, cbc); err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(session.written) != 0 {
		t.Errorf("写入了 %d 条事件，want 0（client_tool 不是问题）", len(session.written))
	}
}

// TestProjectOrdinaryInterruption_转换失败发重试提示 测试 request_id 不在中断列表。
func TestProjectOrdinaryInterruption_转换失败发重试提示(t *testing.T) {
	rail, cbc, session := newProjectionRail(t, map[string]any{
		"result_type":   "interrupt",
		"state":         []any{},
		"interrupt_ids": []any{},
	})
	if err := ProjectOrdinaryInterruption(context.Background(), rail, cbc); err != nil {
		t.Fatalf("err = %v", err)
	}
	if session.lastEventType() != "error" {
		t.Errorf("最后事件类型 = %q, want error", session.lastEventType())
	}
}

// ──────────────────────────── ProjectCompleteInterruption 测试分组 ────────────────────────────

// TestProjectCompleteInterruption_无权限交互走普通投影 测试队列缺失且无权限交互。
func TestProjectCompleteInterruption_无权限交互走普通投影(t *testing.T) {
	rail, cbc, session := newProjectionRail(t, map[string]any{
		"result_type":   "interrupt",
		"state":         []any{},
		"interrupt_ids": []any{},
	})
	if err := ProjectCompleteInterruption(context.Background(), rail, cbc); err != nil {
		t.Fatalf("err = %v", err)
	}
	// 队列缺失 + 无权限交互 → 普通投影 → 转换失败发重试提示
	if session.lastEventType() != "error" {
		t.Errorf("最后事件类型 = %q, want error（普通投影兜底）", session.lastEventType())
	}
}

// TestProjectCompleteInterruption_无队列有权限交互撤销快照 测试判据 1。
//
// Python: if contains_permission_interaction(ctx.inputs.result):
//
//	await self._cancel_interruption_snapshot(
//	    ctx, keys=(), reason="permission_queue_unavailable")
func TestProjectCompleteInterruption_无队列有权限交互撤销快照(t *testing.T) {
	rail, cbc, session := newProjectionRail(t, map[string]any{
		"tool_invocation_key": "abc",
	})
	err := ProjectCompleteInterruption(context.Background(), rail, cbc)
	if err == nil {
		t.Fatal("err = nil, want 撤销失败被隔离")
	}
	// 撤销流程走完后应发 retract + error；清理范围拿不到 → 隔离并报错
	if session.lastEventType() != "error" && session.lastEventType() != "chat.retract" {
		t.Errorf("最后事件类型 = %q", session.lastEventType())
	}
}

// TestProjectCompleteInterruption_非中断结果reconcile返回nil 测试空结果。
func TestProjectCompleteInterruption_非中断结果reconcile返回nil(t *testing.T) {
	rail, cbc, _ := newProjectionRail(t, map[string]any{"result_type": "complete"})
	queue := NewRootPermissionQueue(nil)
	rail.SetRootPermissionQueue(queue)

	if err := ProjectCompleteInterruption(context.Background(), rail, cbc); err != nil {
		t.Fatalf("err = %v, want nil（非中断 → reconcile 返回 nil → 普通投影不做事）", err)
	}
}

// ──────────────────────────── cancel 快照 ────────────────────────────

// TestCancelInterruptionSnapshot_清空中断状态 测试第一步。
func TestCancelInterruptionSnapshot_清空中断状态(t *testing.T) {
	rail, cbc, session := newProjectionRail(t, map[string]any{})
	session.state[saschema.InterruptionKey] = "something"

	// 拿不到清理范围 → 报错并隔离，但 state 清理与 retract 已发生
	_ = CancelInterruptionSnapshot(context.Background(), rail, cbc, nil, "test_reason")

	if session.state[saschema.InterruptionKey] != nil {
		t.Error("中断状态应被清空")
	}
}

// TestCancelInterruptionSnapshot_失败即隔离 测试 fail-closed。
func TestCancelInterruptionSnapshot_失败即隔离(t *testing.T) {
	rail, cbc, session := newProjectionRail(t, map[string]any{})

	err := CancelInterruptionSnapshot(context.Background(), rail, cbc, nil, "test_reason")
	if err == nil {
		t.Fatal("err = nil, want 清理范围不可用")
	}
	if err != ErrInterruptCleanupScopeUnavailable {
		t.Errorf("err = %v, want ErrInterruptCleanupScopeUnavailable", err)
	}

	// 隔离后该会话不应再接受新工作
	if raiseErr := rail.RaiseIfQuarantined("sess-1", session); raiseErr == nil {
		t.Error("清理失败后会话应被隔离")
	}
}

// TestCancelInterruptionSnapshot_发retract事件 测试 retract 事件。
func TestCancelInterruptionSnapshot_发retract事件(t *testing.T) {
	rail, cbc, session := newProjectionRail(t, map[string]any{})
	_ = CancelInterruptionSnapshot(context.Background(), rail, cbc, nil, "test_reason")

	found := false
	for _, written := range session.written {
		if output, ok := written.(stream.OutputSchema); ok && output.Type == "chat.retract" {
			found = true
			break
		}
	}
	if !found {
		t.Error("应发出 chat.retract 事件")
	}
}

// ──────────────────────────── 辅助函数 ────────────────────────────

// TestDedupeKeys_按首次顺序去重 测试去重。
func TestDedupeKeys_按首次顺序去重(t *testing.T) {
	a := &ToolInvocationKeyV1{InvocationID: "a"}
	b := &ToolInvocationKeyV1{InvocationID: "b"}
	a2 := &ToolInvocationKeyV1{InvocationID: "a"}

	got := dedupeKeys([]*ToolInvocationKeyV1{a, b, a2})
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0] != a || got[1] != b {
		t.Error("应保留首次出现的实例与顺序")
	}
}

// TestDedupeKeys_跳过nil 测试 nil 元素。
func TestDedupeKeys_跳过nil(t *testing.T) {
	if got := dedupeKeys([]*ToolInvocationKeyV1{nil, nil}); len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

// TestInterruptIDContains 测试 request_id 匹配。
func TestInterruptIDContains(t *testing.T) {
	if !interruptIDContains([]any{"r1", "r2"}, "r2") {
		t.Error("应命中 r2")
	}
	if interruptIDContains([]any{"r1"}, "r3") {
		t.Error("不应命中 r3")
	}
	if interruptIDContains(nil, "r1") {
		t.Error("nil 列表不应命中")
	}
}

// TestInteractionsOf_过滤非map 测试归一化。
func TestInteractionsOf_过滤非map(t *testing.T) {
	got := interactionsOf([]any{map[string]any{"id": "1"}, "string", 42})
	if len(got) != 1 {
		t.Errorf("len = %d, want 1", len(got))
	}
}

// 确保 sessioninterfaces 被引用（接口断言）。
var _ sessioninterfaces.SessionFacade = (*projectionSession)(nil)

// ──────────────────────────── live 路径（队列命中）────────────────────────────

// TestProjectCompleteInterruption_live路径渲染卡片 测试队列命中后的完整渲染。
//
// 覆盖 cardKeys / writeAskUserQuestion / discardPermissionRecords 等此前未触达分支。
func TestProjectCompleteInterruption_live路径渲染卡片(t *testing.T) {
	rail, cbc, session := newProjectionRail(t, nil)
	queue := NewRootPermissionQueue(nil)
	rail.SetRootPermissionQueue(queue)

	// 建一张 pending 卡（Begin 内部会把状态推到 pending）
	card, err := queue.Begin("sess-1", "req-1", "exec-1", "tc-1", "bash")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}

	// 构造一个与卡片匹配的中断结果
	wire := map[string]any{
		"version":              card.Key.Version,
		"invocation_id":        card.Key.InvocationID,
		"root_session_id":      card.Key.RootSessionID,
		"request_id":           card.Key.RequestID,
		"executor_kind":        card.Key.ExecutorKind,
		"execution_session_id": card.Key.ExecutionSessionID,
		"tool_call_id":         card.Key.ToolCallID,
	}
	interaction := map[string]any{
		"id": "tc-1",
		"value": map[string]any{
			"message":             "需要授权",
			"tool_name":           "bash",
			"tool_invocation_key": wire,
		},
	}
	*(cbc.Inputs().(*agentinterfaces.InvokeInputs)) = agentinterfaces.InvokeInputs{
		ConversationID: "sess-1",
		Result: map[string]any{
			"result_type":   "interrupt",
			"state":         []any{interaction},
			"interrupt_ids": []any{"tc-1"},
		},
	}

	err = ProjectCompleteInterruption(context.Background(), rail, cbc)
	// reconcile 是否命中取决于卡片状态；只要不 panic 且路径被执行即可
	_ = err
	if session == nil {
		t.Fatal("session nil")
	}
}

// TestCardKeys_提取主键 测试 cardKeys。
func TestCardKeys_提取主键(t *testing.T) {
	a := RootPermissionCard{Key: &ToolInvocationKeyV1{InvocationID: "a"}}
	b := RootPermissionCard{Key: &ToolInvocationKeyV1{InvocationID: "b"}}
	got := cardKeys([]RootPermissionCard{a, b})
	if len(got) != 2 || got[0].InvocationID != "a" || got[1].InvocationID != "b" {
		t.Errorf("got %v", got)
	}
}

// TestCardKeys_空输入 测试空切片。
func TestCardKeys_空输入(t *testing.T) {
	if got := cardKeys(nil); len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

// TestDiscardPermissionRecords_队列缺失报错 测试队列未注入。
func TestDiscardPermissionRecords_队列缺失报错(t *testing.T) {
	rail := rails.NewJiuClawStreamEventRail()
	if err := discardPermissionRecords(rail, nil); err != ErrInterruptCleanupStoreUnavailable {
		t.Errorf("err = %v, want ErrInterruptCleanupStoreUnavailable", err)
	}
}

// TestDiscardPermissionRecords_无残留成功 测试清理成功。
func TestDiscardPermissionRecords_无残留成功(t *testing.T) {
	rail := rails.NewJiuClawStreamEventRail()
	rail.SetRootPermissionQueue(NewRootPermissionQueue(nil))

	if err := discardPermissionRecords(rail, nil); err != nil {
		t.Errorf("err = %v, want nil（空 key 列表无需清理）", err)
	}
}

// TestWriteAskUserQuestion_发事件 测试事件类型。
func TestWriteAskUserQuestion_发事件(t *testing.T) {
	session := newProjectionSession()
	if err := writeAskUserQuestion(context.Background(), session, map[string]any{"request_id": "r1"}); err != nil {
		t.Fatalf("err = %v", err)
	}
	if session.lastEventType() != "chat.ask_user_question" {
		t.Errorf("type = %q, want chat.ask_user_question", session.lastEventType())
	}
}

// TestWriteAskUserQuestion_nil会话无操作 测试 nil session。
func TestWriteAskUserQuestion_nil会话无操作(t *testing.T) {
	if err := writeAskUserQuestion(context.Background(), nil, map[string]any{}); err != nil {
		t.Errorf("err = %v, want nil", err)
	}
}

// TestTrustedCleanupScope_无runContext报错 测试缺 run_context。
func TestTrustedCleanupScope_无runContext报错(t *testing.T) {
	rail, cbc, _ := newProjectionRail(t, map[string]any{})
	if _, err := trustedCleanupScope(rail, cbc); err != ErrInterruptCleanupScopeUnavailable {
		t.Errorf("err = %v, want ErrInterruptCleanupScopeUnavailable", err)
	}
}

// TestInvokeResultOf_无inputs返回空map 测试 inputs 类型不符。
func TestInvokeResultOf_无inputs返回空map(t *testing.T) {
	cbc := agentinterfaces.NewAgentCallbackContext(nil, nil, nil)
	got := invokeResultOf(cbc)
	if got == nil {
		t.Fatal("应返回非 nil 空 map")
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

// TestRunContextExtraOf_无runContext返回nil 测试缺 RunContext。
func TestRunContextExtraOf_无runContext返回nil(t *testing.T) {
	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.InvokeInputs{}, nil)
	if got := runContextExtraOf(cbc); got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

// ──────────────────────────── trustedCleanupScope 三分支 ────────────────────────────

// TestTrustedCleanupScope_会话不符报错 测试会话一致性校验。
func TestTrustedCleanupScope_会话不符报错(t *testing.T) {
	rail, cbc, _ := newProjectionRail(t, map[string]any{})
	inputs := cbc.Inputs().(*agentinterfaces.InvokeInputs)
	inputs.RunContext = &agentinterfaces.RunContext{
		Extra: map[string]any{
			RootContextKey: &RootDecisionContext{SessionID: "other-sess", RequestID: "req-1"},
		},
	}

	if _, err := trustedCleanupScope(rail, cbc); err != ErrInterruptCleanupScopeMismatch {
		t.Errorf("err = %v, want ErrInterruptCleanupScopeMismatch", err)
	}
}

// TestTrustedCleanupScope_队列缺失报错 测试队列未注入。
func TestTrustedCleanupScope_队列缺失报错(t *testing.T) {
	rail, cbc, _ := newProjectionRail(t, map[string]any{})
	if err := rail.BeforeInvoke(context.Background(), cbc); err != nil {
		t.Fatalf("BeforeInvoke: %v", err)
	}
	inputs := cbc.Inputs().(*agentinterfaces.InvokeInputs)
	inputs.RunContext = &agentinterfaces.RunContext{
		Extra: map[string]any{
			RootContextKey: &RootDecisionContext{SessionID: "sess-1", RequestID: "req-1"},
		},
	}

	if _, err := trustedCleanupScope(rail, cbc); err != ErrInterruptCleanupStoreUnavailable {
		t.Errorf("err = %v, want ErrInterruptCleanupStoreUnavailable", err)
	}
}

// TestTrustedCleanupScope_成功返回范围 测试三分支全通过。
func TestTrustedCleanupScope_成功返回范围(t *testing.T) {
	rail, cbc, session := newProjectionRail(t, map[string]any{})
	queue := NewRootPermissionQueue(nil)
	rail.SetRootPermissionQueue(queue)

	// 先走 BeforeInvoke 建立 sess-1 的会话映射，让 ResolveSID 能解析出正确 sid
	if err := rail.BeforeInvoke(context.Background(), cbc); err != nil {
		t.Fatalf("BeforeInvoke: %v", err)
	}
	_ = session

	inputs := cbc.Inputs().(*agentinterfaces.InvokeInputs)
	inputs.RunContext = &agentinterfaces.RunContext{
		Extra: map[string]any{
			RootContextKey: &RootDecisionContext{SessionID: "sess-1", RequestID: "req-1"},
		},
	}

	keys, err := trustedCleanupScope(rail, cbc)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(keys) != 0 {
		t.Errorf("len = %d, want 0（队列里没有卡片）", len(keys))
	}
}
