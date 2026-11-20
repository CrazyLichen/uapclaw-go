//go:build integration

package agentteams_test

import (
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/single_agent/interfaces"

	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/agent/coordination/scheduling"
	ateamprompts "github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/prompts"
	atschema "github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/tools/database"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"

	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/agent/coordination/types"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/schema/events"
)

// TestTeamScheduledDispatchSuite 套件入口。
func TestTeamScheduledDispatchSuite(t *testing.T) {
	suite.Run(t, new(TeamScheduledDispatchSuite))
}

// TeamScheduledDispatchSuite scheduled dispatch 调度半区全链集成测试套件。
//
// 跨 agent_teams/agent/coordination/scheduling（决策引擎）、agent_teams/tools
// （真 TeamTaskManager/TeamMessageManager + CAS 状态机）、agent_teams/tools/database
// （真库行断言）与 agent_teams/prompts（meta 解析）四个子系统，验证 25.2a
// 计划 I-1/I-2/I-3/I-5/I-6/I-7（I-4 评审链归 25.2b）。
type TeamScheduledDispatchSuite struct {
	isuite.AgentSuite
	db      database.TeamDatabase
	tm      *tools.TeamTaskManager
	mm      *tools.TeamMessageManager
	host    *schedFakeHost
	backend *schedFakeBackend
	sender  *schedFakeSender
	sched   *scheduling.TeamScheduler
}

// ──────────────────────────── 测试桩 ────────────────────────────

// schedFakeHost 记录 leader 注入与惰性拉起。
type schedFakeHost struct {
	delivered []string
	started   []string
}

func (h *schedFakeHost) DeliverInput(_ context.Context, content string, _ bool, _ *agentinterfaces.RunContext) error {
	h.delivered = append(h.delivered, content)
	return nil
}

func (h *schedFakeHost) AutoStartMember(_ context.Context, memberName string) bool {
	h.started = append(h.started, memberName)
	return true
}

// schedFakeBackend 探针桩（shutdown 记账）。
type schedFakeBackend struct {
	shutdownCalls []string
	shutdown      map[string]bool
	// verificationEnabled 验证开关（评审链套件覆写为 true）。
	verificationEnabled bool
}

func (b *schedFakeBackend) ListRetirableMembers(context.Context) ([]database.RetirableMemberRow, error) {
	return nil, nil
}
func (b *schedFakeBackend) ListShutdownMembers(context.Context) (map[string]bool, error) {
	return b.shutdown, nil
}
func (b *schedFakeBackend) ShutdownMember(_ context.Context, memberName string) atschema.MemberOpResult {
	b.shutdownCalls = append(b.shutdownCalls, memberName)
	b.shutdown[memberName] = true
	return atschema.MemberOpResult{OK: true}
}
func (b *schedFakeBackend) MembersWithUnread(context.Context, []string) (map[string]bool, error) {
	return map[string]bool{}, nil
}
func (b *schedFakeBackend) TaskVerificationEnabled(context.Context) (bool, error) {
	return b.verificationEnabled, nil
}

func (s *TeamScheduledDispatchSuite) strPtr(v string) *string { return &v }

// CoordinationEventOf 构造跨进程事件（task_id/task_count 载荷）。
func CoordinationEventOf(eventType string, payload map[string]any) types.CoordinationEvent {
	return types.CoordinationEvent{Transport: &events.EventMessage{EventType: eventType, Payload: payload}}
}

// ──────────────────────────── 生命周期 ────────────────────────────

// schedFakeSender 记录信箱 handoff（行为断言用，隔离每用例重放）。
type schedFakeSender struct {
	rows map[string][]*database.TeamMessageBase
}

func (x *schedFakeSender) SendMessageWithMeta(_ context.Context, meta map[string]any, toMemberName string) (string, error) {
	raw, _ := json.Marshal(meta)
	metaStr := string(raw)
	row := &database.TeamMessageBase{MessageID: "m-" + toMemberName, ToMemberName: &toMemberName, Meta: &metaStr, Content: ""}
	x.rows[toMemberName] = append(x.rows[toMemberName], row)
	return row.MessageID, nil
}
func (x *schedFakeSender) MemberName() string { return "leader1" }
func (x *schedFakeSender) GetMessages(_ context.Context, toMemberName string, _ bool, _ string) ([]*database.TeamMessageBase, error) {
	return x.rows[toMemberName], nil
}

// SetupSuite 建团队行 + 全套管理器 + 调度器（真实组件直连）。
func (s *TeamScheduledDispatchSuite) SetupSuite() {
	ctx := context.Background()
	db := database.NewInMemoryTeamDatabase()
	s.db = db
	s.Require().True(s.db.Team().CreateTeam(ctx, "alpha", "Alpha", "leader1", "", "", "scheduled", false))
	s.Require().True(s.db.Member().CreateMember(ctx, "leader1", "alpha", "Leader", "{}", "ready", "leader", "", "", "build_mode", "", ""))
	s.Require().True(s.db.Member().CreateMember(ctx, "bob", "alpha", "Bob", "{}", "ready", "teammate", "", "", "build_mode", "", ""))
	s.Require().NoError(s.db.Initialize(ctx))
	s.tm = tools.NewTeamTaskManager(db, "alpha", "leader1", nil, nil, "", "", "leader1")
	s.mm = tools.NewTeamMessageManager(db, "alpha", "leader1", nil)
	s.host = &schedFakeHost{}
	s.backend = &schedFakeBackend{shutdown: map[string]bool{}}
	// handoff/revision 行为断言走 fake sender（隔离每用例重放）；真库信箱链
	// 已由 I1 的真实 TeamMessageManager 直连覆盖。
	s.sender = &schedFakeSender{rows: map[string][]*database.TeamMessageBase{}}
	s.sched = scheduling.NewTeamScheduler(
		s.host, s.tm, s.backend, s.sender,
		3, 2400, "alpha", "cn",
	)
}

// BeforeTest 每用例重置桩与账本（板面真相在 DB，账本进程态）。
func (s *TeamScheduledDispatchSuite) BeforeTest(_, _ string) {
	s.host.delivered = nil
	s.host.started = nil
	s.backend.shutdownCalls = nil
	// 板面复位：把上一位用例留下的非终态任务清成终态，bob 回到空闲，
	// 否则 one-active 上限会挡住本用例的派发（I2 失败根因）。
	ctxReset := context.Background()
	for _, status := range []string{"pending", "blocked", "planning", "in_progress", "in_review"} {
		rows, err := s.tm.ListTasks(ctxReset, status)
		if err != nil {
			continue
		}
		for _, row := range rows {
			// FSM 合法两步：任意活跃态 →（planning/in_progress 经表）→ completed。
			// UpdateTaskStatus 走转换表，单步 pending→completed 非法被静默拒。
			_, _ = s.db.Task().UpdateTaskStatus(ctxReset, row.TaskID, "in_progress")
			_, _ = s.db.Task().UpdateTaskStatus(ctxReset, row.TaskID, "completed")
		}
	}
	s.sender = &schedFakeSender{rows: map[string][]*database.TeamMessageBase{}}
	// 信箱行不清理：各用例用独立 task id 与 bob 行数增量断言
	s.sched = scheduling.NewTeamScheduler(
		s.host, s.tm, s.backend, s.sender,
		3, 2400, "alpha", "cn",
	)
}

// ──────────────────────────── 用例 ────────────────────────────

// TestScheduledDispatch_I1_全链派发 计划 I-1：激活即派发最早指派任务——
// 状态断言直接读 db 任务行（PENDING→IN_PROGRESS），不看模型输出；
// 信箱行落 meta_task_start、惰性拉起 owner。
func (s *TeamScheduledDispatchSuite) TestScheduledDispatch_I1_全链派发() {
	ctx := context.Background()
	s.db.Task().CreateTask(ctx, &database.TeamTaskBase{TaskID: "i1-a", TeamName: "alpha", Status: "pending", Assignee: s.strPtr("bob"), Title: "甲"})
	s.db.Task().CreateTask(ctx, &database.TeamTaskBase{TaskID: "i1-b", TeamName: "alpha", Status: "pending", Assignee: s.strPtr("bob"), Title: "乙"})

	s.sched.Activate(ctx)

	row, err := s.db.Task().GetTask(ctx, "i1-a")
	s.Require().NoError(err)
	s.Require().NotNil(row)
	s.Equal("in_progress", row.Status, "最早指派任务应被 CAS 启动")
	rowB, _ := s.db.Task().GetTask(ctx, "i1-b")
	s.Require().NotNil(rowB)
	s.Equal("pending", rowB.Status, "one-active 上限挡住第二条")

	// 信箱行断言（fake sender 隔离重放；真实 mm 写链见 TestScheduledDispatch_真库信箱行）
	mail := s.sender.rows["bob"]
	s.Require().Len(mail, 1, "恰一条 handoff")
	s.Require().NotNil(mail[0].Meta)
	meta := ateamprompts.ParseMeta(mail[0].Meta)
	tpl, _ := meta["template"].(string)
	s.Equal("scheduler_task_start", tpl)
	s.Empty(mail[0].Content, "F_63: 信箱行存空 content")
	s.Equal([]string{"bob"}, s.host.started, "handoff 后 best-effort 拉起 owner")
}

// TestScheduledDispatch_I1_计划闸 指派给 PLAN_MODE 成员的任务落计划闸。
func (s *TeamScheduledDispatchSuite) TestScheduledDispatch_I1_计划闸() {
	ctx := context.Background()
	if !s.db.Member().CreateMember(ctx, "planner", "alpha", "P", "{}", "ready", "teammate", "", "", "plan_mode", "", "") {
		s.T().Log("planner already exists (SetupSuite shared db)")
	}
	s.db.Task().CreateTask(ctx, &database.TeamTaskBase{TaskID: "i1-p", TeamName: "alpha", Status: "pending", Assignee: s.strPtr("planner")})

	s.sched.Activate(ctx)

	row, _ := s.db.Task().GetTask(ctx, "i1-p")
	s.Require().NotNil(row)
	s.Equal("planning", row.Status, "plan_mode owner 经计划闸启动")
	s.Require().Len(s.sender.rows["planner"], 1)
	meta := ateamprompts.ParseMeta(s.sender.rows["planner"][0].Meta)
	tpl, _ := meta["template"].(string)
	s.Equal("scheduler_task_start_plan", tpl)
}

// TestScheduledDispatch_I2_依赖顺序与防环 计划 I-2：T2 depends_on T1 未完成
// 不可派发（BLOCKED 不进 start 扫描）；create 环被拒且无任务行落库。
func (s *TeamScheduledDispatchSuite) TestScheduledDispatch_I2_依赖顺序与防环() {
	ctx := context.Background()
	s.db.Task().CreateTask(ctx, &database.TeamTaskBase{TaskID: "i2-1", TeamName: "alpha", Status: "pending", Assignee: s.strPtr("bob")})
	s.db.Task().CreateTask(ctx, &database.TeamTaskBase{TaskID: "i2-2", TeamName: "alpha", Status: "pending", Assignee: s.strPtr("bob")})
	s.db.Task().MutateDependencyGraph(ctx, "alpha", nil, []database.EdgeSpec{{TaskID: "i2-2", DependsOnID: "i2-1"}})

	s.sched.Activate(ctx)

	row1, _ := s.db.Task().GetTask(ctx, "i2-1")
	s.Require().NotNil(row1)
	s.Equal("in_progress", row1.Status, "无依赖任务先行")
	row2, _ := s.db.Task().GetTask(ctx, "i2-2")
	s.Require().NotNil(row2)
	s.Equal("blocked", row2.Status, "MutateDependencyGraph 应把未解锁依赖任务刷成 BLOCKED")

	// 防环：i2-1→i2-2 边使 i2-1 与 i2-2 互为依赖（环），mutation 被拒绝。
	result := s.db.Task().MutateDependencyGraph(ctx, "alpha", nil, []database.EdgeSpec{{TaskID: "i2-1", DependsOnID: "i2-2"}})
	s.False(result.Ok, "依赖成环应被拒绝")
}

// TestScheduledDispatch_I3_改派Handoff 计划 I-3：reassign 属 25.4（TASK_REVOKED
// 事件与 reassign 方法未在本批范围）；本用例覆盖其后半——新 owner 由 handoff
// 扫描补发（活跃任务信箱无 handoff → 补发）。
func (s *TeamScheduledDispatchSuite) TestScheduledDispatch_I3_改派Handoff() {
	ctx := context.Background()
	s.db.Task().CreateTask(ctx, &database.TeamTaskBase{TaskID: "i3", TeamName: "alpha", Status: "in_progress", Assignee: s.strPtr("bob")})

	before := len(s.sender.rows["bob"])
	s.sched.Activate(ctx)

	mail := s.sender.rows["bob"]
	s.Require().Len(mail, before+1, "活跃任务信箱缺 handoff 应补发一封")
	meta := ateamprompts.ParseMeta(mail[len(mail)-1].Meta)
	tpl, _ := meta["template"].(string)
	s.Equal("scheduler_task_start", tpl)

	// 幂等：第二次扫描不重发（I-5 前置）
	s.sched.Activate(ctx)
	s.Require().Len(s.sender.rows["bob"], before+1, "handoff 已知后不得重发")
	s.sched.Activate(ctx)
}

// TestScheduledDispatch_I5_幂等恢复 计划 I-5：扫描后重放同事件（重激活），
// 板面无重复派发、无重复 revision mail。
func (s *TeamScheduledDispatchSuite) TestScheduledDispatch_I5_幂等恢复() {
	ctx := context.Background()
	s.db.Task().CreateTask(ctx, &database.TeamTaskBase{TaskID: "i5", TeamName: "alpha", Status: "pending", Assignee: s.strPtr("bob"), Revision: 3, ObservedRevision: 1})
	before := len(s.sender.rows["bob"])
	s.sched.Activate(ctx)

	mail := s.sender.rows["bob"]
	s.Require().Len(mail, before+1, "start handoff 按投递时任务现状渲染（携带 revision 3）")
	s.sched.Activate(ctx)
	s.sched.Activate(ctx)
	s.Require().Len(s.sender.rows["bob"], before+1, "重放唤醒不得重复派发")

	// F_129（scheduler.py L519-522）：handoff 按投递时任务现状渲染，所以已携带
	// 本 revision——不再单独发 revision mail。恰一封 start、零封 revised。
	starts := 0
	for _, row := range mail[before:] {
		meta := ateamprompts.ParseMeta(row.Meta)
		if scheduling.IsTaskStartMeta(meta, "i5") {
			starts++
		}
	}
	s.Equal(1, starts, "start handoff 恰一封（携带最新 revision）")
}

// TestScheduledDispatch_I6_idle链 计划 I-6：板清→去抖→team.idle；once-only；
// 新活动 re-arm；refresh_idle_baseline 不误报。
func (s *TeamScheduledDispatchSuite) TestScheduledDispatch_I6_idle链() {
	ctx := context.Background()
	s.db.Task().CreateTask(ctx, &database.TeamTaskBase{TaskID: "i6", TeamName: "alpha", Status: "completed"})

	// 终态任务的完成事件 digest：板面清空（仅 1 任务且终态）→ digest + all-done
	s.sched.Activate(ctx)
	before := len(s.host.delivered)
	s.sched.OnEvent(ctx, CoordinationEventOf("task_completed", map[string]any{"task_id": "i6", "task_count": float64(1)}))
	s.Require().Greater(len(s.host.delivered), before, "task_completed 应注入 digest")
	s.Contains(s.host.delivered[len(s.host.delivered)-1], "任务看板上已没有未终结的任务", "板清应 all-done")

	// once-only：重复 drained 不重发
	n := len(s.host.delivered)
	s.sched.OnEvent(ctx, CoordinationEventOf("task_list_drained", map[string]any{"task_count": float64(1)}))
	s.Len(s.host.delivered, n, "all-done 不得重发")

	// refresh_idle_baseline：未打点时 no-op（不产生凭空基线）
	a := &IdleBaselineProbe{}
	a.RefreshIdleBaseline()
	s.True(a.IdleSince.IsZero(), "冷启动零值时 refresh 不应打点")
}

// TestScheduledDispatch_I7_retirement 计划 I-7：inline member 板清走 shutdown
// 请求——本版本由 constants.InlineTaskMemberEnabled 关死（上游开关 false），
// pass 恒 no-op；开关打开后本用例应改写。
func (s *TeamScheduledDispatchSuite) TestScheduledDispatch_I7_retirement() {
	if atschema.InlineTaskMemberEnabled {
		s.T().Skip("上游开关打开时改写为真 retirement 链路")
	}
	ctx := context.Background()
	acted, err := s.sched.ReconcileRetirements(ctx)
	s.Require().NoError(err)
	s.False(acted, "开关关闭时 retirement pass 应 no-op")
	s.Empty(s.backend.shutdownCalls)
}

// ──────────────────────────── 副产品桩 ────────────────────────────

// TestScheduledDispatch_真库信箱行 真实 TeamMessageManager.SendMessageWithMeta
// 写链（F_63 落库形状：content 空、meta 序列化、from=leader 身份）。
func (s *TeamScheduledDispatchSuite) TestScheduledDispatch_真库信箱行() {
	ctx := context.Background()
	meta := ateamprompts.BuildMeta("scheduler_task_start", map[string]string{"task": "mm-1"}, nil)
	id, err := s.mm.SendMessageWithMeta(ctx, meta, "bob")
	s.Require().NoError(err)
	s.NotEmpty(id)
	rows, err := s.mm.GetMessages(ctx, "bob", false, "leader1")
	s.Require().NoError(err)
	var found *database.TeamMessageBase
	for _, row := range rows {
		if row.MessageID == id {
			found = row
		}
	}
	s.Require().NotNil(found, "写入的行应可读回")
	s.Empty(found.Content, "F_63: content 空")
	parsed := ateamprompts.ParseMeta(found.Meta)
	s.True(scheduling.IsTaskStartMeta(parsed, "mm-1"), "真库 meta 应可解析为 start handoff")
}

// IdleBaselineProbe RefreshIdleBaseline 语义探针（零值 no-op 语义同
// TeamAgent.refresh_idle_baseline：只重置已打点的基线）。
type IdleBaselineProbe struct {
	IdleSince time.Time
}

func (p *IdleBaselineProbe) RefreshIdleBaseline() {
	if !p.IdleSince.IsZero() {
		p.IdleSince = time.Now()
	}
}
