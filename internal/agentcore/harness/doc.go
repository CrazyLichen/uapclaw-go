// Package harness 提供深度 Agent（DeepAgent）的核心编排能力，
// 包括双层 Agent 架构、热重配置、选择性 Rail 路由、任务循环控制等。
//
// DeepAgent 是外层编排 Agent，内层包装 ReActAgent 执行 Think-Act-Observe 循环。
// 外层负责：配置热重载、Rail 注册与选择性路由、任务循环驱动、
// 子 Agent 创建与调度、上下文引擎代理、会话状态管理。
//
// 文件目录：
//
//	harness/
//	├── doc.go               # 包文档
//	├── deep_agent.go        # DeepAgent 结构体及全部方法实现
//	├── factory.go           # CreateDeepAgent 工厂函数及辅助函数
//	└── registry.go          # HarnessConfig 注册表、发现机制与 Load 创建
//
// 对应 Python 代码：openjiuwen/harness/deep_agent.py, openjiuwen/harness/factory.py,
// openjiuwen/harness/harness_config/registry.py
package harness
