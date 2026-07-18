package compressor

import (
	"context"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine"
	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/processor"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/output_parsers"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DialogueCompressorConfig 对话压缩器配置。
//
// 对应 Python: DialogueCompressorConfig (pydantic.BaseModel)
type DialogueCompressorConfig struct {
	// MessagesThreshold 消息数触发阈值，0 表示不启用
	MessagesThreshold int
	// TokensThreshold Token 数触发阈值
	TokensThreshold int
	// MessagesToKeep 保留最近 N 条不压缩，0 表示不保留
	MessagesToKeep int
	// KeepLastRound 保留最后一轮完整对话
	KeepLastRound bool
	// CompressionTargetTokens 每块摘要目标 Token 数
	CompressionTargetTokens int
	// CustomCompressionPrompt 自定义压缩提示词，空字符串表示使用内置提示词
	CustomCompressionPrompt string
	// Model 压缩模型请求配置
	Model *llm_schema.ModelRequestConfig
	// ModelClient 压缩模型客户端配置
	ModelClient *llm_schema.ModelClientConfig
}

// compressTarget 压缩目标，对应 Python _CompressTarget
type compressTarget struct {
	// blockID 块标识（如 react_1）
	blockID string
	// userIDx 锚点 UserMessage 索引
	userIDx int
	// startIDx 替换范围起始索引
	startIDx int
	// endIDx 替换范围结束索引
	endIDx int
	// messages 目标块内的消息列表
	messages []llm_schema.BaseMessage
}

// dialogueRound 对话轮次，对应 Python _DialogueRound
type dialogueRound struct {
	// userIDx 锚点 UserMessage 索引
	userIDx int
	// startIDx 轮次起始索引
	startIDx int
	// endIDx 轮次结束索引
	endIDx int
	// messages 轮次内的消息列表
	messages []llm_schema.BaseMessage
	// blockMessageCount 轮次消息数量
	blockMessageCount int
}

// DialogueCompressor 对话压缩器，将已完成的 ReAct 对话轮次压缩为摘要消息。
//
// 当上下文消息超过阈值（消息数量或 Token 数）时，识别完整的对话轮次，
// 调用 LLM 生成压缩摘要，替换原始消息以减少 Token 消耗。
//
// 对应 Python: openjiuwen/core/context_engine/processor/compressor/dialogue_compressor.py (DialogueCompressor)
type DialogueCompressor struct {
	*processor.BaseProcessor
	// model 压缩用 LLM 实例
	model *llm.Model
	// tokenThreshold Token 触发阈值
	tokenThreshold int
	// messageNumThreshold 消息数触发阈值（0 表示不启用）
	messageNumThreshold int
	// messagesToKeep 保留最近 N 条不压缩（0 表示不保留）
	messagesToKeep int
	// keepLastRound 保留最后一轮完整对话
	keepLastRound bool
	// compressionTargetTokens 每块摘要目标 Token 数
	compressionTargetTokens int
	// compressedPrompt 压缩提示词
	compressedPrompt string
}

// DialogueCompressorOption DialogueCompressor 构造选项函数。
type DialogueCompressorOption func(*DialogueCompressor)

// ──────────────────────────── 常量 ────────────────────────────

// dialogueMemoryBlockMarker 对话记忆块标记
const dialogueMemoryBlockMarker = "[DIALOGUE_MEMORY_BLOCK]"

// defaultCompressionPrompt 内置压缩提示词，与 Python DEFAULT_COMPRESSION_PROMPT 对齐
const defaultCompressionPrompt = `You are a **Task Data Preservation Expert** focused on compressing historical ReAct blocks with high fidelity.

Your output will replace only the explicitly listed target ReAct blocks.

## COMPRESSION RESPONSIBILITY

- Preserve the information most useful for correctly completing and continuing the task.
- Retain both action continuity and task-critical factual basis.
- Keep unresolved work, handoff state, decisions, constraints, corrections, key findings, and important tool results.
- Preserve the user's original requirements, constraints, acceptance criteria, and preferences as completely as possible.
- Preserve the model's final result, final answer, or completed outcome for each finished block.
- Do not weaken or over-compress the user's original request unless absolutely necessary.

## INPUT BOUNDARIES

- You will receive the full conversation context so you can understand the global task.
- You will also receive a separate list of compression targets.
- Compress ONLY the listed target blocks.
- Do NOT rewrite non-target messages.
- Treat non-target messages as reference context only.

## INFORMATION PRIORITY

Preserve information in this order:
1. Task goals and user intent
2. Critical factual basis for correct continuation
3. Open work / unfinished work
4. Handoff state at the block boundary
5. Key decisions, constraints, changes, and corrections
6. Important files, artifacts, resources, outputs, and tool results
7. Supporting details

Never drop higher-priority information to preserve lower-priority details.

## HANDOFF / BOUNDARY RULES

- Preserve the minimum handoff information needed to connect each compressed block to later context.
- If later messages supersede or correct earlier block content, reflect the corrected state appropriately.
- Do NOT absorb standalone content from non-target messages unless required to explain the target block correctly.

## TASK-TYPE ADAPTATION

- For execution-heavy tasks, prioritize action continuity, work-in-progress state, dependencies, blockers, and exact handoff status.
- For information-heavy tasks, prioritize findings, evidence, extracted structure, comparisons, conclusions, and unresolved questions.
- In all cases, preserve both what was done and what was learned.

## OUTPUT RULES

- Target length for each block summary: <= {compression_target_tokens} tokens.

// ──────────────────────────── 导出函数 ────────────────────────────

- Each block is a finished historical ReAct block, not ongoing work.
- Preserve both ` + "`User Requirements`" + ` and ` + "`Final Result`" + ` explicitly in each summary when they exist.
- Return valid JSON only.
- Use this exact schema:
{
  "blocks": [
    {
      "block_id": "react_1",
      "summary": "..."
    }
  ]
}
- Include at most one result per block_id.
- Do not emit undeclared block_ids.
`

// ──────────────────────────── 导出函数 ────────────────────────────

// NewDialogueCompressor 创建对话压缩器实例。
//
// 对应 Python: DialogueCompressor.__init__(config)
func NewDialogueCompressor(config *DialogueCompressorConfig, opts ...DialogueCompressorOption) (*DialogueCompressor, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	bp := processor.NewBaseProcessor(config)

	compressedPrompt := config.CustomCompressionPrompt
	if compressedPrompt == "" {
		compressedPrompt = defaultCompressionPrompt
	}

	dc := &DialogueCompressor{
		BaseProcessor:           bp,
		tokenThreshold:          config.TokensThreshold,
		messageNumThreshold:     config.MessagesThreshold,
		messagesToKeep:          config.MessagesToKeep,
		keepLastRound:           config.KeepLastRound,
		compressionTargetTokens: config.CompressionTargetTokens,
		compressedPrompt:        compressedPrompt,
	}

	// 应用选项
	for _, opt := range opts {
		opt(dc)
	}

	// 如果未通过选项注入 Model，则从配置创建
	if dc.model == nil {
		model, err := llm.NewModel(config.ModelClient, config.Model)
		if err != nil {
			return nil, err
		}
		dc.model = model
	}

	return dc, nil
}

// WithCompressorModel 注入已有 Model 实例（测试用）。
func WithCompressorModel(model *llm.Model) DialogueCompressorOption {
	return func(dc *DialogueCompressor) { dc.model = model }
}

// Validate 校验对话压缩器配置。
func (c *DialogueCompressorConfig) Validate() error {
	if c.TokensThreshold <= 0 {
		return fmt.Errorf("DialogueCompressorConfig.TokensThreshold 必须大于 0，当前值: %d", c.TokensThreshold)
	}
	if c.MessagesThreshold < 0 {
		return fmt.Errorf("DialogueCompressorConfig.MessagesThreshold 不能为负数，当前值: %d", c.MessagesThreshold)
	}
	if c.MessagesToKeep < 0 {
		return fmt.Errorf("DialogueCompressorConfig.MessagesToKeep 不能为负数，当前值: %d", c.MessagesToKeep)
	}
	if c.CompressionTargetTokens <= 0 {
		return fmt.Errorf("DialogueCompressorConfig.CompressionTargetTokens 必须大于 0，当前值: %d", c.CompressionTargetTokens)
	}
	return nil
}

// SetModelDefaults 设置默认模型配置。
func (c *DialogueCompressorConfig) SetModelDefaults(model *llm_schema.ModelRequestConfig, modelClient *llm_schema.ModelClientConfig) {
	if c.Model == nil && model != nil {
		c.Model = model
	}
	if c.ModelClient == nil && modelClient != nil {
		c.ModelClient = modelClient
	}
}

// GetModel 返回模型请求配置。
func (c *DialogueCompressorConfig) GetModel() *llm_schema.ModelRequestConfig {
	return c.Model
}

// ProcessorType 返回处理器类型标识。
func (dc *DialogueCompressor) ProcessorType() string { return "DialogueCompressor" }

// TriggerAddMessages 判断是否需要介入消息添加。
//
// 触发条件（满足任一）：
//   - MessagesThreshold > 0 && 总消息数 > MessagesThreshold
//   - 总 Token 数 > TokensThreshold
//
// 前置条件：MessagesToKeep > 0 && 总消息数 < MessagesToKeep → 直接返回 false
//
// 对应 Python: DialogueCompressor.trigger_add_messages()
func (dc *DialogueCompressor) TriggerAddMessages(ctx context.Context, mc iface.ModelContext, messagesToAdd []llm_schema.BaseMessage, _ ...iface.Option) (bool, error) {
	messageSize := mc.Len() + len(messagesToAdd)

	if dc.messageNumThreshold > 0 && messageSize > dc.messageNumThreshold {
		logger.Info(logger.ComponentAgentCore).
			Str("event_type", "DialogueCompressor_triggered").
			Int("message_size", messageSize).
			Int("threshold", dc.messageNumThreshold).
			Msg("上下文消息数超过阈值")
		return true, nil
	}

	if dc.messagesToKeep > 0 && messageSize < dc.messagesToKeep {
		return false, nil
	}

	allMsgs, _ := mc.GetMessages(0, true)
	tokens := dc.countMessagesTokens(mc, allMsgs)
	if tokens > dc.tokenThreshold {
		logger.Info(logger.ComponentAgentCore).
			Str("event_type", "DialogueCompressor_triggered").
			Int("tokens", tokens).
			Int("threshold", dc.tokenThreshold).
			Msg("上下文 Token 数超过阈值")
		return true, nil
	}

	return false, nil
}

// OnAddMessages 执行对话压缩。
//
// 对应 Python: DialogueCompressor.on_add_messages()
func (dc *DialogueCompressor) OnAddMessages(ctx context.Context, mc iface.ModelContext, messagesToAdd []llm_schema.BaseMessage, _ ...iface.Option) (*iface.ContextEvent, []llm_schema.BaseMessage, error) {
	allMsgs, _ := mc.GetMessages(0, true)
	allMessages := append(allMsgs, messagesToAdd...)
	dc.ResetCompressionUsage()

	compressUntilIdx := dc.GetCompressIdx(allMessages)
	if compressUntilIdx == -1 {
		return nil, messagesToAdd, nil
	}

	targets := dc.BuildCompressTargets(allMessages[:compressUntilIdx])
	if len(targets) == 0 {
		return nil, messagesToAdd, nil
	}

	response, err := dc.InvokeMultiBlockCompression(ctx, allMessages, targets)
	if err != nil {
		// MODEL_CALL_FAILED 时降级跳过
		if isModelCallFailedError(err) {
			logger.Warn(logger.ComponentAgentCore).
				Str("processor_type", dc.ProcessorType()).
				Err(err).
				Msg("压缩模型调用失败，跳过当前处理器")
			return nil, messagesToAdd, nil
		}
		return nil, messagesToAdd, err
	}

	replacements, modifiedIndices := dc.BuildJSONReplacements(ctx, mc, targets, response.ParserContent)
	if len(replacements) > 0 {
		updatedMessages := processor.ReplaceMessages(allMessages, replacements)
		event := &iface.ContextEvent{
			EventType:        dc.ProcessorType(),
			MessagesToModify: modifiedIndices,
			CompactSummary:   dc.ExtractCompactSummaryFromReplacements(replacements),
			CompressionUsage: dc.CurrentCompressionUsage(),
		}
		mc.SetMessages(updatedMessages, true)
		return event, []llm_schema.BaseMessage{}, nil
	}

	if !IsValidBlocksPayload(response.ParserContent) {
		fallback := dc.BuildFallbackReplacement(ctx, mc, targets, response.Content.Text())
		if fallback != nil {
			updatedMessages := processor.ReplaceMessages(allMessages, []processor.Replacement{*fallback})
			event := &iface.ContextEvent{
				EventType:        dc.ProcessorType(),
				MessagesToModify: buildRangeIndices(fallback.StartIdx, fallback.EndIdx),
				CompactSummary:   dc.ExtractCompactSummaryFromReplacements([]processor.Replacement{*fallback}),
				CompressionUsage: dc.CurrentCompressionUsage(),
			}
			mc.SetMessages(updatedMessages, true)
			return event, []llm_schema.BaseMessage{}, nil
		}
	}

	return nil, messagesToAdd, nil
}

// GetCompressIdx 计算压缩截止位置。
//
// 对应 Python: DialogueCompressor.get_compress_idx()
func (dc *DialogueCompressor) GetCompressIdx(messages []llm_schema.BaseMessage) int {
	keepIndex := len(messages)
	if dc.messagesToKeep > 0 {
		keepIndex = len(messages) - dc.messagesToKeep
	}
	if !dc.keepLastRound {
		return keepIndex
	}

	lastFinalAssistantIdx := processor.FindLastFinalAssistantIdx(messages)
	if lastFinalAssistantIdx == -1 {
		return keepIndex
	}
	if lastFinalAssistantIdx < keepIndex {
		return lastFinalAssistantIdx
	}
	return keepIndex
}

// GetCompressPairs 识别消息列表中的对话轮次配对。
//
// 遍历消息列表，寻找 UserMessage → ... → AssistantMessage(无 tool_calls) 的配对。
// 对应 Python: DialogueCompressor.get_compress_pairs()
func GetCompressPairs(messages []llm_schema.BaseMessage) [][2]int {
	currentUser := -1
	var result [][2]int

	for i, msg := range messages {
		if msg.GetRole() == llm_schema.RoleTypeUser {
			if currentUser == -1 {
				currentUser = i
			}
		} else if am, ok := msg.(*llm_schema.AssistantMessage); ok && len(am.ToolCalls) == 0 && currentUser != -1 {
			if i-currentUser >= 1 {
				result = append(result, [2]int{currentUser, i})
				currentUser = -1
			}
		}
		// 其他消息类型（ToolMessage、有 tool_calls 的 AssistantMessage）→ continue
	}

	return result
}

// BuildCompressTargets 从消息列表构建压缩目标列表。
//
// 对应 Python: DialogueCompressor._build_compress_targets()
func (dc *DialogueCompressor) BuildCompressTargets(messages []llm_schema.BaseMessage) []compressTarget {
	rounds := dc.collectCompleteRounds(messages)
	if len(rounds) == 0 {
		return nil
	}

	// 过滤可压缩轮次（blockMessageCount > 2）
	var compressibleIndices []int
	for index, round := range rounds {
		if round.blockMessageCount > 2 {
			compressibleIndices = append(compressibleIndices, index)
		}
	}
	if len(compressibleIndices) == 0 {
		return nil
	}

	firstTarget := compressibleIndices[0]
	lastTarget := compressibleIndices[len(compressibleIndices)-1]
	selectedRounds := rounds[firstTarget : lastTarget+1]

	targets := make([]compressTarget, 0, len(selectedRounds))
	for blockNo, round := range selectedRounds {
		targets = append(targets, compressTarget{
			blockID:  fmt.Sprintf("react_%d", blockNo+1),
			userIDx:  round.userIDx,
			startIDx: round.startIDx,
			endIDx:   round.endIDx,
			messages: round.messages,
		})
	}

	return targets
}

// InvokeMultiBlockCompression 调用 LLM 执行多块压缩。
//
// 对应 Python: DialogueCompressor._invoke_multi_block_compression()
func (dc *DialogueCompressor) InvokeMultiBlockCompression(ctx context.Context, contextMessages []llm_schema.BaseMessage, targets []compressTarget) (*llm_schema.AssistantMessage, error) {
	systemPrompt := strings.ReplaceAll(dc.compressedPrompt, "{compression_target_tokens}", fmt.Sprintf("%d", dc.compressionTargetTokens))

	modelMessages := []llm_schema.BaseMessage{
		llm_schema.NewSystemMessage(systemPrompt),
		llm_schema.NewUserMessage(dc.BuildSplitContextPayload(contextMessages, targets)),
		llm_schema.NewUserMessage(dc.BuildTargetsPayload(targets)),
	}

	// 将 BaseMessage 列表转换为 MessagesParam
	response, err := dc.model.Invoke(ctx, model_clients.NewMessagesParam(modelMessages...), model_clients.WithInvokeOutputParser(output_parsers.NewJsonOutputParser()))
	if err != nil {
		return nil, exception.NewBaseError(
			exception.NewStatusCode("MODEL_CALL_FAILED", 181001, ""),
			exception.WithMsg(fmt.Sprintf("%s 调用压缩模型失败", dc.ProcessorType())),
			exception.WithCause(err),
		)
	}

	dc.RecordCompressionUsage(response)
	return response, nil
}

// BuildJSONReplacements 从 LLM 返回的 ParserContent 解析 JSON 构建替换列表。
//
// 对应 Python: DialogueCompressor._build_json_replacements()
func (dc *DialogueCompressor) BuildJSONReplacements(ctx context.Context, mc iface.ModelContext, targets []compressTarget, parserContent any) ([]processor.Replacement, []int) {
	if !IsValidBlocksPayload(parserContent) {
		return nil, nil
	}

	parserMap, ok := parserContent.(map[string]any)
	if !ok {
		return nil, nil
	}
	blocks, ok := parserMap["blocks"].([]any)
	if !ok {
		return nil, nil
	}

	// 构建 blockID → summary 映射
	blockMap := make(map[string]string)
	for _, item := range blocks {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		blockID, _ := itemMap["block_id"].(string)
		if blockID == "" {
			continue
		}
		summary, ok := itemMap["summary"].(string)
		if !ok {
			continue
		}
		summary = strings.TrimSpace(summary)
		if summary == "" {
			continue
		}
		blockMap[blockID] = summary
	}

	var replacements []processor.Replacement
	var modifiedIndices []int

	for _, target := range targets {
		summary, ok := blockMap[target.blockID]
		if !ok || summary == "" {
			continue
		}

		replacementMessage := llm_schema.NewUserMessage(WrapMemoryBlock(summary))
		replacementMessages := []llm_schema.BaseMessage{replacementMessage}

		if !dc.HasCompressionBenefit(mc, target.messages, replacementMessages) {
			continue
		}

		replacements = append(replacements, processor.Replacement{
			StartIdx: target.startIDx,
			EndIdx:   target.endIDx,
			Messages: replacementMessages,
		})
		for idx := target.startIDx; idx <= target.endIDx; idx++ {
			modifiedIndices = append(modifiedIndices, idx)
		}
	}

	return replacements, modifiedIndices
}

// BuildFallbackReplacement 构建降级替换（JSON 解析失败时用 LLM 原始输出整段替换）。
//
// 对应 Python: DialogueCompressor._build_fallback_replacement()
func (dc *DialogueCompressor) BuildFallbackReplacement(ctx context.Context, mc iface.ModelContext, targets []compressTarget, summary string) *processor.Replacement {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return nil
	}

	startIDx := targets[0].startIDx
	endIDx := targets[0].endIDx
	var originalMessages []llm_schema.BaseMessage
	for _, target := range targets {
		if target.startIDx < startIDx {
			startIDx = target.startIDx
		}
		if target.endIDx > endIDx {
			endIDx = target.endIDx
		}
		originalMessages = append(originalMessages, target.messages...)
	}

	replacementMessage := llm_schema.NewUserMessage(WrapMemoryBlock(summary))
	replacementMessages := []llm_schema.BaseMessage{replacementMessage}

	if !dc.HasCompressionBenefit(mc, originalMessages, replacementMessages) {
		return nil
	}

	return &processor.Replacement{
		StartIdx: startIDx,
		EndIdx:   endIDx,
		Messages: replacementMessages,
	}
}

// BuildSplitContextPayload 构建上下文载荷（目标前/目标块/目标后）。
//
// 对应 Python: DialogueCompressor._build_split_context_payload()
func (dc *DialogueCompressor) BuildSplitContextPayload(contextMessages []llm_schema.BaseMessage, targets []compressTarget) string {
	firstTargetStart := targets[0].startIDx
	lastTargetEnd := targets[0].endIDx
	for _, t := range targets[1:] {
		if t.startIDx < firstTargetStart {
			firstTargetStart = t.startIDx
		}
		if t.endIDx > lastTargetEnd {
			lastTargetEnd = t.endIDx
		}
	}

	beforeTargets := serializeMessagesRange(contextMessages, 0, firstTargetStart)
	if beforeTargets == "" {
		beforeTargets = "(none)"
	}

	var targetBlocks []string
	targetBlocks = append(targetBlocks, "[Compression Targets]")
	for _, target := range targets {
		targetBlocks = append(targetBlocks, fmt.Sprintf("[Block: %s]", target.blockID))
		blockText := serializeMessagesRangeWithOffset(target.messages, target.startIDx)
		if blockText == "" {
			blockText = "(empty)"
		}
		targetBlocks = append(targetBlocks, blockText, "")
	}

	afterTargets := serializeMessagesRange(contextMessages, lastTargetEnd+1, len(contextMessages))
	if afterTargets == "" {
		afterTargets = "(none)"
	}

	var parts []string
	parts = append(parts, "[Context Before Targets]", beforeTargets, "")
	parts = append(parts, targetBlocks...)
	parts = append(parts, "[Context After Targets]", afterTargets)

	return strings.Join(parts, "\n")
}

// BuildTargetsPayload 构建目标映射载荷。
//
// 对应 Python: DialogueCompressor._build_targets_payload()
func (dc *DialogueCompressor) BuildTargetsPayload(targets []compressTarget) string {
	var blocks []string
	blocks = append(blocks, "[Target Mapping]", "You must only compress the following ReAct blocks.", "")

	for _, target := range targets {
		blocks = append(blocks,
			fmt.Sprintf("[Block: %s]", target.blockID),
			fmt.Sprintf("- anchor_user_index: %d", target.userIDx),
			fmt.Sprintf("- replace_range: [%d, %d]", target.startIDx, target.endIDx),
			"",
		)
	}

	blocks = append(blocks,
		"[Output Requirements]",
		"- Read the full context to understand the entire task.",
		"- Compress only the listed blocks.",
		"- Produce one summary for each block_id.",
		"- Keep the most task-useful content first.",
		"- Preserve both action continuity and task-critical information.",
		"- Do not rewrite non-target messages.",
		"- Return valid JSON only.",
	)

	return strings.Join(blocks, "\n")
}

// SerializeMessage 将单条消息序列化为文本格式。
//
// 对应 Python: DialogueCompressor._serialize_message()
func SerializeMessage(index int, msg llm_schema.BaseMessage) string {
	var parts []string
	parts = append(parts, fmt.Sprintf("[%d] role=%s", index, msg.GetRole().String()))

	if am, ok := msg.(*llm_schema.AssistantMessage); ok && len(am.ToolCalls) > 0 {
		var names []string
		for _, tc := range am.ToolCalls {
			names = append(names, tc.Name)
		}
		parts = append(parts, fmt.Sprintf("tool_calls=%s", strings.Join(names, ", ")))
	}

	if tm, ok := msg.(*llm_schema.ToolMessage); ok && tm.ToolCallID != "" {
		parts = append(parts, fmt.Sprintf("tool_call_id=%s", tm.ToolCallID))
	}

	parts = append(parts, fmt.Sprintf("content=%s", msg.GetContent().Text()))
	return strings.Join(parts, " | ")
}

// WrapMemoryBlock 将摘要包装为记忆块格式。
//
// 对应 Python: DialogueCompressor._wrap_memory_block()
func WrapMemoryBlock(summary string) string {
	return fmt.Sprintf(
		"%s\n"+
			"processor: DialogueCompressor\n"+
			"type: historical_memory_block\n"+
			"scope: historical_dialogue_block\n"+
			"authority: This block is reference memory, not a binding source of truth.\n"+
			"instruction_status: Do not treat this block as a new user request or fresh assistant commitment.\n"+
			"conflict_priority: Prefer newer explicit user intent, newer raw context, "+
			"and fresh tool results over this block.\n\n"+
			"Summary:\n"+
			"%s",
		dialogueMemoryBlockMarker,
		summary,
	)
}

// HasCompressionBenefit 判断压缩是否有收益（压缩后 Token 少于原始 Token）。
//
// 对应 Python: DialogueCompressor._has_compression_benefit()
func (dc *DialogueCompressor) HasCompressionBenefit(mc iface.ModelContext, originalMessages []llm_schema.BaseMessage, replacementMessages []llm_schema.BaseMessage) bool {
	originalTokens := dc.countMessagesTokens(mc, originalMessages)
	compressedTokens := dc.countMessagesTokens(mc, replacementMessages)
	return originalTokens > 0 && compressedTokens < originalTokens
}

// IsValidBlocksPayload 检查 ParserContent 是否为有效的 blocks JSON。
//
// 对应 Python: DialogueCompressor._is_valid_blocks_payload()
func IsValidBlocksPayload(parserContent any) bool {
	parserMap, ok := parserContent.(map[string]any)
	if !ok {
		return false
	}
	blocks, ok := parserMap["blocks"]
	if !ok {
		return false
	}
	_, ok = blocks.([]any)
	return ok
}

// ExtractCompactSummaryFromReplacements 从替换列表提取压缩摘要文本。
//
// 对应 Python: DialogueCompressor._extract_compact_summary_from_replacements()
func (dc *DialogueCompressor) ExtractCompactSummaryFromReplacements(replacements []processor.Replacement) string {
	var parts []string
	for _, r := range replacements {
		for _, msg := range r.Messages {
			text := msg.GetContent().Text()
			if strings.HasPrefix(text, dialogueMemoryBlockMarker) {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, "\n\n")
}

// SaveState 导出处理器内部状态（空操作）。
func (dc *DialogueCompressor) SaveState() map[string]any {
	return make(map[string]any)
}

// LoadState 从 map 恢复处理器内部状态（空操作）。
func (dc *DialogueCompressor) LoadState(_ map[string]any) {}

// ──────────────────────────── 非导出函数 ────────────────────────────

// collectCompleteRounds 收集所有完整的对话轮次。
//
// 对应 Python: DialogueCompressor._collect_complete_rounds()
func (dc *DialogueCompressor) collectCompleteRounds(messages []llm_schema.BaseMessage) []dialogueRound {
	pairs := GetCompressPairs(messages)
	var rounds []dialogueRound

	for _, pair := range pairs {
		userIDx := pair[0]
		assistantIDx := pair[1]
		if userIDx < 0 || assistantIDx <= userIDx {
			continue
		}
		roundMessages := make([]llm_schema.BaseMessage, assistantIDx-userIDx)
		copy(roundMessages, messages[userIDx+1:assistantIDx+1])

		rounds = append(rounds, dialogueRound{
			userIDx:           userIDx,
			startIDx:          userIDx + 1,
			endIDx:            assistantIDx,
			messages:          roundMessages,
			blockMessageCount: assistantIDx - userIDx + 1,
		})
	}

	return rounds
}

// countMessagesTokens 计算消息列表的 Token 数。
func (dc *DialogueCompressor) countMessagesTokens(mc iface.ModelContext, messages []llm_schema.BaseMessage) int {
	modelName := ""
	if dc.model != nil && dc.model.ModelConfig != nil {
		modelName = dc.model.ModelConfig.ModelName
	}
	return processor.CountMessagesTokens(mc.TokenCounter(), messages, modelName, dc.ProcessorType())
}

// isModelCallFailedError 判断错误是否为 MODEL_CALL_FAILED
func isModelCallFailedError(err error) bool {
	baseErr, ok := err.(*exception.BaseError)
	if !ok {
		return false
	}
	return baseErr.Status().Name() == "MODEL_CALL_FAILED"
}

// serializeMessagesRange 序列化指定范围的消息
func serializeMessagesRange(messages []llm_schema.BaseMessage, start, end int) string {
	if start >= end || start >= len(messages) {
		return ""
	}
	if end > len(messages) {
		end = len(messages)
	}
	var lines []string
	for i := start; i < end; i++ {
		lines = append(lines, SerializeMessage(i, messages[i]))
	}
	return strings.Join(lines, "\n")
}

// serializeMessagesRangeWithOffset 序列化消息列表，索引从 offset 开始
func serializeMessagesRangeWithOffset(messages []llm_schema.BaseMessage, offset int) string {
	if len(messages) == 0 {
		return ""
	}
	var lines []string
	for i, msg := range messages {
		lines = append(lines, SerializeMessage(offset+i, msg))
	}
	return strings.Join(lines, "\n")
}

// buildRangeIndices 构建索引范围
func buildRangeIndices(start, end int) []int {
	indices := make([]int, 0, end-start+1)
	for i := start; i <= end; i++ {
		indices = append(indices, i)
	}
	return indices
}

// init 自动注册到 context_engine 注册表
func init() {
	context_engine.RegisterProcessorFactory("DialogueCompressor",
		func(config iface.ProcessorConfig) (iface.ContextProcessor, error) {
			cfg, ok := config.(*DialogueCompressorConfig)
			if !ok {
				return nil, fmt.Errorf("DialogueCompressor: 配置类型不匹配，期望 *DialogueCompressorConfig，实际 %T", config)
			}
			dc, err := NewDialogueCompressor(cfg)
			if err != nil {
				return nil, err
			}
			return dc, nil
		},
	)
}
