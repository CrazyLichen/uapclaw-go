package schema_test

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
)

// TestToolRegistry_先注册后查 注册后能查到，params 原样透传给 provider。
//
// 对齐上游 register_tool_provider：
// `_TOOL_PROVIDER_REGISTRY[name] = factory`（deep_agent_spec.py:81-88）。
func TestToolRegistry_先注册后查(t *testing.T) {
	hschema.ResetToolRegistryForTest()
	t.Cleanup(hschema.ResetToolRegistryForTest)

	var gotParams map[string]any
	hschema.RegisterToolProvider("swarm.demo", func(
		params map[string]any, ctx hschema.BuildContextInterface,
	) ([]any, error) {
		gotParams = params
		return []any{"demo-tool"}, nil
	})

	provider, ok := hschema.LookupToolProvider("swarm.demo")
	require.True(t, ok, "注册后应能查到")

	tools, err := provider(map[string]any{"k": "v"}, nil)
	require.NoError(t, err)
	assert.Equal(t, []any{"demo-tool"}, tools)
	assert.Equal(t, map[string]any{"k": "v"}, gotParams, "params 应原样透传")
}

// TestToolRegistry_覆盖注册幂等 同名注册后者覆盖前者。
//
// Python: _TOOL_PROVIDER_REGISTRY[name] = factory（dict 赋值即覆盖）。
func TestToolRegistry_覆盖注册幂等(t *testing.T) {
	hschema.ResetToolRegistryForTest()
	t.Cleanup(hschema.ResetToolRegistryForTest)

	hschema.RegisterToolProvider("swarm.dup", func(
		map[string]any, hschema.BuildContextInterface,
	) ([]any, error) {
		return nil, nil
	})
	hschema.RegisterToolProvider("swarm.dup", func(
		map[string]any, hschema.BuildContextInterface,
	) ([]any, error) {
		return []any{"second"}, nil
	})

	provider, ok := hschema.LookupToolProvider("swarm.dup")
	require.True(t, ok)
	tools, err := provider(nil, nil)
	require.NoError(t, err)
	assert.Equal(t, []any{"second"}, tools)
}

// TestToolRegistry_未知类型查空 未注册名返回 (nil, false)。
func TestToolRegistry_未知类型查空(t *testing.T) {
	hschema.ResetToolRegistryForTest()
	t.Cleanup(hschema.ResetToolRegistryForTest)

	_, ok := hschema.LookupToolProvider("swarm.missing")
	assert.False(t, ok)
}

// TestToolRegistry_并发读写 注册与并发查表不产生数据竞争（-race 覆盖）。
//
// Go 特有：Python 单线程无此问题；RWMutex 必须真的生效。
func TestToolRegistry_并发读写(t *testing.T) {
	hschema.ResetToolRegistryForTest()
	t.Cleanup(hschema.ResetToolRegistryForTest)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		name := "swarm.race"
		wg.Add(1)
		go func() {
			defer wg.Done()
			hschema.RegisterToolProvider(name, func(
				map[string]any, hschema.BuildContextInterface,
			) ([]any, error) {
				return nil, nil
			})
			_, _ = hschema.LookupToolProvider(name)
		}()
	}
	wg.Wait()
}
