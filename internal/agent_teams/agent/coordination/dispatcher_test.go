package coordination

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/agent/coordination/types"
	schema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema/events"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// fakeDispatcherHost 实现 types.DispatcherHost 用于测试
type fakeDispatcherHost struct {
	agentReady   bool
	agentRunning bool
	inFlight     bool
	pendingInt   bool
}

func (f *fakeDispatcherHost) IsAgentReady() bool                                  { return f.agentReady }
func (f *fakeDispatcherHost) IsAgentRunning() bool                                { return f.agentRunning }
func (f *fakeDispatcherHost) HasInFlightRound() bool                              { return f.inFlight }
func (f *fakeDispatcherHost) HasPendingInterrupt() bool                           { return f.pendingInt }
func (f *fakeDispatcherHost) CancelAgent(_ context.Context) error                 { return nil }
func (f *fakeDispatcherHost) DeliverInput(_ context.Context, _ any, _ bool) error { return nil }
func (f *fakeDispatcherHost) ResumeInterrupt(_ context.Context, _ *interaction.InteractiveInput) error {
	return nil
}
func (f *fakeDispatcherHost) ShutdownSelf(_ context.Context) error                     { return nil }
func (f *fakeDispatcherHost) ConcludeCompletedRound(_ context.Context, _, _ int) error { return nil }

// fakeDispatcherBlueprint 实现 types.DispatcherBlueprint 用于测试
type fakeDispatcherBlueprint struct {
	role       schema.TeamRole
	memberName string
}

func (f *fakeDispatcherBlueprint) Role() schema.TeamRole { return f.role }
func (f *fakeDispatcherBlueprint) MemberName() string    { return f.memberName }

// fakePollCtrl 实现 types.PollController 用于测试
type fakePollCtrl struct {
	paused bool
}

func (f *fakePollCtrl) PausePolls()                   { f.paused = true }
func (f *fakePollCtrl) ResumePolls(_ context.Context) { f.paused = false }

// newTestDispatcher 创建测试用 EventDispatcher（4 参数，内部自动创建 handler）。
func newTestDispatcher(host types.DispatcherHost, bp types.DispatcherBlueprint, pollCtrl types.PollController) *EventDispatcher {
	return NewEventDispatcher(host, bp, nil, pollCtrl)
}

// ──────────────────────────── 适配层测试 ────────────────────────────

func TestWrapCallback_适配层(t *testing.T) {
	var called bool
	var receivedEvent types.CoordinationEvent
	innerFn := coordCallbackFunc(func(ctx context.Context, event types.CoordinationEvent) {
		called = true
		receivedEvent = event
	})
	wrapped := wrapCallback(innerFn)
	event := types.CoordinationEvent{Inner: &types.InnerEventMessage{EventType: types.InnerEventTypeUserInput, Payload: map[string]any{"content": "hello"}}}
	data := packEvent(event)
	result := wrapped(context.Background(), data)
	if !called {
		t.Error("wrapCallback 应调用内部函数")
	}
	if receivedEvent.Inner.EventType != types.InnerEventTypeUserInput {
		t.Errorf("wrapCallback 应传递原始事件，got %q", receivedEvent.Inner.EventType)
	}
	if result != nil {
		t.Errorf("wrapCallback 应返回 nil，got %v", result)
	}
}

func TestWrapCallback_空数据(t *testing.T) {
	var called bool
	innerFn := coordCallbackFunc(func(ctx context.Context, event types.CoordinationEvent) {
		called = true
	})
	wrapped := wrapCallback(innerFn)
	wrapped(context.Background(), map[string]any{}) // 无 coordEventMapKey
	if called {
		t.Error("无 event key 时不应调用内部函数")
	}
}

func TestPackEvent(t *testing.T) {
	event := types.CoordinationEvent{Inner: &types.InnerEventMessage{EventType: types.InnerEventTypeShutdown}}
	data := packEvent(event)
	raw, ok := data[coordEventMapKey]
	if !ok {
		t.Error("packEvent 应包含 coordination event key")
	}
	got, ok := raw.(types.CoordinationEvent)
	if !ok {
		t.Error("packEvent 值应为 CoordinationEvent 类型")
	}
	if got.Inner.EventType != types.InnerEventTypeShutdown {
		t.Error("packEvent 应保留原始事件数据")
	}
}

// ──────────────────────────── 粗筛测试 ────────────────────────────

func TestEventDispatcher_Agent未就绪跳过(t *testing.T) {
	host := &fakeDispatcherHost{agentReady: false}
	bp := &fakeDispatcherBlueprint{role: schema.TeamRoleLeader, memberName: "leader1"}
	d := newTestDispatcher(host, bp, &fakePollCtrl{})

	triggered := false
	d.framework.OnCustom("user_input", func(ctx context.Context, data map[string]any) any {
		triggered = true
		return nil
	})

	d.Dispatch(context.Background(), types.CoordinationEvent{Inner: &types.InnerEventMessage{EventType: types.InnerEventTypeUserInput}})
	if triggered {
		t.Error("agent 未就绪时不应触发回调")
	}
}

func TestEventDispatcher_Inner事件正常触发(t *testing.T) {
	host := &fakeDispatcherHost{agentReady: true}
	bp := &fakeDispatcherBlueprint{role: schema.TeamRoleLeader, memberName: "leader1"}
	d := newTestDispatcher(host, bp, &fakePollCtrl{})

	triggered := false
	d.framework.OnCustom("user_input", func(ctx context.Context, data map[string]any) any {
		triggered = true
		return nil
	})

	d.Dispatch(context.Background(), types.CoordinationEvent{
		Inner: &types.InnerEventMessage{EventType: types.InnerEventTypeUserInput, Payload: map[string]any{"content": "hi"}},
	})
	if !triggered {
		t.Error("agent 就绪时 user_input 应触发回调")
	}
}

func TestEventDispatcher_HumanAgent轮询跳过(t *testing.T) {
	host := &fakeDispatcherHost{agentReady: true}
	bp := &fakeDispatcherBlueprint{role: schema.TeamRoleHumanAgent, memberName: "ha1"}
	d := newTestDispatcher(host, bp, &fakePollCtrl{})

	mailboxTriggered := false
	d.framework.OnCustom("coordination_poll_mailbox", func(ctx context.Context, data map[string]any) any {
		mailboxTriggered = true
		return nil
	})
	taskTriggered := false
	d.framework.OnCustom("coordination_poll_task", func(ctx context.Context, data map[string]any) any {
		taskTriggered = true
		return nil
	})

	d.Dispatch(context.Background(), types.CoordinationEvent{Inner: &types.InnerEventMessage{EventType: types.InnerEventTypePollMailbox}})
	d.Dispatch(context.Background(), types.CoordinationEvent{Inner: &types.InnerEventMessage{EventType: types.InnerEventTypePollTask}})
	if mailboxTriggered {
		t.Error("Human-agent 不应触发 POLL_MAILBOX")
	}
	if taskTriggered {
		t.Error("Human-agent 不应触发 POLL_TASK")
	}
}

func TestEventDispatcher_HumanAgent白名单(t *testing.T) {
	host := &fakeDispatcherHost{agentReady: true}
	bp := &fakeDispatcherBlueprint{role: schema.TeamRoleHumanAgent, memberName: "ha1"}
	d := newTestDispatcher(host, bp, &fakePollCtrl{})

	// 白名单内事件应触发
	allowedEvents := []string{"team_cleaned", "member_shutdown", "member_canceled", "team_standby", "message", "broadcast", "task_claimed"}
	for _, evType := range allowedEvents {
		triggered := false
		d.framework.OnCustom(evType, func(ctx context.Context, data map[string]any) any {
			triggered = true
			return nil
		})
		d.Dispatch(context.Background(), types.CoordinationEvent{
			Transport: &events.EventMessage{EventType: evType},
		})
		if !triggered {
			t.Errorf("Human-agent 白名单事件 %q 应触发", evType)
		}
	}

	// 白名单外事件应跳过
	blockedEvents := []string{"task_created", "task_updated", "member_status_changed"}
	for _, evType := range blockedEvents {
		triggered := false
		d.framework.OnCustom(evType, func(ctx context.Context, data map[string]any) any {
			triggered = true
			return nil
		})
		d.Dispatch(context.Background(), types.CoordinationEvent{
			Transport: &events.EventMessage{EventType: evType},
		})
		if triggered {
			t.Errorf("Human-agent 白名单外事件 %q 不应触发", evType)
		}
	}
}

func TestEventDispatcher_无MemberName跳过Transport(t *testing.T) {
	host := &fakeDispatcherHost{agentReady: true}
	bp := &fakeDispatcherBlueprint{role: schema.TeamRoleLeader, memberName: ""}
	d := newTestDispatcher(host, bp, &fakePollCtrl{})

	triggered := false
	d.framework.OnCustom("message", func(ctx context.Context, data map[string]any) any {
		triggered = true
		return nil
	})

	d.Dispatch(context.Background(), types.CoordinationEvent{
		Transport: &events.EventMessage{EventType: "message"},
	})
	if triggered {
		t.Error("无 member_name 时 transport 事件应被跳过")
	}
}

// ──────────────────────────── 接线集成测试 ────────────────────────────

func TestEventDispatcher_内部创建Handler(t *testing.T) {
	host := &fakeDispatcherHost{agentReady: true}
	bp := &fakeDispatcherBlueprint{role: schema.TeamRoleLeader, memberName: "leader1"}
	d := newTestDispatcher(host, bp, &fakePollCtrl{})

	if d.Lifecycle == nil {
		t.Error("Lifecycle handler 不应为 nil")
	}
	if d.Member == nil {
		t.Error("Member handler 不应为 nil")
	}
	if d.Message == nil {
		t.Error("Message handler 不应为 nil")
	}
	if d.TaskBoard == nil {
		t.Error("TaskBoard handler 不应为 nil")
	}
	if d.StaleTask == nil {
		t.Error("StaleTask handler 不应为 nil")
	}
	if d.TeamCompletion == nil {
		t.Error("TeamCompletion handler 不应为 nil")
	}
}

func TestEventDispatcher_共享StaleClaimThrottle(t *testing.T) {
	host := &fakeDispatcherHost{agentReady: true}
	bp := &fakeDispatcherBlueprint{role: schema.TeamRoleLeader, memberName: "leader1"}
	d := newTestDispatcher(host, bp, &fakePollCtrl{})

	if d.Member.StaleClaimThrottle() == nil {
		t.Error("Member handler 的 staleClaimThrottle 不应为 nil")
	}
	d.Member.StaleClaimThrottle()["task-1"] = 123.0
	val, ok := d.StaleTask.StaleClaimThrottle()["task-1"]
	if !ok || val != 123.0 {
		t.Error("Member 和 StaleTask 应共享同一个 staleClaimThrottle 引用")
	}
}

func TestEventDispatcher_Handler回调已注册(t *testing.T) {
	host := &fakeDispatcherHost{agentReady: true}
	bp := &fakeDispatcherBlueprint{role: schema.TeamRoleLeader, memberName: "leader1"}
	d := newTestDispatcher(host, bp, &fakePollCtrl{})

	innerEventTypes := []types.InnerEventType{
		types.InnerEventTypeUserInput,
		types.InnerEventTypePollMailbox,
		types.InnerEventTypePollTask,
	}
	for _, evType := range innerEventTypes {
		d.Dispatch(context.Background(), types.CoordinationEvent{
			Inner: &types.InnerEventMessage{EventType: evType},
		})
	}

	transportEvents := []string{
		events.TeamEventStandby,
		events.TeamEventCleaned,
		events.TeamEventMemberSpawned,
		events.TeamEventMessage,
		events.TeamEventTaskClaimed,
		events.TeamEventTeamCompleted,
	}
	for _, evType := range transportEvents {
		d.Dispatch(context.Background(), types.CoordinationEvent{
			Transport: &events.EventMessage{EventType: evType},
		})
	}
}

func TestEventDispatcher_verifyDispatcherReady(t *testing.T) {
	host := &fakeDispatcherHost{agentReady: true}
	bp := &fakeDispatcherBlueprint{role: schema.TeamRoleLeader, memberName: "leader1"}
	d := newTestDispatcher(host, bp, &fakePollCtrl{})

	if err := d.verifyDispatcherReady(); err != nil {
		t.Errorf("handler 接线完成后 verifyDispatcherReady 不应返回错误: %v", err)
	}
}

func TestEventDispatcher_TeamCompletionRearm(t *testing.T) {
	host := &fakeDispatcherHost{agentReady: true}
	bp := &fakeDispatcherBlueprint{role: schema.TeamRoleLeader, memberName: "leader1"}
	d := newTestDispatcher(host, bp, &fakePollCtrl{})

	d.TeamCompletion.Rearm()
}
