package dashscope

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients/openai"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DashScopeModelClient 阿里云百炼 DashScope 模型客户端。
//
// 嵌入 OpenAIModelClient 复用 Invoke/Stream（通义千问兼容 OpenAI Chat Completion 协议），
// 仅覆写 GenerateImage/GenerateSpeech/GenerateVideo 三个多模态方法。
//
// 不依赖 dashscope-go SDK，自行实现 DashScope 原生 API 的 HTTP 调用
// （与 OpenAI 客户端不依赖 openai-go SDK 的设计一致）。
//
// 对应 Python: openjiuwen/core/foundation/llm/model_clients/dashscope_model_client.py (DashScopeModelClient)
type DashScopeModelClient struct {
	openai.OpenAIModelClient
}

// ──────────────────────────── 常量 ────────────────────────────

// logComponent dashscope 包日志组件标识（AgentCore 层）。
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// NewDashScopeModelClient 创建 DashScope 客户端。
//
// 构造流程与 OpenAI 客户端一致，仅覆盖 clientName 为 "DashScope client"。
// 使用 NewOpenAIModelClient 构造嵌入实例，确保 baseHeaders 等私有字段正确初始化。
//
// 对应 Python: DashScopeModelClient.__init__(model_config, model_client_config)
func NewDashScopeModelClient(
	modelConfig *llmschema.ModelRequestConfig,
	clientConfig *llmschema.ModelClientConfig,
) (*DashScopeModelClient, error) {
	openaiClient, err := openai.NewOpenAIModelClient(modelConfig, clientConfig)
	if err != nil {
		return nil, err
	}

	// 覆盖 clientName 为 DashScope（OpenAI 构造函数设置的是 "OpenAI client"）
	embed, err := model_clients.NewBaseClientEmbed(
		modelConfig,
		clientConfig,
		model_clients.WithClientName("DashScope client"),
	)
	if err != nil {
		return nil, err
	}
	openaiClient.BaseClientEmbed = *embed

	return &DashScopeModelClient{
		OpenAIModelClient: *openaiClient,
	}, nil
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GenerateImage 实现 DashScope 万相文生图。
//
// 调用 DashScope 多模态 API（MultiModalConversation），支持文生图和图生图。
//
// 参数验证规则（与 Python 端一致）：
//   - 恰好 1 条 UserMessage
//   - content 中 text_count ≥ 1
//   - content 中 image_count ≤ 3
//
// 对应 Python: DashScopeModelClient.generate_image()
func (c *DashScopeModelClient) GenerateImage(
	ctx context.Context,
	messages []*llmschema.UserMessage,
	opts ...model_clients.GenerateImageOption,
) (*llmschema.ImageGenerationResponse, error) {
	params := model_clients.NewGenerateImageParams(opts...)

	// 1. 参数验证
	contentList, err := validateImageMessages(messages)
	if err != nil {
		logger.Error(logComponent).
			Str("model_provider", "DashScope").
			Str("method", "generate_image").
			Err(err).
			Msg("DashScope 图片生成参数验证失败")
		return nil, err
	}

	// 2. 获取模型名称
	model := params.Model
	if model == "" && c.ModelConfig != nil {
		model = c.ModelConfig.ModelName
	}

	// 3. 构建 DashScope 原生请求体
	reqBody := map[string]any{
		"model": model,
		"input": map[string]any{
			"messages": []map[string]any{
				{
					"role":    "user",
					"content": contentList,
				},
			},
		},
		"parameters": buildImageParameters(params),
	}

	// 4. 调用 DashScope 多模态 API（对齐 Python: 多模态方法使用 logger，非回调）
	logger.Info(logComponent).
		Str("model_name", model).
		Str("model_provider", "DashScope").
		Str("method", "generate_image").
		Str("size", params.Size).
		Int("n", params.N).
		Int("seed", params.Seed).
		Msg("DashScope generate image started.")

	resp, err := CallDashScopeAPI(
		ctx, c.ClientConfig.APIBase, c.ClientConfig.APIKey,
		multiModalPath, reqBody,
		params.Timeout, c.ClientConfig.VerifySSL, c.ClientConfig.SSLCert,
	)
	if err != nil {
		return nil, err
	}

	// 5. 解析响应 → 提取图片 URL
	imageURLs, err := extractImageURLs(resp)
	if err != nil {
		logger.Error(logComponent).
			Str("model_name", model).
			Str("model_provider", "DashScope").
			Str("method", "generate_image").
			Err(err).
			Msg("DashScope generate image failed.")
		return nil, err
	}

	logger.Info(logComponent).
		Str("model_name", model).
		Str("model_provider", "DashScope").
		Int("image_count", len(imageURLs)).
		Msg("DashScope generate image completed.")

	return llmschema.NewImageGenerationResponse(
		llmschema.WithImageModel(model),
		llmschema.WithImages(imageURLs),
	), nil
}

// GenerateSpeech 实现 DashScope Cosyvoice 语音合成。
//
// 调用 DashScope 多模态 API（MultiModalConversation），支持文字转语音。
//
// 参数验证规则（与 Python 端一致）：
//   - 恰好 1 条 UserMessage
//   - content 非空
//
// 对应 Python: DashScopeModelClient.generate_speech()
func (c *DashScopeModelClient) GenerateSpeech(
	ctx context.Context,
	messages []*llmschema.UserMessage,
	opts ...model_clients.GenerateSpeechOption,
) (*llmschema.AudioGenerationResponse, error) {
	params := model_clients.NewGenerateSpeechParams(opts...)

	// 1. 参数验证
	text, err := validateSpeechMessages(messages)
	if err != nil {
		logger.Error(logComponent).
			Str("model_provider", "DashScope").
			Str("method", "generate_speech").
			Err(err).
			Msg("DashScope 语音生成参数验证失败")
		return nil, err
	}

	// 2. 获取模型名称
	model := params.Model
	if model == "" && c.ModelConfig != nil {
		model = c.ModelConfig.ModelName
	}

	// 3. 构建 DashScope 原生请求体
	reqBody := map[string]any{
		"model": model,
		"input": map[string]any{
			"messages": []map[string]any{
				{
					"role":    "user",
					"content": []map[string]any{{"text": text}},
				},
			},
		},
		"parameters": map[string]any{
			"voice":         params.Voice,
			"language_type": params.LanguageType,
		},
	}

	// 合并额外参数
	if len(params.Extra) > 0 {
		parameters := reqBody["parameters"].(map[string]any)
		for k, v := range params.Extra {
			parameters[k] = v
		}
	}

	// 4. 调用 DashScope 多模态 API（对齐 Python: 多模态方法使用 logger，非回调）
	logger.Info(logComponent).
		Str("model_name", model).
		Str("model_provider", "DashScope").
		Str("method", "generate_speech").
		Str("voice", params.Voice).
		Str("language_type", params.LanguageType).
		Msg("DashScope generate speech started.")

	resp, err := CallDashScopeAPI(
		ctx, c.ClientConfig.APIBase, c.ClientConfig.APIKey,
		multiModalPath, reqBody,
		params.Timeout, c.ClientConfig.VerifySSL, c.ClientConfig.SSLCert,
	)
	if err != nil {
		return nil, err
	}

	// 5. 解析响应 → 提取音频信息
	audioURL, audioData, audioFormat, err := extractAudioInfo(resp)
	if err != nil {
		logger.Error(logComponent).
			Str("model_name", model).
			Str("model_provider", "DashScope").
			Str("method", "generate_speech").
			Err(err).
			Msg("DashScope generate speech failed.")
		return nil, err
	}

	logger.Info(logComponent).
		Str("model_name", model).
		Str("model_provider", "DashScope").
		Str("method", "generate_speech").
		Str("format", audioFormat).
		Bool("url_present", audioURL != "").
		Bool("data_present", len(audioData) > 0).
		Msg("DashScope generate speech completed.")

	audioResp := llmschema.NewAudioGenerationResponse(
		llmschema.WithAudioModel(model),
		llmschema.WithAudioFormat(audioFormat),
	)
	if audioURL != "" {
		audioResp.AudioURL = &audioURL
	}
	if len(audioData) > 0 {
		audioResp.AudioData = audioData
	}

	return audioResp, nil
}

// GenerateVideo 实现 DashScope 视频生成。
//
// 调用 DashScope 视频生成 API（VideoSynthesis），支持文生视频和图生视频。
//
// 参数验证规则（与 Python 端一致）：
//   - 恰好 1 条 UserMessage
//   - content 非空
//   - ImgURL 存在时为图生视频 (i2v)，否则为文生视频 (t2v)
//
// 对应 Python: DashScopeModelClient.generate_video()
func (c *DashScopeModelClient) GenerateVideo(
	ctx context.Context,
	messages []*llmschema.UserMessage,
	opts ...model_clients.GenerateVideoOption,
) (*llmschema.VideoGenerationResponse, error) {
	params := model_clients.NewGenerateVideoParams(opts...)

	// 1. 参数验证
	prompt, err := validateVideoMessages(messages)
	if err != nil {
		logger.Error(logComponent).
			Str("model_provider", "DashScope").
			Str("method", "generate_video").
			Err(err).
			Msg("DashScope 视频生成参数验证失败")
		return nil, err
	}

	// 2. 获取模型名称
	model := params.Model
	if model == "" && c.ModelConfig != nil {
		model = c.ModelConfig.ModelName
	}

	// 3. 构建请求 input 部分
	input := map[string]any{
		"prompt": prompt,
	}
	if params.ImgURL != "" {
		input["img_url"] = params.ImgURL
	}
	if params.AudioURL != "" {
		input["audio_url"] = params.AudioURL
	}

	// 4. 构建请求 parameters 部分
	parameters := map[string]any{
		"prompt_extend": params.PromptExtend,
		"watermark":     params.Watermark,
	}
	if params.Duration > 0 {
		parameters["duration"] = params.Duration
	}
	if params.NegativePrompt != "" {
		parameters["negative_prompt"] = params.NegativePrompt
	}
	if params.Seed != nil {
		parameters["seed"] = *params.Seed
	}

	// 根据是否为图生视频选择 size/resolution 参数
	isI2V := params.ImgURL != ""
	if isI2V {
		// 图生视频 (i2v)：优先使用 resolution
		if params.Resolution != "" {
			parameters["resolution"] = params.Resolution
		} else if params.Size != "" {
			parameters["size"] = params.Size
		}
	} else {
		// 文生视频 (t2v)：优先使用 size
		if params.Size != "" {
			parameters["size"] = params.Size
		} else if params.Resolution != "" {
			parameters["resolution"] = params.Resolution
		}
	}

	// 合并额外参数
	if len(params.Extra) > 0 {
		for k, v := range params.Extra {
			parameters[k] = v
		}
	}

	// 5. 构建 DashScope 原生请求体
	reqBody := map[string]any{
		"model":      model,
		"input":      input,
		"parameters": parameters,
	}

	// 6. 调用 DashScope 视频生成 API（对齐 Python: 多模态方法使用 logger，非回调）
	if isI2V {
		resolutionOrSize := params.Resolution
		if resolutionOrSize == "" {
			resolutionOrSize = params.Size
		}
		logger.Info(logComponent).
			Str("model_name", model).
			Str("model_provider", "DashScope").
			Str("method", "generate_video_i2v").
			Str("resolution", resolutionOrSize).
			Int("duration", params.Duration).
			Msg("DashScope generate video (i2v) started.")
	} else {
		sizeOrResolution := params.Size
		if sizeOrResolution == "" {
			sizeOrResolution = params.Resolution
		}
		logger.Info(logComponent).
			Str("model_name", model).
			Str("model_provider", "DashScope").
			Str("method", "generate_video_t2v").
			Str("size", sizeOrResolution).
			Int("duration", params.Duration).
			Msg("DashScope generate video (t2v) started.")
	}

	resp, err := CallDashScopeAPI(
		ctx, c.ClientConfig.APIBase, c.ClientConfig.APIKey,
		videoSynthesisPath, reqBody,
		params.Timeout, c.ClientConfig.VerifySSL, c.ClientConfig.SSLCert,
	)
	if err != nil {
		return nil, err
	}

	// 7. 解析响应 → 提取视频信息
	videoURL, videoDuration, videoResolution, err := extractVideoInfo(resp)
	if err != nil {
		logger.Error(logComponent).
			Str("model_name", model).
			Str("model_provider", "DashScope").
			Str("method", "generate_video").
			Err(err).
			Msg("DashScope generate video failed.")
		return nil, err
	}

	// 对齐 Python: 成功日志记录 video_url 前 100 字符
	videoURLSummary := videoURL
	if len(videoURLSummary) > 100 {
		videoURLSummary = videoURLSummary[:100] + "..."
	}
	logger.Info(logComponent).
		Str("model_name", model).
		Str("model_provider", "DashScope").
		Str("method", "generate_video").
		Str("video_url", videoURLSummary).
		Msg("DashScope generate video completed.")

	videoResp := llmschema.NewVideoGenerationResponse(
		llmschema.WithVideoModel(model),
		llmschema.WithVideoFormat("mp4"),
		llmschema.WithVideoURL(videoURL),
	)
	if videoDuration > 0 {
		videoResp.Duration = &videoDuration
	}
	if videoResolution != "" {
		videoResp.Resolution = videoResolution
	}

	return videoResp, nil
}

// Release 释放模型缓存（当前不支持）。
//
// DashScope API 不支持 KV Cache 释放，仅 InferenceAffinity (vLLM) 客户端支持。
func (c *DashScopeModelClient) Release(
	_ context.Context,
	_ ...model_clients.ReleaseOption,
) (bool, error) {
	return false, exception.NewBaseError(
		exception.StatusModelCallFailed,
		exception.WithMsg("DashScope client does not support KV cache release"),
	)
}

// SupportsKVCacheRelease DashScope 客户端不支持 KV Cache 释放。
func (c *DashScopeModelClient) SupportsKVCacheRelease() bool {
	return false
}

// ──────────────────────────── 非导出函数 ────────────────────────────
func init() {
	// 注册 DashScope 客户端到全局注册表（2.6 回填点）
	registry := model_clients.GetClientRegistry()

	dashScopeFactory := func(mc *llmschema.ModelRequestConfig, cc *llmschema.ModelClientConfig) model_clients.BaseModelClient {
		client, err := NewDashScopeModelClient(mc, cc)
		if err != nil {
			logger.Error(logComponent).Err(err).Msg("创建 DashScope 客户端失败")
			return nil
		}
		return client
	}

	registry.Register("DashScope", "llm", dashScopeFactory)
}

// validateImageMessages 验证图片生成的消息参数。
//
// 验证规则（与 Python 端一致）：
//   - 恰好 1 条 UserMessage
//   - content 中 text_count ≥ 1
//   - content 中 image_count ≤ 3
//
// 返回 DashScope 格式的 content 列表。
func validateImageMessages(messages []*llmschema.UserMessage) ([]map[string]any, error) {
	if len(messages) != 1 {
		return nil, exception.NewBaseError(
			exception.StatusModelInvokeParamError,
			exception.WithMsg(fmt.Sprintf("图片生成需要恰好 1 条消息，但收到 %d 条。", len(messages))),
		)
	}

	msg := messages[0]
	contentList := make([]map[string]any, 0)
	textCount := 0
	imageCount := 0

	if msg.Content.IsText() {
		// 纯文本
		contentList = append(contentList, map[string]any{"text": msg.Content.Text()})
		textCount = 1
	} else {
		// 多模态内容
		for _, part := range msg.Content.Parts() {
			switch part.Type {
			case "text":
				contentList = append(contentList, map[string]any{"text": part.Text})
				textCount++
			case "image_url":
				if part.ImageURL != nil && part.ImageURL.URL != "" {
					contentList = append(contentList, map[string]any{"image": part.ImageURL.URL})
					imageCount++
				}
			default:
				// 尝试按 dict 格式处理
				if part.Text != "" {
					contentList = append(contentList, map[string]any{"text": part.Text})
					textCount++
				}
			}
		}
	}

	if textCount == 0 {
		return nil, exception.NewBaseError(
			exception.StatusModelInvokeParamError,
			exception.WithMsg("图片生成需要至少 1 个文本提示词。"),
		)
	}

	if imageCount > 3 {
		return nil, exception.NewBaseError(
			exception.StatusModelInvokeParamError,
			exception.WithMsg(fmt.Sprintf("图片生成最多支持 3 张输入图片，但收到 %d 张。", imageCount)),
		)
	}

	return contentList, nil
}

// validateSpeechMessages 验证语音生成的消息参数。
//
// 验证规则：恰好 1 条 UserMessage，content 非空。
func validateSpeechMessages(messages []*llmschema.UserMessage) (string, error) {
	if len(messages) != 1 {
		return "", exception.NewBaseError(
			exception.StatusModelInvokeParamError,
			exception.WithMsg(fmt.Sprintf("语音生成需要恰好 1 条消息，但收到 %d 条。", len(messages))),
		)
	}

	text := messages[0].Content.Text()
	if !messages[0].Content.IsText() {
		// 多模态内容，提取文本部分
		var texts []string
		for _, part := range messages[0].Content.Parts() {
			if part.Text != "" {
				texts = append(texts, part.Text)
			}
		}
		text = strings.Join(texts, " ")
	}

	if text == "" || strings.TrimSpace(text) == "" {
		return "", exception.NewBaseError(
			exception.StatusModelInvokeParamError,
			exception.WithMsg("语音生成需要非空的文本内容。"),
		)
	}

	return text, nil
}

// validateVideoMessages 验证视频生成的消息参数。
//
// 验证规则：恰好 1 条 UserMessage，content 非空。
func validateVideoMessages(messages []*llmschema.UserMessage) (string, error) {
	if len(messages) != 1 {
		return "", exception.NewBaseError(
			exception.StatusModelInvokeParamError,
			exception.WithMsg(fmt.Sprintf("视频生成需要恰好 1 条消息，但收到 %d 条。", len(messages))),
		)
	}

	text := messages[0].Content.Text()
	if !messages[0].Content.IsText() {
		var texts []string
		for _, part := range messages[0].Content.Parts() {
			if part.Text != "" {
				texts = append(texts, part.Text)
			}
		}
		text = strings.Join(texts, " ")
	}

	if text == "" || strings.TrimSpace(text) == "" {
		return "", exception.NewBaseError(
			exception.StatusModelInvokeParamError,
			exception.WithMsg("视频生成需要非空的文本内容。"),
		)
	}

	return text, nil
}

// buildImageParameters 构建图片生成 API 的 parameters 部分。
func buildImageParameters(params *model_clients.GenerateImageParams) map[string]any {
	parameters := map[string]any{
		"result_format": "message",
		"size":          params.Size,
		"n":             params.N,
		"prompt_extend": params.PromptExtend,
		"watermark":     params.Watermark,
	}

	if params.Seed > 0 {
		parameters["seed"] = params.Seed
	}
	if params.NegativePrompt != "" {
		parameters["negative_prompt"] = params.NegativePrompt
	}

	// 合并额外参数
	if len(params.Extra) > 0 {
		for k, v := range params.Extra {
			parameters[k] = v
		}
	}

	return parameters
}

// extractImageURLs 从 DashScope 多模态 API 响应中提取图片 URL。
func extractImageURLs(resp *DashScopeResponse) ([]string, error) {
	var output MultiModalOutput
	if err := json.Unmarshal(resp.Output, &output); err != nil {
		return nil, exception.NewBaseError(
			exception.StatusModelCallFailed,
			exception.WithMsg(fmt.Sprintf("DashScope 图片生成响应解析失败: %s", err)),
		)
	}

	var imageURLs []string
	for _, choice := range output.Choices {
		if choice.Message == nil {
			continue
		}
		for _, item := range choice.Message.Content {
			if item.Image != "" {
				imageURLs = append(imageURLs, item.Image)
			}
		}
	}

	if len(imageURLs) == 0 {
		return nil, exception.NewBaseError(
			exception.StatusModelCallFailed,
			exception.WithMsg("DashScope 图片生成 API 未返回图片。"),
		)
	}

	return imageURLs, nil
}

// extractAudioInfo 从 DashScope 语音生成 API 响应中提取音频信息。
func extractAudioInfo(resp *DashScopeResponse) (audioURL string, audioData []byte, audioFormat string, err error) {
	var output AudioOutput
	if err := json.Unmarshal(resp.Output, &output); err != nil {
		return "", nil, "", exception.NewBaseError(
			exception.StatusModelCallFailed,
			exception.WithMsg(fmt.Sprintf("DashScope 语音生成响应解析失败: %s", err)),
		)
	}

	audioURL = output.URL
	if output.Data != "" {
		audioData = []byte(output.Data)
	}

	// 从 URL 推断音频格式
	if audioURL != "" {
		switch {
		case strings.HasSuffix(audioURL, ".wav"):
			audioFormat = "wav"
		case strings.HasSuffix(audioURL, ".mp3"):
			audioFormat = "mp3"
		case strings.HasSuffix(audioURL, ".pcm"):
			audioFormat = "pcm"
		default:
			audioFormat = "mp3"
		}
	} else {
		audioFormat = "mp3"
	}

	if audioURL == "" && len(audioData) == 0 {
		return "", nil, "", exception.NewBaseError(
			exception.StatusModelCallFailed,
			exception.WithMsg("DashScope 语音生成 API 未返回音频数据。"),
		)
	}

	return audioURL, audioData, audioFormat, nil
}

// extractVideoInfo 从 DashScope 视频生成 API 响应中提取视频信息。
func extractVideoInfo(resp *DashScopeResponse) (videoURL string, videoDuration float64, videoResolution string, err error) {
	var output VideoOutput
	if err := json.Unmarshal(resp.Output, &output); err != nil {
		return "", 0, "", exception.NewBaseError(
			exception.StatusModelCallFailed,
			exception.WithMsg(fmt.Sprintf("DashScope 视频生成响应解析失败: %s", err)),
		)
	}

	if output.VideoURL == "" {
		return "", 0, "", exception.NewBaseError(
			exception.StatusModelCallFailed,
			exception.WithMsg("DashScope 视频生成 API 未返回视频 URL。"),
		)
	}

	// 尝试从 usage 中提取时长和分辨率
	if len(resp.Usage) > 0 {
		var usage VideoUsage
		if json.Unmarshal(resp.Usage, &usage) == nil {
			// 提取时长
			switch v := usage.Duration.(type) {
			case float64:
				videoDuration = v
			case int:
				videoDuration = float64(v)
			case string:
				_, _ = fmt.Sscanf(v, "%f", &videoDuration)
			}
			// 提取分辨率
			if usage.Size != "" {
				videoResolution = usage.Size
			}
		}
	}

	return output.VideoURL, videoDuration, videoResolution, nil
}
