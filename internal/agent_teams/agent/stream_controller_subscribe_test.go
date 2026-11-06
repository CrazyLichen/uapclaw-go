package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/harness"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	streambase "github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
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
