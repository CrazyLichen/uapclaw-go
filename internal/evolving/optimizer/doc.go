// Package optimizer 提供自演化系统的维度优化器。
//
// BaseOptimizer 定义优化器的公共接口和 Mixin 辅助结构体，
// 子优化器嵌入 Mixin 获得公共字段和方法，自己实现 Backward/Step 等核心方法。
// TextualParameter 是梯度容器，存储 target→梯度值和可选描述。
//
// 文件目录：
//
//	optimizer/
//	├── doc.go                # 包文档
//	├── base.go               # BaseOptimizer 接口 + BaseOptimizerMixin + TextualParameter
//	├── llm_call/             # LLM 维度提示词优化器
//	│   ├── doc.go            # 包文档
//	│   ├── base.go           # LLMCallOptimizerBase 嵌入结构体
//	│   ├── instruction_optimizer.go # InstructionOptimizer 核心实现
//	│   └── templates.go      # PromptTemplate 模板常量
//	└── llm_resilience/       # LLM 弹性重试策略
//	    ├── doc.go            # 包文档
//	    └── llm_resilience.go # LLMInvokePolicy + InvokeTextWithRetry
//
// 对应 Python 代码：openjiuwen/agent_evolving/optimizer/base.py
package optimizer
