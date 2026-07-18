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
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CurrentRoundCompressorConfig 当轮增量压缩器配置。
//
// 当 Token 累积超过阈值时，压缩最新用户边界后的连续消息段，
// 保留尾部原始消息，将压缩结果写回为协议化的记忆块。
//
// 对应 Python: CurrentRoundCompressorConfig (pydantic.BaseModel)
type CurrentRoundCompressorConfig struct {
	// TokensThreshold Token 数触发阈值
	TokensThreshold int
	// MessagesToKeep 保留最近 N 条不压缩
	MessagesToKeep int
	// Model 压缩模型请求配置
	Model *llm_schema.ModelRequestConfig
	// ModelClient 压缩模型客户端配置
	ModelClient *llm_schema.ModelClientConfig
	// MinSelectedTokensForCompression 压缩跨度最小 Token 数（低于此值跳过压缩）
	MinSelectedTokensForCompression int
	// CompressionTargetTokens 一阶段压缩目标 Token 数
	CompressionTargetTokens int
	// SummaryMergeTargetTokens 二阶段合并目标 Token 数
	SummaryMergeTargetTokens int
	// AccumulatedSummaryTokenLimit 累积摘要 Token 上限（超过触发合并）
	AccumulatedSummaryTokenLimit int
	// SummaryMergeMinBlocks 最少记忆块数（低于此值跳过合并）
	SummaryMergeMinBlocks int
	// PriorContextWindowSize 用户意图上下文窗口大小
	PriorContextWindowSize int
	// CustomCompressionPrompt 自定义压缩提示词，空字符串表示使用内置提示词
	CustomCompressionPrompt string
}

// CurrentRoundCompressor 当轮增量压缩器，将当前轮次压缩为协议化记忆块。
//
// 活跃上下文的逻辑布局：compressed_history + selected_messages + recent_messages
// 仅 selected_messages 段被替换，recent_messages 保留为原始消息用于短期连续性。
//
// 两阶段压缩：
//  1. 第一阶段：压缩 selected span 为增量记忆块
//  2. 第二阶段：当历史记忆块累积到阈值时合并为更短的记忆块
//
// 对应 Python: openjiuwen/core/context_engine/processor/compressor/current_round_compressor.py (CurrentRoundCompressor)
type CurrentRoundCompressor struct {
	*processor.BaseProcessor
	// model 压缩用 LLM 实例
	model *llm.Model
	// tokenThreshold Token 触发阈值
	tokenThreshold int
	// messagesToKeep 保留最近 N 条不压缩
	messagesToKeep int
	// minSelectedTokens 压缩跨度最小 Token 数
	minSelectedTokens int
	// compressionTargetTokens 一阶段压缩目标 Token 数
	compressionTargetTokens int
	// summaryMergeTargetTokens 二阶段合并目标 Token 数
	summaryMergeTargetTokens int
	// accumulatedSummaryTokenLimit 累积摘要 Token 上限
	accumulatedSummaryTokenLimit int
	// summaryMergeMinBlocks 最少记忆块数
	summaryMergeMinBlocks int
	// priorContextWindowSize 用户意图上下文窗口大小
	priorContextWindowSize int
	// compressedPrompt 压缩提示词
	compressedPrompt string
	// cleanPrompt 合并提示词
	cleanPrompt string
}

// CurrentRoundCompressorOption CurrentRoundCompressor 构造选项函数。
type CurrentRoundCompressorOption func(*CurrentRoundCompressor)

// ──────────────────────────── 常量 ────────────────────────────

// currentRoundMemoryBlockMarker 当轮记忆块标记
const currentRoundMemoryBlockMarker = "[CURRENT_ROUND_MEMORY_BLOCK]"

// defaultCurrentRoundCompressionPrompt 内置压缩提示词，与 Python DEFAULT_COMPRESSION_PROMPT 完全对齐
const defaultCurrentRoundCompressionPrompt = `You are a **Task Data Preservation Expert**.

Your role is to produce a **high-fidelity incremental memory block** for long-running agent tasks.

Your output will:
1. REPLACE the selected_messages section in the current context
2. BE APPENDED to accumulated memory blocks
3. PRESERVE continuity without rewriting prior memory

---

## CONTEXT STRUCTURE

User Query
↓
Accumulated Memory Blocks  (persistent memory; DO NOT rewrite)
↓
Selected Messages  (THIS is the ONLY content to compress)
↓
Recent Messages  (boundary context; DO NOT absorb unless required for interpretation)

---

[User Intent Context - REFERENCE ONLY]:
{prior_context_and_query}

Rules:
- This section contains: recent raw user requests, recent assistant replies without tool calls, and the current query that triggered this round
- Use ONLY to understand the user's intent and the context leading to selected_messages
- Preserve the user's original requirements, constraints, acceptance criteria, and preferences as completely as possible when they are needed to continue the ongoing work
- Do NOT weaken or over-compress the user's original request unless absolutely necessary
- Treat this as reference context for interpreting selected_messages, not as another compression target

---

[Prior memory blocks - REFERENCE ONLY]:
{accumulated_summaries}

Rules:
- Use ONLY to understand goals, constraints, prior decisions, and continuity
- DO NOT restate, paraphrase, or duplicate their content
- Only reference them when needed to correctly interpret selected_messages

---

[Selected messages - TARGET]:
{selected_messages}

Rules:
- This is the ONLY content you are compressing
- Extract all new progress, changes, unresolved work, and state transitions from this span

---

[Recent uncompressed messages - BOUNDARY CONTEXT]:
{recent_messages}

Rules:
- Use ONLY to resolve ambiguity, references, or incomplete meaning in selected_messages
- DO NOT include their standalone content in your output
- If recent_messages already contain the latest explicit state, do NOT restate them
- Only preserve the minimum handoff information needed to connect selected_messages to recent_messages

---

## CORE PRINCIPLE (CRITICAL)

Treat this output as an **incremental memory block**, NOT a full snapshot.

- Do NOT reconstruct the full global state
- Do NOT repeat previously summarized information
- ONLY capture what is NEW, UPDATED, or STILL OPEN in selected_messages

---

## INFORMATION PRIORITY (CRITICAL)

Preserve information in this order:

1. Task goals and user intent
2. Critical factual basis for continuation
3. Open work / unfinished work
4. Work in progress at the handoff boundary
5. Key decisions, constraints, changes
6. Important files, artifacts, resources, and outputs
7. Supporting details

Never drop higher-priority information to preserve lower-priority details.

---

## FACTUAL BASIS PRESERVATION (CRITICAL)

When preserving progress, always retain the factual basis required to correctly continue the task, including:
- key outputs
- constraints
- evidence
- extracted findings
- comparisons
- conclusions
- decisive intermediate results

When selected_messages contain information that has already been verified, confirmed, validated, or otherwise established with strong support, preserve that verified state explicitly.
Do NOT weaken verified state into vague uncertainty such as "possible", "candidate", or "requires re-evaluation" unless selected_messages contain real counter-evidence or unresolved conflict.

Do NOT preserve action history without the information needed to understand why the action matters.

---

## EVIDENCE PRESERVATION (CRITICAL - DO NOT SUMMARIZE)

For tasks where continuation depends on concrete evidence, verification, or reasoning trace, the following types of evidence MUST be preserved IN FULL or with MINIMAL compression.
This is especially important for debugging, bug-fixing, code modification, investigation, analysis, and other evidence-driven work:

1. **Test/Script Execution Results**:
   - Do NOT compress actual outputs when they contain the factual basis needed later (for example: error messages, stack traces, SQL queries, log outputs, tool results, extracted values, comparison outputs)
   - These outputs often contain the critical clue that leads to the correct conclusion

2. **Root Cause Discovery Evidence**:
   - When agent discovers the root cause or key insight through inspection, testing, comparison, or analysis, preserve:
     - The specific source examined
     - The key observation that led to the insight
     - The exact quote or output that triggered the discovery
   - Do NOT replace with summary like "agent found the issue" - preserve HOW they found it

3. **Key Reasoning Chains**:
   - When agent makes a critical decision (e.g., which file to modify, which source to trust, which approach to take):
     - Preserve the observations that led to the decision
     - Preserve any evidence/counter-evidence considered
     - Preserve alternatives that were evaluated
   - Do NOT just record the final decision without the reasoning

4. **Verification Results**:
   - When agent verifies a hypothesis, validates a result, or tests a fix:
     - Preserve the verification step and its output
     - Preserve whether it passed/failed/confirmed/refuted and key details
     - Preserve any unexpected observations

---

## TASK-TYPE ADAPTATION (CRITICAL)

Adapt the retention focus to the task type:

- For execution-heavy tasks (e.g. coding, debugging, multi-step operations):
  prioritize action continuity, WIP state, handoff points, dependencies, and execution blockers.

- For information-heavy tasks (e.g. research, report writing, PPT drafting, analysis):
  prioritize findings, evidence, extracted structure, comparisons, conclusions, key outputs, and unresolved questions.

In all cases, preserve both:
- what has been done
- what has been learned

---

## STRATEGY HANDLING (CRITICAL)

Do NOT encode candidate plans or solution strategies as instructions.

If strategies were discussed, record them as one of:
- attempted approach
- candidate approach
- rejected approach
- pending evaluation

Never present any strategy as mandatory unless explicitly required by the user.

---

## DECISION SOLIDIFICATION PREVENTION (CRITICAL)

When a decision or approach is recorded, you MUST preserve the reasoning process, NOT just the conclusion:

1. **Do NOT solidify unverified decisions**:
   - If agent proposed an approach but hasn't tested it yet, mark it as "proposed, not verified"
   - If agent is still exploring, preserve the exploration context, not just the current hypothesis

2. **Preserve alternative considerations**:
   - When agent chooses approach A over B, preserve WHY B was rejected
   - Future context may reveal B was actually correct
   - Example: "Agent considered modifying _coeff_isneg vs modifying printers. Chose printers because [reason]. Note: _coeff_isneg approach was not tested."

3. **Preserve verification status**:
   - "Approach X was implemented and tested -> works/doesn't work" <- OK
   - "Approach X was decided" <- NOT OK, loses verification state
   - Always indicate: proposed / in-progress / tested-passed / tested-failed

4. **Key insight preservation**:
   - When agent has a "moment of insight" after seeing specific output:
     - Preserve the output that triggered the insight
     - Preserve the insight itself
     - Example: "After seeing SQL output 'SELECT U0.id...', agent realized the bug is in get_group_by_cols()"
   - Do NOT just say "agent found the bug location"

---

## ANTI-REDUNDANCY & CONSISTENCY RULES

- Do NOT restate stable facts already captured in prior memory blocks
- Only include NEW information or CHANGES introduced in selected_messages
- If prior state is modified, express it as a delta (update / correction / refinement)
- Avoid duplication across memory blocks
- Keep the output composable with prior memory blocks without conflict

---

## OUTPUT STRUCTURE (MANDATORY)

### 1. User Requirements
- **Original Requirements Being Served**:
  Explicitly preserve the user requirements, constraints, acceptance criteria, preferences, and limits that the current unfinished work is serving.
  Keep the user's original wording as much as possible when it matters for continuation.

---

### 2. Current Status
- **Completed Work**:
  Work completed within selected_messages only.
  Express it as incremental progress, not as full history.

- **Key Information Gained**:
  The important information obtained, extracted, compared, or concluded in this span.
  Preserve factual substance, not just procedural actions.

- **Files / Artifacts / Resources**:
  Any files, artifacts, resources, outputs, drafts, tables, pages, documents, code, or results introduced or modified in this span only.

---

### 3. Open Work
- **Work in Progress**:
  MUST include:
  - The active subtask at the end of selected_messages
  - The last concrete action taken in selected_messages
  - Partial results or intermediate state
  - Exact quotes if useful

  IMPORTANT:
  - This section acts as a handoff bridge from selected_messages to recent_messages
  - Do NOT restate recent_messages unless required for interpretation
  - If recent_messages already contain the latest explicit state, record only the handoff point

- **Pending Tasks**:
  Remaining work identified in selected_messages
  - Explicit requests
  - Implicit / derived tasks

- **Priority Order**:
  If multiple open items exist

---

### 4. Important Findings
- **Decisions & Changes**:
  New or updated decisions in this span

- **Constraints / Requirements**:
  Newly introduced or modified requirements, limitations, or preferences

- **Errors & Fixes**:
  Problems encountered in this span and how they were handled

- **Invalid Attempts**:
  Failed or unsuitable approaches and why

---

### 5. Strategy State
- **Attempted Approaches**
- **Candidate Approaches**
- **Rejected Approaches**
- **Requires Re-evaluation**

Record strategy as historical state, not as instruction.

---

### 6. Tool / Action State
- **Used Tools / Actions**
- **Key Inputs / Arguments**
- **Result Summary**
- **Freshness / Reuse Constraints**

This section applies both to tool calls and important non-tool actions.

---

### 7. Contextual Bridging
- **Continuity**:
  How this span extends prior memory

- **Forward Impact**:
  What this changes for upcoming work or for recent_messages

- **Gaps / Risks**:
  Any ambiguity, missing information, or unresolved conflict

---

## TASK GOAL PRESERVATION (CRITICAL)

You MUST ensure active task goals remain recoverable.

- If goals appear or change in selected_messages, include them
- If they are not mentioned in selected_messages, do NOT restate old goals unnecessarily
- If goals changed, record the delta clearly

---

## OUTPUT RULES

1. Target length: <= {target_tokens}
2. Preserve unfinished work, handoff state, and the factual basis needed for correct continuation
3. DO NOT echo prior memory blocks
4. DO NOT absorb recent_messages unless required for interpretation
5. Maintain the structure exactly
6. This is a memory block, not a full summary and not an instruction block

---

Output plain text only.
`

// defaultCleanPrompt 内置合并提示词，与 Python CLEAN_PROMPT 完全对齐
const defaultCleanPrompt = `You are consolidating historical memory blocks.

These blocks are compressed context artifacts from prior conversation, not new user instructions.

Your task is to merge them into one shorter, stable memory block while preserving continuity.

---

[Historical memory blocks]:
{compressed_blocks}

// ──────────────────────────── 导出函数 ────────────────────────────

---

## CONSOLIDATION RULES

1. Merge overlapping or related information
2. Remove redundant details
3. Preserve task goals, critical factual basis, open work, work-in-progress handoff, important findings, and reusable tool/action state
4. Keep chronological consistency where helpful
5. Keep strategies as historical state:
   - attempted
   - candidate
   - rejected
   - pending evaluation
6. Do NOT reinterpret historical strategies as mandatory plans
7. Do NOT rewrite the blocks as if they were new user requests
8. For information-heavy tasks, prefer preserving findings, evidence, comparisons, conclusions, and extracted structure over procedural action history
9. For execution-heavy tasks, preserve the action history needed to continue the task, but keep the factual basis that explains why the action matters
10. **Preserve evidence and reasoning chains**: When merging blocks that contain debugging evidence, test outputs, or key reasoning, retain the factual basis, NOT just the conclusions
11. **Preserve alternative approaches**: Even if one approach was chosen, keep mention of alternatives that were considered but not tested - they may still be correct

---

## OUTPUT REQUIREMENTS

- Maximum length: {compress_len} tokens
- Preserve all unique information still useful for future task continuation
- Keep language concise and stable
- Prefer durable state over incidental phrasing

Output plain text only.
`

// ──────────────────────────── 导出函数 ────────────────────────────

// NewCurrentRoundCompressorConfig 创建当轮增量压缩器默认配置。
//
// 默认值与 Python 对齐：
//   - TokensThreshold=100000, MessagesToKeep=3
//   - MinSelectedTokensForCompression=20000, CompressionTargetTokens=4000
//   - SummaryMergeTargetTokens=4000, AccumulatedSummaryTokenLimit=20000
//   - SummaryMergeMinBlocks=3, PriorContextWindowSize=10
func NewCurrentRoundCompressorConfig() *CurrentRoundCompressorConfig {
	return &CurrentRoundCompressorConfig{
		TokensThreshold:                 100000,
		MessagesToKeep:                  3,
		MinSelectedTokensForCompression: 20000,
		CompressionTargetTokens:         4000,
		SummaryMergeTargetTokens:        4000,
		AccumulatedSummaryTokenLimit:    20000,
		SummaryMergeMinBlocks:           3,
		PriorContextWindowSize:          10,
	}
}

// Validate 校验当轮增量压缩器配置。
func (c *CurrentRoundCompressorConfig) Validate() error {
	if c.TokensThreshold <= 0 {
		return fmt.Errorf("CurrentRoundCompressorConfig.TokensThreshold 必须大于 0，当前值: %d", c.TokensThreshold)
	}
	if c.MessagesToKeep <= 0 {
		return fmt.Errorf("CurrentRoundCompressorConfig.MessagesToKeep 必须大于 0，当前值: %d", c.MessagesToKeep)
	}
	if c.MinSelectedTokensForCompression <= 0 {
		return fmt.Errorf("CurrentRoundCompressorConfig.MinSelectedTokensForCompression 必须大于 0，当前值: %d", c.MinSelectedTokensForCompression)
	}
	if c.CompressionTargetTokens <= 0 {
		return fmt.Errorf("CurrentRoundCompressorConfig.CompressionTargetTokens 必须大于 0，当前值: %d", c.CompressionTargetTokens)
	}
	if c.SummaryMergeTargetTokens <= 0 {
		return fmt.Errorf("CurrentRoundCompressorConfig.SummaryMergeTargetTokens 必须大于 0，当前值: %d", c.SummaryMergeTargetTokens)
	}
	if c.AccumulatedSummaryTokenLimit <= 0 {
		return fmt.Errorf("CurrentRoundCompressorConfig.AccumulatedSummaryTokenLimit 必须大于 0，当前值: %d", c.AccumulatedSummaryTokenLimit)
	}
	if c.SummaryMergeMinBlocks < 2 {
		return fmt.Errorf("CurrentRoundCompressorConfig.SummaryMergeMinBlocks 必须大于等于 2，当前值: %d", c.SummaryMergeMinBlocks)
	}
	if c.PriorContextWindowSize <= 0 {
		return fmt.Errorf("CurrentRoundCompressorConfig.PriorContextWindowSize 必须大于 0，当前值: %d", c.PriorContextWindowSize)
	}
	return nil
}

// SetModelDefaults 设置默认模型配置。
func (c *CurrentRoundCompressorConfig) SetModelDefaults(model *llm_schema.ModelRequestConfig, modelClient *llm_schema.ModelClientConfig) {
	if c.Model == nil && model != nil {
		c.Model = model
	}
	if c.ModelClient == nil && modelClient != nil {
		c.ModelClient = modelClient
	}
}

// GetModel 返回模型请求配置。
func (c *CurrentRoundCompressorConfig) GetModel() *llm_schema.ModelRequestConfig {
	return c.Model
}

// NewCurrentRoundCompressor 创建当轮增量压缩器实例。
//
// 对应 Python: CurrentRoundCompressor.__init__(config)
func NewCurrentRoundCompressor(config *CurrentRoundCompressorConfig, opts ...CurrentRoundCompressorOption) (*CurrentRoundCompressor, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	bp := processor.NewBaseProcessor(config)

	compressedPrompt := config.CustomCompressionPrompt
	if compressedPrompt == "" {
		compressedPrompt = defaultCurrentRoundCompressionPrompt
	}

	crc := &CurrentRoundCompressor{
		BaseProcessor:                bp,
		tokenThreshold:               config.TokensThreshold,
		messagesToKeep:               config.MessagesToKeep,
		minSelectedTokens:            config.MinSelectedTokensForCompression,
		compressionTargetTokens:      config.CompressionTargetTokens,
		summaryMergeTargetTokens:     config.SummaryMergeTargetTokens,
		accumulatedSummaryTokenLimit: config.AccumulatedSummaryTokenLimit,
		summaryMergeMinBlocks:        config.SummaryMergeMinBlocks,
		priorContextWindowSize:       config.PriorContextWindowSize,
		compressedPrompt:             compressedPrompt,
		cleanPrompt:                  defaultCleanPrompt,
	}

	// 应用选项
	for _, opt := range opts {
		opt(crc)
	}

	// 如果未通过选项注入 Model，则从配置创建
	if crc.model == nil {
		model, err := llm.NewModel(config.ModelClient, config.Model)
		if err != nil {
			return nil, err
		}
		crc.model = model
	}

	return crc, nil
}

// WithCurrentRoundModel 注入已有 Model 实例（测试用）。
func WithCurrentRoundModel(model *llm.Model) CurrentRoundCompressorOption {
	return func(crc *CurrentRoundCompressor) { crc.model = model }
}

// ProcessorType 返回处理器类型标识。
func (crc *CurrentRoundCompressor) ProcessorType() string { return "CurrentRoundCompressor" }

// TriggerAddMessages 判断是否需要介入消息添加。
//
// 触发条件：合并后的上下文 Token 数 > TokensThreshold。
// 前置条件：总消息数 < MessagesToKeep → 直接返回 false。
//
// 对应 Python: CurrentRoundCompressor.trigger_add_messages()
func (crc *CurrentRoundCompressor) TriggerAddMessages(ctx context.Context, mc iface.ModelContext, messagesToAdd []llm_schema.BaseMessage, _ ...iface.Option) (bool, error) {
	messageSize := mc.Len() + len(messagesToAdd)
	if messageSize < crc.messagesToKeep {
		return false, nil
	}

	modelName := crc.getModelName()
	allMsgs, _ := mc.GetMessages(0, true)
	tokens := processor.CountMessagesTokens(mc.TokenCounter(), append(allMsgs, messagesToAdd...), modelName, crc.ProcessorType())
	if tokens > crc.tokenThreshold {
		logger.Info(logger.ComponentAgentCore).
			Str("event_type", "CurrentRoundCompressor_triggered").
			Int("tokens", tokens).
			Int("threshold", crc.tokenThreshold).
			Msg("上下文 Token 数超过阈值")
		return true, nil
	}
	return false, nil
}

// OnAddMessages 执行当轮增量压缩。
//
// 1. 合并上下文和新消息
// 2. 查找可压缩索引
// 3. 执行两阶段压缩
// 4. MODEL_CALL_FAILED 时降级透传，其他错误抛出 CONTEXT_EXECUTION_ERROR
//
// 对应 Python: CurrentRoundCompressor.on_add_messages()
func (crc *CurrentRoundCompressor) OnAddMessages(ctx context.Context, mc iface.ModelContext, messagesToAdd []llm_schema.BaseMessage, _ ...iface.Option) (*iface.ContextEvent, []llm_schema.BaseMessage, error) {
	ctxMsgs, _ := mc.GetMessages(0, true)
	contextMessages := append(ctxMsgs, messagesToAdd...)
	crc.ResetCompressionUsage()

	lastUserIdx := crc.GetCompressIdx(contextMessages)
	if lastUserIdx == -1 {
		return nil, messagesToAdd, nil
	}

	keepStartIdx := len(contextMessages) - crc.messagesToKeep
	if keepStartIdx < 0 {
		keepStartIdx = 0
	}
	endIdx := keepStartIdx - 1

	compressedContext, modifiedIndices, compactSummary, err := crc.MultiCompress(ctx, mc, contextMessages, lastUserIdx, endIdx)
	if err != nil {
		// MODEL_CALL_FAILED 时降级跳过
		if isModelCallFailedError(err) {
			logger.Warn(logger.ComponentAgentCore).
				Str("processor_type", crc.ProcessorType()).
				Err(err).
				Msg("压缩模型调用失败，跳过当前处理器")
			return nil, messagesToAdd, nil
		}
		return nil, messagesToAdd, exception.NewBaseError(
			exception.StatusContextExecutionError,
			exception.WithMsg("压缩消息失败"),
			exception.WithCause(err),
		)
	}

	if compressedContext != nil {
		event := &iface.ContextEvent{
			EventType:        crc.ProcessorType(),
			MessagesToModify: modifiedIndices,
			CompactSummary:   compactSummary,
			CompressionUsage: crc.CurrentCompressionUsage(),
		}
		mc.SetMessages(compressedContext, true)
		return event, []llm_schema.BaseMessage{}, nil
	}

	return nil, messagesToAdd, nil
}

// GetCompressIdx 查找最新的可压缩 UserMessage 边界索引。
//
// 从后往前遍历，找到最后一条 UserMessage 的索引。
// 如果该索引在保留尾部范围内、或为最后一条消息、或未找到，返回 -1。
//
// 对应 Python: CurrentRoundCompressor.get_compress_idx()
func (crc *CurrentRoundCompressor) GetCompressIdx(messages []llm_schema.BaseMessage) int {
	compressedIdx := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if _, ok := messages[i].(*llm_schema.UserMessage); ok {
			compressedIdx = i
			break
		}
	}
	if compressedIdx == len(messages)-1 {
		return -1
	}
	if compressedIdx < 0 {
		return -1
	}
	keepIndex := len(messages) - crc.messagesToKeep
	if compressedIdx >= keepIndex {
		return -1
	}
	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "compress_idx_found").
		Int("compress_idx", compressedIdx).
		Int("keep_start_idx", keepIndex).
		Msg("找到可压缩索引")
	return compressedIdx
}

// MultiCompress 两阶段压缩：先压缩选定跨度，再合并旧记忆块。
//
// 对应 Python: CurrentRoundCompressor.multi_compress()
func (crc *CurrentRoundCompressor) MultiCompress(ctx context.Context, mc iface.ModelContext, contextMessages []llm_schema.BaseMessage, lastUserIdx int, endIdx int) ([]llm_schema.BaseMessage, []int, string, error) {
	updated := false
	var modifiedIndices []int
	var compactSummaryParts []string
	startIdx := lastUserIdx + 1
	actualEndIdx := endIdx

	// 第一阶段：压缩选定跨度
	if actualEndIdx >= startIdx {
		actualEndIdx = processor.FindLastCompletedAPIRoundEndIdx(contextMessages, startIdx, actualEndIdx)
	}
	if actualEndIdx >= startIdx {
		messagesToCompress := contextMessages[startIdx : actualEndIdx+1]
		compressedMsg, err := crc.Compress(ctx, mc, messagesToCompress, contextMessages, actualEndIdx, lastUserIdx)
		if err != nil {
			return nil, nil, "", err
		}
		if compressedMsg != nil {
			compactSummaryParts = append(compactSummaryParts, processor.MessageToText(compressedMsg))
			contextMessages = processor.ReplaceMessages(contextMessages, []processor.Replacement{
				{StartIdx: startIdx, EndIdx: actualEndIdx, Messages: []llm_schema.BaseMessage{compressedMsg}},
			})
			for idx := startIdx; idx <= actualEndIdx; idx++ {
				modifiedIndices = append(modifiedIndices, idx)
			}
			updated = true
		}
	}

	// 第二阶段：合并旧记忆块
	mergeRanges := processor.IterSummaryMergeRanges(contextMessages, currentRoundMemoryBlockMarker, crc.summaryMergeMinBlocks)
	for _, mergeRange := range mergeRanges {
		startIdx_ := mergeRange[0]
		endIdx_ := mergeRange[1]
		oldCompressMessages := contextMessages[startIdx_ : endIdx_+1]
		compressedMsg, err := crc.MergeSummaryBlocks(ctx, mc, oldCompressMessages)
		if err != nil {
			return nil, nil, "", err
		}
		if compressedMsg != nil {
			compactSummaryParts = append(compactSummaryParts, processor.MessageToText(compressedMsg))
			contextMessages = processor.ReplaceMessages(contextMessages, []processor.Replacement{
				{StartIdx: startIdx_, EndIdx: endIdx_, Messages: []llm_schema.BaseMessage{compressedMsg}},
			})
			for idx := startIdx_; idx <= endIdx_; idx++ {
				modifiedIndices = append(modifiedIndices, idx)
			}
			updated = true
			break // 仅处理第一个合并范围
		}
	}

	if !updated {
		return nil, modifiedIndices, "", nil
	}

	// 过滤空摘要部分
	var nonEmptyParts []string
	for _, part := range compactSummaryParts {
		if part != "" {
			nonEmptyParts = append(nonEmptyParts, part)
		}
	}
	return contextMessages, modifiedIndices, strings.Join(nonEmptyParts, "\n\n"), nil
}

// Compress 压缩一个选定跨度为单个记忆块。
//
// 仅当选定跨度 Token 数 >= minSelectedTokens 且压缩后有收益时才执行压缩。
//
// 对应 Python: CurrentRoundCompressor.compress()
func (crc *CurrentRoundCompressor) Compress(ctx context.Context, mc iface.ModelContext, messagesToCompress []llm_schema.BaseMessage, allContextMessages []llm_schema.BaseMessage, compressEndIdx int, currentQueryIdx int) (*llm_schema.UserMessage, error) {
	modelName := crc.getModelName()
	inputTokens := processor.CountMessagesTokens(mc.TokenCounter(), messagesToCompress, modelName, crc.ProcessorType())
	if inputTokens < crc.minSelectedTokens {
		logger.Debug(logger.ComponentAgentCore).
			Str("event_type", "compress_skipped").
			Int("input_tokens", inputTokens).
			Int("min_selected_tokens", crc.minSelectedTokens).
			Msg("选定跨度 Token 数低于最小阈值，跳过压缩")
		return nil, nil
	}

	// 收集先验摘要
	priorSummaries := ""
	recentContext := ""
	priorContextAndQuery := ""

	summaryIndices := processor.CollectSummaryIndices(allContextMessages, currentRoundMemoryBlockMarker)
	if len(summaryIndices) > 0 {
		var parts []string
		for _, idx := range summaryIndices {
			parts = append(parts, allContextMessages[idx].GetContent().Text())
		}
		priorSummaries = strings.Join(parts, "\n---\n")
	}

	recentContext = crc.FormatRecentContext(allContextMessages, compressEndIdx)
	priorContextAndQuery = crc.FormatPriorContextAndQuery(allContextMessages, currentQueryIdx)

	filledPrompt := crc.BuildPrompt(crc.compressionTargetTokens, priorSummaries, recentContext, priorContextAndQuery)

	// 将选定消息格式化后填入 {selected_messages} 占位符
	var processedParts []string
	for _, msg := range messagesToCompress {
		processedParts = append(processedParts, fmt.Sprintf("role:%s, content:%s", msg.GetRole().String(), msg.GetContent().Text()))
	}
	filledPrompt = strings.ReplaceAll(filledPrompt, "{selected_messages}", strings.Join(processedParts, "\n"))

	// 调用 LLM（纯文本输出，不使用 output_parser）
	response, err := crc.model.Invoke(ctx, model_clients.NewMessagesParam(llm_schema.NewUserMessage(filledPrompt)))
	if err != nil {
		logger.Warn(logger.ComponentAgentCore).
			Str("processor_type", crc.ProcessorType()).
			Err(err).
			Msg("压缩模型调用失败，跳过当前轮次压缩")
		return nil, nil
	}
	crc.RecordCompressionUsage(response)

	summary := response.GetContent().Text()
	if summary != "" {
		compressedTokens := processor.CountMessagesTokens(mc.TokenCounter(), []llm_schema.BaseMessage{llm_schema.NewUserMessage(summary)}, modelName, crc.ProcessorType())
		if compressedTokens >= inputTokens {
			logger.Debug(logger.ComponentAgentCore).
				Str("event_type", "compress_no_benefit").
				Int("compressed_tokens", compressedTokens).
				Int("input_tokens", inputTokens).
				Msg("压缩后 Token 数未减少，跳过")
			return nil, nil
		}
		logger.Info(logger.ComponentAgentCore).
			Str("event_type", "compress_success").
			Int("original_tokens", inputTokens).
			Int("compressed_tokens", compressedTokens).
			Int("saved_tokens", inputTokens-compressedTokens).
			Msg("当轮压缩成功")
	}

	return llm_schema.NewUserMessage(crc.WrapCurrentRoundMemoryBlock(summary)), nil
}

// MergeSummaryBlocks 合并多个历史记忆块为更短的记忆块。
//
// 当累积摘要 Token 数超过 accumulatedSummaryTokenLimit 时触发合并。
//
// 对应 Python: CurrentRoundCompressor._merge_summary_blocks()
func (crc *CurrentRoundCompressor) MergeSummaryBlocks(ctx context.Context, mc iface.ModelContext, oldCompressMessages []llm_schema.BaseMessage) (*llm_schema.UserMessage, error) {
	modelName := crc.getModelName()
	totalTokens := processor.CountMessagesTokens(mc.TokenCounter(), oldCompressMessages, modelName, crc.ProcessorType())
	if totalTokens <= crc.accumulatedSummaryTokenLimit {
		logger.Debug(logger.ComponentAgentCore).
			Str("event_type", "merge_skipped").
			Int("total_tokens", totalTokens).
			Int("limit", crc.accumulatedSummaryTokenLimit).
			Msg("累积摘要 Token 数未超限，跳过合并")
		return nil, nil
	}

	// 格式化历史记忆块
	var blockParts []string
	for i, msg := range oldCompressMessages {
		blockParts = append(blockParts, fmt.Sprintf("[MEMORY_BLOCK_%d]\n%s", i+1, msg.GetContent().Text()))
	}
	mergedBlocks := strings.Join(blockParts, "\n\n")

	filledPrompt := strings.ReplaceAll(crc.cleanPrompt, "{compress_len}", fmt.Sprintf("%d", crc.summaryMergeTargetTokens))
	filledPrompt = strings.ReplaceAll(filledPrompt, "{compressed_blocks}", mergedBlocks)
	if mergedBlocks == "" {
		filledPrompt = strings.ReplaceAll(crc.cleanPrompt, "{compressed_blocks}", "(none)")
		filledPrompt = strings.ReplaceAll(filledPrompt, "{compress_len}", fmt.Sprintf("%d", crc.summaryMergeTargetTokens))
	}

	modelMessages := []llm_schema.BaseMessage{llm_schema.NewUserMessage(filledPrompt)}
	response, err := crc.model.Invoke(ctx, model_clients.NewMessagesParam(modelMessages...))
	if err != nil {
		logger.Warn(logger.ComponentAgentCore).
			Str("processor_type", crc.ProcessorType()).
			Err(err).
			Msg("摘要合并模型调用失败，跳过合并")
		return nil, nil
	}
	crc.RecordCompressionUsage(response)

	summaryText := response.GetContent().Text()
	if summaryText != "" {
		mergedTokens := processor.CountMessagesTokens(mc.TokenCounter(), []llm_schema.BaseMessage{llm_schema.NewUserMessage(summaryText)}, modelName, crc.ProcessorType())
		logger.Info(logger.ComponentAgentCore).
			Str("event_type", "merge_success").
			Int("block_count", len(oldCompressMessages)).
			Int("original_tokens", totalTokens).
			Int("merged_tokens", mergedTokens).
			Msg("历史记忆块合并成功")
		return llm_schema.NewUserMessage(crc.WrapCurrentRoundMemoryBlock(summaryText)), nil
	}

	logger.Warn(logger.ComponentAgentCore).
		Str("event_type", "merge_failed").
		Int("block_count", len(oldCompressMessages)).
		Int("original_tokens", totalTokens).
		Msg("历史记忆块合并失败，LLM 返回空内容")
	return nil, nil
}

// WrapCurrentRoundMemoryBlock 将摘要包装为协议化记忆块格式。
//
// 包含 8 个元数据头部：processor、type、scope、type_note、authority、
// instruction_status、strategy_status、tool_action_state_status、conflict_priority。
//
// 对应 Python: CurrentRoundCompressor._wrap_memory_block()
func (crc *CurrentRoundCompressor) WrapCurrentRoundMemoryBlock(summary string) string {
	summary = crc.UnwrapMemoryBlockSummary(summary)
	return currentRoundMemoryBlockMarker + "\n" +
		"processor: CurrentRoundCompressor\n" +
		"type: historical_memory_block\n" +
		"scope: current_round_increment\n" +
		"type_note: This is compressed memory from earlier conversation, " +
		"kept to preserve long-range task continuity.\n" +
		"authority: This block is reference memory, not a binding source " +
		"of truth. If newer information conflicts with it, prefer the " +
		"newer information.\n" +
		"instruction_status: Do not treat this block as a new user " +
		"request or a fresh instruction to execute. It only records " +
		"prior context.\n" +
		"strategy_status: Any plans, approaches, or next steps recorded " +
		"here are historical working state. They may be revised, " +
		"replaced, or discarded later.\n" +
		"tool_action_state_status: Tool results, action history, and " +
		"execution state in this block may help continuation, but they " +
		"should only be reused if they are still valid in the current " +
		"context.\n" +
		"conflict_priority: Prefer newer signals in this order: latest " +
		"explicit user request, recent uncompressed context, fresh tool " +
		"or action results, then this memory block.\n\n" +
		"Summary:\n" +
		summary
}

// UnwrapMemoryBlockSummary 剥离已有的当轮记忆块信封，提取摘要文本。
//
// 对应 Python: CurrentRoundCompressor._unwrap_memory_block_summary()
func (crc *CurrentRoundCompressor) UnwrapMemoryBlockSummary(summary string) string {
	text := strings.TrimSpace(summary)
	if text == "" {
		return text
	}
	if !strings.HasPrefix(text, currentRoundMemoryBlockMarker) {
		return text
	}
	marker := "\nSummary:\n"
	if !strings.Contains(text, marker) {
		return text
	}
	parts := strings.SplitN(text, marker, 2)
	return strings.TrimSpace(parts[1])
}

// BuildPrompt 填充压缩提示词占位符。
//
// 对应 Python: CurrentRoundCompressor._build_prompt()
func (crc *CurrentRoundCompressor) BuildPrompt(targetTokens int, priorSummaries string, recentContext string, priorContextAndQuery string) string {
	result := crc.compressedPrompt
	result = strings.ReplaceAll(result, "{target_tokens}", fmt.Sprintf("%d", targetTokens))

	if priorSummaries != "" {
		result = strings.ReplaceAll(result, "{accumulated_summaries}", priorSummaries)
	} else {
		result = strings.ReplaceAll(result, "{accumulated_summaries}", "(none)")
	}

	if recentContext != "" {
		result = strings.ReplaceAll(result, "{recent_messages}", recentContext)
	} else {
		result = strings.ReplaceAll(result, "{recent_messages}", "(none)")
	}

	if priorContextAndQuery != "" {
		result = strings.ReplaceAll(result, "{prior_context_and_query}", priorContextAndQuery)
	} else {
		result = strings.ReplaceAll(result, "{prior_context_and_query}", "(none)")
	}

	return result
}

// FormatRecentContext 序列化压缩跨度后的尾部消息作为边界上下文。
//
// 排除已有的记忆块消息（避免重复输入）。
//
// 对应 Python: CurrentRoundCompressor._format_recent_context()
func (crc *CurrentRoundCompressor) FormatRecentContext(allContextMessages []llm_schema.BaseMessage, endIdx int) string {
	var recentMessages []llm_schema.BaseMessage
	for _, msg := range allContextMessages[endIdx+1:] {
		if processor.IsSummaryMessage(msg, currentRoundMemoryBlockMarker) {
			continue
		}
		recentMessages = append(recentMessages, msg)
	}
	if len(recentMessages) == 0 {
		return ""
	}
	var parts []string
	for _, msg := range recentMessages {
		parts = append(parts, fmt.Sprintf("role:%s, content:%s", msg.GetRole().String(), msg.GetContent().Text()))
	}
	return strings.Join(parts, "\n")
}

// FormatPriorContextAndQuery 格式化用户意图上下文窗口 + 当前查询消息。
//
// 仅包含纯 UserMessage（非摘要）和不含 tool_calls 的 AssistantMessage。
//
// 对应 Python: CurrentRoundCompressor._format_prior_context_and_query()
func (crc *CurrentRoundCompressor) FormatPriorContextAndQuery(allContextMessages []llm_schema.BaseMessage, currentQueryIdx int) string {
	var lines []string
	var priorMessages []llm_schema.BaseMessage

	if currentQueryIdx > 0 {
		for _, msg := range allContextMessages[:currentQueryIdx] {
			isPlainUser := false
			if um, ok := msg.(*llm_schema.UserMessage); ok && !processor.IsSummaryMessage(um, currentRoundMemoryBlockMarker) {
				isPlainUser = true
			}
			isPlainAssistant := false
			if am, ok := msg.(*llm_schema.AssistantMessage); ok && len(am.ToolCalls) == 0 {
				isPlainAssistant = true
			}
			if isPlainUser || isPlainAssistant {
				priorMessages = append(priorMessages, msg)
			}
		}
		// 截取最后 priorContextWindowSize 条
		if len(priorMessages) > crc.priorContextWindowSize {
			priorMessages = priorMessages[len(priorMessages)-crc.priorContextWindowSize:]
		}
	}

	for _, msg := range priorMessages {
		lines = append(lines, fmt.Sprintf("role:%s, content:%s", msg.GetRole().String(), msg.GetContent().Text()))
	}

	if currentQueryIdx >= 0 && currentQueryIdx < len(allContextMessages) {
		queryMsg := allContextMessages[currentQueryIdx]
		lines = append(lines, fmt.Sprintf("\n--- Current User Intent ---\nrole:%s, content:%s", queryMsg.GetRole().String(), queryMsg.GetContent().Text()))
	}

	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n")
}

// OnGetContextWindow 处理即将返回的上下文窗口（空操作）。
func (crc *CurrentRoundCompressor) OnGetContextWindow(_ context.Context, _ iface.ModelContext, cw iface.ContextWindow, _ ...iface.Option) (*iface.ContextEvent, iface.ContextWindow, error) {
	return nil, cw, nil
}

// TriggerGetContextWindow 判断是否需要介入上下文窗口获取（始终不触发）。
func (crc *CurrentRoundCompressor) TriggerGetContextWindow(_ context.Context, _ iface.ModelContext, _ iface.ContextWindow, _ ...iface.Option) (bool, error) {
	return false, nil
}

// SaveState 导出处理器内部状态（空操作）。
func (crc *CurrentRoundCompressor) SaveState() map[string]any {
	return make(map[string]any)
}

// LoadState 从 map 恢复处理器内部状态（空操作）。
func (crc *CurrentRoundCompressor) LoadState(_ map[string]any) {}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getModelName 获取模型名称
func (crc *CurrentRoundCompressor) getModelName() string {
	if crc.model != nil && crc.model.ModelConfig != nil {
		return crc.model.ModelConfig.ModelName
	}
	return ""
}

// init 自动注册到 context_engine 注册表
func init() {
	context_engine.RegisterProcessorFactory("CurrentRoundCompressor",
		func(config iface.ProcessorConfig) (iface.ContextProcessor, error) {
			cfg, ok := config.(*CurrentRoundCompressorConfig)
			if !ok {
				return nil, fmt.Errorf("CurrentRoundCompressor: 配置类型不匹配，期望 *CurrentRoundCompressorConfig，实际 %T", config)
			}
			crc, err := NewCurrentRoundCompressor(cfg)
			if err != nil {
				return nil, err
			}
			return crc, nil
		},
	)
}
