// Package external 提供外部记忆提供者（MemoryProvider）的接口定义和默认实现。
//
// MemoryProvider 是外部记忆系统的统一接口协议，使 Agent 可以接入不同的
// 外部记忆后端（Mem0、OpenViking、AgentArts、OpenJiuwen LTM），而无需修改
// Agent 核心逻辑。ExternalMemoryRail 在 Agent 生命周期钩子中桥接 Provider 方法。
//
// 文件目录：
//
//	external/
//	├── doc.go           # 包文档
//	└── provider.go      # MemoryProvider 接口 + BaseMemoryProvider + ToolSchema + ProviderOption
//
// 对应 Python 代码：openjiuwen/core/memory/external/
package external
