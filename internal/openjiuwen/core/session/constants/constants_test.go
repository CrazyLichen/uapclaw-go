package constants

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestBuiltinDefaults 测试内置默认配置完整性
func TestBuiltinDefaults(t *testing.T) {
	defaults := BuiltinDefaults()

	// 验证所有配置键都有默认值
	assert.Equal(t, 60.0, defaults[WorkflowExecuteTimeoutKey])
	assert.Equal(t, -1.0, defaults[WorkflowStreamFrameTimeoutKey])
	assert.Equal(t, -1.0, defaults[WorkflowStreamFirstFrameTimeoutKey])
	assert.Equal(t, -1.0, defaults[CompStreamCallTimeoutKey])
	assert.Equal(t, -1.0, defaults[StreamInputGenTimeoutKey])
	assert.Equal(t, 5.0, defaults[EndCompTemplateRenderPositionTimeoutKey])
	assert.Equal(t, 5.0, defaults[EndCompTemplateBranchRenderTimeoutKey])
	assert.Equal(t, 1000, defaults[LoopNumberMaxLimitKey])
	assert.Equal(t, false, defaults[ForceDelWorkflowStateKey])
}

// TestBuiltinDefaults_包含所有配置键 测试默认值字典覆盖所有键
func TestBuiltinDefaults_包含所有配置键(t *testing.T) {
	defaults := BuiltinDefaults()
	expectedKeys := []string{
		WorkflowExecuteTimeoutKey,
		WorkflowStreamFrameTimeoutKey,
		WorkflowStreamFirstFrameTimeoutKey,
		CompStreamCallTimeoutKey,
		StreamInputGenTimeoutKey,
		EndCompTemplateRenderPositionTimeoutKey,
		EndCompTemplateBranchRenderTimeoutKey,
		LoopNumberMaxLimitKey,
		ForceDelWorkflowStateKey,
	}
	assert.Len(t, defaults, len(expectedKeys))
	for _, key := range expectedKeys {
		_, exists := defaults[key]
		assert.True(t, exists, "默认值字典缺少键: %s", key)
	}
}

// TestEnvConfigKeys_映射完整 测试环境变量映射表覆盖所有可配置项
func TestEnvConfigKeys_映射完整(t *testing.T) {
	assert.Len(t, EnvConfigKeys, 7)
	// 验证每个 EnvConfigKey 都有对应的类型定义
	for _, entry := range EnvConfigKeys {
		_, exists := EnvConfigTypes[entry.EnvKey]
		assert.True(t, exists, "EnvConfigTypes 缺少键: %s", entry.EnvKey)
	}
}

// TestEnvConfigTypes_类型正确 测试环境变量类型映射
func TestEnvConfigTypes_类型正确(t *testing.T) {
	assert.Equal(t, EnvConfigTypeFloat, EnvConfigTypes[WorkflowExecuteTimeoutEnvKey])
	assert.Equal(t, EnvConfigTypeFloat, EnvConfigTypes[WorkflowStreamFrameTimeoutEnvKey])
	assert.Equal(t, EnvConfigTypeFloat, EnvConfigTypes[WorkflowStreamFirstFrameTimeoutEnvKey])
	assert.Equal(t, EnvConfigTypeFloat, EnvConfigTypes[CompStreamCallTimeoutEnvKey])
	assert.Equal(t, EnvConfigTypeFloat, EnvConfigTypes[StreamInputGenTimeoutEnvKey])
	assert.Equal(t, EnvConfigTypeInt, EnvConfigTypes[LoopNumberMaxLimitEnvKey])
	assert.Equal(t, EnvConfigTypeBool, EnvConfigTypes[ForceDelWorkflowStateEnvKey])
}

// TestInteractiveInputKey_值正确 测试交互输入键的值与 Python 一致
func TestInteractiveInputKey_值正确(t *testing.T) {
	assert.Equal(t, "__interactive_input__", InteractiveInputKey)
}
