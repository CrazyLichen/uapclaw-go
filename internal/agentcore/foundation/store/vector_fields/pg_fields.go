package vector_fields

import "fmt"

// ──────────────────────────── 结构体 ────────────────────────────

// PGVectorField PostgreSQL + pgvector 索引配置。
// 支持 HNSW 和 IVFFlat 两种索引类型。
//
// 对应 Python: vector_fields/pg_fields.py (PGVectorField)
type PGVectorField struct {
	VectorField
	// M HNSW 每层最大连接数
	M int `vf:"construct"`
	// EfConstruction 索引构建时考虑的候选邻居数（HNSW）
	EfConstruction int `vf:"construct"`
	// EfSearch 搜索时探索的候选数（HNSW）
	EfSearch int `vf:"construct"`
	// Lists IVFFlat 列表数
	Lists int `vf:"construct"`
	// Probes IVFFlat 探测数
	Probes int `vf:"construct"`
	// ExtraSearch 额外搜索参数
	ExtraSearch map[string]any `vf:"search"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewPGVectorFieldHNSW 创建 PG HNSW 索引配置。
// fieldName 为向量字段名。
func NewPGVectorFieldHNSW(fieldName string, m, efConstruction, efSearch int) *PGVectorField {
	return &PGVectorField{
		VectorField:    VectorField{DatabaseType: DatabaseTypePG, IndexType: IndexTypeHNSW, VectorFieldName: fieldName},
		M:              m,
		EfConstruction: efConstruction,
		EfSearch:       efSearch,
	}
}

// NewPGVectorFieldIVFFlat 创建 PG IVFFlat 索引配置。
// fieldName 为向量字段名。
// 注意：IVFFlat 在 Python 的 index_type 枚举中对应 "ivfflat"，
// Go 的 IndexType 枚举中没有 IVFFlat（已移除，避免与 Python 不一致），
// 此处使用 IndexTypeIVF 替代，database_type 仍为 PG。
func NewPGVectorFieldIVFFlat(fieldName string, lists, probes int) *PGVectorField {
	return &PGVectorField{
		VectorField: VectorField{DatabaseType: DatabaseTypePG, IndexType: IndexTypeIVF, VectorFieldName: fieldName},
		Lists:       lists,
		Probes:      probes,
	}
}

// Validate 校验 PGVectorField 参数。
func (p *PGVectorField) Validate() error {
	if p.IndexType == IndexTypeHNSW {
		if p.M < 2 || p.M > 2000 {
			return fmt.Errorf("m 必须在 [2, 2000] 范围内，当前值: %d", p.M)
		}
		if p.EfConstruction < 1 {
			return fmt.Errorf("ef_construction 必须 >= 1，当前值: %d", p.EfConstruction)
		}
		if p.EfSearch < 1 {
			return fmt.Errorf("ef_search 必须 >= 1，当前值: %d", p.EfSearch)
		}
	}
	if p.IndexType == IndexTypeIVF {
		if p.Lists < 1 {
			return fmt.Errorf("lists 必须 >= 1，当前值: %d", p.Lists)
		}
		if p.Probes < 1 {
			return fmt.Errorf("probes 必须 >= 1，当前值: %d", p.Probes)
		}
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────
