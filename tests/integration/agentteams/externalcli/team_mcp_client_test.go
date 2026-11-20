//go:build integration

package externalcli_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/external"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/messager"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/spawn"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamMcpClientSuite ExternalTeamClient + team-mcp server 面 + 共享 DB 工厂
// 的跨进程缝集成套件（15.16b）。
//
// 跨子系统：agent_teams/external（ExternalTeamClient connect +
// member scope 真团队工具）→ agent_teams/tools（TeamBackend / Task /
// MessageManager 经 CreateTeamTools 构建的同一批 TeamTool 实例）→
// agent_teams/spawn（GetSharedDB 共享数据库工厂缝）→ agent_teams/messager
// （inprocess 传输 + hybrid publish-only 门卫）→ agent_teams/external
// （read_inbox 渲染）。
type TeamMcpClientSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestTeamMcpClientSuite 套件入口（审计硬要求：suite.Run 字面调用）。
func TestTeamMcpClientSuite(t *testing.T) {
	suite.Run(t, new(TeamMcpClientSuite))
}

// ──────────────────────────── 测试 ────────────────────────────

// memDBConfig 指向内存数据库的描述符 DB 配置。
type memDBConfig struct{}

// GetDBType 内存数据库。
func (memDBConfig) GetDBType() database.DatabaseType { return database.DatabaseTypeMemory }

// GetConnectionString 空串。
func (memDBConfig) GetConnectionString() string { return "" }

// ensureMemberRow 在共享 DB 里登记成员行（Python 由 leader build_team 写入；
// 外部 client 用例直接落行以让 claim 的成员校验成立）。
func (s *TeamMcpClientSuite) ensureMemberRow(memberName string) {
	db := spawn.GetSharedDB(memDBConfig{})
	s.Require().NotNil(db)
	db.Member().CreateMember(context.Background(), memberName, "team-it",
		memberName, "{}", "ready", "teammate", "", "", "member", "", "{}")
}

// newClient 构建已连接的 member scope 客户端（共享 DB 工厂 → 内存库）。
func (s *TeamMcpClientSuite) newClient(ctx context.Context, memberName string) *external.ExternalTeamClient {
	external.SetSharedDBFactory(func(config database.DBConfigProvider) database.TeamDatabase {
		return spawn.GetSharedDB(config)
	})
	descriptor := &external.TeamJoinDescriptor{
		// 同团队全部成员共享同一会话 id（Python descriptor.session_id 同源）
		SessionID:       "sess-it-team",
		TeamName:        "team-it",
		MemberName:      memberName,
		Role:            "teammate",
		Scope:           "member",
		Language:        "cn",
		DispatchMode:    "autonomous",
		TeammateMode:    "build_mode",
		DBConfig:        memDBConfig{},
		TransportConfig: &atschema.MessagerTransportConfig{Backend: "inprocess"},
	}
	client := external.NewExternalTeamClient(descriptor)
	s.Require().NoError(client.Connect(ctx))
	return client
}

// TestMemberScopeConnect_真团队工具集 member scope connect 后拿到与
// teammate 相同的 TeamTool 面（view_task/claim_task/send_message），
// operator 专属 read_inbox 被排除。
func (s *TeamMcpClientSuite) TestMemberScopeConnect_真团队工具集() {
	ctx := context.Background()
	client := s.newClient(ctx, "dev")
	defer client.Close(ctx)

	tools := client.Tools()
	s.Require().NotEmpty(tools)
	for _, name := range []string{"view_task", "claim_task", "send_message"} {
		s.Require().Contains(tools, name, "member scope 应含真团队工具 %s", name)
	}
	s.Assert().NotContains(tools, "read_inbox", "read_inbox 是 operator 专属")
	s.Assert().NotContains(tools, "checkpoint", "checkpoint 被显式排除")
	// 卡片投影面（server list_tools 的源）：name 非空、InputParams 就位
	for name, tl := range tools {
		card := tl.Card()
		s.Require().NotNil(card)
		s.Assert().Equal(name, card.Name)
	}
}

// TestSendMessage_共享DB真写直发消息 send_message 经 TeamMessageManager
// 写共享数据库并发布总线事件；同库第二成员的收件箱能看到。
func (s *TeamMcpClientSuite) TestSendMessage_共享DB真写直发消息() {
	ctx := context.Background()
	dev := s.newClient(ctx, "dev")
	defer dev.Close(ctx)
	peer := s.newClient(ctx, "peer")
	defer peer.Close(ctx)

	messageID, err := dev.SendMessage(ctx, "peer", "你好，认领 t1")
	s.Require().NoError(err)
	s.Require().NotNil(messageID)
	s.Assert().NotEqual("", *messageID)

	// 广播
	broadcastID, err := dev.SendMessage(ctx, "*", "全员注意")
	s.Require().NoError(err)
	s.Assert().NotNil(broadcastID)

	// peer 的收件箱（operator 面经 ReadInbox 渲染）可见两条未读
	peer2 := s.newClient(ctx, "peer")
	defer peer2.Close(ctx)
	view, err := peer2.FetchInbox(ctx, false)
	s.Require().NoError(err)
	s.Assert().Len(view.Messages, 2, "直发+广播各一条")

	text, err := peer2.ReadInbox(ctx, false)
	s.Require().NoError(err)
	s.Assert().Contains(text, "你好，认领 t1")
	s.Assert().Contains(text, "全员注意")
}

// TestTaskOps_共享任务板 创建→可认领→认领→完成全链路走
// TeamTaskManager（与进程内 leader 同一管理器）。
func (s *TeamMcpClientSuite) TestTaskOps_共享任务板() {
	ctx := context.Background()
	s.ensureMemberRow("dev")
	op := s.newClient(ctx, "operator")
	defer op.Close(ctx)
	dev := s.newClient(ctx, "dev")
	defer dev.Close(ctx)

	taskID := "task-it-1"
	created, err := op.CreateTask(ctx, "t-1", "搭建骨架", &taskID, nil)
	s.Require().NoError(err)
	s.Require().NotNil(created.Task)

	claimable, err := dev.ClaimableTasks(ctx)
	s.Require().NoError(err)
	found := false
	for _, t := range claimable {
		if t.TaskID == "task-it-1" {
			found = true
		}
	}
	s.Assert().True(found, "任务应可被 dev 认领")

	res, err := dev.ClaimTask(ctx, "task-it-1")
	s.Require().NoError(err)
	s.Assert().True(res.OK)

	res, err = dev.CompleteTask(ctx, "task-it-1")
	s.Require().NoError(err)
	s.Assert().True(res.OK)

	detail, err := op.GetTask(ctx, "task-it-1")
	s.Require().NoError(err)
	s.Require().NotNil(detail)
	s.Assert().Equal("completed", detail.Task.Status)
}

// TestListMembers_名册直读 名册直读共享 DB 成员行。
func (s *TeamMcpClientSuite) TestListMembers_名册直读() {
	ctx := context.Background()
	op := s.newClient(ctx, "operator")
	defer op.Close(ctx)
	s.ensureMemberRow("operator")
	members, err := op.ListMembers(ctx)
	// 名册直读共享 DB 成员行（ensureMemberRow 落的行应可见）
	s.Require().NoError(err)
	found := false
	for _, m := range members {
		if m.MemberName == "operator" {
			found = true
		}
	}
	s.Assert().True(found, "operator 行应可见")
}

// TestHybridMessager_PublishOnly门卫 hybrid 后端在进程内可构造，
// subscribe/send 面按 Python NotImplementedError 语义拒绝。
func (s *TeamMcpClientSuite) TestHybridMessager_PublishOnly门卫() {
	m := messager.NewHybridMessager(nil, "member1")
	ctx := context.Background()
	s.Assert().Error(m.Subscribe(ctx, "t", nil))
	s.Assert().Error(m.Send(ctx, "a", nil))
}
