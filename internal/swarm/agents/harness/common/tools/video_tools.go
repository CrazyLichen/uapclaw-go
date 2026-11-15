package tools

// video_understanding 工具：视频理解调用侧。
//
// 对齐 Python jiuwenswarm/agents/harness/common/tools/video_tools.py（235 行）。
// 接收视频文件路径/URL + 问题 → 「视频模型槽」（VIDEO_API_KEY/VIDEO_API_BASE，
// 经 ApplyVideoModelConfigFromYAML 落 env）→ chat-completions 式理解调用 → 理解文本。
//
// 语言差异：
//   - Python requests.post → Go net/http（verify 走 getRequestsVerify）
//   - Python asyncio.to_thread → Go 工具闭包已在框架 goroutine 中，直接同步调用
//   - Python ProxyError 回退 trust_env=False Session → Go Transport.Proxy 显式关 nil
//   - 工具闭包返回 str → Go 返回 string（经 map[string]any{"result": ...} 包装同 read_pdf）

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// ──────────────────────────── 常量 ────────────────────────────

// userAgent 视频理解请求 UA。
// Python: _USER_AGENT:25-29
const userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) " +
	"AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

// supportedVideoModelAliases 视频模型选择白名单。
// Python: _SUPPORTED_VIDEO_MODEL_ALIASES:35-41
var supportedVideoModelAliases = []string{
	"video_understanding",
	"video_tools.py",
	"jiuwenswarm/agentserver/tools/video_tools.py",
}

// ──────────────────────────── 结构体 ────────────────────────────

// VideoUnderstandingRequest 视频理解请求参数。
// Python: VideoUnderstandingRequest:60-69
type VideoUnderstandingRequest struct {
	// Query 关于视频的问题
	Query string
	// VideoPath 视频本地路径或 HTTP(S) URL
	VideoPath string
	// Model 理解模型名，默认 glm-4.6v
	Model string
	// TimeoutSeconds 请求超时（秒），钳制 [10,600]
	TimeoutSeconds int
	// MaxTokens 输出上限，钳制 [128,8192]
	MaxTokens int
	// Temperature 采样温度，钳制 [0.0,2.0]
	Temperature float64
	// ThinkingEnabled 是否开启 thinking
	ThinkingEnabled bool
}

// ──────────────────────────── 导出函数 ────────────────────────────

// CreateVideoUnderstandingTool 构造 video_understanding 工具。
// Python: @tool(name="video_understanding", ...)（video_tools.py:220-235）
func CreateVideoUnderstandingTool() tool.Tool {
	description := "Analyze and understand video content. " +
		"Use this tool when the user provides a video file path (e.g., .mp4, .mov, .avi) " +
		"or video URL and asks questions about the video content, such as describing " +
		"scenes, actions, people, or objects in the video. " +
		"Input: query (question about the video) and video_path (local file path or HTTP/HTTPS URL)."
	card := tool.NewToolCard("video_understanding", description, nil, nil)
	fn := func(_ context.Context, input struct {
		Query           string  `json:"query"`
		VideoPath       string  `json:"video_path"`
		Model           string  `json:"model"`
		TimeoutSeconds  int     `json:"timeout_seconds"`
		MaxTokens       int     `json:"max_tokens"`
		Temperature     float64 `json:"temperature"`
		ThinkingEnabled bool    `json:"thinking_enabled"`
	}, _ ...tool.ToolOption) (map[string]any, error) {
		result := videoUnderstandingCore(map[string]any{
			"query":            input.Query,
			"video_path":       input.VideoPath,
			"model":            input.Model,
			"timeout_seconds":  input.TimeoutSeconds,
			"max_tokens":       input.MaxTokens,
			"temperature":      input.Temperature,
			"thinking_enabled": input.ThinkingEnabled,
		})
		return map[string]any{"result": result}, nil
	}
	invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
	return invokeFn
}

// NormalizeVideoModelSelection 模型选择值归一化：剥 @ 前缀、\→/、小写。
// Python: _normalize_video_model_selection:42-49
func NormalizeVideoModelSelection(value string) string {
	v := strings.TrimSpace(value)
	v = strings.TrimPrefix(v, "@")
	v = strings.ReplaceAll(v, "\\", "/")
	return strings.ToLower(v)
}

// IsVideoModelSupported 视频模型选择是否在白名单内；空值放行。
// Python: _is_video_model_supported:50-57
func IsVideoModelSupported(selection string) bool {
	normalized := NormalizeVideoModelSelection(selection)
	if normalized == "" {
		return true
	}
	for _, alias := range supportedVideoModelAliases {
		if normalized == alias || strings.HasSuffix(normalized, alias) {
			return true
		}
	}
	return false
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// videoUnderstandingCore 工具主体（对齐 Python 的 try/except 包裹）。
// Python: video_understanding:220-235
func videoUnderstandingCore(inputs map[string]any) (result string) {
	defer func() {
		if r := recover(); r != nil {
			result = fmt.Sprintf("[ERROR]: glm video understanding failed: %v", r)
		}
	}()
	// Python: apply_video_model_config_from_yaml(get_config())，失败仅 warning
	func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Warn(logComponent).
					Str("error", fmt.Sprint(r)).
					Msg("[video_understanding] refresh config failed")
			}
		}()
		ApplyVideoModelConfigFromYAML(loadConfigMapOrEmpty())
	}()
	req := normalizeVideoUnderstandingRequest(inputs)
	logger.Info(logComponent).
		Str("model", req.Model).
		Str("api_base", os.Getenv("VIDEO_API_BASE")).
		Msg("[video_understanding] using model")
	return glmVideoUnderstandingSync(req)
}

// normalizeVideoUnderstandingRequest 归一化工具输入。
// Python: _normalize_request:129-149
func normalizeVideoUnderstandingRequest(inputs map[string]any) VideoUnderstandingRequest {
	query := strings.TrimSpace(strValOrEmpty(inputs["query"]))
	videoPath := strings.TrimSpace(strValOrEmpty(inputs["video_path"]))
	defaultModel := strings.TrimSpace(os.Getenv("VIDEO_MODEL_NAME"))
	if defaultModel == "" {
		defaultModel = "glm-4.6v"
	}
	model := strings.TrimSpace(strValOrEmpty(strOrAny(inputs["model"], defaultModel)))
	timeoutSeconds := clampInt(toIntDefault(inputs["timeout_seconds"], 120), 10, 600, 120)
	maxTokens := clampInt(toIntDefault(inputs["max_tokens"], 2048), 128, 8192, 2048)
	temperature := clampFloat(toFloatDefault(inputs["temperature"], 0.2), 0.0, 2.0)
	thinkingEnabled, _ := inputs["thinking_enabled"].(bool)

	if query == "" {
		panic(fmt.Errorf("query cannot be empty."))
	}
	if videoPath == "" {
		panic(fmt.Errorf("video_path cannot be empty."))
	}
	return VideoUnderstandingRequest{
		Query: query, VideoPath: videoPath, Model: model,
		TimeoutSeconds: timeoutSeconds, MaxTokens: maxTokens,
		Temperature: temperature, ThinkingEnabled: thinkingEnabled,
	}
}

// resolveChatCompletionsURL 拼 chat/completions 端点。
// Python: _resolve_chat_completions_url:151-156
func resolveChatCompletionsURL(base string) string {
	b := strings.TrimRight(strings.TrimSpace(base), "/")
	if b == "" {
		return ""
	}
	if strings.HasSuffix(b, "/chat/completions") {
		return b
	}
	return b + "/chat/completions"
}

// glmVideoUnderstandingSync 同步调用视频理解 API。
// Python: _glm_video_understanding_sync:158-218
func glmVideoUnderstandingSync(req VideoUnderstandingRequest) string {
	yamlKey := strings.TrimSpace(os.Getenv("VIDEO_API_KEY"))
	yamlBase := strings.TrimSpace(os.Getenv("VIDEO_API_BASE"))

	var apiKey, apiURL string
	if yamlKey != "" && yamlBase != "" {
		apiKey = yamlKey
		apiURL = resolveChatCompletionsURL(yamlBase)
	} else if yamlKey != "" && yamlBase == "" {
		panic(fmt.Errorf("VIDEO_API_BASE is required when VIDEO_API_KEY is set."))
	} else {
		apiKey = strings.TrimSpace(os.Getenv("ZHIPU_API_KEY"))
		if apiKey == "" {
			panic(fmt.Errorf(
				"No video API credentials. Config file: %s\n"+
					"Set models.video.model_config with api_key and api_base, or set ZHIPU_API_KEY.",
				path.ConfigFile()))
		}
		apiURL = envURLOrDefault("ZHIPU_API_URL", "https://open.bigmodel.cn/api/paas/v4/chat/completions")
	}

	videoURL := videoPathToURL(req.VideoPath)

	// Python: payload 组装（messages: video_url 块在前 + text 块在后）
	content := []map[string]any{
		{"type": "video_url", "video_url": map[string]any{"url": videoURL}},
		{"type": "text", "text": req.Query},
	}
	payload := map[string]any{
		"model":       req.Model,
		"messages":    []map[string]any{{"role": "user", "content": content}},
		"stream":      false,
		"max_tokens":  req.MaxTokens,
		"temperature": req.Temperature,
	}
	if req.ThinkingEnabled {
		payload["thinking"] = map[string]any{"type": "enabled"}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}

	httpReq, err := http.NewRequest(http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		panic(err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", userAgent)
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	client := &http.Client{Timeout: time.Duration(req.TimeoutSeconds) * time.Second}
	if !getRequestsVerify() {
		// Python: verify=False → 跳过 TLS 校验
		client.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		panic(err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Python: error_data.get("error", {}).get("message", response.text[:200])
		errorMsg := truncate(string(respBody), 200)
		var errorData map[string]any
		if json.Unmarshal(respBody, &errorData) == nil {
			if errObj, ok := errorData["error"].(map[string]any); ok {
				if msg, ok := errObj["message"].(string); ok && msg != "" {
					errorMsg = msg
				}
			}
		}
		panic(fmt.Errorf("API error %d: %s", resp.StatusCode, errorMsg))
	}

	var data map[string]any
	if err := json.Unmarshal(respBody, &data); err != nil {
		panic(err)
	}
	answer := extractVideoAnswer(data)
	if answer == "" {
		return "[ERROR]: GLM returned empty answer."
	}
	return answer
}

// videoPathToURL 本地路径读字节转 data URI；URL 原样返回。
// Python: _video_path_to_url:93-108
func videoPathToURL(videoPath string) string {
	value := strings.TrimSpace(videoPath)
	if value == "" {
		panic(fmt.Errorf("video_path cannot be empty"))
	}
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return value
	}
	expanded := expandHome(value)
	info, err := os.Stat(expanded)
	if err != nil {
		panic(fmt.Errorf("video file does not exist: %s", expanded))
	}
	if info.IsDir() {
		panic(fmt.Errorf("video_path is not a file: %s", expanded))
	}
	mime := guessVideoMime(expanded)
	data, err := os.ReadFile(expanded)
	if err != nil {
		panic(err)
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
}

// guessVideoMime 视频文件 mime 推断。
// Python: _guess_video_mime:80-91
func guessVideoMime(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mov":
		return "video/quicktime"
	case ".avi":
		return "video/x-msvideo"
	case ".mkv":
		return "video/x-matroska"
	case ".webm":
		return "video/webm"
	case ".mpeg", ".mpg":
		return "video/mpeg"
	case ".m4v":
		return "video/x-m4v"
	default: // ".mp4" 与未知扩展统一回落
		return "video/mp4"
	}
}

// extractVideoAnswer 从 chat-completions 响应提取答案文本。
// Python: _extract_answer:110-127
func extractVideoAnswer(data map[string]any) string {
	choices, ok := data["choices"].([]any)
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
	switch content := message["content"].(type) {
	case string:
		return strings.TrimSpace(content)
	case []any:
		var texts []string
		for _, item := range content {
			if m, ok := item.(map[string]any); ok {
				if text, ok := m["text"].(string); ok && text != "" {
					texts = append(texts, text)
				}
			}
		}
		return strings.TrimSpace(strings.Join(texts, "\n"))
	default:
		return strings.TrimSpace(fmt.Sprint(content))
	}
}

// envURLOrDefault 环境变量 URL，缺省回落。
// Python: env_url(name, default)。
func envURLOrDefault(name, defaultValue string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return defaultValue
}

// strValOrEmpty 宽松转字符串（nil/<nil> → ""）。
func strValOrEmpty(v any) string {
	if v == nil {
		return ""
	}
	s := fmt.Sprint(v)
	if s == "<nil>" {
		return ""
	}
	return s
}

// clampFloat 浮点钳制。
func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// toFloatDefault Python float(value) 等价：失败回退默认值。
func toFloatDefault(value any, fallback float64) float64 {
	switch v := value.(type) {
	case nil:
		return fallback
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case string:
		var f float64
		if _, err := fmt.Sscanf(strings.TrimSpace(v), "%g", &f); err == nil {
			return f
		}
		return fallback
	default:
		return fallback
	}
}
