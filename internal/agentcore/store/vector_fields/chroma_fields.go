package vector_fields

import "fmt"

// ──────────────────────────── 结构体 ────────────────────────────

// ChromaVectorField ChromaDB HNSW 索引配置。
// ChromaDB 仅支持 HNSW 索引，database_type 和 index_type 自动设为 chroma/hnsw。
//
// 对应 Python: vector_fields/chroma_fields.py (ChromaVectorField)
type ChromaVectorField struct {
	VectorField
	// MaxNeighbors HNSW 图中每个节点的最大边数
	MaxNeighbors int `vf:"construct"`
	// EfConstruction 索引构建时考虑的候选邻居数
	EfConstruction int `vf:"construct"`
	// EfSearch 搜索时探索的候选数
	EfSearch float64 `vf:"construct"`
	// ExtraSearch 额外搜索参数，支持 resize_factor/num_threads/batch_size/sync_threshold
	ExtraSearch map[string]any `vf:"search"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewChromaVectorField 创建 ChromaDB HNSW 索引配置。
// fieldName 为向量字段名。
func NewChromaVectorField(fieldName string, maxNeighbors, efConstruction int, efSearch float64) *ChromaVectorField {
	return &ChromaVectorField{
		VectorField:    VectorField{DatabaseType: DatabaseTypeChroma, IndexType: IndexTypeHNSW, VectorFieldName: fieldName},
		MaxNeighbors:   maxNeighbors,
		EfConstruction: efConstruction,
		EfSearch:       efSearch,
	}
}

// Validate 校验 ChromaVectorField 参数。
func (c *ChromaVectorField) Validate() error {
	if c.MaxNeighbors < 2 || c.MaxNeighbors > 2048 {
		return fmt.Errorf("max_neighbors 必须在 [2, 2048] 范围内，当前值: %d", c.MaxNeighbors)
	}
	if c.EfConstruction < 1 {
		return fmt.Errorf("ef_construction 必须 >= 1，当前值: %d", c.EfConstruction)
	}
	if c.EfSearch < 1 {
		return fmt.Errorf("ef_search 必须 >= 1，当前值: %f", c.EfSearch)
	}
	return nil
}
