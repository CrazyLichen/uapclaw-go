package native

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness_providers"
)

// 本文件迁移 test_factory.py 中依赖 native provider 真身的欠账条目
// （15.5 待回填①的 native 部分；其余条目仍挂 15.7/15.8/15.9/25.1f），
// 以及 provider.py create 配置校验的各分支。
// 上游：tests/unit_tests/harness_providers/test_factory.py:45-96

// ──────────────────────────── 测试替身 ────────────────────────────

// testManifest 对齐 test_factory.py:18-41 _manifest（native 分支所需字段）。
func testManifest() hschema.AgentTemplateSpec {
	return hschema.AgentTemplateSpec{
		AgentCard: *saschema.NewAgentCard(saschema.WithAgentName("expert"), saschema.WithAgentDescription("An expert agent")),
		Model: &hschema.ModelSpec{
			ModelClientConfig: llmschema.ModelClientConfig{
				ClientProvider: "openai",
				APIKey:         "secret",
				APIBase:        "https://llm",
			},
			ModelRequestConfig: llmschema.NewModelRequestConfig(),
		},
	}
}

// ──────────────────────────── 测试 ────────────────────────────

// TestResolveProviderNative 对齐 test_factory.py:45-51 native 成员：
// 内置名解析出 card 名非空的 provider；未知名报 unknown。
func TestResolveProviderNative(t *testing.T) {
	provider, err := harness_providers.ResolveProvider("native")
	require.NoError(t, err)
	assert.Equal(t, ProviderName, provider.Card().Name)

	_, err = harness_providers.ResolveProvider("nope")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown harness provider")
}

// TestAuthFallbackNotDeclared 对齐 test_model_config.py:69-72 的语义锚点：
// native 卡不声明 AUTH_FALLBACK（三方 provider 才有）。
func TestAuthFallbackNotDeclared(t *testing.T) {
	assert.False(t, AgentFactoryCard().Capabilities[harnessprotocol.HarnessCapabilityAuthFallback])
}

// TestManifestProviderConfigNative 对齐 test_factory.py:54-66 native 分支：
// 整份模板 + 语言进配置（factory.py:297-301）。
func TestManifestProviderConfigNative(t *testing.T) {
	config, err := harness_providers.ManifestProviderConfig(testManifest(),
		harness_providers.ManifestProviderConfigOptions{Provider: "native", Language: "en"})
	require.NoError(t, err)
	assert.Equal(t, "en", config["language"])
	template, ok := config["agent_template"].(map[string]any)
	require.True(t, ok)
	card, ok := template["agent_card"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "expert", card["name"])
}

// TestCreateHarnessNativeHarness 对齐 test_factory.py:77-86 native 分支：
// create_harness(provider="native") 返回 DeepAgentHarness 真身；未知名报错。
func TestCreateHarnessNativeHarness(t *testing.T) {
	h, err := harness_providers.CreateHarness(testManifest(),
		harness_providers.CreateHarnessOptions{Provider: "native", Language: "en"})
	require.NoError(t, err)
	native, ok := h.(*DeepAgentHarness)
	require.True(t, ok, "create_harness(native) 应返回 DeepAgentHarness")
	assert.Equal(t, ProviderName, native.Card().Name)

	_, err = harness_providers.CreateHarness(testManifest(),
		harness_providers.CreateHarnessOptions{Provider: "nope"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown harness provider")
}

// TestCreate_配置校验 对齐 provider.py:34-64 create 的各守卫分支。
func TestCreate_配置校验(t *testing.T) {
	p := &NativeHarnessProvider{}

	// 非 object 配置在 Go 类型层不可表达（Create 收 JsonObject map），
	// Python "must be an object" 分支保留为防御（见 provider.go 注释）。
	// 空 object 是合法配置（Python create({}) 也通过），返回 DeepAgentHarness。
	h, err := p.Create(harnessprotocol.JsonObject{})
	require.NoError(t, err)
	_, ok := h.(*DeepAgentHarness)
	require.True(t, ok)

	// 未知键拒绝，键序确定性（provider.py:39-41）
	_, err = p.Create(map[string]any{"deep_agent": map[string]any{}, "bogus": 1, "alpha": 2})
	assert.ErrorContains(t, err, "unknown native harness configuration fields: alpha, bogus")

	// deep_agent 非对象（provider.py:43-44）
	_, err = p.Create(map[string]any{"deep_agent": "x"})
	assert.ErrorContains(t, err, "deep_agent config must be an object")

	// 非法字符串旋钮（provider.py:67-72 _optional_str）
	_, err = p.Create(map[string]any{"deep_agent": map[string]any{}, "session_id": ""})
	assert.ErrorContains(t, err, "non-empty strings")

	// 最小可用配置：返回 DeepAgentHarness，容量缺省 1024
	h2, err := p.Create(map[string]any{"deep_agent": map[string]any{}})
	require.NoError(t, err)
	native, ok2 := h2.(*DeepAgentHarness)
	require.True(t, ok2)
	assert.Equal(t, 1024, native.base.EventBufferConfig().Capacity)
}

// TestCreate_模板兜底 对齐 provider.py:49-52：模板补 spec 缺省的 card 与 model。
func TestCreate_模板兜底(t *testing.T) {
	p := &NativeHarnessProvider{}
	template := map[string]any{
		"agent_card": map[string]any{"id": "tpl", "name": "tpl", "description": "d"},
		"model": map[string]any{
			"model_client_config":  map[string]any{"client_provider": "openai", "api_key": "k", "api_base": "https://llm"},
			"model_request_config": map[string]any{"model": "gpt-tpl"},
		},
	}
	h, err := p.Create(map[string]any{"agent_template": template})
	require.NoError(t, err)
	native, ok := h.(*DeepAgentHarness)
	require.True(t, ok)
	require.NotNil(t, native.agentTemplate)
	assert.Equal(t, "tpl", native.agentTemplate.AgentCard.GetName())
	require.NotNil(t, native.agentTemplate.Model)
}
