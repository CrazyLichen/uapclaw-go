package tracker

import (
	"context"
	"strings"
	"testing"
	"time"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	sessionstate "github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
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
	_ = tb.DB().Team().CreateTeam(ctx, "test-team", "测试团队", "leader", "目标描述", "")
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
