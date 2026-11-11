package endpointprofiles

import (
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// TestLegacyProviderAliases_十四条 对齐上游别名表规模。
// Python: utils/endpoint_profiles.py:104-142 LEGACY_PROVIDER_ALIASES
func TestLegacyProviderAliases_十四条(t *testing.T) {
	if len(legacyProviderAliases) != 14 {
		t.Fatalf("别名条数 = %d, want 14", len(legacyProviderAliases))
	}
}

// TestEndpointProfiles_十七个 对齐上游 profile 表规模。
// Python: utils/endpoint_profiles.py:52-99 ENDPOINT_PROFILES
func TestEndpointProfiles_十七个(t *testing.T) {
	if len(endpointProfiles) != 17 {
		t.Fatalf("profile 数 = %d, want 17", len(endpointProfiles))
	}
}

// TestNormalizeModelClientConfig_DashScope归一 验证别名归一保留原名。
// Python: endpoint_profiles.py:192-232 normalize_model_client_config
func TestNormalizeModelClientConfig_DashScope归一(t *testing.T) {
	in := &llmschema.ModelClientConfig{
		ClientProvider: "DashScope",
		APIKey:         "sk-test",
		APIBase:        "https://dashscope.aliyuncs.com/compatible-mode/v1",
	}
	out, err := NormalizeModelClientConfig(in)
	if err != nil {
		t.Fatalf("归一失败: %v", err)
	}
	if out.ClientProvider != "OpenAI" {
		t.Errorf("ClientProvider = %q, want OpenAI", out.ClientProvider)
	}
	if out.EndpointProfile != "dashscope" {
		t.Errorf("EndpointProfile = %q, want dashscope", out.EndpointProfile)
	}
	// legacy_client_provider 是 provenance，供 reasoning.providerKey 分辨厂商
	if got := LegacyProvider(out); got != "DashScope" {
		t.Errorf("LegacyProvider = %q, want DashScope", got)
	}
	// 不修改入参
	if in.ClientProvider != "DashScope" {
		t.Error("NormalizeModelClientConfig 修改了入参")
	}
	if in.EndpointProfile != "" {
		t.Error("NormalizeModelClientConfig 修改了入参 EndpointProfile")
	}
}

// TestNormalizeModelClientConfig_InferenceAffinity注入KVCache 验证 extensions 注入。
// Python: endpoint_profiles.py:132-138
// TestNormalizeModelClientConfig_InferenceAffinity被拒 严格对齐上游：别名注入的
// mode="release" 不在 KVCacheExtensionConfig 的 Literal["none","affinity"] 内，
// 上游在函数末尾 ModelClientConfig(**data) 处抛 ValidationError。
// Python: endpoint_profiles.py:128 注入 release + schema/config.py:48 Literal 不含它
func TestNormalizeModelClientConfig_InferenceAffinity被拒(t *testing.T) {
	_, err := NormalizeModelClientConfig(&llmschema.ModelClientConfig{
		ClientProvider: "InferenceAffinity",
		APIBase:        "http://127.0.0.1:8000/v1",
	})
	if err == nil {
		t.Fatal("InferenceAffinity 应因 kv_cache.mode=release 被拒（上游 Literal 只允许 none/affinity）")
	}
}

// TestNormalizeModelClientConfig_AscendAffinity 验证 auth_mode 与 affinity 注入。
// Python: endpoint_profiles.py:139-147
func TestNormalizeModelClientConfig_AscendAffinity(t *testing.T) {
	out, err := NormalizeModelClientConfig(&llmschema.ModelClientConfig{
		ClientProvider: "AscendAffinity",
	})
	if err != nil {
		t.Fatalf("归一失败: %v", err)
	}
	if out.AuthMode != llmschema.AuthModeCustomHeaders {
		t.Errorf("AuthMode = %q, want custom_headers", out.AuthMode)
	}
	if out.Extensions == nil || out.Extensions.KVCache.Mode != "affinity" {
		t.Errorf("kv_cache.mode = %+v", out.Extensions)
	}
}

// TestNormalizeModelClientConfig_OpenAIAccount 验证 responses 与 oauth 注入。
// Python: endpoint_profiles.py:148-155
func TestNormalizeModelClientConfig_OpenAIAccount(t *testing.T) {
	out, err := NormalizeModelClientConfig(&llmschema.ModelClientConfig{
		ClientProvider: "OpenAIAccount",
	})
	if err != nil {
		t.Fatalf("归一失败: %v", err)
	}
	if out.APIMode != llmschema.APIModeResponses {
		t.Errorf("APIMode = %q, want responses", out.APIMode)
	}
	if out.AuthMode != llmschema.AuthModeOpenAIAccountOAuth {
		t.Errorf("AuthMode = %q", out.AuthMode)
	}
}

// TestNormalizeModelClientConfig_OpenAI不动 验证无别名 provider 原样返回。
func TestNormalizeModelClientConfig_OpenAI不动(t *testing.T) {
	out, err := NormalizeModelClientConfig(&llmschema.ModelClientConfig{ClientProvider: "OpenAI"})
	if err != nil {
		t.Fatalf("归一失败: %v", err)
	}
	if out.ClientProvider != "OpenAI" || out.EndpointProfile != "" {
		t.Errorf("out = %+v", out)
	}
	if LegacyProvider(out) != "OpenAI" {
		t.Errorf("LegacyProvider = %q", LegacyProvider(out))
	}
}

// TestNormalizeModelClientConfig_显式endpoint不覆盖 验证用户显式配置优先。
func TestNormalizeModelClientConfig_显式endpoint不覆盖(t *testing.T) {
	out, err := NormalizeModelClientConfig(&llmschema.ModelClientConfig{
		ClientProvider:  "DashScope",
		EndpointProfile: "vllm",
	})
	if err != nil {
		t.Fatalf("归一失败: %v", err)
	}
	if out.EndpointProfile != "vllm" {
		t.Errorf("EndpointProfile = %q, want vllm（显式配置应保留）", out.EndpointProfile)
	}
}

// TestNormalizeModelClientConfig_Anthropic协议改走Anthropic客户端
// 验证 anthropic_messages 协议下兼容 provider 改用 Anthropic 客户端。
// Python: endpoint_profiles.py:203-205
func TestNormalizeModelClientConfig_Anthropic协议改走Anthropic客户端(t *testing.T) {
	out, err := NormalizeModelClientConfig(&llmschema.ModelClientConfig{
		ClientProvider: "DashScope",
		APIMode:        "anthropic_messages",
	})
	if err != nil {
		t.Fatalf("归一失败: %v", err)
	}
	if out.ClientProvider != "Anthropic" {
		t.Errorf("ClientProvider = %q, want Anthropic", out.ClientProvider)
	}
	if LegacyProvider(out) != "DashScope" {
		t.Errorf("LegacyProvider = %q, want DashScope", LegacyProvider(out))
	}
}

// TestNormalizeModelClientConfig_nil 验证 nil 安全。
func TestNormalizeModelClientConfig_nil(t *testing.T) {
	got, err := NormalizeModelClientConfig(nil)
	if got != nil || err != nil {
		t.Errorf("got=%+v err=%v, want nil/nil", got, err)
	}
}

// TestResolveEndpointProfile_缺省推断 验证未配置时的缺省档案。
// Python: endpoint_profiles.py:235-247 resolve_endpoint_profile
func TestResolveEndpointProfile_缺省推断(t *testing.T) {
	p, ok := ResolveEndpointProfile(&llmschema.ModelClientConfig{ClientProvider: "OpenAI"})
	if !ok || p.Name != "openai" {
		t.Errorf("p = %+v ok=%v", p, ok)
	}
	p, ok = ResolveEndpointProfile(&llmschema.ModelClientConfig{ClientProvider: "DashScope"})
	if !ok || p.Name != "openai_compatible" {
		t.Errorf("p = %+v ok=%v", p, ok)
	}
}

// TestResolveEndpointProfile_ollama缺省base 验证 default_api_base 表。
func TestResolveEndpointProfile_ollama缺省base(t *testing.T) {
	p, ok := ResolveEndpointProfile(&llmschema.ModelClientConfig{EndpointProfile: "ollama"})
	if !ok || p.DefaultAPIBase == "" {
		t.Errorf("p = %+v ok=%v", p, ok)
	}
}

// TestIsImplicitOpenAIAccountDefault 覆盖隐式默认值判定三分支。
// Python: endpoint_profiles.py:183-190
func TestIsImplicitOpenAIAccountDefault(t *testing.T) {
	// 非 OpenAIAccount → 恒 false
	if isImplicitOpenAIAccountDefault("DashScope", "api_mode", "") {
		t.Error("非 OpenAIAccount 应返回 false")
	}
	// api_mode 空 → true
	if !isImplicitOpenAIAccountDefault("OpenAIAccount", "api_mode", "") {
		t.Error("api_mode 空应为隐式默认")
	}
	// auth_mode=api_key → true
	if !isImplicitOpenAIAccountDefault("OpenAIAccount", "auth_mode", llmschema.AuthModeAPIKey) {
		t.Error("auth_mode=api_key 应为隐式默认")
	}
	// 其它键 → false
	if isImplicitOpenAIAccountDefault("OpenAIAccount", "timeout", 30) {
		t.Error("未知键应返回 false")
	}
	// 显式非默认 auth_mode → false
	if isImplicitOpenAIAccountDefault("OpenAIAccount", "auth_mode", llmschema.AuthModeCustomHeaders) {
		t.Error("显式 auth_mode 不应视为隐式默认")
	}
}

// TestLegacyProvider_nil 覆盖 nil 入参。
func TestLegacyProvider_nil(t *testing.T) {
	if got := LegacyProvider(nil); got != "" {
		t.Errorf("got %q, want 空串", got)
	}
}

// TestNormalizeModelClientConfig_深拷贝 验证 Extensions/Extra 不与入参共享。
func TestNormalizeModelClientConfig_深拷贝(t *testing.T) {
	in := &llmschema.ModelClientConfig{
		ClientProvider: "DashScope",
		Extra:          map[string]any{"k": "v"},
		Extensions: &llmschema.LLMExtensionsConfig{
			PromptCache: map[string]any{"p": 1},
			KVCache:     llmschema.KVCacheExtensionConfig{Mode: "affinity"},
		},
	}
	out, err := NormalizeModelClientConfig(in)
	if err != nil {
		t.Fatalf("归一失败: %v", err)
	}
	// 改动出参不应影响入参
	out.Extra["k"] = "changed"
	out.Extensions.PromptCache["p"] = 2
	if in.Extra["k"] != "v" {
		t.Error("Extra 与入参共享")
	}
	if in.Extensions.PromptCache["p"] != 1 {
		t.Error("PromptCache 与入参共享")
	}
}

// TestNormalizeModelClientConfig_用户显式kvMode优先 验证 extensions 走
// deep_merge_defaults 语义：别名值只作默认，用户显式值胜出。
// Python: endpoint_profiles.py:159-169 deep_merge_defaults(target, defaults)
//
//	merged = deepcopy(defaults); merged[key] = target[key]  ← target 即显式值
//
// 调用侧 :220-230 是 deep_merge_defaults(explicit_extensions, normalized_extensions)。
func TestNormalizeModelClientConfig_用户显式kvMode优先(t *testing.T) {
	in := &llmschema.ModelClientConfig{
		ClientProvider: "InferenceAffinity",
		Extensions: &llmschema.LLMExtensionsConfig{
			KVCache: llmschema.KVCacheExtensionConfig{Mode: "none"},
		},
	}
	out, err := NormalizeModelClientConfig(in)
	if err != nil {
		t.Fatalf("归一失败: %v", err)
	}
	if out.Extensions.KVCache.Mode != "none" {
		t.Errorf("Mode = %q, want none（用户显式值必须保留，不得被别名覆盖）", out.Extensions.KVCache.Mode)
	}
}

// TestNormalizeModelClientConfig_kvMode空缺时填别名值 验证「别名只补空缺」。
func TestNormalizeModelClientConfig_kvMode空缺时填别名值(t *testing.T) {
	out, err := NormalizeModelClientConfig(&llmschema.ModelClientConfig{ClientProvider: "AscendAffinity"})
	if err != nil {
		t.Fatalf("归一失败: %v", err)
	}
	if out.Extensions == nil || out.Extensions.KVCache.Mode != "affinity" {
		t.Errorf("Mode = %+v, want affinity", out.Extensions)
	}
}
