//go:build integration

package attachment_mode

import (
	"testing"

	"github.com/stretchr/testify/suite"

	swruntime "github.com/uapclaw/uapclaw-go/internal/swarm/runtime"
	swarmschema "github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PlanEventSuite 断言 plan 控制事件真正下发到客户端（IT-S3-2/3）。
//
// S3 缺陷 #3：plan_mode_exited 曾只缓冲在 adapter 里、全仓无消费方。
type PlanEventSuite struct {
	isuite.BaseIntegrationSuite
	// env 计划模式测试环境
	env *testhelpers.PlanEnv
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestPlanEventSuite 运行 PlanEventSuite。
func TestPlanEventSuite(t *testing.T) { suite.Run(t, new(PlanEventSuite)) }

// TestIT_S3_2_退出事件下发_unary 退出 plan 后客户端收到 plan_mode_exited 帧。
//
// 跨子系统：runtime（check_post_process_exit）+ server（push 帧）+ session。
func (s *PlanEventSuite) TestIT_S3_2_退出事件下发_unary() {
	s.env.Send(itSession, planMode, "计划", map[string]any{"plan_entry_source": "plan_toggle"})
	s.Equal("plan", s.env.PlanMode(itSession))

	s.env.Send(itSession, normalMode, "退出", nil)

	payload := s.env.Server.LastPushPayload()
	s.Require().NotNil(payload, "退出 plan 必须下发 push 帧")
	s.Equal(swruntime.PlanModeExitedEvent, payload["event_type"])
	s.Equal(normalMode, payload["mode"])
}

// TestIT_S3_2_退出事件下发_stream 流式轮次的退出事件同样走 push（不占 sequence）。
func (s *PlanEventSuite) TestIT_S3_2_退出事件下发_stream() {
	s.env.SendStream(itSession, planMode, "计划", map[string]any{"plan_entry_source": "plan_toggle"})
	s.Equal("plan", s.env.PlanMode(itSession))

	chunks := s.env.SendStream(itSession, normalMode, "退出", nil)
	s.NotEmpty(chunks, "流式轮次必须有 chunk")
	s.True(s.env.HasEvent(itSession, swruntime.PlanModeExitedEvent), "流式轮次也必须下发退出事件")
}

// TestIT_S3_3_plan_status与后端一致 session.plan_status 返回值与后端 plan 状态一致。
//
// 跨子系统：runtime（metadata 同步）+ server（只读查询）+ session。
func (s *PlanEventSuite) TestIT_S3_3_plan_status与后端一致() {
	s.env.Send(itSession, planMode, "计划", map[string]any{"plan_entry_source": "plan_toggle"})
	status := s.planStatus()
	s.Equal(itSession, status["session_id"])
	s.Equal(true, status["in_plan"], "进入 plan 后 session.plan_status 应为 true")

	s.env.Send(itSession, normalMode, "退出", nil)
	s.Equal(false, s.planStatus()["in_plan"], "退出 plan 后 session.plan_status 应为 false")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// SetupTest 每例隔离环境。
func (s *PlanEventSuite) SetupTest() {
	s.env = testhelpers.NewPlanEnv(s.T(), s.MockLLM, "mock_s3_events")
}

// planStatus 调用 session.plan_status 并返回 payload。
func (s *PlanEventSuite) planStatus() map[string]any {
	s.T().Helper()
	requestID := s.env.Server.Send(
		string(swarmschema.ReqMethodSessionPlanStatus),
		map[string]any{"session_id": itSession},
		itSession, false,
	)
	resp := s.env.Server.AwaitUnary(requestID)
	s.Require().NotNil(resp)
	s.Require().True(resp.OK, "plan_status 不应失败")
	return resp.Payload
}
