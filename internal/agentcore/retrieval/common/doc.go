// Package common 提供 retrieval 子包共享的数据类型和工具函数。
//
// 本包存放 embedding、reranker 等子包共同依赖的文档模型，
// 对齐 Python 的 openjiuwen/core/retrieval/common/ 模块。
//
// 文件目录：
//
//	common/
//	├── doc.go           # 包文档
//	└── document.go      # MultimodalDocument 多模态文档模型
//
// 对应 Python 代码：
//
//	openjiuwen/core/retrieval/common/document.py
//
// 核心类型索引：
//
//	ModalityKind      — 内容模态类型枚举
//	ModalityField     — 单个模态字段
//	MultimodalDocument — 多模态文档（文本/图片/音频/视频）
package common
