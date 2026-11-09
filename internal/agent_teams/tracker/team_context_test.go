package tracker

import (
	"context"
	"strings"
	"testing"
	"time"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	sessionstate "github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSession 极简 SessionFacade，只为基线读写。
type fakeSession struct {
	id        string
	state     map[string]any
	commits   int
	commitErr error
}

func newFakeSession(id string) *fakeSession {
	return &fakeSession{id: id, state: map[string]any{}}
}

func (f *fakeSession) GetSessionID() string { return f.id }

func (f *fakeSession) UpdateState(d map[string]any) {
	for k, v := range d {
		f.state[k] = v
	}
}

func (f *fakeSession) GetState(k sessionstate.StateKey) (any, error) {
	return f.state[k.String()], nil
}

func (f *fakeSession) DumpState() map[string]any { return f.state }

func (f *fakeSession) WriteStream(_ context.Context, _ any) error { return nil }

func (f *fakeSession) WriteCustomStream(_ context.Context, _ any) error { return nil }

func (f *fakeSession) GetEnv(_ string, _ ...any) any { return nil }

func (f *fakeSession) Interact(_ context.Context, _ any) error { return nil }

func (f *fakeSession) ClearSession(_ context.Context) { f.state = map[string]any{} }

// Commit 测试替身的 flush。
func (f *fakeSession) Commit(_ context.Context) error {
	f.commits++
	return f.commitErr
}

// TestReadBaseline_空与非法类型 测试空 state 与错误类型都返回空 map
func TestReadBaseline_空与非法类型(t *testing.T) {
	sess := newFakeSession("s1")
	if got := (&TeamContextTracker{}).readBaseline(sess); len(got) != 0 {
		t.Fatalf("空 state 返回 %v, want empty", got)
	}
	sess.state[TeamContextStateKey] = "not-a-map"
	if got := (&TeamContextTracker{}).readBaseline(sess); len(got) != 0 {
		t.Fatalf("非法类型返回 %v, want empty", got)
	}
}

// TestComposeBlocks_顺序 测试 identity→policy→roster 且 identity/info 合一块
//
// Python: _compose_blocks (team_context.py L304-357)
func TestComposeBlocks_顺序(t *testing.T) {
	tr := &TeamContextTracker{language: "cn"}
	identity := "我是 leader"
	info := "团队 A"
	policy := "<team-policy>x</team-policy>"
	roster := "<team-event kind=\"roster\">roster</team-event>"
	blocks := tr.composeBlocks(&identity, &info, &roster, &policy)
	if len(blocks) != 3 {
		t.Fatalf("blocks = %d, want 3: %v", len(blocks), blocks)
	}
	if !strings.Contains(blocks[0], "我是 leader") || !strings.Contains(blocks[0], "团队 A") {
		t.Fatalf("blocks[0] 应合并 identity+info: %q", blocks[0])
	}
	if !strings.HasPrefix(blocks[0], "<team-context>") {
		t.Fatalf("blocks[0] 应是 <team-context>: %q", blocks[0])
	}
	if blocks[1] != policy {
		t.Fatalf("blocks[1] = %q, want policy", blocks[1])
	}
	if blocks[2] != roster {
		t.Fatalf("blocks[2] = %q, want roster", blocks[2])
	}
}

// TestComposeBlocks_只有info不渲染空identity 测试 team-info-only 更新走 flat 路径
//
// Python: _compose_blocks 中 fork 分支要求 identity_body 本身存在（L324-330）
func TestComposeBlocks_只有info不渲染空identity(t *testing.T) {
	tr := &TeamContextTracker{language: "cn", forkCapable: true, forkSource: "bob"}
	info := "团队 A"
	blocks := tr.composeBlocks(nil, &info, nil, nil)
	if len(blocks) != 1 {
		t.Fatalf("blocks = %d, want 1", len(blocks))
	}
	if strings.Contains(blocks[0], "<identity>") {
		t.Fatalf("不应渲染空 <identity>: %q", blocks[0])
	}
	if !strings.Contains(blocks[0], "团队 A") {
		t.Fatalf("应含团队信息: %q", blocks[0])
	}
}

// TestPolicyBlock_哈希门 测试首投输出、同文本零输出、换文本重播
//
// Python: _policy_block (team_context.py L359-373)
func TestPolicyBlock_哈希门(t *testing.T) {
	policy := "<team-policy>v1</team-policy>"
	tr := &TeamContextTracker{policyText: &policy}

	baseline := map[string]any{}
	updated := map[string]any{}
	first := tr.policyBlock(baseline, updated)
	if first == nil || *first != policy {
		t.Fatalf("首投应返回政策块, got %v", first)
	}
	// 把 updated 当作新基线：同文本应无输出
	if got := tr.policyBlock(updated, map[string]any{}); got != nil {
		t.Fatalf("同文本应无输出, got %q", *got)
	}
}

// TestPolicyBlock_换文本重播 测试政策文本变化时重新投递
func TestPolicyBlock_换文本重播(t *testing.T) {
	v1 := "<team-policy>v1</team-policy>"
	v2 := "<team-policy>v2</team-policy>"
	tr := &TeamContextTracker{policyText: &v1}
	updated := map[string]any{}
	_ = tr.policyBlock(map[string]any{}, updated)

	tr.policyText = &v2
	if got := tr.policyBlock(updated, map[string]any{}); got == nil || *got != v2 {
		t.Fatalf("换文本应重播 v2, got %v", got)
	}
}

// TestPolicyBlock_nil时禁用 测试 policyText 为 nil 时通道关闭
func TestPolicyBlock_nil时禁用(t *testing.T) {
	tr := &TeamContextTracker{}
	if got := tr.policyBlock(map[string]any{}, map[string]any{}); got != nil {
		t.Fatalf("nil policyText 应返回 nil, got %q", *got)
	}
}

// TestPersist_写状态与flush 测试基线写入会话 state 并触发 commit
func TestPersist_写状态与flush(t *testing.T) {
	sess := newFakeSession("s1")
	tr := &TeamContextTracker{memberName: "alice"}
	tr.persist(sess, map[string]any{baselineIdentityEmitted: true})

	got, ok := sess.state[TeamContextStateKey].(map[string]any)
	if !ok {
		t.Fatalf("state 未写入: %v", sess.state)
	}
	if got[baselineIdentityEmitted] != true {
		t.Fatalf("基线内容错误: %v", got)
	}
	if sess.commits != 1 {
		t.Fatalf("commit 次数 = %d, want 1", sess.commits)
	}
}

// TestPersist_commit失败只告警 测试 checkpoint 写失败不中断流程
func TestPersist_commit失败只告警(t *testing.T) {
	sess := newFakeSession("s1")
	sess.commitErr = context.Canceled
	tr := &TeamContextTracker{memberName: "alice"}
	tr.persist(sess, map[string]any{baselineIdentityEmitted: true}) // 不应 panic
	if _, ok := sess.state[TeamContextStateKey]; !ok {
		t.Fatal("UpdateState 仍应写入")
	}
}

// TestIdentityBody_首投后零新增 测试第二次调用返回 nil（不重述恒定字段）
//
// Python: _identity_body (team_context.py L379-455)
func TestIdentityBody_首投后零新增(t *testing.T) {
	tr := &TeamContextTracker{
		memberName:  "alice",
		displayName: "Alice",
		language:    "cn",
	}
	baseline := map[string]any{}
	updated := map[string]any{}
	first := tr.identityBody(baseline, updated)
	if first == nil || *first == "" {
		t.Fatal("首投应返回身份正文")
	}
	if updated[baselineIdentityEmitted] != true {
		t.Fatal("首投应置 identity_emitted")
	}
	// 无 backend：已投递后 prompt 演进通道关闭，直接返回 nil
	if second := tr.identityBody(updated, map[string]any{}); second != nil {
		t.Fatalf("二次应无输出, got %q", *second)
	}
}

// TestIdentityBody_成员行缺失返回nil 测试有 backend 但成员行不存在时不宣告
func TestIdentityBody_成员行缺失返回nil(t *testing.T) {
	tb, _ := newTestBackend(t)
	tr := NewTeamContextTracker(tb, "ghost", atschema.TeamRoleTeammate,
		"", "", "", "", "", false, "cn", "", nil)
	updated := map[string]any{}
	if got := tr.identityBody(map[string]any{}, updated); got != nil {
		t.Fatalf("成员行不存在应返回 nil, got %q", *got)
	}
	if _, exists := updated[baselineIdentityEmitted]; exists {
		t.Fatal("未投递不应置 identity_emitted")
	}
}

// TestTeamInfoBody_无团队行不播 测试团队行缺失时什么都不宣告
//
// Python: _team_info_body 的「Nothing is announced until the team row exists」
func TestTeamInfoBody_无团队行不播(t *testing.T) {
	tb, _ := newTestBackend(t) // 未建 team 行
	tr := NewTeamContextTracker(tb, "alice", atschema.TeamRoleTeammate,
		"", "", "", "", "", false, "cn", "", nil)
	updated := map[string]any{}
	if got := tr.teamInfoBody(map[string]any{}, updated); got != nil {
		t.Fatalf("无团队行应返回 nil, got %q", *got)
	}
	// 但探针基线仍应推进（否则每次都查 DB）
	if _, ok := updated[baselineTeamInfoMtime]; !ok {
		t.Fatal("探针推进应记入基线")
	}
}

// TestTeamInfoBody_有团队行首投 测试团队行存在时渲染团队信息
func TestTeamInfoBody_有团队行首投(t *testing.T) {
	tb, _ := newTestBackend(t)
	ctx := context.Background()
	_ = tb.DB().Team().CreateTeam(ctx, "test-team", "测试团队", "leader", "目标描述", "", "autonomous", false)
	tr := NewTeamContextTracker(tb, "alice", atschema.TeamRoleTeammate,
		"", "", "", "/team/ws", "", false, "cn", "", nil)
	got := tr.teamInfoBody(map[string]any{}, map[string]any{})
	if got == nil || !strings.Contains(*got, "测试团队") {
		t.Fatalf("应渲染团队信息, got %v", got)
	}
}

// TestTeamInfoBody_mtime未变零新增 测试探针不动时不重复宣告
func TestTeamInfoBody_mtime未变零新增(t *testing.T) {
	tb, _ := newTestBackend(t)
	tr := NewTeamContextTracker(tb, "alice", atschema.TeamRoleTeammate,
		"", "", "", "", "", false, "cn", "", nil)
	updated := map[string]any{}
	_ = tr.teamInfoBody(map[string]any{}, updated) // 首投（可能 nil，无团队行）
	// 用推进后的基线再调一次：mtime 未变 → nil
	if got := tr.teamInfoBody(updated, map[string]any{}); got != nil {
		t.Fatalf("mtime 未变应返回 nil, got %q", *got)
	}
}

// TestRosterBlock_无backend返回nil 测试 teamBackend 为 nil 时名册通道关闭
func TestRosterBlock_无backend返回nil(t *testing.T) {
	tr := &TeamContextTracker{language: "cn", markHumans: true}
	updated := map[string]any{}
	if got := tr.rosterBlock(map[string]any{}, updated); got != nil {
		t.Fatalf("无 backend 应返回 nil, got %q", *got)
	}
	if _, exists := updated[baselineRoster]; exists {
		t.Fatal("不应记 roster 基线")
	}
}

// TestRosterBlock_首投快照 测试首次渲染全量名册事件
func TestRosterBlock_首投快照(t *testing.T) {
	tb, db := newTestBackend(t)
	createPeer(t, db, "test-team", "bob", "Bob")
	tr := NewTeamContextTracker(tb, "alice", atschema.TeamRoleTeammate,
		"", "", "", "", "", false, "cn", "", nil)
	updated := map[string]any{}
	got := tr.rosterBlock(map[string]any{}, updated)
	if got == nil {
		t.Fatal("首投应返回名册快照")
	}
	if !strings.Contains(*got, "roster") || !strings.Contains(*got, "bob") {
		t.Fatalf("快照内容错误: %q", *got)
	}
	if _, ok := updated[baselineRoster]; !ok {
		t.Fatal("投递成功后应记 roster 基线")
	}
}

// TestRosterBlock_名册变化播增量 测试名册集合变化播 roster-change 且不重述旧成员
//
// 库里没有删除单个成员的 DAO 方法（上游 MemberDao 也没有 delete），故用
// 「换一个同伴集合」模拟 leave+join：先只有 bob，再加 carol，两个成员行都在，
// diff 结果为 joined=[carol]。leave 分支由 prompts.DiffRoster 的单元测试覆盖，
// 此处只验 tracker 的 delta 接线与 kind 契约 token。
func TestRosterBlock_名册变化播增量(t *testing.T) {
	tb, db := newTestBackend(t)
	createPeer(t, db, "test-team", "bob", "Bob")
	tr := NewTeamContextTracker(tb, "alice", atschema.TeamRoleTeammate,
		"", "", "", "", "", false, "cn", "", nil)

	updated := map[string]any{}
	if first := tr.rosterBlock(map[string]any{}, updated); first == nil {
		t.Fatal("首投应返回快照")
	}

	// 毫秒级时间戳：同毫秒内两次 CreateMember 的 UpdatedAt 相同，探针不推进，
	// 故等 2ms 保证 mtime 真的前进（不引入时钟注入——生产用的就是墙钟）。
	time.Sleep(2 * time.Millisecond)
	createPeer(t, db, "test-team", "carol", "Carol")
	got := tr.rosterBlock(updated, map[string]any{})
	if got == nil {
		t.Fatal("名册变化应返回增量")
	}
	if !strings.Contains(*got, "roster-change") {
		t.Fatalf("kind 应为 roster-change: %q", *got)
	}
	if !strings.Contains(*got, "carol") {
		t.Fatalf("增量应含新成员 carol: %q", *got)
	}
}

// TestRosterBlock_探针未动零新增 测试 mtime 不变时返回 nil
func TestRosterBlock_探针未动零新增(t *testing.T) {
	tb, db := newTestBackend(t)
	createPeer(t, db, "test-team", "bob", "Bob")
	tr := NewTeamContextTracker(tb, "alice", atschema.TeamRoleTeammate,
		"", "", "", "", "", false, "cn", "", nil)
	updated := map[string]any{}
	_ = tr.rosterBlock(map[string]any{}, updated)
	if got := tr.rosterBlock(updated, map[string]any{}); got != nil {
		t.Fatalf("探针未动应返回 nil, got %q", *got)
	}
}

// ──────────────────────────── 任务通知三兄弟 ────────────────────────────

// TestTaskOutcome_带reviewer完成触发通知 测试验收通过的完成才通知
//
// Python: TaskOutcomeTracker (team_context.py L871-907)
func TestTaskOutcome_带reviewer完成触发通知(t *testing.T) {
	tb, db := newTestBackend(t)
	createAssignedTask(t, db, "test-team", "t-1", "alice",
		string(atschema.TaskStatusCompleted), `[{"reviewer_id":"bob"}]`)

	tr := newTaskOutcomeTracker(tb, "alice")
	notice := tr.claim(newFakeSession("s1"))
	if notice == nil {
		t.Fatal("带 reviewer 的完成应通知")
	}
	if !strings.Contains(notice.Text, "task-verified") {
		t.Fatalf("kind 应为 task-verified: %q", notice.Text)
	}
	if !strings.Contains(notice.Text, "t-1") {
		t.Fatalf("应含任务 id: %q", notice.Text)
	}
	if len(notice.Entries) != 1 || notice.Entries[0] != "t-1" {
		t.Fatalf("entries = %v", notice.Entries)
	}
}

// TestTaskOutcome_无reviewer不通知 测试自己交差的完成不播
func TestTaskOutcome_无reviewer不通知(t *testing.T) {
	tb, db := newTestBackend(t)
	createAssignedTask(t, db, "test-team", "t-1", "alice", string(atschema.TaskStatusCompleted), "")

	tr := newTaskOutcomeTracker(tb, "alice")
	if got := tr.claim(newFakeSession("s1")); got != nil {
		t.Fatalf("无 reviewer 不应通知, got %+v", got)
	}
}

// TestTaskOutcome_重复claim只播一次 测试基线生效
func TestTaskOutcome_重复claim只播一次(t *testing.T) {
	tb, db := newTestBackend(t)
	createAssignedTask(t, db, "test-team", "t-1", "alice",
		string(atschema.TaskStatusCompleted), `[{"reviewer_id":"bob"}]`)

	tr := newTaskOutcomeTracker(tb, "alice")
	sess := newFakeSession("s1")
	if tr.claim(sess) == nil {
		t.Fatal("首次应通知")
	}
	if got := tr.claim(sess); got != nil {
		t.Fatalf("重复 claim 不应再通知, got %+v", got)
	}
}

// TestTaskOutcome_无backend返回nil 测试 tracker 关闭时 claim nil
func TestTaskOutcome_无backend返回nil(t *testing.T) {
	tr := newTaskOutcomeTracker(nil, "alice")
	if got := tr.claim(newFakeSession("s1")); got != nil {
		t.Fatalf("无 backend 应返回 nil, got %+v", got)
	}
}

// TestTaskRevision_未读编辑触发通知 测试 revision>observed 时通知且含编辑说明
//
// Python: TaskRevisionTracker (team_context.py L918-971)
func TestTaskRevision_未读编辑触发通知(t *testing.T) {
	tb, db := newTestBackend(t)
	ctx := context.Background()
	createAssignedTask(t, db, "test-team", "t-1", "alice", string(atschema.TaskStatusClaimed), "")
	if ok, err := db.Task().AppendChangeNote(ctx, "t-1", 1, "改了要求", "leader"); err != nil || !ok {
		t.Fatalf("AppendChangeNote = (%v,%v)", ok, err)
	}

	tr := newTaskRevisionTracker(tb, "alice")
	notice := tr.claim(newFakeSession("s1"))
	if notice == nil {
		t.Fatal("应通知未读编辑")
	}
	if !strings.Contains(notice.Text, "task-revised") {
		t.Fatalf("kind 应为 task-revised: %q", notice.Text)
	}
	if !strings.Contains(notice.Text, "改了要求") {
		t.Fatalf("应含编辑说明: %q", notice.Text)
	}
	if !strings.Contains(notice.Text, "r0 → r1") {
		t.Fatalf("应含修订区间: %q", notice.Text)
	}
}

// TestTaskRevision_entry格式 测试 entry 形如 taskID@revision
func TestTaskRevision_entry格式(t *testing.T) {
	tr := newTaskRevisionTracker(nil, "alice")
	task := &database.TeamTaskBase{TaskID: "t-9", Revision: 4}
	if got := tr.hookEntry(task); got != "t-9@4" {
		t.Fatalf("entry = %q, want t-9@4", got)
	}
}

// TestTaskRevision_superseded保留最新 测试每个任务只保留最新修订
//
// Python: TaskRevisionTracker._superseded (team_context.py L948-959)
func TestTaskRevision_superseded保留最新(t *testing.T) {
	tr := newTaskRevisionTracker(nil, "alice")
	got := tr.supersededEntries([]string{"t-1@1", "t-1@3", "t-1@2", "t-2@1"})
	if len(got) != 2 {
		t.Fatalf("superseded = %v, want 2 条被淘汰", got)
	}
	for _, e := range got {
		if e == "t-1@3" || e == "t-2@1" {
			t.Fatalf("最新条目被误淘汰: %s", e)
		}
	}
}

// TestTaskNotice_claim后release可再claim 测试投递失败归还条目
//
// Python: release (team_context.py L795-799)
func TestTaskNotice_claim后release可再claim(t *testing.T) {
	tb, db := newTestBackend(t)
	createAssignedTask(t, db, "test-team", "t-1", "alice",
		string(atschema.TaskStatusCompleted), `[{"reviewer_id":"bob"}]`)

	tr := newTaskOutcomeTracker(tb, "alice")
	sess := newFakeSession("s1")
	first := tr.claim(sess)
	if first == nil {
		t.Fatal("应 claim 到通知")
	}
	tr.release(first)
	if second := tr.claim(sess); second == nil {
		t.Fatal("release 后应能再次 claim")
	}
}

// TestTaskNotice_persist幂等 测试基线未变时不重复 commit
//
// Python: persist (team_context.py L801-821)
func TestTaskNotice_persist幂等(t *testing.T) {
	tb, db := newTestBackend(t)
	createAssignedTask(t, db, "test-team", "t-1", "alice",
		string(atschema.TaskStatusCompleted), `[{"reviewer_id":"bob"}]`)

	tr := newTaskOutcomeTracker(tb, "alice")
	sess := newFakeSession("s1")
	_ = tr.claim(sess)
	tr.persist(sess)
	firstCommits := sess.commits
	tr.persist(sess) // 基线未变 → 不再 commit
	if sess.commits != firstCommits {
		t.Fatalf("commit 次数 %d → %d，基线未变不应重写", firstCommits, sess.commits)
	}
}

// TestTaskNotice_persist写回基线 测试基线落进会话 state
func TestTaskNotice_persist写回基线(t *testing.T) {
	tb, db := newTestBackend(t)
	createAssignedTask(t, db, "test-team", "t-1", "alice",
		string(atschema.TaskStatusCompleted), `[{"reviewer_id":"bob"}]`)

	tr := newTaskOutcomeTracker(tb, "alice")
	sess := newFakeSession("s1")
	_ = tr.claim(sess)
	tr.persist(sess)

	raw, ok := sess.state[TaskOutcomeStateKey].([]string)
	if !ok {
		t.Fatalf("基线未写入: %v", sess.state)
	}
	if len(raw) != 1 || raw[0] != "t-1" {
		t.Fatalf("基线内容 = %v, want [t-1]", raw)
	}
}

// TestTaskNotice_announce_wakes为假不发送 测试 TaskOutcomeTracker 不主动唤醒
//
// Python: announce 的 `if not self.WAKES: return False`（L835-836）
func TestTaskNotice_announce_wakes为假不发送(t *testing.T) {
	tb, db := newTestBackend(t)
	createAssignedTask(t, db, "test-team", "t-1", "alice",
		string(atschema.TaskStatusCompleted), `[{"reviewer_id":"bob"}]`)

	tr := newTaskOutcomeTracker(tb, "alice")
	sent := false
	ok, err := tr.announce(newFakeSession("s1"), func(string) error { sent = true; return nil })
	if err != nil {
		t.Fatalf("announce: %v", err)
	}
	if ok || sent {
		t.Fatal("wakes=false 的 tracker 不应发送")
	}
}

// TestTaskRevision_announce发送失败归还 测试 send 抛错时 release
func TestTaskRevision_announce发送失败归还(t *testing.T) {
	tb, db := newTestBackend(t)
	ctx := context.Background()
	createAssignedTask(t, db, "test-team", "t-1", "alice", string(atschema.TaskStatusClaimed), "")
	_, _ = db.Task().AppendChangeNote(ctx, "t-1", 1, "改了", "leader")

	tr := newTaskRevisionTracker(tb, "alice")
	sess := newFakeSession("s1")
	sendErr := context.DeadlineExceeded
	ok, err := tr.announce(sess, func(string) error { return sendErr })
	if ok || err == nil {
		t.Fatalf("发送失败应返回 (false, err), got (%v,%v)", ok, err)
	}
	// 归还后应能再次 claim
	if tr.claim(sess) == nil {
		t.Fatal("release 后应能再次 claim")
	}
}

// TestBuildTaskEventTracker_按dispatchMode分派 测试两种模式返不同具体类型
//
// Python: build_task_event_tracker (team_context.py L974-990)
func TestBuildTaskEventTracker_按dispatchMode分派(t *testing.T) {
	tb, _ := newTestBackend(t)
	if _, ok := BuildTaskEventTracker("scheduled", tb, "alice").(*TaskOutcomeTracker); !ok {
		t.Fatal("scheduled 应返 TaskOutcomeTracker")
	}
	if _, ok := BuildTaskEventTracker("autonomous", tb, "alice").(*TaskRevisionTracker); !ok {
		t.Fatal("autonomous 应返 TaskRevisionTracker")
	}
	if got := BuildTaskEventTracker("scheduled", nil, "alice"); got != nil {
		t.Fatalf("backend nil 应返 nil, got %T", got)
	}
}

// TestTaskNotice_会话切换重载基线 测试换 session id 时从新会话状态重新加载
//
// Python: _bind (team_context.py L750-764)
func TestTaskNotice_会话切换重载基线(t *testing.T) {
	tb, db := newTestBackend(t)
	createAssignedTask(t, db, "test-team", "t-1", "alice",
		string(atschema.TaskStatusCompleted), `[{"reviewer_id":"bob"}]`)

	tr := newTaskOutcomeTracker(tb, "alice")
	s1 := newFakeSession("s1")
	notice := tr.claim(s1)
	if notice == nil {
		t.Fatal("s1 应 claim 到")
	}
	tr.persist(s1)

	// 新会话 s2 有自己的（空）基线 → 应能再次宣告
	s2 := newFakeSession("s2")
	if tr.claim(s2) == nil {
		t.Fatal("新会话 s2 基线为空，应能再次宣告")
	}
}

// TestLastDeliveredText_往返 测试 deliveredText 记入基线并能取回
//
// Python: last_delivered_text (team_context.py L285-302)
func TestLastDeliveredText_往返(t *testing.T) {
	sess := newFakeSession("s1")
	tr := NewTeamContextTracker(nil, "alice", atschema.TeamRoleTeammate,
		"Alice", "", "", "", "", false, "cn", "", nil)

	if _, ok := tr.LastDeliveredText(sess); ok {
		t.Fatal("初始不应有记录")
	}
	delivered := "<team-context>已投递内容</team-context>"
	tr.Commit(sess, &delivered)

	got, ok := tr.LastDeliveredText(sess)
	if !ok || got != delivered {
		t.Fatalf("LastDeliveredText = (%q,%v), want (%q,true)", got, ok, delivered)
	}
}

// TestCommit_nil时不做任何事 测试无待提交且无 deliveredText 时不写状态
func TestCommit_nil时不做任何事(t *testing.T) {
	sess := newFakeSession("s1")
	tr := NewTeamContextTracker(nil, "alice", atschema.TeamRoleTeammate,
		"", "", "", "", "", false, "cn", "", nil)

	tr.Commit(sess, nil)
	if len(sess.state) != 0 {
		t.Fatalf("无待提交时不应写状态, got %v", sess.state)
	}
	if sess.commits != 0 {
		t.Fatalf("commit 次数 = %d, want 0", sess.commits)
	}
}

// TestPendingText_无文本但推进基线 测试探针推进却无内容可说时就地推进基线
//
// 上游理由：那些内容不可能投递失败，不推进会导致每次调用都重新查 DB。
// Python: pending_text 尾部（team_context.py L257-259）
func TestPendingText_无文本但推进基线(t *testing.T) {
	tb, _ := newTestBackend(t) // 无 team 行、无成员行
	tr := NewTeamContextTracker(tb, "alice", atschema.TeamRoleTeammate,
		"", "", "", "", "", false, "cn", "", nil)
	sess := newFakeSession("s1")

	if text, ok := tr.PendingText(sess); ok || text != "" {
		t.Fatalf("无内容时应返回空, got (%q,%v)", text, ok)
	}
	// team_info 探针推进了（mtime 从无到有），基线应已被写回
	if _, ok := sess.state[TeamContextStateKey]; !ok {
		t.Fatal("探针推进且无文本时，基线应就地推进")
	}
}

// TestPendingText_nil会话返回空 测试会话为 nil 时不做任何事
func TestPendingText_nil会话返回空(t *testing.T) {
	tr := NewTeamContextTracker(nil, "alice", atschema.TeamRoleTeammate,
		"Alice", "", "", "", "", false, "cn", "", nil)
	if text, ok := tr.PendingText(nil); ok || text != "" {
		t.Fatalf("nil 会话应返回空, got (%q,%v)", text, ok)
	}
}

// TestSnapshotText_含身份与团队块 测试全量快照忽略基线
//
// Python: snapshot_text (team_context.py L205-225)
func TestSnapshotText_含身份与团队块(t *testing.T) {
	tb, db := newTestBackend(t)
	ctx := context.Background()
	_ = tb.DB().Team().CreateTeam(ctx, "test-team", "测试团队", "alice", "目标", "", "autonomous", false)
	// 本成员自己的行：身份块要等它出现才渲染（display_name 必须来自 DB 行）
	if ok := db.Member().CreateMember(ctx, "alice", "test-team", "Alice", "{}",
		"ready", "teammate", "", "", "build_mode", "", ""); !ok {
		t.Fatal("建本成员行失败")
	}
	createPeer(t, db, "test-team", "bob", "Bob")

	tr := NewTeamContextTracker(tb, "alice", atschema.TeamRoleTeammate,
		"Alice", "/ws/alice", "", "/team/ws", "", false, "cn", "", nil)
	text, ok := tr.SnapshotText()
	if !ok {
		t.Fatal("应返回全量快照")
	}
	for _, want := range []string{"Alice", "测试团队", "bob"} {
		if !strings.Contains(text, want) {
			t.Fatalf("快照应含 %q: %q", want, text)
		}
	}
	// 全量快照不写任何持久化数据
	if text2, _ := tr.SnapshotText(); text2 != text {
		t.Fatal("SnapshotText 应可重复调用且结果稳定")
	}
}

// TestSnapshotText_无内容返回false 测试无 backend 且无身份字段时返回 false
func TestSnapshotText_无内容返回false(t *testing.T) {
	tr := NewTeamContextTracker(nil, "", atschema.TeamRoleTeammate,
		"", "", "", "", "", false, "cn", "", nil)
	if text, ok := tr.SnapshotText(); ok || text != "" {
		t.Fatalf("无内容应返回 false, got (%q,%v)", text, ok)
	}
}

// TestPendingText_身份通道首投 测试有成员行时 PendingText 产出身份块并留待提交
func TestPendingText_身份通道首投(t *testing.T) {
	tb, db := newTestBackend(t)
	ctx := context.Background()
	_ = db.Member().CreateMember(ctx, "alice", "test-team", "Alice", "{}",
		"ready", "teammate", "", "", "build_mode", "私有约定", "")

	tr := NewTeamContextTracker(tb, "alice", atschema.TeamRoleTeammate,
		"", "/ws/alice", "私有约定", "/team/ws", "", false, "cn", "", nil)
	sess := newFakeSession("s1")

	text, ok := tr.PendingText(sess)
	if !ok || !strings.Contains(text, "Alice") {
		t.Fatalf("应产出身份块, got (%q,%v)", text, ok)
	}
	// 未 Commit 前基线不应推进
	if _, written := sess.state[TeamContextStateKey]; written {
		t.Fatal("PendingText 不得自行推进基线（要等 Commit）")
	}
	tr.Commit(sess, nil)
	if _, written := sess.state[TeamContextStateKey]; !written {
		t.Fatal("Commit 后基线应推进")
	}
}

// TestCommit_投递失败不推进基线 验证两段式契约：没调 Commit 基线就不动。
//
// PendingText 只算增量、不推进；只有调用方把文本真正投递出去之后才调
// Commit。投递失败时调用方不调，下一轮 PendingText 必须重新吐出同样的内容
// ——否则那句话就永久丢了。
//
// Python: pending_text / commit 两段式（team_context.py L227-259 / L659-671）
func TestCommit_投递失败不推进基线(t *testing.T) {
	sess := newFakeSession("s1")
	tr := &TeamContextTracker{
		memberName:  "alice",
		displayName: "Alice",
		role:        atschema.TeamRoleLeader,
		language:    "cn",
	}

	first, ok := tr.PendingText(sess)
	if !ok || first == "" {
		t.Fatal("首轮应有待投文本")
	}
	// 模拟「投递失败」：调用方不调 Commit

	second, ok := tr.PendingText(sess)
	if !ok || second == "" {
		t.Fatal("基线未推进，第二轮应重新算出同样的内容")
	}
	if second != first {
		t.Fatalf("两轮内容应一致：first=%q second=%q", first, second)
	}
}

// TestCommit_调Commit后推进基线 验证正常路径：Commit 之后不再重投。
func TestCommit_调Commit后推进基线(t *testing.T) {
	sess := newFakeSession("s1")
	tr := &TeamContextTracker{
		memberName:  "alice",
		displayName: "Alice",
		role:        atschema.TeamRoleLeader,
		language:    "cn",
	}

	first, ok := tr.PendingText(sess)
	if !ok || first == "" {
		t.Fatal("首轮应有待投文本")
	}
	tr.Commit(sess, nil)

	if again, has := tr.PendingText(sess); has {
		t.Fatalf("Commit 后不应再重投，实际：%s", again)
	}
}

// TestTaskOutcomeTracker_Wakes假 评审通过通知不得唤醒作者（F-119）。
func TestTaskOutcomeTracker_Wakes假(t *testing.T) {
	tr := newTaskOutcomeTracker(nil, "alice")
	require.NotNil(t, tr)

	assert.False(t, tr.Wakes(), "scheduled 模式的通知不得唤醒")
}

// TestTaskRevisionTracker_Wakes真 自主模式的任务编辑通知可唤醒（F-129）。
func TestTaskRevisionTracker_Wakes真(t *testing.T) {
	tr := newTaskRevisionTracker(nil, "alice")
	require.NotNil(t, tr)

	assert.True(t, tr.Wakes(), "autonomous 模式的通知可唤醒")
}

// TestAnnounce_wakes为假不发送 Wakes=false 时 Claim 与 send 都不发生。
func TestAnnounce_wakes为假不发送(t *testing.T) {
	tr := newTaskOutcomeTracker(nil, "alice")
	require.NotNil(t, tr)

	sent := false
	ok, err := tr.Announce(nil, func(string) error {
		sent = true
		return nil
	})

	require.NoError(t, err)
	assert.False(t, ok, "不应发出")
	assert.False(t, sent, "send 不应被调用")
}

// TestUnimplementedTaskEventTracker_空实现 测试替身基类的五个方法都安全。
func TestUnimplementedTaskEventTracker_空实现(t *testing.T) {
	var tr UnimplementedTaskEventTracker
	assert.Nil(t, tr.Claim(nil))
	tr.Persist(nil)
	ok, err := tr.Announce(nil, func(string) error { return nil })
	assert.False(t, ok)
	assert.NoError(t, err)
	assert.False(t, tr.Wakes())
	tr.Release(nil)
}

// TestTaskEventTracker_接口满足 两个生产实现都满足扩张后的接口。
func TestTaskEventTracker_接口满足(t *testing.T) {
	var _ TaskEventTracker = (*TaskOutcomeTracker)(nil)
	var _ TaskEventTracker = (*TaskRevisionTracker)(nil)
	var _ TaskEventTracker = UnimplementedTaskEventTracker{}
}
