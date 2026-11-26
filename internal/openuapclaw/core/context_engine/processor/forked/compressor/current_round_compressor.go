package compressor

import (
	"fmt"

	prompts "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/processor/forked/compressor/prompts"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
)

// 常量：当前轮压缩器记忆块标记与默认提示词。
// Python: current_round_compressor.py:19-25
const (
	// MemoryBlockCurrentOpen 当前轮记忆块开启标记
	MemoryBlockCurrentOpen = "<memory_block_current>"
	// MemoryBlockCurrentClose 当前轮记忆块关闭标记
	MemoryBlockCurrentClose = "</memory_block_current>"
)

// DefaultCurrentCompressionPrompt 默认当前轮压缩提示词。
// Python: current_round_compressor.py:25 (DEFAULT_CURRENT_COMPRESSION_PROMPT = CURRENT_COMPACT_PROMPT)
const DefaultCurrentCompressionPrompt = prompts.CurrentCompactPrompt

// ──────────────────────────── 结构体 ────────────────────────────

// CurrentRoundCompressorConfig 当前轮压缩器配置。
// Python: current_round_compressor.py:28 (CurrentRoundCompressorConfig)
type CurrentRoundCompressorConfig struct {
	*PrefixCompactProcessorConfig
	// KeepRecentMessages 当前轮保护的尾部消息条数（≥0）
	// Python: keep_recent_messages: int = Field(default=0, ge=0)
	KeepRecentMessages int `json:"keep_recent_messages"`
}

// NewCurrentRoundCompressorConfig 创建配置，字段使用 Python 子类默认值。
func NewCurrentRoundCompressorConfig() *CurrentRoundCompressorConfig {
	base := NewPrefixCompactProcessorConfig()
	base.TriggerContextRatio = 0.8
	return &CurrentRoundCompressorConfig{PrefixCompactProcessorConfig: base}
}

// Validate 校验配置（基座约束 + keep_recent_messages ≥ 0）。
func (c *CurrentRoundCompressorConfig) Validate() error {
	if c.KeepRecentMessages < 0 {
		return fmt.Errorf("keep_recent_messages 不能为负数，当前值: %d", c.KeepRecentMessages)
	}
	return c.PrefixCompactProcessorConfig.Validate()
}

// GetBase 解包基座配置。
func (c *CurrentRoundCompressorConfig) GetBase() *PrefixCompactProcessorConfig {
	return c.PrefixCompactProcessorConfig
}

// CurrentRoundCompressor 当前轮压缩器（压缩最新用户请求后的已执行工作）。
// Python: current_round_compressor.py:39 (CurrentRoundCompressor)
type CurrentRoundCompressor struct {
	*PrefixCompactProcessor
}

// NewCurrentRoundCompressor 创建当前轮压缩器。
// Python: current_round_compressor.py:43 (__init__ + 类属性覆盖)
func NewCurrentRoundCompressor(config *CurrentRoundCompressorConfig) *CurrentRoundCompressor {
	c := &CurrentRoundCompressor{
		PrefixCompactProcessor: NewPrefixCompactProcessor(config),
	}
	p := c.PrefixCompactProcessor
	p.MemoryBlockOpen = MemoryBlockCurrentOpen
	p.MemoryBlockClose = MemoryBlockCurrentClose
	p.DefaultPrompt = DefaultCurrentCompressionPrompt
	p.ProcessorLabel = "CurrentRoundCompressor"
	p.MemoryBlockMeaning = "This is a compressed summary of work already performed after the latest user request. " +
		"It is not a new user request. " +
		"Use it to recover completed analysis, tool calls, code changes, test results, " +
		"and next steps for the current task."
	p.MemoryBlockConflictPolicy = "Newer raw messages, fresh tool results, and the latest user instructions override this summary."
	p.ReinjectBuilderNames = []string{"plan_mode", "plan", "task_status", "todo", "skills", "read_file"}
	// Python: _build_span 虚方法覆盖 → 构造期注入
	p.buildSpanHook = c.buildSpan
	return c
}

// buildSpan 分段：keep_recent 尾部 + 最后真实用户消息前缀保护，其间压缩。
// Python: current_round_compressor.py:67 (_build_span)
func (c *CurrentRoundCompressor) buildSpan(messages []llm_schema.BaseMessage) PrefixCompactSpan {
	keepRecent := c.Config().KeepRecentMessages
	effectiveKeep := AdjustKeepRecentForToolBoundaries(messages, keepRecent)
	splitIndex := max(len(messages)-effectiveKeep, 0)
	lastUserIndex := FindLastRealUserMessageIndex(messages)
	if lastUserIndex < 0 {
		return PrefixCompactSpan{ProtectedTail: append([]llm_schema.BaseMessage(nil), messages[splitIndex:]...)}
	}
	targetStart := lastUserIndex + 1
	if splitIndex <= targetStart {
		return PrefixCompactSpan{
			PreservedPrefix: append([]llm_schema.BaseMessage(nil), messages[:targetStart]...),
			ProtectedTail:   append([]llm_schema.BaseMessage(nil), messages[targetStart:]...),
		}
	}
	return PrefixCompactSpan{
		PreservedPrefix:    append([]llm_schema.BaseMessage(nil), messages[:targetStart]...),
		MessagesToCompress: append([]llm_schema.BaseMessage(nil), messages[targetStart:splitIndex]...),
		ProtectedTail:      append([]llm_schema.BaseMessage(nil), messages[splitIndex:]...),
	}
}

// ProcessorType 处理器类型标识。
// Python: processor_label = "CurrentRoundCompressor"
func (c *CurrentRoundCompressor) ProcessorType() string { return "CurrentRoundCompressor" }

// Config 返回子类强类型配置。
func (c *CurrentRoundCompressor) Config() *CurrentRoundCompressorConfig {
	if cfg, ok := c.ContextProcessor.Config().(*CurrentRoundCompressorConfig); ok {
		return cfg
	}
	return NewCurrentRoundCompressorConfig()
}
