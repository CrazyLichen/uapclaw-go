// Package agents 提供 Agent 的具体实现。
//
// 本包包含各种 Agent 模式的实现，如 ReActAgent（Reasoning + Acting）等。
// 每个 Agent 实现 single_agent/interfaces 包中定义的 Agent 接口，
// 由 BaseAgent 提供配置/管理能力，子类自行实现 Invoke/Stream。
//
// 文件目录：
//
//	agents/
//	├── doc.go               # 包文档
//	├── react_agent.go       # ReActAgent 结构体定义 + 构造函数
//	├── react_invoke.go      # Invoke/Stream 入口（含回调骨架）+ invokeImpl/streamImpl + ReAct 循环
//	├── react_model_call.go  # LLM 模型调用（callModel/railedModelCall/callLLMInvoke/callLLMStream）
//	├── react_prompt.go      # 系统提示词构建（SystemPromptBuilder/PromptSection/Configure）
//	└── react_helpers.go     # 内部辅助函数（initContext/getLLM/getTools/saveContexts 等）
//
// 对应 Python 代码：openjiuwen/core/single_agent/agents/
package agents
