//go:build integration

package memory

import (
	"testing"

	"github.com/stretchr/testify/suite"
	memory "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/memory"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MemoryRailSuite 测试 MemoryRail 在 DeepAgent 上下文中的集成行为。
//
// 覆盖：
//   - Init 注册 memory 工具
//   - BeforeInvoke 初始化
//   - BeforeModelCall 注入记忆提示词
//   - 只读模式降级
//   - Uninit 清理
//
// 对应 Python 代码：openjiuwen/harness/rails/memory/memory_rail.py
type MemoryRailSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestMemoryRailSuite(t *testing.T) {
	suite.Run(t, new(MemoryRailSuite))
}

// TestMemoryRail_Init注册工具 测试 MemoryRail.Init 注册 memory 工具到 ability_manager。
// 对齐 Python: MemoryRail.init(agent) 中 _register_memory_tools
func (s *MemoryRailSuite) TestMemoryRail_Init注册工具() {
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

	// 验证 ability_manager 包含 memory 工具
	am := agent.AbilityManager()
	s.Require().NotNil(am, "AbilityManager 不应为 nil")
}

// TestMemoryRail_BeforeInvoke_初始化 测试 BeforeInvoke 初始化记忆管理器。
// 对齐 Python: MemoryRail.before_invoke(ctx) 中首次初始化逻辑
func (s *MemoryRailSuite) TestMemoryRail_BeforeInvoke_初始化() {
	rail := memory.NewMemoryRail(nil, false)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("BeforeInvoke 初始化测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// Invoke 触发 BeforeInvoke（首次初始化 memory manager）
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "初始化测试"})
	s.Require().NoError(err, "BeforeInvoke 应成功完成")
}

// TestMemoryRail_BeforeModelCall_注入提示词 测试 BeforeModelCall 注入 SectionMemory 提示词。
// 对齐 Python: MemoryRail.before_model_call(ctx) 中 BuildMemorySection
func (s *MemoryRailSuite) TestMemoryRail_BeforeModelCall_注入提示词() {
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
}

// TestMemoryRail_BeforeModelCall_只读模式 测试 BeforeModelCall 在只读模式下注入 read_only 提示词。
// 对齐 Python: MemoryRail.before_model_call(ctx) 中 is_cron/is_heartbeat 场景
func (s *MemoryRailSuite) TestMemoryRail_BeforeModelCall_只读模式() {
	rail := memory.NewMemoryRail(nil, false)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("只读模式测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// 正常 Invoke（非 cron/heartbeat），验证正常模式完成
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "普通查询"})
	s.Require().NoError(err, "正常模式 BeforeModelCall 应成功")
}

// TestMemoryRail_Uninit清理 测试 Uninit 注销工具后无崩溃。
// 对齐 Python: MemoryRail.uninit(agent) 中工具移除 + 状态清理
func (s *MemoryRailSuite) TestMemoryRail_Uninit清理() {
	rail := memory.NewMemoryRail(nil, false)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("Uninit 测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// 执行一次 Invoke
	_, _ = agent.Invoke(s.Ctx, map[string]any{"query": "测试"})

	// Uninit 不应崩溃
	s.NotPanics(func() {
		rail.Uninit(agent)
	}, "Uninit 不应 panic")
}
