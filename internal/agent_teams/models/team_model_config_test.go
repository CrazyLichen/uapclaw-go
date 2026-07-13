package models

import (
	"testing"
)

// ──────────────────────────── TeamModelConfig ────────────────────────────

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

// TestTeamModelConfig_Build_留桩 测试 Build 留桩返回
func TestTeamModelConfig_Build_留桩(t *testing.T) {
	cfg := NewTeamModelConfig()
	result, err := cfg.Build()
	if result != nil || err != nil {
		t.Errorf("期望 (nil, nil), 实际=(%v, %v)", result, err)
	}
}
