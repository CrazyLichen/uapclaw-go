// Package common 提供 retrieval 子包共享的数据类型和工具函数。
//
// 本包存放 embedding、reranker、indexing 等子包共同依赖的文档模型和配置类型，
// 对齐 Python 的 openjiuwen/core/retrieval/common/ 模块。
//
// 文件目录：
//
//	common/
//	├── doc.go           # 包文档
//	├── config.go        # 索引/知识库/检索/向量存储配置类型
//	└── document.go      # MultimodalDocument + TextChunk 文档数据模型
//
// 对应 Python 代码：
//
//	openjiuwen/core/retrieval/common/config.py
//	openjiuwen/core/retrieval/common/document.py
//
// 核心类型索引：
//
//	ModalityKind       — 内容模态类型枚举
//	ModalityField      — 单个模态字段
//	MultimodalDocument — 多模态文档（文本/图片/音频/视频）
//	TextChunk          — 文本分块数据模型
//	IndexConfig        — 索引配置
//	KnowledgeBaseConfig — 知识库配置
//	RetrievalConfig    — 检索配置
//	VectorStoreConfig  — 向量存储配置
//	IndexTypeKind      — 索引类型枚举
//	StoreType          — 向量存储提供商枚举
//	DistanceMetricKind — 距离度量枚举
package common
