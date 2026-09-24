package coordination

import (
	"context"
	"sync"
	"testing"
	"time"

	schema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema/events"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/agent/coordination/types"
)

// ──────────────────────────── CoordinationEvent 测试 ────────────────────────────

func TestInnerEventType_值(t *testing.T) {
	tests := []struct {
		name  string
		value types.InnerEventType
		want  string
	}{
		{"user_input", types.InnerEventTypeUserInput, "user_input"},
		{"poll_mailbox", types.InnerEventTypePollMailbox, "coordination_poll_mailbox"},
		{"poll_task", types.InnerEventTypePollTask, "coordination_poll_task"},
		{"shutdown", types.InnerEventTypeShutdown, "shutdown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.value) != tt.want {
				t.Errorf("InnerEventType %s = %q, want %q", tt.name, tt.value, tt.want)
			}
		})
	}
}

func TestCoordinationEvent_IsInner(t *testing.T) {
	innerEvent := types.CoordinationEvent{Inner: &types.InnerEventMessage{EventType: types.InnerEventTypeUserInput}}
	if !innerEvent.IsInner() {
		t.Error("IsInner() 应为 true（Inner 非 nil）")
	}
	if innerEvent.IsTransport() {
		t.Error("IsTransport() 应为 false（Inner 非 nil 时）")
	}
}

func TestCoordinationEvent_IsTransport(t *testing.T) {
	transportEvent := types.CoordinationEvent{Transport: &events.EventMessage{EventType: "message"}}
	if !transportEvent.IsTransport() {
		t.Error("IsTransport() 应为 true（Transport 非 nil）")
	}
	if transportEvent.IsInner() {
		t.Error("IsInner() 应为 false（Transport 非 nil 时）")
	}
}

func TestCoordinationEvent_EventType(t *testing.T) {
	innerEvent := types.CoordinationEvent{Inner: &types.InnerEventMessage{EventType: types.InnerEventTypePollTask}}
	if got := innerEvent.EventType(); got != "coordination_poll_task" {
		t.Errorf("EventType() = %q, want %q", got, "coordination_poll_task")
	}
	transportEvent := types.CoordinationEvent{Transport: &events.EventMessage{EventType: "member_shutdown"}}
	if got := transportEvent.EventType(); got != "member_shutdown" {
		t.Errorf("EventType() = %q, want %q", got, "member_shutdown")
	}
}

// ──────────────────────────── EventBus 测试 ────────────────────────────

func TestEventBus_NewEventBus(t *testing.T) {
	bus := NewEventBus(schema.TeamRoleLeader, 30.0, 30.0)
	if bus == nil {
		t.Fatal("NewEventBus 返回 nil")
	}
	if bus.IsRunning() {
		t.Error("新创建的 EventBus 不应运行中")
	}
	if bus.PollsPaused() {
		t.Error("新创建的 EventBus 不应暂停轮询")
	}
	if bus.Role() != schema.TeamRoleLeader {
		t.Errorf("Role() = %q, want %q", bus.Role(), schema.TeamRoleLeader)
	}
}

func TestEventBus_HumanAgent无周期轮询(t *testing.T) {
	bus := NewEventBus(schema.TeamRoleHumanAgent, 30.0, 30.0)
	if bus.periodicPollEnabled {
		t.Error("Human-agent 不应启用周期轮询")
	}
}

func TestEventBus_StartStop(t *testing.T) {
	bus := NewEventBus(schema.TeamRoleLeader, 30.0, 30.0)
	if bus.IsRunning() {
		t.Error("新创建的 EventBus 不应运行中")
	}
	var mu sync.Mutex
	var received []types.CoordinationEvent
	callback := func(ctx context.Context, event types.CoordinationEvent) {
		mu.Lock()
		received = append(received, event)
		mu.Unlock()
	}
	ctx := context.Background()
	bus.Start(ctx, callback)
	if !bus.IsRunning() {
		t.Error("Start 后应运行中")
	}
	bus.Enqueue(types.CoordinationEvent{Inner: &types.InnerEventMessage{EventType: types.InnerEventTypeUserInput}})
	time.Sleep(100 * time.Millisecond)
	bus.Stop()
	if bus.IsRunning() {
		t.Error("Stop 后不应运行中")
	}
	mu.Lock()
	cnt := len(received)
	mu.Unlock()
	if cnt != 1 {
		t.Errorf("期望收到 1 个事件，实际 %d", cnt)
	}
}

func TestEventBus_Start幂等(t *testing.T) {
	bus := NewEventBus(schema.TeamRoleLeader, 30.0, 30.0)
	callback := func(ctx context.Context, event types.CoordinationEvent) {}
	ctx := context.Background()
	bus.Start(ctx, callback)
	bus.Start(ctx, callback) // 二次调用应无副作用
	if !bus.IsRunning() {
		t.Error("重复 Start 后仍应 running")
	}
	bus.Stop()
}

func TestEventBus_Stop幂等(t *testing.T) {
	bus := NewEventBus(schema.TeamRoleLeader, 30.0, 30.0)
	bus.Stop() // 未启动时 stop 应不 panic
	if bus.IsRunning() {
		t.Error("未启动的 EventBus stop 后不应 running")
	}
}

func TestEventBus_轮询事件(t *testing.T) {
	bus := NewEventBus(schema.TeamRoleTeammate, 0.05, 0.05)
	var mu sync.Mutex
	var pollCount int
	callback := func(ctx context.Context, event types.CoordinationEvent) {
		if event.IsInner() && (event.Inner.EventType == types.InnerEventTypePollMailbox || event.Inner.EventType == types.InnerEventTypePollTask) {
			mu.Lock()
			pollCount++
			mu.Unlock()
		}
	}
	ctx := context.Background()
	bus.Start(ctx, callback)
	time.Sleep(200 * time.Millisecond)
	bus.Stop()
	mu.Lock()
	cnt := pollCount
	mu.Unlock()
	if cnt == 0 {
		t.Error("应收到轮询事件")
	}
}

func TestEventBus_HumanAgent无轮询(t *testing.T) {
	bus := NewEventBus(schema.TeamRoleHumanAgent, 0.05, 0.05)
	var mu sync.Mutex
	var pollCount int
	callback := func(ctx context.Context, event types.CoordinationEvent) {
		if event.IsInner() && (event.Inner.EventType == types.InnerEventTypePollMailbox || event.Inner.EventType == types.InnerEventTypePollTask) {
			mu.Lock()
			pollCount++
			mu.Unlock()
		}
	}
	ctx := context.Background()
	bus.Start(ctx, callback)
	time.Sleep(200 * time.Millisecond)
	bus.Stop()
	mu.Lock()
	cnt := pollCount
	mu.Unlock()
	if cnt > 0 {
		t.Errorf("Human-agent 不应有轮询事件，实际 %d", cnt)
	}
}

func TestEventBus_PausePolls和ResumePolls(t *testing.T) {
	bus := NewEventBus(schema.TeamRoleTeammate, 0.05, 0.05)
	var mu sync.Mutex
	var pollCount int
	callback := func(ctx context.Context, event types.CoordinationEvent) {
		if event.IsInner() && (event.Inner.EventType == types.InnerEventTypePollMailbox || event.Inner.EventType == types.InnerEventTypePollTask) {
			mu.Lock()
			pollCount++
			mu.Unlock()
		}
	}
	ctx := context.Background()
	bus.Start(ctx, callback)
	time.Sleep(150 * time.Millisecond) // 等一些轮询事件
	bus.PausePolls()
	if !bus.PollsPaused() {
		t.Error("PausePolls 后应暂停")
	}
	countBeforePause := func() int { mu.Lock(); defer mu.Unlock(); return pollCount }()
	time.Sleep(200 * time.Millisecond) // 等暂停期间
	countAfterPause := func() int { mu.Lock(); defer mu.Unlock(); return pollCount }()
	bus.ResumePolls()
	if bus.PollsPaused() {
		t.Error("ResumePolls 后不应暂停")
	}
	time.Sleep(150 * time.Millisecond) // 等恢复后的轮询
	bus.Stop()
	mu.Lock()
	finalCount := pollCount
	mu.Unlock()
	if countAfterPause > countBeforePause+1 {
		t.Errorf("暂停期间不应有大量新轮询事件（before=%d, after=%d）", countBeforePause, countAfterPause)
	}
	if finalCount <= countAfterPause {
		t.Errorf("恢复后应有新轮询事件（after=%d, final=%d）", countAfterPause, finalCount)
	}
}

func TestEventBus_串行消费(t *testing.T) {
	bus := NewEventBus(schema.TeamRoleLeader, 30.0, 30.0)
	var mu sync.Mutex
	var order []string
	callback := func(ctx context.Context, event types.CoordinationEvent) {
		if event.IsInner() && event.Inner.EventType == types.InnerEventTypeUserInput {
			mu.Lock()
			order = append(order, event.Inner.Payload["seq"].(string))
			mu.Unlock()
		}
	}
	ctx := context.Background()
	bus.Start(ctx, callback)
	bus.Enqueue(types.CoordinationEvent{Inner: &types.InnerEventMessage{EventType: types.InnerEventTypeUserInput, Payload: map[string]any{"seq": "first"}}})
	bus.Enqueue(types.CoordinationEvent{Inner: &types.InnerEventMessage{EventType: types.InnerEventTypeUserInput, Payload: map[string]any{"seq": "second"}}})
	bus.Enqueue(types.CoordinationEvent{Inner: &types.InnerEventMessage{EventType: types.InnerEventTypeUserInput, Payload: map[string]any{"seq": "third"}}})
	time.Sleep(100 * time.Millisecond)
	bus.Stop()
	mu.Lock()
	defer mu.Unlock()
	if len(order) != 3 {
		t.Errorf("期望收到 3 个事件，实际 %d", len(order))
	}
	for i, want := range []string{"first", "second", "third"} {
		if order[i] != want {
			t.Errorf("事件 %d: got %q, want %q", i, order[i], want)
		}
	}
}

func TestEventBus_无回调时不panic(t *testing.T) {
	bus := NewEventBus(schema.TeamRoleLeader, 30.0, 30.0)
	ctx := context.Background()
	bus.Start(ctx, nil) // 无 callback
	bus.Enqueue(types.CoordinationEvent{Inner: &types.InnerEventMessage{EventType: types.InnerEventTypeUserInput}})
	time.Sleep(50 * time.Millisecond)
	bus.Stop()
}
