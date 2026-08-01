// Package extensions 提供扩展系统基础设施，对齐 Python jiuwenswarm/extensions/。
//
// 本包定义了扩展注册中心（ExtensionRegistry）、数据类型（ExtensionMetadata/ExtensionConfig）、
// 钩子事件常量（GatewayHookEvents/AgentServerHookEvents）、钩子上下文（HookContext 系列）
// 以及扩展 SDK 基类（BaseExtension/AgentServerClientExtension）。
//
// 最小子集已实现（10.5.1~10.5.6），回调机制复用 agentcore/runner/callback.CallbackFramework。
// 延后部分：ExtensionLoader（10.5.7 ⤵️）、ExtensionManager（10.5.8 ⤵️）、CryptoUtility（10.5.10 ⤵️）。
// CallbackCompat 不需要（Go 已有 CallbackFramework，10.5.9 ⤴️）。
//
// 文件目录：
//
//	extensions/
//	├── doc.go                 # 包文档
//	├── types.go               # ExtensionMetadata + ExtensionConfig（10.5.1）
//	├── hook_event.go          # GatewayHookEvents + AgentServerHookEvents（10.5.2）
//	├── hooks_context.go       # MemoryHookContext/GatewayChatHookContext/AgentServerChatHookContext/SystemPromptHookContext（10.5.3）
//	├── registry.go            # ExtensionRegistry 单例 + 回调触发（10.5.6）
//	├── loader.go              # ExtensionLoader stub（10.5.7 ⤵️）
//	├── manager.go             # ExtensionManager stub（10.5.8 ⤵️）
//	├── callback_compat.go     # 注释说明已覆盖（10.5.9 ⤴️）
//	└── sdk/
//	    ├── doc.go             # SDK 子包文档
//	    ├── base.go            # BaseExtension 接口 + 默认实现（10.5.4）
//	    ├── agent_server_client.go # AgentServerClientExtension（10.5.5）
//	    └── crypto_utility.go  # CryptoUtility stub（10.5.10 ⤵️）
//
// 对应 Python 代码：jiuwenswarm/extensions/
package extensions
