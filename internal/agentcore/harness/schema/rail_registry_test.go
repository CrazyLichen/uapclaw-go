package schema_test

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
)

// TestRegistry_先注册后查 注册后能查到，params 原样透传给 provider。
//
// 对齐上游 register_rail_provider：`_RAIL_PROVIDER_REGISTRY[name] = factory`
// 与 RailSpec.build 的 `factory(dict(self.params), context)`。
func TestRegistry_先注册后查(t *testing.T) {
	hschema.ResetRailRegistryForTest()
	t.Cleanup(hschema.ResetRailRegistryForTest)

	var gotParams map[string]any
	hschema.RegisterRailProvider("test.rail", func(
		params map[string]any, ctx hschema.BuildContextInterface,
	) (any, error) {
		gotParams = params
		return "built", nil
	})

	provider, ok := hschema.LookupRailProvider("test.rail")
	require.True(t, ok, "注册后应能查到")

	out, err := provider(map[string]any{"k": "v"}, &hschema.BuildContext{})
	require.NoError(t, err)
	assert.Equal(t, "built", out)
	assert.Equal(t, "v", gotParams["k"], "params 应原样传给 provider")
}

// TestRegistry_同名覆盖 重复注册不报错，后者生效。
//
// 对齐上游 docstring："a later registration for the same name overrides it"。
func TestRegistry_同名覆盖(t *testing.T) {
	hschema.ResetRailRegistryForTest()
	t.Cleanup(hschema.ResetRailRegistryForTest)

	hschema.RegisterRailProvider("dup.rail", func(
		map[string]any, hschema.BuildContextInterface,
	) (any, error) {
		return "first", nil
	})
	hschema.RegisterRailProvider("dup.rail", func(
		map[string]any, hschema.BuildContextInterface,
	) (any, error) {
		return "second", nil
	})

	provider, ok := hschema.LookupRailProvider("dup.rail")
	require.True(t, ok)
	out, err := provider(nil, &hschema.BuildContext{})
	require.NoError(t, err)
	assert.Equal(t, "second", out, "后者覆盖前者")
}

// TestRegistry_未注册返回false 不存在时返回 (nil,false)。
func TestRegistry_未注册返回false(t *testing.T) {
	hschema.ResetRailRegistryForTest()
	t.Cleanup(hschema.ResetRailRegistryForTest)

	provider, ok := hschema.LookupRailProvider("nonexistent")

	assert.False(t, ok)
	assert.Nil(t, provider)
}

// TestRegistry_并发读写 多协程读 + 单协程注册不 race（-race 下验证）。
//
// Go 特有：Python 单线程 asyncio 天然无此问题，Go 必须显式加锁。
func TestRegistry_并发读写(t *testing.T) {
	hschema.ResetRailRegistryForTest()
	t.Cleanup(hschema.ResetRailRegistryForTest)

	const readers = 8
	var wg sync.WaitGroup
	wg.Add(readers + 1)

	hschema.RegisterRailProvider("base.rail", func(
		map[string]any, hschema.BuildContextInterface,
	) (any, error) {
		return nil, nil
	})

	for i := 0; i < readers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_, _ = hschema.LookupRailProvider("base.rail")
			}
		}()
	}
	go func() {
		defer wg.Done()
		for j := 0; j < 100; j++ {
			hschema.RegisterRailProvider("dynamic.rail", func(
				map[string]any, hschema.BuildContextInterface,
			) (any, error) {
				return nil, nil
			})
		}
	}()

	wg.Wait()
	_, ok := hschema.LookupRailProvider("dynamic.rail")
	assert.True(t, ok, "并发注册后应可查到")
}

// TestRegistry_RegisteredRailNames 返回已注册的全部名字。
func TestRegistry_RegisteredRailNames(t *testing.T) {
	hschema.ResetRailRegistryForTest()
	t.Cleanup(hschema.ResetRailRegistryForTest)

	hschema.RegisterRailProvider("a.rail", func(
		map[string]any, hschema.BuildContextInterface,
	) (any, error) {
		return nil, nil
	})
	hschema.RegisterRailProvider("b.rail", func(
		map[string]any, hschema.BuildContextInterface,
	) (any, error) {
		return nil, nil
	})

	names := hschema.RegisteredRailNames()

	assert.Len(t, names, 2)
	assert.ElementsMatch(t, []string{"a.rail", "b.rail"}, names)
}

// TestRegistry_Reset清空 Reset 后查不到任何东西。
func TestRegistry_Reset清空(t *testing.T) {
	hschema.RegisterRailProvider("will.be.cleared", func(
		map[string]any, hschema.BuildContextInterface,
	) (any, error) {
		return nil, nil
	})

	hschema.ResetRailRegistryForTest()

	_, ok := hschema.LookupRailProvider("will.be.cleared")
	assert.False(t, ok)
	assert.Empty(t, hschema.RegisteredRailNames())
}
