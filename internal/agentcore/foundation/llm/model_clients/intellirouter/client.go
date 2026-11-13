// Package intellirouter IntelliRouter 模型客户端 —— 包装
// intellirouter.ReliableRouter（23.4a 移植的 PyPI intelli-router），提供
// API 池化、智能路由、自动重试、流式与多模态生成。
//
// 对应 Python 代码：openjiuwen/core/foundation/llm/model_clients/intelli_router_model_client.py
// （1415 行，单文件按语义拆为：client.go 主客户端、client_config.go 配置、
// router_cache.go 路由缓存与 D-5 钩子、routed_model.go 占位符回填、
// event_bridge.go 事件桥、dashscope_gen.go 多模态三件套——拆分边界在
// 各文件头标注 Python 行段）。
//
// 已知偏差（总设计 D-3/D-4/D-5）：web_dashboard/metrics/logging_hook 不移植；
// asyncio loop 绑定防御不移植（net/http.Client 并发安全）；动态类继承
// （_RoutedModelRouterMixin）改构造期函数指针注入。
package intellirouter

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	intelliroutertypes "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter"
	intelliroutercore "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/core"
	llmrouter "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/router"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/strategy"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/routing"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// logComponent 组件级日志常量。
const logComponent = logger.ComponentAgentCore

// clientName 注册名（Python: :441 __client_name__ = "intelli_router"）。
const clientName = "intelli_router"

// ──────────────────────────── 结构体 ────────────────────────────

// IntelliRouterModelClient 包装 ReliableRouter 的模型客户端。
// Python: intelli_router_model_client.py:434-460 IntelliRouterModelClient
type IntelliRouterModelClient struct {
	// BaseClientEmbed 基座（消息/工具转换、配置持有）
	model_clients.BaseClientEmbed
	// routerConfig 提取的配置（注入 router 时为 nil，Python: :444-447）
	routerConfig *IntelliRouterClientConfig
	// router 路由器实例
	Router *llmrouter.ReliableRouter
	// routerKey 缓存键（注入 router 时为 nil）
	routerKey string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewIntelliRouterModelClient 创建客户端。
// Python: intelli_router_model_client.py:443-459 __init__
func NewIntelliRouterModelClient(
	modelConfig *llmschema.ModelRequestConfig,
	clientConfig *llmschema.ModelClientConfig,
) (*IntelliRouterModelClient, error) {
	embed, err := model_clients.NewBaseClientEmbed(
		modelConfig, clientConfig,
		model_clients.WithClientName("IntelliRouter client"),
	)
	if err != nil {
		return nil, err
	}
	c := &IntelliRouterModelClient{BaseClientEmbed: *embed}
	cfg := FromModelClientConfig(clientConfig)
	r, err := GetOrCreateRouter(cfg)
	if err != nil {
		return nil, err
	}
	c.routerConfig = cfg
	c.Router = r
	c.routerKey = MakeRouterKey(cfg)
	return c, nil
}

// Invoke 非流式调用。Python: intelli_router_model_client.py:702-757 invoke
func (c *IntelliRouterModelClient) Invoke(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	params := model_clients.NewInvokeParams(opts...)
	messagesDict, err := c.ConvertMessagesToDict(messages)
	if err != nil {
		return nil, err
	}
	modelName := c.resolveModelName(params.Model)
	tools := c.ConvertToolsToDict(params.Tools)

	result, err := c.Router.Invoke(ctx, messagesDict, llmrouter.InvokeParams{
		Tools:       toAnySlice(tools),
		Temperature: c.effectiveTemperature(params.Temperature),
		TopP:        c.effectiveTopP(params.TopP),
		MaxTokens:   c.effectiveMaxTokens(params.MaxTokens),
		Stop:        c.effectiveStop(params.Stop),
		Model:       modelName,
		Kwargs:      map[string]any{},
	})
	if err != nil {
		// Python: :729-748 先触发 LLM_CALL_ERROR 事件，再翻译路由错误
		c.triggerCallError(ctx, modelName, err, false)
		return nil, routing.ToModelError(err, routerViewOf(c.Router), modelName, false)
	}
	// Python: :751 _to_ow_assistant_message
	return c.toOwAssistantMessage(result, params.OutputParser)
}

// Stream 流式调用。Python: intelli_router_model_client.py:759-825 stream
func (c *IntelliRouterModelClient) Stream(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	sopts := model_clients.NewStreamParams(opts...)
	messagesDict, err := c.ConvertMessagesToDict(messages)
	if err != nil {
		return nil, err
	}
	modelName := c.resolveModelName(sopts.Model)
	tools := c.ConvertToolsToDict(sopts.Tools)

	invParams := llmrouter.InvokeParams{
		Tools:       toAnySlice(tools),
		Temperature: c.effectiveTemperature(sopts.Temperature),
		TopP:        c.effectiveTopP(sopts.TopP),
		MaxTokens:   c.effectiveMaxTokens(sopts.MaxTokens),
		Stop:        c.effectiveStop(sopts.Stop),
		Model:       modelName,
		Kwargs:      map[string]any{},
	}
	out := make(chan *llmschema.AssistantMessageChunk)
	chunkChan := c.Router.Stream(ctx, messagesDict, invParams)
	outputEmitted := false
	accumulatedForParser := ""
	go func() {
		defer close(out)
		for result := range chunkChan {
			if result.Err != nil {
				// Python: :796-820 先触发事件再按 output_emitted 翻译
				c.triggerCallError(ctx, modelName, result.Err, true)
				for chunk := range chunkChan { // 排空残余
					_ = chunk
				}
				modelErr := routing.ToModelError(result.Err, routerViewOf(c.Router), modelName, outputEmitted)
				// channel 装不下 error，用 StreamError 哨兵 chunk 承载（对齐 openai 客户端口径）
				out <- llmschema.NewAssistantMessageChunk("", llmschema.WithChunkStreamError(modelErr))
				return
			}
			converted := toOwChunk(result.Chunk)
			if converted.CarriesOutputToken() {
				outputEmitted = true // Python: :785-786
			}
			if sopts.OutputParser != nil && chunkContent(converted) != "" {
				// Python: :787-796 流式解析器增量解析
				accumulatedForParser += chunkContent(converted)
				parsed, perr := sopts.OutputParser.Parse(accumulatedForParser)
				if perr != nil {
					logDebug(fmt.Sprintf("stream parser attempt failed: %s", perr))
				} else if parsed != nil {
					converted.ParserContent = parsed
					accumulatedForParser = ""
				}
			}
			out <- converted
		}
	}()
	return out, nil
}

// GenerateImage 图片生成（DashScope 直调）。Python: :827-851 generate_image
func (c *IntelliRouterModelClient) GenerateImage(ctx context.Context, messages []*llmschema.UserMessage, opts ...model_clients.GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	gopts := model_clients.NewGenerateImageParams(opts...)
	provider := c.resolveGenerationProvider(gopts.Model)
	if !generationSupports(provider, "image") {
		return nil, fmt.Errorf("Provider '%s' does not support image generation. Supported: %v", provider, generationSupport)
	}
	return c.generateImageDashscope(ctx, messages, gopts)
}

// GenerateSpeech 语音生成（DashScope 直调）。Python: :853-871 generate_speech
func (c *IntelliRouterModelClient) GenerateSpeech(ctx context.Context, messages []*llmschema.UserMessage, opts ...model_clients.GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	gopts := model_clients.NewGenerateSpeechParams(opts...)
	provider := c.resolveGenerationProvider(gopts.Model)
	if !generationSupports(provider, "speech") {
		return nil, fmt.Errorf("Provider '%s' does not support speech generation.Supported: %v", provider, generationSupport)
	}
	return c.generateSpeechDashscope(ctx, messages, gopts)
}

// GenerateVideo 视频生成（DashScope 直调）。Python: :873-905 generate_video
func (c *IntelliRouterModelClient) GenerateVideo(ctx context.Context, messages []*llmschema.UserMessage, opts ...model_clients.GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	gopts := model_clients.NewGenerateVideoParams(opts...)
	provider := c.resolveGenerationProvider(gopts.Model)
	if !generationSupports(provider, "video") {
		return nil, fmt.Errorf("Provider '%s' does not support video generation.Supported: %v", provider, generationSupport)
	}
	return c.generateVideoDashscope(ctx, messages, gopts)
}

// TranscribeAudio 音频转写（本客户端不支持）。Python: BaseModelClient 默认。
func (c *IntelliRouterModelClient) TranscribeAudio(ctx context.Context, audioPath string, opts ...llmschema.TranscribeAudioOption) (*llmschema.TranscriptionResponse, error) {
	return nil, exception.BuildError(exception.StatusModelInvokeParamError,
		exception.WithMsg("IntelliRouter client does not support audio transcription"))
}

// Release 释放资源（无持久资源）。Python: 默认 release。
func (c *IntelliRouterModelClient) Release(ctx context.Context, opts ...model_clients.ReleaseOption) (bool, error) {
	return false, fmt.Errorf("IntelliRouter client does not support KV cache release")
}

// SupportsKVCacheRelease 本客户端不支持 KV cache 释放（对齐旧测试口径，
// Python: InferenceAffinityModelClient 才有 release 实现）。
func (c *IntelliRouterModelClient) SupportsKVCacheRelease() bool { return false }

// SupportsKVCacheAffinity 本客户端不支持 Ascend KV cache 亲和。
func (c *IntelliRouterModelClient) SupportsKVCacheAffinity() bool { return false }

// BuildKVCacheAffinityInvokeKwargs 不支持亲和时返回空参数。
func (c *IntelliRouterModelClient) BuildKVCacheAffinityInvokeKwargs(_ any, _, _ string, _ int) map[string]any {
	return map[string]any{}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveModelName 模型名解析（Python: :711/:768 model or
// self.model_config.model_name or "*"）。
func (c *IntelliRouterModelClient) resolveModelName(model string) string {
	if model != "" {
		return model
	}
	if c.ModelConfig != nil && c.ModelConfig.ModelName != "" {
		return c.ModelConfig.ModelName
	}
	return "*"
}

// effectiveTemperature 调用参数优先于模型配置（Python: :645-659
// _build_call_params：None 剔除）。
func (c *IntelliRouterModelClient) effectiveTemperature(v *float64) *float64 {
	if v != nil {
		return v
	}
	return c.ModelConfig.Temperature
}

func (c *IntelliRouterModelClient) effectiveTopP(v *float64) *float64 {
	if v != nil {
		return v
	}
	return c.ModelConfig.TopP
}

func (c *IntelliRouterModelClient) effectiveMaxTokens(v *int) *int {
	if v != nil {
		return v
	}
	return c.ModelConfig.MaxTokens
}

func (c *IntelliRouterModelClient) effectiveStop(v *string) any {
	if v != nil {
		return *v
	}
	if c.ModelConfig.Stop != nil {
		return *c.ModelConfig.Stop
	}
	return nil
}

// triggerCallError 触发 LLM_CALL_ERROR 回调（Python: :729-746/:796-814；
// 触发失败不吞模型错误）。
func (c *IntelliRouterModelClient) triggerCallError(ctx context.Context, modelName string, callErr error, isStream bool) {
	defer func() { _ = recover() }()
	callback.Trigger(ctx, string(callback.LLMCallError), map[string]any{
		"model_name":     modelName,
		"model_provider": c.ClientConfig.ClientProvider,
		"is_stream":      isStream,
		"error":          callErr,
		"error_message":  callErr.Error(),
	})
}

// toOwAssistantMessage intelli_router AssistantMessage → openjiuwen。
// Python: intelli_router_model_client.py:907-961 _to_ow_assistant_message。
func (c *IntelliRouterModelClient) toOwAssistantMessage(msg *intelliroutertypes.AssistantMessage, outputParser model_clients.BaseOutputParser) (*llmschema.AssistantMessage, error) {
	originalContent := msg.Content
	content := originalContent
	parserContent := msg.ParserContent

	if outputParser != nil && content != "" {
		// Python: :917-927 应用 openjiuwen 输出解析器
		parsed, perr := outputParser.Parse(content)
		if perr != nil {
			logError("parse content failed: " + perr.Error())
		} else if parsed != nil {
			parserContent = parsed
			if s, ok := parsed.(string); ok {
				content = s
			} else {
				content = fmt.Sprintf("%v", parsed)
			}
		}
	}

	var toolCalls []*llmschema.ToolCall
	for i := range msg.ToolCalls {
		tc := &msg.ToolCalls[i]
		idx := 0
		if tc.Index != nil {
			idx = *tc.Index
		}
		toolCalls = append(toolCalls, &llmschema.ToolCall{
			ID: tc.ID, Type: tc.Type, Name: tc.Name, Arguments: tc.Arguments, Index: idx,
		})
	}

	finishReason := msg.FinishReason
	if finishReason == "" {
		finishReason = "stop"
	}
	responseID := ""
	if msg.ResponseID != nil {
		responseID = *msg.ResponseID
	}
	responseModel := ""
	if msg.ResponseModel != nil {
		responseModel = *msg.ResponseModel
	}
	reasoningContent := ""
	if msg.ReasoningContent != nil {
		reasoningContent = *msg.ReasoningContent
	}

	// Python: :943-951 provider_content 兜底链
	providerContent := msg.ProviderContent
	if providerContent == nil {
		if outputParser != nil && originalContent != content {
			providerContent = originalContent
		}
	}

	out := &llmschema.AssistantMessage{
		ToolCalls:          toolCalls,
		UsageMetadata:      toOwUsageMetadata(msg.UsageMetadata),
		FinishReason:       finishReason,
		ParserContent:      parserContent,
		ReasoningContent:   reasoningContent,
		PromptTokenIDs:     msg.PromptTokenIDs,
		CompletionTokenIDs: msg.CompletionTokenIDs,
		Logprobs:           msg.Logprobs,
		ResponseID:         responseID,
		ResponseModel:      responseModel,
		ProviderMetadata:   providerMetadata(msg.ProviderMetadata),
		ProviderContent:    providerContent,
	}
	out.SetContent(llmschema.NewTextContent(content))
	return out, nil
}

// toOwChunk intelli_router chunk → openjiuwen chunk（静态方法口径）。
// Python: intelli_router_model_client.py:963-988 _to_ow_chunk
func toOwChunk(chunk intelliroutertypes.AssistantMessageChunk) *llmschema.AssistantMessageChunk {
	var toolCalls []*llmschema.ToolCall
	for i := range chunk.ToolCalls {
		tc := &chunk.ToolCalls[i]
		idx := 0
		if tc.Index != nil {
			idx = *tc.Index
		}
		toolCalls = append(toolCalls, &llmschema.ToolCall{
			ID: tc.ID, Type: tc.Type, Name: tc.Name, Arguments: tc.Arguments, Index: idx,
		})
	}
	finishReason := "null"
	if chunk.FinishReason != nil && *chunk.FinishReason != "" {
		finishReason = *chunk.FinishReason
	}
	responseID := ""
	if chunk.ResponseID != nil {
		responseID = *chunk.ResponseID
	}
	responseModel := ""
	if chunk.ResponseModel != nil {
		responseModel = *chunk.ResponseModel
	}
	reasoningContent := ""
	if chunk.ReasoningContent != nil {
		reasoningContent = *chunk.ReasoningContent
	}
	out := llmschema.NewAssistantMessageChunk(chunk.Content)
	out.ToolCalls = toolCalls
	out.FinishReason = finishReason
	out.UsageMetadata = toOwUsageMetadata(chunk.UsageMetadata)
	out.ParserContent = chunk.ParserContent
	out.ReasoningContent = reasoningContent
	out.PromptTokenIDs = chunk.PromptTokenIDs
	out.CompletionTokenIDs = chunk.CompletionTokenIDs
	out.Logprobs = chunk.Logprobs
	out.ResponseID = responseID
	out.ResponseModel = responseModel
	out.ProviderMetadata = providerMetadata(chunk.ProviderMetadata)
	out.ProviderContent = chunk.ProviderContent
	return out
}

// chunkContent 取 chunk 文本内容（MessageContent → string）。
func chunkContent(c *llmschema.AssistantMessageChunk) string {
	if c == nil {
		return ""
	}
	return c.Content.Text()
}

// toOwUsageMetadata intelli_router 用量 → openjiuwen 用量。
// Python: intelli_router_model_client.py:990-1049 _to_ow_usage_metadata
// （_int_value/_optional_int/_float_value 三类归一；23.4a 类型已归一，
// Go 侧直接字段搬运 + reasoning_tokens 0 → nil 未上报）。
func toOwUsageMetadata(source *intelliroutertypes.UsageMetadata) *llmschema.UsageMetadata {
	if source == nil {
		return nil
	}
	out := llmschema.NewUsageMetadata()
	out.ModelName = derefStr(source.ModelName)
	out.InputTokens = source.InputTokens
	out.OutputTokens = source.OutputTokens
	out.TotalTokens = source.TotalTokens
	out.CacheTokens = source.CacheTokens
	if source.CacheReadTokens != nil {
		out.CacheReadTokens = source.CacheReadTokens
	}
	if source.CacheMissTokens != nil {
		out.CacheMissTokens = source.CacheMissTokens
	}
	if source.CacheWriteTokens != nil {
		out.CacheWriteTokens = source.CacheWriteTokens
	}
	if source.CacheCreationInputTokens != nil {
		out.CacheCreationTokens = source.CacheCreationInputTokens
	}
	out.CacheStatus = derefStr(source.CacheStatus)
	out.CacheSource = derefStr(source.CacheSource)
	out.CacheAuthoritative = source.CacheAuthoritative
	out.InputCost = source.InputCost
	out.OutputCost = source.OutputCost
	if source.ReasoningTokens > 0 {
		// Python: :1033 reasoning_tokens 提取
		n := source.ReasoningTokens
		out.ReasoningTokens = &n
	}
	return out
}

// providerMetadata provider 私有元数据白名单过滤。
// Python: intelli_router_model_client.py:1051-1067 _provider_metadata
func providerMetadata(source map[string]any) map[string]any {
	if source == nil {
		return map[string]any{}
	}
	allowed := []string{"system_fingerprint", "service_tier", "status", "stop_reason", "stop_sequence", "incomplete_details"}
	out := map[string]any{}
	for _, key := range allowed {
		if v, exists := source[key]; exists {
			out[key] = v
		}
	}
	return out
}

// resolveGenerationProvider 生成 API 的 provider 解析。
// Python: intelli_router_model_client.py:1069-1075 _resolve_generation_provider
func (c *IntelliRouterModelClient) resolveGenerationProvider(model string) string {
	for _, dep := range c.Router.Deployments {
		if model != "" && dep.ModelName != model {
			continue
		}
		return dep.Provider
	}
	return "unknown"
}

// apiBaseForProvider 第一个匹配 provider 的 deployment api_base。
// Python: :1084-1093 _get_api_base_for_provider
func (c *IntelliRouterModelClient) apiBaseForProvider(providerName string) string {
	for _, dep := range c.Router.Deployments {
		if dep.Provider == providerName {
			return dep.APIBase
		}
	}
	return ""
}

// apiKeyForProvider 第一个匹配 provider 的 deployment api_key。
// Python: :1077-1082 _get_api_key_for_provider
func (c *IntelliRouterModelClient) apiKeyForProvider(providerName string) string {
	for _, dep := range c.Router.Deployments {
		if dep.Provider == providerName {
			return dep.APIKey
		}
	}
	return ""
}

// derefStr 指针字符串（nil → ""）。
func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// toAnySlice map 列表 → any 列表（router Tools 字段类型 []any）。
func toAnySlice(maps []map[string]any) []any {
	out := make([]any, 0, len(maps))
	for _, m := range maps {
		out = append(out, m)
	}
	return out
}

// routerViewOf ReliableRouter → routing.RouterView 适配。
// （Python 鸭子类型直读；Go 显式适配器，Python 侧无对应类型。）
func routerViewOf(r *llmrouter.ReliableRouter) routing.RouterView {
	return &reliableRouterView{r: r}
}

// reliableRouterView routing.RouterView 实现。
type reliableRouterView struct {
	r *llmrouter.ReliableRouter
}

func (v *reliableRouterView) Deployments() []*intelliroutercore.Deployment { return v.r.Deployments }
func (v *reliableRouterView) ModelGroupID() string                         { return derefStr(v.r.ModelGroupID) }
func (v *reliableRouterView) PinnedRouteTag() string {
	if v.r.Strategy == nil {
		return ""
	}
	if tf, ok := v.r.Strategy.(*strategy.TagFiltered); ok {
		return derefStr(tf.FallbackTag)
	}
	return ""
}
func (v *reliableRouterView) State() *intelliroutercore.LocalRouterState { return v.r.State }

// generationSupport 生成能力支持表。
// Python: intelli_router_model_client.py:219-222 _GENERATION_SUPPORT
// （dashscope 支持 image/speech/video；其余 provider 均不支持）。
var generationSupport = map[string]map[string]struct{}{
	"dashscope": {"image": {}, "speech": {}, "video": {}},
}

// generationSupports provider 是否支持某生成能力。
func generationSupports(providerName, kind string) bool {
	support, ok := generationSupport[providerName]
	if !ok {
		return false
	}
	_, enabled := support[kind]
	return enabled
}

// logWarn/logInfo/logDebug/logError 组件级日志包装。
func logWarn(msg string)  { logger.Warn(logComponent).Msg(msg) }
func logInfo(msg string)  { logger.Info(logComponent).Msg(msg) }
func logDebug(msg string) { logger.Debug(logComponent).Msg(msg) }
func logError(msg string) { logger.Error(logComponent).Msg(msg) }

// urlHost 提取 URL 主机名（非法/无主机时回退原串去协议头）。
func urlHost(apiBase string) string {
	u, err := url.Parse(apiBase)
	if err != nil || u.Host == "" {
		return strings.TrimPrefix(strings.TrimPrefix(apiBase, "https://"), "http://")
	}
	return u.Host
}

// init 注册 IntelliRouter 客户端到全局注册表。
// Python: __client_name__ = "intelli_router"（:441）经 ClientRegistry 注册。
func init() {
	registry := model_clients.GetClientRegistry()
	registry.Register(clientName, "llm", func(mc *llmschema.ModelRequestConfig, cc *llmschema.ModelClientConfig) (model_clients.BaseModelClient, error) {
		client, err := NewIntelliRouterModelClient(mc, cc)
		if err != nil {
			logger.Error(logComponent).Err(err).Msg("create IntelliRouter client failed")
			return nil, err
		}
		return client, nil
	})
}
