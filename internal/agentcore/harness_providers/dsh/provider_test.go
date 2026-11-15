package dsh

import (
	"testing"

	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── provider 用例 ────────────────────────────

// Python: provider.py:20-22 create（经 FromMapping → DshHarness）
func TestDshProviderCreate(t *testing.T) {
	provider := &DshHarnessProvider{}
	harness, err := provider.Create(harnessprotocol.JsonObject{})
	require.NoError(t, err)
	assert.Equal(t, dshCard.Name, harness.Card().Name)
	// 未启动：状态 READY（协议面可查）
	assert.Equal(t, harnessprotocol.HarnessStateTerminated, harness.State())

	// 非法配置拒绝（未知字段）
	_, err = provider.Create(harnessprotocol.JsonObject{"bogus": 1})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown DSH configuration fields: bogus")
}

// Python: provider.py:24-41 model_config +
// test_model_config.py:56 test_dsh_rejects_what_it_has_no_setting_for
func TestDshProviderModelConfig(t *testing.T) {
	provider := &DshHarnessProvider{}

	// endpoint 三元组映射
	apiBase := "https://gw"
	apiKey := "secret"
	model := "m"
	config, err := provider.ModelConfig(harnessprotocol.ModelChoice{
		Model: &model,
		Endpoint: &harnessprotocol.ModelEndpoint{
			APIBase: &apiBase, APIKey: &apiKey,
		},
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, "m", config["model"])
	assert.Equal(t, "https://gw", config["base_url"])
	assert.Equal(t, "secret", config["api_key"])

	// 无 endpoint：仅 model
	config, err = provider.ModelConfig(harnessprotocol.ModelChoice{Model: &model}, nil)
	require.NoError(t, err)
	assert.Equal(t, "m", config["model"])
	assert.NotContains(t, config, "base_url")

	// effort 拒绝
	effort := "high"
	_, err = provider.ModelConfig(harnessprotocol.ModelChoice{Model: &model, Effort: &effort}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DSH has no reasoning effort setting")

	// fallback 拒绝（「认证回退」在 DSH 是明确不支持）
	_, err = provider.ModelConfig(harnessprotocol.ModelChoice{Model: &model},
		&harnessprotocol.ModelChoice{Model: strPtr("n")})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DSH has no authentication fallback")
}

// Python: provider.py:16-18 card 转发
func TestDshProviderCard(t *testing.T) {
	provider := &DshHarnessProvider{}
	card := provider.Card()
	assert.Equal(t, "deepseek-harness", card.Name)
	assert.Equal(t, AdapterVersion, card.ImplementationVersion)
	assert.True(t, card.Capabilities[harnessprotocol.HarnessCapabilityMcpTools])
	assert.True(t, card.Capabilities[harnessprotocol.HarnessCapabilityEndpointModels])
	assert.True(t, card.OptionalHostCapabilities[harnessprotocol.HostCapabilityMcpServers])
}
