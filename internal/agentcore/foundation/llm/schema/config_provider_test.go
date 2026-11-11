package schema

import "testing"

// TestProviderType_十七值与上游一致 对照上游 ProviderType 枚举。
// Python: schema/config.py:13-31（17 个值）
func TestProviderType_十七值与上游一致(t *testing.T) {
	want := []string{
		"OpenAI", "OpenAIAccount", "OpenRouter", "Anthropic", "SiliconFlow",
		"DashScope", "DeepSeek", "Moonshot", "MiniMax", "ModelArts",
		"VolcEngine", "Qianfan", "Zhipu", "MiMo", "InferenceAffinity",
		"AscendAffinity", "intelli_router",
	}
	if len(providerTypeStrings) != len(want) {
		t.Fatalf("枚举值数 = %d, want %d", len(providerTypeStrings), len(want))
	}
	// Go 新值追加末尾，数值顺序与上游不同，只校验集合等价
	got := map[string]bool{}
	for _, s := range providerTypeStrings {
		got[s] = true
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("缺少 provider %q", w)
		}
	}
}

// TestProviderType_既有零值不位移 验证新值追加末尾。
func TestProviderType_既有零值不位移(t *testing.T) {
	cases := map[string]ProviderType{
		"OpenAI":            ProviderTypeOpenAI,
		"OpenRouter":        ProviderTypeOpenRouter,
		"SiliconFlow":       ProviderTypeSiliconFlow,
		"DashScope":         ProviderTypeDashScope,
		"DeepSeek":          ProviderTypeDeepSeek,
		"InferenceAffinity": ProviderTypeInferenceAffinity,
		"intelli_router":    ProviderTypeIntelliRouter,
	}
	for name, pt := range cases {
		if pt.String() != name {
			t.Errorf("%s.String() = %q", name, pt.String())
		}
	}
	if ProviderTypeOpenAI != 0 {
		t.Fatalf("ProviderTypeOpenAI = %d, want 0（既有零值不得位移）", ProviderTypeOpenAI)
	}
}

// TestParseProviderType_新值可解析 验证新增的 10 个值可被命中。
func TestParseProviderType_新值可解析(t *testing.T) {
	for _, name := range []string{
		"OpenAIAccount", "Anthropic", "Moonshot", "MiniMax", "ModelArts",
		"VolcEngine", "Qianfan", "Zhipu", "MiMo", "AscendAffinity",
	} {
		if _, ok := ParseProviderType(name); !ok {
			t.Errorf("ParseProviderType(%q) 失败", name)
		}
	}
}

// TestKVCacheExtensionConfig_不接受release 对齐上游 Literal["none","affinity"]。
// Python: schema/config.py:48 mode: Literal["none","affinity"] = "none"
func TestKVCacheExtensionConfig_不接受release(t *testing.T) {
	if err := ValidateKVCacheMode("release"); err == nil {
		t.Fatal("release 应被拒绝（上游 Literal 不含它，偏差 D-10）")
	}
	if err := ValidateKVCacheMode("affinity"); err != nil {
		t.Fatalf("affinity 应通过: %v", err)
	}
	if err := ValidateKVCacheMode("none"); err != nil {
		t.Fatalf("none 应通过: %v", err)
	}
	if err := ValidateKVCacheMode(""); err != nil {
		t.Fatalf("空串应通过（未设置）: %v", err)
	}
}

// TestLLMExtensionsConfig_零值可用 验证嵌入 ModelClientConfig 后不 panic。
func TestLLMExtensionsConfig_零值可用(t *testing.T) {
	var ext LLMExtensionsConfig
	if ext.KVCache.Mode != "" {
		t.Fatalf("零值 KVCache.Mode = %q", ext.KVCache.Mode)
	}
	var cfg ModelClientConfig
	if cfg.Extensions != nil {
		t.Fatal("Extensions 零值应为 nil")
	}
}
