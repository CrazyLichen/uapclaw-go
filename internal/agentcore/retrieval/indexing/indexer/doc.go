// Package indexer 提供 Indexer 抽象接口及其具体实现。
//
// Indexer 负责将已解析、分块、嵌入好的 TextChunk 写入向量数据库，
// 支持新建、更新、删除、存在性检查、元信息查询五类操作。
//
// 文件目录：
//
//	indexer/
//	├── doc.go            # 包文档
//	├── base.go           # Indexer 接口定义 + IndexOptions
//	├── embed_chunks.go   # ComputeChunkEmbeddings 共享嵌入逻辑 + ChunkEmbedOptions
//	├── chroma.go         # ChromaIndexer ChromaDB 文档索引实现
//	└── milvus.go         # MilvusIndexer Milvus 文档索引实现
//
// 对应 Python 代码：openjiuwen/core/retrieval/indexing/indexer/
//
// 核心类型索引：
//
//	Indexer              — 文档索引抽象接口
//	ComputeChunkEmbeddings — 分块嵌入计算共享函数
//	ChunkEmbedOptions    — ComputeChunkEmbeddings 选项结构
//	ChromaIndexer        — ChromaDB 文档索引实现（持久 client + Close）
//	MilvusIndexer        — Milvus 文档索引实现（ensureCollection/BuildIndex/UpdateIndex）
//	（13.26）ChromaIndexer/MilvusIndexer 增 6 属性 getter
//	（DatabaseName/TextField/VectorField(any)/SparseVectorField/MetadataField/DocIDField）
//	——knowledgebase 包 validate_index 消费面
package indexer
