package compressor

import (
	prompts "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/processor/forked/compressor/prompts"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
)

// 常量：对话历史压缩器记忆块标记与默认提示词。
// Python: dialogue_compressor.py:19-25 (MEMORY_BLOCK_DIALOGUE_OPEN/CLOSE, DEFAULT_DIALOGUE_COMPRESSION_PROMPT)
const (
	// MemoryBlockDialogueOpen 对话历史记忆块开启标记
	MemoryBlockDialogueOpen = "<memory_block_dialogue>"
	// MemoryBlockDialogueClose 对话历史记忆块关闭标记
	MemoryBlockDialogueClose = "</memory_block_dialogue>"
)

// DefaultDialogueCompressionPrompt 默认对话压缩提示词。
// Python: dialogue_compressor.py:25 (DEFAULT_DIALOGUE_COMPRESSION_PROMPT = DIALOGUE_COMPACT_PROMPT)
const DefaultDialogueCompressionPrompt = prompts.DialogueCompactPrompt

// ──────────────────────────── 结构体 ────────────────────────────

// DialogueCompressorConfig 对话历史压缩器配置。
// Python: dialogue_compressor.py:28 (DialogueCompressorConfig)
type DialogueCompressorConfig struct {
	*PrefixCompactProcessorConfig
}

// NewDialogueCompressorConfig 创建配置，字段使用 Python 子类默认值。
// Python: trigger_context_ratio Field(default=0.8)；min_target 0.1 继承基类。
func NewDialogueCompressorConfig() *DialogueCompressorConfig {
	base := NewPrefixCompactProcessorConfig()
	base.TriggerContextRatio = 0.8
	return &DialogueCompressorConfig{PrefixCompactProcessorConfig: base}
}

// Validate 校验配置（委托基座约束）。
func (c *DialogueCompressorConfig) Validate() error { return c.PrefixCompactProcessorConfig.Validate() }

// GetBase 解包基座配置（前缀压缩基座 config() 消费）。
func (c *DialogueCompressorConfig) GetBase() *PrefixCompactProcessorConfig {
	return c.PrefixCompactProcessorConfig
}

// DialogueCompressor 对话历史压缩器（保留最后真实用户消息起的当前轮，压缩其前历史）。
// Python: dialogue_compressor.py:37 (DialogueCompressor)
type DialogueCompressor struct {
	*PrefixCompactProcessor
}

// NewDialogueCompressor 创建对话压缩器。
// Python: dialogue_compressor.py:41 (__init__ + 类属性覆盖)
func NewDialogueCompressor(config *DialogueCompressorConfig) *DialogueCompressor {
	c := &DialogueCompressor{
		PrefixCompactProcessor: NewPrefixCompactProcessor(config),
	}
	p := c.PrefixCompactProcessor
	p.MemoryBlockOpen = MemoryBlockDialogueOpen
	p.MemoryBlockClose = MemoryBlockDialogueClose
	p.DefaultPrompt = DefaultDialogueCompressionPrompt
	p.ProcessorLabel = "DialogueCompressor"
	p.MemoryBlockMeaning = "This is a compressed handoff of earlier conversation history. " +
		"It is not a new user request. " +
		"If any information here is relevant to the latest user request or later task execution, " +
		"treat it as prior context for the current response."
	p.MemoryBlockConflictPolicy = "Newer raw messages and the latest explicit user intent override this handoff."
	p.ReinjectBuilderNames = []string{"skills", "read_file", "plan_mode", "plan"}
	// Python: _build_span 虚方法覆盖 → 构造期注入（Go embedding 静态派发规避）
	p.buildSpanHook = c.buildSpan
	return c
}

// buildSpan 分段：最后一条真实用户消息起的当前轮受保护，其余历史压缩。
// Python: dialogue_compressor.py:63 (_build_span)
func (c *DialogueCompressor) buildSpan(messages []llm_schema.BaseMessage) PrefixCompactSpan {
	currentRoundStart := FindLastRealUserMessageIndex(messages)
	if currentRoundStart < 0 {
		return PrefixCompactSpan{ProtectedTail: append([]llm_schema.BaseMessage(nil), messages...)}
	}
	keepRecent := len(messages) - currentRoundStart
	effectiveKeep := AdjustKeepRecentForToolBoundaries(messages, keepRecent)
	splitIndex := max(len(messages)-effectiveKeep, 0)
	return PrefixCompactSpan{
		MessagesToCompress: append([]llm_schema.BaseMessage(nil), messages[:splitIndex]...),
		ProtectedTail:      append([]llm_schema.BaseMessage(nil), messages[splitIndex:]...),
	}
}

// ProcessorType 处理器类型标识（覆盖基座标签）。
// Python: processor_label = "DialogueCompressor"（元类注入，D-24.2-1）
func (c *DialogueCompressor) ProcessorType() string { return "DialogueCompressor" }

// Config 返回子类强类型配置（覆盖基座 any 形态）。
// Python: dialogue_compressor.py:52 (config property)
func (c *DialogueCompressor) Config() *DialogueCompressorConfig {
	if cfg, ok := c.ContextProcessor.Config().(*DialogueCompressorConfig); ok {
		return cfg
	}
	return NewDialogueCompressorConfig()
}

// SetModelDefaultsiface.ProcessorConfig 实现：Model/ModelClient 为 nil 时回填。
func (c *DialogueCompressorConfig) SetModelDefaults(model *llm_schema.ModelRequestConfig, modelClient *llm_schema.ModelClientConfig) {
	if c.Model == nil {
		c.Model = model
	}
	if c.ModelClient == nil {
		c.ModelClient = modelClient
	}
}
