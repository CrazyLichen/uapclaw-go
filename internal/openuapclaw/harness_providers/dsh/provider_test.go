package dsh

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_protocol"
)

// Create 经 FromMapping 构造未启动的 harness（Python provider.py:20-22）。
func TestDshProviderCreate(t *testing.T) {
	provider := &DshHarnessProvider{}
	harness, err := provider.Create(harnessprotocol.JsonObject{
		"provider": "deepseek-official",
		"model":    "deepseek-v4-flash",
		"dsh_home": "/tmp/dsh-home",
	})
	require.NoError(t, err)
	assert.Equal(t, "deepseek-harness", harness.Card().Name)

	// 未知配置字段在构造期报错
	_, err = provider.Create(harnessprotocol.JsonObject{"nope": 1})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown DSH configuration fields: nope")
}

// ModelConfig：effort/fallback 显式拒绝 + endpoint 映射
// （Python provider.py:24-41；上游 test_model_config.py:56
// test_dsh_rejects_what_it_has_no_setting_for）。
func TestDshProviderModelConfig(t *testing.T) {
	provider := &DshHarnessProvider{}
	effort := "max"
	model := "deepseek-v4-flash"

	// effort 拒绝
	_, err := provider.ModelConfig(harnessprotocol.ModelChoice{
		Model: &model, Effort: &effort,
	}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DSH has no reasoning effort setting")

	// fallback 拒绝（认证回退面）
	fallback := harnessprotocol.ModelChoice{Model: &model}
	_, err = provider.ModelConfig(harnessprotocol.ModelChoice{Model: &model}, &fallback)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DSH has no authentication fallback")

	// endpoint → base_url/api_key
	apiBase := "https://api.example.com/v1"
	apiKey := "sk-test"
	choice, err := harnessprotocol.NewModelChoice(&model, nil, &harnessprotocol.ModelEndpoint{
		APIBase: &apiBase, APIKey: &apiKey,
	})
	require.NoError(t, err)
	config, err := provider.ModelConfig(choice, nil)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{
		"model":    "deepseek-v4-flash",
		"base_url": apiBase,
		"api_key":  apiKey,
	}, config)

	// 无 endpoint → 仅 model（Python filter-None）
	choice, err = harnessprotocol.NewModelChoice(&model, nil, nil)
	require.NoError(t, err)
	config, err = provider.ModelConfig(choice, nil)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"model": "deepseek-v4-flash"}, config)
}

// Card 转发一致（Python provider.py:16-18）。
func TestDshProviderCard(t *testing.T) {
	provider := &DshHarnessProvider{}
	card := provider.Card()
	assert.Equal(t, "deepseek-harness", card.Name)
	assert.Equal(t, AdapterVersion, card.ImplementationVersion)
	assert.True(t, card.Capabilities[harnessprotocol.HarnessCapabilityMcpTools])
	assert.True(t, card.Capabilities[harnessprotocol.HarnessCapabilityEndpointModels])
	assert.True(t, card.OptionalHostCapabilities[harnessprotocol.HostCapabilityMcpServers])
}
