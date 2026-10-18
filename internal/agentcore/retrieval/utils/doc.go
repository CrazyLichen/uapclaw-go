// Package utils 提供 retrieval 层的公共工具函数。
//
// 本包提供 HTTP 重试请求工具、RRF 融合算法、Milvus 连接别名生成等，供 Reranker、Embedding、VectorStore、Indexer 等组件共享。
// Python: openjiuwen/core/retrieval/utils/
//
// 文件目录：
//
//	utils/
//	├── doc.go              # 包文档
//	├── api_requests.go     # HTTP 重试请求工具
//	├── fusion.go           # RRFFusion 倒数秩融合算法
//	└── common.go           # Milvus 连接别名生成 + 泛型去重工具
//
// 对应 Python 代码：
//
//	openjiuwen/core/retrieval/utils/api_requests.py
//	openjiuwen/core/retrieval/utils/fusion.py
//	openjiuwen/core/retrieval/utils/common.py
//
// 核心类型/函数索引：
//
//	TaskName            — 任务类型（TaskReranker / TaskEmbedding）
//	RetryConfig         — 重试配置
//	RequestWithRetry    — 带重试的 HTTP POST 请求
//	RequestWithRetrySync — 带重试的同步 HTTP POST 请求
//	RRFFusion           — 倒数秩融合算法
//	CreateMilvusAlias   — 生成 Milvus 连接别名
//	Deduplicate         — 泛型去重（保留顺序）
package utils
