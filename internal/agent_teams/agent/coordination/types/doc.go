// Package types 提供协调子系统的共享类型定义。
//
// 包含事件类型、协议接口和回调函数签名。
// coordination 包和 handlers 子包均依赖此包，打破循环依赖。
//
// 文件目录：
//
//	types/
//	├── doc.go       # 包文档
//	├── events.go    # CoordinationEvent / InnerEventType / InnerEventMessage
//	├── protocols.go # AgentRoundController / TeamLifecycleController / PollController / DispatcherHost / DispatcherBlueprint / DispatcherInfra
//	└── callbacks.go # EventCallbackFunc / CallbacksProvider
//
// 对应 Python 代码：openjiuwen/agent_teams/agent/coordination/event_bus.py + dispatcher.py 的协议定义
package types
