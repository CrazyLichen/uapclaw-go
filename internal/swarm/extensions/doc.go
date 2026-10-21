// Package extensions 提供扩展系统基础设施，对齐 Python jiuwenswarm/extensions/。
//
// 本包定义扩展注册中心（ExtensionRegistry）、数据类型（ExtensionMetadata/ExtensionConfig）、
// 钩子事件常量与钩子上下文（含 ToMap/FromMap 双向转换）、扩展加载器与生命周期管理器
// （ExtensionLoader/ExtensionManager，方案 D：hashicorp go-plugin 子进程），
// 以及事件桥写回辅助函数（TriggerMemoryBeforeChat/TriggerMemoryAfterChat）。
// 回调机制复用 agentcore/runner/callback.CallbackFramework。
// 延后部分：CryptoUtility（10.5.10 ⤵️）。CallbackCompat 不需要（10.5.9 ⤴️）。
//
// 文件目录：
//
//	extensions/
//	├── doc.go               # 包文档
//	├── types.go             # ExtensionMetadata + ExtensionConfig（10.5.1）
//	├── hook_event.go        # GatewayHookEvents + AgentServerHookEvents（10.5.2）
//	├── hooks_context.go     # 4 种 HookContext + ToMap/FromMap（10.5.3，FromMap 随 10.5.7 回填）
//	├── registry.go          # ExtensionRegistry 单例 + 回调触发（10.5.6）
//	├── trigger_helper.go    # 记忆钩子触发写回辅助（随 10.5.7 回填）
//	├── loader.go            # ExtensionLoader：发现/加载/声明注册编排（10.5.7）
//	├── manager.go           # ExtensionManager：WithSearchPaths + 生命周期（10.5.8）
//	├── callback_compat.go   # 注释说明已覆盖（10.5.9 ⤴️）
//	└── sdk/                 # 扩展 SDK 子包（10.5.4/10.5.5）
//
// 对应 Python 代码：jiuwenswarm/extensions/
package extensions
