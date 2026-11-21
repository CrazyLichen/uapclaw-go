//go:build integration

// 25.4 任务板语义集成用例（I-3 前半回收 + 七事件链路）。
// 真实跨子系统：tools（TeamBackend/TeamTaskManager/UpdateTaskTool）→
// schema/events → coordination（EventDispatcher/TaskBoardHandler）→ database。
// 工具/管理器真实写库发事件，事件经真实 EventDispatcher 粗筛后由基类
// handler steer，投递由 fake host 捕获断言。mock 只在 host/poll 边界。
package agentteams_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/agent/coordination"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/agent/coordination/types"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/fsm"
	reliabilitycore "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/rails/reliability/core"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/messager"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema/events"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/tools/database"

	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// TaskBoardReassignSuite reassign 全链与七事件集成套件。
type TaskBoardReassignSuite struct {
	isuite.AgentSuite
	db *database.InMemoryTeamDatabase
}

// TestTaskBoardReassignSuite 入口。
func TestTaskBoardReassignSuite(t *testing.T) {
	suite.Run(t, new(TaskBoardReassignSuite))
}

// SetupSuite 建团队行 + 成员（leader1 / alice / bob / reviewer1）。
func (s *TaskBoardReassignSuite) SetupSuite() {
	ctx := context.Background()
	db := database.NewInMemoryTeamDatabase()
	s.db = db
	s.Require().True(db.Team().CreateTeam(ctx, "alpha", "Alpha", "leader1", "", "", "autonomous", false))
	for _, m := range []struct{ name, role string }{
		{"leader1", "leader"}, {"alice", "teammate"}, {"bob", "teammate"}, {"reviewer1", "teammate"},
	} {
		s.Require().True(db.Member().CreateMember(ctx, m.name, "alpha", m.name, "{}", "ready", m.role, "", "", "build_mode", "", ""))
	}
	s.Require().NoError(db.Initialize(ctx))
}

// ──────────────────────────── 测试假实现 ────────────────────────────

// captureMessager 捕获发布事件的消息总线假实现（仅边界 mock）。
type captureMessager struct {
	published []*events.EventMessage
}

func (m *captureMessager) Start(context.Context) error  { return nil }
func (m *captureMessager) Stop(context.Context) error   { return nil }
func (m *captureMessager) Publish(_ context.Context, _ string, msg *events.EventMessage) error {
	m.published = append(m.published, msg)
	return nil
}
func (m *captureMessager) Subscribe(context.Context, string, messager.MessagerHandler) error { return nil }
func (m *captureMessager) Unsubscribe(context.Context, string) error                         { return nil }
func (m *captureMessager) Send(context.Context, string, *events.EventMessage) error          { return nil }
func (m *captureMessager) RegisterDirectMessageHandler(context.Context, messager.MessagerHandler) error {
	return nil
}
func (m *captureMessager) UnregisterDirectMessageHandler(context.Context) error { return nil }

// itestHost handler 侧 host 假实现（捕获投递）。
type itestHost struct {
	deliveries []string
}

func (h *itestHost) IsAgentReady() bool                  { return true }
func (h *itestHost) IsAgentRunning() bool                { return false }
func (h *itestHost) HasInFlightRound() bool              { return false }
func (h *itestHost) HasPendingInterrupt() bool           { return false }
func (h *itestHost) CancelAgent(context.Context) error   { return nil }
func (h *itestHost) AutoStartMember(context.Context, string) bool { return true }
func (h *itestHost) ResumeInterrupt(context.Context, *interaction.InteractiveInput) error { return nil }
func (h *itestHost) AnnounceTaskRevisions(context.Context) error { return nil }
func (h *itestHost) DeliverInput(_ context.Context, content string, _ bool, _ *agentinterfaces.RunContext) error {
	h.deliveries = append(h.deliveries, content)
	return nil
}
func (h *itestHost) ShutdownSelf(context.Context) error                     { return nil }
func (h *itestHost) ConcludeCompletedRound(context.Context, int, int) error { return nil }

// itestBP blueprint 假实现。
type itestBP struct {
	role   schema.TeamRole
	member string
}

func (b *itestBP) Role() schema.TeamRole                                 { return b.role }
func (b *itestBP) DispatcherMemberName() string                          { return b.member }
func (b *itestBP) DispatcherReliability() *reliabilitycore.ReliabilityConfig { return nil }
func (b *itestBP) DispatcherSpec() *schema.TeamAgentSpec                 { return nil }

// itestInfra infra 假实现。
type itestInfra struct {
	tm *tools.TeamTaskManager
	be *tools.TeamBackend
}

func (i *itestInfra) TaskManager() *tools.TeamTaskManager      { return i.tm }
func (i *itestInfra) MessageManager() *tools.TeamMessageManager { return nil }
func (i *itestInfra) TeamBackend() *tools.TeamBackend          { return i.be }
func (i *itestInfra) Messager() messager.Messager              { return nil }

// itestPoll poll 控制器假实现。
type itestPoll struct{}

func (p *itestPoll) PausePolls()                   {}
func (p *itestPoll) ResumePolls(context.Context)   {}

// newMemberSide 按成员视角装配 dispatcher（真实 EventDispatcher + 基类 handler）。
func (s *TaskBoardReassignSuite) newMemberSide(member string, role schema.TeamRole,
	tm *tools.TeamTaskManager, be *tools.TeamBackend) (*coordination.EventDispatcher, *itestHost) {
	host := &itestHost{}
	bp := &itestBP{role: role, member: member}
	infra := &itestInfra{tm: tm, be: be}
	d := coordination.NewEventDispatcher(host, bp, infra, &itestPoll{})
	return d, host
}

// memberTaskManager 成员视角的任务管理器。
func (s *TaskBoardReassignSuite) memberTaskManager(member string, msgr messager.Messager) *tools.TeamTaskManager {
	return tools.NewTeamTaskManager(s.db, "alpha", member, msgr, nil, "", "", "leader1")
}

// ──────────────────────────── 用例 ────────────────────────────

// strPtr 字符串指针助手。
func (s *TaskBoardReassignSuite) strPtr(v string) *string { return &v }

// TestReassign_I3前半_全链 leader 经 UpdateTaskTool 原子改派：状态原位保留、
// 恰发 REVOKED(前任)+CLAIMED(新任) 两定向事件、无 RELEASED；前任/新任各自的
// dispatcher 把定向 steer 投递到成员。
func (s *TaskBoardReassignSuite) TestReassign_I3前半_全链() {
	ctx := context.Background()
	msgr := &captureMessager{}
	leaderBE := tools.NewTeamBackend("alpha", "leader1", true, s.db, msgr, tools.WithLeaderMemberName("leader1"))
	s.Require().True(s.db.Task().CreateTask(ctx, &database.TeamTaskBase{
		TaskID: "i3", TeamName: "alpha", Title: "改派票", Content: "内容",
		Status: fsm.TaskStatusInProgress, Assignee: s.strPtr("alice"),
	}))

	tool := tools.NewUpdateTaskToolInstance(leaderBE, "cn")
	out, err := tool.Invoke(ctx, map[string]any{"task_id": "i3", "assignee": "bob"})
	s.Require().NoError(err)
	s.Require().True(out["success"] == true, "改派应成功: %v", out)

	// 状态原位保留（原子换手，不弹回 PENDING）。
	row, gerr := s.db.Task().GetTask(ctx, "i3")
	s.Require().NoError(gerr)
	s.Require().Equal(fsm.TaskStatusInProgress, row.Status, "状态应原位保留")
	s.Require().NotNil(row.Assignee)
	s.Require().Equal("bob", *row.Assignee)

	// 事件序：REVOKED(alice) → CLAIMED(bob)，且无 RELEASED。
	var seq []string
	var revokedMember, claimedMember string
	for _, em := range msgr.published {
		seq = append(seq, em.EventType)
		if em.EventType == events.TeamEventTaskRevoked {
			revokedMember, _ = em.Payload["member_name"].(string)
		}
		if em.EventType == events.TeamEventTaskClaimed {
			claimedMember, _ = em.Payload["member_name"].(string)
		}
	}
	s.Require().Equal([]string{"task_revoked", "task_claimed"}, seq, "改派恰好发两个定向事件")
	s.Require().Equal("alice", revokedMember)
	s.Require().Equal("bob", claimedMember)

	// 前任 alice：dispatcher steer 其离开陌生任务。
	tmAlice := s.memberTaskManager("alice", nil)
	beAlice := tools.NewTeamBackend("alpha", "alice", false, s.db, nil)
	dAlice, hostAlice := s.newMemberSide("alice", schema.TeamRoleTeammate, tmAlice, beAlice)
	dAlice.Dispatch(ctx, types.CoordinationEvent{Transport: msgr.published[0]})
	s.Require().Len(hostAlice.deliveries, 1, "前任应被 steer")
	s.Require().Contains(hostAlice.deliveries[0], "task-revoked")

	// 新任 bob：收到 task-claimed 定向分配内容。
	tmBob := s.memberTaskManager("bob", nil)
	beBob := tools.NewTeamBackend("alpha", "bob", false, s.db, nil)
	dBob, hostBob := s.newMemberSide("bob", schema.TeamRoleTeammate, tmBob, beBob)
	dBob.Dispatch(ctx, types.CoordinationEvent{Transport: msgr.published[1]})
	s.Require().Len(hostBob.deliveries, 1, "新任应收到定向分配")
	s.Require().Contains(hostBob.deliveries[0], "i3")
}

// TestReassign_I3后半_oneActiveClaim 持在办票成员 claim 第二张被拒（工具边界）。
func (s *TaskBoardReassignSuite) TestReassign_I3后半_oneActiveClaim() {
	ctx := context.Background()
	s.Require().True(s.db.Task().CreateTask(ctx, &database.TeamTaskBase{
		TaskID: "held", TeamName: "alpha", Title: "在办", Content: "C",
		Status: fsm.TaskStatusInProgress, Assignee: s.strPtr("alice"),
	}))
	s.Require().True(s.db.Task().CreateTask(ctx, &database.TeamTaskBase{
		TaskID: "open", TeamName: "alpha", Title: "可认领", Content: "C", Status: fsm.TaskStatusPending,
	}))

	aliceBE := tools.NewTeamBackend("alpha", "alice", false, s.db, nil)
	tool := tools.NewClaimTaskToolInstance(aliceBE.TaskManager(), "cn")
	out, err := tool.Invoke(ctx, map[string]any{"task_id": "open", "status": "claimed"})
	s.Require().NoError(err)
	s.Require().False(out["success"] == true, "已持在办票再认领应被拒")
	msg, _ := out["error"].(string)
	s.Require().Contains(msg, "already have an active task")
}

// TestVerifyGate_基类Steer verify 三闸经基类 handler：reviewer 收 review-request、
// 作者收 revision / verified；非目标成员落看板（空板静默）。
func (s *TaskBoardReassignSuite) TestVerifyGate_基类Steer() {
	ctx := context.Background()
	msgr := &captureMessager{}
	aliceTM := s.memberTaskManager("alice", msgr)
	// 作者 alice 完成带 reviewer 的任务 → SUBMITTED_FOR_REVIEW（真实写路径）。
	s.Require().True(s.db.Task().CreateTask(ctx, &database.TeamTaskBase{
		TaskID: "vg1", TeamName: "alpha", Title: "验收票", Content: "C",
		Status: fsm.TaskStatusInProgress, Assignee: s.strPtr("alice"),
		Reviewer: s.strPtr(`[{"type":"verifier","reviewer_id":"reviewer1"}]`),
	}))
	result, cerr := aliceTM.Complete(ctx, "vg1")
	s.Require().NoError(cerr)
	s.Require().True(result.OK, "带 reviewer 的完成应进评审门")

	// 找到 SUBMITTED_FOR_REVIEW 事件（member_name=作者、reviewer=reviewer1）。
	var submitted *events.EventMessage
	for _, em := range msgr.published {
		if em.EventType == events.TeamEventTaskSubmittedForReview {
			submitted = em
		}
	}
	s.Require().NotNil(submitted)

	// reviewer1 视角 dispatcher → steer review-request。
	tmR := s.memberTaskManager("reviewer1", nil)
	beR := tools.NewTeamBackend("alpha", "reviewer1", false, s.db, nil)
	dR, hostR := s.newMemberSide("reviewer1", schema.TeamRoleTeammate, tmR, beR)
	dR.Dispatch(ctx, types.CoordinationEvent{Transport: submitted})
	s.Require().Len(hostR.deliveries, 1, "reviewer 应被 steer")
	s.Require().Contains(hostR.deliveries[0], "task-review-request")

	// 打回：author 收 task-revision。
	revoked, _ := s.db.Task().GetTask(ctx, "vg1")
	_ = revoked
	tmA := s.memberTaskManager("alice", nil)
	beA := tools.NewTeamBackend("alpha", "alice", false, s.db, nil)
	dA, hostA := s.newMemberSide("alice", schema.TeamRoleTeammate, tmA, beA)
	dA.Dispatch(ctx, types.CoordinationEvent{Transport: &events.EventMessage{
		EventType: events.TeamEventTaskRevisionRequested,
		Payload:   map[string]any{"member_name": "alice", "task_id": "vg1", "feedback": "补测试"},
	}})
	s.Require().Len(hostA.deliveries, 1, "作者应收返工反馈")
	s.Require().Contains(hostA.deliveries[0], "task-revision")

	// 验收通过：作者收 task-verified（one-active 解锁回看板）。
	dA2, hostA2 := s.newMemberSide("alice", schema.TeamRoleTeammate, tmA, beA)
	dA2.Dispatch(ctx, types.CoordinationEvent{Transport: &events.EventMessage{
		EventType: events.TeamEventTaskVerified,
		Payload:   map[string]any{"member_name": "alice", "task_id": "vg1"},
	}})
	s.Require().Len(hostA2.deliveries, 1, "作者应收验收通过解锁")
	s.Require().Contains(hostA2.deliveries[0], "task-verified")
}

// TestInitialPoll_F69 名下 pending 任务经 F_69 启动普查送达（宕机期间被派活的
// 成员唯一的得知途径）。
func (s *TaskBoardReassignSuite) TestInitialPoll_F69() {
	ctx := context.Background()
	tmBob := s.memberTaskManager("bob", nil)
	beBob := tools.NewTeamBackend("alpha", "bob", false, s.db, nil)
	s.Require().True(s.db.Task().CreateTask(ctx, &database.TeamTaskBase{
		TaskID: "f69", TeamName: "alpha", Title: "启动前被指派", Content: "C",
		Status: fsm.TaskStatusPending, Assignee: s.strPtr("bob"),
	}))
	d, host := s.newMemberSide("bob", schema.TeamRoleTeammate, tmBob, beBob)
	d.Dispatch(ctx, types.CoordinationEvent{Inner: &types.InnerEventMessage{
		EventType: types.InnerEventTypeInitialPollTask,
	}})
	s.Require().Len(host.deliveries, 1, "首启普查应投递名下任务")
	s.Require().Contains(host.deliveries[0], "f69")
	s.Require().Contains(host.deliveries[0], "task-board")
}

// TestReleased_唤醒IdleTeammate 任务 reset 回 PENDING 发 RELEASED，idle teammate
// 经看板巡视拿到可认领票。
func (s *TaskBoardReassignSuite) TestReleased_唤醒IdleTeammate() {
	ctx := context.Background()
	msgr := &captureMessager{}
	leaderTM := s.memberTaskManager("leader1", msgr)
	s.Require().True(s.db.Task().CreateTask(ctx, &database.TeamTaskBase{
		TaskID: "rel1", TeamName: "alpha", Title: "释放票", Content: "C",
		Status: fsm.TaskStatusInProgress, Assignee: s.strPtr("alice"),
	}))
	result, rerr := leaderTM.Reset(ctx, "rel1")
	s.Require().NoError(rerr)
	s.Require().True(result.OK)

	var released *events.EventMessage
	for _, em := range msgr.published {
		if em.EventType == events.TeamEventTaskReleased {
			released = em
		}
	}
	s.Require().NotNil(released, "reset 应发布 TASK_RELEASED")

	// teammate 视角：RELEASED 扩大可认领池 → 看板巡视含该票。
	tmBob := s.memberTaskManager("bob", nil)
	beBob := tools.NewTeamBackend("alpha", "bob", false, s.db, nil)
	d, host := s.newMemberSide("bob", schema.TeamRoleTeammate, tmBob, beBob)
	d.Dispatch(ctx, types.CoordinationEvent{Transport: released})
	s.Require().Len(host.deliveries, 1, "RELEASED 应唤醒 idle teammate")
	s.Require().Contains(host.deliveries[0], "rel1")
	s.Require().True(strings.Contains(host.deliveries[0], "task-board"))
}
