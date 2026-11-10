//go:build integration

package adapter_test

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// CommandContextDefaultModeSuite command.context 不带 mode 的缺省解析链路。
//
// 覆盖 runtime/request.go 的「mode == "" → agent」分支：请求由 AgentServer 入站
// 分发到 handleCommandContext → ApplyResolvedModeToRequest → AgentManager.GetAgent
// 真装配。该分支此前靠 .integration-coverage-exempt 的行级豁免放过，且豁免区间
// 已漂移到 agent_manager.go 的 GetChatAgent（2026-11-05 偏差 7）。
type CommandContextDefaultModeSuite struct {
	isuite.BaseIntegrationSuite
	// env 进程内 AgentServer 环境
	env *testhelpers.ServerEnv
}

// TestCommandContextDefaultModeSuite 套件入口。
func TestCommandContextDefaultModeSuite(t *testing.T) {
	suite.Run(t, new(CommandContextDefaultModeSuite))
}

// SetupSuite 启动进程内 AgentServer。
func (s *CommandContextDefaultModeSuite) SetupSuite() {
	s.BaseIntegrationSuite.SetupSuite()
	cfg, err := config.New("")
	s.Require().NoError(err)
	s.env = testhelpers.NewServerEnv(s.T(), cfg, nil)
}

// TestCommandContextWithoutModeUsesAgent 验证不带 mode 的 command.context 走 agent 而非 plan。
func (s *CommandContextDefaultModeSuite) TestCommandContextWithoutModeUsesAgent() {
	// 非 TUI 创建必须带 create_token 认领会话（对齐 Python SessionProvisioner.PrepareSessionCreate）
	createID := s.env.Send(string(schema.ReqMethodSessionCreate), map[string]any{
		"create_token": "tok-ctx-default-mode",
	}, "", false)
	created := s.env.AwaitUnary(createID)
	s.Require().NotNil(created)
	s.Require().True(created.OK, "session.create 应成功: %+v", created.Payload)
	sessionID, _ := created.Payload["session_id"].(string)
	s.Require().NotEmpty(sessionID, "session.create 应返回 session_id")

	// params 刻意不带 mode：命中 runtime/request.go 的缺省分支
	ctxID := s.env.Send(string(schema.ReqMethodCommandContext), map[string]any{}, sessionID, false)
	resp := s.env.AwaitUnary(ctxID)
	s.Require().NotNil(resp, "command.context 应有响应")
	s.True(resp.OK, "command.context 应成功: %+v", resp.Payload)

	_, hasTotal := resp.Payload["total_tokens"]
	_, hasLimit := resp.Payload["context_window_limit"]
	s.True(hasTotal || hasLimit, "payload 应含上下文占用字段: %+v", resp.Payload)

	// 缺省 mode 必须不是 plan
	planID := s.env.Send(string(schema.ReqMethodSessionPlanStatus), map[string]any{"session_id": sessionID}, sessionID, false)
	planResp := s.env.AwaitUnary(planID)
	s.Require().NotNil(planResp)
	mode, _ := planResp.Payload["mode"].(string)
	s.NotEqual("plan", mode, "缺省轮次不应进入 plan")
}
