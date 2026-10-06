package indexer

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// IndexOptions Indexer 方法选项结构。
type IndexOptions struct {
	// Extra 额外参数，对齐 Python **kwargs
	Extra map[string]any
}

// IndexOption Indexer 方法的可选参数。
type IndexOption func(*IndexOptions)

// ──────────────────────────── 枚举 ────────────────────────────
// ──────────────────────────── 接口 ────────────────────────────

// Indexer 文档索引抽象接口。
//
// 负责将 TextChunk 写入向量数据库，支持五种操作：
// BuildIndex（新建）、UpdateIndex（更新）、DeleteIndex（删除）、
// IndexExists（存在性检查）、GetIndexInfo（元信息查询）。
//
// Python: openjiuwen/core/retrieval/indexing/indexer/base.py (Indexer)
type Indexer interface {
	// BuildIndex 构建索引。将 chunks 去重→嵌入→写入向量库。
	// embedModel 为 nil 时由具体实现决定如何获取嵌入。
	BuildIndex(ctx context.Context, chunks []common.TextChunk, config common.IndexConfig, embedModel embedding.BaseEmbedding, opts ...IndexOption) (bool, error)

	// UpdateIndex 更新索引。按 docID 先删后建。
	UpdateIndex(ctx context.Context, chunks []common.TextChunk, docID string, config common.IndexConfig, embedModel embedding.BaseEmbedding, opts ...IndexOption) (bool, error)

	// DeleteIndex 删除索引。按 docID 删除指定文档的所有分块。
	DeleteIndex(ctx context.Context, docID string, indexName string) (bool, error)

	// IndexExists 检查索引是否存在。
	IndexExists(ctx context.Context, indexName string) (bool, error)

	// GetIndexInfo 获取索引元信息。
	GetIndexInfo(ctx context.Context, indexName string) (map[string]any, error)
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 全局变量 ────────────────────────────
// ──────────────────────────── 导出函数 ────────────────────────────

// NewIndexOptions 从可变参数构建 IndexOptions。
func NewIndexOptions(opts ...IndexOption) *IndexOptions {
	o := &IndexOptions{}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// WithIndexExtra 设置额外参数的 IndexOption。
func WithIndexExtra(key string, value any) IndexOption {
	return func(o *IndexOptions) {
		if o.Extra == nil {
			o.Extra = make(map[string]any)
		}
		o.Extra[key] = value
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
