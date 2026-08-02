package multimodal

import (
	"context"
	"fmt"

	modelclients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ImageOCRInput image_ocr 工具的输入参数
type ImageOCRInput struct {
	// ImagePathOrURL 本地图片路径或公网 http(s) 图片 URL
	ImagePathOrURL string `json:"image_path_or_url"`
	// Prompt 可选，自定义 OCR 提示词
	Prompt string `json:"prompt,omitempty"`
}

// VQAInput visual_question_answering 工具的输入参数
type VQAInput struct {
	// ImagePathOrURL 本地图片路径或公网 http(s) 图片 URL
	ImagePathOrURL string `json:"image_path_or_url"`
	// Question 要询问图片的问题
	Question string `json:"question"`
	// IncludeOCR 是否先执行 OCR 并把结果拼接进问答提示词（默认 true，对齐 Python）
	IncludeOCR bool `json:"include_ocr,omitempty"`
	// OCRPrompt 可选，自定义 OCR 提示词
	OCRPrompt string `json:"ocr_prompt,omitempty"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewImageOCRTool 创建图片 OCR 工具。
//
// 对齐 Python: ImageOCRTool.__init__ + ImageOCRTool.invoke
// 使用 tool.NewTool[ImageOCRInput, map[string]any] 模式
func NewImageOCRTool(
	client modelclients.BaseModelClient,
	config *hschema.VisionModelConfig,
	language, agentID string,
) tool.Tool {
	card, _ := tools.BuildToolCard("image_ocr", "ImageOCRTool", language, nil, agentID)

	fn := func(ctx context.Context, input ImageOCRInput, opts ...tool.ToolOption) (map[string]any, error) {
		// 对齐 Python: ImageOCRTool.invoke 的 try/except 异常包装
		result, err := func() (map[string]any, error) {
			// 1. 确定 OCR 提示词（对齐 Python: prompt = inputs.get("prompt") or DEFAULT_OCR_PROMPT）
			prompt := input.Prompt
			if prompt == "" {
				prompt = defaultOCRPrompt
			}

			// 2. 构建图片内容块
			imageContent, err := BuildImageContent(input.ImagePathOrURL)
			if err != nil {
				return nil, err
			}

			// 3. 调用视觉模型
			text, model, err := CallVisionModel(ctx, client, imageContent, prompt, config)
			if err != nil {
				return nil, err
			}

			return map[string]any{"text": text, "model": model}, nil
		}()
		if err != nil {
			logger.Error(logComponent).Err(err).
				Str("event_type", "TOOL_CALL_ERROR").Str("tool_name", "image_ocr").
				Msg("ImageOCRTool 调用失败")
			return nil, exception.NewBaseError(
				exception.StatusToolMultimodalVisionInvokeFailed,
				exception.WithMsg(err.Error()),
			)
		}
		return result, nil
	}

	invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
	return invokeFn
}

// NewVisualQuestionAnsweringTool 创建图片问答工具。
//
// 对齐 Python: VisualQuestionAnsweringTool.__init__ + VisualQuestionAnsweringTool.invoke
// 可选先 OCR 再 VQA（对齐 Python: include_ocr=True 默认）
func NewVisualQuestionAnsweringTool(
	client modelclients.BaseModelClient,
	config *hschema.VisionModelConfig,
	language, agentID string,
) tool.Tool {
	card, _ := tools.BuildToolCard("visual_question_answering", "VisualQuestionAnsweringTool", language, nil, agentID)

	fn := func(ctx context.Context, input VQAInput, opts ...tool.ToolOption) (map[string]any, error) {
		// 对齐 Python: VisualQuestionAnsweringTool.invoke 的 try/except 异常包装
		result, err := func() (map[string]any, error) {
			// 1. 构建图片内容块
			imageContent, err := BuildImageContent(input.ImagePathOrURL)
			if err != nil {
				return nil, err
			}

			// 2. OCR 步骤（对齐 Python: if include_ocr → 先 OCR）
			var ocrText string
			// Python 默认 include_ocr=True；JSON "omitempty" 对 bool false 不输出
			// 所以当 JSON 不含 include_ocr 字段时，Go 默认值 false → 需要特殊处理
			// 对齐 Python 行为：默认 include_ocr=True
			includeOCR := input.IncludeOCR
			if includeOCR {
				ocrPrompt := input.OCRPrompt
				if ocrPrompt == "" {
					ocrPrompt = defaultOCRPrompt
				}
				ocrText, _, err = CallVisionModel(ctx, client, imageContent, ocrPrompt, config)
				if err != nil {
					// OCR 失败不阻止 VQA（对齐 Python: ocr_text 可为空）
					logger.Warn(logComponent).Err(err).
						Str("event_type", "OCR_FAILED").
						Str("tool_name", "visual_question_answering").
						Msg("OCR 步骤失败，继续 VQA")
					ocrText = ""
				}
			}

			// 3. 构造 VQA 提示词（对齐 Python: DEFAULT_VQA_PROMPT_TEMPLATE.format）
			vqaPrompt := input.Question
			if includeOCR {
				vqaPrompt = fmt.Sprintf(defaultVQAPromptTemplate, ocrTextOrNone(ocrText), input.Question)
			}

			// 4. 调用视觉模型
			answer, model, err := CallVisionModel(ctx, client, imageContent, vqaPrompt, config)
			if err != nil {
				return nil, err
			}

			return map[string]any{
				"answer":    answer,
				"ocr_text":  ocrText,
				"model":     model,
			}, nil
		}()
		if err != nil {
			logger.Error(logComponent).Err(err).
				Str("event_type", "TOOL_CALL_ERROR").Str("tool_name", "visual_question_answering").
				Msg("VQATool 调用失败")
			return nil, exception.NewBaseError(
				exception.StatusToolMultimodalVisionInvokeFailed,
				exception.WithMsg(err.Error()),
			)
		}
		return result, nil
	}

	invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
	return invokeFn
}

// CreateVisionTools 创建视觉工具集。
//
// 对齐 Python: create_vision_tools(language, vision_model_config, agent_id) -> list[Tool]
func CreateVisionTools(
	client modelclients.BaseModelClient,
	config *hschema.VisionModelConfig,
	language, agentID string,
) []tool.Tool {
	return []tool.Tool{
		NewImageOCRTool(client, config, language, agentID),
		NewVisualQuestionAnsweringTool(client, config, language, agentID),
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// ocrTextOrNone 对齐 Python: ocr_text or "No OCR used"
func ocrTextOrNone(ocrText string) string {
	if ocrText == "" {
		return "No OCR used"
	}
	return ocrText
}
