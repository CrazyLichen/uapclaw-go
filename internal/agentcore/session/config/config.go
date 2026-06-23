package config

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/constants"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BuiltinConfigLoader 内置配置加载钩子接口。
// 对应 Python: Config._load_builtin_configs_
// Go 不支持虚方法分派，通过接口注入实现模板方法模式（同 5.9 EntityHooks 模式）。
type BuiltinConfigLoader interface {
	// LoadBuiltinConfigs 加载内置默认配置到 envs 字典
	LoadBuiltinConfigs(envs map[string]any)
}

// SessionConfig 会话配置接口。
// 对应 Python: openjiuwen/core/session/config/base.py (Config)
//
// 定义在 config 包而非 interfaces 包，避免 tracer↔interfaces 循环依赖：
// interfaces 导入 tracer（BaseSession.Tracer() 返回 *tracer.Tracer），
// 如果 SessionConfig 在 interfaces 中，tracer 引用 SessionConfig 会产生循环。
// 移到 config 包后，依赖方向为 interfaces→config、tracer→config，无循环。
type SessionConfig interface {
	// GetEnv 获取环境变量值
	GetEnv(key string, defaultValue ...any) any
	// GetEnvs 获取所有环境变量（深拷贝）
	GetEnvs() map[string]any
	// SetEnvs 合并环境变量
	SetEnvs(envs map[string]any)
	// GetWorkflowConfig 按 workflowID 获取工作流配置。
	// ⤵️ 8.15 回填：WorkflowConfig 实现后返回类型从 any 改为 WorkflowConfig
	GetWorkflowConfig(workflowID string) any
	// GetAgentConfig 获取 Agent 配置。
	// 调用方应传入 AgentConfig 实现者（定义于 single_agent/interfaces），
	// 因循环依赖限制（interfaces → session → config）暂保留 any 类型。
	GetAgentConfig() any
	// SetAgentConfig 设置 Agent 配置。
	// 调用方应传入 AgentConfig 实现者（定义于 single_agent/interfaces），
	// 因循环依赖限制（interfaces → session → config）暂保留 any 类型。
	SetAgentConfig(agentConfig any)
	// AddWorkflowConfig 添加工作流配置。
	// ⤵️ 8.15 回填：WorkflowConfig 实现后参数类型从 any 改为 WorkflowConfig
	AddWorkflowConfig(workflowID string, workflowConfig any)
}

// MetadataLike 回调元数据结构体。
// 对应 Python: openjiuwen/core/session/config/base.py (MetadataLike TypedDict)
type MetadataLike struct {
	// Name 名称
	Name string
	// Event 事件
	Event string
}

// defaultSessionConfig SessionConfig 的默认实现。
// 对应 Python: openjiuwen/core/session/config/base.py (Config)
type defaultSessionConfig struct {
	// env 环境变量字典
	env map[string]any
	// callbackMetadata 回调元数据（预留，后续回调系统实现时回填）
	callbackMetadata map[string]MetadataLike
	// workflowConfigs 按 workflowID 索引的工作流配置
	// ⤵️ 8.15 回填：map[string]any → map[string]WorkflowConfig
	workflowConfigs map[string]any
	// agentConfig Agent 配置
	// 调用方应传入 AgentConfig 实现者，因循环依赖限制暂保留 any 类型
	agentConfig any
	// loader 内置配置加载钩子
	loader BuiltinConfigLoader
}

// defaultBuiltinConfigLoader 默认内置配置加载器
type defaultBuiltinConfigLoader struct{}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// 确保 defaultSessionConfig 实现 SessionConfig 接口
var _ SessionConfig = (*defaultSessionConfig)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSessionConfig 创建默认 SessionConfig 实例。
// 对应 Python: Config()
func NewSessionConfig(ctx context.Context) *defaultSessionConfig {
	return NewSessionConfigWithLoader(ctx, &defaultBuiltinConfigLoader{})
}

// NewSessionConfigWithLoader 创建注入自定义 loader 的 SessionConfig 实例。
func NewSessionConfigWithLoader(ctx context.Context, loader BuiltinConfigLoader) *defaultSessionConfig {
	cfg := &defaultSessionConfig{
		env:              make(map[string]any),
		callbackMetadata: make(map[string]MetadataLike),
		workflowConfigs:  make(map[string]any),
		loader:           loader,
	}
	cfg.loadEnvs(ctx)
	return cfg
}

// GetEnv 获取环境变量值。
// 对应 Python: Config.get_env(key, default)
func (c *defaultSessionConfig) GetEnv(key string, defaultValue ...any) any {
	if v, exists := c.env[key]; exists {
		return v
	}
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return nil
}

// GetEnvs 获取所有环境变量（深拷贝）。
// 对应 Python: Config.get_envs() → deepcopy(self._env)
func (c *defaultSessionConfig) GetEnvs() map[string]any {
	result := make(map[string]any, len(c.env))
	for k, v := range c.env {
		result[k] = v
	}
	return result
}

// SetEnvs 合并环境变量。
// 对应 Python: Config.set_envs(envs)
func (c *defaultSessionConfig) SetEnvs(envs map[string]any) {
	if envs == nil {
		return
	}
	for k, v := range envs {
		c.env[k] = v
	}
}

// GetWorkflowConfig 按 workflowID 获取工作流配置。
// 对应 Python: Config.get_workflow_config(workflow_id)
func (c *defaultSessionConfig) GetWorkflowConfig(workflowID string) any {
	if workflowID == "" {
		return nil
	}
	return c.workflowConfigs[workflowID]
}

// GetAgentConfig 获取 Agent 配置。
// 对应 Python: Config.get_agent_config()
func (c *defaultSessionConfig) GetAgentConfig() any {
	return c.agentConfig
}

// SetAgentConfig 设置 Agent 配置。
// 对应 Python: Config.set_agent_config(agent_config)
func (c *defaultSessionConfig) SetAgentConfig(agentConfig any) {
	c.agentConfig = agentConfig
}

// AddWorkflowConfig 添加工作流配置。
// 对应 Python: Config.add_workflow_config(workflow_id, workflow_config)
func (c *defaultSessionConfig) AddWorkflowConfig(workflowID string, workflowConfig any) {
	if workflowID == "" {
		return
	}
	if workflowConfig == nil {
		return
	}
	c.workflowConfigs[workflowID] = workflowConfig
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// loadEnvs 加载环境变量配置。
// 对应 Python: Config._load_envs_()
// 三层优先级：os.Getenv > context.Value > 内置默认值
func (c *defaultSessionConfig) loadEnvs(ctx context.Context) {
	// 1. 加载内置默认值
	c.loader.LoadBuiltinConfigs(c.env)
	// 2. 从 context.Value 和 os.Getenv 覆盖（优先级更高）
	envConfigs := loadEnvConfigs(ctx)
	for k, v := range envConfigs {
		c.env[k] = v
	}
}

// LoadBuiltinConfigs 默认加载器实现。
// 对应 Python: Config._load_builtin_configs_
func (l *defaultBuiltinConfigLoader) LoadBuiltinConfigs(envs map[string]any) {
	defaults := constants.BuiltinDefaults()
	for k, v := range defaults {
		envs[k] = v
	}
}
