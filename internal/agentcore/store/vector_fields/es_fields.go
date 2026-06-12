package vector_fields

import "fmt"

// ──────────────────────────── 结构体 ────────────────────────────

// ESVectorField Elasticsearch 向量索引配置。
// ES 8.x 使用 dense_vector 字段的 HNSW 算法实现 k-NN 搜索。
//
// 对应 Python: es_vector_store.py 中的 k-NN 索引参数
type ESVectorField struct {
	VectorField
	// NumCandidates k-NN 搜索候选集大小（search 阶段）
	NumCandidates int `vf:"search"`
	// ExtraConstruct 构建阶段额外参数
	ExtraConstruct map[string]any `vf:"construct"`
	// ExtraSearch 搜索阶段额外参数
	ExtraSearch map[string]any `vf:"search"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewESVectorField 创建 Elasticsearch 向量索引配置，使用默认参数。
// fieldName 为向量字段名。
func NewESVectorField(fieldName string) *ESVectorField {
	return &ESVectorField{
		VectorField: VectorField{
			DatabaseType:    DatabaseTypeES,
			IndexType:       IndexTypeHNSW,
			VectorFieldName: fieldName,
		},
		NumCandidates: 100,
	}
}

// Validate 校验 ESVectorField 参数。
func (e *ESVectorField) Validate() error {
	if e.NumCandidates < 0 {
		return fmt.Errorf("NumCandidates 不能为负数，当前值: %d", e.NumCandidates)
	}
	return nil
}
