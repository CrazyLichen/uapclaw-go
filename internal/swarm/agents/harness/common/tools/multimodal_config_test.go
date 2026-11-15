package tools

import (
	"os"
	"testing"
)

// ──────────────────────────── 工具组清单 ────────────────────────────

// TestMultimodalToolGroups_六组十工具 对齐 Python MULTIMODAL_TOOL_GROUPS:14-21 集合完整性。
func TestMultimodalToolGroups_六组十工具(t *testing.T) {
	expect := map[string][]string{
		"vision tools":         {"image_ocr", "visual_question_answering"},
		"audio tools":          {"audio_transcription", "audio_question_answering", "audio_metadata"},
		"video tool":           {"video_understanding"},
		"generate_image tool":  {"generate_image"},
		"generate_video tools": {"generate_video", "check_video_status"},
		"generate_visual tool": {"generate_visual"},
	}
	if len(MultimodalToolGroups) != 6 {
		t.Fatalf("组数 = %d", len(MultimodalToolGroups))
	}
	total := 0
	for group, names := range expect {
		got, ok := MultimodalToolGroups[group]
		if !ok {
			t.Fatalf("缺组: %s", group)
		}
		total += len(got)
		if len(got) != len(names) {
			t.Fatalf("组 %s 数量不齐: %v", group, got)
		}
		for _, n := range names {
			if !IsMultimodalToolName(n) {
				t.Fatalf("缺工具: %s", n)
			}
		}
	}
	if total != 10 {
		t.Fatalf("工具总数 = %d, 期望 10", total)
	}
	if IsMultimodalToolName("read_pdf") {
		t.Fatal("read_pdf 不应属于多模态集合")
	}
}

// ──────────────────────────── getModelConfig ────────────────────────────

// TestGetModelConfig_dict形状 对齐 _get_model_config:41-57 dict 分支。
func TestGetModelConfig_dict形状(t *testing.T) {
	base := map[string]any{"models": map[string]any{
		"audio": map[string]any{"model_config": map[string]any{"api_key": "k"}},
	}}
	if mc := getModelConfig(base, "audio"); mc["api_key"] != "k" {
		t.Fatalf("model_config 未命中: %v", mc)
	}
	// model_config 缺失回落 model_client_config
	base2 := map[string]any{"models": map[string]any{
		"audio": map[string]any{"model_client_config": map[string]any{"api_key": "c"}},
	}}
	if mc := getModelConfig(base2, "audio"); mc["api_key"] != "c" {
		t.Fatalf("model_client_config 回落失败: %v", mc)
	}
	// 槽存在但两层都缺 → 空
	if mc := getModelConfig(map[string]any{"models": map[string]any{"audio": map[string]any{}}}, "audio"); len(mc) != 0 {
		t.Fatalf("应返回空: %v", mc)
	}
}

// TestGetModelConfig_list形状 对齐 :58-74 list 分支。
func TestGetModelConfig_list形状(t *testing.T) {
	base := map[string]any{"models": []any{
		map[string]any{"vision": map[string]any{"model_config": map[string]any{"api_key": "v"}}},
		map[string]any{"audio": map[string]any{"model_config": map[string]any{"api_key": "a"}}},
	}}
	if mc := getModelConfig(base, "audio"); mc["api_key"] != "a" {
		t.Fatalf("list 形状未命中: %v", mc)
	}
	if mc := getModelConfig(base, "vision"); mc["api_key"] != "v" {
		t.Fatalf("list 多块未命中: %v", mc)
	}
	// models 为字符串（非法）→ 空
	if mc := getModelConfig(map[string]any{"models": "str"}, "audio"); len(mc) != 0 {
		t.Fatalf("非法 models 应返回空: %v", mc)
	}
	// configBase nil → 空
	if mc := getModelConfig(nil, "audio"); len(mc) != 0 {
		t.Fatalf("nil 应返回空: %v", mc)
	}
}

// ──────────────────────────── CompleteMultimodalModelConfigured ────────────────────────────

// TestCompleteMultimodalModelConfigured_四样判定 对齐 :95-109。
func TestCompleteMultimodalModelConfigured_四样判定(t *testing.T) {
	full := func() map[string]any {
		return map[string]any{"models": map[string]any{"vision": map[string]any{
			"model_config": map[string]any{"api_key": "k", "api_base": "https://b", "model_name": "m", "client_provider": "p"},
		}}}
	}
	if !CompleteMultimodalModelConfigured(full(), "vision") {
		t.Fatal("四样齐应 true")
	}
	// 逐项剔除各 false
	missKey := []string{"api_key", "api_base", "model_name", "client_provider"}
	for _, key := range missKey {
		cfg := full()
		mc := cfg["models"].(map[string]any)["vision"].(map[string]any)["model_config"].(map[string]any)
		delete(mc, key)
		if CompleteMultimodalModelConfigured(cfg, "vision") {
			t.Fatalf("缺 %s 应 false", key)
		}
	}
	// provider 走 model_provider 回落
	cfg := full()
	mc := cfg["models"].(map[string]any)["vision"].(map[string]any)["model_config"].(map[string]any)
	delete(mc, "client_provider")
	mc["model_provider"] = "mp"
	if !CompleteMultimodalModelConfigured(cfg, "vision") {
		t.Fatal("model_provider 回落应 true")
	}
	// api_base 缺失用 base_url 回落
	cfg2 := full()
	mc2 := cfg2["models"].(map[string]any)["vision"].(map[string]any)["model_config"].(map[string]any)
	delete(mc2, "api_base")
	mc2["base_url"] = "https://u"
	if !CompleteMultimodalModelConfigured(cfg2, "vision") {
		t.Fatal("base_url 回落应 true")
	}
	// 非三类类型
	if CompleteMultimodalModelConfigured(full(), "image_gen") {
		t.Fatal("image_gen 不在三类内应 false")
	}
}

// ──────────────────────────── MultimodalModelEnabled ────────────────────────────

// TestMultimodalModelEnabled_环境变量优先 对齐 :113-122。
func TestMultimodalModelEnabled_环境变量优先(t *testing.T) {
	t.Setenv("VISION_ENABLED", "1")
	if !MultimodalModelEnabled(nil, "vision") {
		t.Fatal("VISION_ENABLED=1 应 true")
	}
	t.Setenv("VISION_ENABLED", "false")
	if MultimodalModelEnabled(nil, "vision") {
		t.Fatal("VISION_ENABLED=false 应 false")
	}
	t.Setenv("VISION_ENABLED", "on")
	if !MultimodalModelEnabled(nil, "vision") {
		t.Fatal("on 应 true")
	}
}

// TestMultimodalModelEnabled_显式enabled键 对齐 :124-129。
func TestMultimodalModelEnabled_显式enabled键(t *testing.T) {
	os.Unsetenv("AUDIO_ENABLED")
	cfg := map[string]any{"models": map[string]any{"audio": map[string]any{"enabled": true}}}
	if !MultimodalModelEnabled(cfg, "audio") {
		t.Fatal("enabled=true 应 true")
	}
	cfg2 := map[string]any{"models": map[string]any{"audio": map[string]any{"enabled": false}}}
	if MultimodalModelEnabled(cfg2, "audio") {
		t.Fatal("enabled=false 应 false")
	}
}

// TestMultimodalModelEnabled_遗留配置完整保底 对齐 :130 complete_multimodal_model_configured 回落。
func TestMultimodalModelEnabled_遗留配置完整保底(t *testing.T) {
	os.Unsetenv("VIDEO_ENABLED")
	cfg := map[string]any{"models": map[string]any{"video": map[string]any{
		"model_config": map[string]any{"api_key": "k", "api_base": "https://b", "model_name": "m", "model_provider": "p"},
	}}}
	if !MultimodalModelEnabled(cfg, "video") {
		t.Fatal("遗留完整配置应 true")
	}
	// 无任何配置 → false
	if MultimodalModelEnabled(nil, "video") {
		t.Fatal("无配置应 false")
	}
}

// ──────────────────────────── Apply 系列函数 ────────────────────────────

// TestApplyAudioModelConfigFromYAML_models槽优先 对齐 :150-196。
func TestApplyAudioModelConfigFromYAML_models槽优先(t *testing.T) {
	t.Setenv("API_KEY", "envkey")
	cfg := map[string]any{"models": map[string]any{"audio": map[string]any{
		"model_config": map[string]any{"api_key": "k", "api_base": "https://b", "model_name": "m", "client_provider": "p"},
	}}}
	ApplyAudioModelConfigFromYAML(cfg)
	for env, want := range map[string]string{
		"AUDIO_API_KEY": "k", "AUDIO_API_BASE": "https://b", "AUDIO_MODEL_NAME": "m", "AUDIO_PROVIDER": "p",
	} {
		if got := os.Getenv(env); got != want {
			t.Fatalf("%s = %q, want %q", env, got, want)
		}
	}
}

// TestApplyAudioModelConfigFromYAML_embed回落 对齐 :158-176。
func TestApplyAudioModelConfigFromYAML_embed回落(t *testing.T) {
	os.Unsetenv("API_KEY")
	os.Unsetenv("AUDIO_API_KEY")
	cfg := map[string]any{
		"embed": map[string]any{"embed_api_key": "ek", "embed_api_base": "https://e", "audio_model": "em"},
	}
	ApplyAudioModelConfigFromYAML(cfg)
	if os.Getenv("AUDIO_API_KEY") != "ek" || os.Getenv("AUDIO_API_BASE") != "https://e" || os.Getenv("AUDIO_MODEL_NAME") != "em" {
		t.Fatalf("embed 回落失败: %v/%v/%v",
			os.Getenv("AUDIO_API_KEY"), os.Getenv("AUDIO_API_BASE"), os.Getenv("AUDIO_MODEL_NAME"))
	}
}

// TestApplyAudioModelConfigFromYAML_env回落 对齐 :162-168 环境变量第三级。
func TestApplyAudioModelConfigFromYAML_env回落(t *testing.T) {
	os.Unsetenv("AUDIO_API_KEY")
	t.Setenv("API_KEY", "envkey")
	t.Setenv("API_BASE", "https://env")
	t.Setenv("MODEL_NAME", "envmodel")
	t.Setenv("MODEL_PROVIDER", "envprov")
	ApplyAudioModelConfigFromYAML(map[string]any{})
	for env, want := range map[string]string{
		"AUDIO_API_KEY": "envkey", "AUDIO_API_BASE": "https://env",
		"AUDIO_MODEL_NAME": "envmodel", "AUDIO_PROVIDER": "envprov",
	} {
		if got := os.Getenv(env); got != want {
			t.Fatalf("%s = %q, want %q", env, got, want)
		}
	}
}

// TestApplyAudioModelConfigFromYAML_strict 对齐 strict=true 跳过回退链。
func TestApplyAudioModelConfigFromYAML_strict(t *testing.T) {
	for _, env := range []string{"AUDIO_API_KEY", "AUDIO_API_BASE", "AUDIO_MODEL_NAME", "AUDIO_PROVIDER"} {
		os.Unsetenv(env)
	}
	t.Setenv("API_KEY", "envkey")
	cfg := map[string]any{"models": map[string]any{"audio": map[string]any{
		"model_config": map[string]any{"api_key": "k", "strict": true},
	}}}
	ApplyAudioModelConfigFromYAML(cfg)
	if os.Getenv("AUDIO_API_BASE") != "" {
		t.Fatalf("strict 下不应回退 API_BASE: %q", os.Getenv("AUDIO_API_BASE"))
	}
}

// TestApplyVisionModelConfigFromYAML_槽与env前缀 对齐 :198-244。
func TestApplyVisionModelConfigFromYAML_槽与env前缀(t *testing.T) {
	os.Unsetenv("VISION_API_KEY")
	cfg := map[string]any{
		"models": map[string]any{"vision": map[string]any{
			"model_config": map[string]any{"api_key": "vk", "api_base": "https://vb", "model_name": "vm", "model_provider": "vp"},
		}},
		"embed": map[string]any{"embed_api_key": "ek"},
	}
	ApplyVisionModelConfigFromYAML(cfg)
	if os.Getenv("VISION_API_KEY") != "vk" || os.Getenv("VISION_PROVIDER") != "vp" {
		t.Fatalf("vision 槽: %v/%v", os.Getenv("VISION_API_KEY"), os.Getenv("VISION_PROVIDER"))
	}
}

// TestApplyVideoModelConfigFromYAML_strict标志 对齐 :253-263 strict 分支。
func TestApplyVideoModelConfigFromYAML_strict标志(t *testing.T) {
	os.Unsetenv("VIDEO_UNDERSTANDING_STRICT")
	cfg := map[string]any{"models": map[string]any{"video": map[string]any{
		"model_config": map[string]any{"api_key": "k", "strict": true},
	}}}
	ApplyVideoModelConfigFromYAML(cfg)
	if os.Getenv("VIDEO_UNDERSTANDING_STRICT") != "1" {
		t.Fatalf("strict 应设标志: %q", os.Getenv("VIDEO_UNDERSTANDING_STRICT"))
	}
	// 非 strict 清除
	ApplyVideoModelConfigFromYAML(map[string]any{"models": map[string]any{"video": map[string]any{
		"model_config": map[string]any{"api_key": "k"},
	}}})
	if os.Getenv("VIDEO_UNDERSTANDING_STRICT") != "" {
		t.Fatalf("非 strict 应清标志: %q", os.Getenv("VIDEO_UNDERSTANDING_STRICT"))
	}
}

// TestApplyVideoModelConfigFromYAML_nil清除标志 对齐 :251-253。
func TestApplyVideoModelConfigFromYAML_nil清除标志(t *testing.T) {
	_ = os.Setenv("VIDEO_UNDERSTANDING_STRICT", "1")
	ApplyVideoModelConfigFromYAML(nil)
	if os.Getenv("VIDEO_UNDERSTANDING_STRICT") != "" {
		t.Fatal("nil 输入应清标志")
	}
}

// TestDedicatedMultimodalModelConfigured 对齐 Go 既有门控语义（api_key 有即 true）。
func TestDedicatedMultimodalModelConfigured(t *testing.T) {
	cfg := map[string]any{"models": map[string]any{"vision": map[string]any{
		"model_config": map[string]any{"api_key": "k"},
	}}}
	if !DedicatedMultimodalModelConfigured(cfg, "vision") {
		t.Fatal("有 key 应 true")
	}
	if DedicatedMultimodalModelConfigured(nil, "vision") {
		t.Fatal("nil 应 false")
	}
	if DedicatedMultimodalModelConfigured(cfg, "image_gen") {
		t.Fatal("非三类应 false")
	}
}
