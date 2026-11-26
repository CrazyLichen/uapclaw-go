package evolution

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/trajectory"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
)

// ──────────────────────────── 测试替身 ────────────────────────────

// fakeConfigurableAgent EvolutionConfigurableAgent 测试替身。
type fakeConfigurableAgent struct {
	pending       []agentinterfaces.AgentRail
	registered    []agentinterfaces.AgentRail
	registerCalls []agentinterfaces.AgentRail
}

func (a *fakeConfigurableAgent) FindRailsByType(railTypes ...reflect.Type) []agentinterfaces.AgentRail {
	var result []agentinterfaces.AgentRail
	for _, rail := range append(append([]agentinterfaces.AgentRail{}, a.pending...), a.registered...) {
		railType := reflect.TypeOf(rail)
		for _, want := range railTypes {
			if railType == want {
				result = append(result, rail)
				break
			}
		}
	}
	return result
}

func (a *fakeConfigurableAgent) AddRail(rail agentinterfaces.AgentRail) {
	a.pending = append(a.pending, rail)
}

func (a *fakeConfigurableAgent) StripRailsByType(railTypes ...reflect.Type) int {
	removed := 0
	var kept []agentinterfaces.AgentRail
	for _, rail := range a.pending {
		railType := reflect.TypeOf(rail)
		match := false
		for _, want := range railTypes {
			if railType == want {
				match = true
				break
			}
		}
		if match {
			removed++
		} else {
			kept = append(kept, rail)
		}
	}
	a.pending = kept
	return removed
}

func (a *fakeConfigurableAgent) PendingRails() []agentinterfaces.AgentRail {
	result := make([]agentinterfaces.AgentRail, len(a.pending))
	copy(result, a.pending)
	return result
}

func (a *fakeConfigurableAgent) RemovePendingRail(rail agentinterfaces.AgentRail) {
	var kept []agentinterfaces.AgentRail
	for _, queued := range a.pending {
		if queued != rail {
			kept = append(kept, queued)
		}
	}
	a.pending = kept
}

func (a *fakeConfigurableAgent) RegisterRail(_ context.Context, rail agentinterfaces.AgentRail) error {
	a.registerCalls = append(a.registerCalls, rail)
	a.registered = append(a.registered, rail)
	return nil
}

// newConfigureFixture 构造 configure 测试夹具（mock model + 空 processor）。
func newConfigureFixture(t *testing.T) (*fakeConfigurableAgent, *llm.Model, *trajectory.TrajectorySpanProcessor) {
	t.Helper()
	model, err := llm.NewModel(&llmschema.ModelClientConfig{
		ClientProvider: "OpenAI",
		ClientID:       "cfg-test-client",
		APIKey:         "k",
		APIBase:        "http://localhost:1",
	}, nil)
	require.NoError(t, err)
	return &fakeConfigurableAgent{}, model, nil
}

// ──────────────────────────── 用例 ────────────────────────────

// TestConfigureSkillEvolution_互斥守卫 已配置 team rail 后再配 regular 报错，
// 反向亦然。
// Python: _ensure_no_cross_mode_conflict（configuration.py:208-219）。
func TestConfigureSkillEvolution_互斥守卫(t *testing.T) {
	agent, model, processor := newConfigureFixture(t)

	require.NoError(t, ConfigureSkillEvolution(agent, []string{"/tmp/skills"}, model, "m", processor, true, nil, false, "cn", false, false, nil))
	err := ConfigureSkillEvolution(agent, []string{"/tmp/skills"}, model, "m", processor, false, nil, false, "cn", false, false, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "TeamSkillEvolutionRail is already configured")
}

// TestConfigureSkillEvolution_幂等复用 同配置重复调用不新建 rail；
// 换配置报 config mismatch。
// Python: configure_skill_evolution 幂等语义 + _validate_evolution_rail_config。
func TestConfigureSkillEvolution_幂等复用(t *testing.T) {
	agent, model, processor := newConfigureFixture(t)

	require.NoError(t, ConfigureSkillEvolution(agent, []string{"/tmp/skills"}, model, "m", processor, false, nil, false, "cn", false, false, nil))
	pendingAfterFirst := len(agent.pending)

	require.NoError(t, ConfigureSkillEvolution(agent, []string{"/tmp/skills"}, model, "m", processor, false, nil, false, "cn", false, false, nil))
	assert.Len(t, agent.pending, pendingAfterFirst, "同配置重复调用不应新建 rail")

	err := ConfigureSkillEvolution(agent, []string{"/tmp/skills"}, model, "m", processor, false, nil, true, "cn", false, false, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Evolution rail config mismatch")
}

// TestConfigureSkillEvolution_入栈顺序 新建时中断 rail 先于演化 rail 入栈。
// Python: _add_new_rail_stack（configuration.py:260-268）。
func TestConfigureSkillEvolution_入栈顺序(t *testing.T) {
	agent, model, processor := newConfigureFixture(t)

	require.NoError(t, ConfigureSkillEvolution(agent, []string{"/tmp/skills"}, model, "m", processor, false, nil, false, "cn", false, false, nil))
	require.Len(t, agent.pending, 2)
	_, isInterrupt := agent.pending[0].(*EvolutionInterruptRail)
	assert.True(t, isInterrupt, "中断 rail 应先入栈")
	_, isEvolution := agent.pending[1].(*SkillEvolutionRail)
	assert.True(t, isEvolution, "演化 rail 应后入栈")
}

// TestConfigureSkillEvolutionRuntime_注册顺序 runtime 变体把本次新增 rail
// 按依赖顺序立即注册并摘出排队队列。
// Python: configure_skill_evolution_runtime（configuration.py:115-141）。
func TestConfigureSkillEvolutionRuntime_注册顺序(t *testing.T) {
	agent, model, processor := newConfigureFixture(t)

	require.NoError(t, ConfigureSkillEvolutionRuntime(agent, []string{"/tmp/skills"}, model, "m", processor, false, nil, false, "cn", false, false, nil))
	require.Len(t, agent.registerCalls, 2)
	_, firstIsInterrupt := agent.registerCalls[0].(*EvolutionInterruptRail)
	assert.True(t, firstIsInterrupt, "中断 rail 应先注册")
	assert.Empty(t, agent.pending, "已注册 rail 应从排队队列摘除")
}

// TestUnconfigureSkillEvolution_模式分派 nil 移除全部；false 不动 team rail。
// Python: unconfigure_skill_evolution（configuration.py:412-444）。
func TestUnconfigureSkillEvolution_模式分派(t *testing.T) {
	agent, model, processor := newConfigureFixture(t)
	falseVal := false
	trueVal := true

	require.NoError(t, ConfigureSkillEvolution(agent, []string{"/tmp/skills"}, model, "m", processor, false, nil, false, "cn", false, false, nil))
	// team=nil：移除全部（regular + interrupt = 2）
	assert.Equal(t, 2, UnconfigureSkillEvolution(agent, nil))
	assert.Empty(t, agent.pending)

	require.NoError(t, ConfigureSkillEvolution(agent, []string{"/tmp/skills"}, model, "m", processor, true, nil, false, "cn", false, false, nil))
	assert.Equal(t, 2, UnconfigureSkillEvolution(agent, &trueVal))
	assert.Empty(t, agent.pending)

	require.NoError(t, ConfigureSkillEvolution(agent, []string{"/tmp/skills"}, model, "m", processor, true, nil, false, "cn", false, false, nil))
	// falseVal 移除集含 EvolutionInterruptRail：team 场景下中断 rail 被移除（1），team 演进 rail 保留
	assert.Equal(t, 1, UnconfigureSkillEvolution(agent, &falseVal))
	assert.Len(t, agent.pending, 1)
}

// TestReviewRuntime复用 未显式传 runtime 时从既有 rail 取回同一实例。
// Python: review_runtime or _review_runtime_from_rail(target_rail)（configuration.py:69）。
func TestReviewRuntime复用(t *testing.T) {
	agent, model, processor := newConfigureFixture(t)

	require.NoError(t, ConfigureSkillEvolution(agent, []string{"/tmp/skills"}, model, "m", processor, false, nil, false, "cn", false, false, nil))
	var rail *SkillEvolutionRail
	for _, r := range agent.pending {
		if sr, ok := r.(*SkillEvolutionRail); ok {
			rail = sr
		}
	}
	require.NotNil(t, rail)
	firstRuntime := rail.ReviewRuntime()
	require.NotNil(t, firstRuntime)

	require.NoError(t, ConfigureSkillEvolution(agent, []string{"/tmp/skills"}, model, "m", processor, false, nil, false, "cn", false, false, nil))
	assert.Same(t, firstRuntime, rail.ReviewRuntime(), "重复 configure 应复用既有 runtime")
}
