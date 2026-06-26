// Package embedding 提供向量嵌入模型的抽象接口。
//
// 本包定义了 BaseEmbedding 接口、EmbedOption 配置和 Callback 回调，
// 提供文本到向量的转换能力，供记忆索引等组件进行语义搜索。
//
// 文件目录：
//
//	embedding/
//	├── doc.go           # 包文档
//	└── base.go          # BaseEmbedding 接口、EmbedOption、Callback 定义
//
// 对应 Python 代码：
//
//	openjiuwen/core/foundation/store/base_embedding.py
//
// 核心类型/接口索引：
//
//	BaseEmbedding — 向量嵌入模型抽象接口（EmbedQuery/EmbedDocuments/Dimension）
//	EmbedOption  — 批量嵌入可选参数（BatchSize/Callback）
//	Callback     — 嵌入进度回调接口（OnBatchComplete）
package embedding
