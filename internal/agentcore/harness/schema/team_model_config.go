package schema

import (
	"github.com/google/uuid"
	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamModelConfig 可序列化的团队模型配置。
// Python: TeamModelConfig (openjiuwen/harness/schema/deep_agent_spec.py:127)
//
// 定义在 core schema 包：DeepAgentSpec.Model 需要它，且 core 不能引用 models 包。
//
// 用于团队角色级别的模型配置，包含客户端配置和请求配置。
type TeamModelConfig struct {
	// ModelClientConfig 模型客户端配置
	ModelClientConfig llmschema.ModelClientConfig `json:"model_client_config"`
	// ModelRequestConfig 模型请求配置（可选）
	ModelRequestConfig *llmschema.ModelRequestConfig `json:"model_request_config,omitempty"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamModelConfig 创建默认 TeamModelConfig。
// Python: TeamModelConfig()
func NewTeamModelConfig() TeamModelConfig {
	return TeamModelConfig{
		ModelClientConfig:  llmschema.ModelClientConfig{Timeout: 60, MaxRetries: 3, VerifySSL: true},
		ModelRequestConfig: llmschema.NewModelRequestConfig(),
	}
}

// Build 从配置构建模型实例。
// Python: deep_agent_spec.py:127-132 ModelSpec.build
//
// 构造失败时告警并返回 nil（对齐 Go 侧既有 buildModelFromSpec 语义：
// 模型构造失败不中断装配，agent 以无模型继续——与 Python「构造期不抛、
// 调用期才失败」的可观测行为等价）。
func (c TeamModelConfig) Build() *llm.Model {
	// client_id 缺省自动生成（Python: config.py:136 client_id
	// default_factory=uuid4；缺失会让 CreateModelClient 校验失败）。
	if c.ModelClientConfig.ClientID == "" {
		c.ModelClientConfig.ClientID = uuid.NewString()
	}
	m, err := llm.NewModel(&c.ModelClientConfig, c.ModelRequestConfig)
	if err != nil {
		logger.Warn(logger.ComponentAgentCore).Err(err).
			Str("client_provider", c.ModelClientConfig.ClientProvider).
			Msg("model build failed, agent will have no model")
		return nil
	}
	return m
}
