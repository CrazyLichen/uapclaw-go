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
//	├── client.go           # milvusClient 接口定义
//	├── schema.go           # 集合 Schema 和索引构建
//	├── milvus_writer.go    # graphWriter 写入逻辑
//	├── milvus_searcher.go  # graphSearcher 搜索逻辑
//	├── milvus.go           # MilvusGraphStore 主结构体 + 接口委托 + lazy init + init() 注册
//	└── adapter.go          # milvusClientGraphAdapter SDK 客户端适配器
//
// 对应 Python 代码：openjiuwen/core/foundation/store/graph/milvus/
//
// 核心类型索引：
//
//   - MilvusGraphStore — Milvus 图存储主结构体，实现 graph.BaseGraphStore 接口
//   - milvusClient — Milvus 客户端接口（非导出），用于测试 mock 和生产适配
//   - graphWriter — 图写入器（非导出），负责 Entity/Relation/Episode 的 CRUD 写入
//   - graphSearcher — 图搜索器（非导出），负责混合搜索和 BFS 图扩展
//   - milvusClientGraphAdapter — SDK 客户端适配器（非导出），包装真实 Milvus SDK 客户端
//
// 导出函数索引：
//
//   - NewMilvusGraphStore — 创建 Milvus 图存储实例
//   - EnsureCollections — 确保三个图集合存在
package milvus
