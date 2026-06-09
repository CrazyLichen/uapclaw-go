package vector_fields

import (
	"fmt"
)

// ──────────────────────────── 枚举 ────────────────────────────

// DatabaseType 向量数据库类型。
//
// 对应 Python: vector_fields/base.py (VectorField.database_type)
type DatabaseType int

const (
	// DatabaseTypeMilvus Milvus 向量数据库
	DatabaseTypeMilvus DatabaseType = iota
	// DatabaseTypeChroma ChromaDB 向量数据库
	DatabaseTypeChroma
	// DatabaseTypePG PostgreSQL + pgvector 向量数据库
	DatabaseTypePG
	// DatabaseTypeGauss Gauss 向量数据库
	DatabaseTypeGauss
	// DatabaseTypeES Elasticsearch 向量数据库
	DatabaseTypeES
)

// databaseTypeStrings DatabaseType 枚举值对应的字符串表示，与 Python 枚举值保持一致。
var databaseTypeStrings = [...]string{
	"milvus",
	"chroma",
	"pg",
	"gauss",
	"es",
}

// String 返回 DatabaseType 的字符串表示，与 Python 枚举值一致。
func (dt DatabaseType) String() string {
	if dt >= 0 && int(dt) < len(databaseTypeStrings) {
		return databaseTypeStrings[dt]
	}
	return fmt.Sprintf("UNKNOWN(%d)", dt)
}

// IndexType 向量索引类型。
//
// 对应 Python: vector_fields/base.py (VectorField.index_type)
type IndexType int

const (
	// IndexTypeAUTO 自动选择索引类型
	IndexTypeAUTO IndexType = iota
	// IndexTypeHNSW HNSW 索引
	IndexTypeHNSW
	// IndexTypeFLAT FLAT 索引（精确搜索）
	IndexTypeFLAT
	// IndexTypeIVF IVF 索引
	IndexTypeIVF
	// IndexTypeSCANN SCANN 索引
	IndexTypeSCANN
	// IndexTypeIVFFlat IVFFlat 索引（PG）
	IndexTypeIVFFlat
)

// indexTypeStrings IndexType 枚举值对应的字符串表示，与 Python 枚举值保持一致。
var indexTypeStrings = [...]string{
	"auto",
	"hnsw",
	"flat",
	"ivf",
	"scann",
	"ivfflat",
}

// String 返回 IndexType 的字符串表示，与 Python 枚举值一致。
func (it IndexType) String() string {
	if it >= 0 && int(it) < len(indexTypeStrings) {
		return indexTypeStrings[it]
	}
	return fmt.Sprintf("UNKNOWN(%d)", it)
}
