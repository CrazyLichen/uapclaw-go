package config

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/constants"
)

// TestTrySetEnv_float类型 测试 float 类型转换
func TestTrySetEnv_float类型(t *testing.T) {
	envs := make(map[string]any)

	trySetEnv(envs, constants.WorkflowExecuteTimeoutKey, constants.WorkflowExecuteTimeoutEnvKey, "120.5")

	assert.Equal(t, 120.5, envs[constants.WorkflowExecuteTimeoutKey])
}

// TestTrySetEnv_int类型 测试 int 类型转换
func TestTrySetEnv_int类型(t *testing.T) {
	envs := make(map[string]any)

	trySetEnv(envs, constants.LoopNumberMaxLimitKey, constants.LoopNumberMaxLimitEnvKey, "500")

	assert.Equal(t, 500, envs[constants.LoopNumberMaxLimitKey])
}

// TestTrySetEnv_bool类型_true 测试 bool 类型转换 true
func TestTrySetEnv_bool类型_true(t *testing.T) {
	envs := make(map[string]any)

	trySetEnv(envs, constants.ForceDelWorkflowStateKey, constants.ForceDelWorkflowStateEnvKey, "true")

	assert.Equal(t, true, envs[constants.ForceDelWorkflowStateKey])
}

// TestTrySetEnv_bool类型_false 测试 bool 类型转换 false
func TestTrySetEnv_bool类型_false(t *testing.T) {
	envs := make(map[string]any)

	trySetEnv(envs, constants.ForceDelWorkflowStateKey, constants.ForceDelWorkflowStateEnvKey, "false")

	assert.Equal(t, false, envs[constants.ForceDelWorkflowStateKey])
}

// TestTrySetEnv_nil值跳过 测试 nil 值不设置
func TestTrySetEnv_nil值跳过(t *testing.T) {
	envs := make(map[string]any)

	trySetEnv(envs, "key", "env_key", nil)

	_, exists := envs["key"]
	assert.False(t, exists)
}

// TestTrySetEnv_无效float跳过 测试无效 float 值跳过
func TestTrySetEnv_无效float跳过(t *testing.T) {
	envs := make(map[string]any)

	trySetEnv(envs, constants.WorkflowExecuteTimeoutKey, constants.WorkflowExecuteTimeoutEnvKey, "not_a_number")

	_, exists := envs[constants.WorkflowExecuteTimeoutKey]
	assert.False(t, exists)
}

// TestTrySetEnv_无效bool跳过 测试无效 bool 值跳过
func TestTrySetEnv_无效bool跳过(t *testing.T) {
	envs := make(map[string]any)

	trySetEnv(envs, constants.ForceDelWorkflowStateKey, constants.ForceDelWorkflowStateEnvKey, "yes")

	_, exists := envs[constants.ForceDelWorkflowStateKey]
	assert.False(t, exists)
}

// TestTrySetEnv_直接float64值 测试直接传入 float64
func TestTrySetEnv_直接float64值(t *testing.T) {
	envs := make(map[string]any)

	trySetEnv(envs, constants.WorkflowExecuteTimeoutKey, constants.WorkflowExecuteTimeoutEnvKey, 99.5)

	assert.Equal(t, 99.5, envs[constants.WorkflowExecuteTimeoutKey])
}

// TestTrySetEnv_无类型映射直接设置 测试没有类型映射时直接设置值
func TestTrySetEnv_无类型映射直接设置(t *testing.T) {
	envs := make(map[string]any)

	trySetEnv(envs, "unknown_key", "UNKNOWN_ENV_KEY", "some_value")

	assert.Equal(t, "some_value", envs["unknown_key"])
}

// TestTrySetEnv_无效int跳过 测试无效 int 值跳过
func TestTrySetEnv_无效int跳过(t *testing.T) {
	envs := make(map[string]any)

	trySetEnv(envs, constants.LoopNumberMaxLimitKey, constants.LoopNumberMaxLimitEnvKey, "not_an_int")

	_, exists := envs[constants.LoopNumberMaxLimitKey]
	assert.False(t, exists)
}

// TestTrySetFloat_float32值 测试 float32 类型转换
func TestTrySetFloat_float32值(t *testing.T) {
	envs := make(map[string]any)

	trySetFloat(envs, "key", "env_key", float32(1.5))

	assert.Equal(t, float64(1.5), envs["key"])
}

// TestTrySetFloat_int值 测试 int 类型转 float
func TestTrySetFloat_int值(t *testing.T) {
	envs := make(map[string]any)

	trySetFloat(envs, "key", "env_key", 42)

	assert.Equal(t, float64(42), envs["key"])
}

// TestTrySetFloat_不支持的类型 测试不支持的类型跳过
func TestTrySetFloat_不支持的类型(t *testing.T) {
	envs := make(map[string]any)

	trySetFloat(envs, "key", "env_key", true)

	_, exists := envs["key"]
	assert.False(t, exists)
}

// TestTrySetInt_直接int值 测试直接传入 int
func TestTrySetInt_直接int值(t *testing.T) {
	envs := make(map[string]any)

	trySetInt(envs, "key", "env_key", 42)

	assert.Equal(t, 42, envs["key"])
}

// TestTrySetInt_不支持的类型 测试不支持的类型跳过
func TestTrySetInt_不支持的类型(t *testing.T) {
	envs := make(map[string]any)

	trySetInt(envs, "key", "env_key", 3.14)

	_, exists := envs["key"]
	assert.False(t, exists)
}

// TestTrySetBool_直接bool值 测试直接传入 bool
func TestTrySetBool_直接bool值(t *testing.T) {
	envs := make(map[string]any)

	trySetBool(envs, "key", "env_key", true)

	assert.Equal(t, true, envs["key"])
}

// TestTrySetBool_不支持的类型 测试不支持的类型跳过
func TestTrySetBool_不支持的类型(t *testing.T) {
	envs := make(map[string]any)

	trySetBool(envs, "key", "env_key", 123)

	_, exists := envs["key"]
	assert.False(t, exists)
}
