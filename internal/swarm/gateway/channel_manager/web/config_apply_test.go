package web

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestConfigBadRequest_Error(t *testing.T) {
	err := &ConfigBadRequest{Message: "invalid provider"}
	assert.Equal(t, "invalid provider", err.Error())
}

func TestConfigInternalError_Error(t *testing.T) {
	err := &ConfigInternalError{Message: "internal failure"}
	assert.Equal(t, "internal failure", err.Error())
}

func TestIsAvailableProvider(t *testing.T) {
	assert.True(t, isAvailableProvider("OpenAI"))
	assert.True(t, isAvailableProvider("DashScope"))
	assert.True(t, isAvailableProvider("DeepSeek"))
	assert.False(t, isAvailableProvider("UnknownProvider"))
	assert.False(t, isAvailableProvider(""))
}

func TestIsTruthy(t *testing.T) {
	assert.True(t, isTruthy(true))
	assert.True(t, isTruthy("true"))
	assert.True(t, isTruthy("1"))
	assert.True(t, isTruthy("yes"))
	assert.True(t, isTruthy("True"))
	assert.False(t, isTruthy(false))
	assert.False(t, isTruthy("false"))
	assert.False(t, isTruthy("0"))
	assert.False(t, isTruthy(""))
}

func TestConfigYAMLKeys(t *testing.T) {
	assert.True(t, configYAMLKeys["context_engine_enabled"])
	assert.True(t, configYAMLKeys["kv_cache_affinity_enabled"])
	assert.True(t, configYAMLKeys["permissions_enabled"])
	assert.True(t, configYAMLKeys["memory_forbidden_enabled"])
	assert.True(t, configYAMLKeys["memory_forbidden_description"])
	assert.False(t, configYAMLKeys["unknown_key"])
	assert.Len(t, configYAMLKeys, 5)
}

func TestAvailableModelProviders(t *testing.T) {
	assert.Contains(t, availableModelProviders, "OpenAI")
	assert.Contains(t, availableModelProviders, "DashScope")
	assert.Contains(t, availableModelProviders, "DeepSeek")
	assert.Contains(t, availableModelProviders, "OpenRouter")
}

func TestNotifyConfigSavedOnce_无变更不触发(t *testing.T) {
	called := false
	callback := func(updatedKeys []string, envUpdates map[string]any, configPayload map[string]any) error {
		called = true
		return nil
	}
	// 无变更且非 force，不应触发
	NotifyConfigSavedOnce(callback, nil, nil, false)
	assert.False(t, called)
}

func TestNotifyConfigSavedOnce_force触发(t *testing.T) {
	called := false
	callback := func(updatedKeys []string, envUpdates map[string]any, configPayload map[string]any) error {
		called = true
		return nil
	}
	// force=true，即使无变更也触发
	NotifyConfigSavedOnce(callback, nil, nil, true)
	assert.True(t, called)
}

func TestNotifyConfigSavedOnce_有变更触发(t *testing.T) {
	called := false
	callback := func(updatedKeys []string, envUpdates map[string]any, configPayload map[string]any) error {
		called = true
		return nil
	}
	// 有 envUpdates 变更
	NotifyConfigSavedOnce(callback, map[string]string{"MODEL_PROVIDER": "OpenAI"}, nil, false)
	assert.True(t, called)
}

func TestNotifyConfigSavedOnce_nil回调不触发(t *testing.T) {
	// nil 回调不应 panic
	NotifyConfigSavedOnce(nil, map[string]string{"KEY": "val"}, nil, false)
}

func TestBuildModelsDefaultsFromFrontend_无效类型(t *testing.T) {
	_, err := buildModelsDefaultsFromFrontend("not a list")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must be a non-empty list")
}

func TestBuildModelsDefaultsFromFrontend_空列表(t *testing.T) {
	_, err := buildModelsDefaultsFromFrontend([]any{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must be a non-empty list")
}

func TestBuildModelsDefaultsFromFrontend_缺model_name(t *testing.T) {
	_, err := buildModelsDefaultsFromFrontend([]any{
		map[string]any{"api_key": "key1"},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "model_name is required")
}

func TestBuildModelsDefaultsFromFrontend_缺api_key和origin_index(t *testing.T) {
	_, err := buildModelsDefaultsFromFrontend([]any{
		map[string]any{"model_name": "gpt-4"},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "api_key is required")
}

func TestBuildModelsDefaultsFromFrontend_无效provider(t *testing.T) {
	_, err := buildModelsDefaultsFromFrontend([]any{
		map[string]any{
			"model_name":     "gpt-4",
			"api_key":        "key1",
			"model_provider": "InvalidProvider",
		},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "model_provider must be one of")
}

func TestBuildModelsDefaultsFromFrontend_正常(t *testing.T) {
	result, err := buildModelsDefaultsFromFrontend([]any{
		map[string]any{
			"model_name":     "gpt-4",
			"api_key":        "key1",
			"model_provider": "OpenAI",
			"api_base":       "https://api.openai.com/v1",
			"temperature":    0.7,
			"is_default":     true,
		},
	})
	assert.NoError(t, err)
	assert.Len(t, result, 1)

	mcc, ok := result[0]["model_client_config"].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "gpt-4", mcc["model_name"])
	assert.Equal(t, "OpenAI", mcc["client_provider"])
	assert.Equal(t, "https://api.openai.com/v1", mcc["api_base"])

	mco, ok := result[0]["model_config_obj"].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, 0.7, mco["temperature"])
}

// ──────────────────────────── 非导出函数 ────────────────────────────
