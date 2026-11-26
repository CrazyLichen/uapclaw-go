package compressor

import (
	"fmt"

	prompts "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/processor/forked/compressor/prompts"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
)

// 常量：轮级压缩器记忆块标记与默认提示词。
// Python: round_level_compressor.py:19-25
const (
	// MemoryBlockRoundOpen 轮级记忆块开启标记
	MemoryBlockRoundOpen = "<memory_block_round>"
	// MemoryBlockRoundClose 轮级记忆块关闭标记
	MemoryBlockRoundClose = "</memory_block_round>"
)

// DefaultRoundCompressionPrompt 默认轮级压缩提示词。
// Python: round_level_compressor.py:25 (DEFAULT_ROUND_COMPRESSION_PROMPT = ROUND_COMPACT_PROMPT)
const DefaultRoundCompressionPrompt = prompts.RoundCompactPrompt

// ──────────────────────────── 结构体 ────────────────────────────

// RoundLevelCompressorConfig 轮级压缩器配置。
// Python: round_level_compressor.py:28 (RoundLevelCompressorConfig)
type RoundLevelCompressorConfig struct {
	*PrefixCompactProcessorConfig
	// KeepRecentMessages 跨轮保护的尾部消息条数（≥0）
	// Python: keep_recent_messages: int = Field(default=4, ge=0)
	KeepRecentMessages int `json:"keep_recent_messages"`
}

// NewRoundLevelCompressorConfig 创建配置，字段使用 Python 子类默认值。
func NewRoundLevelCompressorConfig() *RoundLevelCompressorConfig {
	base := NewPrefixCompactProcessorConfig()
	base.TriggerContextRatio = 0.8
	// Python: min_target_context_ratio 继承基类默认 0.1；keep_recent_messages 默认 4
	return &RoundLevelCompressorConfig{PrefixCompactProcessorConfig: base, KeepRecentMessages: 4}
}

// Validate 校验配置（基座约束 + keep_recent_messages ≥ 0）。
func (c *RoundLevelCompressorConfig) Validate() error {
	if c.KeepRecentMessages < 0 {
		return fmt.Errorf("keep_recent_messages 不能为负数，当前值: %d", c.KeepRecentMessages)
	}
	return c.PrefixCompactProcessorConfig.Validate()
}

// GetBase 解包基座配置。
func (c *RoundLevelCompressorConfig) GetBase() *PrefixCompactProcessorConfig {
	return c.PrefixCompactProcessorConfig
}

// RoundLevelCompressor 轮级压缩器（跨轮任务连续性检查点）。
// Python: round_level_compressor.py:38 (RoundLevelCompressor)
type RoundLevelCompressor struct {
	*PrefixCompactProcessor
}

// NewRoundLevelCompressor 创建轮级压缩器。
// Python: round_level_compressor.py:42 (__init__ + 类属性覆盖)
func NewRoundLevelCompressor(config *RoundLevelCompressorConfig) *RoundLevelCompressor {
	c := &RoundLevelCompressor{
		PrefixCompactProcessor: NewPrefixCompactProcessor(config),
	}
	p := c.PrefixCompactProcessor
	p.MemoryBlockOpen = MemoryBlockRoundOpen
	p.MemoryBlockClose = MemoryBlockRoundClose
	p.DefaultPrompt = DefaultRoundCompressionPrompt
	p.ProcessorLabel = "RoundLevelCompressor"
	p.MemoryBlockMeaning = "This is a task-continuity checkpoint across multiple conversation rounds. " +
		"It may include long-running project state, prior decisions, current goals, completed work, " +
		"unfinished work, and ongoing task clues. " +
		"It can help continue unfinished work, but it is not a new user request."
	p.MemoryBlockConflictPolicy = "Newer raw messages, fresh tool results, and the latest explicit user intent override this checkpoint."
	p.ReinjectBuilderNames = []string{"plan_mode", "plan", "task_status", "todo", "skills", "read_file"}
	// Python: _build_span 虚方法覆盖 → 构造期注入
	p.buildSpanHook = c.buildSpan
	return c
}

// buildSpan 分段：keep_recent 尾部保护，其余全部压缩。
// Python: round_level_compressor.py:59 (_build_span)
func (c *RoundLevelCompressor) buildSpan(messages []llm_schema.BaseMessage) PrefixCompactSpan {
	effectiveKeep := AdjustKeepRecentForToolBoundaries(messages, c.Config().KeepRecentMessages)
	splitIndex := max(len(messages)-effectiveKeep, 0)
	return PrefixCompactSpan{
		MessagesToCompress: append([]llm_schema.BaseMessage(nil), messages[:splitIndex]...),
		ProtectedTail:      append([]llm_schema.BaseMessage(nil), messages[splitIndex:]...),
	}
}

// ProcessorType 处理器类型标识。
// Python: processor_label = "RoundLevelCompressor"
func (c *RoundLevelCompressor) ProcessorType() string { return "RoundLevelCompressor" }

// Config 返回子类强类型配置。
func (c *RoundLevelCompressor) Config() *RoundLevelCompressorConfig {
	if cfg, ok := c.ContextProcessor.Config().(*RoundLevelCompressorConfig); ok {
		return cfg
	}
	return NewRoundLevelCompressorConfig()
}
