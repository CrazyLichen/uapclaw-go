package web

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	utils "github.com/uapclaw/uapclaw-go/internal/common/utils"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeCrypto 用于测试的模拟加密提供者（带 error 签名，对齐 crypto.CryptoProvider）
type fakeCrypto struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

func (f *fakeCrypto) Encrypt(_ context.Context, plaintext string) (string, error) {
	return "enc(" + plaintext + ")", nil
}

func (f *fakeCrypto) Decrypt(_ context.Context, ciphertext string) (string, error) {
	return "dec(" + ciphertext + ")", nil
}

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

// ──────────────────────────── 非导出函数 ────────────────────────────

// deepCopySliceOfMap 深拷贝 []map[string]any。
func deepCopySliceOfMap(src []map[string]any) []map[string]any {
	if src == nil {
		return nil
	}
	dst := make([]map[string]any, len(src))
	for i, m := range src {
		dst[i] = utils.DeepCopyMap(m)
	}
	return dst
}

func TestProcessFiles_无files字段(t *testing.T) {
	params := map[string]any{"query": "hello"}
	result := ProcessFiles(context.Background(), params)
	assert.Equal(t, "hello", result["query"])
}

func TestProcessFiles_files非列表(t *testing.T) {
	params := map[string]any{"files": "not a list"}
	result := ProcessFiles(context.Background(), params)
	assert.Equal(t, "not a list", result["files"])
}

func TestProcessFiles_空files列表(t *testing.T) {
	params := map[string]any{"files": []any{}}
	result := ProcessFiles(context.Background(), params)
	filesList, ok := result["files"].([]any)
	assert.True(t, ok)
	assert.Empty(t, filesList)
}

func TestProcessFiles_非map文件项(t *testing.T) {
	params := map[string]any{"files": []any{"string_item"}}
	result := ProcessFiles(context.Background(), params)
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
	result := ProcessFiles(context.Background(), params)
	filesList := result["files"].([]any)
	assert.Len(t, filesList, 1)
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
	result := utils.DeepCopyMap(nil)
	assert.Nil(t, result)
}

func TestDeepCopyMap_基本(t *testing.T) {
	src := map[string]any{"key": "val", "num": 42}
	dst := utils.DeepCopyMap(src)
	assert.Equal(t, src, dst)
}

func TestDeepCopyMap_嵌套(t *testing.T) {
	src := map[string]any{
		"nested": map[string]any{
			"inner": "val",
		},
		"list": []any{"a", "b"},
	}
	dst := utils.DeepCopyMap(src)
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
	// 收敛到 config.GetDefaultModels 权威实现（10.6.19 Task15）——冒烟验证
	result := config.GetDefaultModels(nil)
	// 不会 panic（环境无 config.yaml 时可能返回 env 回退条目或空列表）
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
	result := ProcessFiles(context.Background(), params)
	filesList := result["files"].([]any)
	assert.Len(t, filesList, 1)
}

func TestProcessFiles_有uri字段(t *testing.T) {
	params := map[string]any{
		"files": []any{
			map[string]any{"filename": "test.txt", "uri": "http://invalid-host-12345/nonexistent"},
		},
	}
	result := ProcessFiles(context.Background(), params)
	filesList := result["files"].([]any)
	assert.Len(t, filesList, 1)
}

func TestProcessFiles_无name和filename(t *testing.T) {
	params := map[string]any{
		"files": []any{
			map[string]any{"url": "http://invalid-host-12345/nonexistent"},
		},
	}
	result := ProcessFiles(context.Background(), params)
	filesList := result["files"].([]any)
	assert.Len(t, filesList, 1)
}

// fakeCryptoEncryptFail Encrypt 恒失败的 fake（测试加密失败保留明文语义）
type fakeCryptoEncryptFail struct{}

func (f *fakeCryptoEncryptFail) Encrypt(_ context.Context, plaintext string) (string, error) {
	return "", errors.New("encrypt boom")
}

func (f *fakeCryptoEncryptFail) Decrypt(_ context.Context, ciphertext string) (string, error) {
	return "dec(" + ciphertext + ")", nil
}
