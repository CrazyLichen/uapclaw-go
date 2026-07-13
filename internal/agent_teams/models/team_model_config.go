package models

import (
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamModelConfig 可序列化的团队模型配置。
// 对齐 Python: TeamModelConfig (openjiuwen/agent_teams/schema/deep_agent_spec.py)
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
// 对齐 Python: TeamModelConfig()
func NewTeamModelConfig() TeamModelConfig {
	return TeamModelConfig{
		ModelClientConfig:  *llmschema.NewModelClientConfig("", "", ""),
		ModelRequestConfig: llmschema.NewModelRequestConfig(),
	}
}

// Build 构建团队模型配置。
// ⤵️ 回填: 9.57 — 当前返回 nil, nil
func (c TeamModelConfig) Build() (any, error) {
	return nil, nil
}
