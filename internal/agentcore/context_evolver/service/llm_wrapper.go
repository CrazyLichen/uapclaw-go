package service

import (
	"context"
	"fmt"
	"strings"

	ceconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients/openai"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/context"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// OpenAILLMWrapper 包装 BaseModelClient 提供 LLMService 接口。
// 对齐 Python OpenAILLMWrapper。
//
// 设计决策：
//   - 直接调 BaseModelClient.Invoke()，不走 Model 门面回调（对齐 Python 绕过 Model facade）
//   - 内部用 Go 已有的 model_clients.NewOpenAIModelClient() 创建 client
//   - 保留 isNewerModel 判断（gpt-4/gpt-5/o1/o3 不传 max_tokens）
//
// Python: openjiuwen/extensions/context_evolver/service/task_memory_service.py (OpenAILLMWrapper)
type OpenAILLMWrapper struct {
	// modelName 模型名称
	modelName string
	// temperature 采样温度
	temperature float64
	// maxTokens 最大生成 token 数
	maxTokens int
	// isNewerModel 是否为较新模型（不传 max_tokens）
	isNewerModel bool
	// client 底层模型客户端（直接调用，不走 Model 门面回调）
	client model_clients.BaseModelClient
	// modelConfig 模型请求配置
	modelConfig *llmschema.ModelRequestConfig
	// clientConfig 模型客户端配置
	clientConfig *llmschema.ModelClientConfig
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewOpenAILLMWrapper 创建 OpenAI LLM 适配器。
// 对齐 Python OpenAILLMWrapper.__init__(model_name, api_key, base_url, temperature, max_tokens)。
// 内部用 Go 已有的 model_clients.NewOpenAIModelClient() 创建 client，直接调 client.Invoke()，
// 不走 Model 门面（无回调装饰，与 Python 行为一致）。
func NewOpenAILLMWrapper(modelName string, apiKey string, baseURL string, temperature float64, maxTokens int) (*OpenAILLMWrapper, error) {
	// 对齐 Python：api_key = api_key or config.get("API_KEY")
	if apiKey == "" {
		apiKey = ceconfig.GetString("API_KEY", "")
	}
	if apiKey == "" {
		return nil, exception.NewBaseError(
			exception.StatusToolchainEvolvingMemoryConfigInvalid,
			exception.WithMsg("API key not provided and API_KEY not set in config"),
		)
	}

	// 对齐 Python：base_url = base_url or "https://api.openai.com/v1"
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	// 对齐 Python：_is_newer_model 判断
	isNewer := isNewerModelCheck(modelName)

	// 构建 ModelClientConfig（对齐 Python ModelClientConfig）
	clientConfig := &llmschema.ModelClientConfig{
		ClientProvider: "OpenAI",
		APIKey:         apiKey,
		APIBase:        baseURL,
		VerifySSL:      false,
	}

	// 构建 ModelRequestConfig（对齐 Python ModelRequestConfig）
	modelConfig := &llmschema.ModelRequestConfig{
		ModelName:   modelName,
		Temperature: temperature,
	}

	// 对齐 Python：self.client = OpenAIModelClient(model_config, model_client_config)
	client, err := openai.NewOpenAIModelClient(modelConfig, clientConfig)
	if err != nil {
		return nil, exception.NewBaseError(
			exception.StatusToolchainEvolvingMemoryConfigInvalid,
			exception.WithMsg(fmt.Sprintf("Failed to create OpenAI model client: %s", err.Error())),
		)
	}

	// 对齐 Python：logger.info("Initialized OpenAI LLM with model: %s", model_name)
	logger.Info(logComponent).
		Str("model_name", modelName).
		Msg("Initialized OpenAI LLM")

	return &OpenAILLMWrapper{
		modelName:    modelName,
		temperature:  temperature,
		maxTokens:    maxTokens,
		isNewerModel: isNewer,
		client:       client,
		modelConfig:  modelConfig,
		clientConfig: clientConfig,
	}, nil
}

// Generate 调用 LLM 生成文本响应。
// 对齐 Python OpenAILLMWrapper.async_generate(prompt, system_prompt?, temperature?, max_tokens?)。
// 实现 cecontext.LLMService 接口。
func (w *OpenAILLMWrapper) Generate(ctx context.Context, prompt string, opts ...cecontext.GenerateOption) (string, error) {
	// 解析选项
	cfg := &cecontext.GenerateConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	// 对齐 Python：构建 messages 列表
	dicts := make([]map[string]any, 0, 2)
	if cfg.SystemPrompt != "" {
		dicts = append(dicts, map[string]any{
			"role":    "system",
			"content": cfg.SystemPrompt,
		})
	}
	dicts = append(dicts, map[string]any{
		"role":    "user",
		"content": prompt,
	})

	return w.invokeWithDicts(ctx, dicts, cfg)
}

// GenerateWithMessages 使用消息列表调用 LLM 生成文本响应。
// 对齐 Python OpenAILLMWrapper.async_generate_with_messages(messages, temperature?, max_tokens?)。
func (w *OpenAILLMWrapper) GenerateWithMessages(ctx context.Context, messages []map[string]any, opts ...cecontext.GenerateOption) (string, error) {
	cfg := &cecontext.GenerateConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	return w.invokeWithDicts(ctx, messages, cfg)
}

// String 实现 Stringer 接口。
// 对齐 Python OpenAILLMWrapper.__repr__()。
func (w *OpenAILLMWrapper) String() string {
	return fmt.Sprintf("OpenAILLMWrapper(model=%s)", w.modelName)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// invokeWithDicts 核心调用逻辑，对齐 Python 中 async_generate / async_generate_with_messages 的公共部分。
// 构建 InvokeOption → client.Invoke() → 提取 content。
func (w *OpenAILLMWrapper) invokeWithDicts(ctx context.Context, dicts []map[string]any, cfg *cecontext.GenerateConfig) (string, error) {
	// 对齐 Python：构建 invoke kwargs
	invokeOpts := make([]model_clients.InvokeOption, 0, 2)

	// 温度：优先使用 GenerateOption 中的覆盖值
	temperature := w.temperature
	if cfg.Temperature != 0 {
		temperature = cfg.Temperature
	}
	invokeOpts = append(invokeOpts, model_clients.WithInvokeTemperature(temperature))

	// 对齐 Python：if not self._is_newer_model: invoke_kwargs["max_tokens"] = max_tokens or self.max_tokens
	if !w.isNewerModel {
		maxTokens := w.maxTokens
		if cfg.MaxTokens != 0 {
			maxTokens = cfg.MaxTokens
		}
		invokeOpts = append(invokeOpts, model_clients.WithInvokeMaxTokens(maxTokens))
	}

	// 对齐 Python：response = await self.client.invoke(**invoke_kwargs)
	messagesParam := model_clients.NewDictsMessagesParam(dicts)
	response, err := w.client.Invoke(ctx, messagesParam, invokeOpts...)
	if err != nil {
		// 对齐 Python：except ToolchainError: raise
		if _, ok := err.(*exception.BaseError); ok {
			return "", err
		}
		// 对齐 Python：except Exception as e: logger.error("LLM generation failed: %s", e)
		logger.Error(logComponent).Err(err).Msg("LLM generation failed")
		return "", exception.NewBaseError(
			exception.StatusToolchainEvolvingMemoryLlmGenerationExecutionError,
			exception.WithMsg(err.Error()),
		)
	}

	// 对齐 Python：content = response.content or ""
	content := response.Content.Text()

	// 对齐 Python：logger.debug("LLM generated %s characters", len(content))
	logger.Debug(logComponent).
		Int("char_count", len(content)).
		Msg("LLM generated")

	return content, nil
}

// isNewerModelCheck 判断模型是否为较新模型（不传 max_tokens）。
// 对齐 Python OpenAILLMWrapper._is_newer_model。
// gpt-4/gpt-5/o1/o3 系列使用 max_completion_tokens 而非 max_tokens，
// Go SDK 内置此判断，Wrapper 层只需不在 InvokeOption 中传 MaxTokens 即可。
func isNewerModelCheck(modelName string) bool {
	modelLower := strings.ToLower(modelName)
	return strings.Contains(modelLower, "gpt-4") ||
		strings.Contains(modelLower, "gpt-5") ||
		strings.Contains(modelLower, "o1") ||
		strings.Contains(modelLower, "o3")
}
