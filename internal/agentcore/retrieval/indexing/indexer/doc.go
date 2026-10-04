// Package indexer 提供 Indexer 抽象接口及其具体实现。
//
// Indexer 负责将已解析、分块、嵌入好的 TextChunk 写入向量数据库，
// 支持新建、更新、删除、存在性检查、元信息查询五类操作。
//
// 文件目录：
//
//	indexer/
//	├── doc.go            # 包文档
//	├── base.go           # Indexer 接口定义
//	└── embed_chunks.go   # ComputeChunkEmbeddings 共享嵌入逻辑
//
// 对应 Python 代码：openjiuwen/core/retrieval/indexing/indexer/
//
// 核心类型索引：
//
//	Indexer — 文档索引抽象接口
//	ComputeChunkEmbeddings — 分块嵌入计算共享函数
package indexer
