// Package adapter 提供 Agent 适配器接口与工厂。
//
// 定义 AgentAdapter 接口——Agent SDK 后端的最小能力集，
// 以及 CreateAdapter 工厂函数按 SDK+Mode 创建适配器实例。
//
// 三种模式适配器：
//   - DeepAdapter：Deep SDK 适配器，agent/plan/fast/team 模式均使用
//   - CodeAdapter：Code 模式适配器，组合委托 DeepAdapter，仅覆盖 CreateInstance
//   - Python 中无独立 AgentAdapter，agent 模式直接使用 DeepAdapter
//
// 文件目录：
//
//	adapter/
//	├── doc.go              # 包文档
//	├── interface.go        # AgentAdapter 接口定义
//	├── factory.go          # CreateAdapter 工厂 + ResolveSDKChoice
//	├── deep_adapter.go     # DeepAdapter Deep SDK 适配器
//	├── code_adapter.go     # CodeAdapter Code 模式适配器
//	├── interface_test.go   # 接口编译期检查测试
//	├── factory_test.go     # 工厂函数单元测试
//	├── deep_adapter_test.go # DeepAdapter 单元测试
//	└── code_adapter_test.go # CodeAdapter 单元测试
//
// 对应 Python 代码：jiuwenswarm/server/runtime/agent_adapter/agent_adapters.py
package adapter
