package vector_fields

import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ChromaVectorField ChromaDB HNSW 索引配置。
// ChromaDB 仅支持 HNSW 索引，database_type 和 index_type 自动设为 chroma/hnsw。
//
// Python: vector_fields/chroma_fields.py (ChromaVectorField)
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

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultMaxNeighbors 默认最大邻居数，对齐 Python max_neighbors=16
	defaultMaxNeighbors = 16
	// defaultEfConstruction 默认构建候选数，对齐 Python ef_construction=100
	defaultEfConstruction = 100
	// defaultEfSearch 默认搜索候选数，对齐 Python ef_search=100
	defaultEfSearch = 100.0
)

// ──────────────────────────── 全局变量 ────────────────────────────

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

// NewDefaultChromaVectorField 使用默认参数创建 ChromaDB HNSW 索引配置。
// 向量字段名默认为 "embedding"。
// 对齐 Python ChromaVectorField() 默认值。
func NewDefaultChromaVectorField() *ChromaVectorField {
	return NewChromaVectorField("embedding", defaultMaxNeighbors, defaultEfConstruction, defaultEfSearch)
}

// NewChromaVectorFieldFromName 从向量字段名创建默认 ChromaVectorField。
// 对齐 Python ChromaVectorField(vector_field=name)。
// Python 中 vector_field 参数支持 str，自动包装为 ChromaVectorField。
func NewChromaVectorFieldFromName(name string) *ChromaVectorField {
	return NewChromaVectorField(name, defaultMaxNeighbors, defaultEfConstruction, defaultEfSearch)
}

// ToConstructDict 返回构建阶段配置。
// 对齐 Python vector_field.to_dict(stage="construct")。
func (c *ChromaVectorField) ToConstructDict() map[string]any {
	return ToDict(c, StageConstruct)
}

// ToSearchDict 返回搜索阶段配置。
// 对齐 Python vector_field.to_dict(stage="search")。
func (c *ChromaVectorField) ToSearchDict() map[string]any {
	return ToDict(c, StageSearch)
}

// ValidateExtraSearch 校验 ExtraSearch 字段类型。
//
// 对齐 Python ChromaVectorField.validate_kwargs。
//   - resize_factor 必须为 int 或 float
//   - num_threads/batch_size/sync_threshold 必须为 int
func (c *ChromaVectorField) ValidateExtraSearch() error {
	if c.ExtraSearch == nil {
		return nil
	}

	// 校验 resize_factor，对齐 Python: if not isinstance(search_dict.get("resize_factor", 1.2), (int, float))
	if v, ok := c.ExtraSearch["resize_factor"]; ok {
		switch v.(type) {
		case int, int64, float32, float64:
			// 合法
		default:
			return exception.BuildError(
				exception.StatusRetrievalIndexingVectorFieldInvalid,
				exception.WithParam("error_msg",
					fmt.Sprintf("ChromaVectorField.extra_search 字段 resize_factor 类型无效，期望 int 或 float，实际 %T", v)),
			)
		}
	}

	// 校验 num_threads / batch_size / sync_threshold
	// 对齐 Python: for int_attr in ["num_threads", "batch_size", "sync_threshold"]:
	//   if not isinstance(search_dict.get(int_attr, 1), int):
	intAttrs := []string{"num_threads", "batch_size", "sync_threshold"}
	for _, attr := range intAttrs {
		if v, ok := c.ExtraSearch[attr]; ok {
			switch v.(type) {
			case int, int64:
				// 合法
			default:
				return exception.BuildError(
					exception.StatusRetrievalIndexingVectorFieldInvalid,
					exception.WithParam("error_msg",
						fmt.Sprintf("ChromaVectorField.extra_search 字段 %s 类型无效，期望 int，实际 %T", attr, v)),
				)
			}
		}
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────
