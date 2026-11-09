package coordination

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/agent/coordination/types"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/memory"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/messager"
	schema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// fakeHarnessAccessor 测试用 HarnessAccessor 实现（覆盖 kernel.Start 步骤 6.5 的 harness.Start 接线）。
type fakeHarnessAccessor struct {
	startCalled bool
	startErr    error
}

func (f *fakeHarnessAccessor) RegisterMemberTools(_ *memory.TeamMemoryManager) {}
func (f *fakeHarnessAccessor) InjectMemberMemory(_ context.Context, _ *memory.TeamMemoryManager, _ string) error {
	return nil
}
func (f *fakeHarnessAccessor) Model() *llm.Model { return nil }
func (f *fakeHarnessAccessor) Start(_ context.Context, _ any) error {
	f.startCalled = true
	return f.startErr
}

// fakeStreamControllerAccessor 测试用 StreamControllerAccessor 实现。
type fakeStreamControllerAccessor struct {
	startCalled bool
}

func (f *fakeStreamControllerAccessor) ResetStreamQueue()       {}
func (f *fakeStreamControllerAccessor) Start(_ context.Context) { f.startCalled = true }

// fakeDispatcherInfra S-32: 测试用 DispatcherInfra 实现（对齐 *TeamInfra 具体类型签名）
type fakeDispatcherInfra struct{}

func (f *fakeDispatcherInfra) TaskManager() *tools.TeamTaskManager       { return nil }
func (f *fakeDispatcherInfra) MessageManager() *tools.TeamMessageManager { return nil }
func (f *fakeDispatcherInfra) TeamBackend() *tools.TeamBackend           { return nil }
func (f *fakeDispatcherInfra) Messager() messager.Messager               { return nil }

// fakeKernelHost 实现 KernelHost 用于测试
type fakeKernelHost struct {
	agentReady   bool
	agentRunning bool
	inFlight     bool
	pendingInt   bool
	role         schema.TeamRole
	memberName   string
	// bp/inf M-02: Setup 从 host 取依赖；为 nil 时按 role/memberName 惰性构造默认值
	bp  types.DispatcherBlueprint
	inf types.DispatcherInfra
	// harnessAccessor 非 nil 时返回（覆盖 kernel.Start 步骤 6.5 的 harness.Start 接线）
	harnessAccessor types.HarnessAccessor
	// streamControllerAccessor 非 nil 时返回
	streamControllerAccessor types.StreamControllerAccessor
}

func (f *fakeKernelHost) IsAgentReady() bool                                  { return f.agentReady }
func (f *fakeKernelHost) IsAgentRunning() bool                                { return f.agentRunning }
func (f *fakeKernelHost) HasInFlightRound() bool                              { return f.inFlight }
func (f *fakeKernelHost) HasPendingInterrupt() bool                           { return f.pendingInt }
func (f *fakeKernelHost) CancelAgent(_ context.Context) error                 { return nil }
func (f *fakeKernelHost) AnnounceTaskRevisions(_ context.Context) error       { return nil }
func (f *fakeKernelHost) DeliverInput(_ context.Context, _ any, _ bool) error { return nil }
func (f *fakeKernelHost) ResumeInterrupt(_ context.Context, _ *interaction.InteractiveInput) error {
	return nil
}
func (f *fakeKernelHost) ShutdownSelf(_ context.Context) error                     { return nil }
func (f *fakeKernelHost) ConcludeCompletedRound(_ context.Context, _, _ int) error { return nil }
func (f *fakeKernelHost) Role() schema.TeamRole                                    { return f.role }
func (f *fakeKernelHost) MemberName() string                                       { return f.memberName }
func (f *fakeKernelHost) TeamName() string                                         { return "test-team" }
func (f *fakeKernelHost) RecoverTeam(_ context.Context) ([]string, error)          { return nil, nil }
func (f *fakeKernelHost) UpdateStatus(_ context.Context, _ schema.MemberStatus) error {
	return nil
}
func (f *fakeKernelHost) Blueprint() types.DispatcherBlueprint {
	return &fakeDispatcherBlueprint{role: f.role, memberName: f.memberName}
}
func (f *fakeKernelHost) Infra() types.DispatcherInfra                              { return nil }
func (f *fakeKernelHost) SessionController() types.SessionController                { return nil }
func (f *fakeKernelHost) TeamBackendAccessor() types.TeamBackendAccessor            { return nil }
func (f *fakeKernelHost) WorkspaceManager() types.WorkspaceAccessor                 { return nil }
func (f *fakeKernelHost) SetWorkspaceInitialized()                                  {}
func (f *fakeKernelHost) IsWorkspaceInitialized() bool                              { return false }
func (f *fakeKernelHost) MemoryManager() *memory.TeamMemoryManager                  { return nil }
func (f *fakeKernelHost) HarnessAccessor() types.HarnessAccessor                    { return f.harnessAccessor }
func (f *fakeKernelHost) SpecAny() any                                              { return nil }
func (f *fakeKernelHost) SubscribeTransport(_ context.Context, _ string) error      { return nil }
func (f *fakeKernelHost) UnsubscribeTransport(_ context.Context) error              { return nil }
func (f *fakeKernelHost) PersistAllocatorState()                                    {}
func (f *fakeKernelHost) DrainAgentTask(_ context.Context)                          {}
func (f *fakeKernelHost) MarkLiveTeammates(_ context.Context, _ string) error       { return nil }
func (f *fakeKernelHost) CloseStream()                                              {}
func (f *fakeKernelHost) SetMemberID(ctx context.Context, _ string) context.Context { return ctx }
func (f *fakeKernelHost) Lifecycle() string                                         { return "temporary" }
func (f *fakeKernelHost) CancelRecoveryTasks()                                      {}
func (f *fakeKernelHost) ShutdownAllHandles(_ context.Context)                      {}
func (f *fakeKernelHost) SpawnedHandleNames() []string                              { return nil }
func (f *fakeKernelHost) FirstIterGate() types.FirstIterGateAccessor                { return nil }
func (f *fakeKernelHost) StreamController() types.StreamControllerAccessor {
	return f.streamControllerAccessor
}
func (f *fakeKernelHost) PublishTeamEvent(_ context.Context, _ string, _ map[string]any) error {
	return nil
}

// PendingUserQuery M-30: KernelHost 接口新增方法的测试 stub
func (f *fakeKernelHost) PendingUserQuery() string { return "" }

// DispatcherBlueprint M-02: 未显式设置时按 role/memberName 构造默认 blueprint
func (f *fakeKernelHost) DispatcherBlueprint() types.DispatcherBlueprint {
	if f.bp != nil {
		return f.bp
	}
	return &fakeDispatcherBlueprint{role: f.role, memberName: f.memberName}
}

// DispatcherInfra M-02: 未显式设置时返回默认 fake infra
func (f *fakeKernelHost) DispatcherInfra() types.DispatcherInfra {
	if f.inf != nil {
		return f.inf
	}
	return &fakeDispatcherInfra{}
}

// ──────────────────────────── CoordinationKernel 测试 ────────────────────────────

// TestShouldCleanTeam_空成员列表不清理 S-01 回归：
// ListMembers 对"只有 leader 的团队"返回非 nil 空切片，此时必须判定不清理，
// 否则会走 clean_team 删除团队并 DropCurSessionTables（Python 空列表走 recover_team）。
func TestShouldCleanTeam_空成员列表不清理(t *testing.T) {
	shutdown := &database.TeamMember{MemberName: "m1", Status: string(schema.MemberStatusShutdown)}
	running := &database.TeamMember{MemberName: "m2", Status: string(schema.MemberStatusReady)}

	cases := []struct {
		name    string
		members []*database.TeamMember
		want    bool
	}{
		{"nil 切片", nil, false},
		{"非 nil 空切片", make([]*database.TeamMember, 0, 3), false},
		{"全部 SHUTDOWN", []*database.TeamMember{shutdown}, true},
		{"存在非 SHUTDOWN", []*database.TeamMember{shutdown, running}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := shouldCleanTeam(c.members); got != c.want {
				t.Errorf("shouldCleanTeam(%v) = %v, want %v", c.name, got, c.want)
			}
		})
	}
}

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
	k.Setup(schema.TeamRoleLeader)

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

// nilBlueprintHost M-02: 强制 host 返回 nil blueprint 的测试替身。
type nilBlueprintHost struct {
	*fakeKernelHost
}

// DispatcherBlueprint 恒返回 nil（对齐 Python host.blueprint is None）。
func (n *nilBlueprintHost) DispatcherBlueprint() types.DispatcherBlueprint { return nil }

// TestCoordinationKernel_Setup_nilBlueprint不构造 M-02/M-01：
// blueprint 缺失时必须记录错误并跳过构造（Python 抛 RuntimeError）。
func TestCoordinationKernel_Setup_nilBlueprint不构造(t *testing.T) {
	host := &nilBlueprintHost{&fakeKernelHost{role: schema.TeamRoleLeader, memberName: "leader1"}}
	k := NewCoordinationKernel(host)

	k.Setup(schema.TeamRoleLeader)

	if k.EventBus() != nil {
		t.Error("blueprint 为 nil 时不应构造 EventBus")
	}
	if k.Dispatcher() != nil {
		t.Error("blueprint 为 nil 时不应构造 Dispatcher")
	}
}

func TestCoordinationKernel_Start(t *testing.T) {
	host := &fakeKernelHost{role: schema.TeamRoleLeader, memberName: "leader1", agentReady: true}
	k := NewCoordinationKernel(host)
	k.Setup(schema.TeamRoleLeader)

	k.Start(context.Background(), "")

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
	k.Start(context.Background(), "")
	if k.IsRunning() {
		t.Error("无 setup 时 start 不应运行")
	}
}

// TestCoordinationKernel_Start启动成员运行时 验证步骤 6.5 对齐 Python kernel.py L167-168：
// 会话绑定后调 harness.Start + streamController.Start（先 runtime 后 controller）。
func TestCoordinationKernel_Start启动成员运行时(t *testing.T) {
	ha := &fakeHarnessAccessor{}
	sca := &fakeStreamControllerAccessor{}
	host := &fakeKernelHost{
		role:                     schema.TeamRoleLeader,
		memberName:               "leader1",
		agentReady:               true,
		harnessAccessor:          ha,
		streamControllerAccessor: sca,
	}
	k := NewCoordinationKernel(host)
	k.Setup(schema.TeamRoleLeader)

	// 传非 nil 会话触发 harness.Start 接线
	k.Start(context.Background(), session.CreateAgentSession("s1", nil, nil))

	if !ha.startCalled {
		t.Error("步骤 6.5：harness.Start 应被调用")
	}
	if !sca.startCalled {
		t.Error("步骤 6.5：streamController.Start 应在 harness.Start 之后被调用")
	}
}

// TestCoordinationKernel_StartharnessStart失败不挂Controller 验证 harness.Start 返回 error 时
// 不继续调 streamController.Start（else if 语义）。
func TestCoordinationKernel_StartharnessStart失败不挂Controller(t *testing.T) {
	ha := &fakeHarnessAccessor{startErr: fmt.Errorf("harness start failed")}
	sca := &fakeStreamControllerAccessor{}
	host := &fakeKernelHost{
		role:                     schema.TeamRoleLeader,
		memberName:               "leader1",
		agentReady:               true,
		harnessAccessor:          ha,
		streamControllerAccessor: sca,
	}
	k := NewCoordinationKernel(host)
	k.Setup(schema.TeamRoleLeader)

	k.Start(context.Background(), session.CreateAgentSession("s1", nil, nil))

	if !ha.startCalled {
		t.Error("harness.Start 应被调用")
	}
	if sca.startCalled {
		t.Error("harness.Start 失败时 streamController.Start 不应被调用")
	}
}

func TestCoordinationKernel_Pause(t *testing.T) {
	host := &fakeKernelHost{role: schema.TeamRoleLeader, memberName: "leader1", agentReady: true}
	k := NewCoordinationKernel(host)
	k.Setup(schema.TeamRoleLeader)
	k.Start(context.Background(), "")

	k.Pause(context.Background())

	if k.LifecycleState() != kernelStatePaused {
		t.Errorf("pause 后状态应为 paused，实际 %q", k.LifecycleState())
	}
}

func TestCoordinationKernel_Pause幂等(t *testing.T) {
	host := &fakeKernelHost{role: schema.TeamRoleLeader, memberName: "leader1"}
	k := NewCoordinationKernel(host)

	// idle 态 pause 不应 panic
	k.Pause(context.Background())
	if k.LifecycleState() != kernelStateIdle {
		t.Errorf("idle 态 pause 应保持 idle，实际 %q", k.LifecycleState())
	}
}

func TestCoordinationKernel_Stop(t *testing.T) {
	host := &fakeKernelHost{role: schema.TeamRoleLeader, memberName: "leader1", agentReady: true}
	k := NewCoordinationKernel(host)
	k.Setup(schema.TeamRoleLeader)
	k.Start(context.Background(), "")

	k.Stop(context.Background())

	if k.LifecycleState() != kernelStateStopped {
		t.Errorf("stop 后状态应为 stopped，实际 %q", k.LifecycleState())
	}
}

func TestCoordinationKernel_Stop幂等(t *testing.T) {
	host := &fakeKernelHost{role: schema.TeamRoleLeader, memberName: "leader1"}
	k := NewCoordinationKernel(host)

	// idle 态 stop 不应 panic
	k.Stop(context.Background())
	if k.LifecycleState() != kernelStateIdle {
		t.Errorf("idle 态 stop 应保持 idle，实际 %q", k.LifecycleState())
	}

	// stopped 态再次 stop
	k.lifecycleState = kernelStateStopped
	k.Stop(context.Background())
	if k.LifecycleState() != kernelStateStopped {
		t.Errorf("stopped 态 stop 应保持 stopped，实际 %q", k.LifecycleState())
	}
}

func TestCoordinationKernel_EnqueueUserInput(t *testing.T) {
	host := &fakeKernelHost{role: schema.TeamRoleLeader, memberName: "leader1", agentReady: true}
	k := NewCoordinationKernel(host)
	k.Setup(schema.TeamRoleLeader)

	var mu sync.Mutex
	var receivedEvent types.CoordinationEvent
	k.eventBus.Start(context.Background(), func(ctx context.Context, event types.CoordinationEvent) {
		mu.Lock()
		receivedEvent = event
		mu.Unlock()
	})

	k.EnqueueUserInput("hello team")
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	ev := receivedEvent
	mu.Unlock()
	if ev.Inner == nil || ev.Inner.EventType != types.InnerEventTypeUserInput {
		t.Error("EnqueueUserInput 应产生 USER_INPUT 事件")
	}
}

func TestCoordinationKernel_WakeMailboxIfInterruptCleared(t *testing.T) {
	host := &fakeKernelHost{role: schema.TeamRoleTeammate, memberName: "worker1", agentReady: true, pendingInt: false}
	k := NewCoordinationKernel(host)
	k.Setup(schema.TeamRoleTeammate)

	var mu sync.Mutex
	var receivedEvent types.CoordinationEvent
	k.eventBus.Start(context.Background(), func(ctx context.Context, event types.CoordinationEvent) {
		mu.Lock()
		receivedEvent = event
		mu.Unlock()
	})

	k.WakeMailboxIfInterruptCleared(context.Background())
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	ev := receivedEvent
	mu.Unlock()
	if ev.Inner == nil || ev.Inner.EventType != types.InnerEventTypePollMailbox {
		t.Error("WakeMailboxIfInterruptCleared 应产生 POLL_MAILBOX 事件")
	}
}

func TestCoordinationKernel_WakeMailbox_Leader跳过(t *testing.T) {
	host := &fakeKernelHost{role: schema.TeamRoleLeader, memberName: "leader1", agentReady: true, pendingInt: false}
	k := NewCoordinationKernel(host)
	k.Setup(schema.TeamRoleLeader)

	var mu sync.Mutex
	var received bool
	k.eventBus.Start(context.Background(), func(ctx context.Context, event types.CoordinationEvent) {
		mu.Lock()
		received = true
		mu.Unlock()
	})

	k.WakeMailboxIfInterruptCleared(context.Background())

	mu.Lock()
	r := received
	mu.Unlock()
	if r {
		t.Error("Leader 不应触发 WakeMailboxIfInterruptCleared")
	}
}

func TestCoordinationKernel_WakeMailbox_有中断跳过(t *testing.T) {
	host := &fakeKernelHost{role: schema.TeamRoleTeammate, memberName: "worker1", agentReady: true, pendingInt: true}
	k := NewCoordinationKernel(host)
	k.Setup(schema.TeamRoleTeammate)

	var mu sync.Mutex
	var received bool
	k.eventBus.Start(context.Background(), func(ctx context.Context, event types.CoordinationEvent) {
		mu.Lock()
		received = true
		mu.Unlock()
	})

	k.WakeMailboxIfInterruptCleared(context.Background())

	mu.Lock()
	r := received
	mu.Unlock()
	if r {
		t.Error("有 pending interrupt 时不应触发 WakeMailboxIfInterruptCleared")
	}
}

// TestWithMailboxPollInterval 验证 WithMailboxPollInterval 选项。
func TestWithMailboxPollInterval(t *testing.T) {
	opt := WithMailboxPollInterval(2.5)
	cfg := &kernelOptions{}
	opt(cfg)
	if cfg.mailboxPollInterval != 2.5 {
		t.Errorf("mailboxPollInterval = %v, want 2.5", cfg.mailboxPollInterval)
	}
}

// TestWithTaskPollInterval 验证 WithTaskPollInterval 选项。
func TestWithTaskPollInterval(t *testing.T) {
	opt := WithTaskPollInterval(3.0)
	cfg := &kernelOptions{}
	opt(cfg)
	if cfg.taskPollInterval != 3.0 {
		t.Errorf("taskPollInterval = %v, want 3.0", cfg.taskPollInterval)
	}
}

func TestCoordinationKernel_生命周期转换(t *testing.T) {
	host := &fakeKernelHost{role: schema.TeamRoleLeader, memberName: "leader1", agentReady: true}
	k := NewCoordinationKernel(host)
	k.Setup(schema.TeamRoleLeader)

	// idle → running 启动后进入运行态
	k.Start(context.Background(), "")
	if k.LifecycleState() != kernelStateRunning {
		t.Errorf("期望 running，实际 %q", k.LifecycleState())
	}

	// running → paused 运行态可暂停
	k.Pause(context.Background())
	if k.LifecycleState() != kernelStatePaused {
		t.Errorf("期望 paused，实际 %q", k.LifecycleState())
	}

	// paused → stopped 暂停态可停止
	k.Stop(context.Background())
	if k.LifecycleState() != kernelStateStopped {
		t.Errorf("期望 stopped，实际 %q", k.LifecycleState())
	}

	// stopped → stop again (幂等)
	k.Stop(context.Background())
	if k.LifecycleState() != kernelStateStopped {
		t.Errorf("stop 幂等应保持 stopped，实际 %q", k.LifecycleState())
	}
}

// ──────────────────────────── SubscribedTopics 测试 ────────────────────────────

// TestCoordinationKernel_SubscribedTopics 测试初始订阅主题为空
func TestCoordinationKernel_SubscribedTopics(t *testing.T) {
	host := &fakeKernelHost{role: schema.TeamRoleLeader, memberName: "leader1"}
	k := NewCoordinationKernel(host)

	topics := k.SubscribedTopics()
	if topics != nil {
		t.Errorf("初始订阅主题应为 nil，实际 %v", topics)
	}
}

// TestCoordinationKernel_AddSubscribedTopic 测试添加订阅主题
func TestCoordinationKernel_AddSubscribedTopic(t *testing.T) {
	host := &fakeKernelHost{role: schema.TeamRoleLeader, memberName: "leader1"}
	k := NewCoordinationKernel(host)

	k.AddSubscribedTopic("topic-1")
	k.AddSubscribedTopic("topic-2")

	topics := k.SubscribedTopics()
	if len(topics) != 2 {
		t.Fatalf("应有 2 个订阅主题，实际 %d", len(topics))
	}
	if topics[0] != "topic-1" || topics[1] != "topic-2" {
		t.Errorf("订阅主题内容不匹配，实际 %v", topics)
	}
}

// TestCoordinationKernel_ClearSubscribedTopics 测试清空订阅主题
func TestCoordinationKernel_ClearSubscribedTopics(t *testing.T) {
	host := &fakeKernelHost{role: schema.TeamRoleLeader, memberName: "leader1"}
	k := NewCoordinationKernel(host)

	k.AddSubscribedTopic("topic-1")
	k.AddSubscribedTopic("topic-2")
	k.ClearSubscribedTopics()

	topics := k.SubscribedTopics()
	if topics != nil {
		t.Errorf("清空后订阅主题应为 nil，实际 %v", topics)
	}
}

// TestCoordinationKernel_AddSubscribedTopic后追加 测试追加不影响已有主题
func TestCoordinationKernel_AddSubscribedTopic后追加(t *testing.T) {
	host := &fakeKernelHost{role: schema.TeamRoleLeader, memberName: "leader1"}
	k := NewCoordinationKernel(host)

	k.AddSubscribedTopic("topic-a")
	before := k.SubscribedTopics()
	k.AddSubscribedTopic("topic-b")
	after := k.SubscribedTopics()

	if len(before) != 1 || len(after) != 2 {
		t.Errorf("追加后应从 1 变为 2，实际 %d → %d", len(before), len(after))
	}
}

// fakeSessionController T-16 测试用 SessionController——持有真实 AgentTeamSession
// M-29: TeamSession 返回类型收窄到 *session.AgentTeamSession
type fakeSessionController struct {
	teamSession *session.AgentTeamSession
}

func (f *fakeSessionController) BindSession(_ context.Context, sess any) (context.Context, error) {
	if ts, ok := sess.(*session.AgentTeamSession); ok {
		f.teamSession = ts
	}
	return context.Background(), nil
}
func (f *fakeSessionController) ReleaseSession(_ context.Context) error { return nil }
func (f *fakeSessionController) TeamSession() *session.AgentTeamSession { return f.teamSession }

// TestCoordinationKernel_PersistTeamLifecycle真实写入 T-16 回归测试。
// 原匿名断言 GetState() map[string]any 与 AgentTeamSession 真实签名不匹配
// 导致恒 false，生命周期从未持久化。修复后应通过 SessionFacade 真实写入。
func TestCoordinationKernel_PersistTeamLifecycle真实写入(t *testing.T) {
	teamSession := session.NewAgentTeamSession()
	host := &fakeKernelHost{role: schema.TeamRoleLeader}
	hostSessionCtrl := &fakeSessionController{teamSession: teamSession}
	// 覆盖 fakeKernelHost 的 SessionController 方法
	// （fakeKernelHost 是值方法集合，此处用包装方式注入）
	kernel := &CoordinationKernel{host: &kernelHostWithSession{
		KernelHost:  host,
		sessionCtrl: hostSessionCtrl,
	}}

	kernel.persistTeamLifecycle(context.Background(), "paused")

	// 验证写入：teams 命名空间下 team 桶 lifecycle == "paused"
	if err := verifyLifecyclePersisted(teamSession, "test-team", "paused"); err != nil {
		t.Fatal(err)
	}
}

// TestCoordinationKernel_PersistTeamLifecycle非Facade静默跳过 非 SessionFacade 会话静默跳过不 panic
// M-29: TeamSession 收窄到具体类型后，"非 Facade 会话"以 nil 表示（对齐 Python None 存储）
func TestCoordinationKernel_PersistTeamLifecycle非Facade静默跳过(t *testing.T) {
	host := &fakeKernelHost{role: schema.TeamRoleLeader}
	kernel := &CoordinationKernel{host: &kernelHostWithSession{
		KernelHost:  host,
		sessionCtrl: &fakeSessionController{teamSession: nil},
	}}

	// 不应 panic
	kernel.persistTeamLifecycle(context.Background(), "paused")
}

// kernelHostWithSession 包装 fakeKernelHost 注入 SessionController
type kernelHostWithSession struct {
	types.KernelHost
	sessionCtrl types.SessionController
}

func (k *kernelHostWithSession) SessionController() types.SessionController { return k.sessionCtrl }

// verifyLifecyclePersisted 校验 AgentTeamSession 的 teams 命名空间中 lifecycle 字段
func verifyLifecyclePersisted(sess *session.AgentTeamSession, teamName, want string) error {
	stateRaw, err := sess.GetState(state.StringKey("teams"))
	if err != nil {
		return fmt.Errorf("GetState(teams) 失败: %w", err)
	}
	teamsMap, ok := stateRaw.(map[string]any)
	if !ok {
		return fmt.Errorf("teams 命名空间不存在或类型错误: %T", stateRaw)
	}
	bucket, ok := teamsMap[teamName].(map[string]any)
	if !ok {
		return fmt.Errorf("team 桶 %q 不存在", teamName)
	}
	got, _ := bucket["lifecycle"].(string)
	if got != want {
		return fmt.Errorf("lifecycle = %q, want %q", got, want)
	}
	return nil
}
