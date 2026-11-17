package externalcli

import (
	"context"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	harness_protocol "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
	harnessproviders "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_providers"
)

// fakeReliabilityCtx runtimeReliabilityContext 测试替身（记账各方法调用）。
type fakeReliabilityCtx struct {
	runtimeReliabilityContext
	beginPhase    string
	beginRound    *int
	models        []string
	retries       []string
	finalized     int
	memberErrored bool
}

func (f *fakeReliabilityCtx) HasFinalized() bool { return false }

func (f *fakeReliabilityCtx) BeginAttempt(phase schema.ExternalRuntimePhase, roundID *int) {
	f.beginPhase, f.beginRound = phase, roundID
}

func (f *fakeReliabilityCtx) UpdateModel(model *string) {
	if model != nil {
		f.models = append(f.models, *model)
	}
}

func (f *fakeReliabilityCtx) PublishRetrying(category schema.ExternalRuntimeFailureCategory,
	reason *schema.ExternalRuntimeFailureReason, summary string) error {
	f.retries = append(f.retries, string(category)+":"+summary)
	return nil
}

func (f *fakeReliabilityCtx) FinalizeFailure(category schema.ExternalRuntimeFailureCategory,
	reason *schema.ExternalRuntimeFailureReason, summary string) error {
	f.finalized++
	return nil
}

func (f *fakeReliabilityCtx) MarkMemberError() error {
	f.memberErrored = true
	return nil
}

// newEventTestRuntime 构造带订阅收集的事件测试 runtime。
func newEventTestRuntime(t *testing.T) (*ExternalHarnessMemberRuntime, *[]string) {
	t.Helper()
	r, err := NewExternalHarnessMemberRuntime(&fakeHarnessProtocol{},
		func(_ any) (*harness_protocol.HarnessContext, error) {
			return &harness_protocol.HarnessContext{}, nil
		},
		ExternalHarnessMemberRuntimeOptions{AgentKind: "codex"})
	if err != nil {
		t.Fatalf("构造: %v", err)
	}
	var got []string
	r.Subscribe(
		func(old, new harness_protocol.HarnessState, sessionID string) {
			got = append(got, "state:"+string(new))
		},
		func(kind string, roundID int, turnID string, result *harness_protocol.TurnResult) {
			got = append(got, "round:"+kind)
		},
	)
	return r, &got
}

// envelopeOf 构造一个信封（value 载荷、turn t-1）。
func envelopeOf(t *testing.T, payload harness_protocol.HarnessEventPayload) harness_protocol.HarnessEvent {
	t.Helper()
	return harness_protocol.HarnessEvent{Event: payload, TurnID: "t-1"}
}

// envelopeWithID 构造一个带 provider 载荷的信封。
func envelopeWithID(t *testing.T, payload harness_protocol.HarnessEventPayload) harness_protocol.HarnessEvent {
	t.Helper()
	return harness_protocol.HarnessEvent{Event: payload, TurnID: "t-1"}
}

func TestOnTurnEvent_Started与Failed(t *testing.T) {
	r, got := newEventTestRuntime(t)
	ctx := &fakeReliabilityCtx{}
	r.mu.Lock()
	r.reliabilityCtx = ctx
	r.mu.Unlock()

	r.onEvent(context.Background(), envelopeOf(t,
		harness_protocol.TurnLifecycleEvent{Kind: harness_protocol.TurnEventKindStarted}))
	if r.roundSeq != 1 {
		t.Fatalf("started 未递增 roundSeq: %d", r.roundSeq)
	}
	if ctx.beginPhase != schema.ExternalRuntimePhaseTurn || ctx.beginRound == nil || *ctx.beginRound != 1 {
		t.Fatalf("begin_attempt 参数不符: %s %v", ctx.beginPhase, ctx.beginRound)
	}
	res := &harness_protocol.TurnResult{}
	r.onEvent(context.Background(), envelopeOf(t,
		harness_protocol.TurnLifecycleEvent{Kind: harness_protocol.TurnEventKindFailed, Result: res}))
	r.asyncWG.Wait() // finalize 经 asyncGo 异步派发（D-15.16-a2），等收口
	if ctx.finalized != 1 {
		t.Fatal("failed 未 finalize")
	}
	if len(*got) != 2 || (*got)[0] != "round:started" || (*got)[1] != "round:failed" {
		t.Fatalf("round 回调序不符: %v", *got)
	}
}

func TestOnEvent_StateChanged与模型事件(t *testing.T) {
	r, got := newEventTestRuntime(t)
	ctx := &fakeReliabilityCtx{}
	r.mu.Lock()
	r.reliabilityCtx = ctx
	r.mu.Unlock()

	r.onEvent(context.Background(), envelopeOf(t,
		harness_protocol.StateChangedEvent{Old: harness_protocol.HarnessStateRunning, New: harness_protocol.HarnessStateIdle}))
	if len(*got) != 1 || (*got)[0] != "state:"+string(harness_protocol.HarnessStateIdle) {
		t.Fatalf("state 回调未触发: %v", *got)
	}
	// session/model_changed 记录 reported_model（TrimSpace）
	payload, err := harness_protocol.FreezeJsonObject(map[string]any{"model": " deepseek-v3 "})
	if err != nil {
		t.Fatal(err)
	}
	r.onEvent(context.Background(), envelopeWithID(t,
		harness_protocol.ProviderEvent{EventType: modelChangedEvent, Payload: payload}))
	r.mu.Lock()
	m := r.reportedModel
	r.mu.Unlock()
	if m != "deepseek-v3" {
		t.Fatalf("reported_model 未 TrimSpace 记录: %q", m)
	}
	if len(ctx.models) != 1 || ctx.models[0] != "deepseek-v3" {
		t.Fatalf("reliability update_model 未收到: %v", ctx.models)
	}
}

func TestOnEvent_AuthFallback日志面(t *testing.T) {
	r, _ := newEventTestRuntime(t)
	payload, err := harness_protocol.FreezeJsonObject(map[string]any{"model": "fallback-m"})
	if err != nil {
		t.Fatal(err)
	}
	// 不 panic、不记 reported_model（AUTH_FALLBACK 只走日志）
	r.onEvent(context.Background(), envelopeWithID(t,
		harness_protocol.ProviderEvent{EventType: harness_protocol.AuthFallbackEventType, Payload: payload}))
	r.mu.Lock()
	m := r.reportedModel
	r.mu.Unlock()
	if m != "" {
		t.Fatalf("auth_fallback 不应记录 reported_model: %q", m)
	}
}

func TestOnDiagnostic_Retrying(t *testing.T) {
	r, _ := newEventTestRuntime(t)
	ctx := &fakeReliabilityCtx{}
	r.mu.Lock()
	r.reliabilityCtx = ctx
	r.mu.Unlock()

	data, err := harness_protocol.FreezeJsonObject(map[string]any{
		"kind": "retrying", "category": "rate_limited", "code": "429"})
	if err != nil {
		t.Fatal(err)
	}
	r.onEvent(context.Background(), envelopeOf(t,
		harness_protocol.DiagnosticEvent{Message: "sdk hiccup", Data: data}))
	if len(ctx.retries) != 1 || !strings.Contains(ctx.retries[0], "rate_limited") {
		t.Fatalf("retrying 未发布: %v", ctx.retries)
	}
	// kind != retrying 不发布
	other, err := harness_protocol.FreezeJsonObject(map[string]any{"kind": "other"})
	if err != nil {
		t.Fatal(err)
	}
	r.onEvent(context.Background(), envelopeOf(t,
		harness_protocol.DiagnosticEvent{Data: other}))
	if len(ctx.retries) != 1 {
		t.Fatal("非 retrying 不应发布")
	}
	// ctx nil 短路
	r.mu.Lock()
	r.reliabilityCtx = nil
	r.mu.Unlock()
	r.onEvent(context.Background(), envelopeOf(t,
		harness_protocol.DiagnosticEvent{Message: "x", Data: data}))
}

func TestOnEvent_未映射载荷静默(t *testing.T) {
	r, got := newEventTestRuntime(t)
	r.onEvent(context.Background(), envelopeOf(t,
		harness_protocol.OutputEvent{}))
	if len(*got) != 0 {
		t.Fatalf("未映射载荷不应触发回调: %v", *got)
	}
}

// 编译期锚：onEvent 签名必须满足 EventObserver 缝。
var _ = func(r *ExternalHarnessMemberRuntime) harnessproviders.EventObserver { return r.onEvent }

// envelopeWithItem 构造一个 ItemLifecycle 信封（可指定 item id）。
func envelopeItemID(t *testing.T, payload harness_protocol.HarnessEventPayload, itemID string) harness_protocol.HarnessEvent {
	t.Helper()
	return harness_protocol.HarnessEvent{Event: payload, TurnID: "t-1", ItemID: itemID}
}

// newEventTestRuntimeWithSink 构造带活动 sink 收集的 runtime。
func newEventTestRuntimeWithSink(t *testing.T) (*ExternalHarnessMemberRuntime, *[]*AgentActivityEvent) {
	t.Helper()
	r, _ := newEventTestRuntime(t)
	var events []*AgentActivityEvent
	r.mu.Lock()
	r.activitySink = func(ev *AgentActivityEvent) { events = append(events, ev) }
	r.activityIdentity = [4]string{"agent-1", "exec", "label", "corr-1"}
	r.mu.Unlock()
	return r, &events
}

// mustFreezeScalar 构造冻结标量载荷。
func mustFreezeScalar(t *testing.T, value any) harness_protocol.JsonObject {
	t.Helper()
	obj, err := harness_protocol.FreezeJsonObject(map[string]any{"v": value})
	if err != nil {
		t.Fatal(err)
	}
	return obj
}

func TestOnToolItem_五条Drop(t *testing.T) {
	r, sinkCalls := newEventTestRuntimeWithSink(t)
	// 非 tool
	r.onEvent(context.Background(), envelopeItemID(t, harness_protocol.ItemLifecycleEvent{
		Kind: harness_protocol.ItemEventKindStarted, ItemType: "step",
		Data: mustJsonObject(t, map[string]any{"tool_name": "x"})}, "i-1"))
	// data 非 dict
	r.onEvent(context.Background(), envelopeItemID(t, harness_protocol.ItemLifecycleEvent{
		Kind: harness_protocol.ItemEventKindStarted, ItemType: "tool",
		Data: mustFreezeScalar(t, "oops")}, "i-1"))
	// 无 item_id
	r.onEvent(context.Background(), envelopeOf(t, harness_protocol.ItemLifecycleEvent{
		Kind: harness_protocol.ItemEventKindStarted, ItemType: "tool",
		Data: mustJsonObject(t, map[string]any{"tool_name": "x"})}))
	// 无 name
	r.onEvent(context.Background(), envelopeItemID(t, harness_protocol.ItemLifecycleEvent{
		Kind: harness_protocol.ItemEventKindStarted, ItemType: "tool",
		Data: mustJsonObject(t, map[string]any{})}, "i-1"))
	// kind=updated 不映射
	r.onEvent(context.Background(), envelopeItemID(t, harness_protocol.ItemLifecycleEvent{
		Kind: harness_protocol.ItemEventKindUpdated, ItemType: "tool",
		Data: mustJsonObject(t, map[string]any{"tool_name": "x"})}, "i-1"))
	if len(*sinkCalls) != 0 {
		t.Fatalf("五条 drop 全部静默: %+v", *sinkCalls)
	}
}

func TestOnToolItem_StartedCompleted映射(t *testing.T) {
	r, sinkCalls := newEventTestRuntimeWithSink(t)
	r.onEvent(context.Background(), envelopeItemID(t, harness_protocol.ItemLifecycleEvent{
		Kind: harness_protocol.ItemEventKindStarted, ItemType: "tool",
		Data: mustJsonObject(t, map[string]any{"tool_name": "bash", "arguments": map[string]any{"cmd": "ls"}}),
	}, "i-9"))
	r.onEvent(context.Background(), envelopeItemID(t, harness_protocol.ItemLifecycleEvent{
		Kind: harness_protocol.ItemEventKindCompleted, ItemType: "tool",
		Data: mustJsonObject(t, map[string]any{"tool_name": "bash", "result": "a\nb"}),
	}, "i-9"))
	calls := *sinkCalls
	if len(calls) != 2 ||
		calls[0].ActivityType != "tool_call" || calls[0].ToolCallID != "i-9" || *calls[0].ToolName != "bash" ||
		calls[1].ActivityType != "tool_result" || calls[1].Content == nil || *calls[1].Content != "a\nb" {
		t.Fatalf("映射不符: %+v", calls)
	}
	// 单行紧凑预览
	if strings.Contains(*calls[0].Content, "\n") {
		t.Fatalf("预览须单行: %q", *calls[0].Content)
	}
	// 身份四元组随事件携带
	if calls[0].AgentID != "agent-1" || calls[0].Phase != "exec" || calls[0].Label != "label" || calls[0].CorrelationID != "corr-1" {
		t.Fatalf("身份四元组漂移: %+v", calls[0])
	}
}

func TestEmitActivity_sinkNil与异常吞(t *testing.T) {
	r, _ := newEventTestRuntime(t)
	r.mu.Lock()
	r.activitySink = nil
	r.mu.Unlock()
	r.emitActivity("tool_call", "i-1", strPtrT("bash"), strPtrT("ls")) // nil sink 静默
	boom := false
	r.mu.Lock()
	r.activitySink = func(*AgentActivityEvent) { boom = true; panic("sink blew up") }
	r.mu.Unlock()
	r.emitActivity("tool_call", "i-1", strPtrT("bash"), strPtrT("ls")) // panic 吞为 Debug
	if !boom {
		t.Fatal("sink 未被调用")
	}
}

func TestFinalizeTurnFailure(t *testing.T) {
	r, _ := newEventTestRuntime(t)
	ctx := &fakeReliabilityCtx{}
	// ctx nil 短路
	r.mu.Lock()
	r.reliabilityCtx = nil
	r.mu.Unlock()
	r.finalizeTurnFailure(nil)
	r.mu.Lock()
	r.reliabilityCtx = ctx
	r.mu.Unlock()
	r.finalizeTurnFailure(&harness_protocol.TurnResult{
		Error: &harness_protocol.TurnError{Message: "boom", Category: strPtrT("auth_required")}})
	if ctx.finalized != 1 {
		t.Fatal("未 finalize")
	}
}

func TestFinalizeStartupFailure(t *testing.T) {
	r, _ := newEventTestRuntime(t)
	ctx := &fakeReliabilityCtx{}
	r.mu.Lock()
	r.reliabilityCtx = ctx
	r.mu.Unlock()
	pse := &harnessproviders.ProviderStartupError{
		Msg: "spawn died",
		Err: harness_protocol.TurnError{Message: "no binary", Category: strPtrT("process_start_failed")},
	}
	r.finalizeStartupFailure(pse)
	if ctx.finalized != 1 || !ctx.memberErrored {
		t.Fatalf("startup finalize/mark 不符: finalized=%d errored=%v", ctx.finalized, ctx.memberErrored)
	}
	// 非 ProviderStartupError：reason 由 str(exc) 构造
	ctx2 := &fakeReliabilityCtx{}
	r.mu.Lock()
	r.reliabilityCtx = ctx2
	r.mu.Unlock()
	r.finalizeStartupFailure(context.DeadlineExceeded)
	if ctx2.finalized != 1 || !ctx2.memberErrored {
		t.Fatal("非启动错误形状 finalize 不符")
	}
}

func TestOnProviderInteraction(t *testing.T) {
	r, _ := newEventTestRuntime(t)
	resp := r.onProviderInteraction(&harness_protocol.ProviderInteractionRequest{RequestID: "r1", RequestType: "other"})
	if resp.Status != harness_protocol.InteractionResponseStatusDeclined {
		t.Fatal("非 auth_fallback 应 DECLINED")
	}
	r.mu.Lock()
	r.promoteFallbackModel = nil
	r.mu.Unlock()
	resp = r.onProviderInteraction(&harness_protocol.ProviderInteractionRequest{RequestID: "r2", RequestType: harness_protocol.AuthFallbackRequestType})
	if resp.Status != harness_protocol.InteractionResponseStatusCompleted {
		t.Fatal("无 promote 应 COMPLETED")
	}
	r.mu.Lock()
	r.promoteFallbackModel = func(_ context.Context) (bool, error) { return false, nil }
	r.mu.Unlock()
	resp = r.onProviderInteraction(&harness_protocol.ProviderInteractionRequest{RequestID: "r3", RequestType: harness_protocol.AuthFallbackRequestType})
	if resp.Status != harness_protocol.InteractionResponseStatusDeclined {
		t.Fatal("promote false 应 DECLINED")
	}
}

func TestRecordEvent_无Recorder短路(t *testing.T) {
	r, _ := newEventTestRuntime(t)
	r.onEvent(context.Background(), envelopeOf(t,
		harness_protocol.TurnLifecycleEvent{Kind: harness_protocol.TurnEventKindStarted})) // recorder nil，静默
}

func TestRecordInput(t *testing.T) {
	r, _ := newEventTestRuntime(t)
	// recorder nil 短路
	r.recordInput(&harness_protocol.SendReceipt{TurnID: "t-5"}, "hello")
	// receipt nil 短路（构造 recorder 后行为由集成用例覆盖真实记录链）
}
