package agent

import (
	"context"
	"testing"
	"time"

	harness "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/harness"
	atschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema"
	streambase "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/stream"
	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_protocol"
)

// 25.2a Task 7：idle 事件五函数专项单测（计划要求 injectable clock，
// 禁真实 sleep——去抖窗口注入 0.01s 短窗，真实定时器但等待有界）。

// newIdleTestSC 构造带成员名册与静止探针的测试控制器。
func newIdleTestSC(t *testing.T, probe func(context.Context) (bool, error)) (*StreamController, *TeamAgentState) {
	t.Helper()
	st := NewTeamAgentState()
	st.MemberRegistry = NewMemberActivityRegistry("leader1")
	sc := NewStreamController(
		func() *TeamAgentBlueprint { return nil }, st, nil,
		func(_ context.Context, _ atschema.MemberStatus) error { return nil },
		func(_ context.Context, _ atschema.ExecutionStatus) error { return nil },
		WithTaskBoardProbe(probe),
		WithIdleDebounceSeconds(0.01),
	)
	return sc, st
}

// payloadOf 取 marker.Payload 的 map 形态。
func payloadOf(t *testing.T, chunk streambase.Schema) map[string]any {
	t.Helper()
	marker, ok := chunk.(*atschema.TeamOutputSchema)
	if !ok {
		t.Fatalf("应发 TeamOutputSchema: %T", chunk)
	}
	m, _ := marker.Payload.(map[string]any)
	return m
}

// waitIdleMail 等待队列出现 idle 标记（有界轮询，禁真实长 sleep）。
func waitIdleMail(t *testing.T, q chan streambase.Schema, timeout time.Duration) map[string]any {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case chunk := <-q:
			if chunk == nil {
				continue // CloseStream 的 nil sentinel（终结信号），跳过
			}
			m := payloadOf(t, chunk)
			if et, _ := m["event_type"].(string); et == "team.idle" {
				return m
			}
		default:
			time.Sleep(time.Millisecond)
		}
	}
	return nil
}

// TestScheduleTeamIdle_去抖后板静才发 对齐 schedule_team_idle（py L552-564）：
// 窗口内成员动了 → cancel 不发；板未静 → 不发；全静止 → 恰一封。
func TestScheduleTeamIdle_去抖后板静才发(t *testing.T) {
	// 场景 1：窗口内成员动了（record 返回 CANCEL）→ 不发
	sc, st := newIdleTestSC(t, func(context.Context) (bool, error) { return true, nil })
	q := make(chan streambase.Schema, 8)
	sc.streamQueue = q
	sc.ScheduleTeamIdle()
	st.MemberRegistry.Record("bob", atschema.MemberStatusBusy) // 移动 → CANCEL
	sc.CancelTeamIdle()
	if got := waitIdleMail(t, q, 50*time.Millisecond); got != nil {
		t.Error("窗口内撤销后不应发 idle 标记")
	}

	// 场景 2：板未静（探针 false）→ 不发
	sc2, _ := newIdleTestSC(t, func(context.Context) (bool, error) { return false, nil })
	q2 := make(chan streambase.Schema, 8)
	sc2.streamQueue = q2
	sc2.ScheduleTeamIdle()
	if got := waitIdleMail(t, q2, 50*time.Millisecond); got != nil {
		t.Error("板未静不应发 idle 标记")
	}

	// 场景 3：全静止 + 板静 → 恰一封，payload 对齐 py emit_team_idle（L645-663）
	sc3, st3 := newIdleTestSC(t, func(context.Context) (bool, error) { return true, nil })
	q3 := make(chan streambase.Schema, 8)
	sc3.streamQueue = q3
	st3.MemberRegistry.Record("bob", atschema.MemberStatusReady) // SCHEDULE 边
	sc3.ScheduleTeamIdle()
	marker := waitIdleMail(t, q3, 200*time.Millisecond)
	if marker == nil {
		t.Fatal("全静止应在去抖后发 idle 标记")
	}
	if et, _ := marker["event_type"].(string); et != "team.idle" {
		t.Errorf("event_type 应为 team.idle: %v", marker)
	}
	if mc, _ := marker["member_count"].(int); mc != 2 {
		t.Errorf("member_count 应含 leader+bob=2: %v", marker)
	}
}

// TestEmitTeamIdle_非终态标记_流保持开 idle 标记不是终结信号：队里没有
// nil sentinel（对齐 py emit_team_idle 不关流，L645-663）。
func TestEmitTeamIdle_非终态标记_流保持开(t *testing.T) {
	sc, _ := newIdleTestSC(t, nil)
	q := make(chan streambase.Schema, 8)
	sc.streamQueue = q
	sc.EmitTeamIdle(map[string]string{"leader1": "ready"})
	sc.EmitTeamIdle(map[string]string{"leader1": "ready"})
	if len(q) != 2 {
		t.Errorf("流应保持打开（无 nil sentinel），实际队长 %d", len(q))
	}
}

// TestEmitTeamRecovered_kernelStart尾 recovered 标记的 payload 形状与
// 非终结语义（对齐 py emit_team_recovered，L674-690）。
func TestEmitTeamRecovered_kernelStart尾(t *testing.T) {
	sc, _ := newIdleTestSC(t, nil)
	q := make(chan streambase.Schema, 8)
	sc.streamQueue = q
	sc.EmitTeamRecovered()
	select {
	case chunk := <-q:
		if et, _ := payloadOf(t, chunk)["event_type"].(string); et != "team.recovered" {
			t.Error("event_type 应为 team.recovered")
		}
	default:
		t.Fatal("应入队 recovered 标记")
	}
	if len(q) != 0 {
		t.Error("recovered 不是终结信号，不应关流")
	}
}

// TestCancelTeamIdle_停机路径不漏定时器 CloseStream 与 CancelTeamIdle 双路
// 撤销（对齐 py cancel_team_idle，L566-578）。
func TestCancelTeamIdle_停机路径不漏定时器(t *testing.T) {
	sc, st := newIdleTestSC(t, func(context.Context) (bool, error) { return true, nil })
	q := make(chan streambase.Schema, 4)
	sc.streamQueue = q
	st.MemberRegistry.Record("bob", atschema.MemberStatusReady)
	sc.ScheduleTeamIdle()
	sc.CloseStream() // 拆机路径先 cancel（py close_stream L513-516）
	if got := waitIdleMail(t, q, 50*time.Millisecond); got != nil {
		t.Error("关流后定时器不得再写队列")
	}
	if sc.idleMarkerCancel != nil {
		t.Error("cancel 后 pending 标记应清空")
	}
	// 幂等：重复 cancel 不 panic
	sc.CancelTeamIdle()
}

// TestMapState_idleClock注入 打点/清零走注入时钟（对齐 py idle_since 语义）。
func TestMapState_idleClock注入(t *testing.T) {
	sc, st := newIdleTestSC(t, nil)
	base := time.Now()
	sc.idleClock = func() time.Time { return base }
	sc.mapState(context.Background(), harness.HarnessStateEvent{New: harnessprotocol.HarnessStateIdle})
	if !st.IdleSince.Equal(base) {
		t.Errorf("IDLE 应以注入时钟打点: %v != %v", st.IdleSince, base)
	}
	sc.mapState(context.Background(), harness.HarnessStateEvent{New: harnessprotocol.HarnessStateRunning})
	if !st.IdleSince.IsZero() {
		t.Error("RUNNING 应清零")
	}
}
