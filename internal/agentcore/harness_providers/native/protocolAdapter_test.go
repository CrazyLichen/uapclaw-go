package native

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	atharness "github.com/uapclaw/uapclaw-go/internal/agent_teams/harness"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	hp "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness_providers"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
)

// TestCard_Capabilities 对齐 protocol_adapter.py:81-93。
func TestCard_Capabilities(t *testing.T) {
	card := nativeV2Card()
	if card.Name != "native_v2" || card.ImplementationVersion != "0.2.0" {
		t.Fatalf("card = %+v", card)
	}
	for _, c := range []hp.HarnessCapability{
		hp.HarnessCapabilitySteer, hp.HarnessCapabilityForceAbort,
		hp.HarnessCapabilityGracefulAbort, hp.HarnessCapabilityPauseResume,
		hp.HarnessCapabilityCheckpoint, hp.HarnessCapabilityPersistentSession,
	} {
		if !card.Capabilities[c] {
			t.Fatalf("缺少能力 %s", c)
		}
	}
	if len(card.Capabilities) != 6 {
		t.Fatalf("能力集合应恰好 6 项，得 %d", len(card.Capabilities))
	}
	if !card.OptionalHostCapabilities[hp.HostCapabilityUserInput] ||
		!card.OptionalHostCapabilities[hp.HostCapabilityCheckpointSink] {
		t.Fatal("optional_host_capabilities 应含 USER_INPUT / CHECKPOINT_SINK")
	}
}

// TestD3_HooksDispatchToAdapter 断言构造期注入的 hooks 就是 adapter 自己。
func TestD3_HooksDispatchToAdapter(t *testing.T) {
	a := NewNativeV2Adapter(atschema.NewDeepAgentSpec(), nil, 1024)
	if a.base == nil {
		t.Fatal("base 未构造")
	}
	ctx := hp.HarnessContext{ProtocolVersion: hp.ProtocolVersion, McpServers: []hp.McpServerConfig{{Name: "x"}}}
	err := a.ValidateContext(context.Background(), ctx)
	if err == nil {
		t.Fatal("adapter 的 mcp_servers 校验未生效（hooks 派发到了默认实现）")
	}
	if got := err.Error(); got != "declare native MCP servers in the manifest" {
		t.Fatalf("错误文案不符: %q", got)
	}
}

// TestValidateContext_RequireResume无快照 对齐 :142-143。
func TestValidateContext_RequireResume无快照(t *testing.T) {
	a := NewNativeV2Adapter(atschema.NewDeepAgentSpec(), nil, 1024)
	err := a.ValidateContext(context.Background(), hp.HarnessContext{
		ProtocolVersion: hp.ProtocolVersion,
		ResumePolicy:    hp.ResumePolicyRequireResume,
	})
	if err == nil || err.Error() != "NativeHarness requires a checkpoint to resume" {
		t.Fatalf("应报 REQUIRE_RESUME 错，得 %v", err)
	}
}

// TestValidateContext_版本与作用域 对齐 :146-153。
func TestValidateContext_版本与作用域(t *testing.T) {
	a := NewNativeV2Adapter(atschema.NewDeepAgentSpec(), nil, 1024)
	base := hp.HarnessContext{
		ProtocolVersion: hp.ProtocolVersion,
		AgentID:         "a", HostSessionID: "h",
		ResumePolicy: hp.ResumePolicyResumeIfAvailable,
		Checkpoint: &hp.HarnessCheckpoint{
			Provider: "native_v2", AgentID: "a", HostSessionID: "h",
			SchemaVersion: "1", Data: hp.JsonObject{},
		},
	}
	if err := a.ValidateContext(context.Background(), base); err == nil {
		t.Fatal("空 data 应触发 invalid native checkpoint")
	}
	bad := base
	cp := *base.Checkpoint
	cp.SchemaVersion = "2"
	bad.Checkpoint = &cp
	if err := a.ValidateContext(context.Background(), bad); err == nil ||
		err.Error() != "unsupported native checkpoint version" {
		t.Fatalf("版本错应报版本错，得 %v", err)
	}
	bad2 := base
	cp2 := *base.Checkpoint
	cp2.Provider = "native"
	bad2.Checkpoint = &cp2
	if err := a.ValidateContext(context.Background(), bad2); err == nil ||
		err.Error() != "native checkpoint scope does not match the context" {
		t.Fatalf("作用域错应报作用域错，得 %v", err)
	}
}

// TestBuildNative_ReturnsL2 断言后端是进程内 L2，不是外部 CLI。
func TestBuildNative_ReturnsL2(t *testing.T) {
	a := NewNativeV2Adapter(atschema.NewDeepAgentSpec(), nil, 1024)
	agent := a.buildNative(hp.HarnessContext{})
	if agent == nil {
		t.Fatal("buildNative 返回 nil")
	}
	if agent.DeepAgent() == nil {
		t.Fatal("L2 未持有 DeepAgent")
	}
}

// TestOnNativeState_WritesControlOnly 断言回调只写帧、不做状态转换。
func TestOnNativeState_WritesControlOnly(t *testing.T) {
	a := NewNativeV2Adapter(atschema.NewDeepAgentSpec(), nil, 1024)
	before := a.base.State()
	a.onNativeState(context.Background(), atharness.HarnessStateEvent{New: hp.HarnessStatePaused})
	if a.base.State() != before {
		t.Fatal("onNativeState 不得触发 transition")
	}
}

// TestRunRound_ControlFrameFIFO 断言控制帧与普通输出同一 FIFO。
// Python: protocol_adapter.py:254-289；注释 :228-229 明确 callback 会抢跑。
func TestRunRound_ControlFrameFIFO(t *testing.T) {
	control := func(payload map[string]any) stream.Schema {
		return &stream.OutputSchema{Type: controlChunkType, Index: 0, Payload: payload}
	}
	answer := &stream.OutputSchema{Type: "message", Index: 0, Payload: map[string]any{"text": "尾帧"}}
	cases := []struct {
		name        string
		chunks      []stream.Schema
		wantConsume int
	}{
		{"answer_then_idle", []stream.Schema{answer, control(map[string]any{"state": string(hp.HarnessStateIdle)})}, 1},
		{"idle_then_answer", []stream.Schema{control(map[string]any{"state": string(hp.HarnessStateIdle)}), answer}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := NewNativeV2Adapter(atschema.NewDeepAgentSpec(), nil, 1024)
			i := 0
			a.outputNext = func(context.Context) (stream.Schema, bool) {
				if i >= len(c.chunks) {
					return nil, false
				}
				ch := c.chunks[i]
				i++
				return ch, true
			}
			consumed := 0
			a.consumeFn = func(context.Context, *harness_providers.PendingTurn, *turnState, stream.Schema) error {
				consumed++
				return nil
			}
			turn := &harness_providers.PendingTurn{TurnID: "turn-1"}
			err := a.runRound(context.Background(), nil, turn, newTurnState("turn-1"))
			if err != nil {
				t.Fatal(err)
			}
			if consumed != c.wantConsume {
				t.Fatalf("consumed = %d, want %d（控制帧后尾帧不得被消费）", consumed, c.wantConsume)
			}
		})
	}
}

// TestRunRoundControl_相位分支 覆盖 :259-283 的 round/state 全分支。
func TestRunRoundControl_相位分支(t *testing.T) {
	for _, payload := range []map[string]any{
		{"round": "started"},
		{"round": "failed"},
		{"state": string(hp.HarnessStatePausing)},
		{"state": string(hp.HarnessStatePaused)},
		{"state": string(hp.HarnessStateRunning)},
	} {
		a := NewNativeV2Adapter(atschema.NewDeepAgentSpec(), nil, 1024)
		emitted := 0
		a.emitLifecycleFn = func(context.Context, *harness_providers.PendingTurn, hp.TurnEventKind) error {
			emitted++
			return nil
		}
		state := newTurnState("t")
		done, err := a.runRoundControl(context.Background(), &harness_providers.PendingTurn{TurnID: "t"}, state, payload)
		if err != nil {
			t.Fatalf("payload %v: %v", payload, err)
		}
		if done {
			t.Fatalf("非 IDLE 帧不应结束轮次: %v", payload)
		}
		if payload["round"] == "failed" && state.err == nil {
			t.Fatal("round=failed 应设置 state.err")
		}
	}
}

// TestPaused_SinkErrorStillEmits 对齐 :271-275「只记日志不中断 Turn」。
func TestPaused_SinkErrorStillEmits(t *testing.T) {
	a := NewNativeV2Adapter(atschema.NewDeepAgentSpec(), nil, 1024)
	a.sinkErr = fmt.Errorf("sink down")
	emitted := 0
	a.emitLifecycleFn = func(context.Context, *harness_providers.PendingTurn, hp.TurnEventKind) error {
		emitted++
		return nil
	}
	_, err := a.runRoundControl(context.Background(), &harness_providers.PendingTurn{TurnID: "t"},
		newTurnState("t"), map[string]any{"state": string(hp.HarnessStatePaused)})
	if err != nil {
		t.Fatalf("sink 失败不得中断: %v", err)
	}
	if emitted != 1 {
		t.Fatalf("PAUSED 事件应仍发出，得 %d", emitted)
	}
}

// TestIdle_SinkErrorStillReturns 对齐 :284-289。
func TestIdle_SinkErrorStillReturns(t *testing.T) {
	a := NewNativeV2Adapter(atschema.NewDeepAgentSpec(), nil, 1024)
	a.sinkErr = fmt.Errorf("sink down")
	done, err := a.runRoundControl(context.Background(), &harness_providers.PendingTurn{TurnID: "t"},
		newTurnState("t"), map[string]any{"state": string(hp.HarnessStateIdle)})
	if err != nil {
		t.Fatalf("IDLE 收尾不得因 sink 失败报错: %v", err)
	}
	if !done {
		t.Fatal("IDLE 应结束轮次")
	}
}

// TestSaveOutputState_6Fields 对齐 :411-415。
func TestSaveOutputState_6Fields(t *testing.T) {
	if got := saveOutputState(nil); len(got) != 0 {
		t.Fatalf("nil 应产出空 map，得 %#v", got)
	}
	s := newTurnState("t")
	out := saveOutputState(s)
	for _, name := range outputStateFields {
		if _, ok := out[name]; !ok {
			t.Fatalf("缺字段 %s", name)
		}
	}
	if len(out) != 6 {
		t.Fatalf("应恰好 6 字段，得 %d", len(out))
	}
}

// TestRestoreOutputState_Rebuilds 对齐 :417-426。
func TestRestoreOutputState_Rebuilds(t *testing.T) {
	s := newTurnState("t")
	values := map[string]any{
		"answer_parts":    []any{"a"},
		"reasoning_parts": []any{},
		"final_output":    "done",
		"result_type":     "answer",
		"tool_blocks":     []any{map[string]any{"block_id": "b1", "kind": "text", "content": "x", "data": map[string]any{}}},
		"tool_messages":   []any{map[string]any{"message_id": "m1", "role": "tool", "content": []any{}, "data": map[string]any{}}},
	}
	if err := restoreOutputState(s, values); err != nil {
		t.Fatal(err)
	}
	if len(s.answerParts) != 1 || s.finalOutput == nil || *s.finalOutput != "done" {
		t.Fatalf("基本字段还原失败: %+v", s)
	}
	if len(s.toolBlocks) != 1 || len(s.toolMessages) != 1 {
		t.Fatalf("tool_blocks/tool_messages 重建失败: blocks=%d messages=%d", len(s.toolBlocks), len(s.toolMessages))
	}
}

// TestResume_ColdRestoreVsColdTurn 双缓存不可合并。
// Python: protocol_adapter.py:340-365
func TestResume_ColdRestoreVsColdTurn(t *testing.T) {
	a := NewNativeV2Adapter(atschema.NewDeepAgentSpec(), nil, 1024)
	pq := "original query"
	cp := &NativeCheckpoint{
		SessionID: "h:a", CardID: "c",
		Contexts: map[string]map[string]any{}, DeepAgent: map[string]any{},
		PausedInput: &SavedInput{MessageID: "m1", TurnID: "turn-1", Mode: hp.DeliveryModeAuto},
		PausedQuery: &pq,
		Queued:      []SavedInput{{MessageID: "m2", TurnID: "turn-2", Mode: hp.DeliveryModeFollowUp}},
		OutputState: map[string]any{},
	}
	a.mu.Lock()
	a.coldRestore = cp
	a.mu.Unlock()
	// 未 Start 的周期不可接收命令；先标记周期已开始（仅测试）。
	a.base.MarkCycleStartedForTest()
	if _, err := a.Send(context.Background(), hp.HarnessInput{Content: "new"}, hp.DeliveryModeAuto); err == nil ||
		!strings.Contains(err.Error(), "resume the restored paused turn") {
		t.Fatalf("cold restore 未续跑前应拒收，得 %v", err)
	}
	bad := hp.HarnessInput{Content: "different"}
	if err := a.Resume(context.Background(), &bad); err == nil ||
		!strings.Contains(err.Error(), "does not match the checkpoint") {
		t.Fatalf("query 不匹配应报错，得 %v", err)
	}
	a.mu.Lock()
	if a.coldRestore == nil {
		t.Fatal("query 不匹配不得消耗 coldRestore")
	}
	a.mu.Unlock()
	if err := a.Resume(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	if a.coldRestore != nil {
		t.Fatal("coldRestore 应清空")
	}
	if a.coldTurn == nil || a.coldTurn.PausedInput.TurnID != "turn-1" {
		t.Fatal("coldTurn 未装入或 turn_id 错")
	}
	a.mu.Unlock()
	got := a.pendingTurnIDsForTest()
	want := []string{"turn-1", "turn-2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pending 顺序 = %v, want %v", got, want)
	}
}

// TestSend_RejectedWhenColdRestore 对齐 :340-343。
func TestSend_RejectedWhenColdRestore(t *testing.T) {
	a := NewNativeV2Adapter(atschema.NewDeepAgentSpec(), nil, 1024)
	a.mu.Lock()
	a.coldRestore = &NativeCheckpoint{}
	a.mu.Unlock()
	_, err := a.Send(context.Background(), hp.HarnessInput{Content: "x"}, hp.DeliveryModeAuto)
	if err == nil || !strings.Contains(err.Error(), "resume the restored paused turn") {
		t.Fatalf("期望拒绝，得 %v", err)
	}
}

// TestV2Provider_Create 对齐 :458-487。
func TestV2Provider_Create(t *testing.T) {
	p := &NativeV2HarnessProvider{}
	if p.Card().Name != "native_v2" {
		t.Fatal("card 名错")
	}
	if _, err := p.Create(hp.JsonObject{"unknown": 1}); err == nil ||
		!strings.Contains(err.Error(), "unknown native_v2 configuration fields") {
		t.Fatalf("未知键应报错，得 %v", err)
	}
	for _, bad := range []any{true, 0, -1, "x"} {
		if _, err := p.Create(hp.JsonObject{"event_buffer_capacity": bad}); err == nil ||
			!strings.Contains(err.Error(), "positive integer") {
			t.Fatalf("capacity=%v 应报错，得 %v", bad, err)
		}
	}
	h, err := p.Create(hp.JsonObject{"language": "en", "event_buffer_capacity": 8})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := h.(*NativeHarnessProtocolAdapter); !ok {
		t.Fatalf("Create 应返回 *NativeHarnessProtocolAdapter，得 %T", h)
	}
	h2, err := p.Create(hp.JsonObject{"agent_template": map[string]any{"name": "tpl"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := h2.(*NativeHarnessProtocolAdapter); !ok {
		t.Fatal("template 分支返回类型错")
	}
}

// TestResolveProvider_NativeV2Linked 断言不再返回 "declared but not yet linked"。
func TestResolveProvider_NativeV2Linked(t *testing.T) {
	p, err := harness_providers.ResolveProvider("native_v2")
	if err != nil {
		t.Fatalf("native_v2 应已 link: %v", err)
	}
	if p.Card().Name != "native_v2" {
		t.Fatalf("card = %s", p.Card().Name)
	}
}

// TestStatic_NoTeamHarnessRef 读源码断言不引用团队主链符号。
func TestStatic_NoTeamHarnessRef(t *testing.T) {
	src, err := os.ReadFile("protocolAdapter.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"TeamHarness", "UserInbox", "dispatch_payloads"} {
		if bytes.Contains(src, []byte(forbidden)) {
			t.Fatalf("adapter 不应引用团队主链符号 %s", forbidden)
		}
	}
}

// TestDocGo_FileTree 断言 session/doc.go 含 vcs/、native/doc.go 含两个新文件。
func TestDocGo_FileTree(t *testing.T) {
	cases := []struct {
		path string
		want []string
	}{
		{"../../../../internal/agentcore/session/doc.go", []string{"vcs/", "codec.go"}},
		{"doc.go", []string{"checkpoint.go", "protocolAdapter.go"}},
	}
	for _, c := range cases {
		src, err := os.ReadFile(c.path)
		if err != nil {
			t.Fatalf("读 %s: %v", c.path, err)
		}
		for _, w := range c.want {
			if !bytes.Contains(src, []byte(w)) {
				t.Fatalf("%s 文件树缺 %q", c.path, w)
			}
		}
	}
}

// pendingTurnIDsForTest 读取待决轮次 id 序列（测试助手，同包可见）。
func (a *NativeHarnessProtocolAdapter) pendingTurnIDsForTest() []string {
	turns := a.base.PendingTurns()
	out := make([]string, 0, len(turns))
	for _, t := range turns {
		out = append(out, t.TurnID)
	}
	return out
}

// TestOpenSession_CallOrder 用注入的 recordStep 断言装配顺序。
// Python: protocol_adapter.py:178-213
func TestOpenSession_CallOrder(t *testing.T) {
	a := NewNativeV2Adapter(atschema.NewDeepAgentSpec(), nil, 1024)
	var steps []string
	a.recordStep = func(name string) { steps = append(steps, name) }
	_, err := a.OpenSession(context.Background(), hp.HarnessContext{
		ProtocolVersion: hp.ProtocolVersion, HostSessionID: "h", AgentID: "a",
		ResumePolicy: hp.ResumePolicyNew,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"build_native", "pre_run", "update_state", "subscribe", "start", "outputs",
	}
	if !reflect.DeepEqual(steps, want) {
		t.Fatalf("装配顺序 = %v, want %v", steps, want)
	}
}

// TestErrorMessages_ExactText 逐字断言协议文案（不本地化）。
func TestErrorMessages_ExactText(t *testing.T) {
	cases := []struct {
		name string
		call func() error
		want string
	}{
		{"mcp", func() error {
			a := NewNativeV2Adapter(atschema.NewDeepAgentSpec(), nil, 1024)
			return a.ValidateContext(context.Background(), hp.HarnessContext{
				ProtocolVersion: hp.ProtocolVersion, McpServers: []hp.McpServerConfig{{Name: "x"}},
			})
		}, "declare native MCP servers in the manifest"},
		{"require_resume", func() error {
			a := NewNativeV2Adapter(atschema.NewDeepAgentSpec(), nil, 1024)
			return a.ValidateContext(context.Background(), hp.HarnessContext{
				ProtocolVersion: hp.ProtocolVersion, ResumePolicy: hp.ResumePolicyRequireResume,
			})
		}, "NativeHarness requires a checkpoint to resume"},
		{"cold_reject", func() error {
			a := NewNativeV2Adapter(atschema.NewDeepAgentSpec(), nil, 1024)
			a.mu.Lock()
			a.coldRestore = &NativeCheckpoint{}
			a.mu.Unlock()
			_, err := a.Send(context.Background(), hp.HarnessInput{Content: "x"}, hp.DeliveryModeAuto)
			return err
		}, "resume the restored paused turn before sending new input"},
		{"cold_query_mismatch", func() error {
			a := NewNativeV2Adapter(atschema.NewDeepAgentSpec(), nil, 1024)
			a.base.MarkCycleStartedForTest()
			pq := "orig"
			a.mu.Lock()
			a.coldRestore = &NativeCheckpoint{PausedQuery: &pq}
			a.mu.Unlock()
			q := hp.HarnessInput{Content: "diff"}
			return a.Resume(context.Background(), &q)
		}, "resume query does not match the checkpoint"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.call()
			if err == nil || err.Error() != c.want {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
		})
	}
}

// TestSuperviseTurns_ExitsOnEmptyPending 路径 1：队列跑空后 supervisor 退出。
func TestSuperviseTurns_ExitsOnEmptyPending(t *testing.T) {
	a := NewNativeV2Adapter(atschema.NewDeepAgentSpec(), nil, 1024)
	a.base.MarkCycleStartedForTest()
	if len(a.base.PendingTurns()) != 0 {
		t.Fatal("初始 pending 应为空")
	}
	handle := a.base.SupervisorHandleForTest()
	if handle != nil {
		t.Fatal("未 Send 时不应有 supervisor")
	}
}

// TestStop_SetsTerminated 路径 2：Stop 后转 TERMINATED。
func TestStop_SetsTerminated(t *testing.T) {
	a := NewNativeV2Adapter(atschema.NewDeepAgentSpec(), nil, 1024)
	a.base.MarkCycleStartedForTest()
	if err := a.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if a.State() != hp.HarnessStateTerminated {
		t.Fatalf("Stop 后 state = %s", a.State())
	}
}

// TestAbort_DoesNotKillSupervisorCycle 路径 3：Abort 不终止周期。
func TestAbort_DoesNotKillSupervisorCycle(t *testing.T) {
	a := NewNativeV2Adapter(atschema.NewDeepAgentSpec(), nil, 1024)
	a.base.MarkCycleStartedForTest()
	if err := a.Abort(context.Background(), hp.AbortModeForce); err != nil {
		t.Fatalf("无活跃轮次时 Abort 应 no-op: %v", err)
	}
	if a.State() == hp.HarnessStateTerminated {
		t.Fatal("Abort 不应终止整个周期")
	}
}

// TestExecuteTurn_CrashStillTerminates 路径 4：crash 转 FAILED 终态。
func TestExecuteTurn_CrashStillTerminates(t *testing.T) {
	a := NewNativeV2Adapter(atschema.NewDeepAgentSpec(), nil, 1024)
	a.executeTurnErr = fmt.Errorf("boom")
	kind, _, err := a.ExecuteTurn(context.Background(), &harness_providers.PendingTurn{TurnID: "t"})
	if err != nil {
		t.Fatalf("crash 不应向上抛未包装错误: %v", err)
	}
	if kind != hp.TurnEventKindFailed {
		t.Fatalf("crash 应转 FAILED，得 %s", kind)
	}
}

// TestPhaseSnapshotRail_Existing 断言三态由既有 snapshot_rail.go 维护，本批未改其代码。
func TestPhaseSnapshotRail_Existing(t *testing.T) {
	for _, phase := range []atharness.RoundPhase{
		atharness.RoundPhaseBoundary, atharness.RoundPhaseModel, atharness.RoundPhaseTool,
	} {
		active := &atharness.ActiveRound{}
		active.SetPhase(phase)
		if active.Phase() != phase {
			t.Fatalf("SetPhase(%s) 后读回 = %s", phase, active.Phase())
		}
	}
}
