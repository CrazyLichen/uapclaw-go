// Package hooks 提供 Hooks 配置模型、事件定义与 Hook 执行引擎，对齐 Python jiuwenswarm/common/hooks_config.py + server/hooks/executor.py。
//
// 本包定义 HookType/HookEvent 枚举常量、CommandHookConfig/PromptHookConfig 配置模型、
// HookMatcher 匹配逻辑、HooksConfig 聚合与加载函数。
// Gateway 和 AgentServer 均可引用此包（全局共享，不依赖 server/ 或 agentcore/ 以外的业务域）。
// 11.13 起 HookExecutor 迁入本包，Gateway 与 AgentServer 共享
// （对齐 Python gateway/hooks/handler.py 对 server/hooks/executor.py 的直接引用，规避 Go gateway→server 禁令）。
//
// 文件目录：
//
//	hooks/
//	├── doc.go              # 包文档
//	├── types.go            # HookType 枚举 + HookEvent 常量 + AgentRailEvents/GatewayEvents 分组
//	├── config.go           # CommandHookConfig/PromptHookConfig/HookMatcher/HooksConfig/LoadHooksConfig
//	└── executor.go         # HookExecutor + HookResult + RegisterConfig + ParseCommandOutput + ExtractJSONFromResponse
//
// 对应 Python 代码：jiuwenswarm/common/hooks_config.py + jiuwenswarm/server/hooks/executor.py
package hooks
