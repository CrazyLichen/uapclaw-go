package model_clients

import (
	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// InvokeParams 非流式调用的完整参数。
//
// 对应 Python: BaseModelClient.invoke() 的关键字参数
type InvokeParams struct {
	// Tools 工具列表（支持 ToolInfo 和 McpToolInfo）
	Tools []commonschema.ToolInfoInterface
	// Temperature 温度参数
	Temperature *float64
	// TopP Top-p 采样参数
	TopP *float64
	// Model 模型名称（覆盖 model_config.model_name）
	Model string
	// MaxTokens 最大生成 token 数
	MaxTokens *int
	// Stop 停止序列
	Stop *string
	// OutputParser 输出解析器（2.16 节扩展）
	OutputParser BaseOutputParser
	// Timeout 请求超时时间（秒）
	Timeout *float64
	// Extra 额外参数（对应 Python **kwargs）
	Extra map[string]any
	// CustomHeaders 请求级自定义请求头
	CustomHeaders map[string]string
	// TracerRecordData 追踪记录回调（内部使用）
	TracerRecordData func(map[string]any)
}

// StreamParams 流式调用的完整参数。
//
// 对应 Python: BaseModelClient.stream() 的关键字参数
type StreamParams struct {
	// Tools 工具列表（支持 ToolInfo 和 McpToolInfo）
	Tools []commonschema.ToolInfoInterface
	// Temperature 温度参数
	Temperature *float64
	// TopP Top-p 采样参数
	TopP *float64
	// Model 模型名称（覆盖 model_config.model_name）
	Model string
	// MaxTokens 最大生成 token 数
	MaxTokens *int
	// Stop 停止序列
	Stop *string
	// OutputParser 输出解析器（2.16 节扩展）
	OutputParser BaseOutputParser
	// Timeout 请求超时时间（秒）
	Timeout *float64
	// Extra 额外参数（对应 Python **kwargs）
	Extra map[string]any
	// CustomHeaders 请求级自定义请求头
	CustomHeaders map[string]string
	// TracerRecordData 追踪记录回调（内部使用）
	TracerRecordData func(map[string]any)
}

// GenerateImageParams 图片生成参数。
//
// 对应 Python: BaseModelClient.generate_image() 的关键字参数
type GenerateImageParams struct {
	// Model 模型名称
	Model string
	// Size 图片尺寸，默认 "1664*928"
	Size string
	// NegativePrompt 负向提示词
	NegativePrompt string
	// N 生成数量，默认 1
	N int
	// PromptExtend 是否自动扩展提示词，默认 true
	PromptExtend bool
	// Watermark 是否添加水印
	Watermark bool
	// Seed 随机种子，0 表示随机
	Seed int
	// Timeout 请求超时时间（秒）
	Timeout *float64
	// Extra 额外参数
	Extra map[string]any
}

// GenerateSpeechParams 语音生成参数。
//
// 对应 Python: BaseModelClient.generate_speech() 的关键字参数
type GenerateSpeechParams struct {
	// Model 模型名称
	Model string
	// Voice 语音名称，默认 "Cherry"
	Voice string
	// LanguageType 语言类型，默认 "Auto"
	LanguageType string
	// Timeout 请求超时时间（秒）
	Timeout *float64
	// Extra 额外参数
	Extra map[string]any
}

// GenerateVideoParams 视频生成参数。
//
// 对应 Python: BaseModelClient.generate_video() 的关键字参数
type GenerateVideoParams struct {
	// ImgURL 首帧图片 URL
	ImgURL string
	// AudioURL 音频 URL
	AudioURL string
	// Model 模型名称
	Model string
	// Size 视频尺寸
	Size string
	// Resolution 视频分辨率
	Resolution string
	// Duration 视频时长（秒），默认 5
	Duration int
	// PromptExtend 是否自动扩展提示词，默认 true
	PromptExtend bool
	// Watermark 是否添加水印
	Watermark bool
	// NegativePrompt 负向提示词
	NegativePrompt string
	// Seed 随机种子
	Seed *int
	// Timeout 请求超时时间（秒）
	Timeout *float64
	// Extra 额外参数
	Extra map[string]any
}

// ReleaseParams 释放模型缓存参数（如 vLLM KV Cache）。
//
// 对应 Python: InferenceAffinityModelClient.release() 的关键字参数
type ReleaseParams struct {
	// SessionID 缓存盐值，标识特定的缓存
	SessionID string
	// Messages 消息列表（使用 MessagesParam 统一封装）
	Messages MessagesParam
	// MessagesReleasedIndex 消息释放索引（0-based）
	MessagesReleasedIndex int
	// Model 模型名称（默认使用 model_config.model_name）
	Model string
	// Tools 工具列表（支持 ToolInfo 和 McpToolInfo）
	Tools []commonschema.ToolInfoInterface
	// ToolsReleasedIndex 工具释放索引（0-based，可选）
	ToolsReleasedIndex *int
}

// InvokeOption 非流式调用选项函数。
type InvokeOption func(*InvokeParams)

// StreamOption 流式调用选项函数。
type StreamOption func(*StreamParams)

// GenerateImageOption 图片生成选项函数。
type GenerateImageOption func(*GenerateImageParams)

// GenerateSpeechOption 语音生成选项函数。
type GenerateSpeechOption func(*GenerateSpeechParams)

// GenerateVideoOption 视频生成选项函数。
type GenerateVideoOption func(*GenerateVideoParams)

// ReleaseOption 释放缓存选项函数。
type ReleaseOption func(*ReleaseParams)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewInvokeParams 创建 InvokeParams（默认零值，通过 opts 填充）。
func NewInvokeParams(opts ...InvokeOption) *InvokeParams {
	p := &InvokeParams{}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// NewStreamParams 创建 StreamParams（默认零值，通过 opts 填充）。
func NewStreamParams(opts ...StreamOption) *StreamParams {
	p := &StreamParams{}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// NewGenerateImageParams 创建 GenerateImageParams（含默认值，通过 opts 覆盖）。
func NewGenerateImageParams(opts ...GenerateImageOption) *GenerateImageParams {
	p := &GenerateImageParams{
		Size:         "1664*928",
		N:            1,
		PromptExtend: true,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// NewGenerateSpeechParams 创建 GenerateSpeechParams（含默认值，通过 opts 覆盖）。
func NewGenerateSpeechParams(opts ...GenerateSpeechOption) *GenerateSpeechParams {
	p := &GenerateSpeechParams{
		Voice:        "Cherry",
		LanguageType: "Auto",
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// NewGenerateVideoParams 创建 GenerateVideoParams（含默认值，通过 opts 覆盖）。
func NewGenerateVideoParams(opts ...GenerateVideoOption) *GenerateVideoParams {
	p := &GenerateVideoParams{
		Duration:     5,
		PromptExtend: true,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// NewReleaseParams 创建 ReleaseParams（默认零值，通过 opts 填充）。
func NewReleaseParams(opts ...ReleaseOption) *ReleaseParams {
	p := &ReleaseParams{}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// WithTools 设置工具列表。
func WithTools(tools ...commonschema.ToolInfoInterface) InvokeOption {
	return func(p *InvokeParams) { p.Tools = tools }
}

// WithInvokeTemperature 设置温度参数。
func WithInvokeTemperature(t float64) InvokeOption {
	return func(p *InvokeParams) { p.Temperature = &t }
}

// WithInvokeTopP 设置 Top-p 采样参数。
func WithInvokeTopP(topP float64) InvokeOption {
	return func(p *InvokeParams) { p.TopP = &topP }
}

// WithInvokeModel 设置模型名称。
func WithInvokeModel(m string) InvokeOption {
	return func(p *InvokeParams) { p.Model = m }
}

// WithInvokeMaxTokens 设置最大生成 token 数。
func WithInvokeMaxTokens(n int) InvokeOption {
	return func(p *InvokeParams) { p.MaxTokens = &n }
}

// WithInvokeStop 设置停止序列。
func WithInvokeStop(s string) InvokeOption {
	return func(p *InvokeParams) { p.Stop = &s }
}

// WithInvokeOutputParser 设置输出解析器。
func WithInvokeOutputParser(parser BaseOutputParser) InvokeOption {
	return func(p *InvokeParams) { p.OutputParser = parser }
}

// WithInvokeTimeout 设置请求超时时间（秒）。
func WithInvokeTimeout(t float64) InvokeOption {
	return func(p *InvokeParams) { p.Timeout = &t }
}

// WithInvokeExtra 设置额外参数。
func WithInvokeExtra(extra map[string]any) InvokeOption {
	return func(p *InvokeParams) { p.Extra = extra }
}

// WithInvokeCustomHeaders 设置请求级自定义请求头。
func WithInvokeCustomHeaders(h map[string]string) InvokeOption {
	return func(p *InvokeParams) { p.CustomHeaders = h }
}

// WithInvokeTracerRecordData 设置追踪记录回调。
func WithInvokeTracerRecordData(d func(map[string]any)) InvokeOption {
	return func(p *InvokeParams) { p.TracerRecordData = d }
}

// WithStreamTools 设置工具列表。
func WithStreamTools(tools ...commonschema.ToolInfoInterface) StreamOption {
	return func(p *StreamParams) { p.Tools = tools }
}

// WithStreamTemperature 设置温度参数。
func WithStreamTemperature(t float64) StreamOption {
	return func(p *StreamParams) { p.Temperature = &t }
}

// WithStreamTopP 设置 Top-p 采样参数。
func WithStreamTopP(topP float64) StreamOption {
	return func(p *StreamParams) { p.TopP = &topP }
}

// WithStreamModel 设置模型名称。
func WithStreamModel(m string) StreamOption {
	return func(p *StreamParams) { p.Model = m }
}

// WithStreamMaxTokens 设置最大生成 token 数。
func WithStreamMaxTokens(n int) StreamOption {
	return func(p *StreamParams) { p.MaxTokens = &n }
}

// WithStreamStop 设置停止序列。
func WithStreamStop(s string) StreamOption {
	return func(p *StreamParams) { p.Stop = &s }
}

// WithStreamOutputParser 设置输出解析器。
func WithStreamOutputParser(parser BaseOutputParser) StreamOption {
	return func(p *StreamParams) { p.OutputParser = parser }
}

// WithStreamTimeout 设置请求超时时间（秒）。
func WithStreamTimeout(t float64) StreamOption {
	return func(p *StreamParams) { p.Timeout = &t }
}

// WithStreamExtra 设置额外参数。
func WithStreamExtra(extra map[string]any) StreamOption {
	return func(p *StreamParams) { p.Extra = extra }
}

// WithStreamCustomHeaders 设置请求级自定义请求头。
func WithStreamCustomHeaders(h map[string]string) StreamOption {
	return func(p *StreamParams) { p.CustomHeaders = h }
}

// WithStreamTracerRecordData 设置追踪记录回调。
func WithStreamTracerRecordData(d func(map[string]any)) StreamOption {
	return func(p *StreamParams) { p.TracerRecordData = d }
}

// WithImageModel 设置图片生成模型名称。
func WithImageModel(m string) GenerateImageOption {
	return func(p *GenerateImageParams) { p.Model = m }
}

// WithImageSize 设置图片尺寸。
func WithImageSize(size string) GenerateImageOption {
	return func(p *GenerateImageParams) { p.Size = size }
}

// WithImageNegativePrompt 设置负向提示词。
func WithImageNegativePrompt(np string) GenerateImageOption {
	return func(p *GenerateImageParams) { p.NegativePrompt = np }
}

// WithImageN 设置生成数量。
func WithImageN(n int) GenerateImageOption {
	return func(p *GenerateImageParams) { p.N = n }
}

// WithImagePromptExtend 设置是否自动扩展提示词。
func WithImagePromptExtend(extend bool) GenerateImageOption {
	return func(p *GenerateImageParams) { p.PromptExtend = extend }
}

// WithImageWatermark 设置是否添加水印。
func WithImageWatermark(watermark bool) GenerateImageOption {
	return func(p *GenerateImageParams) { p.Watermark = watermark }
}

// WithImageSeed 设置随机种子。
func WithImageSeed(seed int) GenerateImageOption {
	return func(p *GenerateImageParams) { p.Seed = seed }
}

// WithImageTimeout 设置请求超时时间（秒）。
func WithImageTimeout(t float64) GenerateImageOption {
	return func(p *GenerateImageParams) { p.Timeout = &t }
}

// WithImageExtra 设置额外参数。
func WithImageExtra(extra map[string]any) GenerateImageOption {
	return func(p *GenerateImageParams) { p.Extra = extra }
}

// WithSpeechModel 设置语音生成模型名称。
func WithSpeechModel(m string) GenerateSpeechOption {
	return func(p *GenerateSpeechParams) { p.Model = m }
}

// WithSpeechVoice 设置语音名称。
func WithSpeechVoice(voice string) GenerateSpeechOption {
	return func(p *GenerateSpeechParams) { p.Voice = voice }
}

// WithSpeechLanguageType 设置语言类型。
func WithSpeechLanguageType(lt string) GenerateSpeechOption {
	return func(p *GenerateSpeechParams) { p.LanguageType = lt }
}

// WithSpeechTimeout 设置请求超时时间（秒）。
func WithSpeechTimeout(t float64) GenerateSpeechOption {
	return func(p *GenerateSpeechParams) { p.Timeout = &t }
}

// WithSpeechExtra 设置额外参数。
func WithSpeechExtra(extra map[string]any) GenerateSpeechOption {
	return func(p *GenerateSpeechParams) { p.Extra = extra }
}

// WithVideoImgURL 设置首帧图片 URL。
func WithVideoImgURL(url string) GenerateVideoOption {
	return func(p *GenerateVideoParams) { p.ImgURL = url }
}

// WithVideoAudioURL 设置音频 URL。
func WithVideoAudioURL(url string) GenerateVideoOption {
	return func(p *GenerateVideoParams) { p.AudioURL = url }
}

// WithVideoModel 设置视频生成模型名称。
func WithVideoModel(m string) GenerateVideoOption {
	return func(p *GenerateVideoParams) { p.Model = m }
}

// WithVideoSize 设置视频尺寸。
func WithVideoSize(size string) GenerateVideoOption {
	return func(p *GenerateVideoParams) { p.Size = size }
}

// WithVideoResolution 设置视频分辨率。
func WithVideoResolution(res string) GenerateVideoOption {
	return func(p *GenerateVideoParams) { p.Resolution = res }
}

// WithVideoDuration 设置视频时长（秒）。
func WithVideoDuration(d int) GenerateVideoOption {
	return func(p *GenerateVideoParams) { p.Duration = d }
}

// WithVideoPromptExtend 设置是否自动扩展提示词。
func WithVideoPromptExtend(extend bool) GenerateVideoOption {
	return func(p *GenerateVideoParams) { p.PromptExtend = extend }
}

// WithVideoWatermark 设置是否添加水印。
func WithVideoWatermark(watermark bool) GenerateVideoOption {
	return func(p *GenerateVideoParams) { p.Watermark = watermark }
}

// WithVideoNegativePrompt 设置负向提示词。
func WithVideoNegativePrompt(np string) GenerateVideoOption {
	return func(p *GenerateVideoParams) { p.NegativePrompt = np }
}

// WithVideoSeed 设置随机种子。
func WithVideoSeed(seed int) GenerateVideoOption {
	return func(p *GenerateVideoParams) { p.Seed = &seed }
}

// WithVideoTimeout 设置请求超时时间（秒）。
func WithVideoTimeout(t float64) GenerateVideoOption {
	return func(p *GenerateVideoParams) { p.Timeout = &t }
}

// WithVideoExtra 设置额外参数。
func WithVideoExtra(extra map[string]any) GenerateVideoOption {
	return func(p *GenerateVideoParams) { p.Extra = extra }
}

// WithReleaseSessionID 设置缓存会话 ID。
func WithReleaseSessionID(id string) ReleaseOption {
	return func(p *ReleaseParams) { p.SessionID = id }
}

// WithReleaseMessages 设置消息列表。
func WithReleaseMessages(msgs MessagesParam) ReleaseOption {
	return func(p *ReleaseParams) { p.Messages = msgs }
}

// WithReleaseMessagesIndex 设置消息释放索引（0-based）。
func WithReleaseMessagesIndex(idx int) ReleaseOption {
	return func(p *ReleaseParams) { p.MessagesReleasedIndex = idx }
}

// WithReleaseModel 设置模型名称。
func WithReleaseModel(model string) ReleaseOption {
	return func(p *ReleaseParams) { p.Model = model }
}

// WithReleaseTools 设置工具列表。
func WithReleaseTools(tools ...commonschema.ToolInfoInterface) ReleaseOption {
	return func(p *ReleaseParams) { p.Tools = tools }
}

// WithReleaseToolsIndex 设置工具释放索引（0-based）。
func WithReleaseToolsIndex(idx int) ReleaseOption {
	return func(p *ReleaseParams) { p.ToolsReleasedIndex = &idx }
}

// ToInvokeParams 将 InvokeParams 转换为可用于 BuildRequestParams 的 InvokeParams 指针。
//
// InvokeParams 本身就是目标类型，直接返回指针。
func (p *InvokeParams) ToInvokeParams() *InvokeParams {
	return p
}

// ToStreamParams 将 StreamParams 转换为可用于 BuildRequestParams 的 InvokeParams。
//
// StreamParams 和 InvokeParams 字段完全相同，逐字段拷贝转换。
func (p *StreamParams) ToStreamParams() *InvokeParams {
	return &InvokeParams{
		Tools:            p.Tools,
		Temperature:      p.Temperature,
		TopP:             p.TopP,
		Model:            p.Model,
		MaxTokens:        p.MaxTokens,
		Stop:             p.Stop,
		OutputParser:     p.OutputParser,
		Timeout:          p.Timeout,
		Extra:            p.Extra,
		CustomHeaders:    p.CustomHeaders,
		TracerRecordData: p.TracerRecordData,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────