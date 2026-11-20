package schema_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
)

// TestBuiltinToolSpec_Build_已注册provider被调用 注册命中时 provider 被调用，
// params 原样透传、返回值原样上抛。
//
// Python: BuiltinToolSpec.build (deep_agent_spec.py:307-337)：
// `factory = _TOOL_PROVIDER_REGISTRY.get(self.type)` → `factory(dict(self.params), context)`。
func TestBuiltinToolSpec_Build_已注册provider被调用(t *testing.T) {
	hschema.ResetToolRegistryForTest()
	t.Cleanup(hschema.ResetToolRegistryForTest)

	var seenParams map[string]any
	hschema.RegisterToolProvider("swarm.demo", func(
		params map[string]any, ctx hschema.BuildContextInterface,
	) ([]any, error) {
		seenParams = params
		return []any{"tool-instance"}, nil
	})

	spec := hschema.BuiltinToolSpec{Type: "swarm.demo", Params: map[string]any{"k": "v"}}
	out, err := spec.Build("cn", "card1.swarm.demo", nil)

	require.NoError(t, err)
	assert.Equal(t, []any{"tool-instance"}, out)
	assert.Equal(t, map[string]any{"k": "v"}, seenParams)
}

// TestBuiltinToolSpec_Build_未知type报错 未知 tool 类型直接报错。
//
// 注意与 RailSpec 语义不同：RailSpec.build warn+skip（:283-289），
// 而 BuiltinToolSpec.build **raise ValueError**（deep_agent_spec.py:330-334）：
// `raise ValueError(f"Unknown tool type '{self.type}'. Registered types: ...")`。
func TestBuiltinToolSpec_Build_未知type报错(t *testing.T) {
	hschema.ResetToolRegistryForTest()
	t.Cleanup(hschema.ResetToolRegistryForTest)

	hschema.RegisterToolProvider("swarm.known", func(
		map[string]any, hschema.BuildContextInterface,
	) ([]any, error) {
		return nil, nil
	})

	spec := hschema.BuiltinToolSpec{Type: "swarm.removed", Params: map[string]any{}}
	out, err := spec.Build("cn", "x", nil)

	require.Error(t, err)
	assert.Nil(t, out)
	assert.Contains(t, err.Error(), "Unknown tool type 'swarm.removed'")
	assert.Contains(t, err.Error(), "Registered types:")
	assert.Contains(t, err.Error(), "swarm.known")
}

// TestBuiltinToolSpec_Build_provider返回err上抛 provider 报错时原样上抛。
func TestBuiltinToolSpec_Build_provider返回err上抛(t *testing.T) {
	hschema.ResetToolRegistryForTest()
	t.Cleanup(hschema.ResetToolRegistryForTest)

	sentinel := errors.New("boom")
	hschema.RegisterToolProvider("swarm.err", func(
		map[string]any, hschema.BuildContextInterface,
	) ([]any, error) {
		return nil, sentinel
	})

	spec := hschema.BuiltinToolSpec{Type: "swarm.err"}
	_, err := spec.Build("cn", "x", nil)
	require.ErrorIs(t, err, sentinel)
}

// TestSubAgentSpec_Build_factory命中直返 factory_name 命中注册表时
// 直接返回 provider 结果，不走正常路径。
//
// Python: deep_agent_spec.py:383-384。
func TestSubAgentSpec_Build_factory命中直返(t *testing.T) {
	hschema.ResetSubagentRegistryForTest()
	t.Cleanup(hschema.ResetSubagentRegistryForTest)

	called := false
	want := &hschema.SubAgentConfig{}
	hschema.RegisterSubagentProvider("swarm.hit", func(
		kwargs map[string]any, ctx hschema.BuildContextInterface,
	) (*hschema.SubAgentConfig, error) {
		called = true
		return want, nil
	})

	spec := hschema.SubAgentSpec{
		FactoryName:   "swarm.hit",
		FactoryKwargs: map[string]any{"language": "zh"},
	}
	got, err := spec.Build(nil, "cn", nil)

	require.NoError(t, err)
	assert.Same(t, want, got, "命中时应直接返回 provider 产物")
	assert.True(t, called)
}

// TestSubAgentSpec_Build_factory未命中落正常路径 factory_name 有值但未注册
// 时按 Python if 条件不成立的语义落到正常路径（provider 不被调用）。
func TestSubAgentSpec_Build_factory未命中落正常路径(t *testing.T) {
	hschema.ResetSubagentRegistryForTest()
	t.Cleanup(hschema.ResetSubagentRegistryForTest)

	called := false
	hschema.RegisterSubagentProvider("swarm.other", func(
		map[string]any, hschema.BuildContextInterface,
	) (*hschema.SubAgentConfig, error) {
		called = true
		return nil, nil
	})

	spec := hschema.SubAgentSpec{FactoryName: "swarm.missing", AgentCard: nil}
	_, err := spec.Build(nil, "cn", nil)

	assert.False(t, called, "未命中时不得调用任何 provider")
	assert.NoError(t, err, "正常路径 None card 与上游一致不报错（SubAgentConfig 允许 card=None）")
}
