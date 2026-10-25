// Package vector_store 提供检索层向量存储抽象接口及各后端实现。
//
// 本包定义面向检索场景的 VectorStore 接口，提供向量搜索、稀疏搜索、
// 混合搜索（RRF 融合）等能力。与 foundation 层 BaseVectorStore（CRUD+迁移）
// 不同，本接口面向检索场景。
//
// 文件目录：
//
//	vector_store/
//	├── doc.go              # 包文档
//	├── base.go             # VectorStore 接口 + StoreOptions + CheckConfigsMatching
//	├── chroma.go           # ChromaVectorStore 实现
//	├── milvus.go           # MilvusVectorStore 实现（MilvusClient 接口 + 适配器）
//	└── store.go            # CreateVectorStore 工厂 + StoreCreateOptions
//
// 对应 Python 代码：
//
//	openjiuwen/core/retrieval/vector_store/base.py
//	openjiuwen/core/retrieval/vector_store/chroma_store.py
//	openjiuwen/core/retrieval/vector_store/milvus_store.py
//	openjiuwen/core/retrieval/vector_store/store.py
//
// 核心类型/接口索引：
//
//	VectorStore      — 检索层向量存储抽象接口
//	StoreOptions     — 向量存储操作选项
//	CheckConfigsMatching — 校验配置一致性静态方法
//	CreateChromaClient — ChromaDB 持久客户端创建函数（含建库逻辑，供 Indexer 复用）
//	MilvusClient     — Milvus 客户端接口（依赖注入，供 Indexer 复用）
//	MilvusVectorStore — Milvus 向量存储实现（搜索/稀疏/混合/校验）
//	DefaultMilvusCreateClient — 默认客户端创建函数
//	CreateVectorStore — 向量存储工厂（按 StoreProvider 分发）
//	StoreCreateOptions — 构造选项（Python **kwargs 收纳）
package vector_store
