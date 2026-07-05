package spawn

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/config"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SpawnAgentConfig Spawn 基础配置。
// 对齐 Python: SpawnAgentConfig (agent_config.py)
type SpawnAgentConfig struct {
	// AgentKind Agent 启动方式
	AgentKind SpawnAgentKind `json:"agent_kind"`
	// RunnerConfig Runner 配置（序列化后传递给子进程）
	RunnerConfig map[string]any `json:"runner_config,omitempty"`
	// LoggingConfig 日志配置
	LoggingConfig map[string]any `json:"logging_config,omitempty"`
	// SessionID 会话 ID
	SessionID string `json:"session_id,omitempty"`
	// Payload 额外数据（TEAM_AGENT 构造用）
	Payload map[string]any `json:"payload"`
}

// ClassAgentSpawnConfig 类 Agent Spawn 配置。
// 对齐 Python: ClassAgentSpawnConfig (agent_config.py)
// Python 传 agent_module + agent_class + init_kwargs，
// Go 传 AgentName + AgentCard + InitKwargs。
// AgentCard 包含 Agent 完整元数据，由主进程从 ResourceMgr 中提取传给子进程，
// 子进程据此通过 AgentCreator.CreateByType() 创建 Agent 实例。
type ClassAgentSpawnConfig struct {
	SpawnAgentConfig
	// AgentName Agent 名称
	AgentName string `json:"agent_name"`
	// AgentCard Agent 完整配置卡片（序列化为 map）。
	// 对齐 Python: ClassAgentSpawnConfig.agent_module + agent_class
	// Go 用 AgentCard 替代，因为 Go 没有 importlib 的 module+class 动态导入。
	AgentCard map[string]any `json:"agent_card,omitempty"`
	// InitKwargs 实例化参数。
	// 对齐 Python: ClassAgentSpawnConfig.init_kwargs
	InitKwargs map[string]any `json:"init_kwargs,omitempty"`
}

// SpawnConfig 子进程管理配置。
// 对齐 Python: SpawnConfig (process_manager.py)
type SpawnConfig struct {
	// HealthCheckInterval 健康检查间隔，默认 5s
	HealthCheckInterval time.Duration
	// ShutdownTimeout 关闭超时，默认 10s
	ShutdownTimeout time.Duration
	// HealthCheckTimeout 健康检查响应超时，默认 3s
	HealthCheckTimeout time.Duration
}

// SpawnAgentKind Agent 启动方式枚举。
// 对齐 Python: SpawnAgentKind (agent_config.py)
type SpawnAgentKind string

// ──────────────────────────── 常量 ────────────────────────────
const (
	// SpawnAgentKindClassAgent 类 Agent 启动（通过 ResourceMgr 注册表查找）
	SpawnAgentKindClassAgent SpawnAgentKind = "class_agent"
	// SpawnAgentKindTeamAgent 团队 Agent 启动（通过 FromSpawnPayload 构造）
	SpawnAgentKindTeamAgent SpawnAgentKind = "team_agent"
)

const (
	// DefaultHealthCheckInterval 默认健康检查间隔
	DefaultHealthCheckInterval = 5 * time.Second
	// DefaultShutdownTimeout 默认关闭超时
	DefaultShutdownTimeout = 10 * time.Second
	// DefaultHealthCheckTimeout 默认健康检查响应超时
	DefaultHealthCheckTimeout = 3 * time.Second
	// DefaultMaxHealthFailures 默认最大连续失败次数
	DefaultMaxHealthFailures = 2
	// ForceTerminateGracePeriod 强制终止宽限期
	ForceTerminateGracePeriod = 3 * time.Second
	// ShutdownWaitPeriod 关闭后等待进程退出的宽限期
	ShutdownWaitPeriod = 2 * time.Second
)

// ──────────────────────────── 导出函数 ────────────────────────────

// DefaultSpawnConfig 返回默认 SpawnConfig。
func DefaultSpawnConfig() SpawnConfig {
	return SpawnConfig{
		HealthCheckInterval: DefaultHealthCheckInterval,
		ShutdownTimeout:     DefaultShutdownTimeout,
		HealthCheckTimeout:  DefaultHealthCheckTimeout,
	}
}

// ParseSpawnAgentConfig 根据 agent_kind 解析为对应配置类型。
// 对齐 Python: parse_spawn_agent_config()
func ParseSpawnAgentConfig(payload map[string]any) (SpawnAgentConfig, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return SpawnAgentConfig{}, fmt.Errorf("序列化配置失败: %w", err)
	}

	agentKind, _ := payload["agent_kind"].(string)
	if agentKind == string(SpawnAgentKindClassAgent) {
		var cfg ClassAgentSpawnConfig
		if err := json.Unmarshal(data, &cfg); err != nil {
			return SpawnAgentConfig{}, fmt.Errorf("解析 ClassAgentSpawnConfig 失败: %w", err)
		}
		return cfg.SpawnAgentConfig, nil
	}

	var cfg SpawnAgentConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return SpawnAgentConfig{}, fmt.Errorf("解析 SpawnAgentConfig 失败: %w", err)
	}
	return cfg, nil
}

// SerializeRunnerConfig 将 RunnerConfig 序列化为 JSON-safe map。
// 对齐 Python: serialize_runner_config()
func SerializeRunnerConfig(cfg *config.RunnerConfig) (map[string]any, error) {
	data, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("序列化 RunnerConfig 失败: %w", err)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("转换 RunnerConfig 为 map 失败: %w", err)
	}
	return result, nil
}

// DeserializeRunnerConfig 从 JSON-safe map 反序列化为 RunnerConfig。
// 对齐 Python: deserialize_runner_config()
func DeserializeRunnerConfig(payload map[string]any) (*config.RunnerConfig, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("序列化 RunnerConfig payload 失败: %w", err)
	}
	var cfg config.RunnerConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("反序列化 RunnerConfig 失败: %w", err)
	}
	return &cfg, nil
}
