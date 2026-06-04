package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentServerConfig AgentServer 服务配置。
type AgentServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

// GatewayConfig Gateway 服务配置。
type GatewayConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

// ServerConfig 服务配置（包含 AgentServer 和 Gateway）。
type ServerConfig struct {
	AgentServer AgentServerConfig `yaml:"agentserver"`
	Gateway     GatewayConfig     `yaml:"gateway"`
}

// LoggingConfig 日志配置，各通道可独立设置级别。
// 对应 Python: config.yaml 的 logging 段
type LoggingConfig struct {
	Level        string `yaml:"level"`          // 基础级别，默认 INFO
	Format       string `yaml:"format"`         // 输出格式：json / text
	ConsoleLevel string `yaml:"console_level"`  // 控制台级别
	Gateway      string `yaml:"gateway"`        // gateway.log 级别
	Channel      string `yaml:"channel"`        // channel.log 级别
	AgentServer  string `yaml:"agent_server"`   // agent_server.log 级别
	Full         string `yaml:"full"`           // full.log 级别
}

// WorkspaceConfig 工作区配置。
type WorkspaceConfig struct {
	Path string `yaml:"path"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetServerConfig 获取服务配置。
func (c *Config) GetServerConfig() (*ServerConfig, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	serverVal := c.data["server"]
	if serverVal == nil {
		return nil, fmt.Errorf("配置中不存在 server 段")
	}

	// 将 map 重新序列化为 YAML 再反序列化为结构体，确保类型正确
	bytes, err := yaml.Marshal(serverVal)
	if err != nil {
		return nil, fmt.Errorf("序列化 server 配置失败: %w", err)
	}

	var cfg ServerConfig
	if err := yaml.Unmarshal(bytes, &cfg); err != nil {
		return nil, fmt.Errorf("反序列化 server 配置失败: %w", err)
	}

	return &cfg, nil
}

// UpdateServerConfig 更新服务配置。
func (c *Config) UpdateServerConfig(cfg *ServerConfig) error {
	// 序列化为 map[string]any
	bytes, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("序列化 server 配置失败: %w", err)
	}

	var data map[string]any
	if err := yaml.Unmarshal(bytes, &data); err != nil {
		return fmt.Errorf("反序列化 server 配置失败: %w", err)
	}

	return c.Set("server", data)
}

// GetLoggingConfig 获取日志配置。
func (c *Config) GetLoggingConfig() (*LoggingConfig, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	loggingVal := c.data["logging"]
	if loggingVal == nil {
		return nil, fmt.Errorf("配置中不存在 logging 段")
	}

	bytes, err := yaml.Marshal(loggingVal)
	if err != nil {
		return nil, fmt.Errorf("序列化 logging 配置失败: %w", err)
	}

	var cfg LoggingConfig
	if err := yaml.Unmarshal(bytes, &cfg); err != nil {
		return nil, fmt.Errorf("反序列化 logging 配置失败: %w", err)
	}

	return &cfg, nil
}

// UpdateLoggingConfig 更新日志配置。
func (c *Config) UpdateLoggingConfig(cfg *LoggingConfig) error {
	bytes, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("序列化 logging 配置失败: %w", err)
	}

	var data map[string]any
	if err := yaml.Unmarshal(bytes, &data); err != nil {
		return fmt.Errorf("反序列化 logging 配置失败: %w", err)
	}

	return c.Set("logging", data)
}

// GetWorkspaceConfig 获取工作区配置。
func (c *Config) GetWorkspaceConfig() (*WorkspaceConfig, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	workspaceVal := c.data["workspace"]
	if workspaceVal == nil {
		return nil, fmt.Errorf("配置中不存在 workspace 段")
	}

	bytes, err := yaml.Marshal(workspaceVal)
	if err != nil {
		return nil, fmt.Errorf("序列化 workspace 配置失败: %w", err)
	}

	var cfg WorkspaceConfig
	if err := yaml.Unmarshal(bytes, &cfg); err != nil {
		return nil, fmt.Errorf("反序列化 workspace 配置失败: %w", err)
	}

	return &cfg, nil
}

// UpdateWorkspaceConfig 更新工作区配置。
func (c *Config) UpdateWorkspaceConfig(cfg *WorkspaceConfig) error {
	bytes, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("序列化 workspace 配置失败: %w", err)
	}

	var data map[string]any
	if err := yaml.Unmarshal(bytes, &data); err != nil {
		return fmt.Errorf("反序列化 workspace 配置失败: %w", err)
	}

	return c.Set("workspace", data)
}
