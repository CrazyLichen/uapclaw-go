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

// GoalDispatchSuite command.goal 的分发契约：从 AgentServer 入口投递，
// 验证它不被误当普通 chat 轮（不写 user 历史、不调模型），以及未知 action
// 的结构化报错。
//
// 跨子系统：swarm/server（信封分发）+ swarm/runtime（prepareTurn / 门面短路）
// + swarm/server/session（历史落盘）。
type GoalDispatchSuite struct {
	isuite.BaseIntegrationSuite
	// env 进程内 AgentServer 环境（E2A 信封投递 + 帧收集）
	env *testhelpers.ServerEnv
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestGoalDispatchSuite 套件入口。
func TestGoalDispatchSuite(t *testing.T) {
	suite.Run(t, new(GoalDispatchSuite))
}

// SetupSuite 启动进程内 AgentServer。
func (s *GoalDispatchSuite) SetupSuite() {
	s.BaseIntegrationSuite.SetupSuite()
	cfg, err := config.New("")
	s.Require().NoError(err)
	s.env = testhelpers.NewServerEnv(s.T(), cfg, nil)
}

// SetupTest 隔离数据目录与会话缓存。
func (s *GoalDispatchSuite) SetupTest() {
	s.T().Setenv(pathutil.EnvDataDir, s.T().TempDir())
	pathutil.ResetCache()
	session.ClearAllSessionMetadataCache()
}

// TearDownTest 刷盘并还原环境，避免污染后续用例。
func (s *GoalDispatchSuite) TearDownTest() {
	session.FlushMetadataQueue(5.0)
	session.ClearAllSessionMetadataCache()
	pathutil.ResetCache()
}

// TestCommandGoalGet_不写user历史且不调模型 只读查询的完整契约。
// Python: interface.py:3408 + service.py:1848（sync_metadata=False）
func (s *GoalDispatchSuite) TestCommandGoalGet_不写user历史且不调模型() {
	sid := "goal-dispatch-get"
	rid := s.env.Send(string(schema.ReqMethodCommandGoal),
		map[string]any{"action": "get", "mode": "agent"}, sid, false)
	resp := s.env.AwaitUnary(rid)
	s.Require().NotNil(resp)

	// 空会话的 get 是正常态：Python interface_deep.py:17563 返回
	// "No goal in this session."，不是错误。真正的契约是「不写 user 轮历史、
	// 不调模型」——本环境未注入模型，一旦走到模型就会失败。
	s.True(resp.OK, "空会话 get 属正常态")
	s.Equal("No goal in this session.", resp.Payload["message"])

	// 历史必须为空（未写 user 轮）
	records, err := session.ReadHistoryRecords(sid)
	s.Require().NoError(err)
	s.Empty(records, "只读 goal 查询不得写 user 轮历史")
}

// TestCommandGoalUnknownAction_返回invalid_action 未知 action 不 panic。
// Python: interface_deep.py:17900
func (s *GoalDispatchSuite) TestCommandGoalUnknownAction_返回invalid_action() {
	sid := "goal-dispatch-bad"
	rid := s.env.Send(string(schema.ReqMethodCommandGoal),
		map[string]any{"action": "explode", "mode": "agent"}, sid, false)
	resp := s.env.AwaitUnary(rid)
	s.Require().NotNil(resp)
	s.False(resp.OK, "未知 action 必须失败")
	s.Equal("invalid_action", resp.Payload["code"])
}
