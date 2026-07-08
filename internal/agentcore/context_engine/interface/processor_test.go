package iface

import (
	"errors"
	"testing"
)

// ──────────────────────────── ProcessorConfig 测试 ────────────────────────────

// TestProcessorConfig fakeConfig 实现 ProcessorConfig 接口
func TestProcessorConfig(t *testing.T) {
	cfg := &fakeProcessorConfig{valid: true}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() 返回错误: %v, 期望 nil", err)
	}

	cfg2 := &fakeProcessorConfig{valid: false}
	if err := cfg2.Validate(); err == nil {
		t.Error("Validate() 应返回错误，但返回 nil")
	}
}

// ──────────────────────────── ProcessorFactory 测试 ────────────────────────────

// TestProcessorFactory 工厂函数类型
func TestProcessorFactory(t *testing.T) {
	factory := ProcessorFactory(func(config ProcessorConfig) (ContextProcessor, error) {
		return nil, nil
	})
	if factory == nil {
		t.Fatal("ProcessorFactory 应为非 nil")
	}
}

// ──────────────────────────── fakeProcessorConfig 测试辅助 ────────────────────────────

// fakeProcessorConfig 用于测试的模拟处理器配置
type fakeProcessorConfig struct {
	valid bool
}

// errFakeValidate 模拟校验错误
var errFakeValidate = errors.New("配置校验失败")

func (f *fakeProcessorConfig) Validate() error {
	if !f.valid {
		return errFakeValidate
	}
	return nil
}
