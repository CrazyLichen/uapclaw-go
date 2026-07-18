package compressor

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
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
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RoundLevelCompressorConfig 轮级压缩器配置。
//
// 当全量上下文 Token 超过触发阈值时，按轮次（ReAct block）渐进式压缩：
// L0→L1 递归合并 → 激进压缩（保留近期）→ 激进压缩（全量）→ 硬截断。
//
// 对应 Python: RoundLevelCompressorConfig (pydantic.BaseModel)
type RoundLevelCompressorConfig struct {
	// TriggerTotalTokens 触发压缩的全量上下文 Token 阈值
	TriggerTotalTokens int
	// TargetTotalTokens 压缩目标全量上下文 Token 数
	TargetTotalTokens int
	// KeepRecentMessages 压缩时保留最近 N 条消息不压缩
	KeepRecentMessages int
	// Model 压缩模型请求配置
	Model *llm_schema.ModelRequestConfig
	// ModelClient 压缩模型客户端配置
	ModelClient *llm_schema.ModelClientConfig
	// CompressionCallMaxTokens 单次压缩 LLM 调用 Token 上限
	CompressionCallMaxTokens int
	// FirstPassTargetTokens 一阶段压缩（L0→L1）目标摘要 Token 数
	FirstPassTargetTokens int
	// SecondPassTargetTokens 二阶段激进压缩（保留近期）目标摘要 Token 数
	SecondPassTargetTokens int
	// ThirdPassTargetTokens 三阶段激进压缩（全量）目标摘要 Token 数
	ThirdPassTargetTokens int
	// TruncateHeadRatio 硬截断头部保留比例
	TruncateHeadRatio float64
	// TruncatedMarker 硬截断省略标记
	TruncatedMarker string
	// CompressionMarker 轮级记忆块标记前缀
	CompressionMarker string
	// CustomCompressionPrompt 自定义普通压缩提示词，空字符串表示使用内置提示词
	CustomCompressionPrompt string
	// CustomAggressiveCompressionPrompt 自定义激进压缩提示词，空字符串表示使用内置提示词
	CustomAggressiveCompressionPrompt string
}

// roundCompressTarget 轮级压缩目标，对应 Python _CompressTarget
type roundCompressTarget struct {
	// blockID 块标识
	blockID string
	// scope 块范围类型（completed_react / ongoing_react / existing_round_level_block / recursive_merge / mixed_context）
	scope string
	// startIdx 替换范围起始索引（含）
	startIdx int
	// endIdx 替换范围结束索引（含）
	endIdx int
	// messages 目标块内的消息列表
	messages []llm_schema.BaseMessage
	// currentLevel 当前压缩级别
	currentLevel int
	// nextLevel 压缩后级别
	nextLevel int
	// sourceBlockCount 源块数量（递归合并时 >1）
	sourceBlockCount int
}

// RoundLevelCompressor 轮级压缩器，按 ReAct 轮次渐进式压缩上下文。
//
// 五级降级链：
//  1. 递归压缩（L0→L1 + L1→L2 ...）
//  2. 激进压缩（保留近期）
//  3. 激进压缩（全量）
//  4. 硬截断
//
// 对应 Python: openjiuwen/core/context_engine/processor/compressor/round_level_compressor.py (RoundLevelCompressor)
type RoundLevelCompressor struct {
	*processor.BaseProcessor
	// model 压缩用 LLM 实例
	model *llm.Model
	// targetTotalTokens 压缩目标全量上下文 Token 数
	targetTotalTokens int
	// triggerTotalTokens 触发压缩的全量上下文 Token 阈值
	triggerTotalTokens int
	// compressionCallMaxTokens 单次压缩 LLM 调用 Token 上限
	compressionCallMaxTokens int
	// keepRecentMessages 压缩时保留最近 N 条消息不压缩
	keepRecentMessages int
	// firstPrompt 普通压缩提示词
	firstPrompt string
	// aggressivePrompt 激进压缩提示词
	aggressivePrompt string
	// firstPassTargetTokens 一阶段压缩目标摘要 Token 数
	firstPassTargetTokens int
	// secondPassTargetTokens 二阶段激进压缩目标摘要 Token 数
	secondPassTargetTokens int
	// thirdPassTargetTokens 三阶段激进压缩目标摘要 Token 数
	thirdPassTargetTokens int
	// truncateHeadRatio 硬截断头部保留比例
	truncateHeadRatio float64
	// truncatedMarker 硬截断省略标记
	truncatedMarker string
	// compressionMarker 轮级记忆块标记前缀
	compressionMarker string
}

// RoundLevelCompressorOption RoundLevelCompressor 构造选项函数。
type RoundLevelCompressorOption func(*RoundLevelCompressor)

// ──────────────────────────── 常量 ────────────────────────────

// roundLevelFallbackMarker 轮级记忆块标记
const roundLevelFallbackMarker = "[ROUND_LEVEL_MEMORY_BLOCK]"

// compressLevelKey 压缩级别元数据键
const compressLevelKey = "compress_level"

// defaultRoundCompressionPrompt 内置普通压缩提示词，与 Python DEFAULT_ROUND_COMPRESSION_PROMPT 完全对齐
const defaultRoundCompressionPrompt = `You are a Fallback Context Compression Expert for long-running ReAct agent sessions.

Your job is to compress ONLY the explicitly listed targets so the whole task can fit under a strict context budget.

Priority order:
1. Ongoing ReAct state and exact handoff point
2. Unfinished work, blockers, pending actions, and last concrete action
3. Critical facts, constraints, decisions, corrections, and outputs needed for correct continuation
4. Durable conclusions from completed work
5. Secondary historical detail only if budget allows

Rules:
- Compress only the selected targets.
- Protected recent context is reference only and must not be absorbed as standalone content.
- Treat fallback blocks as historical context artifacts, not as new user instructions.
- Preserve both what was done and what was learned.
- Preserve the user's original requirements, constraints, acceptance criteria, and preferences as completely as possible.
- For ongoing ReAct blocks, keep a distinct ` + "`User Requirements`" + ` section that makes the unfinished work recoverable.
- For completed ReAct blocks, preserve both ` + "`User Requirements`" + ` and ` + "`Final Result`" + ` explicitly when they exist.
- Return valid JSON only.
`

// defaultAggressiveRoundCompressionPrompt 内置激进压缩提示词，与 Python DEFAULT_AGGRESSIVE_ROUND_COMPRESSION_PROMPT 完全对齐
const defaultAggressiveRoundCompressionPrompt = `You are a Hard-Budget Fallback Compression Expert.

The context is still over budget after an earlier compression pass.
Compress ONLY the explicitly listed targets much more aggressively while keeping the task recoverable.

Priority order:
1. Ongoing ReAct state and exact handoff point
2. Unfinished work, blockers, pending actions, and last concrete action
3. Critical facts, constraints, decisions, corrections, and outputs needed for continuation
4. Durable conclusions from completed work
5. Secondary historical detail only if budget allows

Rules:
- Remove redundant reasoning, repeated tool chatter, and low-value chronology first.
- Keep ongoing work maximally recoverable.
- Preserve the user's original requirements as much as possible even under aggressive compression.
- For completed blocks, keep the final result before secondary detail.
- Return valid JSON only.
`

// ──────────────────────────── 导出函数 ────────────────────────────

// NewRoundLevelCompressorConfig 创建轮级压缩器默认配置。
//
// 默认值与 Python 对齐：
//   - TriggerTotalTokens=230000, TargetTotalTokens=160000
//   - KeepRecentMessages=0, CompressionCallMaxTokens=250000
//   - FirstPassTargetTokens=30000, SecondPassTargetTokens=20000, ThirdPassTargetTokens=10000
//   - TruncateHeadRatio=0.2, TruncatedMarker="...[TRUNCATED]..."
//   - CompressionMarker="[ROUND_LEVEL_MEMORY_BLOCK]"
func NewRoundLevelCompressorConfig() *RoundLevelCompressorConfig {
	return &RoundLevelCompressorConfig{
		TriggerTotalTokens:                230000,
		TargetTotalTokens:                 160000,
		KeepRecentMessages:                0,
		CompressionCallMaxTokens:          250000,
		FirstPassTargetTokens:             30000,
		SecondPassTargetTokens:            20000,
		ThirdPassTargetTokens:             10000,
		TruncateHeadRatio:                 0.2,
		TruncatedMarker:                   "...[TRUNCATED]...",
		CompressionMarker:                 roundLevelFallbackMarker,
		CustomCompressionPrompt:           "",
		CustomAggressiveCompressionPrompt: "",
	}
}

// Validate 校验轮级压缩器配置。
func (c *RoundLevelCompressorConfig) Validate() error {
	if c.TriggerTotalTokens <= 0 {
		return fmt.Errorf("RoundLevelCompressorConfig.TriggerTotalTokens 必须大于 0，当前值: %d", c.TriggerTotalTokens)
	}
	if c.TargetTotalTokens <= 0 {
		return fmt.Errorf("RoundLevelCompressorConfig.TargetTotalTokens 必须大于 0，当前值: %d", c.TargetTotalTokens)
	}
	if c.KeepRecentMessages < 0 {
		return fmt.Errorf("RoundLevelCompressorConfig.KeepRecentMessages 不能为负数，当前值: %d", c.KeepRecentMessages)
	}
	if c.CompressionCallMaxTokens <= 0 {
		return fmt.Errorf("RoundLevelCompressorConfig.CompressionCallMaxTokens 必须大于 0，当前值: %d", c.CompressionCallMaxTokens)
	}
	if c.FirstPassTargetTokens <= 0 {
		return fmt.Errorf("RoundLevelCompressorConfig.FirstPassTargetTokens 必须大于 0，当前值: %d", c.FirstPassTargetTokens)
	}
	if c.SecondPassTargetTokens <= 0 {
		return fmt.Errorf("RoundLevelCompressorConfig.SecondPassTargetTokens 必须大于 0，当前值: %d", c.SecondPassTargetTokens)
	}
	if c.ThirdPassTargetTokens <= 0 {
		return fmt.Errorf("RoundLevelCompressorConfig.ThirdPassTargetTokens 必须大于 0，当前值: %d", c.ThirdPassTargetTokens)
	}
	if c.TruncateHeadRatio <= 0.0 || c.TruncateHeadRatio >= 1.0 {
		return fmt.Errorf("RoundLevelCompressorConfig.TruncateHeadRatio 必须在 (0, 1) 之间，当前值: %f", c.TruncateHeadRatio)
	}
	if c.TruncatedMarker == "" {
		return fmt.Errorf("RoundLevelCompressorConfig.TruncatedMarker 不能为空")
	}
	if c.CompressionMarker == "" {
		return fmt.Errorf("RoundLevelCompressorConfig.CompressionMarker 不能为空")
	}
	return nil
}

// SetModelDefaults 设置默认模型配置。
func (c *RoundLevelCompressorConfig) SetModelDefaults(model *llm_schema.ModelRequestConfig, modelClient *llm_schema.ModelClientConfig) {
	if c.Model == nil && model != nil {
		c.Model = model
	}
	if c.ModelClient == nil && modelClient != nil {
		c.ModelClient = modelClient
	}
}

// GetModel 返回模型请求配置。
func (c *RoundLevelCompressorConfig) GetModel() *llm_schema.ModelRequestConfig {
	return c.Model
}

// NewRoundLevelCompressor 创建轮级压缩器实例。
//
// 对应 Python: RoundLevelCompressor.__init__(config)
func NewRoundLevelCompressor(config *RoundLevelCompressorConfig, opts ...RoundLevelCompressorOption) (*RoundLevelCompressor, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	bp := processor.NewBaseProcessor(config)

	firstPrompt := config.CustomCompressionPrompt
	if firstPrompt == "" {
		firstPrompt = defaultRoundCompressionPrompt
	}
	aggressivePrompt := config.CustomAggressiveCompressionPrompt
	if aggressivePrompt == "" {
		aggressivePrompt = defaultAggressiveRoundCompressionPrompt
	}

	rlc := &RoundLevelCompressor{
		BaseProcessor:            bp,
		targetTotalTokens:        config.TargetTotalTokens,
		triggerTotalTokens:       config.TriggerTotalTokens,
		compressionCallMaxTokens: config.CompressionCallMaxTokens,
		keepRecentMessages:       config.KeepRecentMessages,
		firstPrompt:              firstPrompt,
		aggressivePrompt:         aggressivePrompt,
		firstPassTargetTokens:    config.FirstPassTargetTokens,
		secondPassTargetTokens:   config.SecondPassTargetTokens,
		thirdPassTargetTokens:    config.ThirdPassTargetTokens,
		truncateHeadRatio:        config.TruncateHeadRatio,
		truncatedMarker:          config.TruncatedMarker,
		compressionMarker:        config.CompressionMarker,
	}

	// 应用选项
	for _, opt := range opts {
		opt(rlc)
	}

	// 如果未通过选项注入 Model，则从配置创建
	if rlc.model == nil {
		model, err := llm.NewModel(config.ModelClient, config.Model)
		if err != nil {
			return nil, err
		}
		rlc.model = model
	}

	return rlc, nil
}

// WithRoundLevelModel 注入已有 Model 实例（测试用）。
func WithRoundLevelModel(model *llm.Model) RoundLevelCompressorOption {
	return func(rlc *RoundLevelCompressor) { rlc.model = model }
}

// ProcessorType 返回处理器类型标识。
func (rlc *RoundLevelCompressor) ProcessorType() string { return "RoundLevelCompressor" }

// TriggerAddMessages 判断是否需要介入消息添加。
//
// 触发条件：合并后的上下文 Token 数 > TriggerTotalTokens。
//
// 对应 Python: RoundLevelCompressor.trigger_add_messages()
func (rlc *RoundLevelCompressor) TriggerAddMessages(ctx context.Context, mc iface.ModelContext, messagesToAdd []llm_schema.BaseMessage, _ ...iface.Option) (bool, error) {
	allMsgs, _ := mc.GetMessages(0, true)
	allMessages := append(allMsgs, messagesToAdd...)
	totalTokens := rlc.countContextWindowTokens(nil, allMessages, nil, mc)
	if totalTokens > rlc.triggerTotalTokens {
		logger.Info(logger.ComponentAgentCore).
			Str("event_type", "RoundLevelCompressor_triggered").
			Int("total_tokens", totalTokens).
			Int("trigger_total_tokens", rlc.triggerTotalTokens).
			Msg("上下文 Token 数超过触发阈值")
		return true, nil
	}
	return false, nil
}

// OnAddMessages 执行轮级压缩。
//
// 对应 Python: RoundLevelCompressor.on_add_messages()
func (rlc *RoundLevelCompressor) OnAddMessages(ctx context.Context, mc iface.ModelContext, messagesToAdd []llm_schema.BaseMessage, _ ...iface.Option) (*iface.ContextEvent, []llm_schema.BaseMessage, error) {
	allMsgs, _ := mc.GetMessages(0, true)
	allMessages := append(allMsgs, messagesToAdd...)
	rlc.ResetCompressionUsage()

	compressedMessages, err := rlc.compressUntilTarget(ctx, allMessages, mc, nil, nil, rlc.keepRecentMessages, false)
	if err != nil {
		// MODEL_CALL_FAILED 时降级跳过
		if isModelCallFailedError(err) {
			logger.Warn(logger.ComponentAgentCore).
				Str("processor_type", rlc.ProcessorType()).
				Err(err).
				Msg("压缩模型调用失败，跳过当前处理器")
			return nil, messagesToAdd, nil
		}
		return nil, messagesToAdd, exception.NewBaseError(
			exception.StatusContextExecutionError,
			exception.WithMsg("轮次级别压缩失败"),
			exception.WithCause(err),
		)
	}

	if len(compressedMessages) == len(allMessages) && rlc.messagesEqual(compressedMessages, allMessages) {
		return nil, messagesToAdd, nil
	}

	mc.SetMessages(compressedMessages, true)
	event := &iface.ContextEvent{
		EventType:        rlc.ProcessorType(),
		MessagesToModify: buildRangeIndices(0, len(allMessages)-1),
		CompactSummary:   rlc.extractCompactSummary(compressedMessages),
		CompressionUsage: rlc.CurrentCompressionUsage(),
	}
	return event, []llm_schema.BaseMessage{}, nil
}

// TriggerGetContextWindow 判断是否需要介入上下文窗口获取。
//
// 触发条件：全量上下文 Token 数 > TriggerTotalTokens。
//
// 对应 Python: RoundLevelCompressor.trigger_get_context_window()
func (rlc *RoundLevelCompressor) TriggerGetContextWindow(ctx context.Context, mc iface.ModelContext, cw iface.ContextWindow, _ ...iface.Option) (bool, error) {
	totalTokens := rlc.countContextWindowTokens(cw.SystemMessages, cw.ContextMessages, cw.Tools, mc)
	return totalTokens > rlc.triggerTotalTokens, nil
}

// OnGetContextWindow 执行轮级压缩并更新上下文窗口。
//
// 对应 Python: RoundLevelCompressor.on_get_context_window()
func (rlc *RoundLevelCompressor) OnGetContextWindow(ctx context.Context, mc iface.ModelContext, cw iface.ContextWindow, _ ...iface.Option) (*iface.ContextEvent, iface.ContextWindow, error) {
	rlc.ResetCompressionUsage()
	totalTokens := rlc.countContextWindowTokens(cw.SystemMessages, cw.ContextMessages, cw.Tools, mc)
	if totalTokens <= rlc.targetTotalTokens {
		return nil, cw, nil
	}

	compressedMessages, err := rlc.compressUntilTarget(ctx, cw.ContextMessages, mc, cw.SystemMessages, cw.Tools, 0, false)
	if err != nil {
		// 区分模型调用失败与其他错误：兜底压缩失败不允许降级，但错误信息需明确标识原因
		if isModelCallFailedError(err) {
			return nil, cw, exception.NewBaseError(
				exception.StatusModelCallFailed,
				exception.WithMsg("轮次级别压缩失败：模型调用失败"),
				exception.WithCause(err),
			)
		}
		return nil, cw, exception.NewBaseError(
			exception.StatusContextExecutionError,
			exception.WithMsg("轮次级别压缩失败"),
			exception.WithCause(err),
		)
	}

	originalContextLen := len(cw.ContextMessages)
	cw.ContextMessages = compressedMessages
	mc.SetMessages(compressedMessages, true)

	event := &iface.ContextEvent{
		EventType:        rlc.ProcessorType(),
		MessagesToModify: buildRangeIndices(0, originalContextLen-1),
		CompactSummary:   rlc.extractCompactSummary(compressedMessages),
		CompressionUsage: rlc.CurrentCompressionUsage(),
	}
	return event, cw, nil
}

// SaveState 导出处理器内部状态（空操作）。
func (rlc *RoundLevelCompressor) SaveState() map[string]any {
	return make(map[string]any)
}

// LoadState 从 map 恢复处理器内部状态（空操作）。
func (rlc *RoundLevelCompressor) LoadState(_ map[string]any) {}

// ──────────────────────────── 非导出函数 ────────────────────────────

// compressUntilTarget 核心编排，五级降级链。
//
// 对应 Python: RoundLevelCompressor._compress_until_target()
func (rlc *RoundLevelCompressor) compressUntilTarget(
	ctx context.Context,
	contextMessages []llm_schema.BaseMessage,
	mc iface.ModelContext,
	systemMessages []llm_schema.BaseMessage,
	tools []schema.ToolInfoInterface,
	keepRecent int,
	force bool,
) ([]llm_schema.BaseMessage, error) {
	working := make([]llm_schema.BaseMessage, len(contextMessages))
	copy(working, contextMessages)

	if !force && rlc.isUnderContextWindowBudget(systemMessages, working, tools, mc) {
		return working, nil
	}

	// 第一级：递归压缩（L0→L1 + L1→L2 ...）
	recursiveUpdated, err := rlc.runRecursiveCompression(ctx, working, mc, systemMessages, tools, keepRecent)
	if err != nil {
		return nil, err
	}
	if recursiveUpdated != nil {
		working = recursiveUpdated
	}

	if rlc.isUnderContextWindowBudget(systemMessages, working, tools, mc) {
		return working, nil
	}

	// 第二级：激进压缩（保留近期）
	aggressiveKeepRecent, err := rlc.runAggressivePhase(ctx, working, mc, systemMessages, tools, keepRecent, rlc.secondPassTargetTokens, "aggressive_keep_recent")
	if err != nil {
		return nil, err
	}
	if aggressiveKeepRecent != nil {
		working = aggressiveKeepRecent
	}

	if rlc.isUnderContextWindowBudget(systemMessages, working, tools, mc) {
		return working, nil
	}

	// 第三级：激进压缩（全量）
	aggressiveFull, err := rlc.runAggressivePhase(ctx, working, mc, systemMessages, tools, 0, rlc.thirdPassTargetTokens, "aggressive_full_context")
	if err != nil {
		return nil, err
	}
	if aggressiveFull != nil {
		working = aggressiveFull
	}

	if rlc.isUnderContextWindowBudget(systemMessages, working, tools, mc) {
		return working, nil
	}

	// 第五级：硬截断
	return rlc.truncateToTarget(working, mc, systemMessages, tools), nil
}

// runRecursiveCompression 递归压缩，先压缩 L0 原始块，再逐步合并同级别记忆块。
//
// 对应 Python: RoundLevelCompressor._run_recursive_compression()
func (rlc *RoundLevelCompressor) runRecursiveCompression(
	ctx context.Context,
	messages []llm_schema.BaseMessage,
	mc iface.ModelContext,
	systemMessages []llm_schema.BaseMessage,
	tools []schema.ToolInfoInterface,
	keepRecent int,
) ([]llm_schema.BaseMessage, error) {
	working := make([]llm_schema.BaseMessage, len(messages))
	copy(working, messages)
	changed := false

	compressEnd := len(working) - keepRecent - 1
	if compressEnd >= 0 {
		rawTargets := rlc.buildRawTargets(working, compressEnd)
		if len(rawTargets) > 0 {
			updated, err := rlc.applyLLMPhase(ctx, working, mc, systemMessages, tools, rawTargets, rlc.firstPassTargetTokens, false, "l0_to_l1", keepRecent)
			if err != nil {
				return nil, err
			}
			if updated != nil {
				working = updated
				changed = true
			}
		}
	}

	for !rlc.isUnderContextWindowBudget(systemMessages, working, tools, mc) {
		compressEnd = len(working) - keepRecent - 1
		if compressEnd < 0 {
			break
		}
		mergeTargets := rlc.buildRecursiveMergeTargets(working, compressEnd)
		if len(mergeTargets) == 0 {
			break
		}
		updated, err := rlc.applyLLMPhase(ctx, working, mc, systemMessages, tools, mergeTargets, rlc.firstPassTargetTokens, false, fmt.Sprintf("recursive_merge_l%d_to_l%d", mergeTargets[0].currentLevel, mergeTargets[0].nextLevel), keepRecent)
		if err != nil {
			return nil, err
		}
		if updated == nil || rlc.messagesEqual(updated, working) {
			break
		}
		working = updated
		changed = true
	}

	if changed {
		return working, nil
	}
	return nil, nil
}

// runAggressivePhase 激进压缩阶段。
//
// 对应 Python: RoundLevelCompressor._run_aggressive_phase()
func (rlc *RoundLevelCompressor) runAggressivePhase(
	ctx context.Context,
	messages []llm_schema.BaseMessage,
	mc iface.ModelContext,
	systemMessages []llm_schema.BaseMessage,
	tools []schema.ToolInfoInterface,
	keepRecent int,
	targetTokens int,
	phaseName string,
) ([]llm_schema.BaseMessage, error) {
	compressEnd := len(messages) - keepRecent - 1
	if compressEnd < 0 {
		return nil, nil
	}

	targets := rlc.buildAggressiveTargets(messages, compressEnd)
	if len(targets) == 0 {
		return nil, nil
	}
	return rlc.applyLLMPhase(ctx, messages, mc, systemMessages, tools, targets, targetTokens, true, phaseName, keepRecent)
}

// applyLLMPhase 调用 LLM 执行压缩阶段。
//
// 对应 Python: RoundLevelCompressor._apply_llm_phase()
// MODEL_CALL_FAILED 错误直接返回 error，由上层 OnAddMessages 判断是否降级。
func (rlc *RoundLevelCompressor) applyLLMPhase(
	ctx context.Context,
	messages []llm_schema.BaseMessage,
	mc iface.ModelContext,
	systemMessages []llm_schema.BaseMessage,
	tools []schema.ToolInfoInterface,
	targets []roundCompressTarget,
	targetTokens int,
	aggressive bool,
	phaseName string,
	keepRecentMessages int,
) ([]llm_schema.BaseMessage, error) {
	modelMessages := rlc.prepareRoundCompressionMessages(messages, targets, mc, phaseName, targetTokens, aggressive, keepRecentMessages, systemMessages, tools)
	if modelMessages == nil {
		logger.Warn(logger.ComponentAgentCore).
			Str("event_type", "RoundLevelCompressor_phase_skipped").
			Str("phase", phaseName).
			Msg("压缩调用预算不足，跳过阶段")
		return nil, nil
	}

	response, err := rlc.getModel().Invoke(ctx, model_clients.NewMessagesParam(modelMessages...), model_clients.WithInvokeOutputParser(output_parsers.NewJsonOutputParser()))
	if err != nil {
		return nil, exception.NewBaseError(
			exception.StatusModelCallFailed,
			exception.WithMsg(fmt.Sprintf("%s 调用压缩模型失败 phase=%s", rlc.ProcessorType(), phaseName)),
			exception.WithCause(err),
		)
	}
	rlc.RecordCompressionUsage(response)

	replacements := rlc.buildJSONReplacements(targets, response.ParserContent, mc)
	if len(replacements) == 0 {
		contentText := strings.TrimSpace(response.GetContent().Text())
		if contentText != "" {
			fallback := rlc.buildRawFallbackReplacement(targets, contentText, mc)
			if fallback != nil {
				replacements = []processor.Replacement{*fallback}
			}
		}
	}

	if len(replacements) == 0 {
		logger.Warn(logger.ComponentAgentCore).
			Str("event_type", "RoundLevelCompressor_no_replacements").
			Str("phase", phaseName).
			Msg("压缩阶段未产生有效替换")
		return nil, nil
	}

	updatedMessages := processor.ReplaceMessages(messages, replacements)
	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "RoundLevelCompressor_phase_complete").
		Str("phase", phaseName).
		Int("before_tokens", rlc.countContextWindowTokens(systemMessages, messages, tools, mc)).
		Int("after_tokens", rlc.countContextWindowTokens(systemMessages, updatedMessages, tools, mc)).
		Msg("压缩阶段完成")
	return updatedMessages, nil
}

// prepareRoundCompressionMessages 构建压缩 LLM 调用的消息列表。
//
// 对应 Python: RoundLevelCompressor._prepare_round_compression_messages()
func (rlc *RoundLevelCompressor) prepareRoundCompressionMessages(
	contextMessages []llm_schema.BaseMessage,
	targets []roundCompressTarget,
	mc iface.ModelContext,
	phaseName string,
	targetTokens int,
	aggressive bool,
	keepRecentMessages int,
	systemMessages []llm_schema.BaseMessage,
	tools []schema.ToolInfoInterface,
) []llm_schema.BaseMessage {
	systemPrompt := rlc.aggressivePrompt
	if !aggressive {
		systemPrompt = rlc.firstPrompt
	}

	promptText := rlc.buildCompressionUserPrompt(contextMessages, targets, mc, phaseName, targetTokens, keepRecentMessages, systemMessages, tools)

	if rlc.isUnderCompressionCallBudget(systemPrompt, promptText, mc) {
		return []llm_schema.BaseMessage{
			llm_schema.NewSystemMessage(systemPrompt),
			llm_schema.NewUserMessage(promptText),
		}
	}

	compactPrompt := rlc.truncatePromptToBudget(systemPrompt, promptText, mc)
	if compactPrompt == nil {
		return nil
	}
	return []llm_schema.BaseMessage{
		llm_schema.NewSystemMessage(systemPrompt),
		llm_schema.NewUserMessage(*compactPrompt),
	}
}

// buildCompressionUserPrompt 构建压缩用户提示词。
//
// 对应 Python: RoundLevelCompressor._build_compression_user_prompt()
func (rlc *RoundLevelCompressor) buildCompressionUserPrompt(
	contextMessages []llm_schema.BaseMessage,
	targets []roundCompressTarget,
	mc iface.ModelContext,
	phaseName string,
	targetTokens int,
	keepRecentMessages int,
	systemMessages []llm_schema.BaseMessage,
	tools []schema.ToolInfoInterface,
) string {
	// 收集目标索引集合
	targetIndices := make(map[int]bool)
	for _, t := range targets {
		for idx := t.startIdx; idx <= t.endIdx; idx++ {
			targetIndices[idx] = true
		}
	}

	firstTargetIdx := targets[0].startIdx
	lastTargetIdx := targets[0].endIdx
	for _, t := range targets[1:] {
		if t.startIdx < firstTargetIdx {
			firstTargetIdx = t.startIdx
		}
		if t.endIdx > lastTargetIdx {
			lastTargetIdx = t.endIdx
		}
	}

	protectedRecentStart := len(contextMessages) - keepRecentMessages
	if protectedRecentStart < lastTargetIdx+1 {
		protectedRecentStart = lastTargetIdx + 1
	}

	// 参考行数
	var referenceLines []string
	for idx, msg := range contextMessages {
		if !targetIndices[idx] && idx < protectedRecentStart {
			referenceLines = append(referenceLines, rlc.serializeMessage(idx, msg))
		}
	}

	// 目标行数
	var targetLines []string
	for _, t := range targets {
		targetLines = append(targetLines,
			fmt.Sprintf("[Block: %s]", t.blockID),
			fmt.Sprintf("- scope: %s", t.scope),
			fmt.Sprintf("- replace_range: [%d, %d]", t.startIdx, t.endIdx),
			fmt.Sprintf("- current_level: l%d", t.currentLevel),
			fmt.Sprintf("- next_level: l%d", t.nextLevel),
			fmt.Sprintf("- source_block_count: %d", t.sourceBlockCount),
		)
		for offset, msg := range t.messages {
			targetLines = append(targetLines, rlc.serializeMessage(t.startIdx+offset, msg))
		}
		targetLines = append(targetLines, "")
	}

	// 近期行数
	var recentLines []string
	for idx := protectedRecentStart; idx < len(contextMessages); idx++ {
		recentLines = append(recentLines, rlc.serializeMessage(idx, contextMessages[idx]))
	}

	currentWindowTokens := rlc.countContextWindowTokens(systemMessages, contextMessages, tools, mc)

	referenceText := "(none)"
	if len(referenceLines) > 0 {
		referenceText = strings.Join(referenceLines, "\n")
	}
	targetText := "(none)"
	if len(targetLines) > 0 {
		targetText = strings.TrimRight(strings.Join(targetLines, "\n"), "\n")
	}
	recentText := "(none)"
	if len(recentLines) > 0 {
		recentText = strings.Join(recentLines, "\n")
	}

	return strings.Join([]string{
		"[Compression Task]",
		fmt.Sprintf("- phase: %s", phaseName),
		fmt.Sprintf("- target_summary_tokens: %d", targetTokens),
		fmt.Sprintf("- keep_recent_messages: %d", keepRecentMessages),
		fmt.Sprintf("- selected_blocks: %d", len(targets)),
		fmt.Sprintf("- current_context_window_tokens: %d", currentWindowTokens),
		fmt.Sprintf("- compression_call_budget_limit: %d", rlc.compressionCallMaxTokens),
		fmt.Sprintf("- selected_range: [%d, %d]", firstTargetIdx, lastTargetIdx),
		"",
		"[Reference Context]",
		referenceText,
		"",
		"[Selected Targets]",
		targetText,
		"",
		"[Protected Recent Context]",
		recentText,
		"",
		"[Output Contract]",
		"- Return valid JSON only.",
		"- Use schema: {\"blocks\": [{\"block_id\": \"...\", \"summary\": \"...\"}]}",
		"- Emit exactly one summary for each selected block_id.",
		"- Do not emit undeclared block_ids.",
		"- Target content must appear only in [Selected Targets], not elsewhere.",
		"- Preserve the user's original requirements, constraints, acceptance criteria, and preferences as completely as possible.",
		"- Do not weaken or over-compress the user's original request unless absolutely necessary.",
		"- If a selected block is ongoing_react, include a distinct `User Requirements` section tied to the unfinished work.",
		"- If a selected block is completed_react, explicitly preserve both `User Requirements` and `Final Result` when they exist.",
	}, "\n")
}

// truncatePromptToBudget 二分截断提示词至压缩调用预算内。
//
// 对应 Python: RoundLevelCompressor._truncate_prompt_to_budget()
func (rlc *RoundLevelCompressor) truncatePromptToBudget(systemPrompt string, promptText string, mc iface.ModelContext) *string {
	minimumPrompt := "[Compression Task]\n...[TRUNCATED]...\n[Output Contract]\nReturn valid JSON only."
	if !rlc.isUnderCompressionCallBudget(systemPrompt, minimumPrompt, mc) {
		return nil
	}

	low, high := 0, len(promptText)
	best := minimumPrompt
	for low <= high {
		middle := (low + high) / 2
		candidate := rlc.buildHeadTailTruncatedText(promptText, middle)
		if rlc.isUnderCompressionCallBudget(systemPrompt, candidate, mc) {
			best = candidate
			low = middle + 1
		} else {
			high = middle - 1
		}
	}
	return &best
}

// buildRawTargets 构建 L0 原始压缩目标列表。
//
// 对应 Python: RoundLevelCompressor._build_raw_targets()
func (rlc *RoundLevelCompressor) buildRawTargets(messages []llm_schema.BaseMessage, compressEnd int) []roundCompressTarget {
	var targets []roundCompressTarget
	blockNo := 1
	cursor := 0

	for cursor <= compressEnd {
		if rlc.isRoundLevelFallbackBlock(messages[cursor]) {
			cursor = rlc.findRoundLevelBlockEnd(messages, cursor, compressEnd) + 1
			continue
		}

		startIdx := cursor
		endIdx, scope := rlc.findL0BlockEnd(messages, startIdx, compressEnd)
		if endIdx < startIdx {
			cursor++
			continue
		}

		protectedEndIdx := rlc.protectToolCallBoundary(messages, startIdx, endIdx)
		if protectedEndIdx != endIdx && protectedEndIdx <= startIdx {
			break
		}
		endIdx = protectedEndIdx

		blockMessages := make([]llm_schema.BaseMessage, endIdx-startIdx+1)
		copy(blockMessages, messages[startIdx:endIdx+1])

		targets = append(targets, roundCompressTarget{
			blockID:          fmt.Sprintf("block_%d", blockNo),
			scope:            scope,
			startIdx:         startIdx,
			endIdx:           endIdx,
			messages:         blockMessages,
			currentLevel:     0,
			nextLevel:        1,
			sourceBlockCount: 1,
		})
		blockNo++
		cursor = endIdx + 1
	}
	return targets
}

// buildRecursiveMergeTargets 构建递归合并目标列表。
//
// 对应 Python: RoundLevelCompressor._build_recursive_merge_targets()
func (rlc *RoundLevelCompressor) buildRecursiveMergeTargets(messages []llm_schema.BaseMessage, compressEnd int) []roundCompressTarget {
	memoryTargets := rlc.collectRoundLevelMemoryTargets(messages, compressEnd)
	if len(memoryTargets) < 2 {
		return nil
	}

	targetByID := make(map[string]*roundCompressTarget)
	for i := range memoryTargets {
		targetByID[memoryTargets[i].blockID] = &memoryTargets[i]
	}

	effectiveLevels, candidateLevel := rlc.resolveEffectiveMergeLevels(memoryTargets)
	if candidateLevel == nil {
		return nil
	}

	var selectedTargets []roundCompressTarget
	for _, t := range memoryTargets {
		if effectiveLevels[t.blockID] == *candidateLevel {
			selectedTargets = append(selectedTargets, t)
		}
	}
	sort.Slice(selectedTargets, func(i, j int) bool {
		return selectedTargets[i].startIdx < selectedTargets[j].startIdx
	})

	var mergedTargets []roundCompressTarget
	var group []roundCompressTarget

	for _, t := range selectedTargets {
		if len(group) == 0 {
			group = []roundCompressTarget{t}
			continue
		}
		if t.startIdx == group[len(group)-1].endIdx+1 {
			group = append(group, t)
			continue
		}
		if len(group) >= 2 {
			mergedTargets = append(mergedTargets, rlc.buildMergeTarget(group, messages, *candidateLevel, len(mergedTargets)+1))
		}
		group = []roundCompressTarget{t}
	}

	if len(group) >= 2 {
		mergedTargets = append(mergedTargets, rlc.buildMergeTarget(group, messages, *candidateLevel, len(mergedTargets)+1))
	}
	return mergedTargets
}

// resolveEffectiveMergeLevels 解析有效合并级别。
//
// 对应 Python: RoundLevelCompressor._resolve_effective_merge_levels()
func (rlc *RoundLevelCompressor) resolveEffectiveMergeLevels(memoryTargets []roundCompressTarget) (map[string]int, *int) {
	effectiveLevels := make(map[string]int)
	for _, t := range memoryTargets {
		level := t.currentLevel
		if level < 1 {
			level = 1
		}
		effectiveLevels[t.blockID] = level
	}

	for {
		levelCounts := make(map[int]int)
		for _, level := range effectiveLevels {
			levelCounts[level]++
		}

		var orderedLevels []int
		for level := range levelCounts {
			orderedLevels = append(orderedLevels, level)
		}
		sort.Ints(orderedLevels)

		if len(orderedLevels) == 0 {
			return effectiveLevels, nil
		}

		highestLevel := orderedLevels[len(orderedLevels)-1]
		changed := false

		for _, level := range orderedLevels {
			if level == highestLevel || levelCounts[level] != 1 {
				continue
			}
			// 找到下一个更高级别
			var nextHigherLevel int
			for _, candidate := range orderedLevels {
				if candidate > level {
					nextHigherLevel = candidate
					break
				}
			}
			if nextHigherLevel == 0 {
				continue
			}
			// 找到该级别对应的唯一 blockID
			var blockID string
			for bid, lvl := range effectiveLevels {
				if lvl == level {
					blockID = bid
					break
				}
			}
			if blockID != "" {
				effectiveLevels[blockID] = nextHigherLevel
				changed = true
				break
			}
		}
		if changed {
			continue
		}

		// 找到第一个 count >= 2 的级别
		var candidateLevel *int
		for _, level := range orderedLevels {
			if levelCounts[level] >= 2 {
				cl := level
				candidateLevel = &cl
				break
			}
		}
		return effectiveLevels, candidateLevel
	}
}

// buildMergeTarget 构建合并目标。
//
// 对应 Python: RoundLevelCompressor._build_merge_target()
func (rlc *RoundLevelCompressor) buildMergeTarget(group []roundCompressTarget, messages []llm_schema.BaseMessage, candidateLevel int, groupNo int) roundCompressTarget {
	return roundCompressTarget{
		blockID:          fmt.Sprintf("merge_%d_%d", candidateLevel, groupNo),
		scope:            "recursive_merge",
		startIdx:         group[0].startIdx,
		endIdx:           group[len(group)-1].endIdx,
		messages:         messages[group[0].startIdx : group[len(group)-1].endIdx+1],
		currentLevel:     candidateLevel,
		nextLevel:        candidateLevel + 1,
		sourceBlockCount: len(group),
	}
}

// collectRoundLevelMemoryTargets 收集已有轮级记忆块目标。
//
// 对应 Python: RoundLevelCompressor._collect_round_level_memory_targets()
func (rlc *RoundLevelCompressor) collectRoundLevelMemoryTargets(messages []llm_schema.BaseMessage, compressEnd int) []roundCompressTarget {
	var targets []roundCompressTarget
	blockNo := 1
	idx := 0

	for idx <= compressEnd {
		if !rlc.isRoundLevelFallbackBlock(messages[idx]) {
			idx++
			continue
		}
		endIdx := rlc.findRoundLevelBlockEnd(messages, idx, compressEnd)

		// 计算块内最大压缩级别
		maxLevel := 1
		for _, msg := range messages[idx : endIdx+1] {
			level := rlc.getCompressLevel(msg)
			if level > maxLevel {
				maxLevel = level
			}
		}

		blockMessages := make([]llm_schema.BaseMessage, endIdx-idx+1)
		copy(blockMessages, messages[idx:endIdx+1])

		targets = append(targets, roundCompressTarget{
			blockID:          fmt.Sprintf("memory_%d", blockNo),
			scope:            "existing_round_level_block",
			startIdx:         idx,
			endIdx:           endIdx,
			messages:         blockMessages,
			currentLevel:     maxLevel,
			nextLevel:        maxLevel + 1,
			sourceBlockCount: 1,
		})
		blockNo++
		idx = endIdx + 1
	}
	return targets
}

// buildAggressiveTargets 构建激进压缩目标。
//
// 对应 Python: RoundLevelCompressor._build_aggressive_targets()
func (rlc *RoundLevelCompressor) buildAggressiveTargets(messages []llm_schema.BaseMessage, compressEnd int) []roundCompressTarget {
	rawTargets := rlc.buildRawTargets(messages, compressEnd)
	if len(rawTargets) > 0 {
		return rawTargets
	}
	return rlc.collectRoundLevelMemoryTargets(messages, compressEnd)
}

// protectToolCallBoundary 保护工具调用边界，避免截断 AssistantMessage 的 tool_calls
// 与后续 ToolMessage 的配对关系。
//
// 对应 Python: RoundLevelCompressor._protect_tool_call_boundary()
func (rlc *RoundLevelCompressor) protectToolCallBoundary(messages []llm_schema.BaseMessage, startIdx int, endIdx int) int {
	if endIdx < startIdx {
		return endIdx
	}

	protectedEndIdx := endIdx

	// 收集 endIdx 之后所有 ToolMessage 的 tool_call_id
	tailToolIDs := make(map[string]bool)
	for _, msg := range messages[endIdx+1:] {
		if tm, ok := msg.(*llm_schema.ToolMessage); ok && tm.ToolCallID != "" {
			tailToolIDs[tm.ToolCallID] = true
		}
	}

	if len(tailToolIDs) == 0 {
		// 没有后续 ToolMessage，但 endIdx 处的 AssistantMessage 有 tool_calls → 向前缩进
		if am, ok := messages[endIdx].(*llm_schema.AssistantMessage); ok && len(am.ToolCalls) > 0 {
			return endIdx - 1
		}
		return endIdx
	}

	// 检查范围内 AssistantMessage 的 tool_call_id 是否与后续 ToolMessage 匹配
	for idx := startIdx; idx <= endIdx; idx++ {
		am, ok := messages[idx].(*llm_schema.AssistantMessage)
		if !ok || len(am.ToolCalls) == 0 {
			continue
		}
		for _, tc := range am.ToolCalls {
			if tailToolIDs[tc.ID] {
				if idx-1 < protectedEndIdx {
					protectedEndIdx = idx - 1
				}
				break
			}
		}
	}

	// 如果 protectedEndIdx 未变化但 endIdx 处有 tool_calls → 向前缩进
	if protectedEndIdx == endIdx {
		if am, ok := messages[endIdx].(*llm_schema.AssistantMessage); ok && len(am.ToolCalls) > 0 {
			protectedEndIdx = endIdx - 1
		}
	}
	return protectedEndIdx
}

// findL0BlockEnd 查找 L0 原始块的结束索引和范围类型。
//
// 对应 Python: RoundLevelCompressor._find_l0_block_end()
func (rlc *RoundLevelCompressor) findL0BlockEnd(messages []llm_schema.BaseMessage, startIdx int, compressEnd int) (int, string) {
	lastNonRoundLevelIdx := startIdx - 1
	for idx := startIdx; idx <= compressEnd; idx++ {
		if rlc.isRoundLevelFallbackBlock(messages[idx]) {
			break
		}
		lastNonRoundLevelIdx = idx
		if am, ok := messages[idx].(*llm_schema.AssistantMessage); ok && len(am.ToolCalls) == 0 {
			return idx, "completed_react"
		}
	}
	return lastNonRoundLevelIdx, "ongoing_react"
}

// isRoundLevelFallbackBlock 判断消息是否为轮级记忆块。
//
// 对应 Python: RoundLevelCompressor._is_round_level_fallback_block()
func (rlc *RoundLevelCompressor) isRoundLevelFallbackBlock(msg llm_schema.BaseMessage) bool {
	if _, ok := msg.(*llm_schema.UserMessage); !ok {
		return false
	}
	return strings.HasPrefix(msg.GetContent().Text(), rlc.compressionMarker)
}

// findRoundLevelBlockEnd 查找轮级记忆块结束索引。
//
// 对应 Python: RoundLevelCompressor._find_round_level_block_end()
func (rlc *RoundLevelCompressor) findRoundLevelBlockEnd(messages []llm_schema.BaseMessage, start int, compressEnd int) int {
	endIdx := start
	for endIdx+1 <= compressEnd {
		am, ok := messages[endIdx+1].(*llm_schema.AssistantMessage)
		if !ok || len(am.ToolCalls) > 0 || !rlc.looksLikeAck(am) {
			break
		}
		endIdx++
	}
	return endIdx
}

// looksLikeAck 判断 AssistantMessage 是否为确认回复。
//
// 对应 Python: RoundLevelCompressor._looks_like_ack()
func (rlc *RoundLevelCompressor) looksLikeAck(msg *llm_schema.AssistantMessage) bool {
	trimmed := strings.TrimSpace(msg.GetContent().Text())
	return trimmed == "Understood. I have recorded this compressed context."
}

// getCompressLevel 获取消息的压缩级别。
//
// 对应 Python: RoundLevelCompressor._get_compress_level()
func (rlc *RoundLevelCompressor) getCompressLevel(msg llm_schema.BaseMessage) int {
	metadata := msg.GetMetadata()
	if metadata != nil {
		if level, ok := metadata[compressLevelKey]; ok {
			switch v := level.(type) {
			case int:
				return v
			case float64:
				return int(v)
			case int64:
				return int(v)
			}
		}
	}
	if rlc.isRoundLevelFallbackBlock(msg) {
		return 1
	}
	return 0
}

// buildJSONReplacements 从 LLM 返回的 ParserContent 解析 JSON 构建替换列表。
//
// 对应 Python: RoundLevelCompressor._build_json_replacements()
func (rlc *RoundLevelCompressor) buildJSONReplacements(targets []roundCompressTarget, parserContent any, mc iface.ModelContext) []processor.Replacement {
	if !isValidBlocksPayload(parserContent) {
		return nil
	}

	parserMap, ok := parserContent.(map[string]any)
	if !ok {
		return nil
	}
	blocks, ok := parserMap["blocks"].([]any)
	if !ok {
		return nil
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
	for _, t := range targets {
		summary, ok := blockMap[t.blockID]
		if !ok || summary == "" {
			continue
		}
		replacementMessage := rlc.buildMemoryMessage(summary, t.scope, t.nextLevel)
		replacementMessages := []llm_schema.BaseMessage{replacementMessage}
		if !rlc.hasCompressionBenefit(t.messages, replacementMessages, mc) {
			continue
		}
		replacements = append(replacements, processor.Replacement{
			StartIdx: t.startIdx,
			EndIdx:   t.endIdx,
			Messages: replacementMessages,
		})
	}
	return replacements
}

// buildRawFallbackReplacement 构建 JSON 解析失败时的降级替换。
//
// 对应 Python: RoundLevelCompressor._build_raw_fallback_replacement()
func (rlc *RoundLevelCompressor) buildRawFallbackReplacement(targets []roundCompressTarget, summary string, mc iface.ModelContext) *processor.Replacement {
	if len(targets) == 0 || summary == "" {
		return nil
	}

	startIdx := targets[0].startIdx
	endIdx := targets[0].endIdx
	var originalMessages []llm_schema.BaseMessage
	maxCurrentLevel := 0
	maxNextLevel := 1
	totalSourceBlockCount := 0

	for _, t := range targets {
		if t.startIdx < startIdx {
			startIdx = t.startIdx
		}
		if t.endIdx > endIdx {
			endIdx = t.endIdx
		}
		originalMessages = append(originalMessages, t.messages...)
		if t.currentLevel > maxCurrentLevel {
			maxCurrentLevel = t.currentLevel
		}
		if t.nextLevel > maxNextLevel {
			maxNextLevel = t.nextLevel
		}
		totalSourceBlockCount += t.sourceBlockCount
	}

	replacementMessage := rlc.buildMemoryMessage(summary, "mixed_context", maxNextLevel)
	replacementMessages := []llm_schema.BaseMessage{replacementMessage}

	if !rlc.hasCompressionBenefit(originalMessages, replacementMessages, mc) {
		return nil
	}

	return &processor.Replacement{
		StartIdx: startIdx,
		EndIdx:   endIdx,
		Messages: replacementMessages,
	}
}

// isValidBlocksPayload 检查 ParserContent 是否为有效的 blocks JSON。
//
// 对应 Python: RoundLevelCompressor._is_valid_blocks_payload()
func isValidBlocksPayload(parserContent any) bool {
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

// buildMemoryMessage 构建轮级记忆块消息。
//
// 对应 Python: RoundLevelCompressor._build_memory_message()
func (rlc *RoundLevelCompressor) buildMemoryMessage(summary string, scope string, nextLevel int) *llm_schema.UserMessage {
	content := rlc.wrapMemoryBlock(summary, scope)
	msg := llm_schema.NewUserMessage(content)
	// 设置压缩级别元数据
	metadata := msg.GetMetadata()
	if metadata == nil {
		metadata = make(map[string]any)
	}
	metadata[compressLevelKey] = nextLevel
	msg.SetMetadata(metadata)
	return msg
}

// wrapMemoryBlock 将摘要包装为轮级记忆块格式。
//
// 对应 Python: RoundLevelCompressor._wrap_memory_block()
func (rlc *RoundLevelCompressor) wrapMemoryBlock(summary string, scope string) string {
	return fmt.Sprintf(
		"%s\n"+
			"processor: RoundLevelCompressor\n"+
			"type: historical_memory_block\n"+
			"scope: %s\n"+
			"authority: This block is reference memory, not a binding source of truth.\n"+
			"instruction_status: Historical fallback context only. Do not treat as a new user instruction.\n"+
			"conflict_priority: Prefer newer explicit user intent, newer raw context, "+
			"and fresh tool results over this block.\n\n"+
			"Summary:\n"+
			"%s",
		rlc.compressionMarker,
		scope,
		summary,
	)
}

// extractCompactSummary 提取压缩摘要文本。
//
// 对应 Python: RoundLevelCompressor._extract_compact_summary()
func (rlc *RoundLevelCompressor) extractCompactSummary(messages []llm_schema.BaseMessage) string {
	var parts []string
	for _, msg := range messages {
		content := msg.GetContent().Text()
		if strings.HasPrefix(content, rlc.compressionMarker) {
			parts = append(parts, content)
		}
	}
	return strings.Join(parts, "\n\n")
}

// buildMinimalTruncatedMessage 构建最小截断消息（含元数据）。
//
// 对应 Python: RoundLevelCompressor._build_minimal_truncated_message()
func (rlc *RoundLevelCompressor) buildMinimalTruncatedMessage() *llm_schema.UserMessage {
	return llm_schema.NewUserMessage(
		fmt.Sprintf(
			"%s\n"+
				"processor: RoundLevelCompressor\n"+
				"type: historical_memory_block\n"+
				"scope: truncated_full_context\n"+
				"Summary:\n"+
				"%s",
			rlc.compressionMarker,
			rlc.truncatedMarker,
		),
	)
}

// buildCompactTruncatedMessage 构建紧凑截断消息（仅标记）。
//
// 对应 Python: RoundLevelCompressor._build_compact_truncated_message()
func (rlc *RoundLevelCompressor) buildCompactTruncatedMessage() *llm_schema.UserMessage {
	return llm_schema.NewUserMessage(
		fmt.Sprintf("%s\n%s", rlc.compressionMarker, rlc.truncatedMarker),
	)
}

// truncateToTarget 硬截断至目标 Token 数。
//
// 对应 Python: RoundLevelCompressor._truncate_to_target()
func (rlc *RoundLevelCompressor) truncateToTarget(
	contextMessages []llm_schema.BaseMessage,
	mc iface.ModelContext,
	systemMessages []llm_schema.BaseMessage,
	tools []schema.ToolInfoInterface,
) []llm_schema.BaseMessage {
	fixedTokens := rlc.countContextWindowFixedTokens(systemMessages, tools, mc)
	allowedContextTokens := rlc.targetTotalTokens - fixedTokens

	if allowedContextTokens <= 0 {
		return []llm_schema.BaseMessage{rlc.buildCompactTruncatedMessage()}
	}

	// 序列化所有上下文消息
	var serializedParts []string
	for idx, msg := range contextMessages {
		serializedParts = append(serializedParts, rlc.serializeMessage(idx, msg))
	}
	serialized := strings.Join(serializedParts, "\n")
	if serialized == "" {
		return contextMessages
	}

	// 二分查找最大可保留字符数
	low, high := 0, len(serialized)
	var bestMessages []llm_schema.BaseMessage

	for low <= high {
		middle := (low + high) / 2
		candidateContent := rlc.wrapMemoryBlock(
			rlc.buildHeadTailTruncatedText(serialized, middle),
			"truncated_full_context",
		)
		candidateMessages := []llm_schema.BaseMessage{llm_schema.NewUserMessage(candidateContent)}
		candidateTokens := rlc.countContextWindowTokens(systemMessages, candidateMessages, tools, mc)
		if candidateTokens <= rlc.targetTotalTokens {
			bestMessages = candidateMessages
			low = middle + 1
		} else {
			high = middle - 1
		}
	}

	if len(bestMessages) > 0 {
		return bestMessages
	}

	minimalMessage := rlc.buildMinimalTruncatedMessage()
	minimalTokens := rlc.countContextWindowTokens(systemMessages, []llm_schema.BaseMessage{minimalMessage}, tools, mc)
	if minimalTokens <= rlc.targetTotalTokens {
		return []llm_schema.BaseMessage{minimalMessage}
	}

	return []llm_schema.BaseMessage{rlc.buildCompactTruncatedMessage()}
}

// buildHeadTailTruncatedText 构建头尾截断文本。
//
// 对应 Python: RoundLevelCompressor._build_head_tail_truncated_text()
func (rlc *RoundLevelCompressor) buildHeadTailTruncatedText(text string, keptChars int) string {
	if keptChars <= 0 {
		return rlc.truncatedMarker
	}

	headChars := int(float64(keptChars) * rlc.truncateHeadRatio)
	if headChars < 0 {
		headChars = 0
	}
	tailChars := keptChars - headChars
	if tailChars < 0 {
		tailChars = 0
	}

	head := ""
	if headChars > 0 && headChars <= len(text) {
		head = text[:headChars]
	} else if headChars > len(text) {
		head = text
	}

	tail := ""
	if tailChars > 0 && tailChars <= len(text) {
		tail = text[len(text)-tailChars:]
	} else if tailChars > len(text) {
		tail = text
	}

	if head != "" && tail != "" {
		return fmt.Sprintf("%s\n%s\n%s", head, rlc.truncatedMarker, tail)
	}
	if head != "" {
		return head
	}
	if tail != "" {
		return tail
	}
	return rlc.truncatedMarker
}

// countContextWindowTokens 计算全量上下文 Token 数。
//
// 对应 Python: RoundLevelCompressor._count_context_window_tokens()
func (rlc *RoundLevelCompressor) countContextWindowTokens(
	systemMessages []llm_schema.BaseMessage,
	contextMessages []llm_schema.BaseMessage,
	tools []schema.ToolInfoInterface,
	mc iface.ModelContext,
) int {
	tokenCounter := mc.TokenCounter()
	modelName := rlc.getModelName()

	allMessages := make([]llm_schema.BaseMessage, 0, len(systemMessages)+len(contextMessages))
	allMessages = append(allMessages, systemMessages...)
	allMessages = append(allMessages, contextMessages...)

	if tokenCounter != nil {
		count, msgErr := tokenCounter.CountMessages(allMessages, modelName)
		if msgErr == nil {
			toolCount, toolErr := tokenCounter.CountTools(tools, modelName)
			if toolErr == nil {
				return count + toolCount
			}
			// CountMessages 成功但 CountTools 失败，记录工具计数错误
			logger.Warn(logger.ComponentAgentCore).
				Str("processor_type", rlc.ProcessorType()).
				Err(toolErr).
				Msg("token_counter CountTools 返回错误，降级为字符估算")
		} else {
			// CountMessages 失败
			logger.Warn(logger.ComponentAgentCore).
				Str("processor_type", rlc.ProcessorType()).
				Err(msgErr).
				Msg("token_counter CountMessages 返回错误，降级为字符估算")
		}
	}

	total := 0
	for _, msg := range allMessages {
		total += processor.EstimateContentTokens(msg.GetContent().Text())
	}
	for _, tool := range tools {
		total += processor.EstimateContentTokens(rlc.serializeTool(tool))
	}
	return total
}

// countContextWindowFixedTokens 计算固定部分（system + tools）的 Token 数。
//
// 对应 Python: RoundLevelCompressor._count_context_window_fixed_tokens()
func (rlc *RoundLevelCompressor) countContextWindowFixedTokens(
	systemMessages []llm_schema.BaseMessage,
	tools []schema.ToolInfoInterface,
	mc iface.ModelContext,
) int {
	return rlc.countContextWindowTokens(systemMessages, nil, tools, mc)
}

// countCompressionCallTokens 计算压缩调用 Token 数。
//
// 对应 Python: RoundLevelCompressor._count_compression_call_tokens()
func (rlc *RoundLevelCompressor) countCompressionCallTokens(systemPrompt string, promptText string, mc iface.ModelContext) int {
	tokenCounter := mc.TokenCounter()
	modelName := rlc.getModelName()

	messages := []llm_schema.BaseMessage{
		llm_schema.NewSystemMessage(systemPrompt),
		llm_schema.NewUserMessage(promptText),
	}

	if tokenCounter != nil {
		count, err := tokenCounter.CountMessages(messages, modelName)
		if err == nil {
			return count
		}
		logger.Warn(logger.ComponentAgentCore).
			Str("processor_type", rlc.ProcessorType()).
			Err(err).
			Msg("compression token counting fallback")
	}

	total := 0
	for _, msg := range messages {
		total += processor.EstimateContentTokens(msg.GetContent().Text())
	}
	return total
}

// countMessageTokens 计算消息列表的 Token 数。
//
// 对应 Python: RoundLevelCompressor._count_message_tokens()
func (rlc *RoundLevelCompressor) countMessageTokens(messages []llm_schema.BaseMessage, mc iface.ModelContext) int {
	tokenCounter := mc.TokenCounter()
	modelName := rlc.getModelName()

	if tokenCounter != nil {
		count, err := tokenCounter.CountMessages(messages, modelName)
		if err == nil {
			return count
		}
		logger.Warn(logger.ComponentAgentCore).
			Str("processor_type", rlc.ProcessorType()).
			Err(err).
			Msg("token_counter 返回错误，降级为字符估算")
	}

	total := 0
	for _, msg := range messages {
		total += processor.EstimateContentTokens(msg.GetContent().Text())
	}
	return total
}

// isUnderContextWindowBudget 判断是否在上下文窗口预算内。
//
// 对应 Python: RoundLevelCompressor._is_under_context_window_budget()
func (rlc *RoundLevelCompressor) isUnderContextWindowBudget(
	systemMessages []llm_schema.BaseMessage,
	contextMessages []llm_schema.BaseMessage,
	tools []schema.ToolInfoInterface,
	mc iface.ModelContext,
) bool {
	totalTokens := rlc.countContextWindowTokens(systemMessages, contextMessages, tools, mc)
	return totalTokens <= rlc.targetTotalTokens
}

// isUnderCompressionCallBudget 判断是否在压缩调用预算内。
//
// 对应 Python: RoundLevelCompressor._is_under_compression_call_budget()
func (rlc *RoundLevelCompressor) isUnderCompressionCallBudget(systemPrompt string, promptText string, mc iface.ModelContext) bool {
	totalTokens := rlc.countCompressionCallTokens(systemPrompt, promptText, mc)
	return totalTokens <= rlc.compressionCallMaxTokens
}

// hasCompressionBenefit 判断压缩是否有收益（压缩后 Token 少于原始 Token）。
//
// 对应 Python: RoundLevelCompressor._has_compression_benefit()
func (rlc *RoundLevelCompressor) hasCompressionBenefit(originalMessages []llm_schema.BaseMessage, replacementMessages []llm_schema.BaseMessage, mc iface.ModelContext) bool {
	originalTokens := rlc.countMessageTokens(originalMessages, mc)
	replacementTokens := rlc.countMessageTokens(replacementMessages, mc)
	return originalTokens > replacementTokens
}

// estimateContentTokens 估算内容的 Token 数。
//
// 对应 Python: RoundLevelCompressor._estimate_content_tokens()
func estimateContentTokens(content any) int {
	return processor.EstimateContentTokens(content)
}

// serializeMessage 序列化单条消息为文本。
//
// 对应 Python: RoundLevelCompressor._serialize_message()
func (rlc *RoundLevelCompressor) serializeMessage(index int, msg llm_schema.BaseMessage) string {
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

	level := rlc.getCompressLevel(msg)
	if level > 0 {
		parts = append(parts, fmt.Sprintf("compress_level=l%d", level))
	}

	parts = append(parts, fmt.Sprintf("content=%s", msg.GetContent().Text()))
	return strings.Join(parts, " | ")
}

// serializeTool 序列化工具定义为 JSON 文本。
//
// 对应 Python: RoundLevelCompressor._serialize_tool()
func (rlc *RoundLevelCompressor) serializeTool(tool schema.ToolInfoInterface) string {
	data, err := json.Marshal(tool)
	if err != nil {
		return "{}"
	}
	return string(data)
}

// buildModifyIndices 构建修改索引列表。
func (rlc *RoundLevelCompressor) buildModifyIndices(startIdx int, endIdx int) []int {
	return buildRangeIndices(startIdx, endIdx)
}

// getModel 获取压缩模型实例（懒初始化）。
//
// 对应 Python: RoundLevelCompressor._get_model()
func (rlc *RoundLevelCompressor) getModel() *llm.Model {
	return rlc.model
}

// getModelName 获取模型名称。
func (rlc *RoundLevelCompressor) getModelName() string {
	if rlc.model != nil && rlc.model.ModelConfig != nil {
		return rlc.model.ModelConfig.ModelName
	}
	return ""
}

// messagesEqual 判断两个消息列表是否相等（深度比较消息内容）。
//
// 对应 Python: compressed_messages == all_messages（逐条比较消息内容）
func (rlc *RoundLevelCompressor) messagesEqual(a, b []llm_schema.BaseMessage) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].GetRole() != b[i].GetRole() {
			return false
		}
		if a[i].GetContent().Text() != b[i].GetContent().Text() {
			return false
		}
	}
	return true
}

// init 自动注册到 context_engine 注册表
func init() {
	context_engine.RegisterProcessorFactory("RoundLevelCompressor",
		func(config iface.ProcessorConfig) (iface.ContextProcessor, error) {
			cfg, ok := config.(*RoundLevelCompressorConfig)
			if !ok {
				return nil, fmt.Errorf("RoundLevelCompressor: 配置类型不匹配，期望 *RoundLevelCompressorConfig，实际 %T", config)
			}
			rlc, err := NewRoundLevelCompressor(cfg)
			if err != nil {
				return nil, err
			}
			return rlc, nil
		},
	)
}
