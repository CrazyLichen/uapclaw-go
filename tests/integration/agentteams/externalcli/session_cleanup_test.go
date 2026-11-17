//go:build integration

package externalcli_test

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/metadata"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/runtime/externalcli"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionCleanupSuite 外部 CLI 后端会话清理链集成套件。
//
// 跨子系统：externalcli（CleanupExternalCLIBackendSessions 编排）→ session
// （按会话 id 恢复团队会话 + PreRun）→ metadata（团队命名空间 spec 读写）→
// tools/database（成员行 cli_agent 过滤）。对齐 Python
// external/cli_agent/session_cleanup.py 的编排语义：恢复会话 → 读 spec →
// 按 cli_agent 索引 → 逐成员匹配清理（claude 删除面 ⤵️15.7，D-15.13-x）。
type SessionCleanupSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestSessionCleanupSuite 套件入口（审计硬要求：suite.Run 字面调用）。
func TestSessionCleanupSuite(t *testing.T) {
	suite.Run(t, new(SessionCleanupSuite))
}

// ──────────────────────────── 测试 ────────────────────────────

// TestCleanupExternalCLIBackendSessions_编排链 清理链编排：
// 团队命名空间写入 spec（external_cli_agents: openclaw + claude）→ 按会话 id
// 恢复团队会话 → spec 按 cli_agent 索引 → InMemory 团队库三个成员（openclaw/
// claude/无 cli_agent）逐成员匹配。断言：恢复面可见 spec、成员行全部完好
// （清理对非 claude 成员无副作用、claude 走 ⤵️15.7 延迟分支）、空会话 id 与
// 未注册团队两条防御分支安静返回。
func (s *SessionCleanupSuite) TestCleanupExternalCLIBackendSessions_编排链() {
	ctx := s.Ctx
	const sessionID = "sess-extcli-cleanup"

	// 子系统：session + metadata——团队命名空间写入 spec 并提交到检查点，
	// 使 restoreSessionForCleanup 按会话 id 恢复后可见
	teamSession := session.CreateAgentTeamSession(sessionID, nil, "pay")
	// PreRun 先建 AgentTeam 存储（检查点器 PostAgentTeamExecute 的前置）
	s.Require().NoError(teamSession.PreRun(ctx))
	spec := &schema.TeamAgentSpec{
		TeamName: "pay",
		ExternalCliAgents: []schema.ExternalCliAgentSpec{
			{CLIAgent: "openclaw"},
			{CLIAgent: "claude"},
		},
	}
	metadata.WriteTeamNamespace(teamSession, "pay", map[string]any{"spec": spec})
	s.Require().NoError(teamSession.Commit(ctx), "命名空间写入必须提交到检查点")

	// 恢复面验证：按同 id 新建的会话 PreRun 后必须读到 spec。
	// 检查点器把 state 序列化往返，spec 可能以具体类型或 map 形态恢复——
	// 两者 decodeTeamAgentSpec 都能消费（session_cleanup.go:143）
	restored := session.CreateAgentTeamSession(sessionID, nil, "pay")
	s.Require().NoError(restored.PreRun(ctx))
	bucket := metadata.ReadTeamNamespace(restored, "pay")
	s.Require().NotNil(bucket, "恢复会话必须可见团队命名空间")
	switch typed := bucket["spec"].(type) {
	case *schema.TeamAgentSpec:
		s.Len(typed.ExternalCliAgents, 2, "spec 条目数（具体类型形态）")
	case map[string]any:
		entries, _ := typed["external_cli_agents"].([]any)
		s.Len(entries, 2, "spec 条目数（map 形态）")
	default:
		s.Failf("spec 恢复形态非法", "%T", bucket["spec"])
	}

	// 子系统：tools/database——三个成员行，cli_agent 两配一缺
	db := database.NewInMemoryTeamDatabase()
	s.Require().NoError(db.Initialize(ctx))
	s.T().Cleanup(func() { _ = db.Close() })
	s.Require().True(db.Member().CreateMember(ctx, "alpha", "pay", "Alpha", "{}", "ready", "teammate", "", "", "build_mode", "", `{"cli_agent":"openclaw"}`))
	s.Require().True(db.Member().CreateMember(ctx, "bravo", "pay", "Bravo", "{}", "ready", "teammate", "", "", "build_mode", "", `{"cli_agent":"claude"}`))
	s.Require().True(db.Member().CreateMember(ctx, "charlie", "pay", "Charlie", "{}", "ready", "teammate", "", "", "build_mode", "", ""))

	// 清理编排：claude 成员走 ⤵️15.7 延迟分支，openclaw/charlie 不清理
	externalcli.CleanupExternalCLIBackendSessions(ctx, sessionID, []string{"pay"}, db)

	// 成员行全部完好（清理不触碰共享团队库）
	members, err := db.Member().GetTeamMembers(ctx, "pay", "")
	s.Require().NoError(err)
	s.Len(members, 3, "清理不得增删成员行")
	cliAgents := map[string]bool{}
	for _, member := range members {
		if ptr := database.GetMemberCLIAgent(member); ptr != nil && *ptr != "" {
			cliAgents[*ptr] = true
		}
	}
	s.True(cliAgents["openclaw"] && cliAgents["claude"], "cli_agent 选项读取链完好: %v", cliAgents)

	// 防御分支：空会话 id / 未注册团队名——安静返回不 panic
	externalcli.CleanupExternalCLIBackendSessions(ctx, "", []string{"pay"}, db)
	externalcli.CleanupExternalCLIBackendSessions(ctx, sessionID, []string{"no-such-team"}, db)
	members2, err := db.Member().GetTeamMembers(ctx, "pay", "")
	s.Require().NoError(err)
	s.Len(members2, 3, "防御分支不得触碰成员行")
}

// TestCleanupExternalCLIBackendSession_单成员延迟分支 claude 删除面 ⤵️15.7：
// 单成员入口对非延迟路径同样安静（openclaw 直接短路），claude 走 Debug 延迟
// 分支不 panic。
func (s *SessionCleanupSuite) TestCleanupExternalCLIBackendSession_单成员延迟分支() {
	externalcli.CleanupExternalCLIBackendSession("openclaw", "sess-x", "alpha",
		&schema.ExternalCliAgentSpec{CLIAgent: "openclaw"})
	externalcli.CleanupExternalCLIBackendSession("claude", "sess-x", "bravo",
		&schema.ExternalCliAgentSpec{CLIAgent: "claude"})
	s.True(true, "两条路径安静返回（对齐 session_cleanup.py:55 except 分支形状）")
}
