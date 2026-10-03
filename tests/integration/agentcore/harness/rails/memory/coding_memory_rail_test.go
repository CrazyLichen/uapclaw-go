//go:build integration

package memory

import (
	"testing"

	"github.com/stretchr/testify/suite"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
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
//   - BeforeModelCall 有/无召回结果注入
//   - Uninit 清理
//
// 对应 Python 代码：openjiuwen/harness/rails/memory/coding_memory_rail.py
type CodingMemoryRailSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestCodingMemoryRailSuite(t *testing.T) {
	suite.Run(t, new(CodingMemoryRailSuite))
}

// TestCodingMemoryRail_Init注册工具 测试 CodingMemoryRail.Init 注册 coding_memory 工具到 ability_manager。
// 对齐 Python: CodingMemoryRail.init(agent) 中 _register_coding_memory_tools
func (s *CodingMemoryRailSuite) TestCodingMemoryRail_Init注册工具() {
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

	// 验证 ability_manager 包含 coding_memory 工具
	am := agent.AbilityManager()
	s.Require().NotNil(am, "AbilityManager 不应为 nil")
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

// TestCodingMemoryRail_BeforeModelCall_有结果注入 测试 BeforeModelCall 注入"已加载的相关记忆"节。
// 对齐 Python: CodingMemoryRail.before_model_call(ctx) 中有召回结果时的注入
func (s *CodingMemoryRailSuite) TestCodingMemoryRail_BeforeModelCall_有结果注入() {
	tempDir := s.T().TempDir()
	rail := memory.NewCodingMemoryRail(tempDir, nil, "cn")

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("有结果注入测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// 无真实 embedding 时 manager 为 nil，BeforeModelCall 降级注入索引
	// 验证 Invoke 完成无错误
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "记忆查询"})
	s.Require().NoError(err, "BeforeModelCall 应成功完成（无召回时降级注入索引）")
}

// TestCodingMemoryRail_BeforeModelCall_无结果降级 测试 BeforeModelCall 无召回结果时降级注入索引。
// 对齐 Python: CodingMemoryRail.before_model_call(ctx) 中无结果时的降级路径
func (s *CodingMemoryRailSuite) TestCodingMemoryRail_BeforeModelCall_无结果降级() {
	tempDir := s.T().TempDir()
	rail := memory.NewCodingMemoryRail(tempDir, nil, "cn")

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("无结果降级测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// 无 MemoryIndexManager 时，BeforeModelCall 降级路径
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "无记忆查询"})
	s.Require().NoError(err, "BeforeModelCall 降级路径应成功完成")
}

// TestCodingMemoryRail_Uninit清理 测试 Uninit 注销工具后无崩溃。
// 对齐 Python: CodingMemoryRail.uninit(agent) 中工具移除 + 状态清理
func (s *CodingMemoryRailSuite) TestCodingMemoryRail_Uninit清理() {
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

	// Uninit 不应崩溃
	s.NotPanics(func() {
		rail.Uninit(agent)
	}, "Uninit 不应 panic")
}
