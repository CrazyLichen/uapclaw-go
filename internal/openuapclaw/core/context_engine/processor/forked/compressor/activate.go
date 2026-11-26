package compressor

import (
	"fmt"

	context_engine "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine"
	iface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/processor/forked/offloader"
)

// Activate 激活 forked 压缩器链：把同类型名工厂切换为 forked 实现。
// Python: openjiuwen/core/context_engine/processor/forked/__init__.py (forked.activate)。
// 注册表为 last-write-wins（registry.go:28），旧 base 压缩器工厂被本调用覆盖。
// 偏差 D-24.15-a5：Python activate 位于 forked 包顶层 __init__；Go 因
// forked→compressor→forked 包环不可行，Activate 落在 compressor 子包，
// rail 侧调用语义不变。
// Offloader 位（MessageSummaryOffloader）forked 工厂已随 T11 回填（D-24.15-a1 销账）。
func Activate() {
	if originalFactories == nil {
		originalFactories = map[string]iface.ProcessorFactory{}
	}
	for _, typeName := range []string{
		"SessionMemoryCompressor", "DialogueCompressor",
		"CurrentRoundCompressor", "RoundLevelCompressor",
	} {
		if _, ok := originalFactories[typeName]; !ok {
			if factory, found := context_engine.GetProcessorFactory(typeName); found {
				originalFactories[typeName] = factory
			}
		}
	}
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

func init() {
	// T11 回填：forked MessageSummaryOffloader 工厂（原占位旧版工厂被覆盖）。
	// D-24.15-a1 销账。Python: forked.activate() 注册 ForkedMessageSummaryOffloaderConfig。
	context_engine.RegisterProcessorFactory("MessageSummaryOffloader", func(config iface.ProcessorConfig) (iface.ContextProcessor, error) {
		cfg, ok := config.(*offloader.MessageSummaryOffloaderConfig)
		if !ok {
			return nil, fmt.Errorf("MessageSummaryOffloader: 配置类型不匹配，期望 *MessageSummaryOffloaderConfig，实际 %T", config)
		}
		return offloader.NewMessageSummaryOffloader(cfg), nil
	})
}

// ──────────────────────────── 全局变量 ────────────────────────────

// originalFactories Activate 前的工厂快照（Deactivate 还原用）。
// Python: forked/__init__.py:33 (_ORIGINAL_PROCESSORS)
var originalFactories map[string]iface.ProcessorFactory

// ──────────────────────────── 导出函数 ────────────────────────────

// Deactivate 还原 Activate 前的工厂注册（未 Activate 过为 no-op）。
// Python: forked/__init__.py:56 (deactivate)
func Deactivate() {
	for typeName, factory := range originalFactories {
		context_engine.RegisterProcessorFactory(typeName, factory)
	}
	originalFactories = nil
}
