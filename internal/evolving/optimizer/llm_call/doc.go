// Package llm_call 提供 LLM 维度的提示词优化器。
//
// LLMCallOptimizerBase 定义 LLM 维度优化器的公共逻辑，
// 固定 domain="llm"，默认优化目标为 system_prompt 和 user_prompt。
// InstructionOptimizer 通过文本梯度优化改写提示词。
//
// 文件目录：
//
//	llm_call/
//	├── doc.go                    # 包文档
//	├── base.go                   # LLMCallOptimizerBase 嵌入结构体
//	├── instruction_optimizer.go  # InstructionOptimizer 核心实现
//	└── templates.go              # PromptTemplate 模板常量
//
// 对应 Python 代码：openjiuwen/agent_evolving/optimizer/llm_call/
package llm_call
