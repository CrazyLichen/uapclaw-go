package duplex

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

// MaxAudioChars 音频 dataURL 最大字符数。
// 对齐 video_voice.py:20 MAX_AUDIO_CHARS。
const MaxAudioChars = 2_000_000

// MaxTTSTextChars TTS 文本最大字符数。
// 对齐 video_voice.py:21 MAX_TTS_TEXT_CHARS。
const MaxTTSTextChars = 800

// allowedAudioMimeTypes 合法音频 MIME 白名单。
// 对齐 video_voice.py:22-28 _ALLOWED_AUDIO_MIME_TYPES。
var allowedAudioMimeTypes = map[string]bool{
	"audio/webm": true,
	"audio/ogg":  true,
	"audio/wav":  true,
	"audio/mp4":  true,
	"audio/mpeg": true,
}

// asrFillerOnly 纯语气词判定正则。
// 对齐 video_voice.py:29-33 _ASR_FILLER_ONLY（RE2 不支持 IGNORECASE 对非 ASCII 无效，
// Python 同样仅对 ASCII 生效，语义一致）。
var asrFillerOnly = regexp.MustCompile(`^(?:(?:嗯|恩|呃|啊|哦|噢|唔|哼|额|诶|欸|哎|呀|唉|hmm|uhhuh|uh|um|ah|oh|ああ|ええ))+$`)

// audioSuffixByMime 音频 MIME → 文件后缀。
// 对齐 video_voice.py:124-130。
var audioSuffixByMime = map[string]string{
	"audio/mpeg": ".mp3",
	"audio/mp4":  ".m4a",
	"audio/ogg":  ".ogg",
	"audio/webm": ".webm",
	"audio/wav":  ".wav",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TranscribeAudio 批量转写音频输入，返回句号连接的转写文本。
// 对齐 video_voice.py:137-191 transcribe_audio：
// joyai 语音通道走 joyaiTranscribeChannel；否则按端点类型走 transcription/chat 两路。
func TranscribeAudio(audioInputs [][2]string, useJoyaiVoice bool, logEvent func(map[string]any)) string {
	if useJoyaiVoice {
		if len(audioInputs) == 0 {
			return ""
		}
		transcripts := make([]string, 0, len(audioInputs))
		for _, input := range audioInputs {
			text := joyaiTranscribeChannel(input[0], logEvent)
			if text != "" {
				transcripts = append(transcripts, text)
			}
		}
		return strings.Join(transcripts, "。")
	}
	endpoint, apiKey, model := asrModelConfig()
	if len(audioInputs) == 0 || endpoint == "" || apiKey == "" || model == "" {
		return ""
	}
	endpointKind, apiBase := resolveASREndpoint(endpoint, model)
	if endpointKind == "transcription" {
		transcripts := []string{}
		for index, input := range audioInputs {
			filename, audio, mimeType := decodeAudioDataURL(input[0], index+1)
			text := transcriptionRequest(apiBase, apiKey, model, filename, audio, mimeType)
			cleaned := cleanModelText(text)
			if cleaned != "" {
				transcripts = append(transcripts, cleaned)
			}
		}
		return strings.Join(transcripts, "。")
	}
	// chat 路径：音频以 audio_url 内容项提交，附中文转写指令
	content := make([]map[string]any, 0, len(audioInputs)+1)
	for _, input := range audioInputs {
		content = append(content, map[string]any{
			"type":      "audio_url",
			"audio_url": map[string]any{"url": input[0]},
		})
	}
	content = append(content, map[string]any{
		"type": "text",
		"text": "只转写清晰可辨的用户中文，不要解释；静音、噪声或听不清时返回空字符串，不得补写。",
	})
	return cleanModelText(chatTranscribeRequest(apiBase, apiKey, model, content))
}

// SynthesizeSpeech 合成语音，返回 (音频字节, MIME, 模型名)。
// 对齐 video_voice.py:193-218 synthesize_speech。
// 偏差 D-3：httpx → net/http。
func SynthesizeSpeech(text string, useJoyaiVoice bool) ([]byte, string, string) {
	if useJoyaiVoice {
		audio, mime, model := joyaiSynthesizeChannel(text)
		return audio, mime, model
	}
	endpoint, apiKey, model, voice := ttsModelConfig()
	if endpoint == "" || model == "" {
		panicOrError(errors.New("请配置 VOICE_TTS_ENDPOINT 和 VOICE_TTS_MODEL"))
	}
	payload := map[string]any{
		"model":           model,
		"input":           text,
		"response_format": "mp3",
	}
	if voice != "" {
		payload["voice"] = voice
	}
	body, err := json.Marshal(payload)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("stage", "tts").Msg("构造 TTS 请求体失败")
		panicOrError(err)
	}
	req, err := http.NewRequest(http.MethodPost, resolveTTSEndpoint(endpoint), bytes.NewReader(body))
	if err != nil {
		logger.Error(logComponent).Err(err).Str("stage", "tts").Msg("构造 TTS 请求失败")
		panicOrError(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	client := &http.Client{Timeout: 45 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("stage", "tts").Str("endpoint", endpoint).Msg("TTS 请求失败")
		panicOrError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		logger.Error(logComponent).Str("stage", "tts").Int("status", resp.StatusCode).Msg("TTS 非成功状态码")
		panicOrError(fmt.Errorf("TTS 请求失败 (%d)", resp.StatusCode))
	}
	audio, err := readAll(resp.Body)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("stage", "tts").Msg("读取 TTS 音频失败")
		panicOrError(err)
	}
	if len(audio) == 0 {
		panicOrError(errors.New("TTS 没有返回音频"))
	}
	return audio, "audio/mpeg", model
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isAllowedAudioDataURL 校验音频 dataURL 的头部与 base64 参数。
// 对齐 video_voice.py:33-39。
func isAllowedAudioDataURL(value string) bool {
	header, _, found := strings.Cut(value, ",")
	if !found || !strings.HasPrefix(strings.ToLower(header), "data:") {
		return false
	}
	parts := strings.Split(strings.ToLower(header[5:]), ";")
	if !allowedAudioMimeTypes[parts[0]] {
		return false
	}
	for _, param := range parts[1:] {
		if param == "base64" {
			return true
		}
	}
	return false
}

// ttsModelConfig 读取专用 TTS 端点配置（不从其他模型猜测）。
// 对齐 video_voice.py:41-51 tts_model_config。
func ttsModelConfig() (string, string, string, string) {
	return strings.TrimRight(strings.TrimSpace(
		envOr("VOICE_TTS_ENDPOINT", "TTS_API_BASE")), "/"),
		strings.TrimSpace(envOr("VOICE_API_KEY", "TTS_API_KEY")),
		strings.TrimSpace(envOr("VOICE_TTS_MODEL", "TTS_MODEL_NAME")),
		strings.TrimSpace(envOr("VOICE_TTS_VOICE", "TTS_VOICE"))
}

// asrModelConfig 读取归一化 ASR 端点配置（带 ASR_* 回退）。
// 对齐 video_voice.py:53-61 asr_model_config。
func asrModelConfig() (string, string, string) {
	return strings.TrimRight(strings.TrimSpace(
		envOr("VOICE_ASR_ENDPOINT", "ASR_API_BASE")), "/"),
		strings.TrimSpace(envOr("VOICE_API_KEY", "ASR_API_KEY")),
		strings.TrimSpace(envOr("VOICE_ASR_MODEL", "ASR_MODEL_NAME"))
}

// cleanModelText 剥离 <think>…</think> 并去除首尾空白。
// 对齐 video_voice.py:64-66。
func cleanModelText(text string) string {
	return strings.TrimSpace(thinkPattern.ReplaceAllString(text, ""))
}

// thinkPattern 剥离思考段正则。
// 对齐 video_voice.py:65（re.DOTALL）。
var thinkPattern = regexp.MustCompile(`(?s)<think>.*?</think>`)

// isIgnorableASRFiller 仅当 ASR 结果由标点与语气音组成时返回 true。
// 对齐 video_voice.py:68-73。
func isIgnorableASRFiller(transcript string) bool {
	normalized := fillerStripPattern.ReplaceAllString(strings.ToLower(transcript), "")
	return normalized != "" && asrFillerOnly.MatchString(normalized)
}

// fillerStripPattern 去除标点符号与下划线。
// 对齐 video_voice.py:71 re[\W_]+——Python \w 含 Unicode 词字符（汉字为词字符），
// Go 默认 ASCII，故用 \p{L}\p{N} 显式等价。
var fillerStripPattern = regexp.MustCompile(`[^\p{L}\p{N}_]+`)

// asrUsesTranscriptionEndpoint 判断 ASR 是否走 transcription 端点。
// 对齐 video_voice.py:74-84 _asr_uses_transcription_endpoint。
func asrUsesTranscriptionEndpoint(model string) bool {
	mode := strings.ToLower(strings.TrimSpace(envOr("VOICE_ASR_MODE", "ASR_API_MODE")))
	switch mode {
	case "transcription", "transcriptions", "audio":
		return true
	case "chat", "chat_completion", "chat_completions":
		return false
	}
	normalized := strings.ToLower(model)
	return strings.Contains(normalized, "sensevoice") ||
		strings.Contains(normalized, "whisper") ||
		strings.Contains(normalized, "telespeechasr")
}

// openaiEndpointBase 若路径以 route 结尾则剥离 route 得到 SDK base URL。
// 对齐 video_voice.py:86-92 _openai_endpoint_base。
func openaiEndpointBase(endpoint, route string) string {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return strings.TrimRight(endpoint, "/")
	}
	path := strings.TrimRight(parsed.Path, "/")
	if !strings.HasSuffix(strings.ToLower(path), strings.ToLower(route)) {
		return strings.TrimRight(endpoint, "/")
	}
	basePath := strings.TrimRight(path[:len(path)-len(route)], "/")
	parsed.Path = basePath
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

// resolveASREndpoint 解析完整 ASR 端点为 (请求类型, SDK base URL)。
// 对齐 video_voice.py:95-106 resolve_asr_endpoint。
func resolveASREndpoint(endpoint, model string) (string, string) {
	parsed, _ := url.Parse(endpoint)
	path := strings.ToLower(strings.TrimRight(parsed.Path, "/"))
	if strings.HasSuffix(path, "/audio/transcriptions") {
		return "transcription", openaiEndpointBase(endpoint, "/audio/transcriptions")
	}
	if strings.HasSuffix(path, "/chat/completions") {
		return "chat", openaiEndpointBase(endpoint, "/chat/completions")
	}
	kind := "chat"
	if asrUsesTranscriptionEndpoint(model) {
		kind = "transcription"
	}
	return kind, strings.TrimRight(endpoint, "/")
}

// resolveTTSEndpoint 接受完整 speech 端点并保持旧 API-base 兼容。
// 对齐 video_voice.py:108-113 resolve_tts_endpoint。
func resolveTTSEndpoint(endpoint string) string {
	normalized := strings.TrimRight(endpoint, "/")
	parsed, _ := url.Parse(normalized)
	if strings.HasSuffix(strings.ToLower(parsed.Path), "/audio/speech") {
		return normalized
	}
	return normalized + "/audio/speech"
}

// decodeAudioDataURL 解码音频 dataURL 为 (文件名, 字节, MIME)。
// 对齐 video_voice.py:116-135 _decode_audio_data_url。
func decodeAudioDataURL(dataURL string, index int) (string, []byte, string) {
	header, rest, found := strings.Cut(dataURL, ",")
	encoded := rest
	separatorEmpty := !found
	if separatorEmpty || !isAllowedAudioDataURL(dataURL) {
		panicOrError(errors.New("audio data URL is invalid"))
	}
	mimeType := strings.ToLower(strings.Split(header[5:], ";")[0])
	suffix, ok := audioSuffixByMime[mimeType]
	if !ok {
		suffix = ".audio"
	}
	audio, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		panicOrError(errors.New("audio base64 payload is invalid"))
	}
	if len(audio) == 0 {
		panicOrError(errors.New("audio payload is empty"))
	}
	return fmt.Sprintf("microphone-%d%s", index, suffix), audio, mimeType
}

// transcriptionRequest 调用 /audio/transcriptions（multipart 上传）。
// 对齐 video_voice.py:155-168 client.audio.transcriptions.create（openai SDK → net/http，
// Python 侧 SDK 调用，Go 侧等价 HTTP 实现，响应 text 字段提取一致）。
func transcriptionRequest(apiBase, apiKey, model, filename string, audio []byte, mimeType string) string {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	_ = writer.WriteField("model", model)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("stage", "asr").Msg("构造 multipart 失败")
		return ""
	}
	_, _ = part.Write(audio)
	_ = writer.Close()
	req, err := http.NewRequest(http.MethodPost, apiBase+"/audio/transcriptions", &buf)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("stage", "asr").Msg("构造转写请求失败")
		return ""
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+apiKey)
	body := doJSONRequest(req, "asr")
	if text, ok := body["text"].(string); ok {
		return text
	}
	return ""
}

// chatTranscribeRequest 调用 /chat/completions 完成音频转写。
// 对齐 video_voice.py:170-184（max_tokens=160、temperature=0、content 数组消息）。
func chatTranscribeRequest(apiBase, apiKey, model string, content []map[string]any) string {
	payload := map[string]any{
		"model":       model,
		"messages":    []map[string]any{{"role": "user", "content": content}},
		"max_tokens":  160,
		"temperature": 0,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("stage", "asr").Msg("构造 chat 转写请求体失败")
		return ""
	}
	req, err := http.NewRequest(http.MethodPost, apiBase+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		logger.Error(logComponent).Err(err).Str("stage", "asr").Msg("构造 chat 转写请求失败")
		return ""
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	result := doJSONRequest(req, "asr")
	choices, ok := result["choices"].([]any)
	if !ok || len(choices) == 0 {
		return ""
	}
	first, ok := choices[0].(map[string]any)
	if !ok {
		return ""
	}
	message, ok := first["message"].(map[string]any)
	if !ok {
		return ""
	}
	text, _ := message["content"].(string)
	return text
}

// envOr 依次取环境变量，全空回空串。
// Python 侧无对应实现：Python `os.environ.get(A) or os.environ.get(B) or ""` 的 Go 等价。
func envOr(keys ...string) string {
	for _, key := range keys {
		if value := os.Getenv(key); value != "" {
			return value
		}
	}
	return ""
}

// joyaiTranscribeChannel 导出函数的包内别名（对齐 video_voice.py:145 调用形态）。
func joyaiTranscribeChannel(dataURL string, logEvent func(map[string]any)) string {
	return JoyAITranscribeChannel(dataURL, logEvent)
}

// joyaiSynthesizeChannel 导出函数的包内别名（对齐 video_voice.py:195 调用形态）。
func joyaiSynthesizeChannel(text string) ([]byte, string, string) {
	return JoyAISynthesizeChannel(text)
}
