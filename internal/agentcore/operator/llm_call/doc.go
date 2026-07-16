// Package llm_call 提供 LLM 提示词参数句柄。
//
// LLMCallOperator 管理 system_prompt 和 user_prompt 参数，
// 不执行 LLM 调用。参数变更通过 onParameterUpdated 回调推送给消费者。
//
// 文件目录：
//
//	llm_call/
//	├── doc.go                # 子包文档
//	└── llm_call_operator.go  # LLMCallOperator
//
// 对应 Python 代码：openjiuwen/core/operator/llm_call/
package llm_call
