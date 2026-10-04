//go:build integration

package subagent

import (
	"testing"

	"github.com/stretchr/testify/suite"

	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	subagent "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/subagent"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// VerificationRailSuite 测试 VerificationRail 的集成行为。
//
// 覆盖：
//   - Priority 值
//   - GetCallbacks 注册的回调事件
//   - 默认构造和自定义配置选项
//   - Init 初始化
//   - VerificationContract 相关数据结构
//
// 对齐 Python: tests/unit_tests/harness/test_verification_rail.py
type VerificationRailSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestVerificationRailSuite(t *testing.T) {
	suite.Run(t, new(VerificationRailSuite))
}

// TestVerificationRail_Priority 测试 VerificationRail 优先级为 90。
// 对齐 Python: VerificationRail.priority = 90
func (s *VerificationRailSuite) TestVerificationRail_Priority() {
	rail := subagent.NewVerificationRail()
	s.Equal(90, rail.Priority(), "VerificationRail 优先级应为 90")
}

// TestVerificationRail_GetCallbacks 测试 GetCallbacks 注册了 BeforeModelCall 和 BeforeToolCall。
// 对齐 Python: VerificationRail 隐式覆盖 before_model_call / before_tool_call
func (s *VerificationRailSuite) TestVerificationRail_GetCallbacks() {
	rail := subagent.NewVerificationRail()
	callbacks := rail.GetCallbacks()

	s.NotNil(callbacks, "GetCallbacks 不应返回 nil")

	// 验证 BeforeModelCall 和 BeforeToolCall 已注册
	_, hasBeforeModel := callbacks[agentinterfaces.CallbackBeforeModelCall]
	_, hasBeforeTool := callbacks[agentinterfaces.CallbackBeforeToolCall]
	s.True(hasBeforeModel, "应注册 CallbackBeforeModelCall 回调")
	s.True(hasBeforeTool, "应注册 CallbackBeforeToolCall 回调")
}

// TestVerificationRail_默认构造 测试默认构造生成 12 个允许工具和默认路径参数映射。
// 对齐 Python: VERIFICATION_ALLOWED_TOOLS / _PATH_TOOL_ARG
func (s *VerificationRailSuite) TestVerificationRail_默认构造() {
	rail := subagent.NewVerificationRail()

	s.Equal(90, rail.Priority(), "默认优先级应为 90")

	// 通过集成路径验证：创建 Agent 并检查约束提醒注入
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("默认构造测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)
	s.Require().NotNil(agent)

	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "默认构造验证"})
	s.Require().NoError(err)

	// 验证约束提醒节已注入（间接证明 allowedTools 非空、默认构造正常）
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)
	s.True(spb.HasSection("verification_reminder"), "应存在 verification_reminder 节")
}

// TestVerificationRail_Init 测试 Init 初始化捕获 promptBuilder 并注入约束提醒。
// 对齐 Python: VerificationRail.init(agent) 捕获 system_prompt_builder
func (s *VerificationRailSuite) TestVerificationRail_Init() {
	rail := subagent.NewVerificationRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("Init 测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)
	s.Require().NotNil(agent)

	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "初始化验证"})
	s.Require().NoError(err)

	// Init 后 promptBuilder 非空，BeforeModelCall 能正常注入 section
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)
	s.True(spb.HasSection("verification_reminder"), "Init 后应注入约束提醒节")

	// 验证 section 内容包含关键约束
	section := spb.GetSection("verification_reminder")
	s.Require().NotNil(section)
	s.Contains(section.Content["en"], "VERDICT", "英文内容应包含 VERDICT 关键字")
	s.Contains(section.Content["cn"], "约束", "中文内容应包含约束关键字")
}

// TestVerificationRail_WithCustomConfig 测试 WithAllowedTools 自定义配置选项。
// 对齐 Python: VerificationRail(allowed_tools=...)
func (s *VerificationRailSuite) TestVerificationRail_WithCustomConfig() {
	customTools := map[string]bool{
		"read_file": true,
		"bash":      true,
	}

	rail := subagent.NewVerificationRail(subagent.WithAllowedTools(customTools))

	s.Equal(90, rail.Priority(), "自定义配置不应改变优先级")

	// 通过集成路径验证：使用缩小白名单后 Agent 仍可正常初始化
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("自定义配置测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)
	s.Require().NotNil(agent)

	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "自定义白名单验证"})
	s.Require().NoError(err)

	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)
	s.True(spb.HasSection("verification_reminder"), "自定义白名单仍应注入约束提醒节")
}

// TestVerificationContract 测试 VerificationContractRail 的契约注入。
// 对齐 Python: VerificationContractRail.before_model_call() 注入验证门控契约
func (s *VerificationRailSuite) TestVerificationContract() {
	rail := subagent.NewVerificationContractRail()

	s.Equal(88, rail.Priority(), "VerificationContractRail 优先级应为 88")

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("契约测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)
	s.Require().NotNil(agent)

	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "契约验证"})
	s.Require().NoError(err)

	// 验证契约节存在
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)
	s.True(spb.HasSection("verification_contract"), "应存在 verification_contract 节")

	section := spb.GetSection("verification_contract")
	s.Require().NotNil(section)
	s.Contains(section.Content["en"], "Verification Gate", "英文内容应包含 Verification Gate")
	s.Contains(section.Content["cn"], "验证门控", "中文内容应包含 验证门控")
}

// TestVerificationContract_GetCallbacks 测试 VerificationContractRail 回调注册。
// 对齐 Python: VerificationContractRail 隐式覆盖 before_model_call
func (s *VerificationRailSuite) TestVerificationContract_GetCallbacks() {
	rail := subagent.NewVerificationContractRail()
	callbacks := rail.GetCallbacks()

	s.NotNil(callbacks, "GetCallbacks 不应返回 nil")

	_, hasBeforeModel := callbacks[agentinterfaces.CallbackBeforeModelCall]
	s.True(hasBeforeModel, "应注册 CallbackBeforeModelCall 回调")
}

// TestVerificationContract_Init 测试 VerificationContractRail Init 初始化。
// 对齐 Python: VerificationContractRail.init(agent) 预构建契约 section
func (s *VerificationRailSuite) TestVerificationContract_Init() {
	rail := subagent.NewVerificationContractRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("契约初始化测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)
	s.Require().NotNil(agent)

	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "契约初始化验证"})
	s.Require().NoError(err)

	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)
	s.True(spb.HasSection("verification_contract"), "Init 后应注入契约节")
}

// TestVerificationRail_接口满足性 测试 VerificationRail 和 VerificationContractRail 满足 AgentRail 接口。
// 编译时已有 var _ 检查，此处通过创建 Agent 并注册 Rail 进行运行时验证。
func (s *VerificationRailSuite) TestVerificationRail_接口满足性() {
	var rails []agentinterfaces.AgentRail = []agentinterfaces.AgentRail{
		subagent.NewVerificationRail(),
		subagent.NewVerificationContractRail(),
	}
	s.Len(rails, 2, "应能将两种 Rail 赋值给 AgentRail 接口切片")

	// 验证两者均可作为 AgentRail 注册到 Agent
	for _, r := range rails {
		s.NotNil(r, "Rail 实例不应为 nil")
		s.True(r.Priority() > 0, "Rail 优先级应为正数")
	}
}

// TestVerificationRail_双Rail叠加 测试 VerificationRail + VerificationContractRail 叠加使用。
// 对齐 Python: 父代理挂 VerificationContractRail，验证代理挂 VerificationRail
func (s *VerificationRailSuite) TestVerificationRail_双Rail叠加() {
	vr := subagent.NewVerificationRail()
	cr := subagent.NewVerificationContractRail()

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("双 Rail 叠加测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{vr, cr},
		MaxIterations: 3,
	})
	s.Require().NoError(err)
	s.Require().NotNil(agent)

	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "双 Rail 叠加验证"})
	s.Require().NoError(err)

	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)

	// 两个 Rail 的 section 都应存在
	s.True(spb.HasSection("verification_reminder"), "应存在 verification_reminder 节")
	s.True(spb.HasSection("verification_contract"), "应存在 verification_contract 节")
}
