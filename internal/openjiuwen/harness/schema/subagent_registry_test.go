package schema_test

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hschema "github.com/uapclaw/uapclaw-go/internal/openjiuwen/harness/schema"
)

// TestSubagentRegistry_先注册后查 注册后能查到，factory_kwargs 原样透传。
//
// 对齐上游查表分支：
// `_SUBAGENT_PROVIDER_REGISTRY[self.factory_name](dict(self.factory_kwargs), context)`
// （deep_agent_spec.py:383-384）。
func TestSubagentRegistry_先注册后查(t *testing.T) {
	hschema.ResetSubagentRegistryForTest()
	t.Cleanup(hschema.ResetSubagentRegistryForTest)

	var gotKwargs map[string]any
	hschema.RegisterSubagentProvider("swarm.browser_agent", func(
		kwargs map[string]any, ctx hschema.BuildContextInterface,
	) (*hschema.SubAgentConfig, error) {
		gotKwargs = kwargs
		return &hschema.SubAgentConfig{}, nil
	})

	provider, ok := hschema.LookupSubagentProvider("swarm.browser_agent")
	require.True(t, ok, "注册后应能查到")

	cfg, err := provider(map[string]any{"language": "zh"}, nil)
	require.NoError(t, err)
	assert.NotNil(t, cfg)
	assert.Equal(t, map[string]any{"language": "zh"}, gotKwargs, "kwargs 应原样透传")
}

// TestSubagentRegistry_覆盖注册幂等 同名注册后者覆盖前者。
func TestSubagentRegistry_覆盖注册幂等(t *testing.T) {
	hschema.ResetSubagentRegistryForTest()
	t.Cleanup(hschema.ResetSubagentRegistryForTest)

	hschema.RegisterSubagentProvider("swarm.dup", func(
		map[string]any, hschema.BuildContextInterface,
	) (*hschema.SubAgentConfig, error) {
		return nil, assert.AnError
	})
	hschema.RegisterSubagentProvider("swarm.dup", func(
		map[string]any, hschema.BuildContextInterface,
	) (*hschema.SubAgentConfig, error) {
		return &hschema.SubAgentConfig{}, nil
	})

	provider, ok := hschema.LookupSubagentProvider("swarm.dup")
	require.True(t, ok)
	cfg, err := provider(nil, nil)
	require.NoError(t, err)
	assert.NotNil(t, cfg)
}

// TestSubagentRegistry_未知factory查空 未注册名返回 (nil, false)。
func TestSubagentRegistry_未知factory查空(t *testing.T) {
	hschema.ResetSubagentRegistryForTest()
	t.Cleanup(hschema.ResetSubagentRegistryForTest)

	_, ok := hschema.LookupSubagentProvider("swarm.missing")
	assert.False(t, ok)
}

// TestSubagentRegistry_并发读写 注册与并发查表不产生数据竞争（-race 覆盖）。
func TestSubagentRegistry_并发读写(t *testing.T) {
	hschema.ResetSubagentRegistryForTest()
	t.Cleanup(hschema.ResetSubagentRegistryForTest)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		name := "swarm.race"
		wg.Add(1)
		go func() {
			defer wg.Done()
			hschema.RegisterSubagentProvider(name, func(
				map[string]any, hschema.BuildContextInterface,
			) (*hschema.SubAgentConfig, error) {
				return nil, nil
			})
			_, _ = hschema.LookupSubagentProvider(name)
		}()
	}
	wg.Wait()
}
