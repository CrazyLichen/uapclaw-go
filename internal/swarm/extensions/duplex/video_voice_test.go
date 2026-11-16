package duplex

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// ───────────── 纯函数段（对齐 video_voice.py:33-115） ─────────────

// TestIsAllowedAudioDataURL 对齐 video_voice.py:33 is_allowed_audio_data_url。
func TestIsAllowedAudioDataURL_五种MIME与边界(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"data:audio/webm;base64,AAAA", true},
		{"data:audio/ogg;base64,AAAA", true},
		{"data:audio/wav;base64,AAAA", true},
		{"data:audio/mp4;base64,AAAA", true},
		{"data:audio/mpeg;foo;base64,AAAA", true}, // base64 出现在参数列表即认可
		{"data:audio/mp3;base64,AAAA", false},
		{"data:audio/wav,AAAA", false},   // 非 base64 编码
		{"audio/wav;base64,AAAA", false}, // 缺 data: 头
		{"data:text/plain;base64,AAAA", false},
	}
	for _, c := range cases {
		if got := isAllowedAudioDataURL(c.in); got != c.want {
			t.Fatalf("%q 期望 %v, got %v", c.in, c.want, got)
		}
	}
}

// TestTTSASRModelConfig 对齐 video_voice.py:41-61（环境变量优先级链）。
func TestTTSASRModelConfig_环境变量优先级(t *testing.T) {
	t.Setenv("VOICE_TTS_ENDPOINT", " https://tts.example/v1 ")
	t.Setenv("TTS_API_BASE", "https://legacy.example")
	t.Setenv("VOICE_API_KEY", "vk")
	t.Setenv("TTS_API_KEY", "tk")
	t.Setenv("VOICE_TTS_MODEL", " tts-model ")
	t.Setenv("VOICE_TTS_VOICE", " alloy ")
	endpoint, apiKey, model, voice := ttsModelConfig()
	if endpoint != "https://tts.example/v1" || apiKey != "vk" || model != "tts-model" || voice != "alloy" {
		t.Fatalf("VOICE_* 优先: %q %q %q %q", endpoint, apiKey, model, voice)
	}
	os.Unsetenv("VOICE_TTS_ENDPOINT")
	os.Unsetenv("VOICE_TTS_MODEL")
	endpoint, _, model, _ = ttsModelConfig()
	if endpoint != "https://legacy.example" || model != "" {
		t.Fatalf("缺 VOICE_TTS_* 回落 TTS_*: %q %q", endpoint, model)
	}

	t.Setenv("VOICE_ASR_ENDPOINT", " https://asr.example ")
	t.Setenv("ASR_API_BASE", "https://legacy-asr.example")
	t.Setenv("VOICE_ASR_MODEL", " asr-model ")
	aEndpoint, aKey, aModel := asrModelConfig()
	if aEndpoint != "https://asr.example" || aKey != "vk" || aModel != "asr-model" {
		t.Fatalf("VOICE_ASR_* 优先: %q %q %q", aEndpoint, aKey, aModel)
	}
	os.Unsetenv("VOICE_ASR_ENDPOINT")
	aEndpoint, _, _ = asrModelConfig()
	if aEndpoint != "https://legacy-asr.example" {
		t.Fatalf("缺 VOICE_ASR_ENDPOINT 回落 ASR_API_BASE: %q", aEndpoint)
	}
}

// TestCleanModelText 对齐 video_voice.py:64 clean_model_text。
func TestCleanModelText_剥离think(t *testing.T) {
	if got := cleanModelText("<think>推理过程</think>答案"); got != "答案" {
		t.Fatalf("应剥离 think, got %q", got)
	}
	if got := cleanModelText("<think>多行\n推理</think>\n多行\n答案"); got != "多行\n答案" {
		t.Fatalf("DOTALL 多行剥离: %q", got)
	}
	if got := cleanModelText("  纯文本  "); got != "纯文本" {
		t.Fatalf("应 strip: %q", got)
	}
}

// TestIsIgnorableASRFiller 对齐 video_voice.py:68-73。
func TestIsIgnorableASRFiller_中英日语气词(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"嗯", true}, {"嗯。嗯，呃！", true}, {"HMM", true}, {"uh-um", true},
		{"", false}, {"好的", false}, {"嗯这是问题", false},
	}
	for _, c := range cases {
		if got := isIgnorableASRFiller(c.in); got != c.want {
			t.Fatalf("%q 期望 %v, got %v", c.in, c.want, got)
		}
	}
}

// TestASRUsesTranscriptionEndpoint 对齐 video_voice.py:74-84。
func TestASRUsesTranscriptionEndpoint(t *testing.T) {
	t.Setenv("VOICE_ASR_MODE", "transcriptions")
	if !asrUsesTranscriptionEndpoint("any") {
		t.Fatal("mode=transcriptions 应为 true")
	}
	t.Setenv("VOICE_ASR_MODE", "chat_completions")
	if asrUsesTranscriptionEndpoint("whisper") {
		t.Fatal("mode 显式 chat 优先于模型名")
	}
	os.Unsetenv("VOICE_ASR_MODE")
	os.Unsetenv("ASR_API_MODE")
	if !asrUsesTranscriptionEndpoint("SenseVoice-small") || !asrUsesTranscriptionEndpoint("whisper-1") {
		t.Fatal("模型名含已知 ASR 关键字应为 true")
	}
	if asrUsesTranscriptionEndpoint("qwen-max") {
		t.Fatal("普通 chat 模型应为 false")
	}
}

// TestResolveEndpoints 对齐 video_voice.py:86-113。
func TestResolveEndpoints(t *testing.T) {
	kind, base := resolveASREndpoint("https://api.example/v1/audio/transcriptions", "any")
	if kind != "transcription" || base != "https://api.example/v1" {
		t.Fatalf("transcription 路由解析: %q %q", kind, base)
	}
	kind, base = resolveASREndpoint("https://api.example/v1/chat/completions", "any")
	if kind != "chat" || base != "https://api.example/v1" {
		t.Fatalf("chat 路由解析: %q %q", kind, base)
	}
	kind, base = resolveASREndpoint("https://api.example/v1", "whisper-1")
	if kind != "transcription" || base != "https://api.example/v1" {
		t.Fatalf("按模型名推断: %q %q", kind, base)
	}
	if got := resolveTTSEndpoint("https://tts.example/v1/audio/speech"); got != "https://tts.example/v1/audio/speech" {
		t.Fatalf("已含 speech 路由不重复追加: %q", got)
	}
	if got := resolveTTSEndpoint("https://tts.example/v1/"); got != "https://tts.example/v1/audio/speech" {
		t.Fatalf("补 speech 路由: %q", got)
	}
}

// TestDecodeAudioDataURL 对齐 video_voice.py:116-135。
func TestDecodeAudioDataURL_超限与坏base64(t *testing.T) {
	wav := "data:audio/wav;base64," + base64.StdEncoding.EncodeToString([]byte("abc"))
	name, audio, mime := decodeAudioDataURL(wav, 3)
	if name != "microphone-3.wav" || string(audio) != "abc" || mime != "audio/wav" {
		t.Fatalf("解码结果不符: %q %q %q", name, audio, mime)
	}
	if _, _, _, err := catchDecode("data:audio/mp3;base64,AAAA"); err == nil || err.Error() != "audio data URL is invalid" {
		t.Fatalf("非法 MIME 应拒绝: %v", err)
	}
	if _, _, _, err := catchDecode("data:audio/wav;base64,!!!"); err == nil || err.Error() != "audio base64 payload is invalid" {
		t.Fatalf("坏 base64: %v", err)
	}
	if _, _, _, err := catchDecode("data:audio/wav;base64," + base64.StdEncoding.EncodeToString([]byte{})); err == nil || err.Error() != "audio payload is empty" {
		t.Fatalf("空 payload: %v", err)
	}
	// mpeg 后缀
	name, _, _ = decodeAudioDataURL("data:audio/mpeg;base64,"+base64.StdEncoding.EncodeToString([]byte("x")), 2)
	if name != "microphone-2.mp3" {
		t.Fatalf("mpeg 后缀: %q", name)
	}
}

// ───────────── 请求段（对齐 video_voice.py:137-218） ─────────────

// TestTranscribeAudio_假端点 httptest 假 OpenAI 兼容 ASR。
func TestTranscribeAudio_假端点(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(gotPath, "/audio/transcriptions") {
			_ = json.NewEncoder(w).Encode(map[string]any{"text": "<think>x</think> 你好世界 "})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"content": " 语音转写 "}}},
		})
	}))
	defer server.Close()

	t.Setenv("VOICE_ASR_ENDPOINT", server.URL+"/audio/transcriptions")
	t.Setenv("VOICE_API_KEY", "k")
	t.Setenv("VOICE_ASR_MODEL", "whisper-1")
	t.Setenv("VIDEO_LIVE_MODE", "realtime") // 非 joyai 语音通道
	got := TranscribeAudio([][2]string{{"data:audio/wav;base64," + base64.StdEncoding.EncodeToString([]byte("a")), "用户麦克风"}}, false, nil)
	if got != "你好世界" {
		t.Fatalf("transcription 端点结果: %q", got)
	}

	// chat 端点
	t.Setenv("VOICE_ASR_ENDPOINT", server.URL+"/chat/completions")
	got = TranscribeAudio([][2]string{{"data:audio/wav;base64," + base64.StdEncoding.EncodeToString([]byte("a")), "用户麦克风"}}, false, nil)
	if got != "语音转写" {
		t.Fatalf("chat 端点结果: %q", got)
	}

	// 空输入返回空
	if got := TranscribeAudio(nil, false, nil); got != "" {
		t.Fatalf("空输入应返回空串, got %q", got)
	}
}

// catchSynthesize 包装返回 error 的合成调用。
func catchSynthesize(text string, joyai bool) ([]byte, string, string, error) {
	a, m, mo, err := func() (au []byte, mi string, mo string, e error) {
		defer func() {
			if r := recover(); r != nil {
				e = r.(error)
			}
		}()
		au, mi, mo = SynthesizeSpeech(text, joyai)
		return au, mi, mo, nil
	}()
	return a, m, mo, err
}

// TestSynthesizeSpeech_假端点 对齐 video_voice.py:193-218。
func TestSynthesizeSpeech_假端点(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/speech" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Write([]byte("AUDIOBYTES"))
	}))
	defer server.Close()

	t.Setenv("VOICE_TTS_ENDPOINT", server.URL+"/v1")
	t.Setenv("VOICE_API_KEY", "k")
	t.Setenv("VOICE_TTS_MODEL", "tts-model")
	t.Setenv("VOICE_TTS_VOICE", "alloy")
	t.Setenv("VIDEO_LIVE_MODE", "realtime")
	audio, mime, model := SynthesizeSpeech("你好", false)
	if string(audio) != "AUDIOBYTES" || mime != "audio/mpeg" || model != "tts-model" {
		t.Fatalf("TTS 结果不符: %q %q %q", audio, mime, model)
	}

	// 非 2xx
	t.Setenv("VOICE_TTS_ENDPOINT", server.URL+"/none")
	_, _, _, err := catchSynthesize("你好", false)
	if err == nil || err.Error() != "TTS 请求失败 (404)" {
		t.Fatalf("非 2xx 报错: %v", err)
	}
	// 超长文本与缺配置由 handler 层与配置读取路径覆盖（对齐 Python：synthesize 不校验长度）
}

// catchDecode 包装 panic 安全的解码调用（实现返回 error 而非 panic）。
func catchDecode(dataURL string) (string, []byte, string, error) {
	name, audio, mime, err := func() (n string, a []byte, m string, e error) {
		defer func() {
			if r := recover(); r != nil {
				e = r.(error)
			}
		}()
		n, a, m = decodeAudioDataURL(dataURL, 1)
		return n, a, m, nil
	}()
	return name, audio, mime, err
}
