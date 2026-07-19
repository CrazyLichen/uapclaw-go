// Package subagents 提供子代理配置构建器。
//
// 各 builder 函数根据运行时配置和主模型构建 SubAgentConfig，
// 供 DeepAdapter._build_configured_subagents 调用。
//
// 文件目录：
//
//	subagents/
//	├── doc.go              # 包文档
//	├── research_agent.go   # research 子代理配置构建
//	├── explore_agent.go    # explore 子代理配置构建
//	├── plan_agent.go       # plan 子代理配置构建
//	├── code_agent.go       # code 子代理配置构建
//	├── browser_agent.go    # browser 子代理配置构建（完整配置 + 工厂名称 + 默认提示词）
//	└── verification_agent.go # verification 子代理配置构建
//
// 对应 Python 代码：jiuwenswarm/server/runtime/agent_adapter/subagent_builders/
package subagents
