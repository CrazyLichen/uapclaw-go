package coordination

import (
	"context"
	"testing"
	"time"

	schema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/agent/coordination/types"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// fakeKernelHost 实现 KernelHost 用于测试
type fakeKernelHost struct {
	agentReady   bool
	agentRunning bool
	inFlight     bool
	pendingInt   bool
	role         schema.TeamRole
	memberName   string
}

func (f *fakeKernelHost) IsAgentReady() bool                                       { return f.agentReady }
func (f *fakeKernelHost) IsAgentRunning() bool                                     { return f.agentRunning }
func (f *fakeKernelHost) HasInFlightRound() bool                                   { return f.inFlight }
func (f *fakeKernelHost) HasPendingInterrupt() bool                                { return f.pendingInt }
func (f *fakeKernelHost) CancelAgent(_ context.Context) error                      { return nil }
func (f *fakeKernelHost) DeliverInput(_ context.Context, _ any, _ bool) error      { return nil }
func (f *fakeKernelHost) ResumeInterrupt(_ context.Context, _ any) error           { return nil }
func (f *fakeKernelHost) ShutdownSelf(_ context.Context) error                     { return nil }
func (f *fakeKernelHost) ConcludeCompletedRound(_ context.Context, _, _ int) error { return nil }
func (f *fakeKernelHost) Role() schema.TeamRole                                    { return f.role }
func (f *fakeKernelHost) MemberName() string                                       { return f.memberName }
func (f *fakeKernelHost) Blueprint() types.DispatcherBlueprint                     { return &fakeDispatcherBlueprint{role: f.role, memberName: f.memberName} }
func (f *fakeKernelHost) Infra() types.DispatcherInfra                             { return nil }

// ──────────────────────────── CoordinationKernel 测试 ────────────────────────────

func TestNewCoordinationKernel(t *testing.T) {
	host := &fakeKernelHost{role: schema.TeamRoleLeader, memberName: "leader1"}
	k := NewCoordinationKernel(host)
	if k == nil {
		t.Fatal("NewCoordinationKernel 返回 nil")
	}
	if k.LifecycleState() != kernelStateIdle {
		t.Errorf("初始状态应为 idle，实际 %q", k.LifecycleState())
	}
	if k.EventBus() != nil {
		t.Error("setup 前 EventBus 应为 nil")
	}
	if k.Dispatcher() != nil {
		t.Error("setup 前 Dispatcher 应为 nil")
	}
}

func TestCoordinationKernel_Setup(t *testing.T) {
	host := &fakeKernelHost{role: schema.TeamRoleLeader, memberName: "leader1"}
	k := NewCoordinationKernel(host)
	bp := &fakeDispatcherBlueprint{role: schema.TeamRoleLeader, memberName: "leader1"}
	k.Setup(schema.TeamRoleLeader, bp, nil)

	if k.EventBus() == nil {
		t.Error("setup 后 EventBus 不应为 nil")
	}
	if k.Dispatcher() == nil {
		t.Error("setup 后 Dispatcher 不应为 nil")
	}
	if k.Dispatcher().Lifecycle == nil {
		t.Error("setup 后 Dispatcher.Lifecycle 不应为 nil")
	}
}

func TestCoordinationKernel_Start(t *testing.T) {
	host := &fakeKernelHost{role: schema.TeamRoleLeader, memberName: "leader1", agentReady: true}
	k := NewCoordinationKernel(host)
	bp := &fakeDispatcherBlueprint{role: schema.TeamRoleLeader, memberName: "leader1"}
	k.Setup(schema.TeamRoleLeader, bp, nil)

	k.Start(context.Background())

	if !k.IsRunning() {
		t.Error("start 后应运行中")
	}
	if k.LifecycleState() != kernelStateRunning {
		t.Errorf("start 后状态应为 running，实际 %q", k.LifecycleState())
	}
}

func TestCoordinationKernel_Start无Setup(t *testing.T) {
	host := &fakeKernelHost{role: schema.TeamRoleLeader, memberName: "leader1"}
	k := NewCoordinationKernel(host)

	// 无 setup 时 start 应直接返回，不 panic
	k.Start(context.Background())
	if k.IsRunning() {
		t.Error("无 setup 时 start 不应运行")
	}
}

func TestCoordinationKernel_Pause(t *testing.T) {
	host := &fakeKernelHost{role: schema.TeamRoleLeader, memberName: "leader1", agentReady: true}
	k := NewCoordinationKernel(host)
	bp := &fakeDispatcherBlueprint{role: schema.TeamRoleLeader, memberName: "leader1"}
	k.Setup(schema.TeamRoleLeader, bp, nil)
	k.Start(context.Background())

	k.Pause()

	if k.LifecycleState() != kernelStatePaused {
		t.Errorf("pause 后状态应为 paused，实际 %q", k.LifecycleState())
	}
}

func TestCoordinationKernel_Pause幂等(t *testing.T) {
	host := &fakeKernelHost{role: schema.TeamRoleLeader, memberName: "leader1"}
	k := NewCoordinationKernel(host)

	// idle 态 pause 不应 panic
	k.Pause()
	if k.LifecycleState() != kernelStateIdle {
		t.Errorf("idle 态 pause 应保持 idle，实际 %q", k.LifecycleState())
	}
}

func TestCoordinationKernel_Stop(t *testing.T) {
	host := &fakeKernelHost{role: schema.TeamRoleLeader, memberName: "leader1", agentReady: true}
	k := NewCoordinationKernel(host)
	bp := &fakeDispatcherBlueprint{role: schema.TeamRoleLeader, memberName: "leader1"}
	k.Setup(schema.TeamRoleLeader, bp, nil)
	k.Start(context.Background())

	k.Stop()

	if k.LifecycleState() != kernelStateStopped {
		t.Errorf("stop 后状态应为 stopped，实际 %q", k.LifecycleState())
	}
}

func TestCoordinationKernel_Stop幂等(t *testing.T) {
	host := &fakeKernelHost{role: schema.TeamRoleLeader, memberName: "leader1"}
	k := NewCoordinationKernel(host)

	// idle 态 stop 不应 panic
	k.Stop()
	if k.LifecycleState() != kernelStateIdle {
		t.Errorf("idle 态 stop 应保持 idle，实际 %q", k.LifecycleState())
	}

	// stopped 态再次 stop
	k.lifecycleState = kernelStateStopped
	k.Stop()
	if k.LifecycleState() != kernelStateStopped {
		t.Errorf("stopped 态 stop 应保持 stopped，实际 %q", k.LifecycleState())
	}
}

func TestCoordinationKernel_EnqueueUserInput(t *testing.T) {
	host := &fakeKernelHost{role: schema.TeamRoleLeader, memberName: "leader1", agentReady: true}
	k := NewCoordinationKernel(host)
	bp := &fakeDispatcherBlueprint{role: schema.TeamRoleLeader, memberName: "leader1"}
	k.Setup(schema.TeamRoleLeader, bp, nil)

	var receivedEvent types.CoordinationEvent
	k.eventBus.Start(context.Background(), func(ctx context.Context, event types.CoordinationEvent) {
		receivedEvent = event
	})

	k.EnqueueUserInput("hello team")
	time.Sleep(50 * time.Millisecond)

	if receivedEvent.Inner == nil || receivedEvent.Inner.EventType != types.InnerEventTypeUserInput {
		t.Error("EnqueueUserInput 应产生 USER_INPUT 事件")
	}
}

func TestCoordinationKernel_WakeMailboxIfInterruptCleared(t *testing.T) {
	host := &fakeKernelHost{role: schema.TeamRoleTeammate, memberName: "worker1", agentReady: true, pendingInt: false}
	k := NewCoordinationKernel(host)
	bp := &fakeDispatcherBlueprint{role: schema.TeamRoleTeammate, memberName: "worker1"}
	k.Setup(schema.TeamRoleTeammate, bp, nil)

	var receivedEvent types.CoordinationEvent
	k.eventBus.Start(context.Background(), func(ctx context.Context, event types.CoordinationEvent) {
		receivedEvent = event
	})

	k.WakeMailboxIfInterruptCleared()
	time.Sleep(50 * time.Millisecond)

	if receivedEvent.Inner == nil || receivedEvent.Inner.EventType != types.InnerEventTypePollMailbox {
		t.Error("WakeMailboxIfInterruptCleared 应产生 POLL_MAILBOX 事件")
	}
}

func TestCoordinationKernel_WakeMailbox_Leader跳过(t *testing.T) {
	host := &fakeKernelHost{role: schema.TeamRoleLeader, memberName: "leader1", agentReady: true, pendingInt: false}
	k := NewCoordinationKernel(host)
	bp := &fakeDispatcherBlueprint{role: schema.TeamRoleLeader, memberName: "leader1"}
	k.Setup(schema.TeamRoleLeader, bp, nil)

	var received bool
	k.eventBus.Start(context.Background(), func(ctx context.Context, event types.CoordinationEvent) {
		received = true
	})

	k.WakeMailboxIfInterruptCleared()

	if received {
		t.Error("Leader 不应触发 WakeMailboxIfInterruptCleared")
	}
}

func TestCoordinationKernel_WakeMailbox_有中断跳过(t *testing.T) {
	host := &fakeKernelHost{role: schema.TeamRoleTeammate, memberName: "worker1", agentReady: true, pendingInt: true}
	k := NewCoordinationKernel(host)
	bp := &fakeDispatcherBlueprint{role: schema.TeamRoleTeammate, memberName: "worker1"}
	k.Setup(schema.TeamRoleTeammate, bp, nil)

	var received bool
	k.eventBus.Start(context.Background(), func(ctx context.Context, event types.CoordinationEvent) {
		received = true
	})

	k.WakeMailboxIfInterruptCleared()

	if received {
		t.Error("有 pending interrupt 时不应触发 WakeMailboxIfInterruptCleared")
	}
}

func TestCoordinationKernel_生命周期转换(t *testing.T) {
	host := &fakeKernelHost{role: schema.TeamRoleLeader, memberName: "leader1", agentReady: true}
	k := NewCoordinationKernel(host)
	bp := &fakeDispatcherBlueprint{role: schema.TeamRoleLeader, memberName: "leader1"}
	k.Setup(schema.TeamRoleLeader, bp, nil)

	// idle → running
	k.Start(context.Background())
	if k.LifecycleState() != kernelStateRunning {
		t.Errorf("期望 running，实际 %q", k.LifecycleState())
	}

	// running → paused
	k.Pause()
	if k.LifecycleState() != kernelStatePaused {
		t.Errorf("期望 paused，实际 %q", k.LifecycleState())
	}

	// paused → stopped
	k.Stop()
	if k.LifecycleState() != kernelStateStopped {
		t.Errorf("期望 stopped，实际 %q", k.LifecycleState())
	}

	// stopped → stop again (幂等)
	k.Stop()
	if k.LifecycleState() != kernelStateStopped {
		t.Errorf("stop 幂等应保持 stopped，实际 %q", k.LifecycleState())
	}
}
