// Package service 提供 Context Evolver 的服务层函数。
//
// 包含轨迹格式化、试验评估、MaTTS 试验运行、LLM/Embedding 适配器、
// 任务记忆服务（TaskMemoryService）等，供 ContextEvolvingReActAgent
// 和其他上层调用方使用。
// 返回值使用具体 struct（RetrieveResult/SummarizeResult/AddMemoryResult/MaTTSResult）替代 map[string]any。
//
// 文件目录：
//
//	service/
//	├── doc.go                     # 包文档
//	├── llm_wrapper.go             # OpenAILLMWrapper — BaseModelClient 适配，支持 ceconfig APIKey 兜底
//	├── embedding_wrapper.go       # OpenAIEmbeddingWrapper — BaseEmbedding 适配，支持 ceconfig APIKey 兜底
//	├── task_memory_service.go     # TaskMemoryService 核心编排器 + 结果 struct + ceconfig 运行时参数
//	└── trajectory_generator.go    # 轨迹生成和 MaTTS 试验函数
//
// 对应 Python 代码：openjiuwen/extensions/context_evolver/service/
package service
