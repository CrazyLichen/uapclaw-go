// Package retriever 提供检索查询管线的统一门面。
//
// Retriever 接口把 VectorStore 的向量/稀疏/混合搜索能力包装为带
// mode 校验、结果归一化、BM25 兜底的可编排检索器，供 KB 编排层
// （SimpleKnowledgeBase/GraphKnowledgeBase，13.27/13.28）按 index_type
// 多态分发。13.23 增加图检索：GraphRetriever 组合 chunk 检索与三元组
// beam search 图扩展（消费 kb_{id}_triples 索引，RRF k=60 融合），
// 能力接口（EmbedModelProvider 等）消解 Python hasattr/getattr 鸭子探测。
//
// 文件目录：
//
//	retriever/
//	├── doc.go                 # 包文档
//	├── base.go                # Retriever 接口 + RetrievalMode + RetrieveOptions（13.21 平铺 Alpha + 13.23 三潜伏字段）
//	├── capability.go          # 能力接口五件套（13.23，hasattr/getattr 同构）
//	├── vector_retriever.go    # VectorRetriever 向量检索器
//	├── sparse_retriever.go    # SparseRetriever 稀疏检索器（BM25）
//	├── hybrid_retriever.go    # HybridRetriever 混合检索器（向量+稀疏，alpha 权重路由）
//	├── graph_numeric.go       # 余弦打分 + topk（13.23，gonum Norm）
//	├── graph_beam_search.go   # TripleBeamSearch beam search（13.23 图扩展核心算法）
//	├── graph_fetch.go         # Milvus/Chroma 过滤查询 fetch 层（13.23）
//	└── graph_retriever_impl.go # GraphRetriever 图检索器（13.23 主链）
//
// 对应 Python 代码：
//
//	openjiuwen/core/retrieval/retriever/base.py
//	openjiuwen/core/retrieval/retriever/vector_retriever.py
//	openjiuwen/core/retrieval/retriever/sparse_retriever.py
//	openjiuwen/core/retrieval/retriever/hybrid_retriever.py
//	openjiuwen/core/retrieval/retriever/graph_retriever.py
//
// 核心类型/接口索引：
//
//	Retriever      — 检索器抽象接口（四方法）
//	RetrievalMode  — 检索模式枚举（vector/sparse/hybrid）
//	VectorRetriever  — 向量检索器实现
//	SparseRetriever  — 稀疏检索器实现
//	HybridRetriever  — 混合检索器实现（alpha 权重路由，RRF 融合在 store 层）
//	GraphRetriever   — 图检索器（chunk 检索 + beam search 图扩展 + RRF 融合）
//	TripleBeamSearch — 三元组 beam search（两轮嵌入余弦打分 + 三重过滤）
package retriever
