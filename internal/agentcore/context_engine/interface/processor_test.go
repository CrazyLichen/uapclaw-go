package iface

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
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

func (f *fakeProcessorConfig) SetModelDefaults(_ *llmschema.ModelRequestConfig, _ *llmschema.ModelClientConfig) {
}

// TestWithSysOperation 验证 WithSysOperation 选项函数
func TestWithSysOperation(t *testing.T) {
	var op sys_operation.SysOperation = nil
	opt := WithSysOperation(op)
	o := &ProcessorOption{}
	opt(o)
	assert.Nil(t, o.SysOperation)
}

// TestWithModel 验证 WithModel 选项函数
func TestWithModel(t *testing.T) {
	clientCfg := &llmschema.ModelClientConfig{ClientProvider: "llm_OpenAI", ClientID: "llm_OpenAI"}
	modelCfg := &llmschema.ModelRequestConfig{ModelName: "gpt-4"}
	m, err := llm.NewModel(clientCfg, modelCfg)
	assert.NoError(t, err)

	opt := WithModel(m)
	o := &ProcessorOption{}
	opt(o)
	assert.Equal(t, m, o.Model)
}
