package config

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/constants"
)

// TestNewSessionConfig_默认值加载 测试默认配置加载
func TestNewSessionConfig_默认值加载(t *testing.T) {
	cfg := NewSessionConfig(context.Background())

	// 验证内置默认值
	assert.Equal(t, 60.0, cfg.GetEnv(constants.WorkflowExecuteTimeoutKey))
	assert.Equal(t, -1.0, cfg.GetEnv(constants.WorkflowStreamFrameTimeoutKey))
	assert.Equal(t, -1.0, cfg.GetEnv(constants.WorkflowStreamFirstFrameTimeoutKey))
	assert.Equal(t, -1.0, cfg.GetEnv(constants.CompStreamCallTimeoutKey))
	assert.Equal(t, -1.0, cfg.GetEnv(constants.StreamInputGenTimeoutKey))
	assert.Equal(t, 5.0, cfg.GetEnv(constants.EndCompTemplateRenderPositionTimeoutKey))
	assert.Equal(t, 5.0, cfg.GetEnv(constants.EndCompTemplateBranchRenderTimeoutKey))
	assert.Equal(t, 1000, cfg.GetEnv(constants.LoopNumberMaxLimitKey))
	assert.Equal(t, false, cfg.GetEnv(constants.ForceDelWorkflowStateKey))
}

// TestNewSessionConfig_GetEnv默认值 测试 GetEnv 的 defaultValue 参数
func TestNewSessionConfig_GetEnv默认值(t *testing.T) {
	cfg := NewSessionConfig(context.Background())

	// 不存在的键返回 defaultValue
	assert.Equal(t, "fallback", cfg.GetEnv("nonexistent_key", "fallback"))
	// 不存在的键无 defaultValue 返回 nil
	assert.Nil(t, cfg.GetEnv("nonexistent_key"))
}

// TestNewSessionConfig_SetEnvs 测试 SetEnvs 合并环境变量
func TestNewSessionConfig_SetEnvs(t *testing.T) {
	cfg := NewSessionConfig(context.Background())

	cfg.SetEnvs(map[string]any{
		constants.WorkflowExecuteTimeoutKey: 120.0,
		"custom_key":                        "custom_value",
	})

	assert.Equal(t, 120.0, cfg.GetEnv(constants.WorkflowExecuteTimeoutKey))
	assert.Equal(t, "custom_value", cfg.GetEnv("custom_key"))
}

// TestNewSessionConfig_SetEnvs_nil 测试 SetEnvs 传入 nil 不 panic
func TestNewSessionConfig_SetEnvs_nil(t *testing.T) {
	cfg := NewSessionConfig(context.Background())

	assert.NotPanics(t, func() {
		cfg.SetEnvs(nil)
	})
}

// TestNewSessionConfig_GetEnvs深拷贝 测试 GetEnvs 返回深拷贝
func TestNewSessionConfig_GetEnvs深拷贝(t *testing.T) {
	cfg := NewSessionConfig(context.Background())

	envs := cfg.GetEnvs()
	envs[constants.WorkflowExecuteTimeoutKey] = 999.0

	// 原始值不受影响
	assert.Equal(t, 60.0, cfg.GetEnv(constants.WorkflowExecuteTimeoutKey))
}

// TestNewSessionConfig_WorkflowConfig 测试工作流配置的存取
// ⤵️ 8.15 回填：WorkflowConfig 实现后 AddWorkflowConfig 参数从 any 改为具体类型
func TestNewSessionConfig_WorkflowConfig(t *testing.T) {
	cfg := NewSessionConfig(context.Background())

	// 无配置时返回 nil
	assert.Nil(t, cfg.GetWorkflowConfig("wf1"))

	// 添加 nil 跳过
	cfg.AddWorkflowConfig("wf1", nil)
	assert.Nil(t, cfg.GetWorkflowConfig("wf1"))

	// 空字符串 workflowID 跳过
	cfg.AddWorkflowConfig("", "some_config")

	// 添加有效配置
	cfg.AddWorkflowConfig("wf1", "test_workflow_config")
	assert.Equal(t, "test_workflow_config", cfg.GetWorkflowConfig("wf1"))
}

// TestNewSessionConfig_AgentConfig 测试 Agent 配置的存取
// AgentConfig 已实现，因循环依赖 config 包保留 any，调用方通过类型断言使用
func TestNewSessionConfig_AgentConfig(t *testing.T) {
	cfg := NewSessionConfig(context.Background())

	// 无配置时返回 nil
	assert.Nil(t, cfg.GetAgentConfig())

	// 设置 Agent 配置（使用字符串测试 any 存储）
	cfg.SetAgentConfig("test_agent_config")
	assert.Equal(t, "test_agent_config", cfg.GetAgentConfig())
}

// TestNewSessionConfig_ContextEnvs 测试 context 注入环境变量
func TestNewSessionConfig_ContextEnvs(t *testing.T) {
	ctx := context.Background()
	ctx = WithEnvs(ctx, map[string]any{
		constants.WorkflowExecuteTimeoutEnvKey: 200.0,
	})

	cfg := NewSessionConfig(ctx)

	assert.Equal(t, 200.0, cfg.GetEnv(constants.WorkflowExecuteTimeoutKey))
}

// TestNewSessionConfigWithLoader_自定义Loader 测试自定义 loader
func TestNewSessionConfigWithLoader_自定义Loader(t *testing.T) {
	loader := &testConfigLoader{}
	cfg := NewSessionConfigWithLoader(context.Background(), loader)

	assert.Equal(t, "from_test_loader", cfg.GetEnv("test_key"))
	assert.Equal(t, 60.0, cfg.GetEnv(constants.WorkflowExecuteTimeoutKey))
}

// testConfigLoader 测试用自定义 loader
type testConfigLoader struct{}

func (l *testConfigLoader) LoadBuiltinConfigs(envs map[string]any) {
	defaults := constants.BuiltinDefaults()
	for k, v := range defaults {
		envs[k] = v
	}
	envs["test_key"] = "from_test_loader"
}
