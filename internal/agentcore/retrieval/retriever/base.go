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
	// BatchRetrieve 批量并发检索。裸 errgroup 首错即返回（对齐 Python
	// 裸 asyncio.gather：全启动、首错传播、其余 goroutine 不取消跑完结果丢弃）。
	BatchRetrieve(ctx context.Context, queries []string, topK int, opts ...RetrieveOption) ([][]common.RetrievalResult, error)
	// RetrieveSearchResults 检索原始 SearchResult（不归一化、不阈值过滤）。
	RetrieveSearchResults(ctx context.Context, query string, topK int, mode RetrievalMode, opts ...RetrieveOption) ([]common.SearchResult, error)
	// Close 关闭检索器并释放底层资源。
	Close()
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

// RetrieveOptions 检索选项（Python **kwargs 收纳容器）。
//
// 本节 Vector/Sparse 收而不用（对齐 Python 死参数：kwargs 收了不读）；
// 13.21 HybridRetriever 消费 alpha 键；13.27 SimpleKnowledgeBase 写入 filters 键。
type RetrieveOptions struct {
	// Extra 透传给实现的额外参数
	Extra map[string]any
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

// WithRetrieveExtra 设置 Extra 单键的 RetrieveOption。
func WithRetrieveExtra(key string, value any) RetrieveOption {
	return func(o *RetrieveOptions) {
		if o.Extra == nil {
			o.Extra = make(map[string]any)
		}
		o.Extra[key] = value
	}
}
