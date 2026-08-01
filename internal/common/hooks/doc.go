// Package hooks 提供 Hooks 配置模型和事件定义，对齐 Python jiuwenswarm/common/hooks_config.py。
//
// 本包定义 HookType/HookEvent 枚举常量、CommandHookConfig/PromptHookConfig 配置模型、
// HookMatcher 匹配逻辑、HooksConfig 聚合与加载函数。
// Gateway 和 AgentServer 均可引用此包（全局共享，不依赖 server/ 或 agentcore/）。
//
// 文件目录：
//
//	hooks/
//	├── doc.go    # 包文档
//	├── types.go  # HookType 枚举 + HookEvent 常量 + AgentRailEvents/GatewayEvents 分组
//	└── config.go # CommandHookConfig/PromptHookConfig/HookMatcher/HooksConfig/LoadHooksConfig
//
// 对应 Python 代码：jiuwenswarm/common/hooks_config.py
package hooks
