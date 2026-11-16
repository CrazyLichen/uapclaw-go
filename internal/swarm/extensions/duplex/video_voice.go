package duplex

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
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

// ──────────────────────────── 结构体（RPC 频道契约，供 extension.go 复用） ────────────────────────────

// DuplexResponse WebChannel 响应描述。
// 对齐 video_voice.py 各 handler 中 channel.send_response(ws, req_id, ok=..., error=..., code=..., payload=...) 调用形态。
type DuplexResponse struct {
	// OK 是否成功
	OK bool
	// Error 错误文案（失败时）
	Error string
	// Code 错误码（失败时）
	Code string
	// Payload 成功载荷
	Payload map[string]any
}

// DuplexMethodHandler RPC 方法签名。
// 对齐 video_voice.py 各 handler 的 (ws, req_id, params, session_id) 形参。
type DuplexMethodHandler func(ctx context.Context, ws any, reqID string, params map[string]any, sessionID string) *DuplexResponse

// DuplexChannel WebChannel 的窄接口（依赖倒置允许场景）。
// Python 侧无对应接口：Python 鸭子类型直接调用 channel 对象。
type DuplexChannel interface {
	// SendResponse 发送 RPC 响应
	SendResponse(ctx context.Context, ws any, reqID string, resp *DuplexResponse)
	// SendEvent 推送事件帧
	SendEvent(ctx context.Context, ws any, event string, payload map[string]any)
	// SendEventChecked 推送事件帧并上报错误（changed 通知需要错误语义）
	SendEventChecked(ctx context.Context, ws any, event string, payload map[string]any) error
	// RegisterMethod 注册本地 RPC 方法
	RegisterMethod(method string, handler DuplexMethodHandler, localOnly, availableWhenDisabled bool)
}

// ──────────────────────────── 导出函数（handlers 段） ────────────────────────────

// CreateVoiceHandlers 构建视频插件的 ASR/TTS RPC handlers。
// 对齐 video_voice.py:219-558 create_voice_handlers。
// 偏差 D-2：stream_tasks 的 asyncio.Task → goroutine + context（取消语义用 ctx.Done 对齐 task.cancel）。
func CreateVoiceHandlers(channel DuplexChannel, useJoyaiVoice func() bool, logEvent func(map[string]any), logASR func(map[string]any)) map[string]DuplexMethodHandler {
	streamTasks := &ttsStreamRegistry{tasks: map[string]*ttsStreamTask{}}

	ttsSynthesize := func(ctx context.Context, ws any, reqID string, params map[string]any, sessionID string) *DuplexResponse {
		startedAt := time.Now()
		text := ""
		if params != nil {
			text = strings.TrimSpace(toString(params["text"]))
		}
		requestLog := map[string]any{
			"stage": "tts_requested", "request_id": reqID, "session_id": sessionID,
			"text_chars": len([]rune(text)), "text_preview": runePreview(text, 300),
		}
		logEvent(requestLog)
		if text == "" || len([]rune(text)) > MaxTTSTextChars {
			logEvent(withEntries(requestLog, map[string]any{
				"stage":      "tts_failed",
				"latency_ms": elapsedMS(startedAt),
				"error":      fmt.Sprintf("text must contain 1-%d characters", MaxTTSTextChars),
			}))
			return &DuplexResponse{OK: false, Error: fmt.Sprintf("text must contain 1-%d characters", MaxTTSTextChars), Code: "BAD_REQUEST"}
		}
		var audio []byte
		var mime, model string
		func() {
			defer RecoverDuplexError(&duplexErrSlot)
			audio, mime, model = SynthesizeSpeech(text, useJoyaiVoice())
		}()
		if duplexErrSlot != nil {
			errText := strings.TrimSpace(duplexErrSlot.Error())
			if errText == "" {
				errText = "TTS failed"
			}
			logEvent(withEntries(requestLog, map[string]any{"stage": "tts_failed", "latency_ms": elapsedMS(startedAt), "error": errText}))
			duplexErrSlot = nil
			return &DuplexResponse{OK: false, Error: errText, Code: "TTS_ERROR"}
		}
		logEvent(withEntries(requestLog, map[string]any{
			"stage": "tts_completed", "latency_ms": elapsedMS(startedAt),
			"audio_bytes": len(audio), "audio_mime": mime, "model": model,
		}))
		return &DuplexResponse{OK: true, Payload: map[string]any{
			"success": true, "audio_base64": base64.StdEncoding.EncodeToString(audio),
			"audio_mime": mime, "model": model,
		}}
	}

	ttsStreamStart := func(ctx context.Context, ws any, reqID string, params map[string]any, sessionID string) *DuplexResponse {
		text := strings.TrimSpace(toString(params["text"]))
		streamID := strings.TrimSpace(toString(params["stream_id"]))
		if text == "" || len([]rune(text)) > MaxTTSTextChars {
			return &DuplexResponse{OK: false, Error: fmt.Sprintf("text must contain 1-%d characters", MaxTTSTextChars), Code: "BAD_REQUEST"}
		}
		if !ttsStreamIDPattern.MatchString(streamID) {
			return &DuplexResponse{OK: false, Error: "stream_id is invalid", Code: "BAD_REQUEST"}
		}
		if !useJoyaiVoice() {
			return &DuplexResponse{OK: false, Error: "streaming TTS is unavailable for the configured voice provider", Code: "TTS_STREAM_UNAVAILABLE"}
		}
		key := streamKey(ws, streamID)
		if streamTasks.active(key) {
			return &DuplexResponse{OK: false, Error: "stream_id is already active", Code: "CONFLICT"}
		}
		logEvent(map[string]any{
			"stage": "tts_stream_requested", "request_id": reqID, "session_id": sessionID,
			"stream_id": streamID, "text_chars": len([]rune(text)), "text_preview": runePreview(text, 300),
		})
		runCtx, cancel := context.WithCancel(context.Background())
		streamTasks.set(key, cancel)
		go runTTSStream(runCtx, channel, ws, streamID, text, reqID, sessionID, startedLogClock(), logEvent, streamTasks, key)
		return &DuplexResponse{OK: true, Payload: map[string]any{"started": true, "stream_id": streamID, "sample_rate": 24000}}
	}

	ttsStreamCancel := func(ctx context.Context, ws any, reqID string, params map[string]any, sessionID string) *DuplexResponse {
		streamID := strings.TrimSpace(toString(params["stream_id"]))
		cancelled := streamTasks.cancel(streamKey(ws, streamID))
		return &DuplexResponse{OK: true, Payload: map[string]any{"stream_id": streamID, "cancelled": cancelled}}
	}

	videoTranscribe := func(ctx context.Context, ws any, reqID string, params map[string]any, sessionID string) *DuplexResponse {
		startedAt := time.Now()
		var dataURL string
		if params != nil {
			dataURL = toString(params["audio_data_url"])
		}
		requestLog := map[string]any{
			"request_id": reqID, "session_id": sessionID,
			"audio_chars": len(dataURL),
			"audio_mime":  dataURLMime(dataURL),
		}
		if len(dataURL) > MaxAudioChars || !isAllowedAudioDataURL(dataURL) {
			logASR(withEntries(requestLog, map[string]any{
				"outcome": "rejected", "transcript": "", "has_transcript": false,
				"latency_ms": elapsedMS(startedAt), "error": "audio_data_url is invalid",
			}))
			return &DuplexResponse{OK: false, Error: "audio_data_url is invalid", Code: "BAD_REQUEST"}
		}
		var transcript string
		func() {
			defer RecoverDuplexError(&duplexErrSlot)
			transcript = TranscribeAudio([][2]string{{dataURL, "用户麦克风"}}, useJoyaiVoice(), logASR)
		}()
		if duplexErrSlot != nil {
			errText := duplexErrSlot.Error()
			if errText == "" {
				errText = "ASR failed"
			}
			logASR(withEntries(requestLog, map[string]any{
				"outcome": "failed", "transcript": "", "has_transcript": false,
				"latency_ms": elapsedMS(startedAt), "error": errText,
			}))
			duplexErrSlot = nil
			return &DuplexResponse{OK: false, Error: errText, Code: "ASR_ERROR"}
		}
		rawTranscript := transcript
		ignoredFiller := isIgnorableASRFiller(rawTranscript)
		if ignoredFiller {
			transcript = ""
		}
		outcome := "empty"
		if ignoredFiller {
			outcome = "ignored_filler"
		} else if strings.TrimSpace(transcript) != "" {
			outcome = "completed"
		}
		asrEvent := withEntries(requestLog, map[string]any{
			"outcome": outcome, "transcript": transcript,
			"has_transcript": strings.TrimSpace(transcript) != "",
			"latency_ms":     elapsedMS(startedAt),
		})
		if ignoredFiller {
			asrEvent["raw_transcript"] = rawTranscript
			asrEvent["ignored_reason"] = "filler_only"
		}
		logASR(asrEvent)
		stageEvent := map[string]any{
			"stage": "asr_completed", "request_id": reqID, "transcript": transcript,
			"has_transcript": strings.TrimSpace(transcript) != "",
		}
		if ignoredFiller {
			stageEvent["stage"] = "asr_ignored_filler"
			stageEvent["raw_transcript"] = rawTranscript
			stageEvent["ignored_reason"] = "filler_only"
		}
		logEvent(stageEvent)
		payload := map[string]any{"transcript": transcript}
		if ignoredFiller {
			payload["ignored_reason"] = "filler_only"
		}
		return &DuplexResponse{OK: true, Payload: payload}
	}

	return map[string]DuplexMethodHandler{
		"video.transcribe":  videoTranscribe,
		"tts.synthesize":    ttsSynthesize,
		"tts.stream.start":  ttsStreamStart,
		"tts.stream.cancel": ttsStreamCancel,
	}
}

// ──────────────────────────── 非导出函数（handlers 辅助） ────────────────────────────

// duplexErrSlot RecoverDuplexError 的单线程槽位（handlers 串行消费）。
// Python 侧无对应实现：Python 异常即返回路径，Go 用包级槽承接 panic 捕获结果。
var duplexErrSlot error

// ttsStreamIDPattern 流 ID 合法字符集。
// 对齐 video_voice.py:414 re[A-Za-z0-9._:-]{1,128}。
var ttsStreamIDPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

// ttsStreamRegistry 流式 TTS 任务登记表。
// 对齐 video_voice.py:221 stream_tasks dict。
type ttsStreamRegistry struct {
	mu    sync.Mutex
	tasks map[string]*ttsStreamTask
}

// ttsStreamTask 单个流式 TTS 任务登记项。
// Python 侧无对应实现：Python 比较 asyncio.current_task 身份，Go 用序号 token 等价。
type ttsStreamTask struct {
	token  int64
	cancel context.CancelFunc
}

// active 判断 key 是否仍在跑。
// 对齐 video_voice.py:431-434 existing is not None and not existing.done()。
func (r *ttsStreamRegistry) active(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.tasks[key]
	return ok
}

// set 登记 key。
func (r *ttsStreamRegistry) set(key string, cancel context.CancelFunc) *ttsStreamTask {
	r.mu.Lock()
	defer r.mu.Unlock()
	task := &ttsStreamTask{token: time.Now().UnixNano(), cancel: cancel}
	r.tasks[key] = task
	return task
}

// cancel 取消并清理，返回是否取消了在跑任务。
// 对齐 video_voice.py:533-539 tts_stream_cancel 的 task.cancel 分支。
func (r *ttsStreamRegistry) cancel(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	task, ok := r.tasks[key]
	if ok {
		delete(r.tasks, key)
		task.cancel()
	}
	return ok
}

// pop 若当前任务仍是登记任务则移除。
// 对齐 video_voice.py:375-378 finally 分支。
func (r *ttsStreamRegistry) pop(key string, current *ttsStreamTask) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if current != nil && r.tasks[key] != nil && r.tasks[key].token == current.token {
		delete(r.tasks, key)
	}
}

// runTTSStream 流式合成并发送 chunk/done/error 事件。
// 对齐 video_voice.py:265-378 run_tts_stream。
func runTTSStream(ctx context.Context, channel DuplexChannel, ws any, streamID, text, requestID, sessionID string, startedAt time.Time, logEvent func(map[string]any), registry *ttsStreamRegistry, key string) {
	firstChunkMS := -1.0
	sequence := 0
	emitChunk := func(chunk []byte) {
		sequence++
		elapsedMS := elapsedMS(startedAt)
		if firstChunkMS < 0 {
			firstChunkMS = elapsedMS
			logEvent(map[string]any{
				"stage": "tts_stream_first_chunk", "request_id": requestID,
				"session_id": sessionID, "stream_id": streamID,
				"text_chars": len([]rune(text)), "first_chunk_ms": firstChunkMS,
				"chunk_bytes": len(chunk),
			})
		}
		channel.SendEvent(ctx, ws, "video.tts.chunk", map[string]any{
			"stream_id": streamID, "sequence": sequence, "sample_rate": 24000,
			"audio_base64": base64.StdEncoding.EncodeToString(chunk),
		})
	}
	var audioBytes, chunkCount int
	func() {
		defer RecoverDuplexError(&duplexErrSlot)
		audioBytes, chunkCount = JoyAIStreamChannelPCM(text, emitChunk)
	}()
	var taskToken *ttsStreamTask
	if task, ok := ctx.Value(ttsCancelKey{}).(*ttsStreamTask); ok {
		taskToken = task
	}
	if duplexErrSlot != nil {
		errText := strings.TrimSpace(duplexErrSlot.Error())
		if errText == "" {
			errText = "TTS stream failed"
		}
		duplexErrSlot = nil
		if ctx.Err() != nil {
			logEvent(map[string]any{
				"stage": "tts_stream_cancelled", "request_id": requestID,
				"session_id": sessionID, "stream_id": streamID,
				"text_chars": len([]rune(text)), "latency_ms": elapsedMS(startedAt),
			})
			channel.SendEvent(ctx, ws, "video.tts.cancelled", map[string]any{"stream_id": streamID})
		} else {
			logEvent(withEntries(map[string]any{
				"stage": "tts_stream_failed", "request_id": requestID,
				"session_id": sessionID, "stream_id": streamID,
				"text_chars": len([]rune(text)), "latency_ms": elapsedMS(startedAt),
			}, map[string]any{"error": errText}))
			channel.SendEvent(ctx, ws, "video.tts.error", map[string]any{"stream_id": streamID, "error": errText})
		}
		registry.pop(key, taskToken)
		return
	}
	latencyMS := elapsedMS(startedAt)
	logEvent(map[string]any{
		"stage": "tts_stream_completed", "request_id": requestID,
		"session_id": sessionID, "stream_id": streamID,
		"text_chars": len([]rune(text)), "latency_ms": latencyMS,
		"first_chunk_ms": firstChunkMS, "audio_bytes": audioBytes,
		"chunk_count": chunkCount, "model": joyaiTTSModelDisplayName,
	})
	channel.SendEvent(ctx, ws, "video.tts.done", map[string]any{
		"stream_id": streamID, "audio_bytes": audioBytes, "chunk_count": chunkCount,
		"latency_ms": latencyMS, "first_chunk_ms": firstChunkMS,
	})
	registry.pop(key, taskToken)
}

// ttsCancelKey runTTSStream 取消函数的 ctx 键。
// Python 侧无对应实现：asyncio.current_task 定位自身，Go 需显式携带。
type ttsCancelKey struct{}

// streamKey 构造 (ws, stream_id) 复合键。
// 对齐 video_voice.py:274 stream_key = (id(ws), stream_id)。
func streamKey(ws any, streamID string) string {
	return fmt.Sprintf("%p|%s", ws, streamID)
}

// startedLogClock 记录起始时刻。
// Python 侧无对应实现：perf_counter() 起点取法。
func startedLogClock() time.Time { return time.Now() }

// elapsedMS 起点到当前的毫秒数（保留 1 位小数）。
// 对齐 video_voice.py 各 round((perf_counter()-started_at)*1000, 1)。
func elapsedMS(startedAt time.Time) float64 {
	return math.Round(float64(time.Since(startedAt).Microseconds())/100.0) / 10.0
}

// toString any → string（非字符串回空串）。
// 对齐 str(params.get(key) or "") 的空安全取法。
func toString(value any) string {
	s, _ := value.(string)
	return s
}

// runePreview 按 rune 截断预览。
// 对齐 text[:300]（Python 按 rune 截断）。
func runePreview(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}

// dataURLMime 提取 dataURL 的 MIME 段。
// 对齐 video_voice.py:485-488 data_url[5:].partition(";")[0]。
func dataURLMime(dataURL string) string {
	if strings.HasPrefix(dataURL, "data:") {
		return strings.Split(dataURL[5:], ";")[0]
	}
	return ""
}

// withEntries 合并 map（后者覆盖前者）。
// Python 侧无对应实现：Python {**base, **extra} 字面量，Go 需显式合并。
func withEntries(base map[string]any, extra map[string]any) map[string]any {
	merged := make(map[string]any, len(base)+len(extra))
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range extra {
		merged[k] = v
	}
	return merged
}
