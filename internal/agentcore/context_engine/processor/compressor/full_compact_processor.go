package compressor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine"
	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/context"
	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/processor"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// FullCompactProcessorConfig 全量压缩处理器配置。
//
// 当上下文 Token 数超过阈值时，使用 LLM 生成完整摘要或加载 Session Memory 替换历史消息，
// 是上下文管理的最后防线。
//
// 对应 Python: FullCompactProcessorConfig (pydantic.BaseModel)
type FullCompactProcessorConfig struct {
	// TriggerTotalTokens 触发全量压缩的 Token 阈值
	TriggerTotalTokens int
	// CompressionCallMaxTokens 压缩调用最大 Token 预算
	CompressionCallMaxTokens int
	// MessagesToKeep 保留最近的消息数量
	MessagesToKeep int
	// SessionMemoryEnabled 是否启用 Session Memory 路径
	SessionMemoryEnabled bool
	// Model 压缩模型请求配置
	Model *llm_schema.ModelRequestConfig
	// ModelClient 压缩模型客户端配置
	ModelClient *llm_schema.ModelClientConfig
	// KeepToolMessagePairs 保留工具消息对时是否包含对应的 AssistantMessage
	KeepToolMessagePairs bool
	// StateSnapshotMaxChars 状态快照最大字符数
	StateSnapshotMaxChars int
	// ReinjectRecentSkills 重新注入最近 Skill 读取轮次的最大数量
	ReinjectRecentSkills int
	// ReinjectFileToolNames 文件相关工具名称列表
	ReinjectFileToolNames []string
	// ReinjectToolResultHintNames 工具结果提示工具名称列表
	ReinjectToolResultHintNames []string
	// Marker 全量压缩边界标记
	Marker string
	// StateMarker 状态消息标记
	StateMarker string
	// SyntheticUserMarker 合成用户消息标记
	SyntheticUserMarker string
	// SummaryIntro 摘要引导文本
	SummaryIntro string
	// RecentMessagesNotice 近期消息保留提示
	RecentMessagesNotice string
	// SessionMemoryMarker Session Memory 边界标记
	SessionMemoryMarker string
	// SessionMemoryIntro Session Memory 摘要引导文本
	SessionMemoryIntro string
}

// FullCompactProcessor 全量压缩处理器，上下文管理的最后防线。
//
// 触发后执行双路径流程：
//  1. Session Memory 路径（优先）：加载已提交的 Session Memory 笔记
//  2. LLM 全量压缩路径（回退）：调用 LLM 生成完整摘要
//
// 对应 Python: openjiuwen/core/context_engine/processor/compressor/full_compact_processor.py (FullCompactProcessor)
type FullCompactProcessor struct {
	*processor.BaseProcessor
	// fcpConfig 全量压缩处理器具体配置
	fcpConfig *FullCompactProcessorConfig
	// model 压缩用 LLM 实例
	model *llm.Model
	// reinjector 状态重新注入器
	reinjector *FullCompactStateReinjector
}

// ReinjectedStateBuilderSpec 重新注入状态构建器规格。
//
// 每个 Builder 负责从历史消息中提取特定类型的状态信息，
// 压缩后作为 UserMessage 重新注入上下文。
//
// 对应 Python: ReinjectedStateBuilderSpec (dataclass)
type ReinjectedStateBuilderSpec struct {
	// Name 构建器名称（用于过滤）
	Name string
	// Label 构建器标签（用于状态消息标题）
	Label string
	// Builder 构建器函数，第二个参数为 FullCompactProcessor 实例，
	// 使 Builder 能访问 processor 的配置（StateMarker、ReinjectRecentSkills）和方法（TruncateStateText）。
	// 返回 []BaseMessage（列表）或 string（文本）。
	//
	// 对应 Python: builder(processor, *, context, messages, messages_to_keep)
	Builder func(ctx context.Context, fcp *FullCompactProcessor, mc iface.ModelContext, messages []llm_schema.BaseMessage, messagesToKeep []llm_schema.BaseMessage) any
}

// FullCompactStateReinjector 状态重新注入器，管理多个 Builder 的注册和执行。
//
// 压缩后遍历注册的 Builder，将非空结果作为状态消息重新注入上下文，
// 确保关键信息不会因压缩而丢失。
//
// 对应 Python: FullCompactStateReinjector
type FullCompactStateReinjector struct {
	// builders 注册的构建器列表
	builders []ReinjectedStateBuilderSpec
}

// FullCompactProcessorOption FullCompactProcessor 构造选项函数。
type FullCompactProcessorOption func(*FullCompactProcessor)

// ──────────────────────────── 常量 ────────────────────────────

// baseCompactPrompt 全量压缩提示词，与 Python BASE_COMPACT_PROMPT 对齐
const baseCompactPrompt = `CRITICAL: Respond with TEXT ONLY. Do NOT call any tools.

- Do NOT use Read, Bash, Grep, Glob, Edit, Write, or ANY other tool.
- You already have all the context you need in the conversation above.
- Tool calls will be REJECTED and will waste your only turn — you will fail the task.
- Your entire response must be plain text: an <analysis> block followed by a <summary> block.

Your task is to create a detailed summary of the conversation so far,
pay close attention to the user's explicit requests and your previous actions.
This summary should be thorough in capturing technical details, code patterns,
and architectural decisions that would be essential for continuing development work without losing context.

Before providing your final summary, wrap your analysis in <analysis>
tags to organize your thoughts and ensure you've covered all necessary points. In your analysis process:

1. Chronologically analyze each message and section of the conversation. For each section thoroughly identify:
   - The user's explicit requests and intents
   - Your approach to addressing the user's requests
   - Key decisions, technical concepts and code patterns
   - Specific details like:
     - file names
     - full code snippets
     - function signatures
     - file edits
   - Errors that you ran into and how you fixed them
   - Pay special attention to specific user feedback that you received, especially if the user told you to do something
     differently.
2. Double-check for technical accuracy and completeness, addressing each required element thoroughly.

Your summary should include the following sections:

1. Primary Request and Intent: Capture all of the user's explicit requests and intents in detail
2. Key Technical Concepts: List all important technical concepts, technologies, and frameworks discussed.
3. Files and Code Sections: Enumerate specific files and code sections examined, modified, or created.
    Pay special attention to the most recent messages and include full code snippets where applicable and
    include a summary of why this file read or edit is important.
4. Errors and fixes: List all errors that you ran into, and how you fixed them.
    Pay special attention to specific user feedback that you received,
    especially if the user told you to do something differently.
5. Problem Solving: Document problems solved and any ongoing troubleshooting efforts.
6. All user messages: List ALL user messages that are not tool results.
    These are critical for understanding the users' feedback and changing intent.
7. Pending Tasks: Outline any pending tasks that you have explicitly been asked to work on.
8. Current Work: Describe in detail precisely what was being worked on immediately before this summary request,
    paying special attention to the most recent messages from both user and assistant.
    Include file names and code snippets where applicable.
9. Optional Next Step: List the next step that you will take that is related to the most recent work you were doing.
    IMPORTANT: ensure that this step is DIRECTLY in line with the user's most recent explicit requests,
    and the task you were working on immediately before this summary request. If your last task was concluded,
    then only list next steps if they are explicitly in line with the users request.
    Do not start on tangential requests or really old requests that were already completed without
    confirming with the user first.
    If there is a next step, include direct quotes from the most recent conversation showing exactly what task you were
    working on and where you left off.
    This should be verbatim to ensure there's no drift in task interpretation.

Here's an example of how your output should be structured:

<example>
<analysis>
[Your thought process, ensuring all points are covered thoroughly and accurately]
</analysis>

<summary>
1. Primary Request and Intent:
   [Detailed description]

2. Key Technical Concepts:
   - [Concept 1]
   - [Concept 2]
   - [...]

3. Files and Code Sections:
   - [File Name 1]
      - [Summary of why this file is important]
      - [Summary of the changes made to this file, if any]
      - [Important Code Snippet]
   - [File Name 2]
      - [Important Code Snippet]
   - [...]

4. Errors and fixes:
    - [Detailed description of error 1]:
      - [How you fixed the error]
      - [User feedback on the error if any]
    - [...]

5. Problem Solving:
   [Description of solved problems and ongoing troubleshooting]

6. All user messages:
    - [Detailed non tool use user message]
    - [...]

7. Pending Tasks:
   - [Task 1]
   - [Task 2]
   - [...]

8. Current Work:
   [Precise description of current work]

9. Optional Next Step:
   [Optional Next step to take]

</summary>
</example>

Please provide your summary based on the conversation so far,
following this structure and ensuring precision and thoroughness in your response.
`

// ──────────────────────────── 全局变量 ────────────────────────────

// analysisRegex 匹配 <analysis>...</analysis> 的正则（编译一次复用）
var analysisRegex = regexp.MustCompile(`(?s)<analysis>.*?</analysis>`)

// summaryRegex 匹配 <summary>...</summary> 的正则（编译一次复用）
var summaryRegex = regexp.MustCompile(`(?s)<summary>(.*?)</summary>`)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewFullCompactProcessorConfig 创建全量压缩处理器默认配置。
func NewFullCompactProcessorConfig() *FullCompactProcessorConfig {
	return &FullCompactProcessorConfig{
		TriggerTotalTokens:          180000,
		CompressionCallMaxTokens:    200000,
		MessagesToKeep:              10,
		SessionMemoryEnabled:        true,
		KeepToolMessagePairs:        true,
		StateSnapshotMaxChars:       4000,
		ReinjectRecentSkills:        3,
		ReinjectFileToolNames:       []string{"read_file", "write_file", "edit_file", "glob", "grep"},
		ReinjectToolResultHintNames: []string{"read_file", "write_file", "edit_file", "glob", "grep"},
		Marker:                      "[FULL_COMPACT_BOUNDARY]",
		StateMarker:                 "[FULL_COMPACT_STATE]",
		SyntheticUserMarker:         "[earlier conversation truncated for compaction retry]",
		SummaryIntro:                "This session is being continued from a previous conversation that ran out of context. The summary below covers the earlier portion of the conversation.",
		RecentMessagesNotice:        "Recent messages are preserved verbatim.",
		SessionMemoryMarker:         "[SESSION_MEMORY_BOUNDARY]",
		SessionMemoryIntro:          "Earlier conversation has been replaced with the session memory file. Use it as the canonical summary of prior work.",
	}
}

// Validate 校验全量压缩处理器配置。
func (c *FullCompactProcessorConfig) Validate() error {
	if c.TriggerTotalTokens <= 0 {
		return fmt.Errorf("FullCompactProcessorConfig.TriggerTotalTokens 必须大于 0，当前值: %d", c.TriggerTotalTokens)
	}
	if c.CompressionCallMaxTokens <= 0 {
		return fmt.Errorf("FullCompactProcessorConfig.CompressionCallMaxTokens 必须大于 0，当前值: %d", c.CompressionCallMaxTokens)
	}
	if c.MessagesToKeep < 0 {
		return fmt.Errorf("FullCompactProcessorConfig.MessagesToKeep 不能为负数，当前值: %d", c.MessagesToKeep)
	}
	if c.StateSnapshotMaxChars <= 0 {
		return fmt.Errorf("FullCompactProcessorConfig.StateSnapshotMaxChars 必须大于 0，当前值: %d", c.StateSnapshotMaxChars)
	}
	if c.ReinjectRecentSkills < 0 {
		return fmt.Errorf("FullCompactProcessorConfig.ReinjectRecentSkills 不能为负数，当前值: %d", c.ReinjectRecentSkills)
	}
	return nil
}

// ProcessorType 返回处理器类型标识。
func (fcp *FullCompactProcessor) ProcessorType() string {
	return "FullCompactProcessor"
}

// TriggerAddMessages 判断是否需要介入消息添加。
//
// 触发条件：消息列表构成完整 API 轮次 && 上下文 Token 数 > TriggerTotalTokens
//
// 对应 Python: FullCompactProcessor.trigger_add_messages()
func (fcp *FullCompactProcessor) TriggerAddMessages(_ context.Context, mc iface.ModelContext, messagesToAdd []llm_schema.BaseMessage, _ ...iface.Option) (bool, error) {
	allMsgs, _ := mc.GetMessages(0, true)
	candidateMessages := append(allMsgs, messagesToAdd...)
	if !fcp.IsAPIRound(candidateMessages) {
		return false, nil
	}
	tokens := fcp.countContextWindowTokens(mc, candidateMessages)
	triggered := tokens > fcp.fcpConfig.TriggerTotalTokens
	return triggered, nil
}

// OnAddMessages 执行全量压缩，主入口。
//
// 对应 Python: FullCompactProcessor.on_add_messages()
func (fcp *FullCompactProcessor) OnAddMessages(ctx context.Context, mc iface.ModelContext, messagesToAdd []llm_schema.BaseMessage, _ ...iface.Option) (*iface.ContextEvent, []llm_schema.BaseMessage, error) {
	allMsgs, _ := mc.GetMessages(0, true)
	allMessages := append(allMsgs, messagesToAdd...)
	fcp.ResetCompressionUsage()

	event, newContextMessages, sessionMemoryMessage := fcp._buildReplacementMessages(ctx, mc, allMessages)
	if newContextMessages == nil {
		return nil, messagesToAdd, nil
	}
	mc.SetMessages(newContextMessages, true)
	if event != nil {
		event.CompressionUsage = fcp.CurrentCompressionUsage()
	}
	if sessionMemoryMessage == nil {
		fcp._invalidateSessionMemoryAnchor(ctx, mc)
	}
	return event, []llm_schema.BaseMessage{}, nil
}

// OnGetContextWindow 透传上下文窗口。
func (fcp *FullCompactProcessor) OnGetContextWindow(_ context.Context, _ iface.ModelContext, cw iface.ContextWindow, _ ...iface.Option) (*iface.ContextEvent, iface.ContextWindow, error) {
	return nil, cw, nil
}

// TriggerGetContextWindow 不触发上下文窗口获取。
func (fcp *FullCompactProcessor) TriggerGetContextWindow(_ context.Context, _ iface.ModelContext, _ iface.ContextWindow, _ ...iface.Option) (bool, error) {
	return false, nil
}

// SaveState 导出处理器内部状态（空操作）。
func (fcp *FullCompactProcessor) SaveState() map[string]any {
	return make(map[string]any)
}

// LoadState 从 map 恢复处理器内部状态（空操作）。
func (fcp *FullCompactProcessor) LoadState(_ map[string]any) {}

// TruncateStateText 截断状态文本到 StateSnapshotMaxChars（头尾保留）。
//
// 对应 Python: FullCompactProcessor.truncate_state_text()
func (fcp *FullCompactProcessor) TruncateStateText(text string) string {
	if len(text) <= fcp.fcpConfig.StateSnapshotMaxChars {
		return text
	}
	return _buildHeadTailTruncatedText(text, fcp.fcpConfig.StateSnapshotMaxChars)
}

// NewFullCompactProcessor 创建全量压缩处理器实例。
func NewFullCompactProcessor(config *FullCompactProcessorConfig, opts ...FullCompactProcessorOption) (*FullCompactProcessor, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	bp := processor.NewBaseProcessor(config)

	reinjector := newFullCompactStateReinjector()

	fcp := &FullCompactProcessor{
		BaseProcessor: bp,
		fcpConfig:     config,
		reinjector:    reinjector,
	}

	for _, opt := range opts {
		opt(fcp)
	}

	if fcp.model == nil && config.Model != nil && config.ModelClient != nil {
		model, err := llm.NewModel(config.ModelClient, config.Model)
		if err != nil {
			return nil, err
		}
		fcp.model = model
	}

	return fcp, nil
}

// WithFullCompactModel 注入已有 Model 实例（测试用）。
func WithFullCompactModel(model *llm.Model) FullCompactProcessorOption {
	return func(fcp *FullCompactProcessor) { fcp.model = model }
}

// RegisterBuilder 注册状态构建器，同名则替换。
//
// 对应 Python: FullCompactStateReinjector.register_builder()
func (r *FullCompactStateReinjector) RegisterBuilder(name, label string, builder func(ctx context.Context, fcp *FullCompactProcessor, mc iface.ModelContext, messages []llm_schema.BaseMessage, messagesToKeep []llm_schema.BaseMessage) any) {
	spec := ReinjectedStateBuilderSpec{Name: name, Label: label, Builder: builder}
	for i, existing := range r.builders {
		if existing.Name == name {
			r.builders[i] = spec
			return
		}
	}
	r.builders = append(r.builders, spec)
}

// IterBuilders 返回所有注册的构建器。
//
// 对应 Python: FullCompactStateReinjector.iter_builders()
func (r *FullCompactStateReinjector) IterBuilders() []ReinjectedStateBuilderSpec {
	result := make([]ReinjectedStateBuilderSpec, len(r.builders))
	copy(result, r.builders)
	return result
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newFullCompactStateReinjector 创建状态重新注入器并注册默认 Builder。
func newFullCompactStateReinjector() *FullCompactStateReinjector {
	r := &FullCompactStateReinjector{}
	r.RegisterBuilder("skills", "SKILLS", buildSkillReinjectedContent)
	r.RegisterBuilder("task_status", "TASK_STATUS", buildTaskStatusReinjectedContent)
	r.RegisterBuilder("plan_mode", "PLAN_MODE", buildPlanModeReinjectedContent)
	r.RegisterBuilder("plan", "PLAN", buildPlanReinjectedContent)
	return r
}

// _buildReplacementMessages 主构建逻辑，尝试 Session Memory 路径和 LLM 全量压缩路径。
//
// 对应 Python: FullCompactProcessor._build_replacement_messages()
func (fcp *FullCompactProcessor) _buildReplacementMessages(ctx context.Context, mc iface.ModelContext, allMessages []llm_schema.BaseMessage) (*iface.ContextEvent, []llm_schema.BaseMessage, *llm_schema.UserMessage) {
	boundaryIndex := fcp._findLastCompactionBoundaryIndex(allMessages)
	prefix, activeMessages := fcp._splitMessagesAtCompactionBoundary(allMessages, boundaryIndex)
	if len(activeMessages) == 0 {
		logger.Info(logger.ComponentAgentCore).Msg("[FullCompact] replacement skipped: no active messages after boundary")
		return nil, nil, nil
	}

	// Session Memory 路径（优先）
	sessionMemoryMessages, sessionMemoryMessage := fcp._buildSessionMemoryMessages(ctx, mc, prefix, activeMessages, boundaryIndex >= 0)
	if sessionMemoryMessages != nil {
		sessionMemoryTokens := fcp.countContextWindowTokens(mc, sessionMemoryMessages)
		if sessionMemoryTokens <= fcp.fcpConfig.TriggerTotalTokens {
			logger.Info(logger.ComponentAgentCore).Msg("[FullCompact] using session_memory replacement")
			return &iface.ContextEvent{
				EventType:        fcp.ProcessorType(),
				MessagesToModify: buildRangeIndices(0, len(allMessages)-1),
				CompactSummary:   processor.MessageToText(sessionMemoryMessage),
			}, sessionMemoryMessages, sessionMemoryMessage
		}
		logger.Info(logger.ComponentAgentCore).Msg("[FullCompact] session_memory candidate rejected: token budget exceeded")
	} else {
		logger.Info(logger.ComponentAgentCore).Msg("[FullCompact] session_memory candidate unavailable, fallback to full_compact")
	}

	// LLM 全量压缩路径（回退）
	newContextMessages, compactSummary := fcp._buildFullCompactMessages(ctx, mc, prefix, activeMessages)
	if newContextMessages == nil {
		logger.Warn(logger.ComponentAgentCore).Msg("[FullCompact] full_compact candidate build failed")
		return nil, nil, nil
	}
	logger.Info(logger.ComponentAgentCore).
		Int("output_messages", len(newContextMessages)).
		Msg("[FullCompact] using full_compact replacement")

	return &iface.ContextEvent{
		EventType:        fcp.ProcessorType(),
		MessagesToModify: buildRangeIndices(0, len(allMessages)-1),
		CompactSummary:   compactSummary,
	}, newContextMessages, nil
}

// _buildFullCompactMessages 构建 LLM 全量压缩消息。
//
// 对应 Python: FullCompactProcessor._build_full_compact_messages()
func (fcp *FullCompactProcessor) _buildFullCompactMessages(ctx context.Context, mc iface.ModelContext, prefix []llm_schema.BaseMessage, activeMessages []llm_schema.BaseMessage) ([]llm_schema.BaseMessage, string) {
	compactSource := fcp._prepareMessagesForPrompt(activeMessages)
	if len(compactSource) == 0 {
		return nil, ""
	}

	compactInput := fcp._truncateForPromptBudget(compactSource, mc)
	if len(compactInput) == 0 {
		return nil, ""
	}

	summary := fcp._generateSummary(ctx, compactInput, mc)
	if summary == "" {
		logger.Warn(logger.ComponentAgentCore).Msg("[FullCompact] full_compact summary generation returned empty content")
		return nil, ""
	}

	messagesToKeep := fcp._selectMessagesToKeep(activeMessages)
	summaryMessage := llm_schema.NewUserMessage(fcp._buildSummaryMessage(summary, len(messagesToKeep) > 0))
	boundary := llm_schema.NewSystemMessage(fcp.fcpConfig.Marker + "\nConversation compacted")

	newContextMessages := make([]llm_schema.BaseMessage, 0, len(prefix)+2+len(messagesToKeep)+4)
	newContextMessages = append(newContextMessages, prefix...)
	newContextMessages = append(newContextMessages, boundary, summaryMessage)
	newContextMessages = append(newContextMessages, messagesToKeep...)
	newContextMessages = append(newContextMessages, fcp.buildReinjectedStateMessages(ctx, mc, activeMessages, messagesToKeep)...)

	return newContextMessages, summary
}

// _buildSessionMemoryMessages 构建 Session Memory 路径消息。
//
// ⤵️ 5.31 回填：当前 Session Memory 路径返回 nil（不可用）
//
// 对应 Python: FullCompactProcessor._build_session_memory_messages()
func (fcp *FullCompactProcessor) _buildSessionMemoryMessages(ctx context.Context, mc iface.ModelContext, prefix []llm_schema.BaseMessage, activeMessages []llm_schema.BaseMessage, hasBoundary bool) ([]llm_schema.BaseMessage, *llm_schema.UserMessage) {
	if !fcp.fcpConfig.SessionMemoryEnabled {
		logger.Info(logger.ComponentAgentCore).Msg("[FullCompact] session_memory disabled")
		return nil, nil
	}

	sessionMemoryRuntime := fcp._loadSessionMemoryRuntime(ctx, mc)
	if sessionMemoryRuntime != nil {
		if isExtracting, _ := sessionMemoryRuntime["is_extracting"].(bool); isExtracting {
			logger.Info(logger.ComponentAgentCore).Msg("[FullCompact] session_memory extraction in progress, using latest committed notes")
		}
	}

	sessionMemoryText := fcp._loadSessionMemoryText(ctx, mc, sessionMemoryRuntime)
	if sessionMemoryText == "" {
		logger.Info(logger.ComponentAgentCore).Msg("[FullCompact] session_memory unavailable: empty notes content or unresolved path")
		return nil, nil
	}

	preservedMessages := fcp._selectMessagesAfterSessionMemory(activeMessages, sessionMemoryRuntime, hasBoundary)
	if preservedMessages == nil {
		logger.Info(logger.ComponentAgentCore).
			Bool("has_boundary", hasBoundary).
			Int("active", len(activeMessages)).
			Msg("[FullCompact] session_memory skipped: no valid active anchor")
		return nil, nil
	}

	boundary := llm_schema.NewSystemMessage(fcp.fcpConfig.SessionMemoryMarker + "\nEarlier conversation replaced with session memory")
	sessionMemoryMessage := llm_schema.NewUserMessage(fcp._buildSessionMemoryMessage(sessionMemoryText, len(preservedMessages) > 0))

	candidateMessages := make([]llm_schema.BaseMessage, 0, len(prefix)+2+len(preservedMessages)+4)
	candidateMessages = append(candidateMessages, prefix...)
	candidateMessages = append(candidateMessages, boundary, sessionMemoryMessage)
	candidateMessages = append(candidateMessages, preservedMessages...)
	candidateMessages = append(candidateMessages, fcp.buildReinjectedStateMessages(ctx, mc, activeMessages, preservedMessages)...)

	return candidateMessages, sessionMemoryMessage
}

// _splitMessagesAtCompactionBoundary 在压缩边界处分割消息列表。
//
// 对应 Python: FullCompactProcessor._split_messages_at_compaction_boundary()
func (fcp *FullCompactProcessor) _splitMessagesAtCompactionBoundary(messages []llm_schema.BaseMessage, boundaryIndex int) ([]llm_schema.BaseMessage, []llm_schema.BaseMessage) {
	if boundaryIndex > 0 {
		prefix := make([]llm_schema.BaseMessage, boundaryIndex)
		copy(prefix, messages[:boundaryIndex])
		activeMessages := messages[boundaryIndex+1:]
		return prefix, activeMessages
	}
	if boundaryIndex == 0 {
		return nil, messages[1:]
	}
	return nil, messages
}

// _generateSummary 调用 LLM 生成摘要，失败时回退到 _buildFallbackSummary。
//
// 对应 Python: FullCompactProcessor._generate_summary()
func (fcp *FullCompactProcessor) _generateSummary(ctx context.Context, messages []llm_schema.BaseMessage, mc iface.ModelContext) string {
	if fcp.model == nil {
		return fcp._buildFallbackSummary(messages)
	}

	promptMessages := []llm_schema.BaseMessage{
		llm_schema.NewSystemMessage(baseCompactPrompt),
		llm_schema.NewUserMessage(fcp._serializeMessages(messages)),
	}

	response, err := fcp.model.Invoke(ctx, model_clients.NewMessagesParam(promptMessages...))
	if err != nil {
		logger.Warn(logger.ComponentAgentCore).
			Err(err).
			Msg("[FullCompact] LLM summary generation failed, falling back")
		return fcp._buildFallbackSummary(messages)
	}

	fcp.RecordCompressionUsage(response)
	content := strings.TrimSpace(response.GetContent().Text())
	if content == "" {
		logger.Warn(logger.ComponentAgentCore).Msg("[FullCompact] LLM returned empty summary, falling back")
		return fcp._buildFallbackSummary(messages)
	}
	return _formatSummary(content)
}

// _truncateForPromptBudget 按 API 轮次分组从前往后丢弃，使 prompt 适配 Token 预算。
//
// 对应 Python: FullCompactProcessor._truncate_for_prompt_budget()
func (fcp *FullCompactProcessor) _truncateForPromptBudget(messages []llm_schema.BaseMessage, mc iface.ModelContext) []llm_schema.BaseMessage {
	groups := fcp._groupMessagesByAPIRound(messages)
	for len(groups) > 0 {
		candidate := processor.FlattenGroups(groups)
		if fcp._countPromptTokens(candidate, mc) <= fcp.fcpConfig.CompressionCallMaxTokens {
			return candidate
		}
		if len(groups) == 1 {
			return fcp._truncateMessagesFromHead(candidate, mc)
		}
		groups = groups[1:]
		if len(groups) > 0 {
			if _, ok := groups[0][0].(*llm_schema.AssistantMessage); ok {
				synthetic := llm_schema.NewUserMessage(fcp.fcpConfig.SyntheticUserMarker)
				groups[0] = append([]llm_schema.BaseMessage{synthetic}, groups[0]...)
			}
		}
	}
	return fcp._buildMinimalCompactInput(messages)
}

// _truncateMessagesFromHead 从头部逐条移除消息直到适配 Token 预算。
//
// 对应 Python: FullCompactProcessor._truncate_messages_from_head()
func (fcp *FullCompactProcessor) _truncateMessagesFromHead(messages []llm_schema.BaseMessage, mc iface.ModelContext) []llm_schema.BaseMessage {
	candidate := make([]llm_schema.BaseMessage, len(messages))
	copy(candidate, messages)

	for len(candidate) > 0 {
		if fcp._countPromptTokens(candidate, mc) <= fcp.fcpConfig.CompressionCallMaxTokens {
			return candidate
		}
		if fcp._isSyntheticMarkerMessage(candidate[0]) {
			if len(candidate) == 1 {
				return fcp._buildMinimalCompactInput(messages)
			}
			candidate = candidate[2:]
		} else {
			candidate = candidate[1:]
		}
		if len(candidate) > 0 {
			if _, ok := candidate[0].(*llm_schema.AssistantMessage); ok {
				synthetic := llm_schema.NewUserMessage(fcp.fcpConfig.SyntheticUserMarker)
				candidate = append([]llm_schema.BaseMessage{synthetic}, candidate...)
			}
		}
	}
	return fcp._buildMinimalCompactInput(messages)
}

// _groupMessagesByAPIRound 按已完成 API 轮次分组。
//
// 对应 Python: FullCompactProcessor._group_messages_by_api_round()
func (fcp *FullCompactProcessor) _groupMessagesByAPIRound(messages []llm_schema.BaseMessage) [][]llm_schema.BaseMessage {
	groups := processor.GroupCompletedAPIRoundsMessages(messages)
	return groups
}

// _buildMinimalCompactInput 构建最小压缩输入（仅保留最后一条消息）。
//
// 对应 Python: FullCompactProcessor._build_minimal_compact_input()
func (fcp *FullCompactProcessor) _buildMinimalCompactInput(messages []llm_schema.BaseMessage) []llm_schema.BaseMessage {
	if len(messages) == 0 {
		return nil
	}
	tail := []llm_schema.BaseMessage{messages[len(messages)-1]}
	if _, ok := tail[0].(*llm_schema.AssistantMessage); ok {
		return []llm_schema.BaseMessage{
			llm_schema.NewUserMessage(fcp.fcpConfig.SyntheticUserMarker),
			tail[0],
		}
	}
	return tail
}

// _selectMessagesToKeep 保留最近 MessagesToKeep 条消息。
//
// 对应 Python: FullCompactProcessor._select_messages_to_keep()
func (fcp *FullCompactProcessor) _selectMessagesToKeep(messages []llm_schema.BaseMessage) []llm_schema.BaseMessage {
	keepRecent := fcp.fcpConfig.MessagesToKeep
	if keepRecent <= 0 || len(messages) == 0 {
		return nil
	}

	startIndex := len(messages) - keepRecent
	if startIndex < 0 {
		startIndex = 0
	}
	if fcp.fcpConfig.KeepToolMessagePairs {
		startIndex = fcp._adjustStartIndexForToolPairs(messages, startIndex)
	}
	return messages[startIndex:]
}

// _adjustStartIndexForToolPairs 调整起始索引以包含 ToolMessage 对应的 AssistantMessage。
//
// 对应 Python: FullCompactProcessor._adjust_start_index_for_tool_pairs()
func (fcp *FullCompactProcessor) _adjustStartIndexForToolPairs(messages []llm_schema.BaseMessage, startIndex int) int {
	if startIndex <= 0 || startIndex >= len(messages) {
		return startIndex
	}

	adjusted := startIndex

	// 收集保留范围内 ToolMessage 的 tool_call_id
	neededToolIDs := make(map[string]bool)
	for _, msg := range messages[startIndex:] {
		if tm, ok := msg.(*llm_schema.ToolMessage); ok && tm.ToolCallID != "" {
			neededToolIDs[tm.ToolCallID] = true
		}
	}
	if len(neededToolIDs) == 0 {
		return adjusted
	}

	// 收集保留范围内已有的 tool_call_id
	presentToolCalls := make(map[string]bool)
	for _, msg := range messages[startIndex:] {
		if am, ok := msg.(*llm_schema.AssistantMessage); ok {
			for _, tc := range am.ToolCalls {
				if tc.ID != "" {
					presentToolCalls[tc.ID] = true
				}
			}
		}
	}

	// 计算缺失的 tool_call_id
	missingToolCalls := make(map[string]bool)
	for id := range neededToolIDs {
		if !presentToolCalls[id] {
			missingToolCalls[id] = true
		}
	}
	if len(missingToolCalls) == 0 {
		return adjusted
	}

	// 从 startIndex-1 向前搜索包含缺失 tool_call_id 的 AssistantMessage
	for idx := startIndex - 1; idx >= 0; idx-- {
		msg := messages[idx]
		am, ok := msg.(*llm_schema.AssistantMessage)
		if !ok {
			continue
		}
		matched := false
		for _, tc := range am.ToolCalls {
			if missingToolCalls[tc.ID] {
				delete(missingToolCalls, tc.ID)
				matched = true
			}
		}
		if matched {
			adjusted = idx
		}
		if len(missingToolCalls) == 0 {
			break
		}
	}
	return adjusted
}

// _prepareMessagesForPrompt 过滤掉 boundary/state/session_memory 标记消息。
//
// 对应 Python: FullCompactProcessor._prepare_messages_for_prompt()
func (fcp *FullCompactProcessor) _prepareMessagesForPrompt(messages []llm_schema.BaseMessage) []llm_schema.BaseMessage {
	result := make([]llm_schema.BaseMessage, 0, len(messages))
	for _, msg := range messages {
		if fcp._isBoundaryMessage(msg) || fcp._isStateMessage(msg) || fcp._isSessionMemoryBoundaryMessage(msg) {
			continue
		}
		result = append(result, msg)
	}
	return result
}

// _buildSummaryMessage 构建摘要消息文本。
//
// 对应 Python: FullCompactProcessor._build_summary_message()
func (fcp *FullCompactProcessor) _buildSummaryMessage(summary string, hasPreservedMessages bool) string {
	parts := []string{fcp.fcpConfig.SummaryIntro, "", summary}
	if hasPreservedMessages {
		parts = append(parts, "", fcp.fcpConfig.RecentMessagesNotice)
	}
	return strings.Join(parts, "\n")
}

// _buildSessionMemoryMessage 构建 Session Memory 消息文本。
//
// 对应 Python: FullCompactProcessor._build_session_memory_message()
func (fcp *FullCompactProcessor) _buildSessionMemoryMessage(sessionMemoryText string, hasPreservedMessages bool) string {
	parts := []string{fcp.fcpConfig.SessionMemoryIntro, "", strings.TrimSpace(sessionMemoryText)}
	if hasPreservedMessages {
		parts = append(parts, "", fcp.fcpConfig.RecentMessagesNotice)
	}
	return strings.Join(parts, "\n")
}

// _loadSessionMemoryRuntime 加载 Session Memory 运行时信息。
//
// 对应 Python: FullCompactProcessor._load_session_memory_runtime()
func (fcp *FullCompactProcessor) _loadSessionMemoryRuntime(_ context.Context, mc iface.ModelContext) map[string]any {
	sess := mc.GetSessionRef()
	if sess == nil {
		return nil
	}
	return cecontext.GetSessionMemoryRuntime(sess)
}

// _loadSessionMemoryText 加载 Session Memory 文本内容。
//
// 对应 Python: FullCompactProcessor._load_session_memory_text()
func (fcp *FullCompactProcessor) _loadSessionMemoryText(_ context.Context, mc iface.ModelContext, runtime map[string]any) string {
	memoryPath, _ := runtime["memory_path"].(string)
	if memoryPath == "" {
		return ""
	}
	data, err := os.ReadFile(memoryPath)
	if err != nil {
		return ""
	}
	return string(data)
}

// _resolveSessionMemoryPath 解析 Session Memory 文件路径。
//
// 对应 Python: FullCompactProcessor._resolve_session_memory_path()
func (fcp *FullCompactProcessor) _resolveSessionMemoryPath(_ context.Context, mc iface.ModelContext, _ map[string]any) string {
	workspaceDir := mc.WorkspaceDir()
	if workspaceDir == "" {
		return ""
	}
	return cecontext.GetSessionMemoryPath(workspaceDir, mc.SessionID())
}

// _selectMessagesAfterSessionMemory 选择 Session Memory 之后的消息。
//
// 对应 Python: FullCompactProcessor._select_messages_after_session_memory()
func (fcp *FullCompactProcessor) _selectMessagesAfterSessionMemory(messages []llm_schema.BaseMessage, runtime map[string]any, _ bool) []llm_schema.BaseMessage {
	notesUptoID, _ := runtime["notes_upto_message_id"].(string)
	if notesUptoID == "" {
		return messages
	}
	idx := cecontext.FindMessageIndexByContextMessageID(messages, notesUptoID)
	if idx < 0 {
		return messages
	}
	if idx+1 >= len(messages) {
		return nil
	}
	return messages[idx+1:]
}

// _invalidateSessionMemoryAnchor 使 Session Memory 锚点失效。
//
// 对应 Python: FullCompactProcessor._invalidate_session_memory_anchor()
func (fcp *FullCompactProcessor) _invalidateSessionMemoryAnchor(_ context.Context, mc iface.ModelContext) {
	sess := mc.GetSessionRef()
	if sess == nil {
		return
	}
	cecontext.InvalidateSessionMemoryAnchor(sess)
}

// buildReinjectedStateMessages 构建重新注入的状态消息。
//
// 遍历注册的 Builder，将非空结果加入最终消息序列。
//
// 对应 Python: FullCompactProcessor.build_reinjected_state_messages()
func (fcp *FullCompactProcessor) buildReinjectedStateMessages(ctx context.Context, mc iface.ModelContext, sourceMessages []llm_schema.BaseMessage, messagesToKeep []llm_schema.BaseMessage) []llm_schema.BaseMessage {
	candidateMessages := fcp._prepareMessagesForPrompt(sourceMessages)
	if len(candidateMessages) == 0 {
		return nil
	}

	var stateMessages []llm_schema.BaseMessage
	for _, spec := range fcp.reinjector.IterBuilders() {
		content := spec.Builder(ctx, fcp, mc, candidateMessages, messagesToKeep)
		if content == nil {
			continue
		}
		switch v := content.(type) {
		case []llm_schema.BaseMessage:
			stateMessages = append(stateMessages, v...)
		case string:
			if v != "" {
				stateMessages = append(stateMessages, fcp._makeStateMessage(spec.Label, v))
			}
		}
	}
	return stateMessages
}

// _makeStateMessage 构建状态 UserMessage。
//
// 对应 Python: FullCompactProcessor._make_state_message()
func (fcp *FullCompactProcessor) _makeStateMessage(label, content string) *llm_schema.UserMessage {
	compactContent := fcp.TruncateStateText(content)
	return llm_schema.NewUserMessage(fcp.fcpConfig.StateMarker + "\n[" + label + "]\n" + compactContent)
}

// countContextWindowTokens 计算上下文窗口 Token 数。
//
// 对应 Python: FullCompactProcessor._count_context_window_tokens()
func (fcp *FullCompactProcessor) countContextWindowTokens(mc iface.ModelContext, messages []llm_schema.BaseMessage) int {
	tokenCounter := mc.TokenCounter()
	if tokenCounter != nil {
		count, err := tokenCounter.CountMessages(messages, "")
		if err == nil {
			return count
		}
		logger.Warn(logger.ComponentAgentCore).
			Str("processor_type", fcp.ProcessorType()).
			Err(err).
			Msg("TokenCounter 返回错误，降级为字符估算")
	}
	total := 0
	for _, msg := range messages {
		total += processor.EstimateContentTokens(msg.GetContent().Text())
	}
	return total
}

// _countPromptTokens 计算含 BASE_COMPACT_PROMPT 的 prompt token 数。
//
// 对应 Python: FullCompactProcessor._count_prompt_tokens()
func (fcp *FullCompactProcessor) _countPromptTokens(messages []llm_schema.BaseMessage, mc iface.ModelContext) int {
	promptMessages := []llm_schema.BaseMessage{
		llm_schema.NewSystemMessage(baseCompactPrompt),
		llm_schema.NewUserMessage(fcp._serializeMessages(messages)),
	}
	tokenCounter := mc.TokenCounter()
	if tokenCounter != nil {
		count, err := tokenCounter.CountMessages(promptMessages, "")
		if err == nil {
			return count
		}
	}
	total := 0
	for _, msg := range promptMessages {
		total += processor.EstimateContentTokens(msg.GetContent().Text())
	}
	return total
}

// _buildFallbackSummary 构建降级摘要（最近 20 条消息序列化）。
//
// 对应 Python: FullCompactProcessor._build_fallback_summary()
func (fcp *FullCompactProcessor) _buildFallbackSummary(messages []llm_schema.BaseMessage) string {
	tail := messages
	startIdx := 1
	if len(tail) > 20 {
		tail = tail[len(tail)-20:]
		startIdx = len(messages) - 19
	}
	var lines []string
	for idx, msg := range tail {
		lines = append(lines, fmt.Sprintf("[%d] %s: %s", startIdx+idx, msg.GetRole().String(), processor.MessageToText(msg)))
	}
	return "Summary:\n" + strings.Join(lines, "\n")
}

// _formatSummary 提取 <summary> 标签内容，先去除 <analysis>...</analysis>。
//
// 对应 Python: FullCompactProcessor._format_summary()
func _formatSummary(content string) string {
	stripped := analysisRegex.ReplaceAllString(content, "")
	stripped = strings.TrimSpace(stripped)
	match := summaryRegex.FindStringSubmatch(stripped)
	if len(match) > 1 {
		return "Summary:\n" + strings.TrimSpace(match[1])
	}
	return stripped
}

// _serializeMessages 逐条序列化消息，换行连接。
//
// 对应 Python: FullCompactProcessor._serialize_messages()
func (fcp *FullCompactProcessor) _serializeMessages(messages []llm_schema.BaseMessage) string {
	var lines []string
	for _, msg := range messages {
		lines = append(lines, fcp._serializeMessage(msg))
	}
	return strings.Join(lines, "\n")
}

// _serializeMessage 完整序列化单条消息。
//
// 包含 role + tool_calls JSON（含 id/name/arguments/type）+ tool_call_id + content
//
// 对应 Python: FullCompactProcessor._serialize_message()
func (fcp *FullCompactProcessor) _serializeMessage(msg llm_schema.BaseMessage) string {
	var parts []string
	parts = append(parts, fmt.Sprintf("role=%s", msg.GetRole().String()))

	if am, ok := msg.(*llm_schema.AssistantMessage); ok && len(am.ToolCalls) > 0 {
		serializedToolCalls := make([]map[string]string, 0, len(am.ToolCalls))
		for _, tc := range am.ToolCalls {
			serializedToolCalls = append(serializedToolCalls, map[string]string{
				"id":        tc.ID,
				"name":      tc.Name,
				"arguments": tc.Arguments,
				"type":      tc.Type,
			})
		}
		tcJSON, _ := json.Marshal(serializedToolCalls)
		parts = append(parts, fmt.Sprintf("tool_calls=%s", string(tcJSON)))
	}

	if tm, ok := msg.(*llm_schema.ToolMessage); ok && tm.ToolCallID != "" {
		parts = append(parts, fmt.Sprintf("tool_call_id=%s", tm.ToolCallID))
	}

	parts = append(parts, fmt.Sprintf("content=%s", processor.MessageToText(msg)))
	return strings.Join(parts, " | ")
}

// _findLastCompactionBoundaryIndex 从后找 boundary 或 sessionMemoryBoundary 消息索引。
//
// 对应 Python: FullCompactProcessor._find_last_compaction_boundary_index()
func (fcp *FullCompactProcessor) _findLastCompactionBoundaryIndex(messages []llm_schema.BaseMessage) int {
	for idx := len(messages) - 1; idx >= 0; idx-- {
		if fcp._isBoundaryMessage(messages[idx]) || fcp._isSessionMemoryBoundaryMessage(messages[idx]) {
			return idx
		}
	}
	return -1
}

// _isBoundaryMessage 判断是否为压缩边界消息。
//
// SystemMessage && Content 以 Marker 开头
func (fcp *FullCompactProcessor) _isBoundaryMessage(msg llm_schema.BaseMessage) bool {
	_, ok := msg.(*llm_schema.SystemMessage)
	return ok && strings.HasPrefix(msg.GetContent().Text(), fcp.fcpConfig.Marker)
}

// _isStateMessage 判断是否为状态消息。
//
// UserMessage && Content 以 StateMarker 开头
func (fcp *FullCompactProcessor) _isStateMessage(msg llm_schema.BaseMessage) bool {
	_, ok := msg.(*llm_schema.UserMessage)
	return ok && strings.HasPrefix(msg.GetContent().Text(), fcp.fcpConfig.StateMarker)
}

// _isSessionMemoryBoundaryMessage 判断是否为 Session Memory 边界消息。
//
// SystemMessage && Content 以 SessionMemoryMarker 开头
func (fcp *FullCompactProcessor) _isSessionMemoryBoundaryMessage(msg llm_schema.BaseMessage) bool {
	_, ok := msg.(*llm_schema.SystemMessage)
	return ok && strings.HasPrefix(msg.GetContent().Text(), fcp.fcpConfig.SessionMemoryMarker)
}

// _isSessionMemorySummaryMessage 判断是否为 Session Memory 摘要消息。
//
// UserMessage && Content 以 SessionMemoryIntro 开头
func (fcp *FullCompactProcessor) _isSessionMemorySummaryMessage(msg llm_schema.BaseMessage) bool {
	_, ok := msg.(*llm_schema.UserMessage)
	return ok && strings.HasPrefix(msg.GetContent().Text(), fcp.fcpConfig.SessionMemoryIntro)
}

// _isSyntheticMarkerMessage 判断是否为合成标记消息。
//
// UserMessage && Content == SyntheticUserMarker
func (fcp *FullCompactProcessor) _isSyntheticMarkerMessage(msg llm_schema.BaseMessage) bool {
	_, ok := msg.(*llm_schema.UserMessage)
	return ok && msg.GetContent().Text() == fcp.fcpConfig.SyntheticUserMarker
}

// _buildHeadTailTruncatedText 头尾保留截断文本。
//
// 对应 Python: FullCompactProcessor._build_head_tail_truncated_text()
func _buildHeadTailTruncatedText(text string, keptChars int) string {
	if keptChars <= 0 {
		return "...[TRUNCATED]..."
	}
	headChars := keptChars * 20 / 100 // 20%
	if headChars < 0 {
		headChars = 0
	}
	tailChars := keptChars - headChars
	if tailChars < 0 {
		tailChars = 0
	}
	head := text[:headChars]
	var tail string
	if tailChars > 0 {
		tail = text[len(text)-tailChars:]
	}
	if head != "" && tail != "" {
		return head + "\n...[TRUNCATED]...\n" + tail
	}
	if head != "" {
		return head
	}
	if tail != "" {
		return tail
	}
	return "...[TRUNCATED]..."
}

// buildSkillReinjectedContent 构建技能重新注入内容。
//
// 遍历已完成 API round，找含 skill 文件读取的轮次，
// 最多 ReinjectRecentSkills 个，提取 skill 内容+工具调用描述。
//
// 对应 Python: build_skill_reinjected_content()
func buildSkillReinjectedContent(_ context.Context, fcp *FullCompactProcessor, _ iface.ModelContext, messages []llm_schema.BaseMessage, messagesToKeep []llm_schema.BaseMessage) any {
	keepSigs := make(map[string]bool)
	for _, msg := range messagesToKeep {
		keepSigs[processor.MessageSignature(msg)] = true
	}

	rounds := processor.GroupCompletedAPIRoundsMessages(messages)
	var selectedRounds [][]llm_schema.BaseMessage
	seenRoundSigs := make(map[string]bool)

	for i := len(rounds) - 1; i >= 0; i-- {
		roundMsgs := rounds[i]
		roundSig := processor.RoundSignature(roundMsgs)
		if seenRoundSigs[roundSig] {
			continue
		}
		// 检查轮次中的消息是否在 keep 集合中
		overlap := false
		for _, msg := range roundMsgs {
			if keepSigs[processor.MessageSignature(msg)] {
				overlap = true
				break
			}
		}
		if overlap {
			continue
		}
		if !processor.RoundContainsSkillRead(roundMsgs) {
			continue
		}
		selectedRounds = append(selectedRounds, roundMsgs)
		seenRoundSigs[roundSig] = true
		// 达到 ReinjectRecentSkills 上限时提前终止
		if len(selectedRounds) >= fcp.fcpConfig.ReinjectRecentSkills {
			break
		}
	}

	// 反转顺序（因为是从后往前选的）
	for i, j := 0, len(selectedRounds)-1; i < j; i, j = i+1, j-1 {
		selectedRounds[i], selectedRounds[j] = selectedRounds[j], selectedRounds[i]
	}

	// 构建重新注入消息，使用 processor 的 StateMarker 和 TruncateStateText
	// 对应 Python: UserMessage(content=f"{processor.state_marker}\n[SKILLS]\n{processor.truncate_state_text(serialized_round)}")
	var reinjectedMessages []llm_schema.BaseMessage
	for _, roundMsgs := range selectedRounds {
		var serializedParts []string
		for _, msg := range roundMsgs {
			serializedParts = append(serializedParts, fmt.Sprintf("role=%s, content=%s", msg.GetRole().String(), processor.MessageToText(msg)))
		}
		serialized := strings.Join(serializedParts, "\n")
		// 使用 processor 的 TruncateStateText 截断过长内容
		truncated := fcp.TruncateStateText(serialized)
		reinjectedMessages = append(reinjectedMessages, llm_schema.NewUserMessage(
			fcp.fcpConfig.StateMarker+"\n[SKILLS]\n"+truncated,
		))
	}
	return reinjectedMessages
}

// buildTaskStatusReinjectedContent 构建任务状态重新注入内容。
//
// 对应 Python: build_task_status_reinjected_content()
func buildTaskStatusReinjectedContent(_ context.Context, _ *FullCompactProcessor, mc iface.ModelContext, _ []llm_schema.BaseMessage, _ []llm_schema.BaseMessage) any {
	sess := mc.GetSessionRef()
	if sess == nil {
		return ""
	}
	st, err := sess.GetState(state.StringKey("task_status"))
	if err != nil || st == nil {
		return ""
	}
	data, err := json.Marshal(st)
	if err != nil {
		return ""
	}
	return string(data)
}

// buildPlanModeReinjectedContent 构建计划模式重新注入内容。
//
// 对应 Python: build_plan_mode_reinjected_content()
func buildPlanModeReinjectedContent(_ context.Context, _ *FullCompactProcessor, mc iface.ModelContext, _ []llm_schema.BaseMessage, _ []llm_schema.BaseMessage) any {
	sess := mc.GetSessionRef()
	if sess == nil {
		return ""
	}
	st, err := sess.GetState(state.StringKey("plan_mode"))
	if err != nil || st == nil {
		return ""
	}
	data, err := json.Marshal(st)
	if err != nil {
		return ""
	}
	return string(data)
}

// buildPlanReinjectedContent 构建计划重新注入内容，空实现。
//
// 对应 Python: build_plan_reinjected_content()
func buildPlanReinjectedContent(_ context.Context, _ *FullCompactProcessor, _ iface.ModelContext, _ []llm_schema.BaseMessage, _ []llm_schema.BaseMessage) any {
	return ""
}

// init 自动注册到 context_engine 注册表
func init() {
	context_engine.RegisterProcessorFactory("FullCompactProcessor",
		func(config iface.ProcessorConfig) (iface.ContextProcessor, error) {
			cfg, ok := config.(*FullCompactProcessorConfig)
			if !ok {
				return nil, fmt.Errorf("FullCompactProcessor: 配置类型不匹配，期望 *FullCompactProcessorConfig，实际 %T", config)
			}
			fcp, err := NewFullCompactProcessor(cfg)
			if err != nil {
				return nil, err
			}
			return fcp, nil
		},
	)
}
