// Package graph 提供知识图谱图存储的接口定义和核心数据模型。
//
// 图存储用于管理知识图谱中的实体（Entity）、关系（Relation）和片段（Episode），
// 支持基于 Milvus 的混合语义搜索（dense embedding + BM25 sparse）、BFS 图扩展和可选 reranking。
// 本包定义了 BaseGraphStore 接口、图对象模型、配置体系、排序策略和工厂模式，
// 具体后端实现由 milvus 子包提供。
//
// 文件目录：
//
//	graph/
//	├── doc.go           # 包文档
//	├── base.go          # BaseGraphStore 接口 / Options / Option / 常量 / 工厂
//	├── graph_object.go  # BaseGraphObject / NamedGraphObject / Entity / Relation / Episode / EmbedTask
//	├── config.go        # GraphConfig / GraphStoreStorageConfig / GraphStoreIndexConfig / BM25Config
//	├── ranking.go       # BaseRankConfig / WeightedRankConfig / RRFRankConfig / RankerRegistry
//	└── utils.go         # UUID生成 / 时间戳转换 / 批处理 / 格式化
//
// 对应 Python 代码：
//
//	openjiuwen/core/foundation/store/graph/
//
// 核心类型/接口索引：
//
//	BaseGraphStore — 图存储基础接口
//	Entity — 知识图谱实体（节点）
//	Relation — 知识图谱关系（边）
//	Episode — 对话片段
//	GraphConfig — 图存储顶层配置
//	BaseRankConfig — 排序策略接口
//	GraphStoreFactory — 图存储工厂
package graph
