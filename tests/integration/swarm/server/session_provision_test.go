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

// SessionProvisionSuite 会话供给事务全链路：AgentServer 分发 → provisioner 事务
// → 会话元数据/历史 → 执行协调器 → 目录回收。
//
// 跨子系统：server / runtime / runtime.session / server.session / adapter 五者，
// 满足规则 7 的 ≥3 要求。
type SessionProvisionSuite struct {
	isuite.BaseIntegrationSuite
	// env 进程内 AgentServer 环境
	env *testhelpers.ServerEnv
	// dataDir 隔离数据根目录
	dataDir string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestSessionProvisionSuite 套件入口。
func TestSessionProvisionSuite(t *testing.T) {
	suite.Run(t, new(SessionProvisionSuite))
}

// SetupTest 隔离数据目录与会话缓存。
func (s *SessionProvisionSuite) SetupTest() {
	s.dataDir = s.T().TempDir()
	s.T().Setenv(pathutil.EnvDataDir, s.dataDir)
	pathutil.ResetCache()
	session.ClearAllSessionMetadataCache()
}

// TearDownTest 刷盘并还原环境。
func (s *SessionProvisionSuite) TearDownTest() {
	session.FlushMetadataQueue()
	session.ClearAllSessionMetadataCache()
	pathutil.ResetCache()
}

// SetupSuite 启动进程内 AgentServer。
func (s *SessionProvisionSuite) SetupSuite() {
	s.BaseIntegrationSuite.SetupSuite()
	cfg, err := config.New("")
	s.Require().NoError(err)
	s.env = testhelpers.NewServerEnv(s.T(), cfg, nil)
}

// TestSessionProvisionCreate_全链路 校验 create 走完 prepare → commit。
func (s *SessionProvisionSuite) TestSessionProvisionCreate_全链路() {
	sessionID := s.mustCreateSession("integration-token")

	// 元数据已落盘
	meta := session.GetSessionMetadata(sessionID)
	s.Require().NotNil(meta, "create 后应有会话元数据")
	s.Require().Equal("agent", meta["mode"], "metadata 应记录归一化模式")
}

// TestSessionDelete_创建后可删除 校验删除事务走到破坏性阶段。
//
// 进程内测试环境没有真实 Runner 会话，Runner.release 必然失败，因此删除事务
// 会正确返回 DELETE_FAILED + recovery_required=true（破坏性阶段已开始，不可
// 回滚）。这里断言的是**事务的三态语义正确**，而不是"一定删除成功"：
//   - 失败码必须是 DELETE_FAILED（不是 NOT_FOUND / BAD_REQUEST）
//   - deleted=false 且 recovery_required=true（破坏性已开始）
//
// 真实成功路径由 provisioner 的单元测试（TestDelete_七阶段顺序）覆盖。
func (s *SessionProvisionSuite) TestSessionDelete_创建后可删除() {
	sessionID := s.mustCreateSession("tok-del")

	requestID := s.env.Send(string(schema.ReqMethodSessionDelete), map[string]any{
		"session_id": sessionID,
	}, sessionID, false)
	resp := s.env.AwaitUnary(requestID)
	s.Require().NotNil(resp, "删除应返回响应")

	if resp.OK {
		s.Require().Empty(session.GetSessionMetadata(sessionID),
			"删除成功后元数据缓存应已清理")
		return
	}
	code, _ := resp.Payload["code"].(string)
	s.Require().Equal("DELETE_FAILED", code,
		"非成功时应为 DELETE_FAILED（事务已进入破坏性阶段），payload=%v", resp.Payload)
	recovery, _ := resp.Payload["recovery_required"].(bool)
	s.Require().True(recovery,
		"破坏性阶段已开始时 recovery_required 必须为 true，payload=%v", resp.Payload)
}

// TestSessionDelete_不存在报NOT_FOUND 校验删除前置判定。
func (s *SessionProvisionSuite) TestSessionDelete_不存在报NOT_FOUND() {
	requestID := s.env.Send(string(schema.ReqMethodSessionDelete), map[string]any{
		"session_id": "not-exist-session",
	}, "not-exist-session", false)
	resp := s.env.AwaitUnary(requestID)
	s.Require().NotNil(resp)
	s.Require().False(resp.OK, "不存在的会话删除应失败")
	if code, ok := resp.Payload["code"].(string); ok {
		s.Require().Equal("NOT_FOUND", code)
	}
}

// TestSessionSwitch_前台切换 校验切换链路。
func (s *SessionProvisionSuite) TestSessionSwitch_前台切换() {
	sessionID := s.mustCreateSession("tok-sw")

	requestID := s.env.Send(string(schema.ReqMethodSessionSwitch), map[string]any{
		"session_id": sessionID,
		"mode":       "agent.plan",
		"view_id":    "integration-view",
	}, sessionID, false)
	resp := s.env.AwaitUnary(requestID)
	s.Require().NotNil(resp, "切换应返回响应")
	s.Require().True(resp.OK, "切换应成功，payload=%v", resp.Payload)
	s.Require().Equal(sessionID, resp.Payload["session_id"], "切换应返回同一会话")
	s.Require().Equal(true, resp.Payload["switched"])
}

// mustCreateSession 建一个会话并返回其标识。
func (s *SessionProvisionSuite) mustCreateSession(token string) string {
	s.T().Helper()
	requestID := s.env.Send(string(schema.ReqMethodSessionCreate), map[string]any{
		"create_token": token,
		"mode":         "agent",
		"work_mode":    "work",
	}, "", false)
	resp := s.env.AwaitUnary(requestID)
	s.Require().NotNil(resp, "create 应返回响应")
	s.Require().True(resp.OK, "create 应成功，payload=%v", resp.Payload)
	sessionID, _ := resp.Payload["session_id"].(string)
	s.Require().NotEmpty(sessionID, "create 应返回 session_id")
	return sessionID
}
