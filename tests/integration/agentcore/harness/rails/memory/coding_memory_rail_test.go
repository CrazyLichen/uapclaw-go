//go:build integration

package memory

import (
	"testing"

	"github.com/stretchr/testify/suite"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	hsections "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	memory "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/memory"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CodingMemoryRailSuite 测试 CodingMemoryRail 在 DeepAgent 上下文中的集成行为。
//
// 覆盖：
//   - Init 注册 coding_memory 工具
//   - BeforeInvoke 异步预取
//   - BeforeModelCall 有/无召回结果注入 + SectionMemory 内容
//   - Uninit 清理工具和 Section
//
// 对应 Python 代码：openjiuwen/harness/rails/memory/coding_memory_rail.py
type CodingMemoryRailSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestCodingMemoryRailSuite(t *testing.T) {
	suite.Run(t, new(CodingMemoryRailSuite))
}

// TestCodingMemoryRail_Init注册3个工具 测试 CodingMemoryRail.Init 注册 3 个 coding_memory 工具到 ability_manager。
// 对齐 Python: CodingMemoryRail.init(agent) 中 _register_coding_memory_tools
func (s *CodingMemoryRailSuite) TestCodingMemoryRail_Init注册3个工具() {
	tempDir := s.T().TempDir()
	rail := memory.NewCodingMemoryRail(tempDir, nil, "cn")

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("Init 注册工具测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)
	s.Require().NotNil(agent)

	// 执行 Invoke 触发 Init + BeforeInvoke + BeforeModelCall
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "测试 coding memory"})
	s.Require().NoError(err, "Invoke 不应返回错误")

	// 验证 ability_manager 包含 3 个 coding_memory 工具
	am := agent.AbilityManager()
	s.Require().NotNil(am, "AbilityManager 不应为 nil")
	s.NotNil(am.Get("coding_memory_read"), "应注册 coding_memory_read")
	s.NotNil(am.Get("coding_memory_write"), "应注册 coding_memory_write")
	s.NotNil(am.Get("coding_memory_edit"), "应注册 coding_memory_edit")
}

// TestCodingMemoryRail_BeforeModelCall_注入SectionMemory 测试 BeforeModelCall 注入 SectionMemory 节。
// 对齐 Python: CodingMemoryRail.before_model_call(ctx) 中向 SystemPromptBuilder 添加 memory section
func (s *CodingMemoryRailSuite) TestCodingMemoryRail_BeforeModelCall_注入SectionMemory() {
	tempDir := s.T().TempDir()
	rail := memory.NewCodingMemoryRail(tempDir, nil, "cn")

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("Section 注入测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// Invoke 触发 BeforeModelCall，应注入 SectionMemory
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "记忆查询"})
	s.Require().NoError(err, "BeforeModelCall 应成功完成")

	// 验证 SectionMemory 已注入
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb, "SystemPromptBuilder 不应为 nil")
	s.True(spb.HasSection(hsections.SectionMemory), "应存在 SectionMemory 节")

	// 验证节内容非空
	section := spb.GetSection(hsections.SectionMemory)
	s.Require().NotNil(section, "SectionMemory 节不应为 nil")
	s.NotEmpty(section.Content, "SectionMemory 节内容不应为空")
}

// TestCodingMemoryRail_BeforeInvoke_异步预取 测试 BeforeInvoke 启动异步预取 goroutine。
// 对齐 Python: CodingMemoryRail.before_invoke(ctx) 中 autoRecall 启动
func (s *CodingMemoryRailSuite) TestCodingMemoryRail_BeforeInvoke_异步预取() {
	tempDir := s.T().TempDir()
	rail := memory.NewCodingMemoryRail(tempDir, nil, "cn")

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("异步预取测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// Invoke 触发 BeforeInvoke（内部启动异步预取 goroutine）
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "预取测试"})
	s.Require().NoError(err, "BeforeInvoke 应成功完成（无 manager 时降级）")
}

// TestCodingMemoryRail_Uninit清理工具和节 测试 Uninit 注销工具并移除 SectionMemory。
// 对齐 Python: CodingMemoryRail.uninit(agent) 中工具移除 + 状态清理 + section 移除
func (s *CodingMemoryRailSuite) TestCodingMemoryRail_Uninit清理工具和节() {
	tempDir := s.T().TempDir()
	rail := memory.NewCodingMemoryRail(tempDir, nil, "cn")

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("Uninit 测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// 执行一次 Invoke
	_, _ = agent.Invoke(s.Ctx, map[string]any{"query": "测试"})

	// Uninit 应成功
	s.NotPanics(func() {
		rail.Uninit(agent)
	}, "Uninit 不应 panic")

	// 验证工具已从 AM 注销
	am := agent.AbilityManager()
	s.Require().NotNil(am)
	s.Nil(am.Get("coding_memory_read"), "Uninit 后应注销 coding_memory_read")
	s.Nil(am.Get("coding_memory_write"), "Uninit 后应注销 coding_memory_write")
	s.Nil(am.Get("coding_memory_edit"), "Uninit 后应注销 coding_memory_edit")

	// 验证 SectionMemory 已移除
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)
	s.False(spb.HasSection(hsections.SectionMemory), "Uninit 后应移除 SectionMemory 节")
}
