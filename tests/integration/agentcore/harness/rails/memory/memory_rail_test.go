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

// MemoryRailSuite 测试 MemoryRail 在 DeepAgent 上下文中的集成行为。
//
// 覆盖：
//   - Init 注册 5 个 memory 工具
//   - BeforeInvoke 初始化
//   - BeforeModelCall 注入 SectionMemory（默认/主动/只读模式）
//   - Uninit 清理工具和 Section
//
// 对应 Python 代码：openjiuwen/harness/rails/memory/memory_rail.py
type MemoryRailSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestMemoryRailSuite(t *testing.T) {
	suite.Run(t, new(MemoryRailSuite))
}

// TestMemoryRail_Init注册5个工具 测试 MemoryRail.Init 注册 5 个 memory 工具到 ability_manager。
// 对齐 Python: MemoryRail.init(agent) 中 _register_memory_tools
func (s *MemoryRailSuite) TestMemoryRail_Init注册5个工具() {
	rail := memory.NewMemoryRail(nil, false)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("Init 注册工具测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)
	s.Require().NotNil(agent)

	// 执行 Invoke 触发 Init + BeforeInvoke + BeforeModelCall
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "测试 memory"})
	s.Require().NoError(err, "Invoke 不应返回错误")

	// 验证 ability_manager 包含 5 个 memory 工具
	am := agent.AbilityManager()
	s.Require().NotNil(am, "AbilityManager 不应为 nil")
	s.NotNil(am.Get("memory_search"), "应注册 memory_search")
	s.NotNil(am.Get("memory_get"), "应注册 memory_get")
	s.NotNil(am.Get("write_memory"), "应注册 write_memory")
	s.NotNil(am.Get("edit_memory"), "应注册 edit_memory")
	s.NotNil(am.Get("read_memory"), "应注册 read_memory")
}

// TestMemoryRail_BeforeModelCall_默认模式注入 测试 BeforeModelCall 以默认（inactive）模式注入 SectionMemory。
// 对齐 Python: MemoryRail.before_model_call(ctx) 中 BuildMemorySection
func (s *MemoryRailSuite) TestMemoryRail_BeforeModelCall_默认模式注入() {
	rail := memory.NewMemoryRail(nil, false)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("注入提示词测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// Invoke 触发 BeforeModelCall，注入 SectionMemory
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "记忆提示词"})
	s.Require().NoError(err, "BeforeModelCall 注入记忆节应成功完成")

	// 验证 SectionMemory 已注入
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb, "SystemPromptBuilder 不应为 nil")
	s.True(spb.HasSection(hsections.SectionMemory), "应存在 SectionMemory 节")

	// 验证节内容非空
	section := spb.GetSection(hsections.SectionMemory)
	s.Require().NotNil(section, "SectionMemory 节不应为 nil")
	s.NotEmpty(section.Content, "SectionMemory 节内容不应为空")
}

// TestMemoryRail_BeforeModelCall_主动模式内容 测试 BeforeModelCall 以 proactive 模式注入包含主动记录指令的 SectionMemory。
// 对齐 Python: MemoryRail(is_proactive=True) 中 BuildMemorySection(mode="proactive")
func (s *MemoryRailSuite) TestMemoryRail_BeforeModelCall_主动模式内容() {
	rail := memory.NewMemoryRail(nil, true)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("主动模式测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "主动记录"})
	s.Require().NoError(err)

	// 验证 SectionMemory 已注入
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)
	s.True(spb.HasSection(hsections.SectionMemory), "主动模式应注入 SectionMemory")

	section := spb.GetSection(hsections.SectionMemory)
	s.Require().NotNil(section)
	// 主动模式内容应包含"核心操作规范"或"proactive"关键字
	cnContent := section.Content["cn"]
	s.NotEmpty(cnContent, "中文内容不应为空")
}

// TestMemoryRail_Uninit清理工具和节 测试 Uninit 注销 5 个工具并移除 SectionMemory。
// 对齐 Python: MemoryRail.uninit(agent) 中工具移除 + 状态清理 + section 移除
func (s *MemoryRailSuite) TestMemoryRail_Uninit清理工具和节() {
	rail := memory.NewMemoryRail(nil, false)

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

	// 验证 5 个工具已从 AM 注销
	am := agent.AbilityManager()
	s.Require().NotNil(am)
	s.Nil(am.Get("memory_search"), "Uninit 后应注销 memory_search")
	s.Nil(am.Get("memory_get"), "Uninit 后应注销 memory_get")
	s.Nil(am.Get("write_memory"), "Uninit 后应注销 write_memory")
	s.Nil(am.Get("edit_memory"), "Uninit 后应注销 edit_memory")
	s.Nil(am.Get("read_memory"), "Uninit 后应注销 read_memory")

	// 验证 SectionMemory 已移除
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)
	s.False(spb.HasSection(hsections.SectionMemory), "Uninit 后应移除 SectionMemory 节")
}
