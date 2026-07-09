package runtime

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestNewAgentManager(t *testing.T) {
	am := NewAgentManager()
	require.NotNil(t, am)
	require.NotNil(t, am.stubAgent)
}

func TestAgentManager_GetAgent(t *testing.T) {
	am := NewAgentManager()
	agent, err := am.GetAgent("web", "agent", "", "")
	require.NoError(t, err)
	assert.NotNil(t, agent)
}

func TestAgentManager_GetAgentNoWait(t *testing.T) {
	am := NewAgentManager()
	agent := am.GetAgentNoWait("web", "agent", "", "")
	assert.NotNil(t, agent)
}

func TestAgentManager_ReloadAgentsConfig(t *testing.T) {
	am := NewAgentManager()
	err := am.ReloadAgentsConfig(nil, nil)
	assert.NoError(t, err)
}

func TestReloadAgentsConfig_环境变量注入(t *testing.T) {
	am := NewAgentManager()

	envOverrides := map[string]any{
		"MODEL_PROVIDER": "openai",
		"MODEL_NAME":     "gpt-4",
	}

	err := am.ReloadAgentsConfig(nil, envOverrides)
	if err != nil {
		t.Fatalf("ReloadAgentsConfig 失败: %v", err)
	}

	if os.Getenv("MODEL_PROVIDER") != "openai" {
		t.Errorf("期望 MODEL_PROVIDER=openai，实际 %s", os.Getenv("MODEL_PROVIDER"))
	}
	if os.Getenv("MODEL_NAME") != "gpt-4" {
		t.Errorf("期望 MODEL_NAME=gpt-4，实际 %s", os.Getenv("MODEL_NAME"))
	}

	// 清理
	_ = os.Unsetenv("MODEL_PROVIDER")
	_ = os.Unsetenv("MODEL_NAME")
}

func TestReloadAgentsConfig_空字符串Unsetenv(t *testing.T) {
	_ = os.Setenv("TEST_RELOAD_KEY", "old_value")
	defer func() { _ = os.Unsetenv("TEST_RELOAD_KEY") }()

	am := NewAgentManager()
	envOverrides := map[string]any{
		"TEST_RELOAD_KEY": "",
	}

	err := am.ReloadAgentsConfig(nil, envOverrides)
	if err != nil {
		t.Fatalf("ReloadAgentsConfig 失败: %v", err)
	}

	if os.Getenv("TEST_RELOAD_KEY") != "" {
		t.Errorf("期望空字符串（已 unset），实际 %s", os.Getenv("TEST_RELOAD_KEY"))
	}
}

func TestReloadAgentsConfig_nil值Unsetenv(t *testing.T) {
	_ = os.Setenv("TEST_RELOAD_NIL", "old_value")
	defer func() { _ = os.Unsetenv("TEST_RELOAD_NIL") }()

	am := NewAgentManager()
	envOverrides := map[string]any{
		"TEST_RELOAD_NIL": nil,
	}

	err := am.ReloadAgentsConfig(nil, envOverrides)
	if err != nil {
		t.Fatalf("ReloadAgentsConfig 失败: %v", err)
	}

	if os.Getenv("TEST_RELOAD_NIL") != "" {
		t.Errorf("期望空字符串（已 unset），实际 %s", os.Getenv("TEST_RELOAD_NIL"))
	}
}

func TestAgentManager_Initialize(t *testing.T) {
	am := NewAgentManager()
	caps, err := am.Initialize("web", nil)
	require.NoError(t, err)
	assert.NotNil(t, caps)
	_, ok := caps["capabilities"]
	assert.True(t, ok)
}

func TestAgentManager_CancelAllInflightWork(t *testing.T) {
	am := NewAgentManager()
	err := am.CancelAllInflightWork(context.Background())
	assert.NoError(t, err)
}

func TestAgentManager_Cleanup(t *testing.T) {
	am := NewAgentManager()
	err := am.Cleanup()
	assert.NoError(t, err)
}
