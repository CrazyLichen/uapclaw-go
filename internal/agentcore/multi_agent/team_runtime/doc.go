// Package team_runtime 提供多 Agent 团队的消息通信运行时基础设施。
//
// 包含消息信封（MessageEnvelope）、订阅管理器（SubscriptionManager）、
// 消息路由器（MessageRouter）、消息总线（MessageBus）、
// 团队运行时（TeamRuntime）和可通信 Agent（CommunicableAgent）。
//
// 支持两种通信模式：
//   - P2P（点对点）：请求-响应模式，发送方等待接收方返回结果
//   - Pub-Sub（发布订阅）：发后即忘模式，消息扇出到所有匹配订阅者
//
// 底层消息队列复用 runner/message_queue 包的 MessageQueueInMemory。
//
// 文件目录：
//
//	team_runtime/
//	├── doc.go                # 包文档
//	├── envelope.go           # MessageEnvelope 消息信封
//	├── subscription_manager.go # SubscriptionManager 订阅管理器
//	├── message_router.go     # MessageRouter 消息路由器
//	├── message_bus.go        # MessageBus + MessageBusConfig 消息总线
//	├── team_runtime.go       # TeamRuntime + RuntimeConfig 团队运行时
//	├── runtime_bindable.go   # RuntimeBindable 接口
//	└── communicable_agent.go # CommunicableAgent 可通信 Agent 实现
//
// 对应 Python 代码：openjiuwen/core/multi_agent/team_runtime/
package team_runtime
