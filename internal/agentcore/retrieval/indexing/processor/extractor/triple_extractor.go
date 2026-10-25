package extractor

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TripleExtractor 基于 LLM 的 OpenIE 三元组抽取器。
//
// Python: openjiuwen/core/retrieval/indexing/processor/extractor/triple_extractor.py
type TripleExtractor struct {
	// llmClient LLM 客户端实例（Python: llm_client: Any → Go 具体类型，13.15 先例）
	llmClient *llm.Model
	// modelName 模型名（Python 死参数：存字段但 invoke 从不使用——偏差 §8-1）
	modelName string
	// temperature 温度参数（invoke 时透传）
	temperature float64
	// limiter 并发信号量（对齐 asyncio.Semaphore(max_concurrent)）
	limiter chan struct{}
}

// ──────────────────────────── 枚举 ────────────────────────────

// TripleExtractorOption TripleExtractor 构造选项函数。
type TripleExtractorOption func(*TripleExtractor)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译期断言 TripleExtractor 实现 Extractor（Task 6 实现 Extract/Process 后恢复）
// var _ Extractor = (*TripleExtractor)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// WithTripleTemperature 设置温度参数。
func WithTripleTemperature(t float64) TripleExtractorOption {
	return func(e *TripleExtractor) { e.temperature = t }
}

// WithTripleMaxConcurrent 设置最大并发数。
func WithTripleMaxConcurrent(n int) TripleExtractorOption {
	return func(e *TripleExtractor) { e.limiter = make(chan struct{}, n) }
}

// NewTripleExtractor 构造 TripleExtractor。
// 默认 temperature=0.0 / maxConcurrent=50（对齐 Python 默认参数）。
//
// Python: triple_extractor.py TripleExtractor.__init__
func NewTripleExtractor(llmClient *llm.Model, modelName string, opts ...TripleExtractorOption) *TripleExtractor {
	e := &TripleExtractor{
		llmClient:   llmClient,
		modelName:   modelName,
		temperature: 0.0,
		limiter:     make(chan struct{}, 50),
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// ──────────────────────────── 非导出函数 ────────────────────────────
