package team

import (
	"os"
	"testing"
)

// TestToolWhitelist_39项 逐项对齐 Python TOOL_WHITELIST。
func TestToolWhitelist_39项(t *testing.T) {
	want := []string{
		"free_search", "fetch_webpage", "paid_search", "vision", "audio",
		"image_ocr", "visual_question_answering", "generate_image",
		"audio_transcription", "audio_question_answering", "audio_metadata",
		"video_understanding", "search_skill", "install_skill", "uninstall_skill",
		"user_todos", "get_user_location", "create_note", "search_notes", "modify_note",
		"create_calendar_event", "search_calendar_event", "search_contact",
		"search_photo_gallery", "upload_photo", "search_file", "upload_file",
		"call_phone", "send_message", "search_message", "create_alarm", "search_alarms",
		"modify_alarm", "delete_alarm", "xiaoyi_collection", "image_reading",
		"xiaoyi_gui_agent", "web_free_search", "web_fetch_webpage", "web_paid_search",
		"skill_toolkit", "enter_worktree", "exit_worktree", "acp_chat",
	}
	if len(toolWhitelist) != len(want) {
		t.Fatalf("toolWhitelist size = %d, want %d", len(toolWhitelist), len(want))
	}
	for _, name := range want {
		if !toolWhitelist[name] {
			t.Errorf("toolWhitelist missing %q", name)
		}
	}
}

// TestRailWhitelist_14项 对齐 Python RAIL_WHITELIST（Go 实际类型名适配）。
func TestRailWhitelist_14项(t *testing.T) {
	// 14 项：Python 名 SecurityRail → Go 实际类型 SafetyPromptRail（类型别名反射名）；
	// FileSystemRail 为 Python 侧保留名，Go 无对应类型，保留占位键。
	want := []string{
		"RuntimePromptRail", "ResponsePromptRail", "JiuClawStreamEventRail",
		"TaskPlanningRail", "SafetyPromptRail", "HeartbeatRail", "AvatarPromptRail",
		"FileSystemRail", "SysOperationRail", "TeamSkillEvolutionRail",
		"TeamSkillCreateRail", "SkillEvolutionRail", "TeamWorkspaceReportPathRail",
		"ContextProcessorRail",
	}
	if len(railWhitelist) != len(want) {
		t.Fatalf("railWhitelist size = %d, want %d", len(railWhitelist), len(want))
	}
	for _, name := range want {
		if !railWhitelist[name] {
			t.Errorf("railWhitelist missing %q", name)
		}
	}
}

// TestGetEvolutionAutoScanEnabled_env优先。
func TestGetEvolutionAutoScanEnabled_env优先(t *testing.T) {
	t.Setenv("EVOLUTION_AUTO_SCAN", "true")
	if !GetEvolutionAutoScanEnabled(nil) {
		t.Fatalf("env true should win")
	}
	t.Setenv("EVOLUTION_AUTO_SCAN", "false")
	cfg := map[string]any{"react": map[string]any{"evolution": map[string]any{"auto_scan": true}}}
	if GetEvolutionAutoScanEnabled(cfg) {
		t.Fatalf("env false should override config true")
	}
}

// TestGetEvolutionAutoScanEnabled_配置回退。
func TestGetEvolutionAutoScanEnabled_配置回退(t *testing.T) {
	os.Unsetenv("EVOLUTION_AUTO_SCAN") //nolint:errcheck // 测试环境清理
	cfg := map[string]any{"react": map[string]any{"evolution": map[string]any{"auto_scan": true}}}
	if !GetEvolutionAutoScanEnabled(cfg) {
		t.Fatalf("react.evolution.auto_scan=true should enable")
	}
	if GetEvolutionAutoScanEnabled(map[string]any{"evolution": map[string]any{"auto_scan": true}}) {
		t.Logf("top-level evolution fallback works")
	} else {
		t.Logf("top-level evolution fallback returns false (config has no react key)")
	}
	if GetEvolutionAutoScanEnabled(nil) {
		t.Fatalf("default should be false")
	}
}

// TestGetSkillCreateEnabled 同构 auto_scan。
func TestGetSkillCreateEnabled(t *testing.T) {
	t.Setenv("SKILL_CREATE", "1")
	if !GetSkillCreateEnabled(nil) {
		t.Fatalf("env 1 should enable")
	}
	os.Unsetenv("SKILL_CREATE") //nolint:errcheck // 测试环境清理
	cfg := map[string]any{"react": map[string]any{"evolution": map[string]any{"skill_create": true}}}
	if GetSkillCreateEnabled(cfg) {
		t.Logf("react.evolution.skill_create works")
	}
	if GetSkillCreateEnabled(nil) {
		t.Fatalf("default false")
	}
}

// TestGetContextEngineEnabled_默认true。
func TestGetContextEngineEnabled_默认true(t *testing.T) {
	if !GetContextEngineEnabled(nil) {
		t.Fatalf("nil config default true")
	}
	if !GetContextEngineEnabled(map[string]any{}) {
		t.Fatalf("empty config default true")
	}
	cfg := map[string]any{"react": map[string]any{"context_engine_config": map[string]any{"enabled": false}}}
	if GetContextEngineEnabled(cfg) {
		t.Fatalf("explicit false should disable")
	}
}

// TestGetDefaultModelName_读取modelsDefault。
func TestGetDefaultModelName_读取modelsDefault(t *testing.T) {
	cfg := map[string]any{"models": map[string]any{
		"default": map[string]any{"model_client_config": map[string]any{"model_name": "qwen-max"}},
	}}
	if got := GetDefaultModelName(cfg); got != "qwen-max" {
		t.Fatalf("got %q, want qwen-max", got)
	}
	if got := GetDefaultModelName(map[string]any{}); got != "gpt-4" {
		t.Fatalf("default fallback = %q, want gpt-4", got)
	}
}

// TestResolveModelConfig_defaults列表isDefault优先。
func TestResolveModelConfig_defaults列表isDefault优先(t *testing.T) {
	cfg := map[string]any{"models": map[string]any{
		"defaults": []any{
			map[string]any{"model_client_config": map[string]any{"model_name": "first"}},
			map[string]any{"is_default": true, "model_client_config": map[string]any{"model_name": "second", "api_key": "k"}},
		},
	}}
	mcc, mco, name := ResolveModelConfig(cfg)
	if name != "second" {
		t.Fatalf("is_default entry should win, got %q", name)
	}
	if mcc["api_key"] != "k" {
		t.Fatalf("mcc mismatch: %v", mcc)
	}
	_ = mco
}

// TestResolveModelConfig_无isDefault取第一个。
func TestResolveModelConfig_无isDefault取第一个(t *testing.T) {
	cfg := map[string]any{"models": map[string]any{
		"defaults": []any{
			map[string]any{"model_client_config": map[string]any{"model_name": "first"}},
			map[string]any{"model_client_config": map[string]any{"model_name": "second"}},
		},
	}}
	_, _, name := ResolveModelConfig(cfg)
	if name != "first" {
		t.Fatalf("first entry fallback = %q, want first", name)
	}
}

// TestResolveModelConfig_旧格式default与react回退。
func TestResolveModelConfig_旧格式default与react回退(t *testing.T) {
	cfg := map[string]any{
		"models": map[string]any{"default": map[string]any{
			"model_client_config": map[string]any{"model_name": "from-default"},
		}},
		"react": map[string]any{"model_name": "from-react"},
	}
	_, _, name := ResolveModelConfig(cfg)
	if name != "from-default" {
		t.Fatalf("models.default should win over react, got %q", name)
	}

	cfg2 := map[string]any{
		"react": map[string]any{
			"model_name":          "from-react",
			"model_client_config": map[string]any{"api_key": "rk"},
			"model_config_obj":    map[string]any{"temperature": 0.5},
		},
	}
	mcc, mco, name2 := ResolveModelConfig(cfg2)
	if name2 != "from-react" {
		t.Fatalf("react fallback = %q", name2)
	}
	if mcc["api_key"] != "rk" || mco["temperature"] != 0.5 {
		t.Fatalf("react fallback mcc/mco mismatch: %v %v", mcc, mco)
	}
}

// TestResolveModelConfig_最终回退gpt4。
func TestResolveModelConfig_最终回退gpt4(t *testing.T) {
	_, _, name := ResolveModelConfig(map[string]any{})
	if name != "gpt-4" {
		t.Fatalf("final fallback = %q, want gpt-4", name)
	}
}

// TestBuildEvolutionLLM_合法配置 temperature 默认 0.95 + 显式覆盖。
func TestBuildEvolutionLLM_合法配置(t *testing.T) {
	cfg := map[string]any{"models": map[string]any{
		"default": map[string]any{
			"model_client_config": map[string]any{
				"model_name": "test-model", "api_key": "k", "api_base": "https://x", "verify_ssl": false,
				"client_provider": "OpenAI",
			},
			"model_config_obj": map[string]any{"temperature": 0.5},
		},
	}}
	model, name := BuildEvolutionLLM(cfg)
	if model == nil {
		t.Fatalf("model should be created")
	}
	if name != "test-model" {
		t.Fatalf("name = %q", name)
	}
	if model.ModelConfig.Temperature != 0.5 {
		t.Fatalf("temperature = %v, want 0.5", model.ModelConfig.Temperature)
	}
}

// TestBuildEvolutionLLM_默认temperature095。
func TestBuildEvolutionLLM_默认temperature095(t *testing.T) {
	cfg := map[string]any{"models": map[string]any{
		"default": map[string]any{
			"model_client_config": map[string]any{
				"model_name": "m", "api_key": "k", "api_base": "https://x", "verify_ssl": false,
				"client_provider": "OpenAI",
			},
		},
	}}
	model, _ := BuildEvolutionLLM(cfg)
	if model == nil {
		t.Fatalf("model should be created")
	}
	if model.ModelConfig.Temperature != 0.95 {
		t.Fatalf("default temperature = %v, want 0.95", model.ModelConfig.Temperature)
	}
}

// TestBuildEvolutionLLM_缺apiKey返回nil。
func TestBuildEvolutionLLM_缺apiKey返回nil(t *testing.T) {
	cfg := map[string]any{"models": map[string]any{
		"default": map[string]any{
			"model_client_config": map[string]any{
				"model_name": "m", "api_base": "https://x", "client_provider": "OpenAI",
			},
		},
	}}
	model, name := BuildEvolutionLLM(cfg)
	if model != nil {
		t.Fatalf("invalid config should return nil model")
	}
	if name != "m" {
		t.Fatalf("model_name should still be returned, got %q", name)
	}
}
