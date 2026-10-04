//go:build integration

package heartbeat

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/suite"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	hsections "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	heartbeat "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// HeartbeatRailSuite 测试 HeartbeatRail 心跳。
//
// 覆盖：
//   - Init 注册成功
//   - 正常模式 Invoke 不注入 SectionHeartbeat
//   - Uninit 移除 SectionHeartbeat
//
// 对齐 Python: tests/unit_tests/harness/test_heartbeat_rail.py
type HeartbeatRailSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestHeartbeatRailSuite(t *testing.T) {
	suite.Run(t, new(HeartbeatRailSuite))
}

// TestHeartbeatRail_Init成功 测试 HeartbeatRail 成功创建和初始化。
// 对齐 Python: HeartbeatRail.init() 捕获 system_prompt_builder 和 sys_operation
func (s *HeartbeatRailSuite) TestHeartbeatRail_Init成功() {
	rail := heartbeat.NewHeartbeatRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("心跳测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "心跳测试"})
	s.Require().NoError(err, "Invoke 不应返回错误")

	railType := reflect.TypeOf(&heartbeat.HeartbeatRail{})
	foundRails := agent.FindRailsByType(railType)
	s.NotEmpty(foundRails, "应注册 HeartbeatRail")
}

// TestHeartbeatRail_正常模式无Heartbeat节 测试正常 Invoke 时 SectionHeartbeat 不被注入。
// 对齐 Python: HeartbeatRail.before_model_call() 仅在 run_kind=heartbeat 时激活
func (s *HeartbeatRailSuite) TestHeartbeatRail_正常模式无Heartbeat节() {
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

	// 正常模式（run_kind != heartbeat）不应注入 SectionHeartbeat
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)
	s.False(spb.HasSection(hsections.SectionHeartbeat), "正常模式不应有 SectionHeartbeat 节")
}

// TestHeartbeatRail_Uninit移除Heartbeat节 测试 Uninit 移除 SectionHeartbeat。
func (s *HeartbeatRailSuite) TestHeartbeatRail_Uninit移除Heartbeat节() {
	rail := heartbeat.NewHeartbeatRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("Uninit 测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	_, _ = agent.Invoke(s.Ctx, map[string]any{"query": "测试"})

	// Uninit 应成功
	s.NotPanics(func() {
		rail.Uninit(agent)
	}, "Uninit 不应 panic")

	// 验证 SectionHeartbeat 不存在
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)
	s.False(spb.HasSection(hsections.SectionHeartbeat), "Uninit 后不应有 SectionHeartbeat 节")
}
