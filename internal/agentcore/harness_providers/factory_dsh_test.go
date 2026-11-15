// 本文件闭合 15.5 待回填① 中归属 dsh 的上游用例（test_factory.py 7 处 dsh
// 引用 + test_model_config.py:28 参数化 dsh 分支 + test_model_config.py:56
// 已迁 dsh/provider_test.go）。dsh 包自注册经 init()，需要外部测试包避免
// internal 测试的 import 环。
package harness_providers_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness_providers"
	_ "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_providers/dsh" // 编译期自注册
	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
)

// ResolveProvider("dsh") 经自注册解析为 DshHarnessProvider
// （上游 test_factory.py:81 create_harness(provider="dsh") 返回 DshHarness）。
func TestResolveProviderDsh(t *testing.T) {
	provider, err := harness_providers.ResolveProvider("dsh")
	require.NoError(t, err)
	assert.Equal(t, "deepseek-harness", provider.Card().Name)
}

// manifest 的 provider 配置经 dsh 的 ModelConfig 映射
// （上游 test_factory.py:61-62 manifest_provider_config(provider="dsh")）。
func TestManifestProviderConfigDsh(t *testing.T) {
	model := "custom"
	choice, err := harnessprotocol.NewModelChoice(&model, nil, &harnessprotocol.ModelEndpoint{
		APIBase: strPtr("https://llm"),
	})
	require.NoError(t, err)
	config, err := harness_providers.ProviderModelConfig("dsh", choice, nil)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{
		"model":    "custom",
		"base_url": "https://llm",
	}, config)
}

// CreateHarness(provider="dsh") 返回满足协议面的 harness
// （上游 test_factory.py:81/148）。
func TestCreateHarnessDsh(t *testing.T) {
	manifest := hschema.AgentTemplateSpec{}
	harness, err := harness_providers.CreateHarness(manifest, harness_providers.CreateHarnessOptions{
		Provider: "dsh",
	})
	require.NoError(t, err)
	assert.Equal(t, "deepseek-harness", harness.Card().Name)
}

func strPtr(value string) *string { return &value }
