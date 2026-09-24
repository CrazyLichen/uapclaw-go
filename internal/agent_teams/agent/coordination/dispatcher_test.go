package coordination

import (
	"context"
	"testing"

	schema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema/events"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// fakeDispatcherHost 实现 DispatcherHost 用于测试
type fakeDispatcherHost struct {
	agentReady   bool
	agentRunning bool
	inFlight     bool
	pendingInt   bool
}

func (f *fakeDispatcherHost) IsAgentReady() bool                                                { return f.agentReady }
func (f *fakeDispatcherHost) IsAgentRunning() bool                                              { return f.agentRunning }
func (f *fakeDispatcherHost) HasInFlightRound() bool                                            { return f.inFlight }
func (f *fakeDispatcherHost) HasPendingInterrupt() bool                                         { return f.pendingInt }
func (f *fakeDispatcherHost) CancelAgent(_ context.Context) error                               { return nil }
func (f *fakeDispatcherHost) DeliverInput(_ context.Context, _ any, _ bool) error               { return nil }
func (f *fakeDispatcherHost) ResumeInterrupt(_ context.Context, _ any) error                    { return nil }
func (f *fakeDispatcherHost) ShutdownSelf(_ context.Context) error                              { return nil }
func (f *fakeDispatcherHost) ConcludeCompletedRound(_ context.Context, _, _ int) error          { return nil }

// fakeDispatcherBlueprint 实现 DispatcherBlueprint 用于测试
type fakeDispatcherBlueprint struct {
	role       schema.TeamRole
	memberName string
}

func (f *fakeDispatcherBlueprint) Role() schema.TeamRole   { return f.role }
func (f *fakeDispatcherBlueprint) MemberName() string      { return f.memberName }

// fakePollCtrl 实现 PollController 用于测试
type fakePollCtrl struct {
	paused bool
}

func (f *fakePollCtrl) PausePolls()  { f.paused = true }
func (f *fakePollCtrl) ResumePolls() { f.paused = false }

// ──────────────────────────── 适配层测试 ────────────────────────────

func TestWrapCallback_适配层(t *testing.T) {
	var called bool
	var receivedEvent CoordinationEvent
	innerFn := coordCallbackFunc(func(ctx context.Context, event CoordinationEvent) {
		called = true
		receivedEvent = event
	})
	wrapped := wrapCallback(innerFn)
	event := CoordinationEvent{Inner: &InnerEventMessage{EventType: InnerEventTypeUserInput, Payload: map[string]any{"content": "hello"}}}
	data := packEvent(event)
	result := wrapped(context.Background(), data)
	if !called {
		t.Error("wrapCallback 应调用内部函数")
	}
	if receivedEvent.Inner.EventType != InnerEventTypeUserInput {
		t.Errorf("wrapCallback 应传递原始事件，got %q", receivedEvent.Inner.EventType)
	}
	if result != nil {
		t.Errorf("wrapCallback 应返回 nil，got %v", result)
	}
}

func TestWrapCallback_空数据(t *testing.T) {
	var called bool
	innerFn := coordCallbackFunc(func(ctx context.Context, event CoordinationEvent) {
		called = true
	})
	wrapped := wrapCallback(innerFn)
	wrapped(context.Background(), map[string]any{}) // 无 coordEventMapKey
	if called {
		t.Error("无 event key 时不应调用内部函数")
	}
}

func TestPackEvent(t *testing.T) {
	event := CoordinationEvent{Inner: &InnerEventMessage{EventType: InnerEventTypeShutdown}}
	data := packEvent(event)
	raw, ok := data[coordEventMapKey]
	if !ok {
		t.Error("packEvent 应包含 coordination event key")
	}
	got, ok := raw.(CoordinationEvent)
	if !ok {
		t.Error("packEvent 值应为 CoordinationEvent 类型")
	}
	if got.Inner.EventType != InnerEventTypeShutdown {
		t.Error("packEvent 应保留原始事件数据")
	}
}

// ──────────────────────────── 粗筛测试 ────────────────────────────

func TestEventDispatcher_Agent未就绪跳过(t *testing.T) {
	host := &fakeDispatcherHost{agentReady: false}
	bp := &fakeDispatcherBlueprint{role: schema.TeamRoleLeader, memberName: "leader1"}
	d := NewEventDispatcher(host, bp, nil, &fakePollCtrl{}, nil)

	// 手动注册一个回调验证不被触发
	triggered := false
	d.framework.OnCustom("user_input", func(ctx context.Context, data map[string]any) any {
		triggered = true
		return nil
	})

	d.Dispatch(context.Background(), CoordinationEvent{Inner: &InnerEventMessage{EventType: InnerEventTypeUserInput}})
	if triggered {
		t.Error("agent 未就绪时不应触发回调")
	}
}

func TestEventDispatcher_Inner事件正常触发(t *testing.T) {
	host := &fakeDispatcherHost{agentReady: true}
	bp := &fakeDispatcherBlueprint{role: schema.TeamRoleLeader, memberName: "leader1"}
	d := NewEventDispatcher(host, bp, nil, &fakePollCtrl{}, nil)

	// 手动注册一个回调
	triggered := false
	d.framework.OnCustom("user_input", func(ctx context.Context, data map[string]any) any {
		triggered = true
		return nil
	})

	d.Dispatch(context.Background(), CoordinationEvent{
		Inner: &InnerEventMessage{EventType: InnerEventTypeUserInput, Payload: map[string]any{"content": "hi"}},
	})
	if !triggered {
		t.Error("agent 就绪时 user_input 应触发回调")
	}
}

func TestEventDispatcher_HumanAgent轮询跳过(t *testing.T) {
	host := &fakeDispatcherHost{agentReady: true}
	bp := &fakeDispatcherBlueprint{role: schema.TeamRoleHumanAgent, memberName: "ha1"}
	d := NewEventDispatcher(host, bp, nil, &fakePollCtrl{}, nil)

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

	d.Dispatch(context.Background(), CoordinationEvent{Inner: &InnerEventMessage{EventType: InnerEventTypePollMailbox}})
	d.Dispatch(context.Background(), CoordinationEvent{Inner: &InnerEventMessage{EventType: InnerEventTypePollTask}})
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
	d := NewEventDispatcher(host, bp, nil, &fakePollCtrl{}, nil)

	// 白名单内事件应触发
	allowedEvents := []string{"team_cleaned", "member_shutdown", "member_canceled", "team_standby", "message", "broadcast", "task_claimed"}
	for _, evType := range allowedEvents {
		triggered := false
		d.framework.OnCustom(evType, func(ctx context.Context, data map[string]any) any {
			triggered = true
			return nil
		})
		d.Dispatch(context.Background(), CoordinationEvent{
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
		d.Dispatch(context.Background(), CoordinationEvent{
			Transport: &events.EventMessage{EventType: evType},
		})
		if triggered {
			t.Errorf("Human-agent 白名单外事件 %q 不应触发", evType)
		}
	}
}

func TestEventDispatcher_无MemberName跳过Transport(t *testing.T) {
	host := &fakeDispatcherHost{agentReady: true}
	bp := &fakeDispatcherBlueprint{role: schema.TeamRoleLeader, memberName: ""} // 空 member_name
	d := NewEventDispatcher(host, bp, nil, &fakePollCtrl{}, nil)

	triggered := false
	d.framework.OnCustom("message", func(ctx context.Context, data map[string]any) any {
		triggered = true
		return nil
	})

	d.Dispatch(context.Background(), CoordinationEvent{
		Transport: &events.EventMessage{EventType: "message"},
	})
	if triggered {
		t.Error("无 member_name 时 transport 事件应被跳过")
	}
}
