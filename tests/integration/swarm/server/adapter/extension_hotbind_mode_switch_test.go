//go:build integration

package adapter_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ExtensionHotBindSuite 领域 15.15 双层级回归：插件热绑的 rail/工具必须
// 存活于 adapter 模式切换（UpdateAgentRails 重建链），不得被冲掉。
// 对应 spec「回归清单 1 号」与 D-15f 语义固化。
type ExtensionHotBindSuite struct {
	isuite.BaseIntegrationSuite
	// env 每测试隔离的集成环境
	env *testhelpers.AdapterTestEnv
}

// hotbindStubRail 热绑用桩 rail（带可识别类型，供 FindRailsByType 断言）。
type hotbindStubRail struct {
	agentinterfaces.AgentRail
}

func (r *hotbindStubRail) Init(context.Context, agentinterfaces.BaseAgent) error { return nil }
func (r *hotbindStubRail) Uninit(agentinterfaces.BaseAgent) error                { return nil }
func (r *hotbindStubRail) Name() string                                          { return "hotbind-stub-rail" }
func (r *hotbindStubRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	return nil
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestExtensionHotBindSuite 运行插件热绑存活集成测试。
func TestExtensionHotBindSuite(t *testing.T) {
	suite.Run(t, new(ExtensionHotBindSuite))
}

// Test插件热绑rail与工具_存活于adapter模式切换 真实链路：
// CreateInstance 装配 → LoadPluginAbility 热绑（rail + 工具卡）→
// UpdateAgentRails（模式切换重建链）→ 插件 rail 仍注册、工具能力仍在、
// DeepAgentConfig.Tools 记账仍在。
func (s *ExtensionHotBindSuite) Test插件热绑rail与工具_存活于adapter模式切换() {
	ctx := s.Ctx

	// 步骤 1：装配 adapter（session-scoped，rail 挂自身 DeepAgent）
	s.env.WriteConfigYAML(s.T(), "")
	d := adapter.NewDeepAdapter()
	d.MarkAsSessionScoped("itest-hotbind")
	s.Require().NoError(d.CreateInstance(s.Ctx, map[string]any{}, "agent", "", nil))
	s.T().Cleanup(func() { _ = d.Cleanup() })

	// 步骤 2：插件热绑（15.15 六入口之一）
	card := tool.NewToolCardWithID("itest_ext_tool", "itest_ext_tool", "集成热绑工具", nil, nil)
	stubRail := &hotbindStubRail{}
	_, err := d.Instance().LoadPluginAbility(
		ctx,
		[]tool.ToolResource{{Card: card}},
		[]agentinterfaces.AgentRail{stubRail},
		nil,
	)
	s.Require().NoError(err)
	s.Require().Equal(1, len(d.Instance().FindRailsByType(reflect.TypeOf(&hotbindStubRail{}))),
		"热绑后桩 rail 必须已注册")
	s.Require().NotNil(d.Instance().AbilityManager().Get("itest_ext_tool"),
		"热绑后工具能力必须已注册")

	// 步骤 3：adapter 模式切换（rail 装配重建链）
	d.UpdateAgentRails(ctx, "itest-hotbind", "agent.work.plan")

	// 步骤 4：插件热绑能力存活
	s.Equal(1, len(d.Instance().FindRailsByType(reflect.TypeOf(&hotbindStubRail{}))),
		"模式切换后插件 rail 不得被冲掉")
	s.NotNil(d.Instance().AbilityManager().Get("itest_ext_tool"),
		"模式切换后插件工具能力不得被冲掉")
	s.NotNil(d.Instance().DeepConfig(), "DeepConfig 必须可用")
	// adapter 冷启动自带的工具也在 config.Tools 里；只断言插件卡仍在册
	found := false
	for _, c := range d.Instance().DeepConfig().Tools {
		if c.Name == "itest_ext_tool" {
			found = true
			break
		}
	}
	s.True(found, "模式切换后插件工具的 config 记账不得丢失")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// SetupTest 每测试隔离数据目录与 mock LLM provider。
func (s *ExtensionHotBindSuite) SetupTest() {
	s.env = testhelpers.NewAdapterTestEnv(s.T(), s.MockLLM, "mock_extension_hotbind")
}
