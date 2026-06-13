// Package milvus 提供 Graph Store 的 Milvus 后端实现。
//
// 基于 Milvus 向量数据库实现图存储，支持三集合（Entity/Relation/Episode）的
// CRUD 操作、3通道混合搜索（name_embedding + content_embedding + content_bm25）、
// BFS 图扩展和可选 reranking。
//
// 文件目录：
//
//	milvus/
//	├── doc.go              # 包文档
//	├── schema.go           # 集合 Schema 和索引构建
//	├── milvus_writer.go    # graphWriter 写入逻辑
//	├── milvus_searcher.go  # graphSearcher 搜索逻辑
//	└── milvus.go           # MilvusGraphStore 主结构体 + 接口委托 + lazy init
//
// 对应 Python 代码：openjiuwen/core/foundation/store/graph/milvus/
package milvus
