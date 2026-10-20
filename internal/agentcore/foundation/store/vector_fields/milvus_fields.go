package vector_fields

import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 接口 ────────────────────────────

// MilvusVectorFieldConfig Milvus 向量字段统一接口。
//
// 对齐 Python MilvusVectorField（Union[MilvusAUTO, MilvusFLAT, MilvusHNSW, MilvusIVF, MilvusSCANN] 类型的接口面），
// MilvusAUTO/MilvusFLAT/MilvusHNSW/MilvusIVF/MilvusSCANN 均实现此接口。
// retrieval 层 MilvusVectorStore/MilvusIndexer 通过此接口访问向量字段配置。
type MilvusVectorFieldConfig interface {
	// IndexType 返回索引类型
	IndexType() IndexType
	// VectorFieldName 返回向量字段名
	VectorFieldName() string
	// ToConstructDict 返回构建阶段配置
	ToConstructDict() map[string]any
	// ToSearchDict 返回搜索阶段配置
	ToSearchDict() map[string]any
}

// ──────────────────────────── 结构体 ────────────────────────────

// MilvusAUTO Milvus AUTOINDEX 配置。
// AUTOINDEX 是 Milvus 默认索引类型，在 milvus.yaml 中可配置。
// 默认参数：M=18, efConstruction=240, index_type=HNSW, metric_type=COSINE。
//
// Python: vector_fields/milvus_fields.py (MilvusAUTO)
type MilvusAUTO struct {
	VectorField
}

// MilvusFLAT Milvus FLAT 索引配置。
// FLAT 执行精确最近邻搜索，无近似。精度最高但内存占用大、搜索速度慢。
// 适用于中小规模数据集。
//
// Python: vector_fields/milvus_fields.py (MilvusFLAT)
type MilvusFLAT struct {
	VectorField
}

// MilvusHNSW Milvus HNSW 索引配置。
// HNSW 构建多层图结构进行近似最近邻搜索，搜索性能和精度优秀。
// 支持可选量化变体（SQ/PQ/PRQ）以降低内存占用。
// TODO(#通用): 补充 variant（SQ/PQ/PRQ）、extra_construct、extra_search 字段，
// 以及 validate_extra_args 量化参数校验逻辑，对齐 Python MilvusHNSW。
//
// Python: vector_fields/milvus_fields.py (MilvusHNSW)
type MilvusHNSW struct {
	VectorField
	// M 图中每个节点的最大边数，越高精度越高但内存和构建时间增加
	// S-47: name= 对齐 Python to_dict 输出键（Python 字段名即 M/efConstruction/efSearchFactor）
	M int `vf:"construct,name=M"`
	// EfConstruction 构建索引时考虑的候选邻居数，越高图质量越好但构建越慢
	EfConstruction int `vf:"construct,name=efConstruction"`
	// EfSearchFactor 搜索广度乘数，top_k * EfSearchFactor = Milvus 中的 ef
	EfSearchFactor float64 `vf:"search,name=efSearchFactor"`
}

// MilvusIVF Milvus IVF 索引配置。
// 支持多种量化变体：FLAT、SQ8、PQ、RABITQ。
// TODO(#通用): 补充 variant（FLAT/SQ8/PQ/RABITQ）、extra_construct、extra_search 字段，
// 以及 validate_extra_args 量化参数校验逻辑，对齐 Python MilvusIVF。
//
// Python: vector_fields/milvus_fields.py (MilvusIVF)
type MilvusIVF struct {
	baseIVF
}

// MilvusSCANN Milvus SCANN 索引配置。
// SCANN 是基于 IVF 的索引，使用乘积量化进行压缩。
// 继承 IVF 的簇参数（Nlist、Nprobe）。
//
// Python: vector_fields/milvus_fields.py (MilvusSCANN)
type MilvusSCANN struct {
	baseIVF
	// WithRawData 是否存储原始向量，True 提高精度但增加存储
	WithRawData bool `vf:"construct,keepzero,name=with_raw_data"`
	// ReorderK 搜索时使用高精度向量重排序的结果数，仅 WithRawData=True 时有效
	ReorderK int `vf:"search,name=reorder_k"`
}

// baseIVF IVF 系列索引的公共基类（非导出）。
// IVF 使用 k-means 将向量空间划分为簇，搜索时只查最相关的簇。
//
// Python: vector_fields/milvus_fields.py (_BaseIVF)
type baseIVF struct {
	VectorField
	// Nlist 构建索引时创建的簇数
	Nlist int `vf:"construct,name=nlist"`
	// Nprobe 搜索时查询的簇数，必须 <= Nlist
	Nprobe int `vf:"search,name=nprobe"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// IndexType 返回 AUTO 索引类型。
func (a *MilvusAUTO) IndexType() IndexType { return a.VectorField.IndexType }

// VectorFieldName 返回向量字段名。
func (a *MilvusAUTO) VectorFieldName() string { return a.VectorField.VectorFieldName }

// ToConstructDict 返回构建阶段配置。
// 对齐 Python to_dict(stage="construct")。
func (a *MilvusAUTO) ToConstructDict() map[string]any {
	return ToDict(a, StageConstruct)
}

// ToSearchDict 返回搜索阶段配置。
// 对齐 Python to_dict(stage="search")。
func (a *MilvusAUTO) ToSearchDict() map[string]any {
	return ToDict(a, StageSearch)
}

// NewMilvusAUTO 创建 Milvus AUTOINDEX 配置。
func NewMilvusAUTO(fieldName string) *MilvusAUTO {
	return &MilvusAUTO{
		VectorField: *NewVectorField(DatabaseTypeMilvus, IndexTypeAUTO, fieldName),
	}
}

// IndexType 返回 FLAT 索引类型。
func (f *MilvusFLAT) IndexType() IndexType { return f.VectorField.IndexType }

// VectorFieldName 返回向量字段名。
func (f *MilvusFLAT) VectorFieldName() string { return f.VectorField.VectorFieldName }

// ToConstructDict 返回构建阶段配置。
// 对齐 Python to_dict(stage="construct")。
func (f *MilvusFLAT) ToConstructDict() map[string]any {
	return ToDict(f, StageConstruct)
}

// ToSearchDict 返回搜索阶段配置。
// 对齐 Python to_dict(stage="search")。
func (f *MilvusFLAT) ToSearchDict() map[string]any {
	return ToDict(f, StageSearch)
}

// NewMilvusFLAT 创建 Milvus FLAT 索引配置。
func NewMilvusFLAT(fieldName string) *MilvusFLAT {
	return &MilvusFLAT{
		VectorField: *NewVectorField(DatabaseTypeMilvus, IndexTypeFLAT, fieldName),
	}
}

// IndexType 返回 HNSW 索引类型。
func (h *MilvusHNSW) IndexType() IndexType { return h.VectorField.IndexType }

// VectorFieldName 返回向量字段名。
func (h *MilvusHNSW) VectorFieldName() string { return h.VectorField.VectorFieldName }

// ToConstructDict 返回构建阶段配置。
// 对齐 Python to_dict(stage="construct")。
func (h *MilvusHNSW) ToConstructDict() map[string]any {
	return ToDict(h, StageConstruct)
}

// ToSearchDict 返回搜索阶段配置。
// 对齐 Python to_dict(stage="search")。
func (h *MilvusHNSW) ToSearchDict() map[string]any {
	return ToDict(h, StageSearch)
}

// NewMilvusHNSW 创建 Milvus HNSW 索引配置。
// M: 图中每个节点最大边数，范围 [2, 2048]，默认 30
// efConstruction: 构建时候选邻居数，范围 [1, +∞)，默认 360
// efSearchFactor: 搜索广度乘数，范围 (0, +∞)，默认 2.0
func NewMilvusHNSW(fieldName string, m, efConstruction int, efSearchFactor float64) *MilvusHNSW {
	return &MilvusHNSW{
		VectorField:    *NewVectorField(DatabaseTypeMilvus, IndexTypeHNSW, fieldName),
		M:              m,
		EfConstruction: efConstruction,
		EfSearchFactor: efSearchFactor,
	}
}

// Validate 校验 HNSW 参数：M 范围 [2, 2048]，EfConstruction > 0，EfSearchFactor > 0。
func (h *MilvusHNSW) Validate() error {
	if h.M < 2 || h.M > 2048 {
		return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("HNSW M 必须在范围 [2, 2048] 内，当前值: %d", h.M)),
		)
	}
	if h.EfConstruction < 1 {
		return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("HNSW EfConstruction 必须 >= 1，当前值: %d", h.EfConstruction)),
		)
	}
	if h.EfSearchFactor <= 0 {
		return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("HNSW EfSearchFactor 必须 > 0，当前值: %f", h.EfSearchFactor)),
		)
	}
	return nil
}

// IndexType 返回 IVF 索引类型。
func (iv *MilvusIVF) IndexType() IndexType { return iv.VectorField.IndexType }

// VectorFieldName 返回向量字段名。
func (iv *MilvusIVF) VectorFieldName() string { return iv.VectorField.VectorFieldName }

// ToConstructDict 返回构建阶段配置。
// 对齐 Python to_dict(stage="construct")。
func (iv *MilvusIVF) ToConstructDict() map[string]any {
	return ToDict(iv, StageConstruct)
}

// ToSearchDict 返回搜索阶段配置。
// 对齐 Python to_dict(stage="search")。
func (iv *MilvusIVF) ToSearchDict() map[string]any {
	return ToDict(iv, StageSearch)
}

// NewMilvusIVF 创建 Milvus IVF 索引配置。
// nlist: 构建时的簇数，范围 [1, 65536]，默认 128
// nprobe: 搜索时的查询簇数，范围 [1, 65536] 且 <= nlist，默认 8
func NewMilvusIVF(fieldName string, nlist, nprobe int) *MilvusIVF {
	return &MilvusIVF{
		baseIVF: baseIVF{
			VectorField: *NewVectorField(DatabaseTypeMilvus, IndexTypeIVF, fieldName),
			Nlist:       nlist,
			Nprobe:      nprobe,
		},
	}
}

// Validate 校验 IVF 参数：Nlist > 0，Nprobe > 0，Nprobe <= Nlist。
func (iv *MilvusIVF) Validate() error {
	if iv.Nlist < 1 {
		return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("IVF Nlist 必须 >= 1，当前值: %d", iv.Nlist)),
		)
	}
	if iv.Nprobe < 1 {
		return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("IVF Nprobe 必须 >= 1，当前值: %d", iv.Nprobe)),
		)
	}
	if iv.Nprobe > iv.Nlist {
		return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("IVF Nprobe 必须 <= Nlist，当前值: nprobe=%d, nlist=%d", iv.Nprobe, iv.Nlist)),
		)
	}
	return nil
}

// IndexType 返回 SCANN 索引类型。
func (sc *MilvusSCANN) IndexType() IndexType { return sc.VectorField.IndexType }

// VectorFieldName 返回向量字段名。
func (sc *MilvusSCANN) VectorFieldName() string { return sc.VectorField.VectorFieldName }

// ToConstructDict 返回构建阶段配置。
// 对齐 Python to_dict(stage="construct")。
func (sc *MilvusSCANN) ToConstructDict() map[string]any {
	return ToDict(sc, StageConstruct)
}

// ToSearchDict 返回搜索阶段配置。
// 对齐 Python to_dict(stage="search")。
func (sc *MilvusSCANN) ToSearchDict() map[string]any {
	return ToDict(sc, StageSearch)
}

// NewMilvusSCANN 创建 Milvus SCANN 索引配置。
// nlist: 构建时的簇数，默认 128
// nprobe: 搜索时的查询簇数，默认 8
// withRawData: 是否存储原始向量，默认 true
// reorderK: 搜索时重排序结果数，0 表示不重排序
func NewMilvusSCANN(fieldName string, nlist, nprobe int, withRawData bool, reorderK int) *MilvusSCANN {
	return &MilvusSCANN{
		baseIVF: baseIVF{
			VectorField: *NewVectorField(DatabaseTypeMilvus, IndexTypeSCANN, fieldName),
			Nlist:       nlist,
			Nprobe:      nprobe,
		},
		WithRawData: withRawData,
		ReorderK:    reorderK,
	}
}

// Validate 校验 SCANN 参数：继承 IVF 校验 + ReorderK 不能为负数。
func (s *MilvusSCANN) Validate() error {
	// 先校验 IVF 基类参数
	if s.Nlist < 1 {
		return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("SCANN Nlist 必须 >= 1，当前值: %d", s.Nlist)),
		)
	}
	if s.Nprobe < 1 {
		return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("SCANN Nprobe 必须 >= 1，当前值: %d", s.Nprobe)),
		)
	}
	if s.Nprobe > s.Nlist {
		return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("SCANN Nprobe 必须 <= Nlist，当前值: nprobe=%d, nlist=%d", s.Nprobe, s.Nlist)),
		)
	}
	if s.ReorderK < 0 {
		return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("SCANN ReorderK 必须 >= 0，当前值: %d", s.ReorderK)),
		)
	}
	return nil
}
