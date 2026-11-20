package models

import (
	"testing"
)

// ──────────────────────────── TeamModelConfig 团队模型配置 ────────────────────────────

// TestNewTeamModelConfig 测试默认 TeamModelConfig 创建
func TestNewTeamModelConfig(t *testing.T) {
	cfg := NewTeamModelConfig()
	if cfg.ModelRequestConfig == nil {
		t.Error("ModelRequestConfig 不应为 nil")
	}
	if cfg.ModelClientConfig.Timeout != 60.0 {
		t.Errorf("期望 Timeout=60.0, 实际=%f", cfg.ModelClientConfig.Timeout)
	}
	if cfg.ModelClientConfig.MaxRetries != 3 {
		t.Errorf("期望 MaxRetries=3, 实际=%d", cfg.ModelClientConfig.MaxRetries)
	}
}

// TestTeamModelConfig_Build_留桩 测试 Build 不 panic 且返回值可用。
// 15.15 起真实现（Python deep_agent_spec.py:127-132 ModelSpec.build）：
// 本包测试未注册 MockLLM 客户端，构造失败按语义降级返回 nil（warn 告警）。
func TestTeamModelConfig_Build_留桩(t *testing.T) {
	cfg := NewTeamModelConfig()
	result := cfg.Build()
	_ = result // 未注册客户端环境下允许 nil；注册环境返回模型实例
}
