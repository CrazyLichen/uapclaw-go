package multi_agent

// ──────────────────────────── 结构体 ────────────────────────────

// TeamConfig 团队运行时配置，控制团队的最大 Agent 数、并发数和超时。
//
// 可变参数，描述团队"怎么运行"。所有配置方法支持链式调用。
//
// 对应 Python: openjiuwen/core/multi_agent/config.py (TeamConfig)
// Python 字段: max_agents=10, max_concurrent_messages=100, message_timeout=30.0
type TeamConfig struct {
	// MaxAgents 团队最大 Agent 数量，默认 10
	//
	// 对应 Python: TeamConfig.max_agents: int = Field(default=10)
	MaxAgents int `json:"max_agents,omitempty"`
	// MaxConcurrentMessages 最大并发消息数，默认 100
	//
	// 对应 Python: TeamConfig.max_concurrent_messages: int = Field(default=100)
	MaxConcurrentMessages int `json:"max_concurrent_messages,omitempty"`
	// MessageTimeout 消息处理超时秒数，默认 30.0
	//
	// 对应 Python: TeamConfig.message_timeout: float = Field(default=30.0)
	MessageTimeout float64 `json:"message_timeout,omitempty"`
	// Extra 额外配置字段，对应 Python model_config={"extra": "allow"}
	//
	// json:"-" 表示不参与 JSON 序列化，Extra 是运行时注入的动态配置。
	Extra map[string]any `json:"-"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamConfig 创建 TeamConfig 实例，设置默认值。
//
// 对应 Python: TeamConfig(max_agents=10, max_concurrent_messages=100, message_timeout=30.0)
func NewTeamConfig() *TeamConfig {
	return &TeamConfig{
		MaxAgents:             10,
		MaxConcurrentMessages: 100,
		MessageTimeout:        30.0,
	}
}

// ConfigureMaxAgents 链式配置最大 Agent 数量。
//
// 对应 Python: TeamConfig.configure_max_agents(max_agents) -> self
func (c *TeamConfig) ConfigureMaxAgents(maxAgents int) *TeamConfig {
	c.MaxAgents = maxAgents
	return c
}

// ConfigureTimeout 链式配置消息超时秒数。
//
// 对应 Python: TeamConfig.configure_timeout(timeout) -> self
func (c *TeamConfig) ConfigureTimeout(timeout float64) *TeamConfig {
	c.MessageTimeout = timeout
	return c
}

// ConfigureConcurrency 链式配置最大并发消息数。
//
// 对应 Python: TeamConfig.configure_concurrency(max_concurrent) -> self
func (c *TeamConfig) ConfigureConcurrency(maxConcurrent int) *TeamConfig {
	c.MaxConcurrentMessages = maxConcurrent
	return c
}

// SetExtra 设置额外配置字段。
//
// 对应 Python: model_config={"extra": "allow"} 允许动态额外字段
func (c *TeamConfig) SetExtra(key string, value any) {
	if c.Extra == nil {
		c.Extra = make(map[string]any)
	}
	c.Extra[key] = value
}

// GetExtra 获取额外配置字段。
func (c *TeamConfig) GetExtra(key string) (any, bool) {
	if c.Extra == nil {
		return nil, false
	}
	val, ok := c.Extra[key]
	return val, ok
}

// ──────────────────────────── 非导出函数 ────────────────────────────
