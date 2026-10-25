package extractor

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ExtractOption Extract 的可选参数（对齐 Python **kwargs 收纳）。
type ExtractOption struct {
	// Extra 透传给实现的额外参数
	Extra map[string]any
}

// ──────────────────────────── 枚举 ────────────────────────────
// ──────────────────────────── 常量 ────────────────────────────
// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────
// ──────────────────────────── 非导出函数 ────────────────────────────

// Extractor 抽取器抽象接口，用于三元组等信息抽取。
//
// Python: openjiuwen/core/retrieval/indexing/processor/extractor/base.py (Extractor)
// 注意：不建 Processor 基接口（13.5/13.10 既定决策），Process 作为
// Processor 契约兼容入口直接挂在接口上（对齐 Chunker/Parser 风格）。
type Extractor interface {
	// Extract 从文本分块中抽取信息（如三元组）。
	// Python **kwargs 由 opts 收纳（当前无消费者传参，预留）。
	Extract(ctx context.Context, chunks []common.TextChunk, opts ...ExtractOption) ([]common.Triple, error)
	// Process 处理文本分块（Processor 契约兼容入口），默认转发 Extract。
	// 对齐 Python Extractor.process → self.extract(chunks, **kwargs)。
	Process(ctx context.Context, chunks []common.TextChunk, opts ...ExtractOption) ([]common.Triple, error)
}
