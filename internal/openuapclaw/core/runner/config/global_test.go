package config

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestDEFAULT_RUNNER_CONFIG_默认值 测试 DEFAULT_RUNNER_CONFIG 默认值。
func TestDEFAULT_RUNNER_CONFIG_默认值(t *testing.T) {
	assert.False(t, DEFAULT_RUNNER_CONFIG.DistributedMode)
	assert.NotNil(t, DEFAULT_RUNNER_CONFIG.DistributedConfig)
	assert.Equal(t, MessageQueueTypeFake, DEFAULT_RUNNER_CONFIG.DistributedConfig.MessageQueueConfig.Type)
	assert.Equal(t, 30.0, DEFAULT_RUNNER_CONFIG.DistributedConfig.RequestTimeout)
	assert.Equal(t, 10000, DEFAULT_RUNNER_CONFIG.DistributedConfig.MaxRequestConcurrency)
	assert.False(t, DEFAULT_RUNNER_CONFIG.EnableSessionController)
	assert.False(t, DEFAULT_RUNNER_CONFIG.EnableA2A)
}

// TestSetGetRunnerConfig_基本读写 测试 Set/Get 基本读写。
func TestSetGetRunnerConfig_基本读写(t *testing.T) {
	// 保存原始值并恢复
	globalConfigMu.Lock()
	orig := globalConfig
	globalConfig = nil
	globalConfigMu.Unlock()
	defer func() {
		globalConfigMu.Lock()
		globalConfig = orig
		globalConfigMu.Unlock()
	}()

	// 未设置时 Get 返回默认配置
	cfg := GetRunnerConfig()
	assert.False(t, cfg.DistributedMode)
	assert.NotNil(t, cfg.DistributedConfig)

	// 设置后 Get 返回设置的配置
	custom := &RunnerConfig{
		DistributedMode:         true,
		EnvPrefix:               "test",
		InstanceID:              "test-instance-123",
		EnableSessionController: true,
		EnableA2A:               true,
		DistributedConfig: &DistributedConfig{
			RequestTimeout:        60.0,
			MaxRequestConcurrency: 5000,
			MessageQueueConfig: MessageQueueConfig{
				Type: MessageQueueTypePulsar,
				PulsarConfig: &PulsarConfig{
					URL:        "pulsar://broker:6650",
					MaxWorkers: 16,
				},
			},
		},
	}
	SetRunnerConfig(custom)

	got := GetRunnerConfig()
	assert.True(t, got.DistributedMode)
	assert.Equal(t, "test", got.EnvPrefix)
	assert.Equal(t, "test-instance-123", got.InstanceID)
	assert.True(t, got.EnableSessionController)
	assert.True(t, got.EnableA2A)
	assert.Equal(t, 60.0, got.DistributedConfig.RequestTimeout)
	assert.Equal(t, MessageQueueTypePulsar, got.DistributedConfig.MessageQueueConfig.Type)
	assert.NotNil(t, got.DistributedConfig.MessageQueueConfig.PulsarConfig)
	assert.Equal(t, 16, got.DistributedConfig.MessageQueueConfig.PulsarConfig.MaxWorkers)
}

// TestGetRunnerConfig_并发安全 测试 GetRunnerConfig 并发安全。
func TestGetRunnerConfig_并发安全(t *testing.T) {
	// 保存原始值并恢复
	globalConfigMu.Lock()
	orig := globalConfig
	globalConfig = nil
	globalConfigMu.Unlock()
	defer func() {
		globalConfigMu.Lock()
		globalConfig = orig
		globalConfigMu.Unlock()
	}()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cfg := GetRunnerConfig()
			assert.NotNil(t, cfg)
		}()
	}
	wg.Wait()
}

// TestSetRunnerConfig_并发安全 测试 Set/Get 交替并发安全。
func TestSetRunnerConfig_并发安全(t *testing.T) {
	// 保存原始值并恢复
	globalConfigMu.Lock()
	orig := globalConfig
	globalConfig = nil
	globalConfigMu.Unlock()
	defer func() {
		globalConfigMu.Lock()
		globalConfig = orig
		globalConfigMu.Unlock()
	}()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			SetRunnerConfig(&RunnerConfig{DistributedMode: true, EnvPrefix: "concurrent"})
		}()
		go func() {
			defer wg.Done()
			_ = GetRunnerConfig()
		}()
	}
	wg.Wait()
}

// TestCloneDefaultConfig_深拷贝 测试 cloneDefaultConfig 返回独立的副本。
func TestCloneDefaultConfig_深拷贝(t *testing.T) {
	cfg1 := cloneDefaultConfig()
	cfg2 := cloneDefaultConfig()

	// 修改 cfg1 不影响 cfg2
	cfg1.DistributedMode = true
	cfg1.DistributedConfig.RequestTimeout = 999.0
	assert.False(t, cfg2.DistributedMode)
	assert.Equal(t, 30.0, cfg2.DistributedConfig.RequestTimeout)
}
