package compressor

import (
	"fmt"

	context_engine "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine"
	iface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/interface"
)

// Activate 激活 forked 压缩器链：把同类型名工厂切换为 forked 实现。
// Python: openjiuwen/core/context_engine/processor/forked/__init__.py (forked.activate)。
// 注册表为 last-write-wins（registry.go:28），旧 base 压缩器工厂被本调用覆盖。
// 偏差 D-24.15-a5：Python activate 位于 forked 包顶层 __init__；Go 因
// forked→compressor→forked 包环不可行，Activate 落在 compressor 子包，
// rail 侧调用语义不变。
// Offloader 位（MessageSummaryOffloader）的 forked 版随 15b 回填（D-24.15-a1），
// 届时在此处追加注册；当前保持旧 offloader 工厂生效。
func Activate() {
	context_engine.RegisterProcessorFactory("SessionMemoryCompressor", func(config iface.ProcessorConfig) (iface.ContextProcessor, error) {
		cfg, ok := config.(*SessionMemoryCompressorConfig)
		if !ok {
			return nil, fmt.Errorf("SessionMemoryCompressor: 配置类型不匹配，期望 *SessionMemoryCompressorConfig，实际 %T", config)
		}
		return NewSessionMemoryCompressor(cfg), nil
	})
	context_engine.RegisterProcessorFactory("DialogueCompressor", func(config iface.ProcessorConfig) (iface.ContextProcessor, error) {
		cfg, ok := config.(*DialogueCompressorConfig)
		if !ok {
			return nil, fmt.Errorf("DialogueCompressor: 配置类型不匹配，期望 *DialogueCompressorConfig，实际 %T", config)
		}
		return NewDialogueCompressor(cfg), nil
	})
	context_engine.RegisterProcessorFactory("CurrentRoundCompressor", func(config iface.ProcessorConfig) (iface.ContextProcessor, error) {
		cfg, ok := config.(*CurrentRoundCompressorConfig)
		if !ok {
			return nil, fmt.Errorf("CurrentRoundCompressor: 配置类型不匹配，期望 *CurrentRoundCompressorConfig，实际 %T", config)
		}
		return NewCurrentRoundCompressor(cfg), nil
	})
	context_engine.RegisterProcessorFactory("RoundLevelCompressor", func(config iface.ProcessorConfig) (iface.ContextProcessor, error) {
		cfg, ok := config.(*RoundLevelCompressorConfig)
		if !ok {
			return nil, fmt.Errorf("RoundLevelCompressor: 配置类型不匹配，期望 *RoundLevelCompressorConfig，实际 %T", config)
		}
		return NewRoundLevelCompressor(cfg), nil
	})
}

// 编译期兜底：forked 压缩器满足 iface.ContextProcessor。
var (
	_ iface.ContextProcessor = (*PrefixCompactProcessor)(nil)
	_ iface.ContextProcessor = (*SessionMemoryCompressor)(nil)
)
