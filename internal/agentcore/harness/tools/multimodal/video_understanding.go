package multimodal

import (
	"context"
	"fmt"

	modelclients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// VideoUnderstandingInput video_understanding 工具的输入参数
type VideoUnderstandingInput struct {
	// Query 用户关于视频内容的问题
	Query string `json:"query"`
	// VideoPath 本地视频路径或远程视频 URL
	VideoPath string `json:"video_path"`
	// Model 可选，指定模型名称
	Model string `json:"model,omitempty"`
	// MaxTokens 可选，最大输出 token 数（对齐 Python: max_tokens, 默认 2048）
	MaxTokens int `json:"max_tokens,omitempty"`
	// Temperature 可选，采样温度（对齐 Python: temperature, 默认 0.2）
	Temperature float64 `json:"temperature,omitempty"`
	// TimeoutSeconds 可选，请求超时时间（秒）（对齐 Python: timeout_seconds, 默认 120）
	TimeoutSeconds int `json:"timeout_seconds,omitempty"`
	// ThinkingEnabled 可选，是否启用思维模式（对齐 jiuwenswarm: thinking_enabled, 默认 false）
	ThinkingEnabled bool `json:"thinking_enabled,omitempty"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultVideoMaxTokens 默认最大输出 token 数（对齐 Python: default_max_tokens = 2048）
	defaultVideoMaxTokens = 2048
	// defaultVideoTemperature 默认采样温度（对齐 Python: default_temperature = 0.2）
	defaultVideoTemperature = 0.2
	// defaultVideoTimeoutSeconds 默认超时秒数（对齐 Python: default_timeout_seconds = 120）
	defaultVideoTimeoutSeconds = 120
	// minVideoMaxTokens 最小 token 数（对齐 Python: max(128, ...)）
	minVideoMaxTokens = 128
	// maxVideoMaxTokens 最大 token 数（对齐 Python: min(..., 8192)）
	maxVideoMaxTokens = 8192
	// minVideoTemperature 最小温度（对齐 Python: max(0.0, ...)）
	minVideoTemperature = 0.0
	// maxVideoTemperature 最大温度（对齐 Python: min(..., 2.0)）
	maxVideoTemperature = 2.0
	// minVideoTimeout 最小超时（对齐 Python: max(10, ...)）
	minVideoTimeout = 10
	// maxVideoTimeout 最大超时（对齐 Python: min(..., 600)）
	maxVideoTimeout = 600
	// defaultVideoModel 默认视频理解模型（对齐 jiuwenswarm: glm-4.6v）
	defaultVideoModel = "glm-4.6v"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewVideoUnderstandingTool 创建视频理解工具。
//
// 对齐 Python: VideoUnderstandingTool.__init__ + VideoUnderstandingTool.invoke
// 使用 VideoModelConfig 配置（独立于 VisionModelConfig），通过 BaseModelClient.Invoke 调用 video_url 消息
// 支持 ThinkingEnabled 参数，通过 WithInvokeExtra 传递 thinking payload
func NewVideoUnderstandingTool(
	client modelclients.BaseModelClient,
	config *hschema.VideoModelConfig,
	language, agentID string,
) tool.Tool {
	card, _ := tools.BuildToolCard("video_understanding", "VideoUnderstandingTool", language, nil, agentID)

	fn := func(ctx context.Context, input VideoUnderstandingInput, opts ...tool.ToolOption) (map[string]any, error) {
		result, err := func() (map[string]any, error) {
			// 校验配置（Go 扩展设计决策：使用独立的 VideoModelConfig，Python 中 VideoUnderstandingTool 使用 VisionModelConfig）
			if config == nil || config.APIKey == "" {
				return nil, exception.NewBaseError(
					exception.StatusToolMultimodalVideoConfigInvalid,
					exception.WithMsg("video_model_config is not configured: api_key is required"),
				)
			}
			if config.BaseURL == "" {
				return nil, exception.NewBaseError(
					exception.StatusToolMultimodalVideoConfigInvalid,
					exception.WithMsg("video_model_config is not configured: base_url is required"),
				)
			}

			// 校验输入（对齐 Python: if not query / if not video_path）
			if input.Query == "" {
				return nil, exception.NewBaseError(
					exception.StatusToolMultimodalVideoConfigInvalid,
					exception.WithMsg("query cannot be empty"),
				)
			}
			if input.VideoPath == "" {
				return nil, exception.NewBaseError(
					exception.StatusToolMultimodalVideoConfigInvalid,
					exception.WithMsg("video_path cannot be empty"),
				)
			}

			// 参数裁剪（对齐 Python: max(128, min(max_tokens, 8192)) 等）
			modelName := input.Model
			if modelName == "" && config != nil {
				modelName = config.Model
			}
			if modelName == "" {
				modelName = defaultVideoModel // 对齐 jiuwenswarm: glm-4.6v
			}

			maxTokens := clampInt(input.MaxTokens, minVideoMaxTokens, maxVideoMaxTokens, defaultVideoMaxTokens)
			temperature := clampFloat(input.Temperature, minVideoTemperature, maxVideoTemperature, defaultVideoTemperature)
			timeoutSeconds := clampInt(input.TimeoutSeconds, minVideoTimeout, maxVideoTimeout, defaultVideoTimeoutSeconds)

			// 规范化视频 URL（对齐 Python: _normalize_video_url）
			videoURL, err := NormalizeVideoURL(input.VideoPath)
			if err != nil {
				return nil, err
			}

			// 构造 video_url + text 消息（对齐 Python: content = [video_url, text]）
			videoPart := llmschema.ContentPart{
				Type:     "video_url",
				VideoURL: &llmschema.VideoURL{URL: videoURL},
			}
			textPart := llmschema.ContentPart{Type: "text", Text: input.Query}
			userMsg := llmschema.NewUserMessage("", llmschema.WithMultiModalContent(videoPart, textPart))

			messages := modelclients.NewMessagesParam(userMsg)

			// 调用模型（对齐 Python: self.model.invoke(messages, model, max_tokens, temperature, timeout)）
			timeoutFloat := float64(timeoutSeconds)
			opts := []modelclients.InvokeOption{
				modelclients.WithInvokeModel(modelName),
				modelclients.WithInvokeMaxTokens(maxTokens),
				modelclients.WithInvokeTemperature(temperature),
				modelclients.WithInvokeTimeout(timeoutFloat),
			}

			// thinking_enabled → WithInvokeExtra（对齐 jiuwenswarm: payload["thinking"] = {"type": "enabled"}）
			if input.ThinkingEnabled || (config != nil && config.ThinkingEnabled) {
				opts = append(opts, modelclients.WithInvokeExtra(map[string]any{
					"thinking": map[string]any{"type": "enabled"},
				}))
			}

			resp, err := client.Invoke(ctx, messages, opts...)
			if err != nil {
				return nil, err
			}

			answer := extractResponseText(resp)
			if answer == "" {
				return nil, exception.NewBaseError(
					exception.StatusToolMultimodalVideoInvokeFailed,
					exception.WithMsg("model returned empty answer"),
				)
			}

			return map[string]any{
				"query":      input.Query,
				"video_path": input.VideoPath,
				"model":      modelName,
				"answer":     answer,
			}, nil
		}()
		if err != nil {
			logger.Error(logComponent).Err(err).
				Str("event_type", "TOOL_CALL_ERROR").Str("tool_name", "video_understanding").
				Msg("VideoUnderstandingTool 调用失败")
			return nil, exception.NewBaseError(
				exception.StatusToolMultimodalVideoInvokeFailed,
				exception.WithMsg(fmt.Sprintf("video understanding failed: %s", err.Error())),
			)
		}
		return result, nil
	}

	invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
	return invokeFn
}

// ──────────────────────────── 非导出函数 ────────────────────────────
