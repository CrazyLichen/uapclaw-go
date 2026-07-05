package offloader

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine"
	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/processor"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MessageSummaryOffloaderConfig 消息摘要卸载器配置。
//
// 始终使用自适应压缩生成上下文感知摘要。
// 触发基于新添加消息的角色和大小是否超过 largeMessageThreshold。
//
// 消息阈值、令牌阈值、保留消息数、保留最后轮次
// 为兼容字段，自适应压缩不使用这些参数进行触发或候选选择。
//
// 对应 Python: MessageSummaryOffloaderConfig (pydantic.BaseModel)
type MessageSummaryOffloaderConfig struct {
	// MessagesThreshold 兼容字段；自适应压缩不用于触发
	MessagesThreshold *int
	// TokensThreshold 兼容字段；自适应压缩用 per-message 检查（默认 20000）
	TokensThreshold int
	// LargeMessageThreshold 自适应压缩候选消息的最小大小（默认 1000）
	LargeMessageThreshold int
	// OffloadMessageTypes 可摘要卸载的消息角色列表（默认 ["tool"]）
	OffloadMessageTypes []string
	// ProtectedToolNames 受保护的工具名称列表（默认 ["reload_original_context_messages"]）
	ProtectedToolNames []string
	// MessagesToKeep 兼容字段
	MessagesToKeep *int
	// KeepLastRound 兼容字段（默认 true）
	KeepLastRound bool
	// Model 摘要模型请求配置
	Model *llm_schema.ModelRequestConfig
	// ModelClient 摘要模型客户端配置
	ModelClient *llm_schema.ModelClientConfig
	// SummaryMaxTokens 摘要最大 token 数（默认 900）
	SummaryMaxTokens int
	// EnablePreciseStep 启用 LLM 精确任务提取（默认 false）
	EnablePreciseStep bool
	// StepSummaryMaxContextMessages 任务提取上下文消息数（默认 8）
	StepSummaryMaxContextMessages int
	// ContentMaxCharsForCompression 压缩输入字符上限（默认 200000）
	ContentMaxCharsForCompression int
}

// MessageSummaryOffloader 消息摘要卸载器，基于 LLM 智能摘要替代简单裁剪。
//
//   - Per-message 触发：当新添加的合格消息超过大小阈值时触发
//   - 任务感知压缩：摘要时考虑当前任务上下文
//   - 降级机制：LLM 上下文溢出时用截断内容重试
//
// 对应 Python: openjiuwen/core/context_engine/processor/offloader/message_summary_offloader.py
type MessageSummaryOffloader struct {
	*processor.BaseProcessor
	// config 具体配置（类型断言获取）
	config *MessageSummaryOffloaderConfig
	// model LLM 模型实例，用于生成摘要
	model *llm.Model
}

// MessageSummaryOffloaderOption 摘要卸载器构造选项
type MessageSummaryOffloaderOption func(*MessageSummaryOffloader)

// ──────────────────────────── 常量 ────────────────────────────
const (
	// msoDefaultTokensThreshold 默认 Token 触发阈值
	msoDefaultTokensThreshold = 20000
	// msoDefaultLargeMessageThreshold 默认大消息判定阈值
	msoDefaultLargeMessageThreshold = 1000
	// msoDefaultSummaryMaxTokens 默认摘要最大 token 数
	msoDefaultSummaryMaxTokens = 900
	// msoDefaultStepSummaryMaxContextMessages 默认任务提取上下文消息数
	msoDefaultStepSummaryMaxContextMessages = 8
	// msoDefaultContentMaxCharsForCompression 默认压缩输入字符上限
	msoDefaultContentMaxCharsForCompression = 200000
)

// truncatedMarker 智能截断标记
const truncatedMarker = "...[TRUNCATED]..."

// adaptiveOffloadPromptTemplate 自适应压缩提示词模板
//
// 对应 Python: ADAPTIVE_OFFLOAD_PROMPT_TEMPLATE
const adaptiveOffloadPromptTemplate = `# Adaptive Information Compression Expert

## Core Role
You are an adaptive information compression expert in a React Agent. Your task is to intelligently analyze the information density and structural characteristics of tool return content, automatically select the most suitable compression strategy, generate an optimal condensed text, and offload detailed content to the file system for on-demand loading.

## Constraints
- **Strictly prohibited from executing the step**: You are only responsible for compression; you must not execute any steps, calculations, or operations from the step.
- **Based solely on provided information**: Only use the information in tool_content for compression.
- **No speculative operations**: Do not perform additional queries, calculations, or analysis based on step content.

# Compression Logic Flow

## Step 1: Analyze User Intent
- **Tool Purpose**: Understand the core purpose of this tool call (e.g., querying information, performing calculations, obtaining status).
- **Key Parameters**: What parameters were passed in the function_call? This directly indicates the focus of required information.
- **Role in the step**: What subtasks in the current step is this tool call meant to accomplish?

## Step 2: Select Compression Strategy
Based on the analyzed user intent, quickly scan the important information in tool_content:

### Characteristics favoring EXTRACTIVE compression:
- **Clear and direct results**: Key information related to user intent is explicitly present in the tool return results.
- **No deep processing needed**: The answer already exists directly in the return content; it only needs to be "extracted" to satisfy user intent without summarization or reasoning.
- **Clear structure**: For example, batches of key information, attribute lists, keyword collections, address details, etc.

### Characteristics favoring ABSTRACTIVE compression:
- **Requires integration and understanding**: To obtain an answer that matches user intent, it is necessary to summarize and synthesize multiple paragraphs, viewpoints, or data.
- **Highly narrative**: For example, long analytical reports, article content, Q&A responses, log analysis, etc.

## Step 3: Execute Compression Strategy
Based on the above evaluation, select a compression strategy according to the following process:

### **If EXTRACTIVE compression was selected in the previous step**:
Analyze ` + "`" + `tool_content` + "`" + ` and perform the following operations:
- **Identify core information**: Find sentences and key data that directly answer the calling intent.
- **Execute extractive compression**:
  - **RETAIN**: All original sentences or phrases that directly contain core answers, key facts, final results, main status, and necessary definitions. Prefer not to rewrite; use original expressions when possible.
  - **DELETE**:
    - Background introductions and process descriptions unrelated to the core answer.
    - Sentences that express the same meaning repeatedly.
    - Overly detailed examples and explanatory expansions (if their main points are already covered).
    - Pure formatting metadata, internal log information, redundant transitional statements.
- **Ensure coherence**: Connect the retained original sentences or fragments in a logically clear way to form coherent key information.

### **If ABSTRACTIVE compression was selected in the previous step**:
Compress the tool message content to generate a **high-density, high-integrity** summary that can adequately support the current ` + "`" + `step` + "`" + `'s task needs without loading the original text.

**Summary requirements:**
- **Integrity priority**: The summary should retain **all key facts, data, conclusions, conditions, and limitations** related to the current ` + "`" + `step` + "`" + ` from the original text. Do not omit information that substantially impacts understanding or decision-making.
- **Strict accuracy**: All data, names, relationships, and judgments must be strictly accurate; do not distort, blur, or simplify to the point of potential misunderstanding.
- **Focus and conciseness**: Center around the ` + "`" + `step` + "`" + ` requirements; organize in concise, clear language; remove redundant descriptions, repetitive examples, and irrelevant background buildup, but **do not oversimplify core information**.
- **Clear structure**: Maintain logical coherence; reasonably segment or bulletize to ensure clear information hierarchy and easy reading comprehension.
- **Objective neutrality**: Make only factual statements; do not add explanations, evaluations, or speculations not present in the original text.

【Current step requirements】
{step}

【Current tool call function call】
{function_call}

【Tool message content begins】
{tool_content}
【Tool message content ends】

Return JSON with this schema:
{output_json_schema}`

// outputJSONSchema 输出 JSON Schema 模板
//
// 对应 Python: OUTPUT_JSON_SCHEMA
const outputJSONSchema = `{
  "compression_strategy": "extractive" | "abstractive",
  "summary": "A compact result generated based on the selected strategy (within {summary_max_tokens} tokens). If using extractive strategy, directly concatenate key original text; if using abstractive strategy, provide a condensed summary. Ensure it contains all key information needed for the step, with clear structure and appropriate length.",
  "offload_data_explanation": {
    "category": "The category of information offloaded (e.g., 'raw log data', 'complete product list', 'detailed calculation steps')",
    "description": "Briefly describe what detailed information is missing from the compressed text and its potential use cases, for subsequent on-demand loading of these offloaded information.",
    "inferability": "high" | "medium" | "low"
  }
}`

// stepSummaryPrompt 任务提取提示词模板
//
// 对应 Python: STEP_SUMMARY_PROMPT
const stepSummaryPrompt = `Summarize the current user task in one concise sentence.
Return the task only.

Conversation context:
{context}`

// defaultOffloadSummaryPrompt 旧版摘要提示词（兼容旧序列化引用）
//
// 对应 Python: DEFAULT_OFFLOAD_SUMMARY_PROMPT
const defaultOffloadSummaryPrompt = `
    You are a "high-density summarizer".
    Your task is to shrink the overly long message below into 2–4 concise sentences that:
    Contain ≤ 15 % of the original token count;
    Keep all critical facts, figures, conclusions, requests or decisions verbatim;
    Remove greetings, repetition, filler, examples, jokes, and ornamental language;
    Speak in neutral, third-person style;
    Do NOT explain, comment, or add extra information—output the summary only.
    Begin:
    `

// ──────────────────────────── 全局变量 ────────────────────────────

// contextOverflowKeywords 上下文溢出关键词，不同模型服务商使用不同的错误格式
var contextOverflowKeywords = []string{
	"context length",
	"token limit",
	"too long",
	"exceeds",
	"maximum context",
	"context window",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMessageSummaryOffloader 创建消息摘要卸载器实例。
//
// 对应 Python: MessageSummaryOffloader.__init__(config)
func NewMessageSummaryOffloader(config *MessageSummaryOffloaderConfig, opts ...MessageSummaryOffloaderOption) (*MessageSummaryOffloader, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	bp := processor.NewBaseProcessor(config)
	mso := &MessageSummaryOffloader{
		BaseProcessor: bp,
		config:        config,
	}

	// 应用选项
	for _, opt := range opts {
		opt(mso)
	}

	// 如果未通过选项注入 Model，则从配置创建
	if mso.model == nil {
		if config.ModelClient == nil {
			return nil, fmt.Errorf("MessageSummaryOffloader: ModelClient 不能为空，摘要卸载需要 LLM 模型")
		}
		model, err := llm.NewModel(config.ModelClient, config.Model)
		if err != nil {
			return nil, err
		}
		mso.model = model
	}

	return mso, nil
}

// WithMessageSummaryModel 注入已有 Model 实例（测试用）。
func WithMessageSummaryModel(model *llm.Model) MessageSummaryOffloaderOption {
	return func(mso *MessageSummaryOffloader) { mso.model = model }
}

// Validate 校验消息摘要卸载器配置。
//
// 对应 Python: MessageSummaryOffloader._validate_config()
func (c *MessageSummaryOffloaderConfig) Validate() error {
	c.applyMSODefaults()
	if c.MessagesThreshold != nil && c.MessagesToKeep != nil {
		if *c.MessagesToKeep >= *c.MessagesThreshold {
			return fmt.Errorf("MessageSummaryOffloaderConfig.MessagesToKeep(%d) 不能大于等于 MessagesThreshold(%d)",
				*c.MessagesToKeep, *c.MessagesThreshold)
		}
	}
	return nil
}

// ProcessorType 返回处理器类型标识。
func (mso *MessageSummaryOffloader) ProcessorType() string { return "MessageSummaryOffloader" }

// TriggerAddMessages 判断是否需要介入消息添加。
//
// 触发条件：新添加的消息中，是否存在符合条件的消息（角色匹配 + 大小超阈值 + 非已卸载 + 非受保护工具）。
//
// 对应 Python: MessageSummaryOffloader.trigger_add_messages()
func (mso *MessageSummaryOffloader) TriggerAddMessages(_ context.Context, mc iface.ModelContext, messagesToAdd []llm_schema.BaseMessage, _ ...iface.Option) (bool, error) {
	contextMessages, _ := mc.GetMessages(0, true)
	allMessages := append(contextMessages, messagesToAdd...)
	for _, msg := range messagesToAdd {
		if mso.shouldOffloadMessage(msg, mc, allMessages) {
			return true, nil
		}
	}
	return false, nil
}

// OnAddMessages 执行消息摘要卸载。
//
// 只处理新添加的消息，不处理已有上下文。
//
// 对应 Python: MessageSummaryOffloader.on_add_messages()
func (mso *MessageSummaryOffloader) OnAddMessages(ctx context.Context, mc iface.ModelContext, messagesToAdd []llm_schema.BaseMessage, _ ...iface.Option) (*iface.ContextEvent, []llm_schema.BaseMessage, error) {
	processedMessages := make([]llm_schema.BaseMessage, len(messagesToAdd))
	copy(processedMessages, messagesToAdd)

	event := &iface.ContextEvent{
		EventType: mso.ProcessorType(),
	}
	baseIndex := mc.Len()

	for index, message := range messagesToAdd {
		allMsgs, _ := mc.GetMessages(0, true)
		allMessages := allMsgs
		allMessages = append(allMessages, messagesToAdd...)
		if !mso.shouldOffloadMessage(message, mc, allMessages) {
			continue
		}
		offloadedMsg, err := mso.offloadMessageAdaptive(ctx, message, mc)
		if err != nil {
			logger.Warn(logger.ComponentAgentCore).
				Str("processor_type", mso.ProcessorType()).
				Int("message_idx", baseIndex+index).
				Err(err).
				Msg("自适应摘要卸载失败，跳过")
			continue
		}
		if offloadedMsg == nil {
			continue
		}
		processedMessages[index] = offloadedMsg
		event.MessagesToModify = append(event.MessagesToModify, baseIndex+index)
	}

	if len(event.MessagesToModify) == 0 {
		return nil, messagesToAdd, nil
	}
	return event, processedMessages, nil
}

// SaveState 导出处理器内部状态（空操作）。
func (mso *MessageSummaryOffloader) SaveState() map[string]any {
	return make(map[string]any)
}

// LoadState 从 map 恢复处理器内部状态（空操作）。
func (mso *MessageSummaryOffloader) LoadState(_ map[string]any) {}

// ──────────────────────────── 非导出函数 ────────────────────────────

// applyMSODefaults 应用默认值
func (c *MessageSummaryOffloaderConfig) applyMSODefaults() {
	if c.TokensThreshold == 0 {
		c.TokensThreshold = msoDefaultTokensThreshold
	}
	if c.LargeMessageThreshold == 0 {
		c.LargeMessageThreshold = msoDefaultLargeMessageThreshold
	}
	if len(c.OffloadMessageTypes) == 0 {
		c.OffloadMessageTypes = []string{"tool"}
	}
	if len(c.ProtectedToolNames) == 0 {
		c.ProtectedToolNames = []string{"reload_original_context_messages"}
	}
	if c.SummaryMaxTokens == 0 {
		c.SummaryMaxTokens = msoDefaultSummaryMaxTokens
	}
	if c.StepSummaryMaxContextMessages == 0 {
		c.StepSummaryMaxContextMessages = msoDefaultStepSummaryMaxContextMessages
	}
	if c.ContentMaxCharsForCompression == 0 {
		c.ContentMaxCharsForCompression = msoDefaultContentMaxCharsForCompression
	}
}

// shouldOffloadMessage 判断消息是否符合摘要卸载条件。
//
// 规则（全部通过才卸载）：
//  1. 角色在 OffloadMessageTypes 中
//  2. 不是已卸载消息（Offloadable）
//  3. 不是受保护工具的结果
//  4. 大小超过 LargeMessageThreshold（优先 token 计数，回退 字符数//3）
//
// 对应 Python: MessageSummaryOffloader._should_offload_message()
func (mso *MessageSummaryOffloader) shouldOffloadMessage(message llm_schema.BaseMessage, mc iface.ModelContext, contextMessages []llm_schema.BaseMessage) bool {
	cfg := mso.config

	// 规则 1：角色检查
	roleMatch := false
	role := message.GetRole().String()
	for _, rt := range cfg.OffloadMessageTypes {
		if rt == role {
			roleMatch = true
			break
		}
	}
	if !roleMatch {
		return false
	}

	// 规则 2：已卸载消息不重复处理
	if schema.IsOffloaded(message) {
		return false
	}

	// 规则 3：受保护工具消息不卸载
	if mso.isProtectedToolMessage(message, contextMessages) {
		return false
	}

	// 规则 4：大小检查（token 计数优先，回退字符数//3）
	length := mso.messageSize(message, mc)
	return length > cfg.LargeMessageThreshold
}

// messageSize 计算消息大小用于阈值比较。
//
// 优先使用 TokenCounter，回退到 字符数//3。
//
// 对应 Python: MessageSummaryOffloader._message_size()
func (mso *MessageSummaryOffloader) messageSize(message llm_schema.BaseMessage, mc iface.ModelContext) int {
	tokenCounter := mc.TokenCounter()
	if tokenCounter != nil {
		count, _ := tokenCounter.CountMessages([]llm_schema.BaseMessage{message}, "")
		return count
	}
	content := message.GetContent().Text()
	if content != "" {
		return len(content) / 3
	}
	// 尝试 JSON 序列化多模态内容
	parts := message.GetContent().Parts()
	if len(parts) > 0 {
		if data, err := json.Marshal(parts); err == nil {
			return len(data) / 3
		}
	}
	return 0
}

// isProtectedToolMessage 检查消息是否为受保护工具的结果。
//
// 支持 "tool_name" 和 "tool_name:pattern" 两种格式。
//
// 对应 Python: MessageSummaryOffloader._is_protected_tool_message()
func (mso *MessageSummaryOffloader) isProtectedToolMessage(message llm_schema.BaseMessage, contextMessages []llm_schema.BaseMessage) bool {
	if message.GetRole() != llm_schema.RoleTypeTool {
		return false
	}
	toolCall := processor.ResolveToolCallFromMessage(message, contextMessages)
	if toolCall == nil {
		return false
	}
	toolName := processor.ExtractToolName(toolCall)
	toolArgs := msoExtractToolArgs(toolCall)

	for _, protected := range mso.config.ProtectedToolNames {
		if colonIdx := strings.Index(protected, ":"); colonIdx != -1 {
			protectedTool := protected[:colonIdx]
			protectedPattern := protected[colonIdx+1:]
			if toolName == protectedTool && msoMatchPattern(toolArgs, protectedPattern) {
				return true
			}
		} else {
			if toolName == protected {
				return true
			}
		}
	}
	return false
}

// msoExtractToolArgs 从 ToolCall 提取参数字典。
//
// 对应 Python: MessageOffloader._extract_tool_args()
func msoExtractToolArgs(toolCall *llm_schema.ToolCall) map[string]any {
	if toolCall == nil {
		return map[string]any{}
	}
	if toolCall.Arguments != "" {
		var parsed map[string]any
		if err := json.Unmarshal([]byte(toolCall.Arguments), &parsed); err == nil {
			return parsed
		}
	}
	return map[string]any{}
}

// msoMatchPattern 检查参数值是否匹配通配符模式。
//
// 对应 Python: MessageOffloader._match_pattern()
func msoMatchPattern(args map[string]any, pattern string) bool {
	for _, value := range args {
		if strVal, ok := value.(string); ok {
			matched, _ := filepath.Match(pattern, strVal)
			if matched {
				return true
			}
		}
	}
	return false
}

// newOffloadHandleAndPath 生成卸载句柄和文件路径。
//
// 对应 Python: MessageSummaryOffloader._new_offload_handle_and_path()
//
// ⤵️ 5.31 回填：mc.WorkspaceDir() 方法
func (mso *MessageSummaryOffloader) newOffloadHandleAndPath(mc iface.ModelContext) (string, string) {
	offloadHandle := uuid.New().String()
	sessionID := mc.SessionID()

	// ⤵️ 5.31 回填：使用 mc.WorkspaceDir() 获取工作目录
	workspaceDir := mc.WorkspaceDir()

	fileName := fmt.Sprintf("%s_%s.json", mso.ProcessorType(), offloadHandle)
	if workspaceDir != "" {
		return offloadHandle, filepath.Join(workspaceDir, "context", sessionID+"_context", "offload", fileName)
	}
	return offloadHandle, ""
}

// offloadMessageAdaptive 对消息执行自适应摘要卸载。
//
// 流程：
//  1. 提取函数调用信息
//  2. 提取当前任务步骤
//  3. 执行 LLM 压缩（含降级重试）
//  4. 构建卸载消息
//
// 对应 Python: MessageSummaryOffloader._offload_message_adaptive()
func (mso *MessageSummaryOffloader) offloadMessageAdaptive(ctx context.Context, message llm_schema.BaseMessage, mc iface.ModelContext) (llm_schema.BaseMessage, error) {
	contextMessages, _ := mc.GetMessages(0, true)
	functionCall := mso.getFunctionCallFromChain(message, contextMessages)

	var step string
	if mso.config.EnablePreciseStep {
		var err error
		step, err = mso.getStepFromChainPrecise(ctx, append(contextMessages, message))
		if err != nil {
			logger.Warn(logger.ComponentAgentCore).
				Str("processor_type", mso.ProcessorType()).
				Err(err).
				Msg("精确任务提取失败，回退到默认方式")
			step = mso.getStepFromChainDefault(contextMessages)
		}
		if step == "" {
			step = mso.getStepFromChainDefault(contextMessages)
		}
	} else {
		step = mso.getStepFromChainDefault(contextMessages)
	}

	// 提取工具内容
	toolContent := message.GetContent().Text()
	if toolContent == "" {
		parts := message.GetContent().Parts()
		if len(parts) > 0 {
			if data, err := json.Marshal(parts); err == nil {
				toolContent = string(data)
			} else {
				toolContent = fmt.Sprintf("%v", parts)
			}
		}
	}

	compressionResult, err := mso.compressWithFallback(ctx, step, functionCall, toolContent)
	if err != nil {
		return nil, err
	}
	if compressionResult == nil {
		// 压缩结果比原文更长，保留原始消息
		return message, nil
	}

	summary, _ := compressionResult["summary"].(string)
	finalContent := summary

	offloadDataExplanation, _ := compressionResult["offload_data_explanation"].(map[string]any)
	if len(offloadDataExplanation) > 0 {
		explanationLines := []string{
			"[offloaded_info]",
			fmt.Sprintf("category: %v", offloadDataExplanation["category"]),
			fmt.Sprintf("description: %v", offloadDataExplanation["description"]),
			fmt.Sprintf("inferability: %v", offloadDataExplanation["inferability"]),
		}
		finalContent = summary + "\n\n" + strings.Join(explanationLines, "\n")
	}

	offloadHandle, offloadPath := mso.newOffloadHandleAndPath(mc)

	offloadMsg, err := mso.OffloadMessages(
		ctx, mc,
		message.GetRole().String(),
		finalContent,
		[]llm_schema.BaseMessage{message},
		iface.WithOffloadHandle(offloadHandle),
		iface.WithOffloadPath(offloadPath),
	)
	if err != nil {
		return nil, err
	}
	return offloadMsg, nil
}

// getFunctionCallFromChain 从上下文链中提取触发此工具消息的函数调用。
//
// 回溯上下文查找匹配的 AssistantMessage，通过 ToolCallID 关联。
//
// 对应 Python: MessageSummaryOffloader._get_function_call_from_chain()
func (mso *MessageSummaryOffloader) getFunctionCallFromChain(toolMessage llm_schema.BaseMessage, contextMessages []llm_schema.BaseMessage) *llm_schema.ToolCall {
	return processor.ResolveToolCallFromMessage(toolMessage, contextMessages)
}

// getStepFromChainDefault 启发式提取当前任务（最后一条 UserMessage）。
//
// 对应 Python: MessageSummaryOffloader._get_step_from_chain_default()
func (mso *MessageSummaryOffloader) getStepFromChainDefault(contextMessages []llm_schema.BaseMessage) string {
	for i := len(contextMessages) - 1; i >= 0; i-- {
		if contextMessages[i].GetRole() == llm_schema.RoleTypeUser {
			content := contextMessages[i].GetContent().Text()
			if content != "" {
				return content
			}
			// 多模态内容序列化
			parts := contextMessages[i].GetContent().Parts()
			if len(parts) > 0 {
				if data, err := json.Marshal(parts); err == nil {
					return string(data)
				}
				return fmt.Sprintf("%v", parts)
			}
		}
	}
	return ""
}

// getStepFromChainPrecise 使用 LLM 精确提取当前任务。
//
// 包含降级逻辑：LLM 上下文溢出时缩减上下文重试。
//
// 对应 Python: MessageSummaryOffloader._get_step_from_chain_precise()
func (mso *MessageSummaryOffloader) getStepFromChainPrecise(ctx context.Context, contextMessages []llm_schema.BaseMessage) (string, error) {
	messagesToUse := mso.selectMessagesForStepSummary(contextMessages)
	if messagesToUse == nil {
		return "", nil
	}

	maxRetries := 3
	for attempt := 0; attempt < maxRetries; attempt++ {
		contextText := mso.buildStepContextText(messagesToUse)
		prompt := strings.ReplaceAll(stepSummaryPrompt, "{context}", contextText)

		response, err := mso.model.Invoke(ctx, model_clients.NewTextMessagesParam(prompt))
		if err != nil {
			if mso.isContextOverflowError(err) {
				if attempt >= maxRetries-1 || len(messagesToUse) <= 2 {
					return "", exception.NewBaseError(
						exception.StatusContextExecutionError,
						exception.WithMsg(fmt.Sprintf("精确任务提取在 %d 次尝试后失败: %v", maxRetries, err)),
						exception.WithCause(err),
					)
				}
				messagesToUse = messagesToUse[2:]
				continue
			}
			return "", err
		}

		content := response.GetContent().Text()
		if content != "" {
			return strings.TrimSpace(content), nil
		}
		// 多模态响应
		parts := response.GetContent().Parts()
		if len(parts) > 0 {
			if data, err := json.Marshal(parts); err == nil {
				return strings.TrimSpace(string(data)), nil
			}
			return strings.TrimSpace(fmt.Sprintf("%v", parts)), nil
		}
		return "", nil
	}
	return "", nil
}

// selectMessagesForStepSummary 筛选用于任务提取的消息。
//
// 过滤规则：仅保留 user 消息和 assistant 消息（无 tool_calls）。
// 过滤后消息数 <= 1 时返回 nil（跳过精确提取，回退到默认方式）。
//
// 对应 Python: MessageSummaryOffloader._select_messages_for_step_summary()
func (mso *MessageSummaryOffloader) selectMessagesForStepSummary(contextMessages []llm_schema.BaseMessage) []llm_schema.BaseMessage {
	filtered := make([]llm_schema.BaseMessage, 0, len(contextMessages))
	for _, msg := range contextMessages {
		if mso.isValidForStepSummary(msg) {
			filtered = append(filtered, msg)
		}
	}
	if len(filtered) <= 1 {
		return nil
	}
	maxMessages := mso.config.StepSummaryMaxContextMessages
	if len(filtered) <= maxMessages {
		return filtered
	}
	return filtered[len(filtered)-maxMessages:]
}

// isValidForStepSummary 检查消息是否适用于任务提取。
//
// 有效消息：user 消息 或 assistant 消息（无 tool_calls）。
//
// 对应 Python: MessageSummaryOffloader._is_valid_for_step_summary()
func (mso *MessageSummaryOffloader) isValidForStepSummary(msg llm_schema.BaseMessage) bool {
	if msg.GetRole() == llm_schema.RoleTypeUser {
		return true
	}
	if msg.GetRole() == llm_schema.RoleTypeAssistant {
		// 检查是否有 tool_calls
		if am, ok := msg.(*llm_schema.AssistantMessage); ok {
			return len(am.ToolCalls) == 0
		}
	}
	return false
}

// buildStepContextText 构建任务提取的上下文文本
func (mso *MessageSummaryOffloader) buildStepContextText(messages []llm_schema.BaseMessage) string {
	var sb strings.Builder
	for i, msg := range messages {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		content := msg.GetContent().Text()
		if content == "" {
			content = fmt.Sprintf("%v", msg.GetContent().Parts())
		}
		// 截断到 2000 字符，对齐 Python
		if len(content) > 2000 {
			content = content[:2000]
		}
		fmt.Fprintf(&sb, "[%s] %s", msg.GetRole().String(), content)
	}
	return sb.String()
}

// compressWithFallback 执行带降级重试的 LLM 压缩。
//
// 构建多级内容（全文→截断→半截断），上下文溢出时自动降级重试。
//
// 对应 Python: MessageSummaryOffloader._compress_with_fallback()
func (mso *MessageSummaryOffloader) compressWithFallback(ctx context.Context, step string, functionCall *llm_schema.ToolCall, toolContent string) (map[string]any, error) {
	attempts := mso.buildCompressionAttempts(toolContent)

	for index, contentToCompress := range attempts {
		prompt := mso.buildCompressionPrompt(step, functionCall, contentToCompress)
		response, err := mso.model.Invoke(ctx, model_clients.NewTextMessagesParam(prompt))
		if err != nil {
			if mso.isContextOverflowError(err) {
				if index >= len(attempts)-1 {
					return nil, exception.NewBaseError(
						exception.StatusContextExecutionError,
						exception.WithMsg(fmt.Sprintf("消息压缩在 %d 次尝试后失败: %v", len(attempts), err)),
						exception.WithCause(err),
					)
				}
				continue
			}
			return nil, err
		}

		responseContent := response.GetContent().Text()
		if responseContent == "" {
			parts := response.GetContent().Parts()
			if len(parts) > 0 {
				if data, err := json.Marshal(parts); err == nil {
					responseContent = string(data)
				} else {
					responseContent = fmt.Sprintf("%v", parts)
				}
			}
		}

		result, parseErr := mso.parseCompressionResult(responseContent)
		if parseErr != nil {
			// 解析失败时：如果响应比原文长，放弃压缩
			if len(responseContent) >= len(toolContent) {
				return nil, nil
			}
			// 否则将原始响应当作摘要
			return map[string]any{
				"summary":                  responseContent,
				"offload_data_explanation": map[string]any{},
			}, nil
		}
		return result, nil
	}
	return map[string]any{}, nil
}

// buildCompressionAttempts 构建降级压缩尝试列表。
//
// 尝试 1：全文 → 尝试 2：截断到 ContentMaxCharsForCompression → 尝试 3：半截断
//
// 对应 Python: MessageSummaryOffloader._build_compression_attempts()
func (mso *MessageSummaryOffloader) buildCompressionAttempts(toolContent string) []string {
	attempts := []string{toolContent}
	maxChars := mso.config.ContentMaxCharsForCompression
	if len(toolContent) <= maxChars {
		return attempts
	}

	attempts = append(attempts, mso.smartTruncateContent(toolContent, maxChars))
	reducedLimit := maxChars / 2
	if reducedLimit < 1 {
		reducedLimit = 1
	}
	if reducedLimit < maxChars {
		attempts = append(attempts, mso.smartTruncateContent(toolContent, reducedLimit))
	}
	return attempts
}

// smartTruncateContent 智能截断内容，保留头/中/尾各约 33%。
//
// 对应 Python: MessageSummaryOffloader._smart_truncate_content()
func (mso *MessageSummaryOffloader) smartTruncateContent(content string, maxChars int) string {
	if len(content) <= maxChars {
		return content
	}

	joinerOverhead := 4
	if maxChars <= len(truncatedMarker)*2+joinerOverhead+3 {
		return content[:maxChars]
	}

	availableChars := maxChars - len(truncatedMarker)*2 - joinerOverhead
	headChars := availableChars / 3
	if headChars < 1 {
		headChars = 1
	}
	tailChars := availableChars / 3
	if tailChars < 1 {
		tailChars = 1
	}
	middleChars := availableChars - headChars - tailChars
	if middleChars < 1 {
		middleChars = 1
	}

	center := len(content) / 2
	middleStart := center - middleChars/2
	if middleStart < headChars {
		middleStart = headChars
	}
	middleEnd := middleStart + middleChars
	if middleEnd > len(content)-tailChars {
		middleEnd = len(content) - tailChars
	}
	middleStart = middleEnd - middleChars
	if middleStart < headChars {
		middleStart = headChars
	}

	head := content[:headChars]
	middle := content[middleStart:middleEnd]
	tail := content[len(content)-tailChars:]
	return head + "\n" + truncatedMarker + "\n" + middle + "\n" + truncatedMarker + "\n" + tail
}

// buildCompressionPrompt 构建自适应压缩提示词。
//
// 对应 Python: MessageSummaryOffloader._build_compression_prompt()
func (mso *MessageSummaryOffloader) buildCompressionPrompt(step string, functionCall *llm_schema.ToolCall, toolContent string) string {
	var functionCallText string
	if functionCall == nil {
		functionCallText = "N/A"
	} else {
		data, err := json.Marshal(functionCall)
		if err != nil {
			functionCallText = fmt.Sprintf("%v", functionCall)
		} else {
			functionCallText = string(data)
		}
	}

	outputSchema := strings.ReplaceAll(outputJSONSchema, "{summary_max_tokens}", fmt.Sprintf("%d", mso.config.SummaryMaxTokens))

	stepValue := step
	if stepValue == "" {
		stepValue = "N/A"
	}
	prompt := strings.ReplaceAll(adaptiveOffloadPromptTemplate, "{step}", stepValue)
	prompt = strings.ReplaceAll(prompt, "{function_call}", functionCallText)
	prompt = strings.ReplaceAll(prompt, "{tool_content}", toolContent)
	prompt = strings.ReplaceAll(prompt, "{output_json_schema}", outputSchema)
	return prompt
}

// parseCompressionResult 解析 LLM 压缩响应为结构化字典。
//
// 支持从 Markdown 代码块中提取 JSON。
//
// 对应 Python: MessageSummaryOffloader._parse_compression_result()
func (mso *MessageSummaryOffloader) parseCompressionResult(responseContent string) (map[string]any, error) {
	var result map[string]any

	trimmed := strings.TrimSpace(responseContent)

	// 尝试直接解析
	if err := json.Unmarshal([]byte(trimmed), &result); err != nil {
		// 尝试提取 JSON 对象
		jsonStart := strings.Index(trimmed, "{")
		jsonEnd := strings.LastIndex(trimmed, "}")
		if jsonStart < 0 || jsonEnd <= jsonStart {
			return nil, exception.NewBaseError(
				exception.StatusContextExecutionError,
				exception.WithMsg(fmt.Sprintf("压缩结果中未找到 JSON: %s", truncateStr(trimmed, 200))),
			)
		}
		if err2 := json.Unmarshal([]byte(trimmed[jsonStart:jsonEnd+1]), &result); err2 != nil {
			return nil, exception.NewBaseError(
				exception.StatusContextExecutionError,
				exception.WithMsg(fmt.Sprintf("压缩结果 JSON 解析失败: %s", truncateStr(trimmed, 200))),
			)
		}
	}

	if _, ok := result["summary"]; !ok {
		return nil, exception.NewBaseError(
			exception.StatusContextExecutionError,
			exception.WithMsg("压缩结果缺少 'summary' 字段"),
		)
	}
	return result, nil
}

// isContextOverflowError 检测异常是否由 LLM 上下文溢出引起。
//
// 不同模型服务商的错误报告格式不同，使用关键词匹配进行检测。
//
// 对应 Python: MessageSummaryOffloader._is_context_overflow_error()
func (mso *MessageSummaryOffloader) isContextOverflowError(err error) bool {
	if err == nil {
		return false
	}
	errMsg := strings.ToLower(err.Error())
	for _, keyword := range contextOverflowKeywords {
		if strings.Contains(errMsg, keyword) {
			return true
		}
	}
	return false
}

// truncateStr 截断字符串到指定长度
func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

// init 自动注册到 context_engine 注册表
func init() {
	context_engine.RegisterProcessorFactory("MessageSummaryOffloader",
		func(config iface.ProcessorConfig) (iface.ContextProcessor, error) {
			cfg, ok := config.(*MessageSummaryOffloaderConfig)
			if !ok {
				return nil, fmt.Errorf("MessageSummaryOffloader: 配置类型不匹配，期望 *MessageSummaryOffloaderConfig，实际 %T", config)
			}
			return NewMessageSummaryOffloader(cfg)
		},
	)
}
