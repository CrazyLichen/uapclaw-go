package config

import (
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// DEFAULT_RUNNER_CONFIG 默认 Runner 配置。
	// distributed_mode=false，消息队列类型=FAKE。
	//
	// 对应 Python: DEFAULT_RUNNER_CONFIG
	DEFAULT_RUNNER_CONFIG = &RunnerConfig{
		DistributedMode: false,
		DistributedConfig: &DistributedConfig{
			RequestTimeout:       30.0,
			MaxRequestConcurrency: 10000,
			MessageQueueConfig: MessageQueueConfig{
				Type: MessageQueueTypeFake,
			},
			AgentTopicTemplate: "openjiuwen.single_agent.{agent_id}.{version}",
			ReplyTopicTemplate: "openjiuwen.reply.runner.{instance_id}",
		},
		EnvPrefix:              "",
		InstanceID:             "",
		CheckpointerConfig:     nil,
		EnableSessionController: false,
		EnableA2A:              false,
	}

	// globalConfig 全局 Runner 配置单例
	globalConfig *RunnerConfig
	// globalConfigMu 保护 globalConfig 的并发读写
	globalConfigMu sync.RWMutex
)

// ──────────────────────────── 导出函数 ────────────────────────────

// SetRunnerConfig 设置全局 Runner 配置。
//
// 对应 Python: set_runner_config(cfg)
func SetRunnerConfig(cfg *RunnerConfig) {
	globalConfigMu.Lock()
	defer globalConfigMu.Unlock()
	globalConfig = cfg

	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "runner_config_set").
		Bool("distributed_mode", cfg.DistributedMode).
		Bool("enable_session_controller", cfg.EnableSessionController).
		Bool("enable_a2a", cfg.EnableA2A).
		Msg("Runner 全局配置已设置")
}

// GetRunnerConfig 获取全局 Runner 配置。
// 若尚未设置，则返回 DEFAULT_RUNNER_CONFIG 的深拷贝。
//
// 对应 Python: get_runner_config()
func GetRunnerConfig() *RunnerConfig {
	globalConfigMu.RLock()
	if globalConfig != nil {
		cfg := globalConfig
		globalConfigMu.RUnlock()
		return cfg
	}
	globalConfigMu.RUnlock()

	// 首次访问，用默认配置初始化
	globalConfigMu.Lock()
	defer globalConfigMu.Unlock()

	// 双重检查
	if globalConfig != nil {
		return globalConfig
	}

	globalConfig = cloneDefaultConfig()
	return globalConfig
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// cloneDefaultConfig 深拷贝 DEFAULT_RUNNER_CONFIG。
//
// 对应 Python: DEFAULT_RUNNER_CONFIG.model_copy(deep=True)
func cloneDefaultConfig() *RunnerConfig {
	cfg := *DEFAULT_RUNNER_CONFIG
	if DEFAULT_RUNNER_CONFIG.DistributedConfig != nil {
		dc := *DEFAULT_RUNNER_CONFIG.DistributedConfig
		mq := dc.MessageQueueConfig
		if DEFAULT_RUNNER_CONFIG.DistributedConfig.MessageQueueConfig.PulsarConfig != nil {
			pc := *DEFAULT_RUNNER_CONFIG.DistributedConfig.MessageQueueConfig.PulsarConfig
			mq.PulsarConfig = &pc
		}
		dc.MessageQueueConfig = mq
		cfg.DistributedConfig = &dc
	}
	if DEFAULT_RUNNER_CONFIG.CheckpointerConfig != nil {
		cc := *DEFAULT_RUNNER_CONFIG.CheckpointerConfig
		cfg.CheckpointerConfig = &cc
	}
	return &cfg
}
