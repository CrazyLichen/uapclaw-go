//go:build integration

package heartbeat

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/suite"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	heartbeat "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// HeartbeatRailSuite 测试 HeartbeatRail 心跳。
//
// 对齐 Python: tests/unit_tests/harness/test_heartbeat_rail.py
// 注意：HeartbeatRail 未覆盖 GetCallbacks()，回调方法不会自动触发，
// 仅验证 Init 成功和 Invoke 不崩溃。
type HeartbeatRailSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestHeartbeatRailSuite(t *testing.T) {
	suite.Run(t, new(HeartbeatRailSuite))
}

// TestHeartbeatRail_Init成功 测试 HeartbeatRail 成功创建和初始化。
// 对齐 Python: TestHeartbeatRail.test_init_sets_system_prompt_builder ——
// Python 中 HeartbeatRail.init 捕获 system_prompt_builder 和 sys_operation。
func (s *HeartbeatRailSuite) TestHeartbeatRail_Init成功() {
	rail := heartbeat.NewHeartbeatRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("心跳测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// 先 Invoke 触发 ensureInitialized
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "心跳测试"})
	s.Require().NoError(err, "Invoke 不应返回错误")

	// 验证 HeartbeatRail 已注册
	railType := reflect.TypeOf(&heartbeat.HeartbeatRail{})
	foundRails := agent.FindRailsByType(railType)
	s.NotEmpty(foundRails, "应注册 HeartbeatRail")
}

// TestHeartbeatRail_Invoke不崩溃 测试带 HeartbeatRail 的 Invoke 不崩溃。
// 对齐 Python: TestHeartbeatRail.test_before_model_call_skips_non_heartbeat ——
// Python 中 BeforeModelCall 仅在 run_kind == "heartbeat" 时激活，
// 正常 Invoke 不触发，验证不崩溃。
func (s *HeartbeatRailSuite) TestHeartbeatRail_Invoke不崩溃() {
	rail := heartbeat.NewHeartbeatRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("心跳 Invoke 测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	result, err := agent.Invoke(s.Ctx, map[string]any{"query": "心跳测试"})
	s.Require().NoError(err, "Invoke 不应返回错误")
	s.NotNil(result, "应返回结果")
}
