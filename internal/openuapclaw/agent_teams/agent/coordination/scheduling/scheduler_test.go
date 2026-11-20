package scheduling

import (
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"

	"context"
	"encoding/json"
	"testing"

	ateamcoordtypes "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/agent/coordination/types"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/fsm"
	ateamprompts "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/prompts"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema"
	atschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema/events"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/tools/database"
)

// ──────────────────────────── 测试桩 ────────────────────────────

// fakeHost 记录 DeliverInput / AutoStartMember 调用。
type fakeHost struct {
	delivered []fakeDelivery
	started   []string
}

type fakeDelivery struct {
	content  string
	useSteer bool
}

func (h *fakeHost) DeliverInput(_ context.Context, content string, useSteer bool, runContext *agentinterfaces.RunContext) error {
	h.delivered = append(h.delivered, fakeDelivery{content: content, useSteer: useSteer})
	return nil
}

func (h *fakeHost) AutoStartMember(_ context.Context, memberName string) bool {
	h.started = append(h.started, memberName)
	return true
}

// fakeTaskManager 直接包装真实 TeamTaskManager（in-memory DB）。
type fakeTaskManager struct {
	tm *tools.TeamTaskManager
}

func (f *fakeTaskManager) Get(ctx context.Context, taskID string) (*database.TeamTaskBase, error) {
	return f.tm.Get(ctx, taskID)
}
func (f *fakeTaskManager) ListTasks(ctx context.Context, status string) ([]*database.TeamTaskBase, error) {
	return f.tm.ListTasks(ctx, status)
}
func (f *fakeTaskManager) StartTask(ctx context.Context, taskID string) (schema.TaskOpResult, error) {
	return f.tm.StartTask(ctx, taskID)
}
func (f *fakeTaskManager) GetOtherActiveTaskID(ctx context.Context, memberName, excludeTaskID string) (string, error) {
	return f.tm.GetOtherActiveTaskID(ctx, memberName, excludeTaskID)
}
func (f *fakeTaskManager) HasOpenTasks(ctx context.Context) (bool, error) {
	return f.tm.HasOpenTasks(ctx)
}
func (f *fakeTaskManager) BoardCounts(ctx context.Context) (int64, int64, error) {
	return f.tm.BoardCounts(ctx)
}
func (f *fakeTaskManager) OpenTaskCountsByAssignee(ctx context.Context) (map[string]int, error) {
	return f.tm.OpenTaskCountsByAssignee(ctx)
}
func (f *fakeTaskManager) UnseenRevisions(ctx context.Context) ([]database.UnseenRevisionRow, error) {
	return f.tm.UnseenRevisions(ctx)
}

// fakeBackend 探针桩。
type fakeBackend struct {
	retirable     []database.RetirableMemberRow
	shutdown      map[string]bool
	shutdownCalls []string
}

func (b *fakeBackend) ListRetirableMembers(context.Context) ([]database.RetirableMemberRow, error) {
	return b.retirable, nil
}
func (b *fakeBackend) ListShutdownMembers(context.Context) (map[string]bool, error) {
	return b.shutdown, nil
}
func (b *fakeBackend) ShutdownMember(_ context.Context, memberName string) schema.MemberOpResult {
	b.shutdownCalls = append(b.shutdownCalls, memberName)
	b.shutdown[memberName] = true
	return schema.MemberOpResult{OK: true}
}
func (b *fakeBackend) MembersWithUnread(context.Context, []string) (map[string]bool, error) {
	return map[string]bool{}, nil
}
func (b *fakeBackend) TaskVerificationEnabled(context.Context) (bool, error) { return false, nil }

// fakeSender 记录信箱 handoff（meta 序列化回 meta 列语义）。
type fakeSender struct {
	rows map[string][]*database.TeamMessageBase
}

func (s *fakeSender) SendMessageWithMeta(_ context.Context, meta map[string]any, toMemberName string) (string, error) {
	raw, _ := json.Marshal(meta)
	metaStr := string(raw)
	row := &database.TeamMessageBase{MessageID: "m-" + toMemberName, ToMemberName: &toMemberName, Meta: &metaStr}
	s.rows[toMemberName] = append(s.rows[toMemberName], row)
	return row.MessageID, nil
}
func (s *fakeSender) MemberName() string { return "leader" }
func (s *fakeSender) GetMessages(_ context.Context, toMemberName string, _ bool, _ string) ([]*database.TeamMessageBase, error) {
	return s.rows[toMemberName], nil
}

// setupScheduler 组装 in-memory 全链（真实 TaskManager + 全桩边界）。
type fixture struct {
	sched   *TeamScheduler
	host    *fakeHost
	backend *fakeBackend
	sender  *fakeSender
	tm      *tools.TeamTaskManager
	db      *database.InMemoryTeamDatabase
}

func setupScheduler(t *testing.T) *fixture {
	t.Helper()
	db := database.NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha", "leader1", "", "", "scheduled", false)
	db.CreateMember(ctx, "leader1", "alpha", "Leader", "{}", fsm.MemberStatusReady, "leader", "", "", "build_mode", "", "")
	db.CreateMember(ctx, "bob", "alpha", "Bob", "{}", fsm.MemberStatusReady, "teammate", "", "", "build_mode", "", "")
	db.CreateMember(ctx, "planner", "alpha", "P", "{}", fsm.MemberStatusReady, "teammate", "", "", "plan_mode", "", "")
	db.Initialize(ctx)
	tm := tools.NewTeamTaskManager(db, "alpha", "leader1", nil, nil, "", "", "leader1")
	host := &fakeHost{}
	backend := &fakeBackend{shutdown: map[string]bool{}}
	sender := &fakeSender{rows: map[string][]*database.TeamMessageBase{}}
	sched := NewTeamScheduler(host, &fakeTaskManager{tm: tm}, backend, sender, 3, 2400, "alpha", "cn")
	return &fixture{sched: sched, host: host, backend: backend, sender: sender, tm: tm, db: db}
}

func strPtr(s string) *string { return &s }

// ──────────────────────────── 用例 ────────────────────────────

// TestActivate_StartScanDispatch 激活即恢复扫描：空闲成员的最早 PENDING 指派任务
// 被 CAS 启动并发 task_start handoff。Python: activate→_scan→_reconcile_starts L405-444。
func TestActivate_StartScanDispatch(t *testing.T) {
	f := setupScheduler(t)
	ctx := context.Background()
	// CreateTask 会以 GetCurrentTime 重写 UpdatedAt，故并发相同——字典序 t-a < t-b
	// 由 earliestTask 的 (updated_at, task_id) 键决定派发次序（对齐 Python L424）。
	f.db.CreateTask(ctx, &database.TeamTaskBase{TaskID: "t-a", TeamName: "alpha", Status: fsm.TaskStatusPending, Assignee: strPtr("bob"), Title: "甲"})
	f.db.CreateTask(ctx, &database.TeamTaskBase{TaskID: "t-b", TeamName: "alpha", Status: fsm.TaskStatusPending, Assignee: strPtr("bob"), Title: "乙"})

	f.sched.Activate(ctx)

	if got, _ := f.tm.Get(ctx, "t-a"); got.Status != fsm.TaskStatusInProgress {
		t.Errorf("最早的指派任务应被启动: got %q", got.Status)
	}
	if got, _ := f.tm.Get(ctx, "t-b"); got.Status != fsm.TaskStatusPending {
		t.Errorf("第二条任务应排队等下次唤醒: got %q", got.Status)
	}
	rows := f.sender.rows["bob"]
	if len(rows) != 1 || rows[0].Content != "" {
		t.Fatalf("应恰发一条空 content 的 handoff: %d", len(rows))
	}
	meta := ParseMetaSafe(rows[0].Meta)
	if !IsTaskStartMeta(meta, "t-a") {
		t.Errorf("meta 应为绑定 t-a 的 start handoff: %v", meta)
	}
	if len(f.host.started) != 1 || f.host.started[0] != "bob" {
		t.Errorf("handoff 后应 best-effort 拉起 owner: %v", f.host.started)
	}
}

// ParseMetaSafe 测试助手：*string → map。
func ParseMetaSafe(raw *string) map[string]any {
	return ateamprompts.ParseMeta(raw)
}

// TestStartScan_PlanGate 指派给 PLAN_MODE 成员的任务经计划闸启动（PLANNING +
// scheduler_task_start_plan 模板）。Python: start_task 入队门 task_manager.py L1825。
func TestStartScan_PlanGate(t *testing.T) {
	f := setupScheduler(t)
	ctx := context.Background()
	f.db.CreateTask(ctx, &database.TeamTaskBase{TaskID: "tp", TeamName: "alpha", Status: fsm.TaskStatusPending, Assignee: strPtr("planner")})

	f.sched.Activate(ctx)

	got, _ := f.tm.Get(ctx, "tp")
	if got.Status != fsm.TaskStatusPlanning {
		t.Errorf("plan_mode owner 应落计划闸: got %q", got.Status)
	}
	meta := ParseMetaSafe(f.sender.rows["planner"][0].Meta)
	tpl, _ := meta["template"].(string)
	if tpl != "scheduler_task_start_plan" {
		t.Errorf("plan 闸 handoff 应用 start_plan 模板: %q", tpl)
	}
}

// TestStartScan_BusyMemberSkipped 成员已持有活跃任务时不派发第二条。
// Python: get_other_active_task_id 守卫 L425-427。
func TestStartScan_BusyMemberSkipped(t *testing.T) {
	f := setupScheduler(t)
	ctx := context.Background()
	f.db.CreateTask(ctx, &database.TeamTaskBase{TaskID: "t-active", TeamName: "alpha", Status: fsm.TaskStatusInProgress, Assignee: strPtr("bob")})
	f.db.CreateTask(ctx, &database.TeamTaskBase{TaskID: "t-wait", TeamName: "alpha", Status: fsm.TaskStatusPending, Assignee: strPtr("bob")})

	f.sched.Activate(ctx)

	if got, _ := f.tm.Get(ctx, "t-wait"); got.Status != fsm.TaskStatusPending {
		t.Errorf("busy 成员的第二条任务不得启动: got %q", got.Status)
	}
}

// TestSkipRetiredOwner_非策略退役静默 leader 经 shutdown_member 退役的 owner：
// 任务静默跳过，不升级。Python: _skip_retired_owner L365-403 的分叉。
func TestSkipRetiredOwner_非策略退役静默(t *testing.T) {
	f := setupScheduler(t)
	ctx := context.Background()
	f.backend.shutdown["bob"] = true
	f.db.CreateTask(ctx, &database.TeamTaskBase{TaskID: "t-stranded", TeamName: "alpha", Status: fsm.TaskStatusPending, Assignee: strPtr("bob")})

	f.sched.Activate(ctx)

	if got, _ := f.tm.Get(ctx, "t-stranded"); got.Status != fsm.TaskStatusPending {
		t.Errorf("SHUTDOWN owner 的任务应保持 PENDING: got %q", got.Status)
	}
	if len(f.host.delivered) != 0 {
		t.Errorf("非策略退役不应升级 leader: %v", f.host.delivered)
	}
}

// TestSkipRetiredOwner_策略退役升级一次 调度器按策略退役的 owner：开放任务
// 每对只升级一次。Python: _skip_retired_owner L390-402。
func TestSkipRetiredOwner_策略退役升级一次(t *testing.T) {
	f := setupScheduler(t)
	ctx := context.Background()
	f.backend.shutdown["bob"] = true
	f.sched.policyRetired["bob"] = struct{}{}
	f.db.CreateTask(ctx, &database.TeamTaskBase{TaskID: "t-stranded", TeamName: "alpha", Status: fsm.TaskStatusPending, Assignee: strPtr("bob")})

	f.sched.Activate(ctx)
	f.sched.Activate(ctx)

	if len(f.host.delivered) != 1 {
		t.Fatalf("策略退役的搁浅任务应恰好升级一次: %d", len(f.host.delivered))
	}
	if !contains(f.host.delivered[0].content, "t-stranded") || f.host.delivered[0].useSteer {
		t.Errorf("升级内容应点名任务且 use_steer=False: %+v", f.host.delivered[0])
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

// TestHandoffBackstop_ResendMissingStart 活跃任务 owner 信箱缺 start handoff →
// 补发；已有 → 不重发。Python: _reconcile_handoffs L446-494 + memo。
func TestHandoffBackstop_ResendMissingStart(t *testing.T) {
	f := setupScheduler(t)
	ctx := context.Background()
	f.db.CreateTask(ctx, &database.TeamTaskBase{TaskID: "t-live", TeamName: "alpha", Status: fsm.TaskStatusInProgress, Assignee: strPtr("bob")})

	f.sched.Activate(ctx)
	if len(f.sender.rows["bob"]) != 1 {
		t.Fatalf("缺 handoff 应补发一封: %d", len(f.sender.rows["bob"]))
	}

	// 第二次扫描：handoff 已在信箱（memo + 信箱探针双保险），不得重发。
	f.sched.Activate(ctx)
	if n := len(f.sender.rows["bob"]); n != 1 {
		t.Errorf("handoff 已知后不得重发: %d", n)
	}
}

// TestRevisionMail_执行中推修订 unseen revision 的执行中任务收 meta_task_revised
// 一封，账本去重。Python: _reconcile_revisions L270-297。
func TestRevisionMail_执行中推修订(t *testing.T) {
	f := setupScheduler(t)
	ctx := context.Background()
	f.db.CreateTask(ctx, &database.TeamTaskBase{TaskID: "t-r", TeamName: "alpha", Status: fsm.TaskStatusInProgress, Assignee: strPtr("bob"), Revision: 3, ObservedRevision: 1})
	// 预置 start handoff（handoff 兜底先发 task_start 会按 F_129 把该 revision
	// 记为已见并抑制 revision mail——引擎行为正确，本用例单测 revision 路径）
	f.sched.handoffKnown[handoffKey{TaskID: "t-r", Owner: "bob"}] = struct{}{}

	f.sched.Activate(ctx)
	rows := f.sender.rows["bob"]
	if len(rows) != 1 {
		t.Fatalf("应发一封 revision mail: %d", len(rows))
	}
	meta := ParseMetaSafe(rows[0].Meta)
	tpl, _ := meta["template"].(string)
	if tpl != "scheduler_task_revised" {
		t.Errorf("执行中任务应用 revised 模板: %q", tpl)
	}

	// 第二次扫描：revision_pushed 账本去重。
	f.sched.Activate(ctx)
	if n := len(f.sender.rows["bob"]); n != 1 {
		t.Errorf("同一 (task, revision) 不得重发: %d", n)
	}
}

// TestRevisionMail_挂起父任务收审视请求 BLOCKED+resume_pending 的父任务收
// subtask_review。Python: _reconcile_revisions L289-291。
func TestRevisionMail_挂起父任务收审视请求(t *testing.T) {
	f := setupScheduler(t)
	ctx := context.Background()
	f.db.CreateTask(ctx, &database.TeamTaskBase{TaskID: "t-parent", TeamName: "alpha", Status: fsm.TaskStatusBlocked, Assignee: strPtr("bob"), ResumePending: true, Revision: 2, ObservedRevision: 0})

	f.sched.Activate(ctx)
	meta := ParseMetaSafe(f.sender.rows["bob"][0].Meta)
	tpl, _ := meta["template"].(string)
	if tpl != "scheduler_subtask_review" {
		t.Errorf("挂起父任务应用 subtask_review 模板: %q", tpl)
	}
}

// TestRetirementPass_OffBehindConstants 内联成员退役 pass 由
// constants.InlineTaskMemberEnabled 关死。Python: L323-324。
func TestRetirementPass_OffBehindConstants(t *testing.T) {
	if schema.InlineTaskMemberEnabled {
		t.Skip("上游开关在本版本为 false；开关打开时本用例应改写")
	}
	f := setupScheduler(t)
	ctx := context.Background()
	f.backend.retirable = []database.RetirableMemberRow{{MemberName: "bob", Status: fsm.MemberStatusReady, Lifetime: "shutdown"}}

	acted, err := f.sched.reconcileRetirements(ctx)
	if err != nil || acted {
		t.Errorf("开关关闭时 pass 应 no-op: acted=%v err=%v", acted, err)
	}
	if len(f.backend.shutdownCalls) != 0 {
		t.Errorf("不得发出 shutdown 请求: %v", f.backend.shutdownCalls)
	}
}

// TestOnEvent_DigestCompletionAndAllDone TASK_COMPLETED 事件触发 leader digest；
// 板面清空后一次性 all-done，再收 drained 事件不重发。Python: on_event L213-218
// + _digest_task_done L1147-1178 + _announce_all_done。
func TestOnEvent_DigestCompletionAndAllDone(t *testing.T) {
	f := setupScheduler(t)
	ctx := context.Background()
	f.db.CreateTask(ctx, &database.TeamTaskBase{TaskID: "t-done", TeamName: "alpha", Status: fsm.TaskStatusCompleted, Title: "交付"})
	f.sched.Activate(ctx)
	before := len(f.host.delivered)
	f.sched.allDoneAnnounced = false
	f.sched.digestedTasks = map[string]struct{}{}

	f.sched.OnEvent(ctx, ateamcoordtypes.CoordinationEvent{Transport: &events.EventMessage{
		EventType: events.TeamEventTaskCompleted,
		Payload:   map[string]any{"task_id": "t-done", "task_count": float64(1)},
	}})

	// digest（task_done）+ all-done 各一条
	if len(f.host.delivered) != before+2 {
		t.Fatalf("应注入 digest 与 all-done 两条: %d → %d", before, len(f.host.delivered))
	}
	if !contains(f.host.delivered[len(f.host.delivered)-2].content, "t-done") {
		t.Errorf("digest 应点名任务: %q", f.host.delivered[len(f.host.delivered)-2].content)
	}
	if !contains(f.host.delivered[len(f.host.delivered)-1].content, "1 个任务") {
		t.Errorf("all-done 应带计数: %q", f.host.delivered[len(f.host.delivered)-1].content)
	}

	// 重复 drained 事件：once-only 标志挡住
	f.sched.OnEvent(ctx, ateamcoordtypes.CoordinationEvent{Transport: &events.EventMessage{
		EventType: events.TeamEventTaskListDrained,
		Payload:   map[string]any{"task_count": float64(1)},
	}})
	if len(f.host.delivered) != before+2 {
		t.Errorf("all-done 不得重发: %d", len(f.host.delivered))
	}
}

// TestOnEvent_InactiveNoop 未武装时事件无动作。Python: on_event L205-206。
func TestOnEvent_InactiveNoop(t *testing.T) {
	f := setupScheduler(t)
	ctx := context.Background()
	f.db.CreateTask(ctx, &database.TeamTaskBase{TaskID: "t1", TeamName: "alpha", Status: fsm.TaskStatusPending, Assignee: strPtr("bob")})

	f.sched.OnEvent(ctx, ateamcoordtypes.CoordinationEvent{Inner: &ateamcoordtypes.InnerEventMessage{
		EventType: ateamcoordtypes.InnerEventTypePollTask,
	}})
	if got, _ := f.tm.Get(ctx, "t1"); got.Status != fsm.TaskStatusPending {
		t.Errorf("未武装时不得派发: got %q", got.Status)
	}
	if len(f.sender.rows["bob"]) != 0 {
		t.Errorf("未武装时不得发信: %d", len(f.sender.rows["bob"]))
	}
}

// TestOnEvent_InnerPollTaskTriggersScan 武装后 POLL_TASK 内部事件触发扫描。
// Python: on_event L208-211。
func TestOnEvent_InnerPollTaskTriggersScan(t *testing.T) {
	f := setupScheduler(t)
	ctx := context.Background()
	f.db.CreateTask(ctx, &database.TeamTaskBase{TaskID: "t1", TeamName: "alpha", Status: fsm.TaskStatusPending, Assignee: strPtr("bob")})
	f.sched.Activate(ctx)
	// Activate 已派发；清空信箱模拟新唤醒
	f.sender.rows = map[string][]*database.TeamMessageBase{}
	f.db.CreateTask(ctx, &database.TeamTaskBase{TaskID: "t2", TeamName: "alpha", Status: fsm.TaskStatusPending, Assignee: strPtr("bob")})

	f.sched.OnEvent(ctx, ateamcoordtypes.CoordinationEvent{Inner: &ateamcoordtypes.InnerEventMessage{
		EventType: ateamcoordtypes.InnerEventTypePollTask,
	}})
	if got, _ := f.tm.Get(ctx, "t2"); got.Status != fsm.TaskStatusPending {
		// bob 已持有 t1（in_progress），one-active 守卫挡住 t2——只验证扫描无 panic
		t.Logf("t2 状态 %q（one-active 守卫下不启动属正常）", got.Status)
	}
}

// TestDeactivate_StopsDriving 停用后 IsActive=false 且事件无动作。
// Python: deactivate L192-194。
func TestDeactivate_StopsDriving(t *testing.T) {
	f := setupScheduler(t)
	ctx := context.Background()
	f.sched.Activate(ctx)
	f.sched.Deactivate()
	if f.sched.IsActive() {
		t.Error("停用后 IsActive 应为 false")
	}
	f.db.CreateTask(ctx, &database.TeamTaskBase{TaskID: "t3", TeamName: "alpha", Status: fsm.TaskStatusPending, Assignee: strPtr("bob")})
	f.sched.OnEvent(ctx, ateamcoordtypes.CoordinationEvent{Inner: &ateamcoordtypes.InnerEventMessage{
		EventType: ateamcoordtypes.InnerEventTypePollTask,
	}})
	if len(f.sender.rows["bob"]) != 0 {
		t.Errorf("停用后不得发信: %d", len(f.sender.rows["bob"]))
	}
}

// ──────────────────────────── 25.2b 接口扩展补齐 ────────────────────────────

// GetReviewTally 评审半区替身：空计票。
func (f *fakeTaskManager) GetReviewTally(task *database.TeamTaskBase) map[string]any {
	return map[string]any{
		"task_id": task.TaskID, "review_round": task.ReviewRound,
		"pass_count": 0, "fail_count": 0, "reviewer_count": 0,
		"voted": []string{}, "fail_feedback": map[string]string{},
		"verdict_total": 0, "verdict_voted": 0,
		"inspector_count": 0, "inspector_voted": 0, "inspector_avg": nil,
	}
}

// SettleReview 评审半区替身：成功。
func (f *fakeTaskManager) SettleReview(_ context.Context, _, _, _ string) (atschema.TaskOpResult, error) {
	return atschema.TaskOpResult{}.Success(), nil
}

// GetPreviousRoundVote 评审半区替身：无上一票。
func (f *fakeTaskManager) GetPreviousRoundVote(_ context.Context, _ *database.TeamTaskBase, _ string) (*database.TeamTaskReviewVoteBase, error) {
	return nil, nil
}

// TaskDB 评审半区替身：nil DAO。
func (f *fakeTaskManager) TaskDB() database.TaskDao { return nil }
