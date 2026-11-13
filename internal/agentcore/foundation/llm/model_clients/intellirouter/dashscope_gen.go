package intellirouter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// DashScope 多模态生成三件套。
// Python: intelli_router_model_client.py:1095-1415
// （_generate_image_dashscope / _generate_speech_dashscope / _generate_video_dashscope）。
// Python 经 dashscope SDK 调用；Go 无 SDK，按 DashScope REST 协议直调
// （平台差异：端点/参数集合逐字段对齐，登记于 spec §5）。

// dashscopeCallResult DashScope REST 响应的通用骨架。
type dashscopeCallResult struct {
	// StatusCode HTTP 状态码（Python: response.status_code）
	StatusCode int `json:"-"`
	// Code 业务错误码（Python: response.code）
	Code any `json:"code"`
	// Message 错误消息（Python: response.message）
	Message string `json:"message"`
	// Output 输出（Python: response.output）
	Output map[string]any `json:"output"`
	// Usage 用量（Python: response.usage）
	Usage map[string]any `json:"usage"`
}

// dashscopePost 统一 POST 调用（api_base 直接作为 HTTP API 地址）。
func dashscopePost(ctx context.Context, apiBase, apiKey string, params map[string]any) (*dashscopeCallResult, error) {
	body, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBase, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	client := &http.Client{Timeout: 300 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := &dashscopeCallResult{StatusCode: resp.StatusCode}
	if err := json.Unmarshal(data, result); err != nil {
		return nil, fmt.Errorf("DashScope response parse failed: %w", err)
	}
	return result, nil
}

// userMessageText 提取单条 UserMessage 的文本
// （Python: msg.content if isinstance(msg.content, str) else ""）。
func userMessageText(msg *llmschema.UserMessage) string {
	if msg == nil {
		return ""
	}
	if msg.Content.IsText() {
		return msg.Content.Text()
	}
	return ""
}

// requireSingleMessage 单消息校验（Python: 三个生成函数共用形态）。
func requireSingleMessage(messages []*llmschema.UserMessage, kind string) error {
	if len(messages) != 1 {
		return exception.BuildError(exception.StatusModelInvokeParamError,
			exception.WithMsg(fmt.Sprintf("%s generation requires exactly one message, but got %d.", kind, len(messages))))
	}
	return nil
}

// ──────────────────────────── 导出函数 ────────────────────────────

// generateImageDashscope DashScope 图片生成。
// Python: intelli_router_model_client.py:1095-1206 _generate_image_dashscope
func (c *IntelliRouterModelClient) generateImageDashscope(ctx context.Context, messages []*llmschema.UserMessage, gopts *model_clients.GenerateImageParams) (*llmschema.ImageGenerationResponse, error) {
	if err := requireSingleMessage(messages, "Image"); err != nil {
		return nil, err
	}
	apiBase := c.apiBaseForProvider("dashscope")
	apiKey := c.apiKeyForProvider("dashscope")

	// Python: :1136-1138 model 缺省 qwen-image-max
	model := gopts.Model
	if model == "" {
		model = "qwen-image-max"
	}

	// Python: :1141-1160 content_list 组装（text/image 两键）
	msg := messages[0]
	var contentList []map[string]any
	if msg.Content.IsText() {
		contentList = append(contentList, map[string]any{"text": msg.Content.Text()})
	} else {
		for _, part := range msg.Content.Parts() {
			switch {
			case part.Text != "":
				contentList = append(contentList, map[string]any{"text": part.Text})
			case part.ImageURL != nil && part.ImageURL.URL != "":
				contentList = append(contentList, map[string]any{"image": part.ImageURL.URL})
			}
		}
	}
	if len(contentList) == 0 {
		return nil, exception.BuildError(exception.StatusModelInvokeParamError,
			exception.WithMsg("Image generation requires non-empty content."))
	}
	dashscopeMessages := []map[string]any{{"role": "user", "content": contentList}}

	// Python: :1162-1178 api_params
	params := map[string]any{
		"model":         model,
		"messages":      dashscopeMessages,
		"result_format": "message",
		"stream":        false,
		"watermark":     gopts.Watermark,
		"prompt_extend": gopts.PromptExtend,
		"size":          gopts.Size,
		"n":             gopts.N,
	}
	if gopts.NegativePrompt != "" {
		params["negative_prompt"] = gopts.NegativePrompt
	}
	if gopts.Seed != 0 {
		params["seed"] = gopts.Seed
	}

	logInfo(fmt.Sprintf("Calling DashScope image generation API with model: %s, size: %s", model, gopts.Size))

	result, err := dashscopePost(ctx, apiBase, apiKey, params)
	if err != nil {
		logError("DashScope image generation call failed: " + err.Error())
		return nil, exception.BuildError(exception.StatusModelCallFailed,
			exception.WithMsg("DashScope image generation failed: "+err.Error()), exception.WithCause(err))
	}
	if result.StatusCode != 200 {
		// Python: :1182-1188
		errorMsg := fmt.Sprintf("DashScope image generation failed: %s (code=%v, status=%d)",
			result.Message, result.Code, result.StatusCode)
		logError(errorMsg)
		return nil, exception.BuildError(exception.StatusModelCallFailed, exception.WithMsg(errorMsg))
	}

	// Python: :1191-1201 提取图片 URL
	var imageUrls []string
	if choices, ok := result.Output["choices"].([]any); ok {
		for _, choice := range choices {
			choiceMap, ok := choice.(map[string]any)
			if !ok {
				continue
			}
			message, _ := choiceMap["message"].(map[string]any)
			contentItems, _ := message["content"].([]any)
			for _, item := range contentItems {
				itemMap, ok := item.(map[string]any)
				if !ok {
					continue
				}
				if imageURL, ok := itemMap["image"].(string); ok {
					imageUrls = append(imageUrls, imageURL)
				}
			}
		}
	}

	logInfo(fmt.Sprintf("DashScope image generation succeeded. url_present=%v image_count=%d",
		len(imageUrls) > 0, len(imageUrls)))
	return &llmschema.ImageGenerationResponse{
		GenerationResponse: llmschema.GenerationResponse{Model: model},
		Images:             imageUrls,
	}, nil
}

// generateSpeechDashscope DashScope 语音生成。
// Python: intelli_router_model_client.py:1208-1303 _generate_speech_dashscope
func (c *IntelliRouterModelClient) generateSpeechDashscope(ctx context.Context, messages []*llmschema.UserMessage, gopts *model_clients.GenerateSpeechParams) (*llmschema.AudioGenerationResponse, error) {
	if err := requireSingleMessage(messages, "Speech"); err != nil {
		return nil, err
	}
	apiBase := c.apiBaseForProvider("dashscope")
	apiKey := c.apiKeyForProvider("dashscope")

	// Python: :1236-1238 model 缺省 cosyvoice-v1
	model := gopts.Model
	if model == "" {
		model = "cosyvoice-v1"
	}

	// Python: :1241-1249 提取文本
	text := userMessageText(messages[0])
	if strings.TrimSpace(text) == "" {
		return nil, exception.BuildError(exception.StatusModelInvokeParamError,
			exception.WithMsg("Speech generation requires non-empty text content."))
	}

	params := map[string]any{
		"model":         model,
		"text":          text,
		"voice":         gopts.Voice,
		"language_type": gopts.LanguageType,
	}

	logInfo(fmt.Sprintf("Calling DashScope speech generation API with model: %s, voice: %s", model, gopts.Voice))

	result, err := dashscopePost(ctx, apiBase, apiKey, params)
	if err != nil {
		logError("DashScope speech generation call failed: " + err.Error())
		return nil, exception.BuildError(exception.StatusModelCallFailed,
			exception.WithMsg("DashScope speech generation failed: "+err.Error()), exception.WithCause(err))
	}
	if result.StatusCode != 200 {
		errorMsg := fmt.Sprintf("DashScope speech generation failed: %s (code=%v, status=%d)",
			result.Message, result.Code, result.StatusCode)
		logError(errorMsg)
		return nil, exception.BuildError(exception.StatusModelCallFailed, exception.WithMsg(errorMsg))
	}

	// Python: :1276-1295 提取音频信息
	var audioURL, audioFormat string
	var audioData []byte
	if audioInfo, ok := result.Output["audio"].(map[string]any); ok {
		if u, ok := audioInfo["url"].(string); ok {
			audioURL = u
		}
		if d, ok := audioInfo["data"].(string); ok && d != "" {
			audioData = []byte(d) // Python: data_str.encode("utf-8")
		}
		switch {
		case strings.HasSuffix(audioURL, ".wav"):
			audioFormat = "wav"
		case strings.HasSuffix(audioURL, ".mp3"):
			audioFormat = "mp3"
		case strings.HasSuffix(audioURL, ".pcm"):
			audioFormat = "pcm"
		}
	}
	if audioURL == "" && len(audioData) == 0 {
		return nil, exception.BuildError(exception.StatusModelCallFailed,
			exception.WithMsg("No audio returned from DashScope API."))
	}

	logInfo(fmt.Sprintf("DashScope speech generation succeeded. Format: %s", orDefaultStr(audioFormat, "unknown")))
	out := &llmschema.AudioGenerationResponse{
		GenerationResponse: llmschema.GenerationResponse{Model: model},
		AudioData:          audioData,
		Format:             audioFormat,
	}
	if audioURL != "" {
		out.AudioURL = &audioURL
	}
	return out, nil
}

// generateVideoDashscope DashScope 视频生成。
// Python: intelli_router_model_client.py:1305-1415 _generate_video_dashscope
func (c *IntelliRouterModelClient) generateVideoDashscope(ctx context.Context, messages []*llmschema.UserMessage, gopts *model_clients.GenerateVideoParams) (*llmschema.VideoGenerationResponse, error) {
	if err := requireSingleMessage(messages, "Video"); err != nil {
		return nil, err
	}
	apiBase := c.apiBaseForProvider("dashscope")
	apiKey := c.apiKeyForProvider("dashscope")

	// Python: :1343-1345 model 缺省 wan2.6-t2v
	model := gopts.Model
	if model == "" {
		model = "wan2.6-t2v"
	}

	// Python: :1348-1356 提取 prompt
	prompt := userMessageText(messages[0])
	if strings.TrimSpace(prompt) == "" {
		return nil, exception.BuildError(exception.StatusModelInvokeParamError,
			exception.WithMsg("Video generation requires non-empty text content."))
	}

	params := map[string]any{
		"model":         model,
		"prompt":        prompt,
		"prompt_extend": gopts.PromptExtend,
		"watermark":     gopts.Watermark,
		"duration":      gopts.Duration,
	}
	if gopts.NegativePrompt != "" {
		params["negative_prompt"] = gopts.NegativePrompt
	}
	if gopts.Seed != nil {
		params["seed"] = *gopts.Seed
	}
	if gopts.AudioURL != "" {
		params["audio_url"] = gopts.AudioURL
	}
	if gopts.ImgURL != "" {
		params["img_url"] = gopts.ImgURL
		if gopts.Resolution != "" {
			params["resolution"] = gopts.Resolution
		}
	} else if gopts.Size != "" {
		params["size"] = gopts.Size
	}

	logInfo(fmt.Sprintf("Calling DashScope video generation API with model: %s, duration: %d", model, gopts.Duration))

	result, err := dashscopePost(ctx, apiBase, apiKey, params)
	if err != nil {
		logError("DashScope video generation call failed: " + err.Error())
		return nil, exception.BuildError(exception.StatusModelCallFailed,
			exception.WithMsg("DashScope video generation failed: "+err.Error()), exception.WithCause(err))
	}
	if result.StatusCode != 200 {
		errorMsg := fmt.Sprintf("DashScope video generation failed: %s (code=%v, status=%d)",
			result.Message, result.Code, result.StatusCode)
		logError(errorMsg)
		return nil, exception.BuildError(exception.StatusModelCallFailed, exception.WithMsg(errorMsg))
	}

	// Python: :1396-1407 提取视频信息
	var videoURL string
	var videoDuration *float64
	videoResolution := ""
	if u, ok := result.Output["video_url"].(string); ok {
		videoURL = u
	}
	if d, ok := result.Usage["duration"].(float64); ok && d > 0 {
		videoDuration = &d
	} else if d, ok := result.Usage["output_video_duration"].(float64); ok && d > 0 {
		videoDuration = &d
	}
	if s, ok := result.Usage["size"].(string); ok {
		videoResolution = s
	}
	if videoURL == "" {
		return nil, exception.BuildError(exception.StatusModelCallFailed,
			exception.WithMsg("No video URL returned from DashScope API."))
	}

	videoURLExcerpt := videoURL
	if len(videoURL) > 100 {
		videoURLExcerpt = videoURL[:100]
	}
	logInfo(fmt.Sprintf("DashScope video generation succeeded. Video URL: %s...", videoURLExcerpt))
	out := &llmschema.VideoGenerationResponse{
		GenerationResponse: llmschema.GenerationResponse{Model: model},
		Duration:           videoDuration,
		Resolution:         videoResolution,
		Format:             "mp4",
	}
	out.VideoURL = &videoURL
	return out, nil
}

// orDefaultStr 空串回退。
func orDefaultStr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
