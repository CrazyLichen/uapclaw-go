package duplex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSettingsPayload_密钥打码 对齐 settings.py:92 settings_payload——
// secret 类键（SECRET_SETTINGS）在 payload 中置空串并记录长度。
func TestSettingsPayload_密钥打码(t *testing.T) {
	t.Setenv("JOYAI_API_KEY", "  sk-abc123  ")
	t.Setenv("JOYAI_API_BASE", "https://joyai.example")
	payload := SettingsPayload(true)

	values := payload["values"].(map[string]string)
	if values["joyai_api_key"] != "" {
		t.Fatalf("secret 应置空, got %q", values["joyai_api_key"])
	}
	if values["joyai_api_base"] != "https://joyai.example" {
		t.Fatalf("非 secret 应 strip 后原样, got %q", values["joyai_api_base"])
	}
	lengths := payload["configured_secret_lengths"].(map[string]int)
	if lengths["joyai_api_key"] != len("sk-abc123") {
		t.Fatalf("secret 长度应记录 strip 后长度, got %v", lengths)
	}
	if payload["enabled"] != true || payload["restart_required"] != false {
		t.Fatalf("enabled/restart_required 契约不符: %v", payload)
	}
	// provider 键始终存在
	if _, ok := values["video_live_provider"]; !ok {
		t.Fatal("values 必含 video_live_provider")
	}
}

// TestSettingsPayload_未配置回默认值 对齐 settings.py:93-106——
// DEFAULTS 全键在 values 中始终存在，reply_language 非法回默认。
func TestSettingsPayload_未配置回默认值(t *testing.T) {
	for _, k := range []string{"JOYAI_API_KEY", "QWEN_OMNI_MODEL_NAME", "VIDEO_DUPLEX_REPLY_LANGUAGE"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	payload := SettingsPayload(false)
	values := payload["values"].(map[string]string)
	if len(values) != len(defaultsMap())+1 {
		t.Fatalf("values 键数应等于 DEFAULTS+provider=%d, got %d", len(defaultsMap())+1, len(values))
	}
	t.Setenv("VIDEO_DUPLEX_REPLY_LANGUAGE", "fr")
	payload = SettingsPayload(false)
	if payload["values"].(map[string]string)["reply_language"] != "match" {
		t.Fatal("非法 reply_language 应回默认 match")
	}
}

// TestValidatedValues_非法值拒绝 对齐 settings.py:115 _validated_values。
func TestValidatedValues_非法值拒绝(t *testing.T) {
	if _, err := validatedValues(map[string]any{"nope": "1"}); err == nil ||
		!strings.Contains(err.Error(), "unknown full-duplex settings") {
		t.Fatalf("未知键应报 unknown full-duplex settings, got %v", err)
	}
	if _, err := validatedValues(map[string]any{"joyai_model": 123}); err == nil ||
		!strings.Contains(err.Error(), "must be a string") {
		t.Fatalf("非字符串应报 must be a string, got %v", err)
	}
	if _, err := validatedValues(map[string]any{"video_live_provider": "bogus"}); err == nil ||
		!strings.Contains(err.Error(), "video_live_provider must be joyai or qwen_omni") {
		t.Fatalf("非法 provider, got %v", err)
	}
	if _, err := validatedValues(map[string]any{"voice_protocol": "bogus"}); err == nil ||
		!strings.Contains(err.Error(), "voice_protocol must be native_ws or openai_http") {
		t.Fatalf("非法 protocol, got %v", err)
	}
	if _, err := validatedValues(map[string]any{"reply_language": "fr"}); err == nil ||
		!strings.Contains(err.Error(), "reply_language must be match, zh-CN, or en") {
		t.Fatalf("非法 reply_language, got %v", err)
	}
	long := strings.Repeat("a", 4097)
	if _, err := validatedValues(map[string]any{"joyai_model": long}); err == nil ||
		!strings.Contains(err.Error(), "is too long") {
		t.Fatalf("超长应报 too long, got %v", err)
	}
	// 空 reply_language 回默认而非报错
	got, err := validatedValues(map[string]any{"reply_language": "  "})
	if err != nil || got["reply_language"] != "match" {
		t.Fatalf("空 reply_language 应回默认, got %v err=%v", got, err)
	}
}

// TestUpdateSettings_持久化与清除密钥 对齐 settings.py:147 update_settings——
// 空串 secret 未显式 clear_secrets 时跳过；显式清除写空行；provider 映射为两个环境键。
func TestUpdateSettings_持久化与清除密钥(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	writeFile(t, envPath, "JOYAI_API_KEY=\"old\"\nJOYAI_MODEL_NAME=\"m1\"\n")
	setEnvFileForTest(envPath)

	// 更新非 secret + 保留空 secret
	UpdateSettings(map[string]any{
		"video_live_provider": "qwen_omni",
		"joyai_model":         " joyai-large ",
		"joyai_api_key":       "",
	}, false)
	checkFileContains(t, envPath, `VIDEO_LIVE_MODE="realtime"`)
	checkFileContains(t, envPath, `VIDEO_REALTIME_PROVIDER="qwen_omni"`)
	checkFileContains(t, envPath, `JOYAI_MODEL_NAME="joyai-large"`)
	checkFileContains(t, envPath, `JOYAI_API_KEY="old"`) // 未 clear 时空 secret 不覆盖

	// 显式清除
	UpdateSettings(map[string]any{"joyai_api_key": ""}, true)
	checkFileContains(t, envPath, `JOYAI_API_KEY=`)
	if os.Getenv("JOYAI_API_KEY") != "" {
		t.Fatal("显式清除后进程内环境应为空")
	}
	if os.Getenv("VIDEO_LIVE_MODE") != "realtime" {
		t.Fatalf("provider 应同步进程内环境, got %q", os.Getenv("VIDEO_LIVE_MODE"))
	}
}

// TestReplyLanguage_ToolLanguage_默认回退 对齐 settings.py:162-177。
func TestReplyLanguage_ToolLanguage_默认回退(t *testing.T) {
	t.Setenv("VIDEO_DUPLEX_REPLY_LANGUAGE", " en ")
	if ReplyLanguage() != "en" {
		t.Fatalf("带空白应 strip 后合法, got %q", ReplyLanguage())
	}
	t.Setenv("VIDEO_DUPLEX_REPLY_LANGUAGE", "zh-CN")
	if ToolLanguage() != "zh-CN" {
		t.Fatalf("explicit zh-CN 直通, got %q", ToolLanguage())
	}
	t.Setenv("VIDEO_DUPLEX_REPLY_LANGUAGE", "match")
	t.Setenv("UAPCLAW_PREFERRED_LANGUAGE", "en-US")
	if ToolLanguage() != "en" {
		t.Fatalf("match+preferred en- 前缀应得 en, got %q", ToolLanguage())
	}
}

// TestSetEnabled 对齐 settings.py:180 set_enabled。
func TestSetEnabled(t *testing.T) {
	dir := t.TempDir()
	setEnvFileForTest(filepath.Join(dir, ".env"))
	SetEnabled(true)
	if os.Getenv("VIDEO_DUPLEX_ENABLED") != "true" {
		t.Fatal("set true 后环境应为 true")
	}
	SetEnabled(false)
	if os.Getenv("VIDEO_DUPLEX_ENABLED") != "false" {
		t.Fatal("set false 后环境应为 false")
	}
}

// TestNormalizeFileItems 对齐 video_files.py:5 normalize_file_items。
func TestNormalizeFileItems(t *testing.T) {
	if got := NormalizeFileItems("not-a-list"); len(got) != 0 {
		t.Fatalf("非列表应返回空, got %v", got)
	}
	got := NormalizeFileItems([]any{
		map[string]any{
			"name":           " report.pdf ",
			"mime_type":      "application/pdf",
			"download_url":   "https://x/f",
			"download_token": "tok",
			"path":           "/tmp/f",
			"size":           42,
		},
		map[string]any{"name": "", "path": "/x"},                  // 无名丢弃
		map[string]any{"name": "b.txt"},                           // 无任一下载途径丢弃
		map[string]any{"name": "c.txt", "path": "/c", "size": -1}, // 非法 size 归 0
		map[string]any{"name": "d.txt", "path": "/d", "size": "9"}, // 非 int size 归 0 保留
	})
	if len(got) != 3 {
		t.Fatalf("应保留 3 条, got %d: %v", len(got), got)
	}
	first := got[0]
	if first["name"] != "report.pdf" || first["size"] != 42 {
		t.Fatalf("首条字段不符: %v", first)
	}
	if got[1]["size"] != 0 || got[1]["name"] != "c.txt" {
		t.Fatalf("size 归零: %v", got[1])
	}
}

func defaultsMap() map[string]string {
	return map[string]string{
		"joyai_api_base": "", "joyai_api_key": "", "joyai_model": "",
		"qwen_omni_realtime_url": "", "qwen_omni_api_key": "", "qwen_omni_model": "",
		"qwen_omni_voice": "", "reply_language": "match", "voice_protocol": "openai_http",
		"voice_asr_endpoint": "", "voice_tts_endpoint": "", "voice_api_key": "",
		"voice_asr_model": "", "voice_tts_model": "", "voice_tts_voice": "",
	}
}

// setEnvFileForTest 注入临时 .env 路径（配合 envFileOverride）。
func setEnvFileForTest(path string) { envFileOverride = path }

func writeFile(t *testing.T, path, content string) {	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func checkFileContains(t *testing.T, path, sub string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), sub) {
		t.Fatalf("文件应含 %q, 实际:\n%s", sub, data)
	}
}
