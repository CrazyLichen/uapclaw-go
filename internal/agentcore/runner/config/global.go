package config

import (
	"sync"

	"github.com/mohae/deepcopy"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// DEFAULT_RUNNER_CONFIG 默认 Runner 配置。
	// distributed_mode=false，消息队列类型=FAKE。
	// InstanceID 自动生成 UUID，对齐 Python DEFAULT_RUNNER_CONFIG。
	//
	// 对应 Python: DEFAULT_RUNNER_CONFIG
	DEFAULT_RUNNER_CONFIG = &RunnerConfig{
		DistributedMode: false,
		DistributedConfig: &DistributedConfig{
			RequestTimeout:        30.0,
			MaxRequestConcurrency: 10000,
			MessageQueueConfig: MessageQueueConfig{
				Type: MessageQueueTypeFake,
			},
			AgentTopicTemplate: "openjiuwen.single_agent.{agent_id}.{version}",
			ReplyTopicTemplate: "openjiuwen.reply.runner.{instance_id}",
		},
		EnvPrefix:               "",
		InstanceID:              generateInstanceID(),
		CheckpointerConfig:      nil,
		EnableSessionController: false,
		EnableA2A:               false,
	}

	// globalConfig 全局 Runner 配置单例
	globalConfig *RunnerConfig
	// globalConfigMu 保护 globalConfig 的并发读写
	globalConfigMu sync.RWMutex
)

// SetRunnerConfig 设置全局 Runner 配置。
//
// 对应 Python: set_runner_config(cfg)
// ──────────────────────────── 导出函数 ────────────────────────────

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

// cloneDefaultConfig 深拷贝 DEFAULT_RUNNER_CONFIG。
//
// 对应 Python: DEFAULT_RUNNER_CONFIG.model_copy(deep=True)
// InstanceID 保留原值（深拷贝语义，非重新生成）。
// 使用 deepcopy 库确保 CheckpointerConfig.Conf 等 map[string]any 字段也被深拷贝。
// ──────────────────────────── 非导出函数 ────────────────────────────

func cloneDefaultConfig() *RunnerConfig {
	result := deepcopy.Copy(DEFAULT_RUNNER_CONFIG)
	return result.(*RunnerConfig)
}
