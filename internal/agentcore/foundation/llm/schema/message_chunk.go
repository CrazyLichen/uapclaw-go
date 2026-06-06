package schema

// ──────────────────────────── 结构体 ────────────────────────────

// AssistantMessageChunk 助手流式消息块，用于 SSE 流式场景中增量合并 LLM 响应片段。
//
// 设计要点：
//   - 嵌入 AssistantMessage，复用其全部字段（BaseMessage + 扩展字段）
//   - 提供 Merge 方法替代 Python 的 __add__ 运算符，实现流式增量合并
//   - 不可变语义：Merge 返回新对象，不修改接收者
//   - 流式场景下，多个 chunk 通过 Merge 逐步合并为一个完整的 AssistantMessage
//
// 对应 Python: openjiuwen/core/foundation/llm/schema/message_chunk.py (AssistantMessageChunk)
type AssistantMessageChunk struct {
	AssistantMessage
}

// ToolMessageChunk 工具返回流式消息块，用于 SSE 流式场景中增量合并工具响应片段。
//
// 对应 Python: openjiuwen/core/foundation/llm/schema/message_chunk.py (ToolMessageChunk)
type ToolMessageChunk struct {
	ToolMessage
}

// ──────────────────────────── 导出函数 ────────────────────────────

// AssistantMessageChunkOption AssistantMessageChunk 构造选项函数。
type AssistantMessageChunkOption func(*AssistantMessageChunk)

// WithChunkToolCalls 设置流式消息块的工具调用列表。
func WithChunkToolCalls(calls []*ToolCall) AssistantMessageChunkOption {
	return func(c *AssistantMessageChunk) { c.ToolCalls = calls }
}

// WithChunkUsageMetadata 设置流式消息块的用量元数据。
func WithChunkUsageMetadata(meta *UsageMetadata) AssistantMessageChunkOption {
	return func(c *AssistantMessageChunk) { c.UsageMetadata = meta }
}

// WithChunkFinishReason 设置流式消息块的完成原因。
func WithChunkFinishReason(reason string) AssistantMessageChunkOption {
	return func(c *AssistantMessageChunk) { c.FinishReason = reason }
}

// WithChunkParserContent 设置流式消息块的解析器内容。
func WithChunkParserContent(content any) AssistantMessageChunkOption {
	return func(c *AssistantMessageChunk) { c.ParserContent = content }
}

// WithChunkReasoningContent 设置流式消息块的推理内容。
func WithChunkReasoningContent(content string) AssistantMessageChunkOption {
	return func(c *AssistantMessageChunk) { c.ReasoningContent = content }
}

// WithChunkPromptTokenIDs 设置流式消息块的输入 token ID 列表。
func WithChunkPromptTokenIDs(ids []int) AssistantMessageChunkOption {
	return func(c *AssistantMessageChunk) { c.PromptTokenIDs = ids }
}

// WithChunkCompletionTokenIDs 设置流式消息块的输出 token ID 列表。
func WithChunkCompletionTokenIDs(ids []int) AssistantMessageChunkOption {
	return func(c *AssistantMessageChunk) { c.CompletionTokenIDs = ids }
}

// WithChunkLogprobs 设置流式消息块的对数概率信息。
func WithChunkLogprobs(logprobs any) AssistantMessageChunkOption {
	return func(c *AssistantMessageChunk) { c.Logprobs = logprobs }
}

// NewAssistantMessageChunk 创建助手流式消息块。
//
// 默认值：
//   - role: RoleTypeAssistant
//   - finish_reason: "null"
//   - content: 空文本
//
// 对应 Python: AssistantMessageChunk(role=..., content=..., ...)
func NewAssistantMessageChunk(content string, opts ...AssistantMessageChunkOption) *AssistantMessageChunk {
	chunk := &AssistantMessageChunk{
		AssistantMessage: AssistantMessage{
			BaseMessage: BaseMessage{
				Role:    RoleTypeAssistant,
				Content: NewTextContent(content),
			},
			FinishReason: FinishReasonNull,
		},
	}
	for _, opt := range opts {
		opt(chunk)
	}
	return chunk
}

// ToolMessageChunkOption ToolMessageChunk 构造选项函数。
type ToolMessageChunkOption func(*ToolMessageChunk)

// NewToolMessageChunk 创建工具返回流式消息块。
//
// 对应 Python: ToolMessageChunk(role="tool", content=..., tool_call_id=...)
func NewToolMessageChunk(toolCallID, content string, opts ...ToolMessageChunkOption) *ToolMessageChunk {
	chunk := &ToolMessageChunk{
		ToolMessage: ToolMessage{
			BaseMessage: BaseMessage{
				Role:    RoleTypeTool,
				Content: NewTextContent(content),
			},
			ToolCallID: toolCallID,
		},
	}
	for _, opt := range opts {
		opt(chunk)
	}
	return chunk
}

// Merge 将另一个 AssistantMessageChunk 增量合并到当前 chunk，返回合并后的新 chunk。
//
// 该方法不修改接收者（不可变语义），而是返回新创建的 AssistantMessageChunk。
// 这与 Python 中 __add__ 返回新对象的语义一致。
//
// 各字段合并策略：
//   - content: 纯文本拼接 / 多模态拼接 / 类型不同取右侧
//   - tool_calls: 增量拼接同一调用的片段（最核心逻辑）
//   - finish_reason: 右侧非 "null" 时取右侧
//   - usage_metadata: 取非空侧，优先右侧
//   - parser_content: 智能合并（字符串拼接/列表拼接/字典递归合并）
//   - reasoning_content: 字符串拼接
//   - prompt_token_ids: 取非空侧（仅首个 chunk 携带）
//   - completion_token_ids: 列表拼接（增量 delta）
//   - logprobs: 字典/列表合并
//
// 对应 Python: AssistantMessageChunk.__add__
func (c *AssistantMessageChunk) Merge(other *AssistantMessageChunk) *AssistantMessageChunk {
	if other == nil {
		return c
	}
	if c == nil {
		return other
	}

	// content 合并
	mergedContent := mergeContent(c.Content, other.Content)

	// tool_calls 增量合并
	mergedToolCalls := mergeToolCalls(c.ToolCalls, other.ToolCalls)

	// finish_reason: 右侧非 "null" 时取右侧
	mergedFinishReason := c.FinishReason
	if other.FinishReason != FinishReasonNull {
		mergedFinishReason = other.FinishReason
	}

	// usage_metadata: 取非空侧，优先右侧
	mergedUsageMetadata := c.UsageMetadata
	if other.UsageMetadata != nil {
		mergedUsageMetadata = other.UsageMetadata
	}

	// parser_content: 智能合并
	mergedParserContent := mergeParserContent(c.ParserContent, other.ParserContent)

	// reasoning_content: 字符串拼接
	mergedReasoningContent := c.ReasoningContent + other.ReasoningContent

	// prompt_token_ids: 取非空侧，优先右侧
	mergedPromptTokenIDs := c.PromptTokenIDs
	if len(other.PromptTokenIDs) > 0 {
		mergedPromptTokenIDs = other.PromptTokenIDs
	}

	// completion_token_ids: 列表拼接
	mergedCompletionTokenIDs := concatTokenIDs(c.CompletionTokenIDs, other.CompletionTokenIDs)

	// logprobs: 字典/列表合并
	mergedLogprobs := mergeLogprobs(c.Logprobs, other.Logprobs)

	// name: 取非空，优先左侧
	mergedName := c.Name
	if mergedName == "" {
		mergedName = other.Name
	}

	// metadata: 右侧非空时取右侧，否则取左侧
	mergedMetadata := c.Metadata
	if len(other.Metadata) > 0 {
		mergedMetadata = other.Metadata
	}

	return &AssistantMessageChunk{
		AssistantMessage: AssistantMessage{
			BaseMessage: BaseMessage{
				Role:     c.Role,
				Content:  mergedContent,
				Name:     mergedName,
				Metadata: mergedMetadata,
			},
			ToolCalls:          mergedToolCalls,
			UsageMetadata:      mergedUsageMetadata,
			FinishReason:       mergedFinishReason,
			ParserContent:      mergedParserContent,
			ReasoningContent:   mergedReasoningContent,
			PromptTokenIDs:     mergedPromptTokenIDs,
			CompletionTokenIDs: mergedCompletionTokenIDs,
			Logprobs:           mergedLogprobs,
		},
	}
}

// ToAssistantMessage 将流式消息块转换为完整的助手消息。
//
// 流式合并完成后，调用此方法获取最终的 AssistantMessage。
// 转换为值拷贝，后续对 chunk 的修改不影响返回的 AssistantMessage。
func (c *AssistantMessageChunk) ToAssistantMessage() *AssistantMessage {
	if c == nil {
		return nil
	}
	result := c.AssistantMessage
	return &result
}

// Merge 将另一个 ToolMessageChunk 增量合并到当前 chunk，返回合并后的新 chunk。
//
// 各字段合并策略：
//   - content: 字符串拼接
//   - tool_call_id: 取非空值，优先右侧
//
// 对应 Python: ToolMessageChunk.__add__
func (c *ToolMessageChunk) Merge(other *ToolMessageChunk) *ToolMessageChunk {
	if other == nil {
		return c
	}
	if c == nil {
		return other
	}

	// content: 字符串拼接
	mergedContent := c.Content.Text() + other.Content.Text()

	// tool_call_id: 取非空值，优先右侧
	mergedToolCallID := c.ToolCallID
	if other.ToolCallID != "" {
		mergedToolCallID = other.ToolCallID
	}

	return &ToolMessageChunk{
		ToolMessage: ToolMessage{
			BaseMessage: BaseMessage{
				Role:    c.Role,
				Content: NewTextContent(mergedContent),
			},
			ToolCallID: mergedToolCallID,
		},
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// mergeToolCalls 合并两个 ToolCall 切片，增量拼接同一调用的片段。
//
// 判断"同一调用"的条件（满足以下全部）：
//   1. left 的最后一个 ToolCall 与 incoming 的 ToolCall 的 id 相同，
//      或者某一方 id 为空（流式中间 chunk 可能不携带 id）
//   2. 两者的 type 均为 "function"
//
// 同一调用合并规则：
//   - id: 取非空值
//   - type: 取非空值
//   - name: 左侧非空取左侧，否则取右侧；若仍为空则取 ""
//   - arguments: 字符串拼接
//   - index: 取左侧值
//
// 不同调用：作为新元素追加到列表末尾。
//
// 对应 Python: AssistantMessageChunk.__add__ 中 tool_calls 合并逻辑
func mergeToolCalls(left, right []*ToolCall) []*ToolCall {
	if len(left) == 0 && len(right) == 0 {
		return nil
	}
	if len(left) == 0 {
		return right
	}
	if len(right) == 0 {
		return left
	}

	// 深拷贝 left 的 ToolCall，避免与原始切片共享引用
	merged := make([]*ToolCall, len(left))
	for i, tc := range left {
		tcCopy := *tc
		merged[i] = &tcCopy
	}

	// 遍历 right，判断是否与最后一个元素为同一调用
	for _, incoming := range right {
		if len(merged) > 0 {
			last := merged[len(merged)-1]
			// 判断是否同一调用：id 相同（或某一方为空） + type 均为 function
			sameID := (last.ID != "" && incoming.ID != "" && last.ID == incoming.ID) ||
				last.ID == "" || incoming.ID == ""
			if sameID && last.Type == "function" && incoming.Type == "function" {
				// 合并到现有调用
				mergedID := last.ID
				if mergedID == "" {
					mergedID = incoming.ID
				}
				mergedType := last.Type
				if mergedType == "" {
					mergedType = incoming.Type
				}
				mergedName := last.Name
				if mergedName == "" {
					mergedName = incoming.Name
				}
				merged[len(merged)-1] = &ToolCall{
					ID:        mergedID,
					Type:      mergedType,
					Name:      mergedName,
					Arguments: last.Arguments + incoming.Arguments,
					Index:     last.Index,
				}
				continue
			}
		}
		// 不同调用，追加为新元素
		merged = append(merged, incoming)
	}

	return merged
}

// mergeContent 合并两个 MessageContent。
//
// 合并策略：
//   - 两者均为纯文本 → 字符串拼接，结果仍为纯文本
//   - 两者均为多模态 → Parts 列表拼接，结果仍为多模态
//   - 类型不同或一方为空文本 → 取右侧（other）的值
//
// 对应 Python: isinstance(self.content, str) and isinstance(other.content, str) 等
func mergeContent(left, right MessageContent) MessageContent {
	if left.IsText() && right.IsText() {
		// 两者均为纯文本：字符串拼接
		return NewTextContent(left.Text() + right.Text())
	}
	if !left.IsText() && !right.IsText() {
		// 两者均为多模态：Parts 拼接
		mergedParts := make([]ContentPart, 0, len(left.Parts())+len(right.Parts()))
		mergedParts = append(mergedParts, left.Parts()...)
		mergedParts = append(mergedParts, right.Parts()...)
		return NewMultiModalContent(mergedParts...)
	}
	// 类型不同：取右侧
	return right
}

// mergeParserContent 智能合并 parser_content 字段。
//
// 合并策略：
//   - right 为 nil → 返回 left
//   - left 为 nil → 返回 right
//   - 两者均为 string → 字符串拼接
//   - 两者均为 []any → 列表拼接
//   - 两者均为 map[string]any → 递归合并字典
//   - 其他情况 → 返回 right（取右侧值）
//
// 注意：Go 中不支持 Python 的 __add__ 方法和 Pydantic Model 递归合并，
// 因此省略了 Python 中对自定义对象和 Pydantic Model 的处理分支。
// 这比 Python 实际使用的 `other.parser_content or self.parser_content` 更正确，
// 能正确处理流式场景中多次解析成功的情况（如 dict 递归合并、列表拼接）。
//
// 对应 Python: merge_parser_content()
func mergeParserContent(left, right any) any {
	if right == nil {
		return left
	}
	if left == nil {
		return right
	}

	// 两者均为 string → 字符串拼接
	leftStr, leftIsStr := left.(string)
	rightStr, rightIsStr := right.(string)
	if leftIsStr && rightIsStr {
		return leftStr + rightStr
	}

	// 两者均为 []any → 列表拼接
	leftSlice, leftIsSlice := left.([]any)
	rightSlice, rightIsSlice := right.([]any)
	if leftIsSlice && rightIsSlice {
		merged := make([]any, 0, len(leftSlice)+len(rightSlice))
		merged = append(merged, leftSlice...)
		merged = append(merged, rightSlice...)
		return merged
	}

	// 两者均为 map[string]any → 递归合并字典
	leftMap, leftIsMap := left.(map[string]any)
	rightMap, rightIsMap := right.(map[string]any)
	if leftIsMap && rightIsMap {
		return mergeDicts(leftMap, rightMap)
	}

	// 其他情况：取右侧值
	return right
}

// mergeDicts 递归合并两个 map[string]any。
//
// 对每个键：
//   - 两侧均为 string → 字符串拼接
//   - 两侧均为 []any → 列表拼接
//   - 两侧均为 map[string]any → 递归合并
//   - 其他 → 取右侧值
//
// 对应 Python: merge_dicts()
func mergeDicts(left, right map[string]any) map[string]any {
	result := make(map[string]any, len(left))
	// 先拷贝左侧所有键值
	for k, v := range left {
		result[k] = v
	}

	// 合并右侧键值
	for k, rightVal := range right {
		leftVal, exists := result[k]
		if !exists {
			result[k] = rightVal
			continue
		}

		// 两侧均为 string → 字符串拼接
		leftStr, leftIsStr := leftVal.(string)
		rightStr, rightIsStr := rightVal.(string)
		if leftIsStr && rightIsStr {
			result[k] = leftStr + rightStr
			continue
		}

		// 两侧均为 []any → 列表拼接
		leftSlice, leftIsSlice := leftVal.([]any)
		rightSlice, rightIsSlice := rightVal.([]any)
		if leftIsSlice && rightIsSlice {
			merged := make([]any, 0, len(leftSlice)+len(rightSlice))
			merged = append(merged, leftSlice...)
			merged = append(merged, rightSlice...)
			result[k] = merged
			continue
		}

		// 两侧均为 map[string]any → 递归合并
		leftMap, leftIsMap := leftVal.(map[string]any)
		rightMap, rightIsMap := rightVal.(map[string]any)
		if leftIsMap && rightIsMap {
			result[k] = mergeDicts(leftMap, rightMap)
			continue
		}

		// 其他情况：取右侧值
		result[k] = rightVal
	}

	return result
}

// concatTokenIDs 拼接流式 completion_token_ids 增量。
//
// 两者均为 []int → 列表拼接
// 一方为空 → 取非空侧
//
// 对应 Python: _concat_token_ids()
func concatTokenIDs(left, right []int) []int {
	if len(left) == 0 {
		return right
	}
	if len(right) == 0 {
		return left
	}
	merged := make([]int, 0, len(left)+len(right))
	merged = append(merged, left...)
	merged = append(merged, right...)
	return merged
}

// mergeLogprobs 合并流式 logprobs 增量。
//
// 合并策略：
//   - left 为 nil → 返回 right
//   - right 为 nil → 返回 left
//   - 两者均为 map[string]any → 对每个键：若两侧均为 []any 则列表拼接，否则取右侧值
//   - 两者均为 []any → 列表拼接
//   - 其他情况 → 取右侧值
//
// 对应 Python: _merge_logprobs()
func mergeLogprobs(left, right any) any {
	if left == nil {
		return right
	}
	if right == nil {
		return left
	}

	// 两者均为 map[string]any → 逐键合并
	leftMap, leftIsMap := left.(map[string]any)
	rightMap, rightIsMap := right.(map[string]any)
	if leftIsMap && rightIsMap {
		result := make(map[string]any, len(leftMap))
		for k, v := range leftMap {
			result[k] = v
		}
		for k, rightVal := range rightMap {
			leftVal, exists := result[k]
			if !exists {
				result[k] = rightVal
				continue
			}
			// 同键：若两侧均为 []any 则列表拼接，否则取右侧
			leftSlice, leftIsSlice := leftVal.([]any)
			rightSlice, rightIsSlice := rightVal.([]any)
			if leftIsSlice && rightIsSlice {
				merged := make([]any, 0, len(leftSlice)+len(rightSlice))
				merged = append(merged, leftSlice...)
				merged = append(merged, rightSlice...)
				result[k] = merged
			} else {
				result[k] = rightVal
			}
		}
		return result
	}

	// 两者均为 []any → 列表拼接
	leftSlice, leftIsSlice := left.([]any)
	rightSlice, rightIsSlice := right.([]any)
	if leftIsSlice && rightIsSlice {
		merged := make([]any, 0, len(leftSlice)+len(rightSlice))
		merged = append(merged, leftSlice...)
		merged = append(merged, rightSlice...)
		return merged
	}

	// 其他情况：取右侧值
	return right
}
