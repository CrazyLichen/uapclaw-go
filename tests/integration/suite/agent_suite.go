//go:build integration

package suite

import (
	"context"
	"fmt"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
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
	// registeredToolIDs 每个测试方法中注册到 ResourceMgr 的工具 ID，
	// TearDownTest 时清理，避免 suite 内测试间工具 ID 冲突。
	registeredToolIDs []string
	// registeredSysOpIDs 每个测试方法中注册到 ResourceMgr 的 SysOperation ID，
	// TearDownTest 时清理。
	registeredSysOpIDs []string
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

// TearDownTest 每个测试方法后清理全局 ResourceMgr 中注册的工具和 SysOperation。
// 避免 suite 内测试间因工具 ID 重复注册导致后续测试使用前一个测试的工具实例。
func (s *AgentSuite) TearDownTest() {
	rm := s.GetResourceMgr()
	if rm != nil {
		if len(s.registeredToolIDs) > 0 {
			_, _ = rm.RemoveTool(s.registeredToolIDs)
			s.registeredToolIDs = nil
		}
		if len(s.registeredSysOpIDs) > 0 {
			_ = rm.RemoveSysOperation(s.registeredSysOpIDs)
			s.registeredSysOpIDs = nil
		}
	}
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

// NewSpecWithMockLLM 构造一个带 MockLLM 的 DeepAgentSpec（25.1b 前向构造用）。
//
// NativeHarness 自 25.1b 起收 spec 而非已建好的 agent，所以测试要自己把
// MockLLM 的 card + model 装进 spec，才能物化出与被替换 agent 等价的对象。
//
// 每次调用都生成唯一 card id：全局 ResourceMgr / 回调框架按 id 归集，
// 固定 id 会让上一个用例的 rail 继续处理下个用例的会话。
func (s *AgentSuite) NewSpecWithMockLLM(maxIterations int) (hschema.DeepAgentSpec, error) {
	if _, err := llm.NewModel(s.ClientConfig, s.ModelConfig); err != nil {
		return hschema.DeepAgentSpec{}, fmt.Errorf("创建 Mock Model 失败: %w", err)
	}
	spec := hschema.NewDeepAgentSpec()
	spec.MaxIterations = maxIterations
	spec.Card = s.NewAgentCard(fmt.Sprintf("itest_spec_agent_%d", time.Now().UnixNano()), "测试DeepAgent")
	spec.Model = &hschema.TeamModelConfig{
		ModelClientConfig:  *s.ClientConfig,
		ModelRequestConfig: s.ModelConfig,
	}
	return spec, nil
}

// NewDeepAgentForTest 通过工厂创建 DeepAgent 实例。
// 预填 MockLLM 的 Model 和默认 AgentCard，
// 测试只需传入 ToolInstances/Rails 等差异化参数。
// 对齐 Python: create_deep_agent(model=mock_model, ...)
//
// 注意：CreateDeepAgent 会将 ToolInstances 注册到全局 ResourceMgr。
// 本方法跟踪注册的工具 ID，TearDownTest 时自动清理，避免 suite 内测试间冲突。
func (s *AgentSuite) NewDeepAgentForTest(
	ctx context.Context,
	params hconfig.CreateDeepAgentParams,
) (*harness.DeepAgent, error) {
	// 记录工具 ID，TearDownTest 时清理
	for _, t := range params.ToolInstances {
		card := t.Card()
		toolID := card.GetID()
		if toolID == "" {
			toolID = card.GetName()
		}
		if toolID != "" {
			s.registeredToolIDs = append(s.registeredToolIDs, toolID)
		}
	}

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
		// agent id 必须每实例唯一：全局 ResourceMgr / 回调框架按 id 归集，
		// 固定 id 会让上一个用例的 rail 继续处理下个用例的会话（技能基线写进错的 session）。
		params.Card = s.NewAgentCard(fmt.Sprintf("itest_deep_agent_%d", time.Now().UnixNano()), "测试DeepAgent")
	}
	return harness.CreateDeepAgent(ctx, params)
}
