package compressor

import (
	"testing"

	forked "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/processor/forked"
	cesupport "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/processor/forked/compressor/support"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
)

// 编译期锚：三个 forked 压缩器 config 必须实现 forked.ModelBindingConfig（D-24.2-7，
// 用户裁决改窄接口；24.2 挂账③销账锚点）。
var (
	_ forked.ModelBindingConfig = (*DialogueCompressorConfig)(nil)
	_ forked.ModelBindingConfig = (*CurrentRoundCompressorConfig)(nil)
	_ forked.ModelBindingConfig = (*RoundLevelCompressorConfig)(nil)
)

// spanCase 构造 [user, assistant, user, assistant, user(real), assistant] 的典型序列
func spanCase() []llm_schema.BaseMessage {
	return []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("old-1"),
		llm_schema.NewAssistantMessage("old-answer"),
		llm_schema.NewUserMessage("old-2"),
		llm_schema.NewAssistantMessage("old-answer-2"),
		llm_schema.NewUserMessage("latest"),
		llm_schema.NewAssistantMessage("working"),
	}
}

// TestDialogueCompressor_Span 当前轮起保护、此前历史压缩。
// Python: dialogue_compressor.py:63。
func TestDialogueCompressor_Span(t *testing.T) {
	c := NewDialogueCompressor(NewDialogueCompressorConfig())
	messages := spanCase()
	span := c.buildSpan(messages)
	// 最后真实用户消息下标 4 → keep=2（4,5 无工具边界外扩）→ split=4
	if len(span.MessagesToCompress) != 4 {
		t.Errorf("应压缩前 4 条历史, got %d", len(span.MessagesToCompress))
	}
	if len(span.ProtectedTail) != 2 {
		t.Errorf("应保护最后 2 条, got %d", len(span.ProtectedTail))
	}
	if len(span.PreservedPrefix) != 0 {
		t.Errorf("dialogue 无保留前缀, got %d", len(span.PreservedPrefix))
	}
	// 无真实用户消息 → 全部保护
	internal := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage(cesupport.InternalUserPrefixes[0] + "injected"),
		llm_schema.NewAssistantMessage("a"),
	}
	span = c.buildSpan(internal)
	if len(span.ProtectedTail) != 2 || len(span.MessagesToCompress) != 0 {
		t.Errorf("无真实用户消息应全部保护, compress=%d tail=%d", len(span.MessagesToCompress), len(span.ProtectedTail))
	}
}

// TestCurrentRoundCompressor_Span 前缀到最后真实用户消息、keep_recent 保护尾部。
// Python: current_round_compressor.py:67。
func TestCurrentRoundCompressor_Span(t *testing.T) {
	cfg := NewCurrentRoundCompressorConfig()
	cfg.KeepRecentMessages = 2
	c := NewCurrentRoundCompressor(cfg)
	messages := spanCase()
	// keep=2 → split=4；lastUser=4 → targetStart=5 > split=4 → 全部前缀保护
	span := c.buildSpan(messages)
	if len(span.PreservedPrefix) != 5 || len(span.ProtectedTail) != 1 || len(span.MessagesToCompress) != 0 {
		t.Errorf("split<=targetStart 应前缀+尾巴全保护, pre=%d compress=%d tail=%d",
			len(span.PreservedPrefix), len(span.MessagesToCompress), len(span.ProtectedTail))
	}
	// keep_recent=0 → split=6 > targetStart=5 → 中段压缩
	cfg.KeepRecentMessages = 0
	span = c.buildSpan(messages)
	if len(span.PreservedPrefix) != 5 || len(span.MessagesToCompress) != 1 {
		t.Errorf("中段应压缩 1 条, pre=%d compress=%d", len(span.PreservedPrefix), len(span.MessagesToCompress))
	}
	// 无真实用户消息：keep=0 → split=len → PY messages[split:] 为空尾巴（base 语义）
	internal := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage(cesupport.InternalUserPrefixes[0] + "injected"),
		llm_schema.NewAssistantMessage("a"),
	}
	span = c.buildSpan(internal)
	if len(span.ProtectedTail) != 0 || len(span.PreservedPrefix) != 0 || len(span.MessagesToCompress) != 0 {
		t.Errorf("无真实用户消息且 keep=0 应空跨度（对齐 PY messages[split:]）, pre=%d compress=%d tail=%d",
			len(span.PreservedPrefix), len(span.MessagesToCompress), len(span.ProtectedTail))
	}
}

// TestRoundLevelCompressor_Span keep_recent 尾部保护、其余全压缩。
// Python: round_level_compressor.py:59。
func TestRoundLevelCompressor_Span(t *testing.T) {
	c := NewRoundLevelCompressor(NewRoundLevelCompressorConfig())
	messages := spanCase()
	// 默认 keep=4 → split=2
	span := c.buildSpan(messages)
	if len(span.MessagesToCompress) != 2 || len(span.ProtectedTail) != 4 || len(span.PreservedPrefix) != 0 {
		t.Errorf("默认 keep=4, compress=%d tail=%d pre=%d",
			len(span.MessagesToCompress), len(span.ProtectedTail), len(span.PreservedPrefix))
	}
}

// TestThreeCompressors_LabelsAndDefaults 子类字段覆盖与默认值。
// Python: 三文件类属性。
func TestThreeCompressors_LabelsAndDefaults(t *testing.T) {
	d := NewDialogueCompressor(NewDialogueCompressorConfig())
	if d.ProcessorType() != "DialogueCompressor" || d.MemoryBlockOpen != MemoryBlockDialogueOpen {
		t.Error("dialogue 标签/记忆块覆盖失效")
	}
	if d.Config().TriggerContextRatio != 0.8 {
		t.Error("dialogue trigger 默认 0.8")
	}
	cr := NewCurrentRoundCompressor(NewCurrentRoundCompressorConfig())
	if cr.ProcessorType() != "CurrentRoundCompressor" || cr.MemoryBlockOpen != MemoryBlockCurrentOpen {
		t.Error("current 标签/记忆块覆盖失效")
	}
	if cr.Config().KeepRecentMessages != 0 {
		t.Error("current keep_recent 默认 0")
	}
	rl := NewRoundLevelCompressor(NewRoundLevelCompressorConfig())
	if rl.ProcessorType() != "RoundLevelCompressor" || rl.MemoryBlockOpen != MemoryBlockRoundOpen {
		t.Error("round 标签/记忆块覆盖失效")
	}
	if rl.Config().KeepRecentMessages != 4 {
		t.Error("round keep_recent 默认 4")
	}
	if rl.Config().GetBase().MinTargetContextRatio != 0.1 {
		t.Error("round min_target 继承基类 0.1")
	}
}

// TestThreeCompressors_基座config解包 子类配置经 GetBase 被基座正确消费。
func TestThreeCompressors_基座config解包(t *testing.T) {
	for name, newFn := range map[string]func() *PrefixCompactProcessor{
		"dialogue": func() *PrefixCompactProcessor {
			return NewDialogueCompressor(NewDialogueCompressorConfig()).PrefixCompactProcessor
		},
	} {
		p := newFn()
		if p.config().TriggerContextRatio != 0.8 {
			t.Errorf("%s 基座应解包到子类基座配置(0.8), got %v", name, p.config().TriggerContextRatio)
		}
	}
}

// TestCurrentRoundCompressor_Validate负值拒绝 keep_recent_messages < 0 拒绝。
func TestCurrentRoundCompressor_Validate负值拒绝(t *testing.T) {
	cfg := NewCurrentRoundCompressorConfig()
	cfg.KeepRecentMessages = -1
	if err := cfg.Validate(); err == nil {
		t.Error("负值应被拒绝")
	}
	cfg.KeepRecentMessages = 0
	if err := cfg.Validate(); err != nil {
		t.Errorf("0 应合法: %v", err)
	}
	cfgR := NewRoundLevelCompressorConfig()
	cfgR.KeepRecentMessages = -2
	if err := cfgR.Validate(); err == nil {
		t.Error("round 负值应被拒绝")
	}
}
