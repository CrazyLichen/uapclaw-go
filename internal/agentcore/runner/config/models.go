package config

import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/checkpointer"
	"github.com/uapclaw/uapclaw-go/internal/common/utils"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PulsarConfig Pulsar 消息队列连接配置。
//
// 对应 Python: PulsarConfig(BaseModel)
type PulsarConfig struct {
	// URL Pulsar 服务地址
	URL string
	// MaxWorkers 最大工作协程数，默认 8
	MaxWorkers int
}

// MessageQueueConfig 消息队列配置。
//
// 对应 Python: MessageQueueConfig(BaseModel)
type MessageQueueConfig struct {
	// Type 队列类型，默认 PULSAR
	Type MessageQueueType
	// PulsarConfig Pulsar 配置，可选
	PulsarConfig *PulsarConfig
}

// DistributedConfig 分布式模式配置。
//
// 对应 Python: DistributedConfig(BaseModel)
type DistributedConfig struct {
	// RequestTimeout 请求超时秒数，默认 30.0
	RequestTimeout float64
	// MaxRequestConcurrency 最大请求并发数，默认 10000
	MaxRequestConcurrency int
	// MessageQueueConfig 消息队列配置
	MessageQueueConfig MessageQueueConfig
	// AgentTopicTemplate Agent topic 模板
	AgentTopicTemplate string
	// ReplyTopicTemplate Reply topic 模板
	ReplyTopicTemplate string
}

// RunnerConfig Runner 全局配置。
//
// 对应 Python: RunnerConfig(BaseModel)
type RunnerConfig struct {
	// DistributedMode 分布式模式开关，默认 true
	DistributedMode bool
	// DistributedConfig 分布式配置
	DistributedConfig *DistributedConfig
	// EnvPrefix 环境前缀
	EnvPrefix string
	// InstanceID 实例 ID（UUID）
	InstanceID string
	// CheckpointerConfig 检查点器配置，可选
	CheckpointerConfig *checkpointer.CheckpointerFactoryConfig
	// EnableSessionController Session 控制器开关
	EnableSessionController bool
	// EnableA2A A2A 开关
	EnableA2A bool
}

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewPulsarConfig 创建 Pulsar 配置，MaxWorkers 默认 8。
//
// 对应 Python: PulsarConfig(url=None, max_workers=8)
func NewPulsarConfig(url string) *PulsarConfig {
	return &PulsarConfig{
		URL:        url,
		MaxWorkers: 8,
	}
}

// String 返回脱敏后的配置字符串表示，实现 fmt.Stringer 接口。
//
// 对应 Python: PulsarConfig.__repr__() / __str__()
func (c *PulsarConfig) String() string {
	url := c.URL
	if url != "" {
		url = utils.RedactURLPassword(url)
	}
	return fmt.Sprintf("url=%q max_workers=%d", url, c.MaxWorkers)
}

// GetAgentTopicTemplate 获取 Agent topic 模板，拼接环境前缀。
//
// 对应 Python: DistributedConfig.get_agent_topic_template(env_prefix)
func (c *DistributedConfig) GetAgentTopicTemplate(envPrefix string) string {
	if envPrefix != "" {
		return envPrefix + "." + c.AgentTopicTemplate
	}
	return c.AgentTopicTemplate
}

// GetReplyTopicTemplate 获取 Reply topic 模板，拼接环境前缀。
//
// 对应 Python: DistributedConfig.get_reply_topic_template(env_prefix)
func (c *DistributedConfig) GetReplyTopicTemplate(envPrefix string) string {
	if envPrefix != "" {
		return envPrefix + "." + c.ReplyTopicTemplate
	}
	return c.ReplyTopicTemplate
}

// AgentTopicTemplate 获取 Agent topic 模板，使用 RunnerConfig.EnvPrefix 作为前缀。
//
// 对应 Python: RunnerConfig.agent_topic_template()
func (c *RunnerConfig) AgentTopicTemplate() string {
	if c.DistributedConfig == nil {
		return ""
	}
	return c.DistributedConfig.GetAgentTopicTemplate(c.EnvPrefix)
}

// ReplyTopicTemplate 获取 Reply topic 模板，使用 RunnerConfig.EnvPrefix 作为前缀。
//
// 对应 Python: RunnerConfig.reply_topic_template()
func (c *RunnerConfig) ReplyTopicTemplate() string {
	if c.DistributedConfig == nil {
		return ""
	}
	return c.DistributedConfig.GetReplyTopicTemplate(c.EnvPrefix)
}
