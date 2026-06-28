package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/checkpointer"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestMessageQueueType_枚举值 测试 MessageQueueType 枚举值。
func TestMessageQueueType_枚举值(t *testing.T) {
	assert.Equal(t, MessageQueueType("pulsar"), MessageQueueTypePulsar)
	assert.Equal(t, MessageQueueType("fake"), MessageQueueTypeFake)
}

// TestPulsarConfig_String_脱敏 测试 PulsarConfig.String() 对 URL 密码脱敏。
func TestPulsarConfig_String_脱敏(t *testing.T) {
	// URL 含密码
	cfg := &PulsarConfig{
		URL:        "pulsar://admin:secret@broker:6650",
		MaxWorkers: 8,
	}
	s := cfg.String()
	assert.Contains(t, s, "***")
	assert.NotContains(t, s, "secret")
	assert.Contains(t, s, "max_workers=8")

	// 空 URL
	cfg2 := &PulsarConfig{MaxWorkers: 4}
	s2 := cfg2.String()
	assert.Contains(t, s2, "max_workers=4")

	// 无密码 URL
	cfg3 := &PulsarConfig{
		URL:        "pulsar://broker:6650",
		MaxWorkers: 16,
	}
	s3 := cfg3.String()
	assert.Contains(t, s3, "broker:6650")
}

// TestNewPulsarConfig_默认值 测试 NewPulsarConfig 设置默认 MaxWorkers=8。
func TestNewPulsarConfig_默认值(t *testing.T) {
	cfg := NewPulsarConfig("pulsar://broker:6650")
	assert.Equal(t, "pulsar://broker:6650", cfg.URL)
	assert.Equal(t, 8, cfg.MaxWorkers)

	// 空 URL
	cfg2 := NewPulsarConfig("")
	assert.Equal(t, "", cfg2.URL)
	assert.Equal(t, 8, cfg2.MaxWorkers)
}

// TestDistributedConfig_GetTopicTemplate 测试 GetAgentTopicTemplate / GetReplyTopicTemplate。
func TestDistributedConfig_GetTopicTemplate(t *testing.T) {
	cfg := &DistributedConfig{
		AgentTopicTemplate: "openjiuwen.single_agent.{agent_id}.{version}",
		ReplyTopicTemplate: "openjiuwen.reply.runner.{instance_id}",
	}

	// 无前缀
	assert.Equal(t, "openjiuwen.single_agent.{agent_id}.{version}", cfg.GetAgentTopicTemplate(""))
	assert.Equal(t, "openjiuwen.reply.runner.{instance_id}", cfg.GetReplyTopicTemplate(""))

	// 有前缀
	assert.Equal(t, "prod.openjiuwen.single_agent.{agent_id}.{version}", cfg.GetAgentTopicTemplate("prod"))
	assert.Equal(t, "prod.openjiuwen.reply.runner.{instance_id}", cfg.GetReplyTopicTemplate("prod"))
}

// TestRunnerConfig_TopicTemplate 测试 RunnerConfig.AgentTopicTemplate / ReplyTopicTemplate。
func TestRunnerConfig_TopicTemplate(t *testing.T) {
	// DistributedConfig 为 nil 时返回空
	cfg := &RunnerConfig{DistributedConfig: nil}
	assert.Equal(t, "", cfg.AgentTopicTemplate())
	assert.Equal(t, "", cfg.ReplyTopicTemplate())

	// 有 DistributedConfig
	cfg2 := &RunnerConfig{
		EnvPrefix: "staging",
		DistributedConfig: &DistributedConfig{
			AgentTopicTemplate: "openjiuwen.single_agent.{agent_id}.{version}",
			ReplyTopicTemplate: "openjiuwen.reply.runner.{instance_id}",
		},
	}
	assert.Equal(t, "staging.openjiuwen.single_agent.{agent_id}.{version}", cfg2.AgentTopicTemplate())
	assert.Equal(t, "staging.openjiuwen.reply.runner.{instance_id}", cfg2.ReplyTopicTemplate())
}

// TestRunnerConfig_字段默认值 测试 RunnerConfig 各字段默认值语义。
func TestRunnerConfig_字段默认值(t *testing.T) {
	cfg := &RunnerConfig{
		DistributedMode:   true,
		DistributedConfig: &DistributedConfig{},
	}
	assert.True(t, cfg.DistributedMode)
	assert.False(t, cfg.EnableSessionController)
	assert.False(t, cfg.EnableA2A)
}

// TestRunnerConfig_CheckpointerConfig 测试 RunnerConfig 持有 CheckpointerFactoryConfig。
func TestRunnerConfig_CheckpointerConfig(t *testing.T) {
	cc := &checkpointer.CheckpointerFactoryConfig{
		Type: "in_memory",
		Conf: map[string]any{"host": "localhost"},
	}
	cfg := &RunnerConfig{
		CheckpointerConfig: cc,
	}
	assert.NotNil(t, cfg.CheckpointerConfig)
	assert.Equal(t, "in_memory", cfg.CheckpointerConfig.Type)
}
