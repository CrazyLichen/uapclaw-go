package tools

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// ──────────────────────────── 结构体 ────────────────────────────

// generateImageInput generate_image 工具入参。
// jsonschema description 从 Python docstring 逐字复制。
// Python: generate_image(prompt, size, quality, save_dir) (image_tools.py L425-429)
type generateImageInput struct {
	// Prompt 图片描述
	Prompt string `json:"prompt" jsonschema:"description=Text description of the image to generate,required"`
	// Size 图片尺寸
	Size string `json:"size" jsonschema:"description=Image size, options: \"256x256\", \"512x512\", \"1024x1024\", \"1792x1024\", \"1024x1792\""`
	// Quality 图片质量
	Quality string `json:"quality" jsonschema:"description=Image quality, \"standard\" or \"hd\""`
	// SaveDir 可选保存目录
	SaveDir string `json:"save_dir" jsonschema:"description=Optional directory to save the image (defaults to \"generated_images\")"`
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// imageGenUA 图片下载固定 UA。
	// Python: image_tools.py L395-398（Chrome/91 固定 UA）
	imageGenUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
		"(KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36"

	// imageGenMaxTries 生成重试次数。
	// Python: _RetryExecutor.with_backoff(_call, max_tries=3)
	imageGenMaxTries = 3
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// GenerateImage generate_image 工具包级单例（对齐 Python 模块级 @tool 实例）。
	// Python: generate_image (image_tools.py L417)
	GenerateImage = newGenerateImageTool()

	// imageGenModelBuilder 模型构造器（测试注入点）。
	// Python 直接在函数内构造 Model；Go 提取为包级变量以便测试替换。
	imageGenModelBuilder = defaultImageGenModelBuilder

	// imageGenRetryBaseDelay 重试退避基数秒（Python base_delay=4，4^i 退避；测试可注入 0）。
	imageGenRetryBaseDelay = 4
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ApplyImageGenModelConfigFromYAML 从 config.yaml 读取文生图模型配置并设置环境变量。
//
// 配置优先级:
//  1. models.image_gen.model_config（或 model_client_config 双层 fallback）
//
// 四字段非空才 Setenv（不覆盖已有 env 的空值以外的语义——直接以 YAML 值覆盖）。
//
// Python: apply_image_gen_model_config_from_yaml (multimodal_config.py L256-285)
//
// 注意：与 audio/vision/video 的 apply_* 不同，Python image_gen 版本
// 无 embed 层回退、无 strict 逻辑——仅 model_config 直读。
func ApplyImageGenModelConfigFromYAML(configBase map[string]any) {
	if configBase == nil {
		return
	}
	mc := getModelConfigTyped(configBase, "image_gen")

	apiKey := stripAny(mc["api_key"])
	apiBase := stripAny(mc["api_base"])
	modelName := stripAny(mc["model_name"])
	if modelName == "" {
		modelName = stripAny(mc["model"])
	}
	provider := stripAny(mc["model_provider"])

	if apiKey != "" {
		_ = os.Setenv("IMAGE_GEN_API_KEY", apiKey)
	}
	if apiBase != "" {
		_ = os.Setenv("IMAGE_GEN_API_BASE", apiBase)
	}
	if modelName != "" {
		_ = os.Setenv("IMAGE_GEN_MODEL_NAME", modelName)
	}
	if provider != "" {
		_ = os.Setenv("IMAGE_GEN_PROVIDER", provider)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newGenerateImageTool 构造 generate_image 工具。
// Python: generate_image (image_tools.py L417-480)
func newGenerateImageTool() tool.Tool {
	card := tool.NewToolCard(
		"generate_image",
		"Generate an image from a text description using AI image generation models. "+
			"Use this tool when the user wants to create an image based on a text prompt. "+
			"Returns the path to the saved generated image file.",
		nil, // InputParams 由 NewTool 从 input struct 反射生成
		nil,
	)

	fn := func(ctx context.Context, input generateImageInput, opts ...tool.ToolOption) (map[string]any, error) {
		// Python: apply_image_gen_model_config_from_yaml(get_config())（吞错 debug 日志）
		if cfg := loadConfigMapOrEmpty(); cfg != nil {
			ApplyImageGenModelConfigFromYAML(cfg)
		}

		_, _, model, provider := getImageGenAPICredentials()
		logger.Info(logger.ComponentCommon).
			Str("model_name", model).
			Str("provider", provider).
			Str("size", input.Size).
			Str("quality", input.Quality).
			Msg("[generate_image] using model")

		result := invokeModelImageGeneration(ctx, input.Prompt, input.Size, input.Quality)

		if errText, hasErr := result["error"]; hasErr {
			return map[string]any{"result": errText.(string)}, nil
		}

		imagePath := result["image_path"].(string)

		// Python: save_dir 非空时 MkdirAll + Rename 搬移
		if input.SaveDir != "" {
			if mkErr := os.MkdirAll(input.SaveDir, 0o755); mkErr != nil {
				return map[string]any{"result": fmt.Sprintf("[ERROR]: failed to create save directory: %v", mkErr)}, nil
			}
			newPath := filepath.Join(input.SaveDir, filepath.Base(imagePath))
			if rnErr := os.Rename(imagePath, newPath); rnErr != nil {
				return map[string]any{"result": fmt.Sprintf("[ERROR]: failed to move image to save directory: %v", rnErr)}, nil
			}
			absPath, _ := filepath.Abs(newPath)
			imagePath = absPath
		}

		revisedPrompt := input.Prompt
		if rp, ok := result["revised_prompt"].(string); ok {
			revisedPrompt = rp
		}
		originalURL, _ := result["original_url"].(string)

		// 逐字对齐 Python response_parts 拼接
		responseParts := []string{
			"Image generated successfully!",
			"Saved to: " + imagePath,
			"Prompt: " + input.Prompt,
		}
		if revisedPrompt != input.Prompt {
			responseParts = append(responseParts, "Revised prompt: "+revisedPrompt)
		}
		if originalURL != "" {
			responseParts = append(responseParts, "Original URL: "+originalURL)
		}
		return map[string]any{"result": strings.Join(responseParts, "\n")}, nil
	}

	invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
	return invokeFn
}

// getImageGenAPICredentials 从环境变量获取图片生成凭据。
// 默认 provider: DashScope；默认 api_base: https://dashscope.aliyuncs.com/api/v1；默认 model: wanx-v1。
// Python: _get_image_gen_api_credentials (image_tools.py L97-112)
func getImageGenAPICredentials() (key, base, model, provider string) {
	key = os.Getenv("IMAGE_GEN_API_KEY")
	if key == "" {
		key = os.Getenv("API_KEY")
	}
	base = os.Getenv("IMAGE_GEN_API_BASE")
	if base == "" {
		base = os.Getenv("API_BASE")
	}
	if base == "" {
		base = "https://dashscope.aliyuncs.com/api/v1"
	}
	model = os.Getenv("IMAGE_GEN_MODEL_NAME")
	if model == "" {
		model = "wanx-v1"
	}
	provider = os.Getenv("IMAGE_GEN_PROVIDER")
	if provider == "" {
		provider = "DashScope"
	}
	return key, base, model, provider
}

// defaultImageGenModelBuilder 默认模型构造器。
// Python: _invoke_model_image_generation 内联构造 Model (image_tools.py L343-358)
func defaultImageGenModelBuilder() (*llm.Model, error) {
	apiKey, apiBase, model, provider := getImageGenAPICredentials()
	clientConfig := &llmschema.ModelClientConfig{
		ClientID:       "image_gen_client",
		ClientProvider: provider,
		APIKey:         apiKey,
		APIBase:        apiBase,
		VerifySSL:      false,
	}
	modelConfig := &llmschema.ModelRequestConfig{
		ModelName: model,
	}
	return llm.NewModel(clientConfig, modelConfig)
}

// invokeModelImageGeneration 使用内部 Model 类生成图片（DashScope 等）。
// 返回 map 含 'image_path' 或 'error' 键。
// Python: _invoke_model_image_generation (image_tools.py L324-414)
func invokeModelImageGeneration(ctx context.Context, prompt, size, quality string) map[string]any {
	// Python: 整个生成流程包在 try/except 中，异常返回错误 map
	apiKey, _, genModel, _ := getImageGenAPICredentials()
	if apiKey == "" {
		return map[string]any{"error": "[ERROR]: IMAGE_GEN_API_KEY or API_KEY is not configured for image generation."}
	}

	result, err := withImageGenRecover(func() (*llmschema.ImageGenerationResponse, error) {
		modelInstance, mErr := imageGenModelBuilder()
		if mErr != nil {
			return nil, mErr
		}
		// Python: messages = [UserMessage(content=prompt)] + generate_image(messages, model=model)
		messages := []*llmschema.UserMessage{llmschema.NewUserMessage(prompt)}

		// Python: _RetryExecutor.with_backoff(_call, max_tries=3)（base_delay=4，4^i 退避）
		var lastErr error
		for i := 1; i <= imageGenMaxTries; i++ {
			resp, callErr := modelInstance.GenerateImage(ctx, messages, model_clients.WithImageModel(genModel))
			if callErr == nil {
				return resp, nil
			}
			lastErr = callErr
			if i < imageGenMaxTries {
				time.Sleep(time.Duration(power(imageGenRetryBaseDelay, i)) * time.Second)
			}
		}
		return nil, lastErr
	})
	if err != nil {
		return map[string]any{"error": fmt.Sprintf("[ERROR]: Image generation failed: %v", err)}
	}

	// 输出路径：workspace 下 generated_{UTC 时间戳}_{1000-9999 随机}.png
	// Python: image_tools.py L367-370
	outputDir := workspace.AgentWorkspaceDir()
	timestamp := time.Now().UTC().Format("20060102_150405")
	randomSuffix, _ := rand.Int(rand.Reader, big.NewInt(9000))
	outputPath := filepath.Join(outputDir, fmt.Sprintf("generated_%s_%d.png", timestamp, 1000+randomSuffix.Int64()))

	// Python: image_url 优先判 images（L378-379），后判 images_base64（L380-381）
	var imageURL string
	var imageBase64Data string
	if len(result.Images) > 0 {
		imageURL = result.Images[0]
	} else if len(result.ImagesBase64) > 0 {
		imageBase64Data = result.ImagesBase64[0]
	}

	if imageBase64Data != "" {
		// base64 分支：解码落盘
		// Python: image_tools.py L383-392
		imgBytes, bErr := base64.StdEncoding.DecodeString(imageBase64Data)
		if bErr != nil {
			return map[string]any{"error": fmt.Sprintf("[ERROR]: Image generation failed: %v", bErr)}
		}
		if wErr := os.WriteFile(outputPath, imgBytes, 0o644); wErr != nil {
			return map[string]any{"error": fmt.Sprintf("[ERROR]: Image generation failed: %v", wErr)}
		}
		return map[string]any{
			"image_path":     absPathOrRaw(outputPath),
			"revised_prompt": prompt,
		}
	}
	if imageURL != "" {
		// URL 分支：固定 UA 下载落盘
		// Python: image_tools.py L393-409
		req, rErr := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
		if rErr != nil {
			return map[string]any{"error": fmt.Sprintf("[ERROR]: Image generation failed: %v", rErr)}
		}
		req.Header.Set("User-Agent", imageGenUA)
		resp, dErr := http.DefaultClient.Do(req)
		if dErr != nil {
			return map[string]any{"error": fmt.Sprintf("[ERROR]: Image generation failed: %v", dErr)}
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			return map[string]any{"error": fmt.Sprintf("[ERROR]: Image generation failed: status %d", resp.StatusCode)}
		}
		imgBytes := make([]byte, 0)
		buf := make([]byte, 32*1024)
		for {
			n, rErr2 := resp.Body.Read(buf)
			imgBytes = append(imgBytes, buf[:n]...)
			if rErr2 != nil {
				break
			}
		}
		if wErr := os.WriteFile(outputPath, imgBytes, 0o644); wErr != nil {
			return map[string]any{"error": fmt.Sprintf("[ERROR]: Image generation failed: %v", wErr)}
		}
		return map[string]any{
			"image_path":     absPathOrRaw(outputPath),
			"revised_prompt": prompt,
			"original_url":   imageURL,
		}
	}

	return map[string]any{"error": "[ERROR]: No valid image data in response"}
}

// withImageGenRecover 执行 fn 并 recover panic（对齐 Python try/except Exception）。
// Python: image_tools.py L342/L413-414
func withImageGenRecover(fn func() (*llmschema.ImageGenerationResponse, error)) (resp *llmschema.ImageGenerationResponse, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()
	return fn()
}

// power 整数幂（重试退避 4^i）。
func power(base, exp int) int {
	result := 1
	for i := 0; i < exp; i++ {
		result *= base
	}
	return result
}

// absPathOrRaw 转 absolute 路径（失败回退原值）。
// Python: Path(output_path).absolute() / str(new_path.absolute())
func absPathOrRaw(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

// stripAny map 值转 strip 后字符串（对齐 Python str(x or "").strip()）。
func stripAny(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	if s := fmt.Sprintf("%v", v); s != "<nil>" {
		return strings.TrimSpace(s)
	}
	return ""
}

// getModelConfigTyped 从 configBase 提取 models.{modelType}.model_config（或 model_client_config）。
// Python: _get_model_config (multimodal_config.py L28-61)
func getModelConfigTyped(configBase map[string]any, modelType string) map[string]any {
	if configBase == nil {
		return map[string]any{}
	}
	rawModels, ok := configBase["models"].(map[string]any)
	if !ok {
		return map[string]any{}
	}
	inner, ok := rawModels[modelType].(map[string]any)
	if !ok {
		return map[string]any{}
	}
	// Python: mc = inner.get("model_config") or inner.get("model_client_config")
	if mc, ok := inner["model_config"].(map[string]any); ok && mc != nil {
		return mc
	}
	if mc, ok := inner["model_client_config"].(map[string]any); ok && mc != nil {
		return mc
	}
	return map[string]any{}
}

// loadConfigMapOrEmpty 加载配置，失败返回空 map。
// Python: get_config()（工具内 import 局部获取，异常吞掉）
func loadConfigMapOrEmpty() map[string]any {
	c, err := config.New("")
	if err != nil {
		return map[string]any{}
	}
	loaded, err := c.Load()
	if err != nil {
		return map[string]any{}
	}
	if loaded == nil {
		return map[string]any{}
	}
	return loaded
}
