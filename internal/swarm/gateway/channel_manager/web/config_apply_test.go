package web

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeCrypto 用于测试的模拟加密提供者
type fakeCrypto struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

func (f *fakeCrypto) Encrypt(plaintext string) string  { return "enc(" + plaintext + ")" }
func (f *fakeCrypto) Decrypt(ciphertext string) string { return "dec(" + ciphertext + ")" }

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
	// nil 回调不应 panic，且 force=true 时打 debug 日志
	NotifyConfigSavedOnce(nil, map[string]string{"KEY": "val"}, nil, false)
	// force=true 时也会打日志而不 panic
	NotifyConfigSavedOnce(nil, nil, nil, true)
}

func TestBuildModelsDefaultsFromFrontend_无效类型(t *testing.T) {
	_, err := buildModelsDefaultsFromFrontend("not a list", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must be a non-empty list")
}

func TestBuildModelsDefaultsFromFrontend_空列表(t *testing.T) {
	_, err := buildModelsDefaultsFromFrontend([]any{}, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must be a non-empty list")
}

func TestBuildModelsDefaultsFromFrontend_缺model_name(t *testing.T) {
	_, err := buildModelsDefaultsFromFrontend([]any{
		map[string]any{"api_key": "key1"},
	}, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "model_name is required")
}

func TestBuildModelsDefaultsFromFrontend_缺api_key和origin_index(t *testing.T) {
	_, err := buildModelsDefaultsFromFrontend([]any{
		map[string]any{"model_name": "gpt-4"},
	}, nil)
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
	}, nil)
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
	}, nil)
	assert.NoError(t, err)
	assert.Len(t, result, 1)

	mcc, ok := result[0]["model_client_config"].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "gpt-4", mcc["model_name"])
	assert.Equal(t, "OpenAI", mcc["client_provider"])
	assert.Equal(t, "https://api.openai.com/v1", mcc["api_base"])
	// verify_ssl 默认 false（对齐 Python: bool(item.get("verify_ssl", False))）
	assert.Equal(t, false, mcc["verify_ssl"])

	mco, ok := result[0]["model_config_obj"].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, 0.7, mco["temperature"])
}

func TestBuildModelsDefaultsFromFrontend_verifySslFalse(t *testing.T) {
	result, err := buildModelsDefaultsFromFrontend([]any{
		map[string]any{
			"model_name":     "gpt-4",
			"api_key":        "key1",
			"model_provider": "OpenAI",
			"verify_ssl":     false,
		},
	}, nil)
	assert.NoError(t, err)
	assert.Len(t, result, 1)

	mcc, ok := result[0]["model_client_config"].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, false, mcc["verify_ssl"])
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// deepCopySliceOfMap 深拷贝 []map[string]any。
func deepCopySliceOfMap(src []map[string]any) []map[string]any {
	if src == nil {
		return nil
	}
	dst := make([]map[string]any, len(src))
	for i, m := range src {
		dst[i] = deepCopyMap(m)
	}
	return dst
}

func TestApplyConfigPayload_正常环境变量(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	defer func() { _ = os.Unsetenv("UAPCLAW_DATA_DIR") }()
	workspace.SetUserHome(workspace.UserHomeDir())

	envUpdates, _, err := ApplyConfigPayload(map[string]any{
		"model_provider": "OpenAI",
		"model":          "gpt-4",
	}, nil)
	require.NoError(t, err)
	assert.Contains(t, envUpdates, "MODEL_PROVIDER")
	assert.Equal(t, "OpenAI", envUpdates["MODEL_PROVIDER"])
	assert.Contains(t, envUpdates, "MODEL_NAME")
	assert.Equal(t, "gpt-4", envUpdates["MODEL_NAME"])
	_ = os.Unsetenv("MODEL_PROVIDER")
	_ = os.Unsetenv("MODEL_NAME")
}

func TestApplyConfigPayload_无效Provider(t *testing.T) {
	_, _, err := ApplyConfigPayload(map[string]any{
		"model_provider": "BadProvider",
	}, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Model provider must in")
}

func TestApplyConfigPayload_nil值(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	defer func() { _ = os.Unsetenv("UAPCLAW_DATA_DIR") }()
	workspace.SetUserHome(workspace.UserHomeDir())

	envUpdates, _, err := ApplyConfigPayload(map[string]any{
		"model": nil,
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, "", envUpdates["MODEL_NAME"])
}

func TestApplyConfigPayload_空params(t *testing.T) {
	envUpdates, yamlUpdated, err := ApplyConfigPayload(map[string]any{}, nil)
	require.NoError(t, err)
	assert.Empty(t, envUpdates)
	assert.Empty(t, yamlUpdated)
}

func TestProcessFiles_无files字段(t *testing.T) {
	params := map[string]any{"query": "hello"}
	result := ProcessFiles(params)
	assert.Equal(t, "hello", result["query"])
}

func TestProcessFiles_files非列表(t *testing.T) {
	params := map[string]any{"files": "not a list"}
	result := ProcessFiles(params)
	assert.Equal(t, "not a list", result["files"])
}

func TestProcessFiles_空files列表(t *testing.T) {
	params := map[string]any{"files": []any{}}
	result := ProcessFiles(params)
	filesList, ok := result["files"].([]any)
	assert.True(t, ok)
	assert.Empty(t, filesList)
}

func TestProcessFiles_非map文件项(t *testing.T) {
	params := map[string]any{"files": []any{"string_item"}}
	result := ProcessFiles(params)
	filesList, ok := result["files"].([]any)
	assert.True(t, ok)
	assert.Len(t, filesList, 1)
}

func TestProcessFiles_本地文件无URL(t *testing.T) {
	params := map[string]any{
		"files": []any{
			map[string]any{"name": "local.txt", "content": "data"},
		},
	}
	result := ProcessFiles(params)
	filesList := result["files"].([]any)
	assert.Len(t, filesList, 1)
}

func TestBuildModelsDefaultsFromFrontend_有originIndex(t *testing.T) {
	result, err := buildModelsDefaultsFromFrontend([]any{
		map[string]any{
			"model_name":   "gpt-4",
			"api_key":      "key1",
			"origin_index": 0,
		},
	}, nil)
	assert.NoError(t, err)
	assert.Len(t, result, 1)
	assert.Equal(t, 0, result[0]["origin_index"])
}

func TestBuildModelsDefaultsFromFrontend_重复Alias(t *testing.T) {
	_, err := buildModelsDefaultsFromFrontend([]any{
		map[string]any{
			"model_name": "gpt-4",
			"api_key":    "key1",
			"alias":      "myalias",
		},
		map[string]any{
			"model_name":   "gpt-3.5",
			"api_key":      "key2",
			"origin_index": 0,
			"alias":        "myalias",
		},
	}, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Alias")
}

func TestBuildModelsDefaultsFromFrontend_非对象项(t *testing.T) {
	_, err := buildModelsDefaultsFromFrontend([]any{"not an object"}, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must be object")
}

func TestMergeModelsForReplaceAll_新条目(t *testing.T) {
	parsed := []map[string]any{
		{
			"model_client_config": map[string]any{
				"model_name": "gpt-4",
				"api_key":    "key1",
			},
			"model_config_obj": map[string]any{
				"temperature": 0.95,
			},
			"is_default": true,
		},
	}
	result := mergeModelsForReplaceAll(parsed, nil, nil, nil)
	assert.Len(t, result, 1)
}

func TestMergeModelsForReplaceAll_有crypto(t *testing.T) {
	crypto := &fakeCrypto{}
	parsed := []map[string]any{
		{
			"model_client_config": map[string]any{
				"model_name": "gpt-4",
				"api_key":    "plaintext",
			},
			"model_config_obj": map[string]any{
				"temperature": 0.95,
			},
			"is_default": true,
		},
	}
	result := mergeModelsForReplaceAll(parsed, nil, nil, crypto)
	assert.Len(t, result, 1)
	mcc, ok := result[0]["model_client_config"].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "enc(plaintext)", mcc["api_key"])
}

func TestInferIsDefault_空列表(t *testing.T) {
	result := inferIsDefault([]map[string]any{})
	assert.Empty(t, result)
}

func TestInferIsDefault_无default(t *testing.T) {
	models := []map[string]any{
		{"is_default": false},
		{"is_default": false},
	}
	result := inferIsDefault(models)
	assert.True(t, isTruthy(result[0]["is_default"]))
}

func TestInferIsDefault_多个default(t *testing.T) {
	models := []map[string]any{
		{"is_default": true},
		{"is_default": true},
	}
	result := inferIsDefault(models)
	assert.True(t, isTruthy(result[0]["is_default"]))
	assert.False(t, isTruthy(result[1]["is_default"]))
}

func TestDeepCopyMap_Nil(t *testing.T) {
	result := deepCopyMap(nil)
	assert.Nil(t, result)
}

func TestDeepCopyMap_基本(t *testing.T) {
	src := map[string]any{"key": "val", "num": 42}
	dst := deepCopyMap(src)
	assert.Equal(t, src, dst)
}

func TestDeepCopyMap_嵌套(t *testing.T) {
	src := map[string]any{
		"nested": map[string]any{
			"inner": "val",
		},
		"list": []any{"a", "b"},
	}
	dst := deepCopyMap(src)
	assert.Equal(t, src, dst)
	// 修改 dst 不影响 src
	dst["nested"].(map[string]any)["inner"] = "modified"
	assert.Equal(t, "val", src["nested"].(map[string]any)["inner"])
}

func TestLoadRawModelsDefaults(t *testing.T) {
	// 无配置文件时返回 nil 或空列表
	result := loadRawModelsDefaults()
	// 不会 panic
	_ = result
}

func TestGetDefaultModels(t *testing.T) {
	// 无配置文件时返回 nil 或空列表
	result := GetDefaultModels()
	// 不会 panic
	_ = result
}

func TestGetConfigSnapshot(t *testing.T) {
	result := getConfigSnapshot()
	// 不会 panic，返回 map
	assert.NotNil(t, result)
}

func TestIsTruthy_更多类型(t *testing.T) {
	assert.True(t, isTruthy(1))
	assert.False(t, isTruthy(0))
	assert.True(t, isTruthy(1.5))
	assert.False(t, isTruthy(0.0))
	assert.True(t, isTruthy("YES"))
}

func TestNotifyConfigSavedOnce_回调返回错误(t *testing.T) {
	callback := func(updatedKeys []string, envUpdates map[string]any, configPayload map[string]any) error {
		return fmt.Errorf("callback error")
	}
	// 不应 panic
	NotifyConfigSavedOnce(callback, map[string]string{"KEY": "val"}, nil, false)
}

func TestMergeModelsForReplaceAll_有originIndex匹配(t *testing.T) {
	rawDefaults := []map[string]any{
		{
			"model_client_config": map[string]any{
				"model_name":      "gpt-4",
				"api_key":         "old_key",
				"client_provider": "OpenAI",
			},
			"model_config_obj": map[string]any{
				"temperature": 0.95,
			},
			"is_default": true,
		},
	}
	resolvedDefaults := deepCopySliceOfMap(rawDefaults)
	parsed := []map[string]any{
		{
			"model_client_config": map[string]any{
				"model_name":      "gpt-4",
				"api_key":         "new_key",
				"client_provider": "OpenAI",
			},
			"model_config_obj": map[string]any{
				"temperature": 0.7,
			},
			"is_default":   true,
			"origin_index": 0,
		},
	}
	result := mergeModelsForReplaceAll(parsed, rawDefaults, resolvedDefaults, nil)
	assert.Len(t, result, 1)
}

func TestMergeModelsForReplaceAll_originIndex越界(t *testing.T) {
	parsed := []map[string]any{
		{
			"model_client_config": map[string]any{
				"model_name": "gpt-4",
				"api_key":    "key1",
			},
			"model_config_obj": map[string]any{
				"temperature": 0.95,
			},
			"is_default":   true,
			"origin_index": 99,
		},
	}
	result := mergeModelsForReplaceAll(parsed, nil, nil, nil)
	assert.Len(t, result, 1)
}

func TestMergeModelsForReplaceAll_originIndex非int(t *testing.T) {
	parsed := []map[string]any{
		{
			"model_client_config": map[string]any{
				"model_name": "gpt-4",
				"api_key":    "key1",
			},
			"model_config_obj": map[string]any{
				"temperature": 0.95,
			},
			"is_default":   true,
			"origin_index": "0",
		},
	}
	result := mergeModelsForReplaceAll(parsed, nil, nil, nil)
	assert.Len(t, result, 1)
}

func TestMergeModelsForReplaceAll_有crypto且originIndex(t *testing.T) {
	crypto := &fakeCrypto{}
	rawDefaults := []map[string]any{
		{
			"model_client_config": map[string]any{
				"model_name": "gpt-4",
				"api_key":    "old_enc_key",
			},
			"model_config_obj": map[string]any{
				"temperature": 0.95,
			},
			"is_default": true,
		},
	}
	resolvedDefaults := deepCopySliceOfMap(rawDefaults)
	parsed := []map[string]any{
		{
			"model_client_config": map[string]any{
				"model_name": "gpt-4",
				"api_key":    "new_key",
			},
			"model_config_obj": map[string]any{
				"temperature": 0.7,
			},
			"is_default":   true,
			"origin_index": 0,
		},
	}
	result := mergeModelsForReplaceAll(parsed, rawDefaults, resolvedDefaults, crypto)
	assert.Len(t, result, 1)
}

func TestValuesMatch_更多场景(t *testing.T) {
	// 一边是 bool
	assert.True(t, valuesMatch(true, "true"))
	assert.False(t, valuesMatch(false, "true"))
	// 一边是 nil
	assert.False(t, valuesMatch(nil, "val"))
	assert.False(t, valuesMatch("val", nil))
	// 数值不匹配
	assert.False(t, valuesMatch(1, 2))
	// parseFloat 失败时走字符串比较
	assert.False(t, valuesMatch(true, 1))
}

func TestProcessFiles_有URL但下载失败(t *testing.T) {
	params := map[string]any{
		"files": []any{
			map[string]any{"name": "test.txt", "url": "http://invalid-host-12345/nonexistent"},
		},
	}
	// 下载失败但不会 panic
	result := ProcessFiles(params)
	filesList := result["files"].([]any)
	assert.Len(t, filesList, 1)
}

func TestProcessFiles_有uri字段(t *testing.T) {
	params := map[string]any{
		"files": []any{
			map[string]any{"filename": "test.txt", "uri": "http://invalid-host-12345/nonexistent"},
		},
	}
	result := ProcessFiles(params)
	filesList := result["files"].([]any)
	assert.Len(t, filesList, 1)
}

func TestProcessFiles_无name和filename(t *testing.T) {
	params := map[string]any{
		"files": []any{
			map[string]any{"url": "http://invalid-host-12345/nonexistent"},
		},
	}
	result := ProcessFiles(params)
	filesList := result["files"].([]any)
	assert.Len(t, filesList, 1)
}
