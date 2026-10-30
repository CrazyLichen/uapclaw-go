package retriever

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
)

// ──────────────────────────── 接口 ────────────────────────────

// Retriever 检索器抽象接口。
//
// Python: openjiuwen/core/retrieval/retriever/base.py (Retriever)。
// retrieve/batch_retrieve 为 Python 抽象方法；retrieve_search_results/close
// 在 Python 基类是非抽象"可选 API"（空方法体，裸调用返回 None），Go 接口
// 无默认实现，四方法全进接口。AgenticRetriever(13.25)/GraphRetriever(13.23)
// 届时自行决定是否返回 nil,nil 对齐 Python None 语义。
type Retriever interface {
	// Retrieve 检索文档，返回归一化的 RetrievalResult 列表。
	// mode 零值 "" 时由实现回落各自默认模式（spec §8-1 已知偏差：
	// Python 显式传空串走 155200，Go 空串等同未设）。
	Retrieve(ctx context.Context, query string, topK int, scoreThreshold *float64, mode RetrievalMode, opts ...RetrieveOption) ([]common.RetrievalResult, error)
	// BatchRetrieve 批量并发检索。裸 errgroup 阻塞等全部 goroutine 完成后
	// 返回时间首个错误（审查修复 M-11 修正表述：原"首错即返回"不准确）。
	// 对齐 Python 裸 asyncio.gather 的错误负载与传播序（全启动、其余
	// goroutine 不取消跑完结果丢弃），差异仅在返回时机——Python 首异常
	// 立即向 awaiter 传播，Go Wait() 等最慢查询结束才返回首错
	// （2026-10-27 审查 M-11 拍板维持 errgroup，语义声明容差归档）。
	BatchRetrieve(ctx context.Context, queries []string, topK int, opts ...RetrieveOption) ([][]common.RetrievalResult, error)
	// RetrieveSearchResults 检索原始 SearchResult（不归一化、不阈值过滤）。
	RetrieveSearchResults(ctx context.Context, query string, topK int, mode RetrievalMode, opts ...RetrieveOption) ([]common.SearchResult, error)
	// Close 关闭检索器并释放底层资源。
	Close() error
}

// ──────────────────────────── 枚举 ────────────────────────────

// RetrievalMode 检索模式，对齐 Python Literal["vector", "sparse", "hybrid"]。
type RetrievalMode string

const (
	// RetrievalModeVector 向量检索
	RetrievalModeVector RetrievalMode = "vector"
	// RetrievalModeSparse 稀疏检索（BM25）
	RetrievalModeSparse RetrievalMode = "sparse"
	// RetrievalModeHybrid 混合检索
	RetrievalModeHybrid RetrievalMode = "hybrid"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RetrieveOptions 检索选项（Python **kwargs 的闭合世界对应物）。
//
// 13.21 修订：原 Extra map[string]any 平铺为类型化字段（编译期类型安全，
// 2026-10-28 拍板）。原"Extra 收纳 **kwargs"设计见 13.19 spec §4.1，
// 修订记录归档两 spec，原约定不删除。
type RetrieveOptions struct {
	// Alpha hybrid 检索权重（0=纯稀疏，1=纯向量，0.5=平衡）。
	// nil = 未设置 → HybridRetriever 回落构造默认；非 nil = 调用时覆写。
	// 指针类型区分"未设"与"显式 0"（alpha=0 是合法纯稀疏语义，不能与
	// 零值混淆）。仅 HybridRetriever 消费；Vector/Sparse 不读
	// （对齐 Python 死参数）。
	Alpha *float64
	// GraphHops 图检索跳数（13.23 消费；nil→默认 2；显式 0 →
	// max_length=0 → 155204，Python 行为保真）。潜伏参数：Python 侧
	// kwargs.get 消费、全库零生产方（13.28 接线后激活）。
	GraphHops *int
	// NumBeams beam search 束宽（13.23 TripleBeamSearch 消费；nil→10）。
	NumBeams *int
	// NumCandidatesPerBeam 每束候选数（13.23 消费；nil→100）。
	NumCandidatesPerBeam *int
	// GraphExpansion 图路径是否启用图扩展（13.25 AgenticRetriever 消费）。
	// nil → true（对齐 Python kwargs.pop("graph_expansion", True)）。
	// 指针类型：13.28 GraphKB 传 RetrievalConfig.graph_expansion 默认 False，
	// bool 零值无法区分"未设"与"显式 false"（Python 两层默认值不一致是
	// 真实行为，字面复刻）。
	GraphExpansion *bool
	// Filters 元数据过滤条件（13.27 SimpleKB 写入）。
	// 潜伏参数：Python 侧 filters 进了 retriever kwargs，但 Vector/Sparse/
	// Hybrid/Agentic 四个实现全不读（store 调用的 filters 硬编码 None），
	// 属"写了不消费"的死参数字面复刻（13.19 计划行承诺，13.27 spec §8-14）。
	// 非指针：map 零值 nil 即"未设置"，与 Python Optional[Dict] 同义。
	Filters map[string]any
}

// RetrieveOption RetrieveOptions 的函数选项。
type RetrieveOption func(*RetrieveOptions)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewRetrieveOptions 从可变参数构建 RetrieveOptions。
func NewRetrieveOptions(opts ...RetrieveOption) RetrieveOptions {
	o := RetrieveOptions{}
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// WithRetrieveAlpha 设置 hybrid 检索权重的 RetrieveOption。
func WithRetrieveAlpha(v float64) RetrieveOption {
	return func(o *RetrieveOptions) { o.Alpha = &v }
}

// WithRetrieveGraphHops 设置图检索跳数的 RetrieveOption。
func WithRetrieveGraphHops(v int) RetrieveOption {
	return func(o *RetrieveOptions) { o.GraphHops = &v }
}

// WithRetrieveNumBeams 设置 beam search 束宽的 RetrieveOption。
func WithRetrieveNumBeams(v int) RetrieveOption {
	return func(o *RetrieveOptions) { o.NumBeams = &v }
}

// WithRetrieveNumCandidatesPerBeam 设置每束候选数的 RetrieveOption。
func WithRetrieveNumCandidatesPerBeam(v int) RetrieveOption {
	return func(o *RetrieveOptions) { o.NumCandidatesPerBeam = &v }
}

// WithRetrieveGraphExpansion 设置图路径是否启用图扩展的 RetrieveOption。
func WithRetrieveGraphExpansion(v bool) RetrieveOption {
	return func(o *RetrieveOptions) { o.GraphExpansion = &v }
}

// WithRetrieveFilters 设置元数据过滤条件的 RetrieveOption。
//
// 对齐 Python retrieve(filters=...)：值写进选项，但四个 retriever 实现
// 均不消费（死参数字面复刻，见 RetrieveOptions.Filters 注释）。
func WithRetrieveFilters(filters map[string]any) RetrieveOption {
	return func(o *RetrieveOptions) { o.Filters = filters }
}
