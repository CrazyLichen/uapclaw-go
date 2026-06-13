// Package utils 提供 retrieval 层的公共工具函数。
//
// 本包提供 HTTP 重试请求工具，供 Reranker 和 Embedding 等组件共享。
// 对齐 Python: openjiuwen/core/retrieval/utils/api_requests.py
//
// 文件目录：
//
//	utils/
//	├── doc.go              # 包文档
//	├── api_requests.go     # HTTP 重试请求工具
//	└── api_requests_test.go # 单元测试
//
// 对应 Python 代码：
//
//	openjiuwen/core/retrieval/utils/api_requests.py
//
// 核心类型/函数索引：
//
//	TaskName            — 任务类型（TaskReranker / TaskEmbedding）
//	RetryConfig         — 重试配置
//	RequestWithRetry    — 带重试的 HTTP POST 请求
//	RequestWithRetrySync — 带重试的同步 HTTP POST 请求
package utils
