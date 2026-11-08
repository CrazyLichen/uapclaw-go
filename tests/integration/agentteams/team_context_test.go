//go:build integration

package agentteams_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/inboundtags"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/prompts"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tracker"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamContextSuite 团队状态车道与建队结果集成测试套件。
//
// 跨 agent_teams/tools（真 TeamBackend + 真数据库）、agent_teams/tracker
// （状态决策层）、agentcore/session（会话状态持久化）与 agent_teams/prompts
// （静态 section 组装）四个子系统，验证 25.0c 的增量宣告语义与 25.0g 的
// 政策块通道 / 建队结果披露。
type TeamContextSuite struct {
	isuite.AgentSuite
	// backend 本轮的真团队后端
	backend *tools.TeamBackend
	// db 真数据库（内存实现，状态机契约与 SQL 实现一致）
	db database.TeamDatabase
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// newSession 建一个带内存 checkpointer 的会话。
func (s *TeamContextSuite) newSession(id string) sessioninterfaces.SessionFacade {
	return s.NewTestSession(id)
}

// newTracker 建一个只走身份/团队信息/名册通道的状态决策器。
func (s *TeamContextSuite) newTracker() *tracker.TeamContextTracker {
	return tracker.NewTeamContextTracker(
		s.backend, "leader1", atschema.TeamRoleLeader, "Leader",
		"", "", "", "", false, "cn", "", nil,
	)
}

// newTrackerWithPolicy 建一个带 <team-policy> 块的状态决策器。
func (s *TeamContextSuite) newTrackerWithPolicy(policy *string) *tracker.TeamContextTracker {
	return tracker.NewTeamContextTracker(
		s.backend, "leader1", atschema.TeamRoleLeader, "Leader",
		"", "", "", "", false, "cn", "", policy,
	)
}

// buildTool 取出 build_team 的可调用实例。
//
// CreateTeamTools 返回的工具都被 WrapInvokeWithLogging 包成 loggedTeamTool，
// 按 ToolCard 名挑出来做 Invoke 即可——这是生产真实链路：包装器在 Invoke
// 成功后会调用内部的 MapResult，把渲染结果写进 result[MappedContentKey]。
func (s *TeamContextSuite) buildTool() tool.Tool {
	opts := tools.TeamToolOptions{
		Role: "leader", TeammateMode: "build_mode", Lifecycle: "temporary",
		Language: "cn", DispatchMode: "autonomous", TeamMode: "default",
	}
	for _, tl := range tools.CreateTeamTools(s.backend, opts, nil, nil, nil) {
		if card := tl.Card(); card != nil && card.Name == "build_team" {
			return tl
		}
	}
	s.Require().Fail("工具集里应有 build_team")
	return nil
}

// mappedText 取出包装器注入的渲染文本（生产上供 BuildToolMessageContent 用）。
func (s *TeamContextSuite) mappedText(out map[string]any) string {
	v, ok := out[tools.MappedContentKey]
	s.Require().True(ok, "Invoke 结果应含 %s", tools.MappedContentKey)
	text, ok := v.(string)
	s.Require().True(ok, "%s 应是 string", tools.MappedContentKey)
	return text
}

// SetupTest 每例新建独立团队后端与数据库。
//
// AgentSuite 本身不提供 SetupTest 钩子（其 TearDownTest 由 testify 直接调），
// 这里只做本套件需要的隔离：新库 + 新后端。
func (s *TeamContextSuite) SetupTest() {
	s.db = database.NewInMemoryTeamDatabase()
	s.backend = tools.NewTeamBackend(
		"it_team", "leader1", true, s.db, nil,
		tools.WithLeaderMemberName("leader1"),
		tools.WithDispatchMode("autonomous"),
	)
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestTeamContextSuite 套件入口。
func TestTeamContextSuite(t *testing.T) {
	suite.Run(t, new(TeamContextSuite))
}

// TestTeamContext_首投全量与零新增 验证一轮投递后二轮零新增。
//
// 首投是全量快照（history 里还没有任何 team 状态），第二轮无变更时
// PendingText 必须返回空——否则每轮都会往历史里再塞一遍同样的内容。
func (s *TeamContextSuite) TestTeamContext_首投全量与零新增() {
	ctx := context.Background()
	_, err := s.backend.BuildTeam(ctx, "IT Team", "desc", "Leader", "ld", nil, nil)
	s.Require().NoError(err)

	sess := s.newSession("it-sess-1")
	tr := s.newTracker()

	first, ok := tr.PendingText(sess)
	s.Require().True(ok, "首轮应有待投文本")
	s.Require().Contains(first, "<team-context>", "首投应是全量快照")
	tr.Commit(sess, nil)

	second, hasSecond := tr.PendingText(sess)
	s.False(hasSecond, "无变更时第二轮不应有新内容，实际：%s", second)
}

// TestTeamContext_名册增量 验证 spawn 新成员后只播增量。
func (s *TeamContextSuite) TestTeamContext_名册增量() {
	ctx := context.Background()
	_, err := s.backend.BuildTeam(ctx, "IT Team", "desc", "Leader", "ld", nil, nil)
	s.Require().NoError(err)

	sess := s.newSession("it-sess-2")
	tr := s.newTracker()
	_, _ = tr.PendingText(sess)
	tr.Commit(sess, nil)

	// roster 探针是 MAX(member.updated_at)，精度到毫秒：同一毫秒内 spawn
	// 的新成员不会让 max 变化，探测不到「名册变了」。测试里睡一毫秒避开。
	time.Sleep(2 * time.Millisecond)
	card := agentschema.NewAgentCard(
		agentschema.WithAgentID("it_team_bob"),
		agentschema.WithAgentName("Bob"),
		agentschema.WithAgentDescription("worker"),
	)
	result := s.backend.SpawnMember(ctx, "bob", "Bob", card, string(atschema.TeamRoleTeammate), "desc", "", "")
	s.Require().True(result.OK, "SpawnMember 应成功")

	delta, ok := tr.PendingText(sess)
	s.Require().True(ok, "名册变化后应有增量")
	s.Require().Contains(delta, "<team-event", "名册变化应走 <team-event>")
	s.Require().Contains(delta, "bob", "增量应含新成员")
}

// TestTeamContext_policy通道 验证 framework 建队时 <team-policy> 随首投到达。
//
// 这是 25.0c 偏差 D-3 的端到端验证：25.0c 时 policyText 恒 nil，通道关闭；
// 25.0g 在构造期渲染披露文本并交给 tracker，首轮状态里应出现 <team-policy>。
//
// Python: team_policy_rail.py L204-218 + team_context.py 的 policy 频道
func (s *TeamContextSuite) TestTeamContext_policy通道() {
	ctx := context.Background()
	_, err := s.backend.BuildTeam(ctx, "IT Team", "desc", "Leader", "ld", nil, nil)
	s.Require().NoError(err)

	// framework 建队：构造期算好的披露文本包成 <team-policy>
	disclosure, err := prompts.BuildLeaderPolicyDisclosure(
		"temporary", "build_mode", "default", "scheduled", false, false, "cn", false,
	)
	s.Require().NoError(err)
	policyBlock := inboundtags.RenderTeamPolicyInline(disclosure)

	sess := s.newSession("it-sess-3")
	tr := s.newTrackerWithPolicy(&policyBlock)

	first, ok := tr.PendingText(sess)
	s.Require().True(ok)
	s.Require().Contains(first, "<team-policy>", "首投应含政策块")
	tr.Commit(sess, nil)

	// 政策文本未变 → 第二轮不应重投（sha256 门）
	second, hasSecond := tr.PendingText(sess)
	s.False(hasSecond, "政策未变时第二轮不应重投，实际：%s", second)
}

// TestBuildTeam_验证开关回带 验证 team 行的 enable_task_verification 流到结果文本。
//
// 端到端链路：BuildTeam 计算生效值 → 落 team_info 行 → outcome 携带 →
// BuildTeamTool.Invoke 塞进 data → MapResult 回带 task_verification=。
func (s *TeamContextSuite) TestBuildTeam_验证开关回带() {
	ctx := context.Background()
	s.backend = tools.NewTeamBackend(
		"it_team", "leader1", true, s.db, nil,
		tools.WithLeaderMemberName("leader1"),
		tools.WithSpecEnableTaskVerification(true),
	)

	enableVerify := true
	outcome, err := s.backend.BuildTeam(ctx, "IT Team", "desc", "Leader", "ld", nil, &enableVerify)
	s.Require().NoError(err)
	s.Require().True(outcome.EnableTaskVerification, "spec 天花板开启时生效值应为 true")
	s.Require().True(s.backend.EnableTaskVerification(), "后端生效值应已写入")

	invoker := s.buildTool()
	out, err := invoker.Invoke(ctx, map[string]any{
		"display_name": "IT Team", "team_desc": "desc",
		"leader_display_name": "Leader", "leader_desc": "ld",
	})
	s.Require().NoError(err)
	// 第二次调用走接管，仍应回带 task_verification
	text := s.mappedText(out)
	s.Contains(text, "task_verification=true", "结果应回带生效的验证开关")
	s.Contains(text, "Existing team taken over", "第二次调用应为接管首行")
}

// TestBuildTeam_接管路径 验证二次调用走接管。
func (s *TeamContextSuite) TestBuildTeam_接管路径() {
	ctx := context.Background()
	first, err := s.backend.BuildTeam(ctx, "IT Team", "desc", "Leader", "ld", nil, nil)
	s.Require().NoError(err)
	s.False(first.TakenOver)

	second, err := s.backend.BuildTeam(ctx, "IT Team", "desc", "Leader", "ld", nil, nil)
	s.Require().NoError(err)
	s.True(second.TakenOver, "第二次调用应接管")
}

// TestBuildTeam_结果含政策披露 验证 MapResult 追加披露正文。
func (s *TeamContextSuite) TestBuildTeam_结果含政策披露() {
	ctx := context.Background()
	invoker := s.buildTool()

	out, err := invoker.Invoke(ctx, map[string]any{
		"display_name": "IT Team", "team_desc": "desc",
		"leader_display_name": "Leader", "leader_desc": "ld",
	})
	s.Require().NoError(err)
	text := s.mappedText(out)
	s.Contains(text, "Team created:")
	s.Contains(text, "任务下发", "结果应追加政策披露")
}
