// Package retriever 提供检索查询管线的统一门面。
//
// Retriever 接口把 VectorStore 的向量/稀疏/混合搜索能力包装为带
// mode 校验、结果归一化、BM25 兜底的可编排检索器，供 KB 编排层
// （SimpleKnowledgeBase/GraphKnowledgeBase，13.27/13.28）按 index_type
// 多态分发。
//
// 文件目录：
//
//	retriever/
//	├── doc.go               # 包文档
//	├── base.go              # Retriever 接口 + RetrievalMode + RetrieveOptions
//	├── vector_retriever.go  # VectorRetriever 向量检索器
//	└── sparse_retriever.go  # SparseRetriever 稀疏检索器（BM25）
//
// 对应 Python 代码：
//
//	openjiuwen/core/retrieval/retriever/base.py
//	openjiuwen/core/retrieval/retriever/vector_retriever.py
//	openjiuwen/core/retrieval/retriever/sparse_retriever.py
//
// 核心类型/接口索引：
//
//	Retriever      — 检索器抽象接口（四方法）
//	RetrievalMode  — 检索模式枚举（vector/sparse/hybrid）
//	VectorRetriever  — 向量检索器实现
//	SparseRetriever  — 稀疏检索器实现
package retriever
