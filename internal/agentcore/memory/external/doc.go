// Package external 提供外部记忆提供者（MemoryProvider）的接口定义和默认实现。
//
// MemoryProvider 是外部记忆系统的统一接口协议，使 Agent 可以接入不同的
// 外部记忆后端（Mem0、OpenViking、AgentArts、OpenJiuwen LTM），而无需修改
// Agent 核心逻辑。ExternalMemoryRail 在 Agent 生命周期钩子中桥接 Provider 方法。
//
// 文件目录：
//
//	external/
//	├── doc.go                    # 包文档
//	├── provider.go               # MemoryProvider 接口 + BaseMemoryProvider + ToolSchema + ProviderOption
//	├── agentarts_client.go       # AgentArts HTTP 客户端（REST API 封装：searchMemories/createMemorySession/addMessages）
//	├── agentarts_provider.go     # AgentArtsProvider — MemoryProvider 的 AgentArts 实现（session 映射+consecutiveFailures 计数）
//	├── mem0_client.go            # Mem0 HTTP 客户端（REST API 封装：search/getAll/add）
//	├── mem0_provider.go          # Mem0Provider — MemoryProvider 的 Mem0 实现（熔断器+工具调用）
//	├── openjiuwen_provider.go    # OpenJiuwenProvider — MemoryProvider 的 openjiuwen LTM 实现（全局单例+双模式构造）
//	├── viking_client.go          # OpenViking HTTP 客户端（REST API 封装：health/post/get/close + 身份 Header）
//	└── viking_provider.go        # OpenVikingProvider — MemoryProvider 的 OpenViking 实现（5 工具+会话管理）
//
// 对应 Python 代码：openjiuwen/core/memory/external/
package external
