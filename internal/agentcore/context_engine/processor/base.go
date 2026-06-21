package processor

import (
	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BaseProcessor 上下文处理器基类，提供所有处理器的默认实现。
//
// 具体处理器嵌入此结构体，只需覆写感兴趣的钩子方法。
// 默认行为：Trigger* 返回 false（不触发），On* 透传输入，
// SaveState/LoadState 空操作。
//
// 对应 Python: openjiuwen/core/context_engine/processor/base.py (ContextProcessor)
type BaseProcessor struct {
	// config 处理器配置，各子类实现 iface.ProcessorConfig 接口
	config iface.ProcessorConfig
	// compressionUsage 压缩调用用量追踪
	compressionUsage map[string]any
}

// ──────────────────────────── 枚 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewBaseProcessor 创建处理器基类实例
func NewBaseProcessor(config iface.ProcessorConfig) *BaseProcessor {
	return &BaseProcessor{
		config: config,
	}
}

// Config 返回处理器配置（只读）
func (p *BaseProcessor) Config() iface.ProcessorConfig {
	return p.config
}

// ──────────────────────────── 非导出函数 ────────────────────────────
