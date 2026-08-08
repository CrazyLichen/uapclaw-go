package multimodal

import (
	"context"
	"encoding/base64"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	modelclients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// sandboxPathMarker sandbox 路径标记（对齐 Python: SANDBOX_PATH_MARKER = "home/user"）
const sandboxPathMarker = "home/user"

// defaultOCRPrompt 默认 OCR 提示词（对齐 Python: DEFAULT_OCR_PROMPT）
const defaultOCRPrompt = `You are a meticulous OCR assistant.
Extract all visible text from the image.
Preserve structure, line breaks, numbers, symbols, and uncertain text when possible.
If no text is visible, reply with 'No text found'.`

// defaultVQAPromptTemplate 默认 VQA 提示词模板（对齐 Python: DEFAULT_VQA_PROMPT_TEMPLATE）
// 使用 Go 占位符语法，调用方需自行 format
const defaultVQAPromptTemplate = `You are a careful visual analysis assistant.
Use the image and the OCR result below to answer the user's question accurately.

OCR result:
%s

Question:
%s

Provide a concise but complete answer. If something is uncertain, say so explicitly.`

// maxImageFileSize 图片文件大小限制（20MB）
const maxImageFileSize = 20 * 1024 * 1024

// logComponent 日志组件标识
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildImageContent 构建图片内容块。
//
// 对齐 Python: _build_image_content(image_path_or_url)
// HTTP URL → image_url block；本地文件 → base64 → data:URI → image_url block；
// sandbox 路径 → 返回错误
func BuildImageContent(imagePathOrURL string) (llmschema.ContentPart, error) {
	// 1. 检查 sandbox 路径（对齐 Python: if SANDBOX_PATH_MARKER in image_path_or_url）
	if strings.Contains(imagePathOrURL, sandboxPathMarker) {
		return llmschema.ContentPart{}, exception.NewBaseError(
			exception.StatusToolMultimodalVisionConfigInvalid,
			exception.WithMsg(fmt.Sprintf(
				"vision tools cannot access sandbox-only paths. Use a local path outside the sandbox or an https URL. path=%s",
				imagePathOrURL)),
		)
	}

	// 2. HTTP URL → 直接返回 image_url block（对齐 Python: _is_http_url → return dict）
	if isHTTPURL(imagePathOrURL) {
		return llmschema.ContentPart{
			Type:     "image_url",
			ImageURL: &llmschema.ImageURL{URL: imagePathOrURL},
		}, nil
	}

	// 3. 本地文件 → base64 编码 → data:URI（对齐 Python: image_path.read_bytes → base64 → data URI）
	imagePath := imagePathOrURL
	// Go 不需要 expanduser，直接使用路径
	if _, err := os.Stat(imagePath); err != nil {
		return llmschema.ContentPart{}, exception.NewBaseError(
			exception.StatusToolMultimodalVisionInvokeFailed,
			exception.WithMsg(fmt.Sprintf(
				"image path does not exist or is not a file: %s", imagePath)),
		)
	}

	imageBytes, err := os.ReadFile(imagePath)
	if err != nil {
		return llmschema.ContentPart{}, exception.NewBaseError(
			exception.StatusToolMultimodalVisionInvokeFailed,
			exception.WithMsg(fmt.Sprintf("读取图片文件失败: %s", err.Error())),
		)
	}

	// 大小限制检查
	if len(imageBytes) > maxImageFileSize {
		return llmschema.ContentPart{}, exception.NewBaseError(
			exception.StatusToolMultimodalVisionInvokeFailed,
			exception.WithMsg(fmt.Sprintf(
				"图片文件超过大小限制: %d bytes > %d bytes", len(imageBytes), maxImageFileSize)),
		)
	}

	mimeType := guessImageMIMEType(imagePath, imageBytes)
	encoded := base64.StdEncoding.EncodeToString(imageBytes)
	dataURI := fmt.Sprintf("data:%s;base64,%s", mimeType, encoded)

	logger.Debug(logComponent).
		Str("image_path", imagePath).
		Str("mime_type", mimeType).
		Int("image_size", len(imageBytes)).
		Msg("本地图片已编码为 data URI")

	return llmschema.ContentPart{
		Type:     "image_url",
		ImageURL: &llmschema.ImageURL{URL: dataURI},
	}, nil
}

// CallVisionModel 调用视觉模型，带指数退避重试。
//
// 对齐 Python: _call_vision_model(config, image_content, prompt)
// 返回 (text, model, error)
func CallVisionModel(
	ctx context.Context,
	client modelclients.BaseModelClient,
	imageContent llmschema.ContentPart,
	prompt string,
	config *hschema.VisionModelConfig,
) (string, string, error) {
	// 1. 校验配置（对齐 Python: _require_vision_model_config）
	if config == nil || config.APIKey == "" || config.BaseURL == "" || config.Model == "" {
		return "", "", exception.NewBaseError(
			exception.StatusToolMultimodalVisionConfigInvalid,
			exception.WithMsg("vision model config invalid: api_key/base_url/model are required"),
		)
	}

	// 2. 构造消息（对齐 Python: messages = [{"role":"user","content":[text_part, image_part]}])
	textPart := llmschema.ContentPart{Type: "text", Text: prompt}
	msg := llmschema.NewUserMessage("", llmschema.WithMultiModalContent(textPart, imageContent))
	messages := modelclients.NewMessagesParam(msg)

	// 3. 重试循环（对齐 Python: _call_vision_model 的 retry 逻辑）
	maxRetries := config.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}
	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		resp, err := client.Invoke(ctx, messages,
			modelclients.WithInvokeModel(config.Model),
		)
		if err != nil {
			errStr := err.Error()
			isRetryable := isRetryableError(errStr)
			logger.Warn(logComponent).
				Str("event_type", "VISION_MODEL_RETRY").
				Str("model_name", config.Model).
				Int("attempt", attempt).
				Int("max_retries", maxRetries).
				Bool("is_retryable", isRetryable).
				Err(err).
				Msg("视觉模型调用失败，尝试重试")

			if isRetryable && attempt < maxRetries {
				wait := time.Duration(math.Pow(2, float64(attempt-1))) * time.Second
				time.Sleep(wait)
				lastErr = err
				continue
			}
			return "", "", exception.NewBaseError(
				exception.StatusToolMultimodalVisionInvokeFailed,
				exception.WithMsg(fmt.Sprintf("vision model invoke failed after %d attempts: %s", attempt, errStr)),
			)
		}

		text := extractResponseText(resp)
		if text == "" {
			return "", "", exception.NewBaseError(
				exception.StatusToolMultimodalVisionInvokeFailed,
				exception.WithMsg("vision model returned empty content"),
			)
		}

		logger.Info(logComponent).
			Str("event_type", "VISION_MODEL_SUCCESS").
			Str("model_name", config.Model).
			Int("text_length", len(text)).
			Msg("视觉模型调用成功")

		return text, config.Model, nil
	}

	return "", "", exception.NewBaseError(
		exception.StatusToolMultimodalVisionInvokeFailed,
		exception.WithMsg(fmt.Sprintf("vision model invoke failed after %d retries: %s", maxRetries, lastErr.Error())),
	)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isHTTPURL 判断是否为 HTTP URL（对齐 Python: _is_http_url）
func isHTTPURL(value string) bool {
	u, err := url.Parse(value)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// extractResponseText 从 AssistantMessage 提取文本内容。
//
// 对齐 Python: _extract_response_text(response)
// 支持纯文本和多模态 content parts
func extractResponseText(msg *llmschema.AssistantMessage) string {
	if msg == nil {
		return ""
	}
	content := msg.GetContent()
	if content.IsText() {
		return strings.TrimSpace(content.Text())
	}
	// 多模态 parts: 查找 text 类型 part（对齐 Python: isinstance(content, list) → 查找 type=="text"）
	parts := content.Parts()
	for _, part := range parts {
		if part.Type == "text" && part.Text != "" {
			return strings.TrimSpace(part.Text)
		}
	}
	return strings.TrimSpace(content.String())
}

// guessImageMIMEType 推断图片 MIME 类型（对齐 Python: _guess_mime_type）
// 优先使用 http.DetectContentType 内容检测，降级到扩展名推断，最终降级到 image/jpeg
func guessImageMIMEType(filePath string, data []byte) string {
	// 先尝试从内容检测（http.DetectContentType 需要至少 512 字节）
	if len(data) >= 512 {
		detected := http.DetectContentType(data[:512])
		if strings.HasPrefix(detected, "image/") {
			return detected
		}
	}

	// 降级到扩展名推断
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".bmp":
		return "image/bmp"
	case ".svg":
		return "image/svg+xml"
	case ".tiff", ".tif":
		return "image/tiff"
	default:
		return "image/jpeg"
	}
}

// isRetryableError 判断错误是否可重试（对齐 Python: is_retryable 检查 429/500/502/503/504）
func isRetryableError(errStr string) bool {
	for _, code := range []string{"429", "500", "502", "503", "504"} {
		if strings.Contains(errStr, code) {
			return true
		}
	}
	return false
}
