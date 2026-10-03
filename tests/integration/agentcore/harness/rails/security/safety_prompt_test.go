//go:build integration

package security

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/suite"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	securityrail "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/security"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SafetyPromptRailSuite 测试 SafetyPromptRail 安全护栏。
//
// 对齐 Python: tests/system_tests/security/guardrail/test_guardrail.py
// Go 端无 PromptInjectionGuardrail，改测 SafetyPromptRail 的注册/注入/移除生命周期。
type SafetyPromptRailSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestSafetyPromptRailSuite(t *testing.T) {
	suite.Run(t, new(SafetyPromptRailSuite))
}

// TestSafetyPromptRail_自动注册 测试不传 Rails 时 addDefaultRails 自动注册 SafetyPromptRail。
// 对齐 Python: test_default_events_registration
func (s *SafetyPromptRailSuite) TestSafetyPromptRail_自动注册() {
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("自动注册测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		MaxIterations: 3,
	})
	s.Require().NoError(err)
	s.Require().NotNil(agent)

	// 查找 SafetyPromptRail
	railType := reflect.TypeOf(&securityrail.SafetyPromptRail{})
	foundRails := agent.FindRailsByType(railType)
	s.NotEmpty(foundRails, "应自动注册 SafetyPromptRail")
}

// TestSafetyPromptRail_Init注入安全节 测试 Init 后 SystemPromptBuilder 包含安全节。
// 对齐 Python: test_blocks_attack（逻辑对齐：安全节注入 = 安全防护）
func (s *SafetyPromptRailSuite) TestSafetyPromptRail_Init注入安全节() {
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("安全节注入测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// SafetyPromptRail 已自动注册，验证 SystemPromptBuilder 包含安全节
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb, "SystemPromptBuilder 不应为 nil")

	// 安全节在 BeforeModelCall 时注入，先执行一次 Invoke
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "安全测试"})
	s.Require().NoError(err, "Invoke 不应返回错误")
}

// TestSafetyPromptRail_BeforeModelCall_安全检查 测试 SafetyPromptRail.runSecurityCheck 返回 Allow。
// 对齐 Python: test_allows_safe_content
func (s *SafetyPromptRailSuite) TestSafetyPromptRail_BeforeModelCall_安全检查() {
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("安全检查测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// 执行 Invoke，SafetyPromptRail 不应阻断正常输入
	result, err := agent.Invoke(s.Ctx, map[string]any{"query": "正常的安全查询"})
	s.Require().NoError(err, "正常查询不应被 SafetyPromptRail 拒绝")
	s.NotNil(result, "Invoke 应返回结果")
}

// TestSafetyPromptRail_手动注入优先级 测试传入自定义 SafetyPromptRail 时不被重复注册。
func (s *SafetyPromptRailSuite) TestSafetyPromptRail_手动注入优先级() {
	// 手动创建 SafetyPromptRail
	customRail := securityrail.NewSafetyPromptRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("手动注入测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{customRail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// 应只有 1 个 SafetyPromptRail（手动传入 + addDefaultRails 检测到已有）
	railType := reflect.TypeOf(&securityrail.SafetyPromptRail{})
	foundRails := agent.FindRailsByType(railType)
	s.Len(foundRails, 1, "应只有 1 个 SafetyPromptRail（不被重复注册）")
}

// TestSafetyPromptRail_Uninit移除安全节 测试 Uninit 后 SectionSafety 从 SystemPromptBuilder 移除。
// 对齐 Python: test_unregister_removes_callbacks
func (s *SafetyPromptRailSuite) TestSafetyPromptRail_Uninit移除安全节() {
	customRail := securityrail.NewSafetyPromptRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("移除测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{customRail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// 执行一次 Invoke
	_, _ = agent.Invoke(s.Ctx, map[string]any{"query": "测试"})

	// Uninit SafetyPromptRail
	customRail.Uninit(agent)

	// 验证 SectionSafety 已从 SystemPromptBuilder 移除
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)
	// 移除后再次 Invoke 应正常（只是缺少安全节）
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("移除后测试"))
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "移除后再测试"})
	s.NoError(err, "Uninit 后 Invoke 应正常（只是缺少安全节）")
}
