// Package reranker 提供重排序模型的具体实现。
//
// 本包实现了 BaseReranker 接口的多种提供者：
// StandardReranker（vLLM 风格 /rerank API）、
// ChatReranker（Chat Completion + logit_bias 实验性方案）和
// DashScopeReranker（阿里云 DashScope 云服务重排序）。
// 同时提供 RerankerBase 默认实现基类。
//
// 文件目录：
//
//	reranker/
//	├── doc.go                # 包文档
//	├── reranker_base.go      # RerankerBase 基类 + 通用方法
//	├── standard.go           # StandardReranker 标准重排序客户端
//	├── chat.go               # ChatReranker 对话式重排序客户端
//	├── dashscope.go          # DashScopeReranker 云服务重排序客户端
//	├── reranker_base_test.go # 基类单元测试
//	├── standard_test.go      # StandardReranker 单元测试
//	├── chat_test.go          # ChatReranker 单元测试
//	└── dashscope_test.go     # DashScopeReranker 单元测试
//
// 对应 Python 代码：
//
//	openjiuwen/core/retrieval/reranker/standard_reranker.py
//	openjiuwen/core/retrieval/reranker/chat_reranker.py
//	openjiuwen/core/retrieval/reranker/dashscope_reranker.py
//
// 核心类型/接口索引：
//
//	RerankerBase      — 默认实现基类，提供通用 HTTP 请求/响应处理方法
//	StandardReranker  — 标准重排序客户端（/rerank API）
//	ChatReranker      — 对话式重排序客户端（/chat/completions + logprobs）
//	DashScopeReranker — DashScope 云服务重排序客户端（支持多模态）
package reranker
