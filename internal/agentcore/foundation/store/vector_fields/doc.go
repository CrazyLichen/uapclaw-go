// Package vector_fields 提供向量索引配置的通用框架和各数据库索引子类型。
//
// 本包定义了 VectorField 基类及其配套的枚举类型（DatabaseType、IndexType），
// 通过 vf 结构体标签实现 stage 过滤机制，支持子类扩展。
// 提供 Milvus 特有的索引子类型（AUTO/FLAT/HNSW/IVF/SCANN），
// 以及 Chroma 和 PGVector 的索引子类型，以及 GaussDB 的 DiskANN 索引子类型，
// 以及 Elasticsearch 的 k-NN 索引子类型，
// 各子类型通过 vf 标签区分 construct 和 search 阶段的参数。
//
// 核心设计：
//   - 子类通过嵌入 VectorField 并在字段上添加 `vf:"construct"` 或 `vf:"search"` 标签
//     来标记字段所属阶段
//   - ToDict(v, stage) 通过反射读取标签，只输出匹配阶段的字段
//   - 内部字段用 `vf:"-"` 标记，始终过滤
//   - 支持 `vf:"construct,keepzero"` 修饰符保留零值
//   - 支持 Extra 字段合并（字段名以 Extra 开头且类型为 map[string]any）
//
// 与 vector 包的关系：
//   - vector 包定义 FieldSchema/CollectionSchema（数据描述层）
//   - vector_fields 包定义 VectorField 层次结构（索引配置层）
//   - 两者互不导入，Store 实现同时导入两者
//
// 文件目录：
//
//	vector_fields/
//	├── doc.go              # 包文档
//	├── base.go             # VectorField 基类 + DatabaseType/IndexType 枚举 + vf 标签反射机制
//	├── milvus_fields.go    # Milvus 索引子类型（AUTO/FLAT/HNSW/IVF/SCANN）
//	├── chroma_fields.go    # Chroma 索引子类型（HNSW 配置）
//	├── pg_fields.go        # PGVector 索引子类型（HNSW/IVFFlat 配置）
//	├── gauss_fields.go     # GaussDB DiskANN 索引子类型
//	└── es_fields.go        # Elasticsearch HNSW/k-NN 索引子类型
//
// 对应 Python 代码：openjiuwen/core/foundation/store/vector_fields/
//
// 核心类型/接口索引：
//
//	DatabaseType    — 向量数据库类型枚举（Milvus, Chroma, PG, Gauss, ES）
//	IndexType       — 索引类型枚举（AUTO, HNSW, FLAT, IVF, SCANN, DiskANN）
//	VectorField     — 向量索引配置基类，提供 Validate() 方法
//	ToDict(v,stage) — 包级函数，通过反射将 VectorField 或子类转为指定阶段的字典
//	MilvusAUTO      — Milvus AUTOINDEX 配置
//	MilvusFLAT      — Milvus FLAT 索引配置
//	MilvusHNSW      — Milvus HNSW 索引配置（M, EfConstruction, EfSearchFactor）
//	MilvusIVF       — Milvus IVF 索引配置（Nlist, Nprobe）
//	MilvusSCANN     — Milvus SCANN 索引配置（Nlist, Nprobe, WithRawData, ReorderK）
//	ChromaVectorField — Chroma HNSW 索引配置（MaxNeighbors, EfConstruction, EfSearch）
//	PGVectorField   — PGVector 索引配置（HNSW/IVFFlat，M, EfConstruction, EfSearch, Lists, Probes）
//	GaussDiskANN    — GaussDB DiskANN 索引配置（EnablePQ, PGNseg, PGNclus, NumParallels, QuantizationType, SubgraphCount）
//	ESVectorField   — Elasticsearch k-NN 索引配置（NumCandidates, ExtraConstruct, ExtraSearch）
package vector_fields
