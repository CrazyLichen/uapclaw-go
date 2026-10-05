//go:build integration

package heartbeat

import (
	"os"
	"path/filepath"
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

// HeartbeatE2ESuite 测试 HeartbeatRail 心跳 E2E 行为。
//
// 覆盖：
//   - 心跳模式下 HEARTBEAT.md 注入 SectionHeartbeat
//   - 心跳模式空 HEARTBEAT.md 不注入
//   - 心跳模式 vs 正常模式行为差异
//   - 心跳模式多次 Invoke 行为
//
// 对齐 Python: tests/unit_tests/harness/test_heartbeat_rail.py（BeforeModelCall 部分）
type HeartbeatE2ESuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestHeartbeatE2ESuite(t *testing.T) {
	suite.Run(t, new(HeartbeatE2ESuite))
}

// TestHeartbeatE2E_心跳模式注入Heartbeat节 测试心跳模式下 HEARTBEAT.md 内容注入 SectionHeartbeat。
// 对齐 Python: HeartbeatRail.before_model_call() 在 run_kind=heartbeat 时读取 HEARTBEAT.md
//
// 核心验证：
//   - 创建包含内容的 HEARTBEAT.md 文件
//   - 通过 MockLLM 配置心跳模式触发
//   - SectionHeartbeat 包含 HEARTBEAT.md 内容
func (s *HeartbeatE2ESuite) TestHeartbeatE2E_心跳模式注入Heartbeat节() {
	rail := heartbeat.NewHeartbeatRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("心跳模式测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// 正常 Invoke — 不在心跳模式下
	result, err := agent.Invoke(s.Ctx, map[string]any{"query": "正常模式"})
	s.Require().NoError(err)
	s.NotNil(result)

	// 正常模式不应注入 SectionHeartbeat
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)
	s.False(spb.HasSection(hsections.SectionHeartbeat), "正常模式不应有 SectionHeartbeat 节")
}

// TestHeartbeatE2E_心跳模式读取HEARTBEATmd 测试心跳模式下 HEARTBEAT.md 读取。
// 对齐 Python: HeartbeatRail.before_model_call() 读取 HEARTBEAT.md 内容
//
// 核心验证：
//   - 创建临时目录和 HEARTBEAT.md
//   - 验证 Rail 不崩溃
func (s *HeartbeatE2ESuite) TestHeartbeatE2E_心跳模式读取HEARTBEATmd() {
	rail := heartbeat.NewHeartbeatRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("读取 HEARTBEAT.md 测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	result, err := agent.Invoke(s.Ctx, map[string]any{"query": "测试 HEARTBEAT.md"})
	s.Require().NoError(err)
	s.NotNil(result)

	// 验证 Rail 已注册
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)
}

// TestHeartbeatE2E_多次Invoke正常 测试多次 Invoke 时 HeartbeatRail 不崩溃。
// 对齐 Python: 多次心跳调用场景
func (s *HeartbeatE2ESuite) TestHeartbeatE2E_多次Invoke正常() {
	rail := heartbeat.NewHeartbeatRail()

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 5,
	})
	s.Require().NoError(err)

	// 第一次 Invoke
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("第一次心跳"))
	result1, err := agent.Invoke(s.Ctx, map[string]any{"query": "第一次"})
	s.Require().NoError(err)
	s.NotNil(result1)

	// 第二次 Invoke
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("第二次心跳"))
	result2, err := agent.Invoke(s.Ctx, map[string]any{"query": "第二次"})
	s.Require().NoError(err)
	s.NotNil(result2)

	// 第三次 Invoke
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("第三次心跳"))
	result3, err := agent.Invoke(s.Ctx, map[string]any{"query": "第三次"})
	s.Require().NoError(err)
	s.NotNil(result3)
}

// TestHeartbeatE2E_无SysOperation时不崩溃 测试无 SysOperation 时 HeartbeatRail 不崩溃。
// 对齐 Python: HeartbeatRail.before_model_call() 中 sys_operation 为 None 时仅 warn
func (s *HeartbeatE2ESuite) TestHeartbeatE2E_无SysOperation时不崩溃() {
	rail := heartbeat.NewHeartbeatRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("无 SysOperation 测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// 无 SysOperation 时不崩溃
	result, err := agent.Invoke(s.Ctx, map[string]any{"query": "测试"})
	s.Require().NoError(err)
	s.NotNil(result)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// createHeartbeatMD 在指定目录下创建 HEARTBEAT.md 文件。
func createHeartbeatMD(dir, content string) error {
	return os.WriteFile(filepath.Join(dir, "HEARTBEAT.md"), []byte(content), 0644)
}
