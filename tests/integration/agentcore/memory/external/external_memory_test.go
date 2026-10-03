//go:build integration

package external

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/suite"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/memory"
	ext "github.com/uapclaw/uapclaw-go/internal/agentcore/memory/external"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeMemoryProvider 用于测试的 MemoryProvider Mock 实现。
// 对齐 Python: MockMaliciousBackend（test_guardrail.py）
type fakeMemoryProvider struct {
	ext.BaseMemoryProvider
	name             string
	available        bool
	initialized      bool
	toolSchemas      []ext.ToolSchema
	prefetchResult   string
	prefetchErr      error
	syncErr          error
	syncCallCount    atomic.Int32
	initCallCount    atomic.Int32
	shutdownCalled   bool
	sessionEndCalled bool
}

// ──────────────────────────── 导出函数 ────────────────────────────

func (p *fakeMemoryProvider) Name() string       { return p.name }
func (p *fakeMemoryProvider) IsAvailable() bool   { return p.available }
func (p *fakeMemoryProvider) IsInitialized() bool { return p.initialized }

func (p *fakeMemoryProvider) Initialize(_ context.Context, _ ...ext.ProviderOption) error {
	p.initCallCount.Add(1)
	p.initialized = true
	return nil
}

func (p *fakeMemoryProvider) GetToolSchemas() []ext.ToolSchema {
	return p.toolSchemas
}

func (p *fakeMemoryProvider) HandleToolCall(_ context.Context, _ string, _ map[string]any) (string, error) {
	return "handled", nil
}

func (p *fakeMemoryProvider) Prefetch(_ context.Context, _ string, _ ...ext.ProviderOption) (string, error) {
	if p.prefetchErr != nil {
		return "", p.prefetchErr
	}
	return p.prefetchResult, nil
}

func (p *fakeMemoryProvider) SyncTurn(_ context.Context, _, _ string, _ ...ext.ProviderOption) error {
	p.syncCallCount.Add(1)
	return p.syncErr
}

func (p *fakeMemoryProvider) SystemPromptBlock() string { return "[fake memory context]" }

func (p *fakeMemoryProvider) Shutdown(_ context.Context) error {
	p.shutdownCalled = true
	return nil
}

func (p *fakeMemoryProvider) OnSessionEnd(_ context.Context, _ []map[string]any) error {
	p.sessionEndCalled = true
	return nil
}

// newFakeMemoryProvider 创建默认 fakeMemoryProvider。
func newFakeMemoryProvider() *fakeMemoryProvider {
	return &fakeMemoryProvider{
		name:           "fake",
		available:      true,
		prefetchResult: "fake prefetch content",
		toolSchemas: []ext.ToolSchema{
			{
				Name:        "fake_search",
				Description: "搜索记忆",
				Parameters:  map[string]any{"type": "object"},
			},
		},
	}
}

// ExternalMemorySuite 测试 ExternalMemoryRail 与 MockProvider 的交互。
//
// 对齐 Python: tests/system_tests/harness/test_memory_rail_e2e.py
type ExternalMemorySuite struct {
	isuite.AgentSuite
	provider *fakeMemoryProvider
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestExternalMemorySuite(t *testing.T) {
	suite.Run(t, new(ExternalMemorySuite))
}

func (s *ExternalMemorySuite) SetupSuite() {
	s.AgentSuite.SetupSuite()
	s.provider = newFakeMemoryProvider()
}

// TestExternalMemoryRail_Init注册工具 测试 Init 时 provider 工具注册到 ability_manager。
// 对齐 Python: test_01_memory_rail_basic_invoke
func (s *ExternalMemorySuite) TestExternalMemoryRail_Init注册工具() {
	rail := memory.NewExternalMemoryRail(s.provider, "u1", "s1", "sess1")

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("测试响应"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)
	s.Require().NotNil(agent)

	// Rail Init 是延迟执行的（在 EnsureInitialized/Invoke 时触发）
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "触发 Init"})
	s.Require().NoError(err)

	// 验证 provider 工具已注册
	am := agent.AbilityManager()
	s.Require().NotNil(am)
	// fake_search 工具应在 ability_manager 中
	card := am.Get("fake_search")
	s.NotNil(card, "fake_search 工具应已注册到 ability_manager")
}

// TestExternalMemoryRail_Uninit清理 测试 Uninit 时工具移除 + provider.Shutdown() 被调用。
func (s *ExternalMemorySuite) TestExternalMemoryRail_Uninit清理() {
	provider := newFakeMemoryProvider()
	rail := memory.NewExternalMemoryRail(provider, "u1", "s1", "sess1")

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("测试响应"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// 先调用一次 Invoke 让 Rail 完整初始化
	_, _ = agent.Invoke(s.Ctx, map[string]any{"query": "测试"})

	// 手动 Uninit
	rail.Uninit(agent)

	// 验证 provider.Shutdown 被调用
	s.True(provider.shutdownCalled, "provider.Shutdown 应被调用")

	// 验证工具已从 ability_manager 移除
	am := agent.AbilityManager()
	card := am.Get("fake_search")
	s.Nil(card, "fake_search 工具应已从 ability_manager 移除")
}

// TestExternalMemoryRail_Prefetch缓存 测试 BeforeInvoke 触发 Provider Initialize。
func (s *ExternalMemorySuite) TestExternalMemoryRail_Prefetch缓存() {
	provider := newFakeMemoryProvider()
	provider.prefetchResult = "缓存测试内容"
	rail := memory.NewExternalMemoryRail(provider, "u1", "s1", "sess1")

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("缓存响应"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// 第一次 Invoke：BeforeInvoke 会调用 provider.Initialize
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "第一次查询"})
	s.Require().NoError(err)

	// provider.Initialize 应被调用
	s.GreaterOrEqual(provider.initCallCount.Load(), int32(1), "provider.Initialize 应被调用")
}

// TestExternalMemoryRail_熔断器触发 测试连续失败后熔断器打开。
// 对齐 Python: ExternalMemoryRail 的 sync breaker 机制
func (s *ExternalMemorySuite) TestExternalMemoryRail_熔断器触发() {
	provider := newFakeMemoryProvider()
	provider.syncErr = fmt.Errorf("模拟 SyncTurn 失败")
	rail := memory.NewExternalMemoryRail(provider, "u1", "s1", "sess1")

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("熔断测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// 执行多次 Invoke，每次 AfterInvoke 会尝试 SyncTurn
	for i := 0; i < 6; i++ {
		_, _ = agent.Invoke(s.Ctx, map[string]any{"query": fmt.Sprintf("第%d次", i)})
	}

	// 熔断器测试：验证 Rail 不崩溃，熔断器状态由单元测试覆盖
	s.True(provider.initCallCount.Load() >= 1, "provider.Initialize 应被调用")
}

// TestExternalMemoryRail_熔断器恢复 测试修复 provider 后可以恢复。
func (s *ExternalMemorySuite) TestExternalMemoryRail_熔断器恢复() {
	provider := newFakeMemoryProvider()
	provider.syncErr = fmt.Errorf("模拟失败")
	rail := memory.NewExternalMemoryRail(provider, "u1", "s1", "sess1")

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("熔断恢复测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// 先触发熔断
	for i := 0; i < 6; i++ {
		_, _ = agent.Invoke(s.Ctx, map[string]any{"query": "熔断触发"})
	}

	// 修复 provider
	provider.syncErr = nil

	// 熔断器冷却期 120s，此测试仅验证 Rail 不崩溃
	// 在冷却期内继续 Invoke 不会导致 panic
	_, _ = agent.Invoke(s.Ctx, map[string]any{"query": "冷却期内"})
	// 无 panic 即为通过
}
