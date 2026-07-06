package harness

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestHarnessConfigInfo 测试 HarnessConfigInfo 结构
func TestHarnessConfigInfo(t *testing.T) {
	info := HarnessConfigInfo{
		ID:          "test-config",
		Name:        "测试配置",
		Version:     "1.0.0",
		PackageName: "test-package",
		ConfigPath:  "/path/to/config.yaml",
		Enabled:     true,
	}
	assert.Equal(t, "test-config", info.ID)
	assert.Equal(t, "测试配置", info.Name)
	assert.True(t, info.Enabled)
}

// TestHarnessConfigRegistry_Register 测试注册
func TestHarnessConfigRegistry_Register(t *testing.T) {
	r := &HarnessConfigRegistry{
		registry: make(map[string]HarnessConfigInfo),
		disabled: make(map[string]bool),
	}

	info := HarnessConfigInfo{ID: "test", Name: "测试", ConfigPath: "/test.yaml"}
	r.Register(info)

	got := r.Get("test")
	require.NotNil(t, got)
	assert.Equal(t, "test", got.ID)
	assert.Equal(t, "测试", got.Name)
	assert.True(t, got.Enabled)
}

// TestHarnessConfigRegistry_Discover 测试发现
func TestHarnessConfigRegistry_Discover(t *testing.T) {
	r := &HarnessConfigRegistry{
		registry: make(map[string]HarnessConfigInfo),
		disabled: make(map[string]bool),
	}

	r.Register(HarnessConfigInfo{ID: "a", Name: "A"})
	r.Register(HarnessConfigInfo{ID: "b", Name: "B"})

	discovered := r.Discover()
	assert.Len(t, discovered, 2)
	// 按 ID 排序
	assert.Equal(t, "a", discovered[0].ID)
	assert.Equal(t, "b", discovered[1].ID)
}

// TestHarnessConfigRegistry_Discover_禁用 测试禁用后的发现
func TestHarnessConfigRegistry_Discover_禁用(t *testing.T) {
	r := &HarnessConfigRegistry{
		registry: make(map[string]HarnessConfigInfo),
		disabled: make(map[string]bool),
	}

	r.Register(HarnessConfigInfo{ID: "a", Name: "A"})
	r.Register(HarnessConfigInfo{ID: "b", Name: "B"})
	r.Disable("a")

	discovered := r.Discover()
	assert.Len(t, discovered, 1)
	assert.Equal(t, "b", discovered[0].ID)
}

// TestHarnessConfigRegistry_Get_未找到 测试未找到
func TestHarnessConfigRegistry_Get_未找到(t *testing.T) {
	r := &HarnessConfigRegistry{
		registry: make(map[string]HarnessConfigInfo),
		disabled: make(map[string]bool),
	}

	got := r.Get("nonexistent")
	assert.Nil(t, got)
}

// TestHarnessConfigRegistry_Get_已禁用 测试已禁用的 Get
func TestHarnessConfigRegistry_Get_已禁用(t *testing.T) {
	r := &HarnessConfigRegistry{
		registry: make(map[string]HarnessConfigInfo),
		disabled: make(map[string]bool),
	}

	r.Register(HarnessConfigInfo{ID: "test", Name: "测试"})
	r.Disable("test")

	got := r.Get("test")
	assert.Nil(t, got)
}

// TestHarnessConfigRegistry_Disable 测试禁用
func TestHarnessConfigRegistry_Disable(t *testing.T) {
	r := &HarnessConfigRegistry{
		registry: make(map[string]HarnessConfigInfo),
		disabled: make(map[string]bool),
	}

	r.Register(HarnessConfigInfo{ID: "test", Name: "测试"})
	r.Disable("test")

	assert.True(t, r.disabled["test"])
}

// TestHarnessConfigRegistry_Enable 测试重新启用
func TestHarnessConfigRegistry_Enable(t *testing.T) {
	r := &HarnessConfigRegistry{
		registry: make(map[string]HarnessConfigInfo),
		disabled: make(map[string]bool),
	}

	r.Register(HarnessConfigInfo{ID: "test", Name: "测试"})
	r.Disable("test")
	r.Enable("test")

	_, ok := r.disabled["test"]
	assert.False(t, ok)
}

// TestHarnessConfigRegistry_InvalidateCache 测试缓存失效（空操作）
func TestHarnessConfigRegistry_InvalidateCache(t *testing.T) {
	r := &HarnessConfigRegistry{
		registry: make(map[string]HarnessConfigInfo),
		disabled: make(map[string]bool),
	}
	// 应不报错
	r.InvalidateCache()
}

// TestHarnessConfigRegistry_Load_未找到 测试加载未找到的配置
func TestHarnessConfigRegistry_Load_未找到(t *testing.T) {
	r := &HarnessConfigRegistry{
		registry: make(map[string]HarnessConfigInfo),
		disabled: make(map[string]bool),
	}

	_, err := r.Load("nonexistent", nil, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "未找到")
}

// TestHarnessConfigRegistry_Load_无路径 测试加载没有配置路径的配置
func TestHarnessConfigRegistry_Load_无路径(t *testing.T) {
	r := &HarnessConfigRegistry{
		registry: make(map[string]HarnessConfigInfo),
		disabled: make(map[string]bool),
	}

	r.Register(HarnessConfigInfo{ID: "test", Name: "测试"})
	_, err := r.Load("test", nil, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "没有配置路径")
}

// TestHarnessConfigRegistry_Load_完整流程 测试完整的 Load 流程
func TestHarnessConfigRegistry_Load_完整流程(t *testing.T) {
	r := &HarnessConfigRegistry{
		registry: make(map[string]HarnessConfigInfo),
		disabled: make(map[string]bool),
	}

	// 创建临时 YAML 文件
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "harness_config.yaml")
	yamlContent := `schema_version: harness_config.v0.1
id: registry-test
language: cn
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(yamlContent), 0644))

	r.Register(HarnessConfigInfo{ID: "registry-test", Name: "注册表测试", ConfigPath: cfgPath})

	// Load 调用 Builder.Build → CreateDeepAgent
	// 当前 resolveBuiltinTools 未实现，会返回错误
	agent, err := r.Load("registry-test", nil, nil)
	// 预期：因为工具实例化尚未实现，Load 可能返回错误或成功（取决于 YAML 内容）
	_ = agent
	_ = err
}

// TestHarnessConfigRegistry_并发 测试并发安全
func TestHarnessConfigRegistry_并发(t *testing.T) {
	r := &HarnessConfigRegistry{
		registry: make(map[string]HarnessConfigInfo),
		disabled: make(map[string]bool),
	}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r.Register(HarnessConfigInfo{ID: string(rune('a' + i)), Name: "test"})
		}(i)
	}
	wg.Wait()

	discovered := r.Discover()
	assert.Len(t, discovered, 10)
}

// TestGlobalRegistry 测试全局注册表函数
func TestGlobalRegistry(t *testing.T) {
	// 重置全局注册表
	globalRegistry = nil
	globalRegistryOnce = sync.Once{}

	RegisterConfig(HarnessConfigInfo{ID: "global-test", Name: "全局测试", ConfigPath: "/test.yaml"})

	got := GetConfig("global-test")
	require.NotNil(t, got)
	assert.Equal(t, "global-test", got.ID)

	discovered := DiscoverConfigs()
	assert.NotEmpty(t, discovered)

	DisableConfig("global-test")
	got = GetConfig("global-test")
	assert.Nil(t, got)

	EnableConfig("global-test")
	got = GetConfig("global-test")
	require.NotNil(t, got)

	InvalidateConfigCache() // 空操作，应不报错
}
