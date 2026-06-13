// Package embedding 提供向量嵌入模型的具体实现。
//
// 本包实现了 BaseEmbedding 接口的多种提供者：
// APIEmbedding（通用 HTTP）、OpenAIEmbedding（OpenAI SDK）、
// DashscopeEmbedding（DashScope SDK）、VLLMEmbedding（vLLM 多模态）。
// 同时提供 MultimodalEmbedder 接口。
// MultimodalDocument 多模态文档模型已提升到 retrieval/common 包。
//
// 文件目录：
//
//	embedding/
//	├── doc.go           # 包文档
//	├── common.go        # 共享工具函数 + MultimodalOption/MultimodalEmbedder
//	├── callback.go      # Callback 进度回调接口及默认实现
//	├── utils.go         # base64 解码等工具
//	├── api.go           # APIEmbedding 通用 HTTP 客户端
//	├── openai.go        # OpenAIEmbedding（openai-go SDK）
//	├── dashscope.go     # DashscopeEmbedding（dashscope SDK）
//	└── vllm.go          # VLLMEmbedding（组合 OpenAIEmbedding）
//
// 对应 Python 代码：
//
//	openjiuwen/core/retrieval/embedding/
//
// 核心类型/接口索引：
//
//	MultimodalEmbedder  — 多模态嵌入接口
//	EmbeddingConfig     — 嵌入模型配置
//	APIEmbedding        — 通用 HTTP 嵌入客户端
//	OpenAIEmbedding     — OpenAI 向量嵌入客户端
//	DashscopeEmbedding  — DashScope 向量嵌入客户端
//	VLLMEmbedding       — vLLM 向量嵌入客户端
package embedding
