//go:build integration

package server_test

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionLifecycleSuite 会话生命周期链路：AgentServer 入口 → session 元数据
// → plan 状态清理。
//
// 覆盖删除会话时服务端持有控制器的 plan 会话记忆清理（S3 把 controller 归属从
// adapter 上移到 AgentServer 后，删除路径改调 planController.ResetSession）。
type SessionLifecycleSuite struct {
	isuite.BaseIntegrationSuite
	// env 进程内 AgentServer 环境（E2A 信封投递 + 帧收集）
	env *testhelpers.ServerEnv
	// dataDir 本用例隔离的数据根目录
	dataDir string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestSessionLifecycleSuite 套件入口。
func TestSessionLifecycleSuite(t *testing.T) {
	suite.Run(t, new(SessionLifecycleSuite))
}

// SetupTest 隔离数据目录与会话缓存。
func (s *SessionLifecycleSuite) SetupTest() {
	s.dataDir = s.T().TempDir()
	s.T().Setenv(pathutil.EnvDataDir, s.dataDir)
	pathutil.ResetCache()
	session.ClearAllSessionMetadataCache()
}

// TearDownTest 刷盘并还原环境，避免污染后续用例。
func (s *SessionLifecycleSuite) TearDownTest() {
	session.FlushMetadataQueue()
	session.ClearAllSessionMetadataCache()
	pathutil.ResetCache()
}

// SetupSuite 启动进程内 AgentServer。
func (s *SessionLifecycleSuite) SetupSuite() {
	s.BaseIntegrationSuite.SetupSuite()
	cfg, err := config.New("")
	s.Require().NoError(err)
	s.env = testhelpers.NewServerEnv(s.T(), cfg, nil)
}

// TestDeleteSessionResetsPlanState 创建 → 查询 plan 状态 → 删除会话，全链路无错误。
func (s *SessionLifecycleSuite) TestDeleteSessionResetsPlanState() {
	// 非 TUI 创建必须带 create_token 认领会话（对齐 Python：见
	// SessionProvisioner.PrepareSessionCreate 第 15 步，缺省报 create_token is required）
	createID := s.env.Send(string(schema.ReqMethodSessionCreate), map[string]any{
		"create_token": "tok-lifecycle",
	}, "", false)
	created := s.env.AwaitUnary(createID)
	s.Require().NotNil(created)
	s.Require().True(created.OK, "session.create 应成功: %+v", created.Payload)
	sessionID, _ := created.Payload["session_id"].(string)
	s.Require().NotEmpty(sessionID, "session.create 应返回 session_id")

	statusID := s.env.Send(string(schema.ReqMethodSessionPlanStatus), map[string]any{"session_id": sessionID}, sessionID, false)
	status := s.env.AwaitUnary(statusID)
	s.Require().NotNil(status)
	s.Require().True(status.OK, "session.plan_status 应成功: %+v", status.Payload)
	s.Require().Equal(false, status.Payload["in_plan"], "新会话不应处于计划模式")

	deleteID := s.env.Send(string(schema.ReqMethodSessionDelete), map[string]any{"session_id": sessionID}, sessionID, false)
	deleted := s.env.AwaitUnary(deleteID)
	s.Require().NotNil(deleted)
	s.Require().True(deleted.OK, "session.delete 应成功: %+v", deleted.Payload)
}
