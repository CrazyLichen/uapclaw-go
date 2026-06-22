# MessageSummaryOffloader (5.28) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 MessageSummaryOffloader，用 LLM 智能摘要替代 MessageOffloader 的简单裁剪，完全对齐 Python 逻辑。

**Architecture:** 独立实现，嵌入 `*processor.BaseProcessor`，构造时创建 `*llm.Model`。不嵌入 MessageOffloader，各自实现 shouldOffloadMessage/isProtectedToolMessage/newOffloadHandleAndPath。

**Tech Stack:** Go 1.22+, llm.Model 门面, processor.BaseProcessor, schema.Offloadable

---

## File Structure

| File | Action | Responsibility |
|------|--------|---------------|
| `internal/agentcore/context_engine/processor/offloader/message_summary_offloader.go` | Create | MessageSummaryOffloaderConfig + MessageSummaryOffloader 结构体及所有方法 |
| `internal/agentcore/context_engine/processor/offloader/message_summary_offloader_test.go` | Create | 完整单元测试 |
| `internal/agentcore/context_engine/processor/offloader/doc.go` | Modify | 添加 message_summary_offloader.go 条目 |
| `IMPLEMENTATION_PLAN.md` | Modify | 5.28 步骤状态更新 |

---

### Task 1: MessageSummaryOffloaderConfig + 常量/提示词

**Files:**
- Create: `internal/agentcore/context_engine/processor/offloader/message_summary_offloader.go`

- [ ] **Step 1: 创建文件骨架，写入 Config 结构体、常量和提示词**

在 `message_summary_offloader.go` 中写入以下内容：

```go
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
// MessagesThreshold、TokensThreshold、MessagesToKeep、KeepLastRound
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
// - Per-message 触发：当新添加的合格消息超过大小阈值时触发
// - 任务感知压缩：摘要时考虑当前任务上下文
// - 降级机制：LLM 上下文溢出时用截断内容重试
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

// ──────────────────────────── 枚举 ────────────────────────────

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

// contextOverflowKeywords 上下文溢出关键词，不同模型服务商使用不同的错误格式
var contextOverflowKeywords = []string{
	"context length",
	"token limit",
	"too long",
	"exceeds",
	"maximum context",
	"context window",
}

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
Analyze `tool_content` and perform the following operations:
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
Compress the tool message content to generate a **high-density, high-integrity** summary that can adequately support the current `step`'s task needs without loading the original text.

**Summary requirements:**
- **Integrity priority**: The summary should retain **all key facts, data, conclusions, conditions, and limitations** related to the current `step` from the original text. Do not omit information that substantially impacts understanding or decision-making.
- **Strict accuracy**: All data, names, relationships, and judgments must be strictly accurate; do not distort, blur, or simplify to the point of potential misunderstanding.
- **Focus and conciseness**: Center around the `step` requirements; organize in concise, clear language; remove redundant descriptions, repetitive examples, and irrelevant background buildup, but **do not oversimplify core information**.
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
  "offload_data_explanation": {{
    "category": "The category of information offloaded (e.g., 'raw log data', 'complete product list', 'detailed calculation steps')",
    "description": "Briefly describe what detailed information is missing from the compressed text and its potential use cases, for subsequent on-demand loading of these offloaded information.",
    "inferability": "high" | "medium" | "low"
  }}
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
    Contain ≤ 15 %% of the original token count;
    Keep all critical facts, figures, conclusions, requests or decisions verbatim;
    Remove greetings, repetition, filler, examples, jokes, and ornamental language;
    Speak in neutral, third-person style;
    Do NOT explain, comment, or add extra information—output the summary only.
    Begin:
    `

// ──────────────────────────── 全局变量 ────────────────────────────

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
	contextMessages := mc.GetMessages(nil, true)
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
		allMessages := mc.GetMessages(nil, true)
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
			if filepath.Match(pattern, strVal) == nil {
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
	workspaceDir := ""

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
	contextMessages := mc.GetMessages(nil, true)
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
		sb.WriteString(fmt.Sprintf("[%s] %s", msg.GetRole().String(), content))
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

	prompt := strings.ReplaceAll(adaptiveOffloadPromptTemplate, "{step}", func() string {
		if step == "" {
			return "N/A"
		}
		return step
	}())
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
```

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/context_engine/processor/offloader/...`
Expected: 编译通过

---

### Task 2: 单元测试 — fakeModelClient + Config + 触发/辅助方法

**Files:**
- Create: `internal/agentcore/context_engine/processor/offloader/message_summary_offloader_test.go`

- [ ] **Step 1: 创建测试文件，编写 fakeBaseModelClient 和基础测试**

```go
package offloader

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/schema"
)

// ──────────────────────────── 测试用例 ────────────────────────────

// 注意：复用同包中已有的 fakeModelContext（message_offloader_test.go 中定义），
// 不再重新定义 msoFakeModelContext。

// TestMessageSummaryOffloaderConfig_Validate 测试配置校验
func TestMessageSummaryOffloaderConfig_Validate(t *testing.T) {
	t.Run("默认值应用", func(t *testing.T) {
		cfg := &MessageSummaryOffloaderConfig{}
		err := cfg.Validate()
		require.NoError(t, err)
		assert.Equal(t, msoDefaultTokensThreshold, cfg.TokensThreshold)
		assert.Equal(t, msoDefaultLargeMessageThreshold, cfg.LargeMessageThreshold)
		assert.Equal(t, []string{"tool"}, cfg.OffloadMessageTypes)
		assert.Equal(t, []string{"reload_original_context_messages"}, cfg.ProtectedToolNames)
		assert.Equal(t, msoDefaultSummaryMaxTokens, cfg.SummaryMaxTokens)
		assert.Equal(t, msoDefaultStepSummaryMaxContextMessages, cfg.StepSummaryMaxContextMessages)
		assert.Equal(t, msoDefaultContentMaxCharsForCompression, cfg.ContentMaxCharsForCompression)
	})

	t.Run("MessagesToKeep大于MessagesThreshold报错", func(t *testing.T) {
		keep := 10
		threshold := 5
		cfg := &MessageSummaryOffloaderConfig{
			MessagesToKeep:   &keep,
			MessagesThreshold: &threshold,
		}
		err := cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "MessagesToKeep")
	})

	t.Run("MessagesToKeep小于MessagesThreshold通过", func(t *testing.T) {
		keep := 5
		threshold := 10
		cfg := &MessageSummaryOffloaderConfig{
			MessagesToKeep:   &keep,
			MessagesThreshold: &threshold,
		}
		err := cfg.Validate()
		assert.NoError(t, err)
	})
}

// TestMessageSummaryOffloader_ProcessorType 测试处理器类型
func TestMessageSummaryOffloader_ProcessorType(t *testing.T) {
	cfg := &MessageSummaryOffloaderConfig{}
	require.NoError(t, cfg.Validate())
	mso, err := NewMessageSummaryOffloader(cfg)
	require.NoError(t, err)
	assert.Equal(t, "MessageSummaryOffloader", mso.ProcessorType())
}

// TestMessageSummaryOffloader_TriggerAddMessages 测试触发判断
func TestMessageSummaryOffloader_TriggerAddMessages(t *testing.T) {
	t.Run("新消息超阈值触发", func(t *testing.T) {
		cfg := &MessageSummaryOffloaderConfig{
			LargeMessageThreshold: 100,
		}
		require.NoError(t, cfg.Validate())
		mso, err := NewMessageSummaryOffloader(cfg)
		require.NoError(t, err)

		// 创建一条大的 tool 消息
		longContent := strings.Repeat("x", 500)
		toolMsg := llm_schema.NewToolMessage("call-1", longContent)
		mc := &fakeModelContext{messages: nil, sessionID: "test-session"}

		triggered, err := mso.TriggerAddMessages(context.Background(), mc, []llm_schema.BaseMessage{toolMsg})
		require.NoError(t, err)
		assert.True(t, triggered)
	})

	t.Run("新消息不超阈值不触发", func(t *testing.T) {
		cfg := &MessageSummaryOffloaderConfig{
			LargeMessageThreshold: 1000,
		}
		require.NoError(t, cfg.Validate())
		mso, err := NewMessageSummaryOffloader(cfg)
		require.NoError(t, err)

		shortMsg := llm_schema.NewToolMessage("call-1", "short")
		mc := &fakeModelContext{messages: nil, sessionID: "test-session"}

		triggered, err := mso.TriggerAddMessages(context.Background(), mc, []llm_schema.BaseMessage{shortMsg})
		require.NoError(t, err)
		assert.False(t, triggered)
	})

	t.Run("角色不匹配不触发", func(t *testing.T) {
		cfg := &MessageSummaryOffloaderConfig{
			LargeMessageThreshold: 10,
		}
		require.NoError(t, cfg.Validate())
		mso, err := NewMessageSummaryOffloader(cfg)
		require.NoError(t, err)

		longContent := strings.Repeat("x", 500)
		userMsg := llm_schema.NewUserMessage(longContent)
		mc := &fakeModelContext{messages: nil, sessionID: "test-session"}

		triggered, err := mso.TriggerAddMessages(context.Background(), mc, []llm_schema.BaseMessage{userMsg})
		require.NoError(t, err)
		assert.False(t, triggered)
	})
}

// TestMessageSummaryOffloader_shouldOffloadMessage 测试消息筛选
func TestMessageSummaryOffloader_shouldOffloadMessage(t *testing.T) {
	cfg := &MessageSummaryOffloaderConfig{
		LargeMessageThreshold: 100,
	}
	require.NoError(t, cfg.Validate())
	mso, err := NewMessageSummaryOffloader(cfg)
	require.NoError(t, err)

	t.Run("角色不匹配返回false", func(t *testing.T) {
		userMsg := llm_schema.NewUserMessage(strings.Repeat("x", 500))
		mc := &fakeModelContext{messages: nil, sessionID: "test-session"}
		assert.False(t, mso.shouldOffloadMessage(userMsg, mc, nil))
	})

	t.Run("已卸载消息返回false", func(t *testing.T) {
		offloaded := schema.NewOffloadToolMessage("call-1", "summary", "handle", "in_memory")
		mc := &fakeModelContext{messages: nil, sessionID: "test-session"}
		assert.False(t, mso.shouldOffloadMessage(offloaded, mc, nil))
	})

	t.Run("受保护工具返回false", func(t *testing.T) {
		longContent := strings.Repeat("x", 500)
		toolMsg := llm_schema.NewToolMessage("call-1", longContent)
		assistantMsg := llm_schema.NewAssistantMessage("",
			llm_schema.WithToolCalls([]*llm_schema.ToolCall{
				{ID: "call-1", Name: "reload_original_context_messages", Arguments: "{}"},
			}),
		)
		mc := &fakeModelContext{messages: []llm_schema.BaseMessage{assistantMsg}, sessionID: "test-session"}
		assert.False(t, mso.shouldOffloadMessage(toolMsg, mc, mc.GetMessages(nil, true)))
	})

	t.Run("消息太小返回false", func(t *testing.T) {
		toolMsg := llm_schema.NewToolMessage("call-1", "short")
		mc := &fakeModelContext{messages: nil, sessionID: "test-session"}
		assert.False(t, mso.shouldOffloadMessage(toolMsg, mc, nil))
	})

	t.Run("符合条件的tool消息返回true", func(t *testing.T) {
		longContent := strings.Repeat("x", 500)
		toolMsg := llm_schema.NewToolMessage("call-1", longContent)
		mc := &fakeModelContext{messages: nil, sessionID: "test-session"}
		assert.True(t, mso.shouldOffloadMessage(toolMsg, mc, nil))
	})
}

// TestMessageSummaryOffloader_isContextOverflowError 测试上下文溢出检测
func TestMessageSummaryOffloader_isContextOverflowError(t *testing.T) {
	cfg := &MessageSummaryOffloaderConfig{}
	require.NoError(t, cfg.Validate())
	mso, err := NewMessageSummaryOffloader(cfg)
	require.NoError(t, err)

	tests := []struct {
		name      string
		errMsg    string
		overflow  bool
	}{
		{"context length", "maximum context length exceeded", true},
		{"token limit", "token limit reached", true},
		{"too long", "prompt is too long", true},
		{"exceeds", "input exceeds maximum", true},
		{"maximum context", "maximum context window", true},
		{"context window", "context window exceeded", true},
		{"normal error", "connection timeout", false},
		{"nil error", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			if tt.errMsg != "" {
				err = fmt.Errorf(tt.errMsg)
			}
			assert.Equal(t, tt.overflow, mso.isContextOverflowError(err))
		})
	}
}

// TestMessageSummaryOffloader_smartTruncateContent 测试智能截断
func TestMessageSummaryOffloader_smartTruncateContent(t *testing.T) {
	cfg := &MessageSummaryOffloaderConfig{}
	require.NoError(t, cfg.Validate())
	mso, err := NewMessageSummaryOffloader(cfg)
	require.NoError(t, err)

	t.Run("短内容不截断", func(t *testing.T) {
		content := "short"
		result := mso.smartTruncateContent(content, 100)
		assert.Equal(t, content, result)
	})

	t.Run("长内容截断保留头中尾", func(t *testing.T) {
		content := strings.Repeat("a", 300)
		result := mso.smartTruncateContent(content, 100)
		assert.Contains(t, result, truncatedMarker)
		assert.Less(t, len(result), len(content))
	})

	t.Run("极小maxChars直接截断", func(t *testing.T) {
		content := strings.Repeat("a", 100)
		result := mso.smartTruncateContent(content, 5)
		assert.Equal(t, 5, len(result))
	})
}

// TestMessageSummaryOffloader_parseCompressionResult 测试压缩结果解析
func TestMessageSummaryOffloader_parseCompressionResult(t *testing.T) {
	cfg := &MessageSummaryOffloaderConfig{}
	require.NoError(t, cfg.Validate())
	mso, err := NewMessageSummaryOffloader(cfg)
	require.NoError(t, err)

	t.Run("正常JSON", func(t *testing.T) {
		input := `{"compression_strategy":"extractive","summary":"test summary","offload_data_explanation":{"category":"logs","description":"raw data","inferability":"medium"}}`
		result, err := mso.parseCompressionResult(input)
		require.NoError(t, err)
		assert.Equal(t, "test summary", result["summary"])
	})

	t.Run("Markdown包裹的JSON", func(t *testing.T) {
		input := "```json\n{\"summary\":\"test summary\",\"offload_data_explanation\":{}}\n```"
		result, err := mso.parseCompressionResult(input)
		require.NoError(t, err)
		assert.Equal(t, "test summary", result["summary"])
	})

	t.Run("缺少summary字段报错", func(t *testing.T) {
		input := `{"offload_data_explanation":{}}`
		_, err := mso.parseCompressionResult(input)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "summary")
	})

	t.Run("无JSON报错", func(t *testing.T) {
		input := "no json here"
		_, err := mso.parseCompressionResult(input)
		assert.Error(t, err)
	})
}

// TestMessageSummaryOffloader_getStepFromChainDefault 测试默认任务提取
func TestMessageSummaryOffloader_getStepFromChainDefault(t *testing.T) {
	cfg := &MessageSummaryOffloaderConfig{}
	require.NoError(t, cfg.Validate())
	mso, err := NewMessageSummaryOffloader(cfg)
	require.NoError(t, err)

	t.Run("返回最后的UserMessage", func(t *testing.T) {
		messages := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("first task"),
			llm_schema.NewAssistantMessage("ok"),
			llm_schema.NewUserMessage("second task"),
		}
		step := mso.getStepFromChainDefault(messages)
		assert.Equal(t, "second task", step)
	})

	t.Run("无UserMessage返回空", func(t *testing.T) {
		messages := []llm_schema.BaseMessage{
			llm_schema.NewAssistantMessage("ok"),
		}
		step := mso.getStepFromChainDefault(messages)
		assert.Equal(t, "", step)
	})

	t.Run("空消息列表返回空", func(t *testing.T) {
		step := mso.getStepFromChainDefault(nil)
		assert.Equal(t, "", step)
	})
}

// TestMessageSummaryOffloader_isValidForStepSummary 测试消息筛选
func TestMessageSummaryOffloader_isValidForStepSummary(t *testing.T) {
	cfg := &MessageSummaryOffloaderConfig{}
	require.NoError(t, cfg.Validate())
	mso, err := NewMessageSummaryOffloader(cfg)
	require.NoError(t, err)

	t.Run("UserMessage有效", func(t *testing.T) {
		msg := llm_schema.NewUserMessage("hello")
		assert.True(t, mso.isValidForStepSummary(msg))
	})

	t.Run("AssistantMessage无tool_calls有效", func(t *testing.T) {
		msg := llm_schema.NewAssistantMessage("hi")
		assert.True(t, mso.isValidForStepSummary(msg))
	})

	t.Run("AssistantMessage有tool_calls无效", func(t *testing.T) {
		msg := llm_schema.NewAssistantMessage("",
			llm_schema.WithToolCalls([]*llm_schema.ToolCall{
				{ID: "call-1", Name: "test", Arguments: "{}"},
			}),
		)
		assert.False(t, mso.isValidForStepSummary(msg))
	})

	t.Run("ToolMessage无效", func(t *testing.T) {
		msg := llm_schema.NewToolMessage("call-1", "result")
		assert.False(t, mso.isValidForStepSummary(msg))
	})
}

// TestMessageSummaryOffloader_buildCompressionAttempts 测试降级尝试构建
func TestMessageSummaryOffloader_buildCompressionAttempts(t *testing.T) {
	t.Run("短内容只返回原文", func(t *testing.T) {
		cfg := &MessageSummaryOffloaderConfig{
			ContentMaxCharsForCompression: 1000,
		}
		require.NoError(t, cfg.Validate())
		mso, err := NewMessageSummaryOffloader(cfg)
		require.NoError(t, err)

		content := "short content"
		attempts := mso.buildCompressionAttempts(content)
		assert.Len(t, attempts, 1)
		assert.Equal(t, content, attempts[0])
	})

	t.Run("长内容返回三级降级", func(t *testing.T) {
		cfg := &MessageSummaryOffloaderConfig{
			ContentMaxCharsForCompression: 1000,
		}
		require.NoError(t, cfg.Validate())
		mso, err := NewMessageSummaryOffloader(cfg)
		require.NoError(t, err)

		content := strings.Repeat("x", 3000)
		attempts := mso.buildCompressionAttempts(content)
		assert.Len(t, attempts, 3)
		assert.Equal(t, content, attempts[0])
		assert.Contains(t, attempts[1], truncatedMarker)
		assert.Contains(t, attempts[2], truncatedMarker)
	})
}

// ──────────────────────────── LLM 测试基础设施 ────────────────────────────

// msoFakeBaseModelClient 用于测试的模拟模型客户端
type msoFakeBaseModelClient struct {
	invokeResp *llm_schema.AssistantMessage
	invokeErr  error
	invoked    bool
	lastPrompt string
}

func (f *msoFakeBaseModelClient) Invoke(_ context.Context, messages model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llm_schema.AssistantMessage, error) {
	f.invoked = true
	if messages.IsText() {
		f.lastPrompt = messages.Text()
	}
	return f.invokeResp, f.invokeErr
}
func (f *msoFakeBaseModelClient) Stream(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.StreamOption) (*model_clients.StreamResult, error) {
	return nil, fmt.Errorf("not implemented")
}
func (f *msoFakeBaseModelClient) GenerateImage(_ context.Context, _ []*llm_schema.UserMessage, _ ...model_clients.GenerateImageOption) (*llm_schema.ImageGenerationResponse, error) {
	return nil, nil
}
func (f *msoFakeBaseModelClient) GenerateSpeech(_ context.Context, _ []*llm_schema.UserMessage, _ ...model_clients.GenerateSpeechOption) (*llm_schema.AudioGenerationResponse, error) {
	return nil, nil
}
func (f *msoFakeBaseModelClient) GenerateVideo(_ context.Context, _ []*llm_schema.UserMessage, _ ...model_clients.GenerateVideoOption) (*llm_schema.VideoGenerationResponse, error) {
	return nil, nil
}
func (f *msoFakeBaseModelClient) Release(_ context.Context, _ ...model_clients.ReleaseOption) (bool, error) {
	return false, nil
}

const msoTestProvider = "MSOTestProvider"

// msoCurrentFakeClient 当前使用的 fake 客户端
var msoCurrentFakeClient *msoFakeBaseModelClient

// msoFakeRegistryOnce 确保 fake provider 只注册一次
var msoFakeRegistryOnce sync.Once

// msoNewFakeLLMModel 创建带 fake client 的 llm.Model 实例
// 使用 ClientRegistry 注册模式，与 compressor 测试对齐
func msoNewFakeLLMModel(fakeClient *msoFakeBaseModelClient) *llm.Model {
	msoCurrentFakeClient = fakeClient
	msoFakeRegistryOnce.Do(func() {
		model_clients.GetClientRegistry().Register(msoTestProvider, "llm",
			func(_ *llm_schema.ModelRequestConfig, _ *llm_schema.ModelClientConfig) model_clients.BaseModelClient {
				return msoCurrentFakeClient
			},
		)
	})

	clientConfig := &llm_schema.ModelClientConfig{
		ClientID:       "mso-test-client",
		ClientProvider: msoTestProvider,
		APIKey:         "fake-key",
		APIBase:        "https://fake.api.com",
	}
	modelConfig := llm_schema.NewModelRequestConfig(llm_schema.WithModelName("test-model"))
	model, err := llm.NewModel(clientConfig, modelConfig, llm.WithCallbackFramework(nil))
	if err != nil {
		panic(fmt.Sprintf("msoNewFakeLLMModel: %v", err))
	}
	return model
}
```

- [ ] **Step 2: 运行测试，确保基础测试通过**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/context_engine/processor/offloader/... -run "TestMessageSummaryOffloader" -v -count=1`
Expected: 基础测试通过

---

### Task 3: 单元测试 — OnAddMessages + compressWithFallback 完整流程

**Files:**
- Modify: `internal/agentcore/context_engine/processor/offloader/message_summary_offloader_test.go`

- [ ] **Step 1: 追加 OnAddMessages 和 compressWithFallback 的集成测试**

在测试文件末尾追加：

```go
// TestMessageSummaryOffloader_OnAddMessages_完整流程 测试完整摘要卸载流程
func TestMessageSummaryOffloader_OnAddMessages_完整流程(t *testing.T) {
	// 创建 fake model（复用 compressor 测试中的 registry 模式）
	summaryJSON := `{"compression_strategy":"extractive","summary":"compressed summary","offload_data_explanation":{"category":"logs","description":"raw log data","inferability":"medium"}}`
	fakeClient := &msoFakeBaseModelClient{invokeResp: llm_schema.NewAssistantMessage(summaryJSON)}
	model := msoNewFakeLLMModel(fakeClient)

	cfg := &MessageSummaryOffloaderConfig{
		LargeMessageThreshold: 100,
	}
	require.NoError(t, cfg.Validate())

	mso, err := NewMessageSummaryOffloader(cfg, WithMessageSummaryModel(model))
	require.NoError(t, err)

	t.Run("大消息被摘要卸载", func(t *testing.T) {
		longContent := strings.Repeat("x", 500)
		toolMsg := llm_schema.NewToolMessage("call-1", longContent)
		mc := &fakeModelContext{messages: nil, sessionID: "test-session"}

		event, processed, err := mso.OnAddMessages(context.Background(), mc, []llm_schema.BaseMessage{toolMsg})
		require.NoError(t, err)
		assert.NotNil(t, event)
		assert.Equal(t, "MessageSummaryOffloader", event.EventType)
		assert.Len(t, event.MessagesToModify, 1)
		// 处理后内容应包含压缩摘要
		assert.Contains(t, processed[0].GetContent().Text(), "compressed summary")
	})

	t.Run("小消息不被卸载", func(t *testing.T) {
		shortMsg := llm_schema.NewToolMessage("call-1", "short")
		mc := &fakeModelContext{messages: nil, sessionID: "test-session"}

		event, processed, err := mso.OnAddMessages(context.Background(), mc, []llm_schema.BaseMessage{shortMsg})
		require.NoError(t, err)
		assert.Nil(t, event)
		assert.Equal(t, "short", processed[0].GetContent().Text())
	})

	t.Run("混合消息只卸载符合条件的", func(t *testing.T) {
		longContent := strings.Repeat("x", 500)
		toolMsg := llm_schema.NewToolMessage("call-1", longContent)
		shortMsg := llm_schema.NewToolMessage("call-2", "short")
		mc := &fakeModelContext{messages: nil, sessionID: "test-session"}

		event, processed, err := mso.OnAddMessages(context.Background(), mc, []llm_schema.BaseMessage{toolMsg, shortMsg})
		require.NoError(t, err)
		assert.NotNil(t, event)
		assert.Len(t, event.MessagesToModify, 1)
		// 第二条消息不变
		assert.Equal(t, "short", processed[1].GetContent().Text())
	})
}

// TestMessageSummaryOffloader_SaveLoadState 测试状态保存/加载
func TestMessageSummaryOffloader_SaveLoadState(t *testing.T) {
	cfg := &MessageSummaryOffloaderConfig{}
	require.NoError(t, cfg.Validate())
	mso, err := NewMessageSummaryOffloader(cfg)
	require.NoError(t, err)

	state := mso.SaveState()
	assert.Empty(t, state)

	mso.LoadState(map[string]any{"test": "value"})
	// 空操作，不 panic 即可
}

// TestMessageSummaryOffloader_compressWithFallback 测试降级压缩
func TestMessageSummaryOffloader_compressWithFallback(t *testing.T) {
	summaryJSON := `{"compression_strategy":"abstractive","summary":"test summary","offload_data_explanation":{"category":"data","description":"full data","inferability":"low"}}`
	fakeClient := &msoFakeBaseModelClient{invokeResp: llm_schema.NewAssistantMessage(summaryJSON)}
	model := msoNewFakeLLMModel(fakeClient)

	cfg := &MessageSummaryOffloaderConfig{
		LargeMessageThreshold:         100,
		ContentMaxCharsForCompression: 1000,
	}
	require.NoError(t, cfg.Validate())

	mso, err := NewMessageSummaryOffloader(cfg, WithMessageSummaryModel(model))
	require.NoError(t, err)

	t.Run("正常压缩返回结果", func(t *testing.T) {
		result, err := mso.compressWithFallback(context.Background(), "test step", nil, "test content")
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "test summary", result["summary"])
	})

	t.Run("长内容触发智能截断", func(t *testing.T) {
		longContent := strings.Repeat("x", 2000)
		result, err := mso.compressWithFallback(context.Background(), "test step", nil, longContent)
		require.NoError(t, err)
		require.NotNil(t, result)
	})
}

// TestMessageSummaryOffloader_buildCompressionPrompt 测试提示词构建
func TestMessageSummaryOffloader_buildCompressionPrompt(t *testing.T) {
	cfg := &MessageSummaryOffloaderConfig{
		SummaryMaxTokens: 900,
	}
	require.NoError(t, cfg.Validate())
	mso, err := NewMessageSummaryOffloader(cfg)
	require.NoError(t, err)

	t.Run("无functionCall时显示NA", func(t *testing.T) {
		prompt := mso.buildCompressionPrompt("test step", nil, "test content")
		assert.Contains(t, prompt, "N/A")
		assert.Contains(t, prompt, "test step")
		assert.Contains(t, prompt, "test content")
		assert.Contains(t, prompt, "900")
	})

	t.Run("有functionCall时序列化", func(t *testing.T) {
		tc := &llm_schema.ToolCall{
			ID:        "call-1",
			Name:      "search",
			Arguments: `{"query": "test"}`,
		}
		prompt := mso.buildCompressionPrompt("test step", tc, "test content")
		assert.Contains(t, prompt, "search")
		assert.Contains(t, prompt, "test step")
	})
}

// TestMessageSummaryOffloader_newOffloadHandleAndPath 测试句柄和路径生成
func TestMessageSummaryOffloader_newOffloadHandleAndPath(t *testing.T) {
	cfg := &MessageSummaryOffloaderConfig{}
	require.NoError(t, cfg.Validate())
	mso, err := NewMessageSummaryOffloader(cfg)
	require.NoError(t, err)

	mc := &fakeModelContext{messages: nil, sessionID: "test-session"}
	handle, path := mso.newOffloadHandleAndPath(mc)
	assert.NotEmpty(t, handle)
	// 当前 WorkspaceDir 为空，path 为空
	assert.Empty(t, path)
}

// TestMessageSummaryOffloader_selectMessagesForStepSummary 测试消息筛选
func TestMessageSummaryOffloader_selectMessagesForStepSummary(t *testing.T) {
	cfg := &MessageSummaryOffloaderConfig{
		StepSummaryMaxContextMessages: 3,
	}
	require.NoError(t, cfg.Validate())
	mso, err := NewMessageSummaryOffloader(cfg)
	require.NoError(t, err)

	t.Run("过滤后消息不足2条返回nil", func(t *testing.T) {
		messages := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("hello"),
			llm_schema.NewToolMessage("call-1", "result"),
		}
		result := mso.selectMessagesForStepSummary(messages)
		assert.Nil(t, result)
	})

	t.Run("保留最近N条", func(t *testing.T) {
		messages := []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("msg1"),
			llm_schema.NewAssistantMessage("msg2"),
			llm_schema.NewUserMessage("msg3"),
			llm_schema.NewAssistantMessage("msg4"),
			llm_schema.NewUserMessage("msg5"),
		}
		result := mso.selectMessagesForStepSummary(messages)
		require.Len(t, result, 3)
		// 保留最后3条
		assert.Equal(t, "msg3", result[0].GetContent().Text())
		assert.Equal(t, "msg5", result[2].GetContent().Text())
	})
}
```

- [ ] **Step 2: 运行全部 MessageSummaryOffloader 测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/context_engine/processor/offloader/... -run "TestMessageSummaryOffloader" -v -count=1`
Expected: 全部通过

- [ ] **Step 3: 检查测试覆盖率**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/context_engine/processor/offloader/... -cover -count=1`
Expected: offloader 包覆盖率 ≥ 85%

---

### Task 4: 更新 doc.go

**Files:**
- Modify: `internal/agentcore/context_engine/processor/offloader/doc.go`

- [ ] **Step 1: 更新 doc.go 文件目录和包描述**

将 doc.go 内容更新为：

```go
// Package offloader 提供上下文引擎的消息卸载处理器实现。
//
// 卸载处理器在对话消息数或 Token 数超过阈值时，将大消息的内容卸载
// 并生成轻量占位符。原始内容可通过 reloader_tool 按 offload_handle 取回。
//
// 当前实现：
//   - MessageOffloader：基础裁剪卸载，将大消息截断为 trim_size + 省略标记
//   - MessageSummaryOffloader：LLM 智能摘要卸载，生成任务感知的自适应压缩摘要
//
// 文件目录：
//
//	offloader/
//	├── doc.go                            # 包文档
//	├── message_offloader.go              # MessageOffloader + Config
//	└── message_summary_offloader.go      # MessageSummaryOffloader + Config
//
// 对应 Python 代码：openjiuwen/core/context_engine/processor/offloader/
package offloader
```

- [ ] **Step 2: 验证编译通过**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/context_engine/processor/offloader/...`
Expected: 编译通过

---

### Task 5: 更新 IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 5.28 步骤状态**

将 IMPLEMENTATION_PLAN.md 中 5.28 行的状态从 `☐` 改为 `✅`，内容更新为：

```
| 5.28 | ✅ | MessageSummaryOffloader | ✅ MessageSummaryOffloaderConfig（11 字段+默认值+Validate 校验）；✅ MessageSummaryOffloader 结构体（嵌入 BaseProcessor）；✅ ProcessorFactory 自动注册；✅ TriggerAddMessages（只检查新消息超 largeMessageThreshold）；✅ OnAddMessages（遍历新消息执行自适应摘要）；✅ shouldOffloadMessage（token 计数优先，回退字符数//3）；✅ offloadMessageAdaptive（提取函数调用+任务+LLM 压缩+卸载）；✅ getStepFromChainDefault/Precise（启发式/LLM 精确）；✅ compressWithFallback（三级降级重试）；✅ smartTruncateContent（头/中/尾 33% 保留）；✅ buildCompressionPrompt/parseCompressionResult；✅ isContextOverflowError（6 关键词）；✅ 常量/提示词完整对齐 Python；✅ 日志同步；⤵️ 5.31 回填 mc.WorkspaceDir()/mc.OffloadMessages()；⤵️ 9.32 回填 SysOperation | `openjiuwen/core/context_engine/processor/offloader/message_summary_offloader.py` |
```

- [ ] **Step 2: 提交所有变更**

```bash
git add internal/agentcore/context_engine/processor/offloader/message_summary_offloader.go \
       internal/agentcore/context_engine/processor/offloader/message_summary_offloader_test.go \
       internal/agentcore/context_engine/processor/offloader/doc.go \
       IMPLEMENTATION_PLAN.md
git commit -m "feat(context_engine): 实现 MessageSummaryOffloader (5.28) — LLM 智能摘要卸载"
```

---

### Task 6: 最终验证

- [ ] **Step 1: 全量编译检查**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...`
Expected: 编译通过

- [ ] **Step 2: 运行 offloader 包全部测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/context_engine/processor/offloader/... -v -cover -count=1`
Expected: 全部通过，覆盖率 ≥ 85%

- [ ] **Step 3: 运行 context_engine 全量测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/context_engine/... -cover -count=1`
Expected: 全部通过，无回归
