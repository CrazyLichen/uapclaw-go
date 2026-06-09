// Package vector 提供向量存储的抽象接口定义、配套 Schema 类型和 Milvus 后端实现。
//
// 本包定义了所有向量存储后端必须满足的 BaseVectorStore 接口，
// 以及用于描述集合结构的 CollectionSchema、FieldSchema 等数据模型。
// 同时提供基于 Milvus 的完整向量存储实现（MilvusVectorStore），
// 支持集合 CRUD、向量搜索、批量插入和距离转换等功能。
//
// 文件目录：
//
//	vector/
//	├── doc.go        # 包文档
//	├── base.go       # VectorDataType + FieldSchema + CollectionSchema + VectorSearchResult + BaseVectorStore + Option
//	├── utils.go      # 距离/相似度转换函数（L2/余弦/IP）
//	└── milvus.go     # MilvusVectorStore 结构体 + BaseVectorStore 接口实现
//
// 对应 Python 代码：
//
//	openjiuwen/core/foundation/store/base_vector_store.py
//	openjiuwen/core/foundation/store/vector/milvus_vector_store.py
//	openjiuwen/core/foundation/store/vector/utils.py
//
// 核心类型/接口索引：
//
//	VectorDataType      — 字段数据类型枚举（VARCHAR, FLOAT_VECTOR, INT64 等）
//	FieldSchema         — 集合字段 Schema 定义，通过 NewFieldSchema 构造并校验
//	CollectionSchema    — 集合 Schema 定义，通过 NewCollectionSchema 构造并校验
//	VectorSearchResult  — 向量搜索结果，包含 Score 和 Fields
//	BaseVectorStore     — 向量存储基础接口，定义集合 CRUD、向量搜索和元数据操作
//	MilvusVectorStore   — Milvus 向量存储实现，包含完整 CRUD 和搜索功能
//	Option              — 操作可选参数的函数选项模式
package vector
