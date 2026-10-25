// Package heartbeat 提供 Gateway 心跳服务：周期性向 AgentServer 发送探活请求。
//
// GatewayHeartbeatService 是 Gateway 内的旁路定时器（不经 MessageHandler 入站）：
// 自造 E2A 请求经 AgentClient.SendRequest 发往 AgentServer，仅在 relay 阶段
// 借用 MessageHandler.PublishRobotMessages 出站通道将 heartbeat.relay 事件
// 推送到配置的 RelayChannelID（如 WebChannel，前端 HeartbeatMessageModal 弹窗）。
//
// AgentServer 侧接收链已就绪：HandleHeartbeat（deep_adapter.go）解析
// params.heartbeat / params.run.kind=heartbeat，agentcore RunKindHeartbeat
// → HeartbeatRail 注入 HEARTBEAT.md 读取。
//
// 文件目录：
//
//	heartbeat/
//	├── doc.go           # 包文档
//	├── heartbeat.go     # 常量 + HeartbeatConfig + 接口 + GatewayHeartbeatService
//	└── heartbeat_test.go
//
// 对应 Python 代码：jiuwenswarm/gateway/heartbeat/heartbeat.py
package heartbeat
