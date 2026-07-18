// Package llm_resilience 提供演化层的 LLM 弹性重试策略。
//
// LLMInvokePolicy 控制单次尝试超时、总预算、最大尝试次数和指数退避。
// InvokeTextWithRetry 和 InvokeTextWithRetryAndPrompt 提供带重试的 LLM 文本调用，
// 处理三种失败模式：调用异常、空响应、不可用响应。
//
// 总预算控制采用双重方案：外层 context.WithTimeout + 每次 attempt 前手动检查剩余预算。
//
// 文件目录：
//
//	llm_resilience/
//	├── doc.go                # 包文档
//	└── llm_resilience.go     # LLMInvokePolicy + InvokeTextWithRetry + 辅助函数
//
// 对应 Python 代码：openjiuwen/agent_evolving/optimizer/llm_resilience.py
package llm_resilience
