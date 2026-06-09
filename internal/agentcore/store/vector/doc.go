// Package vector 提供向量存储的抽象接口定义和配套 Schema 类型。
//
// 本包定义了所有向量存储后端必须满足的 BaseVectorStore 接口，
// 以及用于描述集合结构的 CollectionSchema、FieldSchema 等数据模型。
// 当前仅有接口和类型定义，具体后端实现（Milvus、Chroma 等）在后续步骤中添加。
//
// 文件目录：
//
//	vector/
//	├── doc.go        # 包文档
//	└── base.go       # VectorDataType + FieldSchema + CollectionSchema + VectorSearchResult + BaseVectorStore + Option
//
// 对应 Python 代码：openjiuwen/core/foundation/store/base_vector_store.py
//
// 核心类型/接口索引：
//
//	VectorDataType      — 字段数据类型枚举（VARCHAR, FLOAT_VECTOR, INT64 等）
//	FieldSchema         — 集合字段 Schema 定义，通过 NewFieldSchema 构造并校验
//	CollectionSchema    — 集合 Schema 定义，通过 NewCollectionSchema 构造并校验
//	VectorSearchResult  — 向量搜索结果，包含 Score 和 Fields
//	BaseVectorStore     — 向量存储基础接口，定义集合 CRUD 和向量搜索
//	Option              — 操作可选参数的函数选项模式
package vector
