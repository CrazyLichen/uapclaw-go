package harness

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// 15.15 热重载链核对回归用例（spec 第 4/5 节）。
// Python: deep_agent.py:530-700 configure() 热重载链。

// newHotReloadAgent 构造已配置模型的可热重配 agent。
func newHotReloadAgent(t *testing.T) *DeepAgent {
	t.Helper()
	agent := newTestDeepAgent()
	model, err := llm.NewModel(&llmschema.ModelClientConfig{
		ClientID: "c1", ClientProvider: "OpenAI", APIKey: "k",
		APIBase: "http://localhost:0/v1", Timeout: 30, MaxRetries: 1,
	}, &llmschema.ModelRequestConfig{ModelName: "m1"})
	require.NoError(t, err)
	cfg := hschema.NewDeepAgentConfig()
	cfg.Card = agent.Card()
	cfg.Model = model
	require.NoError(t, agent.ConfigureDeepConfig(context.Background(), cfg))
	return agent
}

// TestConfigure热换模型_插件工具与rail保留 load_plugin_ability 后热换模型，
// 已绑工具与未受影响 rail 必须保留（部分更新保留语义）。
// Python: _hot_reconfigure → _hot_reload_rails 按类型替换（deep_agent.py:263-289）。
func TestConfigure热换模型_插件工具与rail保留(t *testing.T) {
	ctx := context.Background()
	agent := newHotReloadAgent(t)

	// 绑定一个插件工具 + 一个非 model 相关 rail
	card := newTestToolCard("keep_tool")
	_, err := agent.LoadPluginAbility(ctx, []any{card}, nil, nil)
	require.NoError(t, err)
	stub := &stubRail{name: "keep-rail"}
	require.NoError(t, agent.RegisterRail(ctx, stub))
	railsBefore := len(agent.FindRailsByType(reflect.TypeOf(&stubRail{})))

	// 热换模型（仅 model 字段变化）
	model2, err := llm.NewModel(&llmschema.ModelClientConfig{
		ClientID: "c2", ClientProvider: "OpenAI", APIKey: "k2",
		APIBase: "http://localhost:1/v1", Timeout: 30, MaxRetries: 1,
	}, &llmschema.ModelRequestConfig{ModelName: "m2"})
	require.NoError(t, err)
	cfg := hschema.NewDeepAgentConfig()
	cfg.Card = agent.Card()
	cfg.Model = model2
	cfg.Language = "cn"
	// Python 语义：config.rails 非 nil → 按类型部分替换（替换列表为空 → 全保留）；
	// nil → 全量退役。热换模型由调用方携带既有 rails 列表实现保留。
	cfg.Rails = []agentinterfaces.AgentRail{}
	require.NoError(t, agent.ConfigureDeepConfig(ctx, cfg))

	// 工具仍在
	assert.NotNil(t, agent.AbilityManager().Get("keep_tool"))
	// rail 仍在且数量不变
	rails := agent.FindRailsByType(reflect.TypeOf(&stubRail{}))
	assert.Len(t, rails, railsBefore)
}

// TestPartialHotReloadRails_未出现在新列表的类型保留 部分更新时其余类型保留。
// Python: _hot_reload_rails partial 分支（deep_agent.py:267-278）。
func TestPartialHotReloadRails_未出现在新列表的类型保留(t *testing.T) {
	ctx := context.Background()
	agent := newHotReloadAgent(t)

	keep := &stubRail{name: "keep-rail"}
	require.NoError(t, agent.RegisterRail(ctx, keep))

	// 新配置只声明 drop 类型——但 stubRail 类型与 keep 相同会被替换；
	// 改用不同 rail 类型验证保留语义
	other := &secondStubRail{}
	require.NoError(t, agent.RegisterRail(ctx, other))

	cfg := hschema.NewDeepAgentConfig()
	cfg.Card = agent.Card()
	cfg.Language = "cn"
	cfg.Rails = []agentinterfaces.AgentRail{other}
	require.NoError(t, agent.ConfigureDeepConfig(ctx, cfg))

	assert.Len(t, agent.FindRailsByType(reflect.TypeOf(&stubRail{})), 1,
		"未出现在新列表的类型应保留")
	assert.NotNil(t, agent.FindRailsByType(reflect.TypeOf(&secondStubRail{})))
}

// secondStubRail 第二种 rail 类型（与 stubRail 区分替换/保留）。
type secondStubRail struct {
	agentinterfaces.AgentRail
}

func (s *secondStubRail) Init(context.Context, agentinterfaces.BaseAgent) error { return nil }
func (s *secondStubRail) Uninit(agentinterfaces.BaseAgent) error                { return nil }
func (s *secondStubRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	return nil
}
