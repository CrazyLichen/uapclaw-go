package parser

import (
	"context"
	"encoding/base64"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ImageCaptioner 图片描述生成器。
//
// 构造即检查 LLM 客户端支持度（nil 或模型名不在白名单仅 Warn 不报错——
// caption 是软能力，禁用态合法）。归档能力经包级 CpImage 提供。
//
// Python: captioner.py ImageCaptioner
type ImageCaptioner struct {
	// llmClient 可选 LLM 客户端（nil = 禁用态：仅归档不 caption）。
	llmClient *llm.Model
}
// ──────────────────────────── 枚举 ────────────────────────────
// ──────────────────────────── 常量 ────────────────────────────

const (
	// imageCaptionPrompt 图片描述生成提示词。
	//
	// Python: captioner.py IMAGE_CAPTION_PROMPT（逐字复制，禁止翻译改写——
	// 记忆 feedback_prompt_verbatim_copy 既定规则）。
	imageCaptionPrompt = "You are an assistant specialized in document and image analysis. " +
		"Your task is to provide a detailed, qualitative description of the provided image " +
		"so that it can be embedded and used for semantic retrieval. " +
		"Describe all visible content including text, figures, charts, tables, diagrams, and layout. " +
		"Focus on what the image conveys and means, not just what it literally depicts. " +
		"Do not include any preamble — output only the description."

	// savedImageDir 归档图片目录（相对路径，字面对齐 Python SAVED_IMAGE_DIR = "images"）。
	savedImageDir = "images"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// supportedLLMClient 支持图片 captioning 的模型名前缀白名单。
	//
	// Python: captioner.py ImageCaptioner._supported_llm_client（类属性，
	// Go 归入包级变量区块）。
	supportedLLMClient = []string{"gpt-4o", "gpt-5", "qwen3-vl", "qwen-vl"}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewImageCaptioner 构造 ImageCaptioner。
//
// 构造即调用 isLLMSupported（对齐 Python __init__ 内 self._is_llm_supported()）。
//
// Python: captioner.py ImageCaptioner.__init__
func NewImageCaptioner(llmClient *llm.Model) *ImageCaptioner {
	c := &ImageCaptioner{llmClient: llmClient}
	c.isLLMSupported()
	return c
}

// CpImage 复制图片到 savedImageDir 目录并返回目标路径。
//
// 分支（逐条对齐 Python cp_image）：
//  1. 源不存在 → 返回 error（对齐 raise FileNotFoundError）
//  2. 目标目录创建失败 → 返回 error（对齐 makedirs 异常传播）
//  3. 源与目标同一路径 → 跳过复制直接返回目标
//  4. 复制失败 → Warn 日志 + 返回原路径 + nil（软失败，对齐 Python fallback）
//
// Python: captioner.py ImageCaptioner.cp_image（static method；Go 无默认参数，
// targetDir 定死 savedImageDir 常量——全 codebase 无非默认调用，spec 决策 5）
func CpImage(imageLoc string) (string, error) {
	if _, err := os.Stat(imageLoc); err != nil {
		return "", err
	}

	if err := os.MkdirAll(savedImageDir, 0o755); err != nil {
		return "", err
	}

	destPath := filepath.Join(savedImageDir, filepath.Base(imageLoc))

	// 仅当目标与源不同路径时复制（对齐 Python abspath 比较）
	srcAbs, srcErr := filepath.Abs(imageLoc)
	destAbs, destErr := filepath.Abs(destPath)
	if srcErr == nil && destErr == nil && srcAbs == destAbs {
		return destPath, nil
	}

	if err := copyFile(imageLoc, destPath); err != nil {
		// 对齐 Python: logger.warning(f"Failed to save copy of {image_loc} to {dest_path}: {e}")
		logger.Warn(logComponent).
			Str("image_loc", imageLoc).
			Str("dest_path", destPath).
			Err(err).
			Msg("failed to save copy of image")
		// 复制失败回落原路径（对齐 Python fall back）
		return imageLoc, nil
	}
	return destPath, nil
}

// CaptionImages 逐图串行生成 caption 列表。
//
// 文件不存在的图：Warn + 空串占位（对齐 Python skip 分支）。
// 顺序与输入一致（对齐 Python captions.append 逐个累积）。
//
// Python: captioner.py ImageCaptioner.caption_images
func (c *ImageCaptioner) CaptionImages(ctx context.Context, imageLocs []string) []string {
	captions := make([]string, 0, len(imageLocs))
	for _, imageLoc := range imageLocs {
		var caption string
		if _, err := os.Stat(imageLoc); err != nil {
			// 对齐 Python: logger.warning(f"Image file {image_loc} does not exist, skipping captioning.")
			logger.Warn(logComponent).
				Str("image_loc", imageLoc).
				Msg("image file does not exist, skipping captioning")
			caption = ""
		} else {
			caption = c.llmCall(ctx, imageLoc)
		}
		captions = append(captions, caption)
	}
	return captions
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// init 注册 Python 端补充的 MIME 类型映射（幂等：retrieval/common/document.go
// init 已注册过，重复注册同一映射无副作用；字面对齐 captioner.py 模块级
// mimetypes.add_type("image/jpeg", ".jfif", strict=True)）。
func init() {
	// Python mimetypes.add_type 无返回值；注册失败仅影响该扩展名识别
	_ = mime.AddExtensionType(".jfif", "image/jpeg")
}

// isLLMSupported 检查 LLM 客户端对图片 captioning 的支持度。
//
// 两分支均仅 Warn 不报错（对齐 Python docstring 声明的 bool 语义但实现
// 只打日志）：
//  1. nil 客户端 → 禁用态提示
//  2. 模型名前缀不在白名单 → 可能不支持提示
//
// Python: captioner.py ImageCaptioner._is_llm_supported
func (c *ImageCaptioner) isLLMSupported() {
	if c.llmClient == nil {
		// Python f"...empty {self.llm_client=}..." 的 = 自省语法 Go 无等价物，
		// 用字面 "nil" 近似（偏差归档 spec §8-3）
		logger.Warn(logComponent).
			Str("llm_client", "nil").
			Msg("image captioning is disabled for empty llm_client, please ensure an appropriate VLM is used")
		return
	}
	modelName := ""
	if c.llmClient.ModelConfig != nil {
		modelName = c.llmClient.ModelConfig.ModelName
	}
	for _, base := range supportedLLMClient {
		if strings.HasPrefix(modelName, base) {
			return
		}
	}
	// 对齐 Python f"{self.llm_client.model_config.model_name=} may not be supported..."
	logger.Warn(logComponent).
		Str("model_name", modelName).
		Msg("model may not be supported for image captioning, please ensure an appropriate VLM is used")
}

// llmCall 生成单张图片的 caption。
//
// 流程（逐条对齐 Python _llm_call_async）：
//  1. nil 客户端 → 返回空串（禁用态非失败）
//  2. mime 按扩展名检测；未知 → 回落 image/png + Warn
//  3. 读文件 base64 编码
//  4. 多模态消息（text 提示词 + image_url data URI）Invoke
//  5. resp.Content.Text() 取描述
//  6. 任一步失败 → Warn + 返回空串（软失败）
//
// Python: captioner.py ImageCaptioner._llm_call_async
func (c *ImageCaptioner) llmCall(ctx context.Context, imageLoc string) string {
	if c.llmClient == nil {
		return ""
	}

	// 对齐 Python: mime_type, _ = mimetypes.guess_type(image_loc)
	mimeType := mime.TypeByExtension(filepath.Ext(imageLoc))
	if mimeType == "" {
		mimeType = "image/png"
		// 对齐 Python: logger.warning(f"Could not determine MIME type for {image_loc=}, using {mime_type}")
		logger.Warn(logComponent).
			Str("image_loc", imageLoc).
			Str("mime_type", mimeType).
			Msg("could not determine MIME type, using fallback")
	}

	// 对齐 Python: logger.info(f"Calling LLM for image captioning for {image_loc=}")
	logger.Info(logComponent).
		Str("image_loc", imageLoc).
		Msg("calling LLM for image captioning")

	imageBytes, err := os.ReadFile(imageLoc)
	if err != nil {
		// 对齐 Python: logger.warning(f"LLM-based caption for {image_loc=} with invocation failed for {image_loc}: {e}")
		//（Python 该条日志变量冗余，Go 字段单次——偏差归档 spec §8-4）
		logger.Warn(logComponent).
			Str("image_loc", imageLoc).
			Err(err).
			Msg("LLM-based caption failed for image")
		return ""
	}
	imageB64 := base64.StdEncoding.EncodeToString(imageBytes)

	// 对齐 Python messages=[{"role": "user", "content": [
	//   {"type": "text", "text": IMAGE_CAPTION_PROMPT},
	//   {"type": "image_url", "image_url": {"url": f"data:{mime_type};base64,{image_b64}"}}]}]
	msg := llmschema.NewUserMessage("", llmschema.WithMultiModalContent(
		llmschema.ContentPart{
			Type: "text",
			Text: imageCaptionPrompt,
		},
		llmschema.ContentPart{
			Type: "image_url",
			ImageURL: &llmschema.ImageURL{
				URL: fmt.Sprintf("data:%s;base64,%s", mimeType, imageB64),
			},
		},
	))

	resp, err := c.llmClient.Invoke(ctx, model_clients.NewMessagesParam(msg))
	if err != nil {
		// 对齐 Python: logger.warning(f"LLM-based caption ... failed ...: {e}")
		//（Python 该条日志变量冗余，Go 字段单次——偏差归档 spec §8-4）
		logger.Warn(logComponent).
			Str("image_loc", imageLoc).
			Err(err).
			Msg("LLM-based caption failed for image")
		return ""
	}
	if resp == nil {
		return ""
	}
	// 对齐 Python: return resp.content if hasattr(resp, "content") else str(resp)
	return resp.Content.Text()
}

// copyFile 读源写目标复制文件。
//
// Python shutil.copy2 保留元数据；caption 场景仅读内容，元数据对下游
// （13.4 多模态嵌入读内容）无意义——偏差归档 spec §8-2。
func copyFile(src, dest string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dest, data, 0o644)
}
