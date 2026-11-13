package intellirouter

// ──────────────────────────── 结构体 ────────────────────────────

// ToolCall 工具调用信息。← types.py:10 ToolCall
type ToolCall struct {
	// ID 工具调用唯一标识
	ID string `json:"id"`
	// Type 调用类型，默认 "function"
	Type string `json:"type,omitempty"`
	// Name 工具名
	Name string `json:"name,omitempty"`
	// Arguments 参数 JSON 字符串
	Arguments string `json:"arguments,omitempty"`
	// Index 流式增量合并用的序号
	Index *int `json:"index,omitempty"`
}

// UsageMetadata Token 用量、费用和缓存命中信息。← types.py:20 UsageMetadata
//
// Cache 字段在 provider 返回时归一化：CacheTokens/CacheReadTokens 描述
// 复用的 prompt token，CacheWriteTokens/CacheCreationInputTokens 描述新写入
// 的缓存 token，CacheMissTokens 描述未命中缓存的 prompt token。
// CacheStatus 为自由格式状态标签（如 "observed"）；CacheSource 标识缓存值
// 来源（如 "provider_usage"）。CacheAuthoritative 为 true 时调用方可将
// 缓存字段视为 provider 上报的权威值；false 表示缺失、推导或尽力而为。
type UsageMetadata struct {
	// ModelName 模型名
	ModelName *string `json:"model_name,omitempty"`
	// InputTokens 输入 token 数
	InputTokens int `json:"input_tokens"`
	// OutputTokens 输出 token 数
	OutputTokens int `json:"output_tokens"`
	// TotalTokens 总 token 数
	TotalTokens int `json:"total_tokens"`
	// CacheTokens 缓存命中 token 数
	CacheTokens int `json:"cache_tokens"`
	// CacheReadTokens 缓存读取 token 数
	CacheReadTokens *int `json:"cache_read_tokens,omitempty"`
	// CacheMissTokens 缓存未命中 token 数
	CacheMissTokens *int `json:"cache_miss_tokens,omitempty"`
	// CacheWriteTokens 缓存写入 token 数
	CacheWriteTokens *int `json:"cache_write_tokens,omitempty"`
	// CacheStatus 缓存状态标签
	CacheStatus *string `json:"cache_status,omitempty"`
	// CacheSource 缓存值来源
	CacheSource *string `json:"cache_source,omitempty"`
	// CacheAuthoritative 缓存字段是否为 provider 权威上报
	CacheAuthoritative bool `json:"cache_authoritative"`
	// CacheCreationInputTokens 缓存创建输入 token 数
	CacheCreationInputTokens *int `json:"cache_creation_input_tokens,omitempty"`
	// ReasoningTokens 推理 token 数
	ReasoningTokens int `json:"reasoning_tokens"`
	// InputCost 输入费用
	InputCost float64 `json:"input_cost"`
	// OutputCost 输出费用
	OutputCost float64 `json:"output_cost"`
	// TotalCost 总费用
	TotalCost float64 `json:"total_cost"`
}

// AssistantMessage 非流式响应消息。← types.py:57 AssistantMessage
type AssistantMessage struct {
	// Content 文本内容
	Content string `json:"content"`
	// ToolCalls 工具调用列表
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	// UsageMetadata 用量信息
	UsageMetadata *UsageMetadata `json:"usage_metadata,omitempty"`
	// FinishReason 结束原因，默认 "stop"
	FinishReason string `json:"finish_reason"`
	// ReasoningContent 思考内容
	ReasoningContent *string `json:"reasoning_content,omitempty"`
	// ParserContent 解析器产出内容
	ParserContent any `json:"parser_content,omitempty"`
	// PromptTokenIDs prompt token id 列表
	PromptTokenIDs []int `json:"prompt_token_ids,omitempty"`
	// CompletionTokenIDs completion token id 列表
	CompletionTokenIDs []int `json:"completion_token_ids,omitempty"`
	// Logprobs 对数概率
	Logprobs any `json:"logprobs,omitempty"`
	// ResponseID 响应唯一标识
	ResponseID *string `json:"response_id,omitempty"`
	// ResponseModel 响应模型名
	ResponseModel *string `json:"response_model,omitempty"`
	// ProviderMetadata provider 扩展元数据
	ProviderMetadata map[string]any `json:"provider_metadata,omitempty"`
	// ProviderContent provider 原始内容
	ProviderContent any `json:"provider_content,omitempty"`
}

// AssistantMessageChunk 流式响应 chunk。← types.py:75 AssistantMessageChunk
type AssistantMessageChunk struct {
	// Content 增量文本内容
	Content string `json:"content"`
	// ReasoningContent 增量思考内容
	ReasoningContent *string `json:"reasoning_content,omitempty"`
	// ToolCalls 增量工具调用
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	// UsageMetadata 用量信息
	UsageMetadata *UsageMetadata `json:"usage_metadata,omitempty"`
	// FinishReason 结束原因
	FinishReason *string `json:"finish_reason,omitempty"`
	// ParserContent 解析器产出内容
	ParserContent any `json:"parser_content,omitempty"`
	// PromptTokenIDs prompt token id 列表
	PromptTokenIDs []int `json:"prompt_token_ids,omitempty"`
	// CompletionTokenIDs completion token id 列表
	CompletionTokenIDs []int `json:"completion_token_ids,omitempty"`
	// Logprobs 对数概率
	Logprobs any `json:"logprobs,omitempty"`
	// ResponseID 响应唯一标识
	ResponseID *string `json:"response_id,omitempty"`
	// ResponseModel 响应模型名
	ResponseModel *string `json:"response_model,omitempty"`
	// ProviderMetadata provider 扩展元数据
	ProviderMetadata map[string]any `json:"provider_metadata,omitempty"`
	// ProviderContent provider 原始内容
	ProviderContent any `json:"provider_content,omitempty"`
}

// ImageGenerationResponse 文生图 / 图生图响应。← types.py:222 ImageGenerationResponse
type ImageGenerationResponse struct {
	// Model 模型名
	Model string `json:"model"`
	// Images 图片 URL 或 base64 列表
	Images []string `json:"images"`
}

// AudioGenerationResponse 语音合成响应。← types.py:238 AudioGenerationResponse
type AudioGenerationResponse struct {
	// Model 模型名
	Model string `json:"model"`
	// AudioURL 音频 URL
	AudioURL *string `json:"audio_url,omitempty"`
	// AudioData 音频二进制
	AudioData []byte `json:"audio_data,omitempty"`
	// Format 音频格式
	Format *string `json:"format,omitempty"`
}

// VideoGenerationResponse 文生视频 / 图生视频响应。← types.py:247 VideoGenerationResponse
type VideoGenerationResponse struct {
	// Model 模型名
	Model string `json:"model"`
	// VideoURL 视频 URL
	VideoURL string `json:"video_url"`
	// Duration 时长（秒）
	Duration *int `json:"duration,omitempty"`
	// Resolution 分辨率
	Resolution *string `json:"resolution,omitempty"`
	// Format 视频格式，默认 "mp4"
	Format string `json:"format"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewUsageMetadata 构造 UsageMetadata，等价 types.py:41 __post_init__：
// total_tokens 未显式给出且 input/output 任一非零时按两者之和派生。
func NewUsageMetadata(m UsageMetadata) UsageMetadata {
	if m.TotalTokens == 0 && (m.InputTokens != 0 || m.OutputTokens != 0) {
		m.TotalTokens = m.InputTokens + m.OutputTokens
	}
	return m
}

// Plus 拼接两个 chunk（用于流式累积）。← types.py:103 AssistantMessageChunk.__add__
func (c AssistantMessageChunk) Plus(other AssistantMessageChunk) AssistantMessageChunk {
	mergedToolCalls := mergeToolCalls(c.ToolCalls, other.ToolCalls)
	var reasoning *string
	if c.ReasoningContent != nil || other.ReasoningContent != nil {
		// ← py:104 (self.reasoning_content or "") + (other.reasoning_content or "")
		merged := derefOrEmpty(c.ReasoningContent) + derefOrEmpty(other.ReasoningContent)
		reasoning = &merged
	}
	var completionIDs []int
	// ← py:110 (self.completion_token_ids or []) + (other.completion_token_ids or []) or None
	completionIDs = append(completionIDs, c.CompletionTokenIDs...)
	completionIDs = append(completionIDs, other.CompletionTokenIDs...)
	if len(completionIDs) == 0 {
		completionIDs = nil
	}
	mergedProviderMetadata := make(map[string]any, len(c.ProviderMetadata)+len(other.ProviderMetadata))
	for k, v := range c.ProviderMetadata {
		mergedProviderMetadata[k] = v
	}
	for k, v := range other.ProviderMetadata {
		mergedProviderMetadata[k] = v
	}
	var finish *string
	if other.FinishReason != nil {
		finish = other.FinishReason
	} else {
		finish = c.FinishReason
	}
	return AssistantMessageChunk{
		Content:            c.Content + other.Content,
		ReasoningContent:   reasoning,
		ToolCalls:          mergedToolCalls,
		UsageMetadata:      mergeUsageMetadata(c.UsageMetadata, other.UsageMetadata),
		FinishReason:       finish,
		ParserContent:      pick(other.ParserContent, c.ParserContent),
		PromptTokenIDs:     pickSlice(c.PromptTokenIDs, other.PromptTokenIDs),
		CompletionTokenIDs: completionIDs,
		Logprobs:           pick(other.Logprobs, c.Logprobs),
		ResponseID:         pickString(other.ResponseID, c.ResponseID),
		ResponseModel:      pickString(other.ResponseModel, c.ResponseModel),
		ProviderMetadata:   mergedProviderMetadata,
		ProviderContent:    pick(other.ProviderContent, c.ProviderContent),
	}
}

// MergeToolCalls 按 index 合并 tool_calls，拼接 arguments 字符串。
// ← types.py:121 AssistantMessageChunk._merge_tool_calls（Python 静态方法 →
// 导出函数：23.4b 客户端包装层同样按 index 聚合流式 tool_call，需要复用）
//
// 不修改任何传入的列表和 ToolCall 对象：所有条目（含 existing 中的）都以新
// 实例返回，保证流式累积（尤其是重复累加或复用 chunk）时不被改写。
func MergeToolCalls(existing, incoming []ToolCall) []ToolCall {
	return mergeToolCalls(existing, incoming)
}

// MergeUsageMetadata 字段级合并两个 UsageMetadata（用于流式累积）。
// ← types.py:159 AssistantMessageChunk._merge_usage_metadata（导出理由同上）
//
// 流式场景下不同阶段上报用量的不同字段（如 Anthropic message_start 携带
// input/cache 统计、message_delta 只携带最终 output_tokens），整体覆盖会丢
// 失先到的字段，因此逐字段合并，且不修改传入的两个对象：
//   - 数值字段：0/None 视为未上报，incoming 非零则取 incoming，否则保留 existing
//   - 字符串字段：None 视为未上报，取 incoming 非 None 的值，否则保留 existing
//   - CacheAuthoritative：一端为 true 即保留 true
//   - TotalTokens：派生字段，input/output 合并后按两者之和重算
func MergeUsageMetadata(existing, incoming *UsageMetadata) *UsageMetadata {
	return mergeUsageMetadata(existing, incoming)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// mergeToolCalls mergeToolCalls 的实现体。← types.py:121-157
func mergeToolCalls(existing, incoming []ToolCall) []ToolCall {
	if existing == nil && incoming == nil {
		return nil
	}
	if incoming == nil {
		result := make([]ToolCall, 0, len(existing))
		for _, tc := range existing {
			result = append(result, copyToolCall(tc))
		}
		return result
	}
	if existing == nil {
		result := make([]ToolCall, 0, len(incoming))
		for _, tc := range incoming {
			result = append(result, copyToolCall(tc))
		}
		return result
	}

	// existing 中的 ToolCall 先拷贝成新实例，后续合并只改写拷贝 ← py:133-139
	byIndex := make(map[int]*ToolCall)
	result := make([]ToolCall, 0, len(existing)+len(incoming))
	for _, tc := range existing {
		copied := copyToolCall(tc)
		result = append(result, copied)
		if copied.Index != nil {
			byIndex[*copied.Index] = &result[len(result)-1]
		}
	}

	for _, tc := range incoming {
		idx := tc.Index
		if idx != nil {
			if target, ok := byIndex[*idx]; ok {
				// 合并到已有条目的拷贝上 ← py:143-148
				if tc.ID != "" {
					target.ID = tc.ID
				}
				if tc.Name != "" {
					target.Name = tc.Name
				}
				target.Arguments += tc.Arguments
				continue
			}
		}
		// 新的工具调用 ← py:150-155
		newTC := copyToolCall(tc)
		result = append(result, newTC)
		if idx != nil {
			byIndex[*idx] = &result[len(result)-1]
		}
	}
	return result
}

// mergeUsageMetadata mergeUsageMetadata 的实现体。← types.py:159-217
func mergeUsageMetadata(existing, incoming *UsageMetadata) *UsageMetadata {
	if existing == nil {
		return incoming
	}
	if incoming == nil {
		return existing
	}

	// ← py:172 _newest_number：incoming None/0 视为未上报取 existing
	newestInt := func(inc, ext int) int {
		if inc == 0 {
			return ext
		}
		return inc
	}
	newestPtr := func(inc, ext *int) *int {
		if inc == nil || *inc == 0 {
			return ext
		}
		return inc
	}
	newestFloat := func(inc, ext float64) float64 {
		if inc == 0 {
			return ext
		}
		return inc
	}
	// ← py:182/184/185 model_name/cache_status/cache_source 用 is not None
	// 判定（空串也视为已上报），与 Plus 的 or 语义不同，单独收口
	newestString := func(inc, ext *string) *string {
		if inc != nil {
			return inc
		}
		return ext
	}

	inputTokens := newestInt(incoming.InputTokens, existing.InputTokens)
	outputTokens := newestInt(incoming.OutputTokens, existing.OutputTokens)

	totalTokens := newestInt(incoming.TotalTokens, existing.TotalTokens)
	if inputTokens != 0 || outputTokens != 0 {
		// ← py:177-178 派生字段重算
		totalTokens = inputTokens + outputTokens
	}

	return &UsageMetadata{
		ModelName:                newestString(incoming.ModelName, existing.ModelName),
		InputTokens:              inputTokens,
		OutputTokens:             outputTokens,
		TotalTokens:              totalTokens,
		CacheTokens:              newestInt(incoming.CacheTokens, existing.CacheTokens),
		CacheReadTokens:          newestPtr(incoming.CacheReadTokens, existing.CacheReadTokens),
		CacheMissTokens:          newestPtr(incoming.CacheMissTokens, existing.CacheMissTokens),
		CacheWriteTokens:         newestPtr(incoming.CacheWriteTokens, existing.CacheWriteTokens),
		CacheStatus:              newestString(incoming.CacheStatus, existing.CacheStatus),
		CacheSource:              newestString(incoming.CacheSource, existing.CacheSource),
		CacheAuthoritative:       existing.CacheAuthoritative || incoming.CacheAuthoritative,
		CacheCreationInputTokens: newestPtr(incoming.CacheCreationInputTokens, existing.CacheCreationInputTokens),
		ReasoningTokens:          newestInt(incoming.ReasoningTokens, existing.ReasoningTokens),
		InputCost:                newestFloat(incoming.InputCost, existing.InputCost),
		OutputCost:               newestFloat(incoming.OutputCost, existing.OutputCost),
		TotalCost:                newestFloat(incoming.TotalCost, existing.TotalCost),
	}
}

// copyToolCall 生成 ToolCall 新实例。← types.py:125/129/150 三处同构拷贝
func copyToolCall(tc ToolCall) ToolCall {
	copied := tc
	return copied
}

// derefOrEmpty 指针空值转空串。← py:104 `(x or "")` 语义
func derefOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// pick other 非 nil 取 other，否则取 fallback。← py:105/108/111 `x if x is not None else y` 语义
func pick(other, fallback any) any {
	if other != nil {
		return other
	}
	return fallback
}

// pickString 同 pick 的字符串版。← py:113/114 `other or self` 语义
func pickString(other, fallback *string) *string {
	if other != nil && *other != "" {
		return other
	}
	return fallback
}

// pickSlice 同 pick 的切片版。← py:109 `self.x or other.x` 语义
func pickSlice(other, fallback []int) []int {
	if len(other) > 0 {
		return other
	}
	return fallback
}
