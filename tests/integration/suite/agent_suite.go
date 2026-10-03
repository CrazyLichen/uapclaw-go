//go:build integration

package suite

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/resources_manager"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentSuite 包含 Agent 执行能力的集成测试套件。
// 等价于 Python conftest.py 中注册 AgentCard + 创建 Agent 的 fixture。
//
// 提供：
//   - SessionSuite 全部能力
//   - AgentCard 和对应的 BaseAgent 实例
//   - NewDeepAgentForTest() 工厂辅助方法
//
// Agent 在具体测试中按需注册，因为不同测试需要不同类型的 Agent。
type AgentSuite struct {
	SessionSuite
	// AgentCard 已注册的 Agent 卡片
	AgentCard *agentschema.AgentCard
	// Agent 已创建的 Agent 实例
	Agent agentinterfaces.BaseAgent
}

// ──────────────────────────── 导出函数 ────────────────────────────

// SetupSuite 初始化 Agent 测试环境。
func (s *AgentSuite) SetupSuite() {
	s.SessionSuite.SetupSuite()
}

// TearDownSuite 清理 Agent 测试环境。
func (s *AgentSuite) TearDownSuite() {
	s.SessionSuite.TearDownSuite()
}

// RegisterAgent 向 ResourceMgr 注册一个 Agent。
// provider 是 Agent 的创建函数，在 Runner 需要创建实例时调用。
// 对齐 Python: Runner.resource_mgr.add_agent(card, provider)
func (s *AgentSuite) RegisterAgent(
	ctx context.Context,
	card *agentschema.AgentCard,
	provider resources_manager.AgentProvider,
) error {
	return s.GetResourceMgr().AddAgent(card, provider)
}

// NewAgentCard 创建带唯一 ID 的测试用 AgentCard。
// 对齐 Python: AgentCard(id=..., name=...)
func (s *AgentSuite) NewAgentCard(id, name string) *agentschema.AgentCard {
	return agentschema.NewAgentCard(
		agentschema.WithAgentID(id),
		agentschema.WithAgentName(name),
	)
}

// NewDeepAgentForTest 通过工厂创建 DeepAgent 实例。
// 预填 MockLLM 的 Model 和默认 AgentCard，
// 测试只需传入 ToolInstances/Rails 等差异化参数。
// 对齐 Python: create_deep_agent(model=mock_model, ...)
func (s *AgentSuite) NewDeepAgentForTest(
	ctx context.Context,
	params hconfig.CreateDeepAgentParams,
) (*harness.DeepAgent, error) {
	// 确保传入 Model（从 ClientConfig + ModelConfig 创建，走 ClientRegistry 获取 MockLLM）
	if params.Model == nil {
		model, err := llm.NewModel(s.ClientConfig, s.ModelConfig)
		if err != nil {
			return nil, fmt.Errorf("创建 Mock Model 失败: %w", err)
		}
		params.Model = model
	}
	// 默认 AgentCard
	if params.Card == nil {
		params.Card = s.NewAgentCard("itest_deep_agent", "测试DeepAgent")
	}
	return harness.CreateDeepAgent(ctx, params)
}
