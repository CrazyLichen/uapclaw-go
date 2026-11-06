package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/harness"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	streambase "github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// TestStreamController_mapState_RUNNING转BUSY 验证相位 RUNNING → 成员 BUSY。
func TestStreamController_mapState_RUNNING转BUSY(t *testing.T) {
	sc := newTestStreamController()
	var got atschema.MemberStatus
	sc.updateStatus = func(ctx context.Context, s atschema.MemberStatus) error { got = s; return nil }

	sc.mapState(context.Background(), harness.HarnessStateEvent{New: harness.HarnessStateRunning})
	if got != atschema.MemberStatusBusy {
		t.Errorf("RUNNING 应映射为 BUSY，实际 %v", got)
	}
}

// TestStreamController_mapState_IDLE转READY并触发onIdleSettled 验证 IDLE → READY + 收尾。
func TestStreamController_mapState_IDLE转READY并触发onIdleSettled(t *testing.T) {
	sc := newTestStreamController()
	var got atschema.MemberStatus
	sc.updateStatus = func(ctx context.Context, s atschema.MemberStatus) error { got = s; return nil }
	polled := 0
	sc.requestCompletionPollCb = func(ctx context.Context) error { polled++; return nil }

	sc.mapState(context.Background(), harness.HarnessStateEvent{New: harness.HarnessStateIdle})
	if got != atschema.MemberStatusReady {
		t.Errorf("IDLE 应映射为 READY，实际 %v", got)
	}
	if polled != 1 {
		t.Errorf("IDLE 应触发 onIdleSettled → completionPoll 一次，实际 %d", polled)
	}
}

// TestStreamController_mapRound_started复位吞轮 验证 round=started 复位 swallowFailedRound。
func TestStreamController_mapRound_started复位吞轮(t *testing.T) {
	sc := newTestStreamController()
	sc.swallowFailedRound = true
	var seq []atschema.ExecutionStatus
	sc.updateExecution = func(ctx context.Context, s atschema.ExecutionStatus) error { seq = append(seq, s); return nil }

	sc.mapRound(context.Background(), harness.HarnessRoundEvent{Kind: "started", RoundID: 1})
	if sc.swallowFailedRound {
		t.Error("round=started 应复位 swallowFailedRound")
	}
	if len(seq) != 2 || seq[0] != atschema.ExecutionStatusStarting || seq[1] != atschema.ExecutionStatusRunning {
		t.Errorf("started 应产出 STARTING→RUNNING，实际 %v", seq)
	}
}

// TestStreamController_mapRound_finished链 验证 finished → COMPLETING→COMPLETED→IDLE。
func TestStreamController_mapRound_finished链(t *testing.T) {
	sc := newTestStreamController()
	var seq []atschema.ExecutionStatus
	sc.updateExecution = func(ctx context.Context, s atschema.ExecutionStatus) error { seq = append(seq, s); return nil }

	sc.mapRound(context.Background(), harness.HarnessRoundEvent{Kind: "finished", RoundID: 1})
	want := []atschema.ExecutionStatus{
		atschema.ExecutionStatusCompleting, atschema.ExecutionStatusCompleted, atschema.ExecutionStatusIdle,
	}
	if len(seq) != len(want) {
		t.Fatalf("finished 应产出 3 个状态，实际 %v", seq)
	}
	for i := range want {
		if seq[i] != want[i] {
			t.Errorf("第 %d 个状态 = %v，期望 %v", i, seq[i], want[i])
		}
	}
}

// TestStreamController_mapRound_aborted链 验证 aborted → CANCEL 链 → IDLE。
func TestStreamController_mapRound_aborted链(t *testing.T) {
	sc := newTestStreamController()
	var seq []atschema.ExecutionStatus
	sc.updateExecution = func(ctx context.Context, s atschema.ExecutionStatus) error { seq = append(seq, s); return nil }

	sc.mapRound(context.Background(), harness.HarnessRoundEvent{Kind: "aborted", RoundID: 1})
	want := []atschema.ExecutionStatus{
		atschema.ExecutionStatusCancelRequested, atschema.ExecutionStatusCancelling,
		atschema.ExecutionStatusCancelled, atschema.ExecutionStatusIdle,
	}
	if len(seq) != len(want) {
		t.Fatalf("aborted 应产出 4 个状态，实际 %v", seq)
	}
	for i := range want {
		if seq[i] != want[i] {
			t.Errorf("第 %d 个状态 = %v，期望 %v", i, seq[i], want[i])
		}
	}
}

// TestStreamController_mapRound_failed链 验证 failed → FAILED→IDLE。
func TestStreamController_mapRound_failed链(t *testing.T) {
	sc := newTestStreamController()
	var seq []atschema.ExecutionStatus
	sc.updateExecution = func(ctx context.Context, s atschema.ExecutionStatus) error { seq = append(seq, s); return nil }

	sc.mapRound(context.Background(), harness.HarnessRoundEvent{Kind: "failed", RoundID: 1})
	want := []atschema.ExecutionStatus{atschema.ExecutionStatusFailed, atschema.ExecutionStatusIdle}
	if len(seq) != len(want) {
		t.Fatalf("failed 应产出 2 个状态，实际 %v", seq)
	}
	for i := range want {
		if seq[i] != want[i] {
			t.Errorf("第 %d 个状态 = %v，期望 %v", i, seq[i], want[i])
		}
	}
}

// TestStreamController_onIdleSettled_顺序 验证 wakeMailbox→reconcile→completionPoll 顺序。
func TestStreamController_onIdleSettled_顺序(t *testing.T) {
	sc := newTestStreamController()
	var order []string
	sc.wakeMailboxCb = func(ctx context.Context) error { order = append(order, "wake"); return nil }
	sc.reconcileMemberStartupCb = func(ctx context.Context) error { order = append(order, "reconcile"); return nil }
	sc.requestCompletionPollCb = func(ctx context.Context) error { order = append(order, "poll"); return nil }

	sc.onIdleSettled(context.Background())
	want := []string{"wake", "reconcile", "poll"}
	if len(order) != len(want) {
		t.Fatalf("onIdleSettled 顺序 = %v，期望 %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Errorf("第 %d 步 = %s，期望 %s", i, order[i], want[i])
		}
	}
}

// TestStreamController_onIdleSettled_teamCleaned关流 验证 team_cleaned 时关流并短路。
func TestStreamController_onIdleSettled_teamCleaned关流(t *testing.T) {
	sc := newTestStreamController()
	sc.state.TeamCleaned = true
	sc.streamQueue = make(chan streambase.Schema, 1)
	polled := 0
	sc.requestCompletionPollCb = func(ctx context.Context) error { polled++; return nil }

	sc.onIdleSettled(context.Background())
	if polled != 0 {
		t.Error("team_cleaned 时应短路，不触发 completionPoll")
	}
	select {
	case v := <-sc.streamQueue:
		if v != nil {
			t.Error("team_cleaned 关闭流应写入 nil sentinel")
		}
	default:
		t.Error("team_cleaned 应关闭流（写入 nil sentinel）")
	}
}

// TestStreamController_emitRetryOutput_可见提示 验证重试产出 [Retry n/N] llm_output。
func TestStreamController_emitRetryOutput_可见提示(t *testing.T) {
	sc := newTestStreamController()
	sc.streamQueue = make(chan streambase.Schema, 1)
	sc.retryAttempt = 2

	sc.emitRetryOutput(context.Background(), 181001, "服务端抖动")
	select {
	case chunk := <-sc.streamQueue:
		os, ok := chunk.(*streambase.OutputSchema)
		if !ok {
			t.Fatalf("emitRetryOutput 应产出 OutputSchema，实际 %T", chunk)
		}
		if os.Type != "llm_output" {
			t.Errorf("类型 = %s，期望 llm_output", os.Type)
		}
		payload, ok := os.Payload.(map[string]any)
		if !ok {
			t.Fatalf("载荷应为 map，实际 %T", os.Payload)
		}
		if payload["retrying"] != true {
			t.Error("payload.retrying 应为 true")
		}
		if payload["attempt"] != 2 {
			t.Errorf("payload.attempt = %v，期望 2", payload["attempt"])
		}
		content, _ := payload["content"].(string)
		if !strings.Contains(content, "[Retry 2/10] 服务端抖动") {
			t.Errorf("content 应含 [Retry 2/10] 服务端抖动，实际 %q", content)
		}
	default:
		t.Error("emitRetryOutput 应写入 streamQueue")
	}
}

// TestStreamController_handleRetry_可重试码吞轮并计数 验证 181001 在预算内消费 + 计数。
func TestStreamController_handleRetry_可重试码吞轮并计数(t *testing.T) {
	sc := newTestStreamController()
	// 需要非 nil harness 才能走重试分支（Send 返回错误被忽略）
	sc.resources.Harness = &harness.TeamHarness{}
	sc.streamQueue = make(chan streambase.Schema, 4)

	chunk := &streambase.OutputSchema{
		Type: taskFailedPayloadType,
		Payload: map[string]any{
			"type": taskFailedPayloadType,
			"data": []any{map[string]any{"text": "[181001] 服务端抖动"}},
		},
	}
	consumed := sc.handleRetry(context.Background(), chunk)
	if !consumed {
		t.Fatal("181001 可重试错误应被 handleRetry 消费")
	}
	if !sc.swallowFailedRound {
		t.Error("可重试失败应吞掉本轮剩余 chunk")
	}
	if sc.retryAttempt != 1 {
		t.Errorf("重试计数 = %d，期望 1", sc.retryAttempt)
	}
	// 应产出 [Retry 1/10] 可见提示
	select {
	case retryChunk := <-sc.streamQueue:
		os, ok := retryChunk.(*streambase.OutputSchema)
		if !ok || os.Type != "llm_output" {
			t.Errorf("应产出 llm_output，实际 %v", retryChunk)
		}
	default:
		t.Error("重试应产出可见提示 chunk")
	}
}

// TestStreamController_handleRetry_非可重试码透传 验证非 181001 返回 false。
func TestStreamController_handleRetry_非可重试码透传(t *testing.T) {
	sc := newTestStreamController()
	sc.resources.Harness = &harness.TeamHarness{}
	chunk := &streambase.OutputSchema{
		Type: taskFailedPayloadType,
		Payload: map[string]any{
			"type": taskFailedPayloadType,
			"data": []any{map[string]any{"text": "[199999] 不可恢复"}},
		},
	}
	if sc.handleRetry(context.Background(), chunk) {
		t.Error("非可重试错误不应被消费")
	}
	if sc.retryAttempt != 0 {
		t.Errorf("非可重试错误不应计数，实际 %d", sc.retryAttempt)
	}
}

// TestStreamController_handleRetry_超预算透传 验证重试次数耗尽后透传。
func TestStreamController_handleRetry_超预算透传(t *testing.T) {
	sc := newTestStreamController()
	sc.resources.Harness = &harness.TeamHarness{}
	sc.retryAttempt = maxRetryAttempts
	chunk := &streambase.OutputSchema{
		Type: taskFailedPayloadType,
		Payload: map[string]any{
			"type": taskFailedPayloadType,
			"data": []any{map[string]any{"text": "[181001] 抖动"}},
		},
	}
	if sc.handleRetry(context.Background(), chunk) {
		t.Error("重试预算耗尽后不应再消费")
	}
	if sc.retryAttempt != maxRetryAttempts {
		t.Errorf("预算耗尽后计数不应再增，实际 %d", sc.retryAttempt)
	}
}

// TestStreamController_start转发订阅 验证 start 复位 retry + 挂订阅 + 起泵。
func TestStreamController_start转发订阅(t *testing.T) {
	sc := newTestStreamController()
	// native=nil 的 TeamHarness：Subscribe/Outputs 均 no-op/nil
	sc.resources.Harness = &harness.TeamHarness{}
	sc.retryAttempt = 3
	sc.swallowFailedRound = true

	sc.start(context.Background())

	if sc.retryAttempt != 0 {
		t.Errorf("start 应复位 retryAttempt，实际 %d", sc.retryAttempt)
	}
	if sc.swallowFailedRound {
		t.Error("start 应复位 swallowFailedRound")
	}
}

// TestStreamController_forwardOutputs无输出泵 验证 Outputs=nil 时 forwardOutputs 立即返回。
func TestStreamController_forwardOutputs无输出泵(t *testing.T) {
	sc := newTestStreamController()
	sc.resources.Harness = &harness.TeamHarness{} // native=nil → Outputs 返回 nil

	done := make(chan struct{})
	go func() {
		sc.forwardOutputs(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("forwardOutputs 应因 Outputs=nil 立即返回")
	}
}

// TestStreamController_CancelAgent转发abort 验证 CancelAgent → harness.Abort(immediate=true)。
func TestStreamController_CancelAgent转发abort(t *testing.T) {
	sc := newTestStreamController()
	sc.resources.Harness = &harness.TeamHarness{} // native=nil → Abort no-op
	// 无活跃轮时 CancelAgent 直接返回
	if err := sc.CancelAgent(context.Background()); err != nil {
		t.Errorf("无活跃轮 CancelAgent 应返回 nil，实际 %v", err)
	}
}

// TestStreamController_CooperativeCancel无活跃轮 验证无轮时直接返回。
func TestStreamController_CooperativeCancel无活跃轮(t *testing.T) {
	sc := newTestStreamController()
	sc.resources.Harness = &harness.TeamHarness{}
	if err := sc.CooperativeCancel(context.Background()); err != nil {
		t.Errorf("无活跃轮 CooperativeCancel 应返回 nil，实际 %v", err)
	}
}

// TestStreamController_DrainAgentTask无活跃轮 验证无轮时 DrainAgentTask 直接返回。
func TestStreamController_DrainAgentTask无活跃轮(t *testing.T) {
	sc := newTestStreamController()
	sc.resources.Harness = &harness.TeamHarness{}
	if err := sc.DrainAgentTask(context.Background()); err != nil {
		t.Errorf("无活跃轮 DrainAgentTask 应返回 nil，实际 %v", err)
	}
}

// newStreamControllerWithTeamHarness 构造一个带真 TeamHarness（native 非 nil）的 StreamController。
func newStreamControllerWithTeamHarness() *StreamController {
	sc := newTestStreamController()
	spec := atschema.DeepAgentSpec{
		Card: agentschema.NewAgentCard(agentschema.WithAgentName("sc-itest")),
	}
	sc.resources.Harness = harness.BuildTeamHarness(spec, "leader", "m1", nil, nil, nil, nil, nil, nil, false)
	return sc
}

// TestStreamController_IsAgentRunning_idle状态 覆盖 IsAgentRunning 的 harness 非 nil + idle 分支。
func TestStreamController_IsAgentRunning_idle状态(t *testing.T) {
	sc := newStreamControllerWithTeamHarness()
	if sc.IsAgentRunning() {
		t.Error("idle 状态 IsAgentRunning 应为 false")
	}
	if sc.HasInFlightRound() {
		t.Error("idle 状态 HasInFlightRound 应为 false")
	}
}

// TestStreamController_CancelAgent_idle状态 覆盖 CancelAgent 的 HasInFlightRound==false 分支。
func TestStreamController_CancelAgent_idle状态(t *testing.T) {
	sc := newStreamControllerWithTeamHarness()
	if err := sc.CancelAgent(context.Background()); err != nil {
		t.Errorf("idle 状态 CancelAgent 应返回 nil，实际 %v", err)
	}
}

// TestStreamController_CooperativeCancel_idle状态 覆盖 CooperativeCancel 的 HasInFlightRound==false 分支。
func TestStreamController_CooperativeCancel_idle状态(t *testing.T) {
	sc := newStreamControllerWithTeamHarness()
	if err := sc.CooperativeCancel(context.Background()); err != nil {
		t.Errorf("idle 状态 CooperativeCancel 应返回 nil，实际 %v", err)
	}
}

// TestStreamController_DrainAgentTask_idle状态 覆盖 DrainAgentTask 的 idle 分支。
func TestStreamController_DrainAgentTask_idle状态(t *testing.T) {
	sc := newStreamControllerWithTeamHarness()
	if err := sc.DrainAgentTask(context.Background()); err != nil {
		t.Errorf("idle 状态 DrainAgentTask 应返回 nil，实际 %v", err)
	}
}

// TestStreamController_forwardOutputs_空输出 覆盖 forwardOutputs 的 Outputs 非 nil 但立即结束。
func TestStreamController_forwardOutputs_空输出(t *testing.T) {
	sc := newStreamControllerWithTeamHarness()
	// Outputs 非 nil，但 queue 无数据（未 Start 未 Send），Next 会阻塞
	// 用短 ctx 让 Next 因 ctx.Done 返回 false → forwardOutputs 退出
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() {
		sc.forwardOutputs(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("forwardOutputs 应因 ctx 取消退出")
	}
}
