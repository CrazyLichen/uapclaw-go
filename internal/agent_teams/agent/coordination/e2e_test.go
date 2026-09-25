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

// ──────────────────────────── 端到端集成测试 ────────────────────────────

// fakeTrackableHost 可追踪 handler 调用状态的 fake host
type fakeTrackableHost struct {
	fakeKernelHost
	mu             sync.Mutex
	deliveredInputs []any
	shutdownCalled bool
	cancelCalled   bool
}

func (f *fakeTrackableHost) DeliverInput(_ context.Context, content any, _ bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deliveredInputs = append(f.deliveredInputs, content)
	return nil
}

func (f *fakeTrackableHost) ShutdownSelf(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.shutdownCalled = true
	return nil
}

func (f *fakeTrackableHost) CancelAgent(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancelCalled = true
	return nil
}

func (f *fakeTrackableHost) DeliveredInputs() []any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]any{}, f.deliveredInputs...)
}

func (f *fakeTrackableHost) WasShutdownCalled() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.shutdownCalled
}

func (f *fakeTrackableHost) WasCancelCalled() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cancelCalled
}

// TestEndToEnd_Kernel完整链路 测试 EventBus → Dispatcher → Handler 完整唤醒链路
func TestEndToEnd_Kernel完整链路(t *testing.T) {
	host := &fakeTrackableHost{
		fakeKernelHost: fakeKernelHost{
			agentReady: true,
			role:       schema.TeamRoleLeader,
			memberName: "leader1",
		},
	}
	k := NewCoordinationKernel(host)
	bp := &fakeDispatcherBlueprint{role: schema.TeamRoleLeader, memberName: "leader1"}
	k.Setup(schema.TeamRoleLeader, bp, nil)
	k.Start(context.Background())

	// 1. 验证 user_input → lifecycle.OnUserInput → DeliverInput
	k.EnqueueUserInput("hello from user")
	time.Sleep(100 * time.Millisecond)
	inputs := host.DeliveredInputs()
	if len(inputs) == 0 {
		t.Error("user_input 应触发 DeliverInput")
	}

	// 2. 验证 team_standby → lifecycle.OnStandby → PausePolls
	k.Enqueue(types.CoordinationEvent{
		Transport: &events.EventMessage{EventType: events.TeamEventStandby},
	})
	time.Sleep(50 * time.Millisecond)
	if k.EventBus() != nil && !k.EventBus().PollsPaused() {
		t.Error("team_standby 应暂停轮询")
	}

	// 恢复轮询以便后续测试
	k.EventBus().ResumePolls()

	// 3. 验证 team_cleaned → lifecycle.OnCleaned（Leader 不关闭）
	k.Enqueue(types.CoordinationEvent{
		Transport: &events.EventMessage{EventType: events.TeamEventCleaned},
	})
	time.Sleep(50 * time.Millisecond)
	if host.WasShutdownCalled() {
		t.Error("Leader 收到 team_cleaned 不应调用 shutdown_self")
	}

	// 停止
	k.Stop()
}

// TestEndToEnd_Teammate取消 测试 teammate MEMBER_CANCELED → cancel_agent
func TestEndToEnd_Teammate取消(t *testing.T) {
	host := &fakeTrackableHost{
		fakeKernelHost: fakeKernelHost{
			agentReady: true,
			role:       schema.TeamRoleTeammate,
			memberName: "worker1",
		},
	}
	k := NewCoordinationKernel(host)
	bp := &fakeDispatcherBlueprint{role: schema.TeamRoleTeammate, memberName: "worker1"}
	k.Setup(schema.TeamRoleTeammate, bp, nil)
	k.Start(context.Background())

	// 发送 member_canceled 目标为自己
	k.Enqueue(types.CoordinationEvent{
		Transport: &events.EventMessage{
			EventType: events.TeamEventMemberCanceled,
			Payload:   map[string]any{"member_name": "worker1"},
		},
	})
	time.Sleep(100 * time.Millisecond)

	if !host.WasCancelCalled() {
		t.Error("MEMBER_CANCELED 目标为自己时应调用 cancel_agent")
	}

	k.Stop()
}

// TestEndToEnd_Teammate清理 测试 teammate team_cleaned → shutdown_self
func TestEndToEnd_Teammate清理(t *testing.T) {
	host := &fakeTrackableHost{
		fakeKernelHost: fakeKernelHost{
			agentReady: true,
			role:       schema.TeamRoleTeammate,
			memberName: "worker1",
		},
	}
	k := NewCoordinationKernel(host)
	bp := &fakeDispatcherBlueprint{role: schema.TeamRoleTeammate, memberName: "worker1"}
	k.Setup(schema.TeamRoleTeammate, bp, nil)
	k.Start(context.Background())

	k.Enqueue(types.CoordinationEvent{
		Transport: &events.EventMessage{EventType: events.TeamEventCleaned},
	})
	time.Sleep(100 * time.Millisecond)

	if !host.WasShutdownCalled() {
		t.Error("Teammate 收到 team_cleaned 应调用 shutdown_self")
	}

	k.Stop()
}

// TestEndToEnd_HumanAgent轮询过滤 测试 Human-agent 不接收 poll 事件
func TestEndToEnd_HumanAgent轮询过滤(t *testing.T) {
	host := &fakeTrackableHost{
		fakeKernelHost: fakeKernelHost{
			agentReady: true,
			role:       schema.TeamRoleHumanAgent,
			memberName: "ha1",
		},
	}
	k := NewCoordinationKernel(host)
	bp := &fakeDispatcherBlueprint{role: schema.TeamRoleHumanAgent, memberName: "ha1"}
	k.Setup(schema.TeamRoleHumanAgent, bp, nil)

	// Human-agent 不启用周期轮询，手动发 poll 事件
	var pollTriggered bool
	d := k.Dispatcher()
	d.framework.OnCustom("coordination_poll_mailbox", func(ctx context.Context, data map[string]any) any {
		pollTriggered = true
		return nil
	})

	k.Start(context.Background())

	// Dispatcher 粗筛应阻止 Human-agent 的 poll 事件
	// 直接通过 Dispatcher.Dispatch 测试
	k.Dispatcher().Dispatch(context.Background(), types.CoordinationEvent{
		Inner: &types.InnerEventMessage{EventType: types.InnerEventTypePollMailbox},
	})
	if pollTriggered {
		t.Error("Human-agent 不应触发 POLL_MAILBOX 回调")
	}

	k.Stop()
}

// TestEndToEnd_TeamCompletion回调 测试 TASK_LIST_DRAINED 触发注册回调
func TestEndToEnd_TeamCompletion回调(t *testing.T) {
	host := &fakeTrackableHost{
		fakeKernelHost: fakeKernelHost{
			agentReady: true,
			role:       schema.TeamRoleLeader,
			memberName: "leader1",
		},
	}
	k := NewCoordinationKernel(host)
	bp := &fakeDispatcherBlueprint{role: schema.TeamRoleLeader, memberName: "leader1"}
	k.Setup(schema.TeamRoleLeader, bp, nil)
	k.Start(context.Background())

	var callbackInvoked bool
	k.Dispatcher().TeamCompletion.RegisterCompletionCallback(func(_ context.Context) error {
		callbackInvoked = true
		return nil
	})

	// 发送 TASK_LIST_DRAINED 事件
	k.Enqueue(types.CoordinationEvent{
		Transport: &events.EventMessage{
			EventType: events.TeamEventTaskListDrained,
			Payload:   map[string]any{},
		},
	})
	time.Sleep(100 * time.Millisecond)

	if !callbackInvoked {
		t.Error("TASK_LIST_DRAINED 应触发注册的完成回调")
	}

	k.Stop()
}

// TestEndToEnd_共享节流映射 测试 Member 和 StaleTask 共享 staleClaimThrottle
func TestEndToEnd_共享节流映射(t *testing.T) {
	host := &fakeTrackableHost{
		fakeKernelHost: fakeKernelHost{
			agentReady: true,
			role:       schema.TeamRoleLeader,
			memberName: "leader1",
		},
	}
	k := NewCoordinationKernel(host)
	bp := &fakeDispatcherBlueprint{role: schema.TeamRoleLeader, memberName: "leader1"}
	k.Setup(schema.TeamRoleLeader, bp, nil)

	// 在 Member handler 中写入值，StaleTask handler 应能读到
	memberThrottle := k.Dispatcher().Member.StaleClaimThrottle()
	staleThrottle := k.Dispatcher().StaleTask.StaleClaimThrottle()

	memberThrottle["task-1"] = 100.0
	if val, ok := staleThrottle["task-1"]; !ok || val != 100.0 {
		t.Error("Member 和 StaleTask 应共享 staleClaimThrottle 引用")
	}
}
